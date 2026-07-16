package main

import (
	"log"
	"net/http"
	"os"

	"github.com/n0remac/GoDom/admin"
	"github.com/n0remac/GoDom/auth"
	"github.com/n0remac/GoDom/database"
	ws "github.com/n0remac/GoDom/websocket"
	robotwebrtc "github.com/n0remac/OrcasMakers/webrtc"
)

const webPort = ":8081"

func main() {
	mux, registry, ds, authApp, cleanup, handled := setup()
	if handled {
		return
	}
	defer cleanup()

	Home(mux, registry)
	OpenSauce(mux)
	Robot(mux)
	robotwebrtc.Mount(mux, NavBar)
	Car(mux, authApp)
	Robotics(mux, ds, authApp)
	Software(mux, ds, authApp)
	Art(mux, ds, authApp)
	Design(mux, ds, authApp)

	go ws.WsHub.Run()
	log.Printf("Starting server on %s", webPort)
	log.Fatal(http.ListenAndServe(webPort, mux))
}

func setup() (*http.ServeMux, *ws.CommandRegistry, *database.DocumentStore, *auth.AuthApp, func(), bool) {
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
		return nil, nil, nil, nil, nil, true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	registry := ws.NewCommandRegistry()
	mux.HandleFunc("/ws/hub", ws.CreateWebsocket(registry))
	imageHandler, err := ds.ImageHandler("/images/")
	if err != nil {
		cleanup()
		log.Fatalf("failed to create image handler: %v", err)
	}
	mux.Handle("/images/", imageHandler)

	authApp := auth.AuthWithStores(mux, registry, store, store)
	admin.Mount(mux, authApp)

	warning, err := admin.MissingAdminWarning(store, os.Args[0])
	if err != nil {
		log.Printf("warning: unable to check admin configuration: %v", err)
	} else if warning != "" {
		log.Print(warning)
	}

	return mux, registry, ds, authApp, cleanup, false
}
