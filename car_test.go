package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/n0remac/GoDom/auth"
)

func validTelemetryJSON() string {
	return `{"protocol_version":1,"speed":12.5,"rpm":900,"gear":"D","fuel_percent":75,"headlights":true,"turn_signal":"off","temperature":21,"humidity":50,"pressure":1013,"altitude":12,"pitch":1,"roll":2,"acceleration":{"x":0,"y":0,"z":9.8},"gyro":{"x":0,"y":0,"z":0},"gps":{"fix":true,"latitude":48.69,"longitude":-122.91,"satellites":8,"speed":12.5},"receiver":"remote"}`
}

func TestCarPageContainsDashboardAndScript(t *testing.T) {
	page := CarPage().Render()
	for _, want := range []string{"Car Dashboard", "car-joystick", "Arm", "STOP", "/car/app.js"} {
		if !strings.Contains(page, want) {
			t.Errorf("car page missing %q", want)
		}
	}
}

func TestCarDeviceTokenAndSync(t *testing.T) {
	t.Setenv("CAR_DEVICE_TOKEN", "secret")
	relay := newCarRelay()
	bad := httptest.NewRecorder()
	relay.deviceSync(bad, httptest.NewRequest(http.MethodPost, "/api/car/device/sync", strings.NewReader(validTelemetryJSON())))
	if bad.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d", bad.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/car/device/sync", strings.NewReader(validTelemetryJSON()))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	relay.deviceSync(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("sync status = %d: %s", response.Code, response.Body.String())
	}
	var command carCommand
	if err := json.NewDecoder(response.Body).Decode(&command); err != nil {
		t.Fatal(err)
	}
	if command.Armed || command.Steering != 0 || command.Throttle != 0 {
		t.Fatalf("startup command = %+v", command)
	}
	state := httptest.NewRecorder()
	relay.state(state, httptest.NewRequest(http.MethodGet, "/api/car/state", nil))
	if !strings.Contains(state.Body.String(), `"online":true`) || !strings.Contains(state.Body.String(), `"rpm":900`) {
		t.Fatalf("state = %s", state.Body.String())
	}
}

func TestCarControlLifecycleAndTimeout(t *testing.T) {
	relay := newCarRelay()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	relay.now = func() time.Time { return now }
	relay.mu.Lock()
	relay.telemetry = CarTelemetry{ProtocolVersion: 1}
	relay.telemetryAt = now
	relay.hasTelemetry = true
	relay.mu.Unlock()
	user := &auth.User{ID: "admin-id", Email: "admin@example.com", Role: auth.RoleAdmin}
	withUser := func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), carUserContextKey{}, user))
	}
	armed := httptest.NewRecorder()
	relay.arm(armed, withUser(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))))
	if armed.Code != http.StatusOK {
		t.Fatalf("arm = %d: %s", armed.Code, armed.Body.String())
	}
	var initial carCommand
	_ = json.NewDecoder(armed.Body).Decode(&initial)
	if !initial.Armed || initial.SessionID == "" || initial.Steering != 0 || initial.Throttle != 0 {
		t.Fatalf("arm response = %+v", initial)
	}
	now = now.Add(100 * time.Millisecond)
	body := []byte(`{"session_id":"` + initial.SessionID + `","sequence":1,"steering":-25,"throttle":40}`)
	command := httptest.NewRecorder()
	relay.command(command, withUser(httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))))
	if command.Code != http.StatusOK {
		t.Fatalf("command = %d: %s", command.Code, command.Body.String())
	}
	now = now.Add(600 * time.Millisecond)
	relay.mu.Lock()
	expired := relay.currentCommandLocked(now)
	relay.mu.Unlock()
	if expired.Armed || expired.Steering != 0 || expired.Throttle != 0 {
		t.Fatalf("expired command = %+v", expired)
	}
}

func TestCarRejectsDuplicateSequenceAndStaleStop(t *testing.T) {
	relay := newCarRelay()
	now := time.Now()
	relay.now = func() time.Time { return now }
	user := &auth.User{ID: "one", Email: "one@example.com"}
	relay.mu.Lock()
	relay.hasTelemetry = true
	relay.telemetryAt = now
	relay.sessionID = "current"
	relay.controllerID = user.ID
	relay.controllerEmail = user.Email
	relay.sequence = 2
	relay.heartbeatAt = now
	relay.mu.Unlock()
	withUser := func(body string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		return r.WithContext(context.WithValue(r.Context(), carUserContextKey{}, user))
	}
	duplicate := httptest.NewRecorder()
	relay.command(duplicate, withUser(`{"session_id":"current","sequence":2,"steering":0,"throttle":0}`))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d", duplicate.Code)
	}
	now = now.Add(100 * time.Millisecond)
	staleStop := httptest.NewRecorder()
	relay.stop(staleStop, withUser(`{"session_id":"old"}`))
	relay.mu.RLock()
	active := relay.sessionID
	relay.mu.RUnlock()
	if active != "current" {
		t.Fatal("stale stop ended the current session")
	}
}

func TestCarConcurrentStateAndSync(t *testing.T) {
	t.Setenv("CAR_DEVICE_TOKEN", "secret")
	relay := newCarRelay()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(validTelemetryJSON()))
			req.Header.Set("Authorization", "Bearer secret")
			relay.deviceSync(httptest.NewRecorder(), req)
		}()
		go func() {
			defer wg.Done()
			relay.state(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		}()
	}
	wg.Wait()
}
