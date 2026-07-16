package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/n0remac/GoDom/websocket"
)

func TestHomeRedirectsToOpenSauceTemporarily(t *testing.T) {
	mux := http.NewServeMux()
	Home(mux, NewCommandRegistry())

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusTemporaryRedirect {
		t.Fatalf("GET / = %d, want %d", response.Code, http.StatusTemporaryRedirect)
	}
	if location := response.Header().Get("Location"); location != "/open-sauce" {
		t.Fatalf("GET / Location = %q, want %q", location, "/open-sauce")
	}
}

func TestHomeDoesNotRedirectUnknownPaths(t *testing.T) {
	mux := http.NewServeMux()
	Home(mux, NewCommandRegistry())

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("GET /missing = %d, want %d", response.Code, http.StatusNotFound)
	}
}
