package webrtc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRobotSocketRequiresToken(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("ROBOT_WEBRTC_TOKEN", "secret")
	hub := newSignalingHub()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ws/webrtc?room=robot&playerId=robot", nil)
	hub.handleWebSocket(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestProductionOrigin(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("PUBLIC_ORIGIN", "https://orcasmaker.com")
	hub := newSignalingHub()
	good := httptest.NewRequest(http.MethodGet, "/ws/webrtc?room=robot&playerId=browser", nil)
	good.Header.Set("Origin", "https://orcasmaker.com")
	if !hub.checkOrigin(good) {
		t.Fatal("expected public origin to be accepted")
	}
	bad := httptest.NewRequest(http.MethodGet, "/ws/webrtc?room=robot&playerId=browser", nil)
	bad.Header.Set("Origin", "https://evil.example")
	if hub.checkOrigin(bad) {
		t.Fatal("expected foreign origin to be rejected")
	}
}

func TestSignalingRoutesAndRejectsSpoofedSender(t *testing.T) {
	t.Setenv("ROBOT_WEBRTC_TOKEN", "secret")
	t.Setenv("ENVIRONMENT", "development")
	hub := newSignalingHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/webrtc", hub.handleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/webrtc?room=robot&playerId="

	robotHeader := http.Header{"Authorization": []string{"Bearer secret"}}
	robot, _, err := websocket.DefaultDialer.Dial(endpoint+"robot", robotHeader)
	if err != nil {
		t.Fatal(err)
	}
	defer robot.Close()
	browserHeader := http.Header{"Origin": []string{server.URL}}
	browser, _, err := websocket.DefaultDialer.Dial(endpoint+"browser-1", browserHeader)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()

	if err := browser.WriteJSON(Message{Type: "join", From: "someone-else", To: robotID, Room: robotRoom}); err != nil {
		t.Fatal(err)
	}
	var protocolError Message
	if err := browser.ReadJSON(&protocolError); err != nil {
		t.Fatal(err)
	}
	if protocolError.Type != "error" {
		t.Fatalf("spoof response = %+v", protocolError)
	}
	if err := browser.WriteJSON(Message{Type: "join", From: "browser-1", To: robotID, Room: robotRoom}); err != nil {
		t.Fatal(err)
	}
	var forwarded Message
	if err := robot.ReadJSON(&forwarded); err != nil {
		t.Fatal(err)
	}
	if forwarded.Type != "join" || forwarded.From != "browser-1" {
		t.Fatalf("forwarded = %+v", forwarded)
	}

	browser2, _, err := websocket.DefaultDialer.Dial(endpoint+"browser-2", browserHeader)
	if err != nil {
		t.Fatal(err)
	}
	defer browser2.Close()
	_ = browser.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := browser.ReadMessage(); err == nil {
		t.Fatal("first controller remained connected after replacement")
	}
	var leave Message
	if err := robot.ReadJSON(&leave); err != nil {
		t.Fatal(err)
	}
	if leave.Type != "leave" || leave.From != "browser-1" {
		t.Fatalf("replacement leave = %+v", leave)
	}

	if err := browser2.WriteMessage(websocket.TextMessage, []byte(`{"type":`)); err != nil {
		t.Fatal(err)
	}
	var malformedError Message
	if err := browser2.ReadJSON(&malformedError); err != nil {
		t.Fatal(err)
	}
	if malformedError.Type != "error" {
		t.Fatalf("malformed response = %+v", malformedError)
	}
}
