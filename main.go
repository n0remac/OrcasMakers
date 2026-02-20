package main

import (
	"log"
	"net/http"

	. "github.com/n0remac/OrcasMakers/websocket"
)

const (
	webPort = ":8080"
)

func main() {
	// Create a new HTTP server
	mux := http.NewServeMux()
	// create global registry
	globalRegistry := NewCommandRegistry()
	mux.HandleFunc("/ws/hub", CreateWebsocket(globalRegistry))

	// Apps
	Home(mux, globalRegistry)

	go WsHub.Run()
	log.Printf("Starting server on %s", webPort)
	log.Fatal(http.ListenAndServe(webPort, mux))
}
