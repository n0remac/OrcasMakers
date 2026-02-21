package main

import (
	"log"
	"net/http"
	"os"

	"github.com/n0remac/GoDom/admin"
	"github.com/n0remac/GoDom/auth"
	"github.com/n0remac/GoDom/database"
	ws "github.com/n0remac/GoDom/websocket"
)

const webPort = ":8080"

func main() {
	mux, registry, cleanup, handled := setup()
	if handled {
		return
	}
	defer cleanup()

	Home(mux, registry)
	Robotics(mux)

	go ws.WsHub.Run()
	log.Printf("Starting server on %s", webPort)
	log.Fatal(http.ListenAndServe(webPort, mux))
}

func setup() (*http.ServeMux, *ws.CommandRegistry, func(), bool) {
	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}
	ds, err := database.NewSQLiteStoreFromDSN("data/orcasmakers.sqlite")
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	cleanup := func() { _ = ds.Close() }

	store := auth.NewSQLiteStore(ds)

	handled, message, err := admin.HandleCLI(store, os.Args[1:], os.Args[0])
	if err != nil {
		cleanup()
		log.Fatal(err)
	}
	if message != "" {
		log.Print(message)
	}
	if handled {
		cleanup()
		return nil, nil, nil, true
	}

	mux := http.NewServeMux()
	registry := ws.NewCommandRegistry()
	mux.HandleFunc("/ws/hub", ws.CreateWebsocket(registry))

	authApp := auth.AuthWithStores(mux, registry, store, store)
	admin.Mount(mux, authApp)

	warning, err := admin.MissingAdminWarning(store, os.Args[0])
	if err != nil {
		log.Printf("warning: unable to check admin configuration: %v", err)
	} else if warning != "" {
		log.Print(warning)
	}

	return mux, registry, cleanup, false
}
