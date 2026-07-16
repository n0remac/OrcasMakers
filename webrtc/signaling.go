package webrtc

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	robotID       = "robot"
	robotRoom     = "robot"
	writeWait     = 10 * time.Second
	pongWait      = 60 * time.Second
	pingPeriod    = 25 * time.Second
	maxSignalSize = 1 << 20
)

var peerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Message struct {
	Type      string          `json:"type"`
	Name      string          `json:"name,omitempty"`
	From      string          `json:"from,omitempty"`
	To        string          `json:"to,omitempty"`
	Room      string          `json:"room,omitempty"`
	Offer     json.RawMessage `json:"offer,omitempty"`
	Answer    json.RawMessage `json:"answer,omitempty"`
	Candidate json.RawMessage `json:"candidate,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type signalClient struct {
	id   string
	conn *websocket.Conn
	send chan []byte
	hub  *signalingHub
	once sync.Once
	done chan struct{}
}

type signalingHub struct {
	mu         sync.RWMutex
	robot      *signalClient
	controller *signalClient
	upgrader   websocket.Upgrader
}

func newSignalingHub() *signalingHub {
	h := &signalingHub{}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     h.checkOrigin,
	}
	return h
}

func (h *signalingHub) checkOrigin(r *http.Request) bool {
	peerID := r.URL.Query().Get("playerId")
	origin := r.Header.Get("Origin")
	if origin == "" {
		return peerID == robotID && validRobotToken(r)
	}
	if os.Getenv("ENVIRONMENT") != "production" {
		return true
	}
	want, err := url.Parse(envOrDefault("PUBLIC_ORIGIN", "https://orcasmaker.com"))
	got, parseErr := url.Parse(origin)
	return err == nil && parseErr == nil && got.Scheme == want.Scheme && got.Host == want.Host
}

func (h *signalingHub) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	peerID := strings.TrimSpace(r.URL.Query().Get("playerId"))
	if r.URL.Query().Get("room") != robotRoom || !peerIDPattern.MatchString(peerID) {
		http.Error(w, "invalid room or playerId", http.StatusBadRequest)
		return
	}
	if peerID == robotID && !validRobotToken(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &signalClient{id: peerID, conn: conn, send: make(chan []byte, 64), done: make(chan struct{}), hub: h}
	h.register(client)
	go client.writePump()
	client.readPump()
}

func (h *signalingHub) register(client *signalClient) {
	h.mu.Lock()
	var previous *signalClient
	if client.id == robotID {
		previous, h.robot = h.robot, client
	} else {
		previous, h.controller = h.controller, client
	}
	h.mu.Unlock()
	if previous != nil {
		previous.close()
	}
}

func (h *signalingHub) unregister(client *signalClient) {
	client.once.Do(func() {
		h.mu.Lock()
		if h.robot == client {
			h.robot = nil
		}
		if h.controller == client {
			h.controller = nil
		}
		h.mu.Unlock()
		leave, _ := json.Marshal(Message{Type: "leave", From: client.id, Room: robotRoom})
		h.sendToOther(client.id, leave)
		close(client.done)
		_ = client.conn.Close()
	})
}

func (h *signalingHub) sendToOther(from string, payload []byte) {
	h.mu.RLock()
	var target *signalClient
	if from == robotID {
		target = h.controller
	} else {
		target = h.robot
	}
	h.mu.RUnlock()
	if target == nil {
		return
	}
	select {
	case target.send <- payload:
	default:
		target.close()
	}
}

func (c *signalClient) readPump() {
	defer c.close()
	c.conn.SetReadLimit(maxSignalSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		messageType, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			c.sendError("text messages required")
			continue
		}
		var message Message
		if err := json.Unmarshal(raw, &message); err != nil {
			c.sendError("invalid JSON")
			continue
		}
		if err := validateSignal(c.id, &message); err != nil {
			c.sendError(err.Error())
			continue
		}
		message.From = c.id
		message.Room = robotRoom
		payload, _ := json.Marshal(message)
		c.hub.sendToOther(c.id, payload)
	}
}

func validateSignal(peerID string, message *Message) error {
	if message.From != "" && message.From != peerID {
		return errors.New("sender identity mismatch")
	}
	if peerID != robotID && message.To != "" && message.To != robotID {
		return errors.New("invalid target")
	}
	if peerID == robotID && message.To == robotID {
		return errors.New("invalid target")
	}
	switch message.Type {
	case "join", "leave":
		return nil
	case "offer":
		if len(message.Offer) == 0 || len(message.Offer) > maxSignalSize {
			return errors.New("invalid offer")
		}
	case "answer":
		if len(message.Answer) == 0 || len(message.Answer) > maxSignalSize {
			return errors.New("invalid answer")
		}
	case "candidate":
		if len(message.Candidate) == 0 || len(message.Candidate) > 64<<10 {
			return errors.New("invalid candidate")
		}
	default:
		return errors.New("unknown signal type")
	}
	return nil
}

func (c *signalClient) sendError(text string) {
	payload, _ := json.Marshal(Message{Type: "error", Error: text})
	select {
	case c.send <- payload:
	default:
		c.close()
	}
}

func (c *signalClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer c.close()
	for {
		select {
		case <-c.done:
			return
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok || c.conn.WriteMessage(websocket.TextMessage, payload) != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if c.conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return
			}
		}
	}
}

func (c *signalClient) close() { c.hub.unregister(c) }
