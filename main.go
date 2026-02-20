package main

import (
	"log"
	"net/http"

	. "github.com/n0remac/robot-webrtc/websocket"
)

const (
	webPort = ":8080"
)

func main() {
	// Create a new HTTP server
	mux := http.NewServeMux()
	// create global registry
	globalRegistry := NewCommandRegistry()

	// Apps
	Home(mux, globalRegistry)

	go WsHub.Run()
	log.Printf("Starting server on %s", webPort)
	log.Fatal(http.ListenAndServe(webPort, mux))
}
