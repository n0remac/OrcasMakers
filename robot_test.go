package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRobotPageContainsControls(t *testing.T) {
	page := RobotPage().Render()
	for _, want := range []string{"Robot Control", `data-key="w"`, "/robot/stream", "/robot/app.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("robot page does not contain %q", want)
		}
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
