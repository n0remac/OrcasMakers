package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRobotPageContainsControls(t *testing.T) {
	page := RobotPage().Render()
	for _, want := range []string{
		"Robot Control",
		`data-key="w"`,
		`id="mobile-robot-controls"`,
		`id="move-joystick"`,
		`id="claw-joystick"`,
		`id="camera-joystick"`,
		`aria-label="Open claw"`,
		`aria-label="Close claw"`,
		`id="mobile-control-space"`,
		`id="desktop-robot-controls"`,
		`@media (pointer: coarse) and (orientation: portrait)`,
		`@media (pointer: coarse) and (orientation: landscape)`,
		`env(safe-area-inset-bottom)`,
		"/robot/stream",
		"/robot/app.js",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("robot page does not contain %q", want)
		}
	}
	if strings.Contains(page, "robot-controller-qr") {
		t.Error("robot page must not display the controller QR code")
	}
	if !strings.Contains(robotAppJS, `window.addEventListener('orientationchange', releaseAll)`) {
		t.Error("robot controls must release when the device orientation changes")
	}
	if !strings.Contains(robotAppJS, "viewport-fit=cover") {
		t.Error("robot controls must opt into viewport safe-area insets")
	}
}

func TestRobotControllerQR(t *testing.T) {
	mux := http.NewServeMux()
	Robot(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/robot/controller-qr.png", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("QR status = %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("QR content type = %q", got)
	}
	if !bytes.Equal(response.Body.Bytes(), robotControllerQR) {
		t.Fatal("QR response does not match the embedded image")
	}

	post := httptest.NewRecorder()
	mux.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/robot/controller-qr.png", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("QR POST status = %d", post.Code)
	}
}

func TestRobotRejectsBadToken(t *testing.T) {
	t.Setenv("ROBOT_TOKEN", "secret")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/ws/robot", nil)
	robots.handleRobot(recorder, request)
	if recorder.Code != 401 {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestRelayForwardsControlToRobot(t *testing.T) {
	t.Setenv("ROBOT_TOKEN", "secret")
	relay := &robotRelay{}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/robot", relay.handleRobot)
	mux.HandleFunc("/ws/robot-control", relay.handleController)
	server := httptest.NewServer(mux)
	defer server.Close()

	robotHeader := http.Header{"Authorization": []string{"Bearer secret"}}
	robot, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/robot", robotHeader)
	if err != nil {
		t.Fatal(err)
	}
	defer robot.Close()
	controller, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws/robot-control", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer controller.Close()
	_, _, _ = controller.ReadMessage() // initial robot status

	want := `{"key":"w","action":"pressed"}`
	if err := controller.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
		t.Fatal(err)
	}
	messageType, got, err := robot.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.TextMessage || string(got) != want {
		t.Fatalf("forwarded message = (%d, %q), want text %q", messageType, got, want)
	}
}
