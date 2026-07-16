package webrtc

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resetTurnLimiter() {
	turnLimiter.Lock()
	turnLimiter.clients = make(map[string]turnLimiterEntry)
	turnLimiter.Unlock()
}

func TestTurnCredentialsRequireSecret(t *testing.T) {
	resetTurnLimiter()
	t.Setenv("TURN_SHARED_SECRET", "")
	t.Setenv("TURN_SHARED_SECRET_FILE", t.TempDir()+"/missing")
	response := httptest.NewRecorder()
	handleTurnCredentials(response, httptest.NewRequest(http.MethodGet, "/webrtc/turn-credentials", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestTurnCredentialsMatchCoturnHMAC(t *testing.T) {
	resetTurnLimiter()
	t.Setenv("TURN_SHARED_SECRET", "test-secret")
	t.Setenv("TURN_SHARED_SECRET_FILE", t.TempDir()+"/missing")
	t.Setenv("TURN_HOST", "turn.example.com")
	response := httptest.NewRecorder()
	handleTurnCredentials(response, httptest.NewRequest(http.MethodGet, "/webrtc/turn-credentials?user=tester", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		URLs     []string `json:"urls"`
		URIs     []string `json:"uris"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(body.Username, ":tester") || len(body.URLs) != 2 || len(body.URIs) != 2 {
		t.Fatalf("unexpected response: %+v", body)
	}
	mac := hmac.New(sha1.New, []byte("test-secret"))
	_, _ = mac.Write([]byte(body.Username))
	if body.Password != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("credential does not match Coturn REST HMAC")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credential response may not be cached")
	}
}

func TestTurnCredentialRateLimit(t *testing.T) {
	resetTurnLimiter()
	t.Setenv("TURN_SHARED_SECRET", "test-secret")
	t.Setenv("TURN_SHARED_SECRET_FILE", t.TempDir()+"/missing")
	for i := 0; i < turnRateLimit; i++ {
		response := httptest.NewRecorder()
		handleTurnCredentials(response, httptest.NewRequest(http.MethodGet, "/webrtc/turn-credentials", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("request %d status = %d", i+1, response.Code)
		}
	}
	response := httptest.NewRecorder()
	handleTurnCredentials(response, httptest.NewRequest(http.MethodGet, "/webrtc/turn-credentials", nil))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited status = %d", response.Code)
	}
}
