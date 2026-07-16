package webrtc

import (
	_ "embed"
	"net/http"
	"time"

	. "github.com/n0remac/GoDom/html"
)

// These assets originate in robot-webrtc/webrtc and are embedded so the
// deployed OrcasMakers binary has no runtime dependency on the source tree.
//
//go:embed robot-control.js
var robotControlJS []byte

//go:embed video.css
var videoCSS []byte

//go:embed logger.js
var loggerJS []byte

// Mount installs the WebRTC robot page, assets, TURN credentials, and its
// feature-local signaling socket.
func Mount(mux *http.ServeMux, navigation ...func() *Node) {
	hub := newSignalingHub()
	pageHandler := RobotControlHandler
	if len(navigation) > 0 && navigation[0] != nil {
		pageHandler = func(w http.ResponseWriter, r *http.Request) {
			serveRobotPage(w, r, navigation[0]())
		}
	}
	mux.HandleFunc("/webrtc", pageHandler)
	mux.HandleFunc("/webrtc/app.js", serveAsset("text/javascript; charset=utf-8", robotControlJS))
	mux.HandleFunc("/webrtc/video.css", serveAsset("text/css; charset=utf-8", videoCSS))
	mux.HandleFunc("/webrtc/logger.js", serveAsset("text/javascript; charset=utf-8", loggerJS))
	mux.HandleFunc("/webrtc/turn-credentials", handleTurnCredentials)
	mux.HandleFunc("/ws/webrtc", hub.handleWebSocket)
}

func RobotControlHandler(w http.ResponseWriter, r *http.Request) {
	serveRobotPage(w, r, nil)
}

func serveRobotPage(w http.ResponseWriter, r *http.Request, navigation *Node) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ServeNode(robotPage(navigation))(w, r)
}

func RobotPage() *Node { return robotPage(nil) }

func robotPage(navigation *Node) *Node {
	if navigation == nil {
		navigation = Nil()
	}
	button := func(key, label string) *Node {
		return Button(
			Class("control-btn btn btn-square btn-lg select-none touch-none"),
			Attr("data-key", key),
			AriaLabel(label),
			T(label),
		)
	}
	empty := func() *Node { return Span(Class("w-16")) }
	pad := func(title, top, left, middle, right string) *Node {
		return Section(
			Class("space-y-3 text-center"),
			H2(Class("font-semibold"), T(title)),
			Div(Class("grid grid-cols-3 gap-2 justify-items-center"),
				empty(), button(top, top), empty(),
				button(left, left), button(middle, middle), button(right, right),
			),
		)
	}

	return DefaultLayout(
		Link(Rel("stylesheet"), Href("/webrtc/video.css")),
		Div(Attrs(map[string]string{"class": "min-h-screen bg-base-100", "data-theme": "dark"}),
			navigation,
			Main(Class("mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 md:p-6"),
				Div(Class("flex flex-wrap items-center justify-between gap-4"),
					Div(H1(Class("text-2xl font-bold"), T("WebRTC Robot Control")), P(Class("text-sm text-base-content/70"), T("Live peer-to-peer video and controls"))),
					Span(Id("webrtc-status"), Class("badge badge-warning"), T("Robot offline")),
				),
				Div(Id("video-area"), Class("relative grid min-h-60 place-items-center overflow-hidden rounded-box border border-base-300 bg-black"),
					Video(Id("robot-video"), Class("aspect-4/3 w-full max-w-3xl object-contain"), Attr("autoplay", ""), Attr("playsinline", "")),
				),
				Button(Id("start-video-btn"), Class("btn btn-primary self-center"), T("Connect to Robot")),
				Div(Id("control-buttons"), Class("grid grid-cols-1 gap-8 md:grid-cols-2 lg:grid-cols-4"),
					pad("Move", "W", "A", "S", "D"),
					pad("Claw", "T", "F", "G", "H"),
					Section(Class("space-y-3 text-center"), H2(Class("font-semibold"), T("Open / Close")), Div(Class("grid grid-cols-3 gap-2 justify-items-center"), empty(), button("r", "R"), empty(), empty(), button("y", "Y"), empty())),
					pad("Camera", "I", "J", "K", "L"),
				),
			),
		),
		Script(Src("/webrtc/logger.js")),
		Script(Src("/webrtc/app.js")),
	)
}

func serveAsset(contentType string, contents []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=300")
		http.ServeContent(w, r, "asset", time.Time{}, newByteReadSeeker(contents))
	}
}
