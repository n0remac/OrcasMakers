package main

import (
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	. "github.com/n0remac/GoDom/html"
)

const maxRobotFrameSize = 16 << 20

type robotRelay struct {
	mu sync.RWMutex

	robot          *websocket.Conn
	robotWriteMu   sync.Mutex
	controller     *websocket.Conn
	controlWriteMu sync.Mutex

	frame []byte
	seq   uint64
}

var robots = &robotRelay{}

var robotUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 256 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || strings.HasSuffix(origin, "://"+r.Host)
	},
}

func Robot(mux *http.ServeMux) {
	mux.HandleFunc("/robot", ServeNode(RobotPage()))
	mux.HandleFunc("/robot/app.js", serveRobotApp)
	mux.HandleFunc("/robot/stream", robots.serveStream)
	mux.HandleFunc("/ws/robot", robots.handleRobot)
	mux.HandleFunc("/ws/robot-control", robots.handleController)
}

func RobotPage() *Node {
	button := func(key, label string) *Node {
		return Button(
			Attr("data-key", key),
			Class("btn btn-square btn-lg select-none touch-none"),
			T(label),
		)
	}
	empty := func() *Node { return Span(Class("w-16")) }
	pad := func(title string, top, left, middle, right string) *Node {
		return Section(
			Class("space-y-3 text-center"),
			H2(Class("font-semibold"), T(title)),
			Div(Class("grid grid-cols-3 gap-2 justify-items-center"),
				empty(), button(top, strings.ToUpper(top)), empty(),
				button(left, strings.ToUpper(left)), button(middle, strings.ToUpper(middle)), button(right, strings.ToUpper(right)),
			),
		)
	}

	return DefaultLayout(
		Div(Class("min-h-screen bg-base-100"),
			NavBar(),
			Main(Class("mx-auto w-full max-w-5xl space-y-6 p-4 md:p-6"),
				Div(Class("flex items-center justify-between gap-4"),
					H1(Class("text-2xl font-bold"), T("Robot Control")),
					Span(Id("robot-status"), Class("text-warning"), T("Connecting…")),
				),
				Div(Class("grid min-h-60 place-items-center overflow-hidden rounded-box border border-base-300 bg-base-200"),
					Img(Src("/robot/stream"), Alt("Robot camera stream"), Class("aspect-4/3 w-full max-w-3xl object-contain")),
				),
				Div(Class("grid grid-cols-1 gap-8 sm:grid-cols-2 lg:grid-cols-4"),
					pad("Move", "w", "a", "s", "d"),
					pad("Claw", "t", "f", "g", "h"),
					Section(Class("space-y-3 text-center"),
						H2(Class("font-semibold"), T("Open / Close")),
						Div(Class("grid grid-cols-3 gap-2 justify-items-center"), empty(), button("r", "R"), empty(), empty(), button("y", "Y"), empty()),
					),
					pad("Camera", "i", "j", "k", "l"),
				),
			),
		),
		Script(Src("/robot/app.js")),
	)
}

func serveRobotApp(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.WriteString(w, robotAppJS)
}

func (r *robotRelay) handleRobot(w http.ResponseWriter, req *http.Request) {
	if !validRobotToken(req) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := robotUpgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(maxRobotFrameSize)

	r.mu.Lock()
	previous := r.robot
	r.robot = conn
	r.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	r.sendStatus()
	log.Printf("robot connected from %s", req.RemoteAddr)

	defer func() {
		_ = conn.Close()
		r.mu.Lock()
		if r.robot == conn {
			r.robot = nil
			r.frame = nil
			r.seq++
		}
		r.mu.Unlock()
		r.sendStatus()
		log.Printf("robot disconnected from %s", req.RemoteAddr)
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage || len(data) == 0 {
			continue
		}
		r.mu.Lock()
		r.frame = append(r.frame[:0], data...)
		r.seq++
		r.mu.Unlock()
	}
}

func (r *robotRelay) handleController(w http.ResponseWriter, req *http.Request) {
	conn, err := robotUpgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))

	r.mu.Lock()
	previous := r.controller
	r.controller = conn
	r.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	r.sendStatus()

	defer func() {
		_ = conn.Close()
		wasActive := false
		r.mu.Lock()
		if r.controller == conn {
			r.controller = nil
			wasActive = true
		}
		r.mu.Unlock()
		if wasActive {
			r.sendToRobot([]byte(`{"type":"stop"}`))
		}
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.TextMessage {
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			r.sendToRobot(data)
		}
	}
}

func (r *robotRelay) sendToRobot(data []byte) {
	r.robotWriteMu.Lock()
	defer r.robotWriteMu.Unlock()
	r.mu.RLock()
	conn := r.robot
	r.mu.RUnlock()
	if conn != nil {
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}
}

func (r *robotRelay) sendStatus() {
	r.controlWriteMu.Lock()
	defer r.controlWriteMu.Unlock()
	r.mu.RLock()
	controller := r.controller
	connected := r.robot != nil
	r.mu.RUnlock()
	if controller != nil {
		_ = controller.WriteJSON(map[string]any{"type": "status", "connected": connected})
	}
}

func (r *robotRelay) serveStream(w http.ResponseWriter, req *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	var sent uint64
	for {
		select {
		case <-req.Context().Done():
			return
		case <-ticker.C:
			r.mu.RLock()
			seq := r.seq
			frame := append([]byte(nil), r.frame...)
			r.mu.RUnlock()
			if len(frame) == 0 || seq == sent {
				continue
			}
			if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame)); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := io.WriteString(w, "\r\n"); err != nil {
				return
			}
			flusher.Flush()
			sent = seq
		}
	}
}

func validRobotToken(r *http.Request) bool {
	want := strings.TrimSpace(os.Getenv("ROBOT_TOKEN"))
	if want == "" {
		tokenFile := os.Getenv("ROBOT_TOKEN_FILE")
		if tokenFile == "" {
			tokenFile = "robot-token"
		}
		if contents, err := os.ReadFile(tokenFile); err == nil {
			want = strings.TrimSpace(string(contents))
		}
	}
	if want == "" {
		return os.Getenv("ENVIRONMENT") != "production"
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

const robotAppJS = `
const controlKeys = new Set(['w','a','s','d','t','f','g','h','i','j','k','l','r','y']);
const pressed = new Set();
const statusEl = document.querySelector('#robot-status');
let socket;
let reconnectTimer;

function connect() {
  clearTimeout(reconnectTimer);
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  socket = new WebSocket(protocol + '//' + location.host + '/ws/robot-control');
  socket.addEventListener('message', event => {
    const message = JSON.parse(event.data);
    if (message.type !== 'status') return;
    statusEl.textContent = message.connected ? 'Robot connected' : 'Robot offline';
    statusEl.classList.toggle('text-success', message.connected);
    statusEl.classList.toggle('text-warning', !message.connected);
  });
  socket.addEventListener('close', () => {
    releaseAll();
    statusEl.textContent = 'Disconnected — retrying…';
    statusEl.classList.remove('text-success');
    statusEl.classList.add('text-warning');
    reconnectTimer = setTimeout(connect, 1000);
  });
}

function send(message) {
  if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(message));
}
function setKey(key, isPressed) {
  if (!controlKeys.has(key) || (isPressed && pressed.has(key)) || (!isPressed && !pressed.has(key))) return;
  isPressed ? pressed.add(key) : pressed.delete(key);
  document.querySelector('[data-key="' + key + '"]')?.classList.toggle('btn-active', isPressed);
  send({key, action: isPressed ? 'pressed' : 'released'});
}
function releaseAll() { [...pressed].forEach(key => setKey(key, false)); }

window.addEventListener('keydown', event => { const key = event.key.toLowerCase(); if (controlKeys.has(key)) { event.preventDefault(); setKey(key, true); } });
window.addEventListener('keyup', event => { const key = event.key.toLowerCase(); if (controlKeys.has(key)) { event.preventDefault(); setKey(key, false); } });
window.addEventListener('blur', releaseAll);
document.addEventListener('visibilitychange', () => { if (document.hidden) releaseAll(); });
document.querySelectorAll('[data-key]').forEach(button => {
  const key = button.dataset.key;
  button.addEventListener('pointerdown', event => { event.preventDefault(); button.setPointerCapture(event.pointerId); setKey(key, true); });
  for (const name of ['pointerup','pointercancel','lostpointercapture']) button.addEventListener(name, () => setKey(key, false));
});
setInterval(() => send({type: 'heartbeat'}), 250);
connect();
`
