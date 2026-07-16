package webrtc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRobotPageAndAssets(t *testing.T) {
	page := RobotPage().Render()
	for _, want := range []string{"WebRTC Robot Control", `id="robot-video"`, `id="webrtc-status"`, "/webrtc/app.js", "/webrtc/video.css", "/webrtc/logger.js", `data-key="W"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q", want)
		}
	}
	mux := http.NewServeMux()
	Mount(mux)
	for path, contentType := range map[string]string{
		"/webrtc/app.js":    "text/javascript",
		"/webrtc/video.css": "text/css",
		"/webrtc/logger.js": "text/javascript",
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), contentType) || response.Body.Len() == 0 {
			t.Errorf("GET %s: status=%d content-type=%q bytes=%d", path, response.Code, response.Header().Get("Content-Type"), response.Body.Len())
		}
	}
}
