package webrtc

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultTurnHost       = "turn.orcasmaker.com"
	defaultTurnPort       = "3478"
	defaultTurnTTL  int64 = 3600
	turnRateLimit         = 10
)

type turnLimiterEntry struct {
	started time.Time
	count   int
}

var turnLimiter = struct {
	sync.Mutex
	clients map[string]turnLimiterEntry
}{clients: make(map[string]turnLimiterEntry)}

func handleTurnCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !allowTurnRequest(clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	secret := turnSharedSecret()
	if secret == "" {
		http.Error(w, "TURN is not configured", http.StatusServiceUnavailable)
		return
	}
	user := strings.TrimSpace(r.URL.Query().Get("user"))
	if user == "" {
		user = "browser"
	}
	if len(user) > 128 || strings.ContainsAny(user, "\r\n") {
		http.Error(w, "invalid user", http.StatusBadRequest)
		return
	}
	ttl := turnCredentialTTL()
	username, password := generateTurnCredentials(secret, user, ttl)
	host := envOrDefault("TURN_HOST", defaultTurnHost)
	port := envOrDefault("TURN_PORT", defaultTurnPort)
	urls := []string{
		fmt.Sprintf("turn:%s:%s?transport=udp", host, port),
		fmt.Sprintf("turn:%s:%s?transport=tcp", host, port),
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username": username,
		"password": password,
		"ttl":      ttl,
		"urls":     urls,
		"uris":     urls,
	})
}

func allowTurnRequest(ip string, now time.Time) bool {
	turnLimiter.Lock()
	defer turnLimiter.Unlock()
	entry := turnLimiter.clients[ip]
	if entry.started.IsZero() || now.Sub(entry.started) >= time.Minute {
		turnLimiter.clients[ip] = turnLimiterEntry{started: now, count: 1}
		return true
	}
	if entry.count >= turnRateLimit {
		return false
	}
	entry.count++
	turnLimiter.clients[ip] = entry
	if len(turnLimiter.clients) > 1024 {
		for key, candidate := range turnLimiter.clients {
			if now.Sub(candidate.started) >= time.Minute {
				delete(turnLimiter.clients, key)
			}
		}
	}
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if parsed := net.ParseIP(host); parsed != nil && parsed.IsLoopback() {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
			return forwarded
		}
	}
	return host
}

func turnSharedSecret() string {
	filename := envOrDefault("TURN_SHARED_SECRET_FILE", "/etc/orcasmakers/turn-shared-secret")
	if contents, err := os.ReadFile(filename); err == nil {
		return strings.TrimSpace(string(contents))
	}
	return strings.TrimSpace(os.Getenv("TURN_SHARED_SECRET"))
}

func turnCredentialTTL() int64 {
	ttl, err := strconv.ParseInt(os.Getenv("TURN_CREDENTIAL_TTL"), 10, 64)
	if err != nil || ttl < 60 || ttl > 86400 {
		return defaultTurnTTL
	}
	return ttl
}

func generateTurnCredentials(secret, user string, ttl int64) (string, string) {
	username := fmt.Sprintf("%d:%s", time.Now().Unix()+ttl, user)
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write([]byte(username))
	return username, base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func validRobotToken(r *http.Request) bool {
	want := strings.TrimSpace(os.Getenv("ROBOT_WEBRTC_TOKEN"))
	if want == "" {
		filename := envOrDefault("ROBOT_WEBRTC_TOKEN_FILE", "/etc/orcasmakers/robot-webrtc-token")
		if contents, err := os.ReadFile(filename); err == nil {
			want = strings.TrimSpace(string(contents))
		}
	}
	if want == "" {
		return os.Getenv("ENVIRONMENT") != "production"
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
