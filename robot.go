package main

import (
	"bytes"
	"crypto/subtle"
	_ "embed"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	. "github.com/n0remac/GoDom/html"
)

const maxRobotFrameSize = 16 << 20

//go:embed robotcontroller.png
var robotControllerQR []byte

type robotRelay struct {
	mu sync.RWMutex

	robot          *websocket.Conn
	robotWriteMu   sync.Mutex
	controller     *websocket.Conn
	controlWriteMu sync.Mutex

	frame []byte
	seq   uint64
}

var robots = &robotRelay{}

var robotUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 256 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || strings.HasSuffix(origin, "://"+r.Host)
	},
}

func Robot(mux *http.ServeMux) {
	mux.HandleFunc("/robot", ServeNode(RobotPage()))
	mux.HandleFunc("/robot/app.js", serveRobotApp)
	mux.HandleFunc("/robot/controller-qr.png", serveRobotControllerQR)
	mux.HandleFunc("/robot/stream", robots.serveStream)
	mux.HandleFunc("/ws/robot", robots.handleRobot)
	mux.HandleFunc("/ws/robot-control", robots.handleController)
}

func RobotPage() *Node {
	button := func(key, label string) *Node {
		return Button(
			Attr("data-key", key),
			Class("btn btn-square btn-lg select-none touch-none"),
			T(label),
		)
	}
	mobileButton := func(key, label, ariaLabel string) *Node {
		return Button(
			Attr("data-key", key),
			AriaLabel(ariaLabel),
			Class("robot-claw-button h-12 w-12 shrink-0 touch-none select-none rounded-full border border-white/60 bg-black/20 text-[10px] font-bold uppercase text-white shadow-lg backdrop-blur-sm"),
			T(label),
		)
	}
	joystick := func(id, label, up, left, down, right string) *Node {
		return Div(
			Id(id),
			Attr("data-joystick", ""),
			Attr("data-up", up),
			Attr("data-left", left),
			Attr("data-down", down),
			Attr("data-right", right),
			Attr("role", "group"),
			AriaLabel(label+" joystick"),
			Class("robot-joystick relative h-20 w-20 touch-none select-none rounded-full border-2 border-white/60 bg-black/15 shadow-lg backdrop-blur-[1px]"),
			Span(Class("pointer-events-none absolute inset-x-0 top-1 text-center text-[10px] font-bold uppercase tracking-wide text-white/90"), T(label)),
			Span(Attr("data-joystick-stick", ""), Class("pointer-events-none absolute left-1/2 top-1/2 h-8 w-8 -translate-x-1/2 -translate-y-1/2 rounded-full border border-white/70 bg-white/35 shadow-md")),
		)
	}
	empty := func() *Node { return Span(Class("w-16")) }
	pad := func(title string, top, left, middle, right string) *Node {
		return Section(
			Class("space-y-3 text-center"),
			H2(Class("font-semibold"), T(title)),
			Div(Class("grid grid-cols-3 gap-2 justify-items-center"),
				empty(), button(top, strings.ToUpper(top)), empty(),
				button(left, strings.ToUpper(left)), button(middle, strings.ToUpper(middle)), button(right, strings.ToUpper(right)),
			),
		)
	}

	return DefaultLayout(
		Div(Id("robot-page"), Class("min-h-screen bg-base-100"),
			NavBar(),
			Main(Id("robot-main"), Class("mx-auto flex w-full max-w-5xl flex-col gap-6 p-4 md:p-6"),
				Div(Id("robot-header"), Class("flex items-center justify-between gap-4"),
					H1(Class("text-2xl font-bold"), T("Robot Control")),
					Span(Id("robot-status"), Class("text-warning"), T("Connecting…")),
				),
				Div(Id("robot-video"), Class("relative grid min-h-60 place-items-center overflow-hidden rounded-box border border-base-300 bg-base-200"),
					Img(Src("/robot/stream"), Alt("Robot camera stream"), Class("aspect-4/3 w-full max-w-3xl object-contain")),
				),
				Div(
					Id("mobile-robot-controls"),
					joystick("move-joystick", "Move", "w", "a", "s", "d"),
					Div(Class("robot-claw-controls flex items-center gap-1"),
						mobileButton("r", "Open", "Open claw"),
						joystick("claw-joystick", "Claw", "t", "f", "g", "h"),
						mobileButton("y", "Close", "Close claw"),
					),
					joystick("camera-joystick", "Camera", "i", "j", "k", "l"),
				),
				Div(Id("mobile-control-space"), AriaHidden("true")),
				Div(Id("desktop-robot-controls"), Class("grid grid-cols-1 gap-8 md:grid-cols-2 lg:grid-cols-4"),
					pad("Move", "w", "a", "s", "d"),
					pad("Claw", "t", "f", "g", "h"),
					Section(Class("space-y-3 text-center"),
						H2(Class("font-semibold"), T("Open / Close")),
						Div(Class("grid grid-cols-3 gap-2 justify-items-center"), empty(), button("r", "R"), empty(), empty(), button("y", "Y"), empty()),
					),
					pad("Camera", "i", "j", "k", "l"),
				),
			),
		),
		Style(Raw(robotControlCSS)),
		Script(Src("/robot/app.js")),
	)
}

const robotControlCSS = `
#mobile-robot-controls,
#mobile-control-space {
  display: none;
}

@media (pointer: coarse) {
  #desktop-robot-controls {
    display: none !important;
  }

  #mobile-robot-controls {
    --robot-control-size: clamp(3.5rem, 20vw, 5rem);
    --robot-button-size: clamp(2.25rem, 11vw, 3rem);
    align-items: flex-start;
    bottom: 0;
    box-sizing: border-box;
    display: flex;
    gap: 0.5rem;
    justify-content: space-between;
    left: 0;
    padding: 0.5rem max(0.5rem, env(safe-area-inset-right)) max(0.5rem, env(safe-area-inset-bottom)) max(0.5rem, env(safe-area-inset-left));
    position: fixed;
    right: 0;
    z-index: 30;
  }

  #mobile-robot-controls .robot-joystick {
    height: var(--robot-control-size) !important;
    width: var(--robot-control-size) !important;
  }

  #mobile-robot-controls [data-joystick-stick] {
    height: 40%;
    width: 40%;
  }

  #mobile-robot-controls .robot-claw-button {
    height: var(--robot-button-size) !important;
    width: var(--robot-button-size) !important;
  }

  #mobile-robot-controls .robot-claw-controls {
    flex-shrink: 0;
  }
}

@media (pointer: coarse) and (orientation: portrait) {
  #robot-page {
    display: flex;
    flex-direction: column;
    height: 100dvh;
    min-height: 100dvh;
    overflow: hidden;
  }

  #robot-main {
    display: grid;
    flex: 1;
    gap: 1rem;
    grid-template-rows: auto minmax(0, 1fr) auto;
    min-height: 0;
  }

  #robot-video {
    min-height: 0;
  }

  #robot-video img {
    height: 100%;
    max-width: none;
    object-fit: contain;
    width: 100%;
  }

  #mobile-control-space {
    display: block;
    height: calc(clamp(3.5rem, 20vw, 5rem) + 0.5rem + max(0.5rem, env(safe-area-inset-bottom)));
  }
}

@media (pointer: coarse) and (orientation: landscape) {
  #robot-page {
    height: 100dvh;
    min-height: 100dvh;
    overflow: hidden;
  }

  #robot-page > nav,
  #robot-header,
  #mobile-control-space {
    display: none !important;
  }

  #robot-main {
    inset: 0;
    max-width: none;
    padding: 0 !important;
    position: fixed;
  }

  #robot-video {
    border: 0;
    border-radius: 0;
    inset: 0;
    min-height: 0;
    position: absolute;
  }

  #robot-video img {
    height: 100%;
    max-width: none;
    object-fit: contain;
    width: 100%;
  }
}
`

func serveRobotControllerQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "robotcontroller.png", time.Time{}, bytes.NewReader(robotControllerQR))
}

func serveRobotApp(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.WriteString(w, robotAppJS)
}

func (r *robotRelay) handleRobot(w http.ResponseWriter, req *http.Request) {
	if !validRobotToken(req) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := robotUpgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(maxRobotFrameSize)

	r.mu.Lock()
	previous := r.robot
	r.robot = conn
	r.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	r.sendStatus()
	log.Printf("robot connected from %s", req.RemoteAddr)

	defer func() {
		_ = conn.Close()
		r.mu.Lock()
		if r.robot == conn {
			r.robot = nil
			r.frame = nil
			r.seq++
		}
		r.mu.Unlock()
		r.sendStatus()
		log.Printf("robot disconnected from %s", req.RemoteAddr)
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage || len(data) == 0 {
			continue
		}
		r.mu.Lock()
		r.frame = append(r.frame[:0], data...)
		r.seq++
		r.mu.Unlock()
	}
}

func (r *robotRelay) handleController(w http.ResponseWriter, req *http.Request) {
	conn, err := robotUpgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(1024)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))

	r.mu.Lock()
	previous := r.controller
	r.controller = conn
	r.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	r.sendStatus()

	defer func() {
		_ = conn.Close()
		wasActive := false
		r.mu.Lock()
		if r.controller == conn {
			r.controller = nil
			wasActive = true
		}
		r.mu.Unlock()
		if wasActive {
			r.sendToRobot([]byte(`{"type":"stop"}`))
		}
	}()

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.TextMessage {
			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			r.sendToRobot(data)
		}
	}
}

func (r *robotRelay) sendToRobot(data []byte) {
	r.robotWriteMu.Lock()
	defer r.robotWriteMu.Unlock()
	r.mu.RLock()
	conn := r.robot
	r.mu.RUnlock()
	if conn != nil {
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}
}

func (r *robotRelay) connected() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.robot != nil
}

func (r *robotRelay) sendStatus() {
	r.controlWriteMu.Lock()
	defer r.controlWriteMu.Unlock()
	r.mu.RLock()
	controller := r.controller
	connected := r.robot != nil
	r.mu.RUnlock()
	if controller != nil {
		_ = controller.WriteJSON(map[string]any{"type": "status", "connected": connected})
	}
}

func (r *robotRelay) serveStream(w http.ResponseWriter, req *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ticker := time.NewTicker(time.Second / 30)
	defer ticker.Stop()
	var sent uint64
	for {
		select {
		case <-req.Context().Done():
			return
		case <-ticker.C:
			r.mu.RLock()
			seq := r.seq
			frame := append([]byte(nil), r.frame...)
			r.mu.RUnlock()
			if len(frame) == 0 || seq == sent {
				continue
			}
			if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(frame)); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := io.WriteString(w, "\r\n"); err != nil {
				return
			}
			flusher.Flush()
			sent = seq
		}
	}
}

func validRobotToken(r *http.Request) bool {
	want := strings.TrimSpace(os.Getenv("ROBOT_TOKEN"))
	if want == "" {
		tokenFile := os.Getenv("ROBOT_TOKEN_FILE")
		if tokenFile == "" {
			tokenFile = "robot-token"
		}
		if contents, err := os.ReadFile(tokenFile); err == nil {
			want = strings.TrimSpace(string(contents))
		}
	}
	if want == "" {
		return os.Getenv("ENVIRONMENT") != "production"
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

const robotAppJS = `
const controlKeys = new Set(['w','a','s','d','t','f','g','h','i','j','k','l','r','y']);
const pressed = new Set();
const joystickResets = [];
const statusEl = document.querySelector('#robot-status');
const viewportMeta = document.querySelector('meta[name="viewport"]');
if (viewportMeta && !viewportMeta.content.includes('viewport-fit')) viewportMeta.content += ', viewport-fit=cover';
let socket;
let reconnectTimer;

function connect() {
  clearTimeout(reconnectTimer);
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  socket = new WebSocket(protocol + '//' + location.host + '/ws/robot-control');
  socket.addEventListener('message', event => {
    const message = JSON.parse(event.data);
    if (message.type !== 'status') return;
    statusEl.textContent = message.connected ? 'Robot connected' : 'Robot offline';
    statusEl.classList.toggle('text-success', message.connected);
    statusEl.classList.toggle('text-warning', !message.connected);
  });
  socket.addEventListener('close', () => {
    releaseAll();
    statusEl.textContent = 'Disconnected — retrying…';
    statusEl.classList.remove('text-success');
    statusEl.classList.add('text-warning');
    reconnectTimer = setTimeout(connect, 1000);
  });
}

function send(message) {
  if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(message));
}
function setKey(key, isPressed) {
  if (!controlKeys.has(key) || (isPressed && pressed.has(key)) || (!isPressed && !pressed.has(key))) return;
  isPressed ? pressed.add(key) : pressed.delete(key);
  document.querySelectorAll('[data-key="' + key + '"]').forEach(button => button.classList.toggle('btn-active', isPressed));
  send({key, action: isPressed ? 'pressed' : 'released'});
}
function releaseAll() {
  [...pressed].forEach(key => setKey(key, false));
  joystickResets.forEach(reset => reset());
}

document.querySelectorAll('[data-joystick]').forEach(joystick => {
  const stick = joystick.querySelector('[data-joystick-stick]');
  const keys = {
    up: joystick.dataset.up,
    left: joystick.dataset.left,
    down: joystick.dataset.down,
    right: joystick.dataset.right,
  };
  const activeKeys = new Set();

  function render(x, y) {
    const travel = (joystick.clientWidth - stick.clientWidth) / 2 - 4;
    stick.style.transform = 'translate(calc(-50% + ' + (x * travel) + 'px), calc(-50% + ' + (y * travel) + 'px))';
  }
  function updateDirections(x, y) {
    const next = new Set();
    const deadZone = 0.35;
    if (y < -deadZone) next.add(keys.up);
    if (y > deadZone) next.add(keys.down);
    if (x < -deadZone) next.add(keys.left);
    if (x > deadZone) next.add(keys.right);
    activeKeys.forEach(key => { if (!next.has(key)) setKey(key, false); });
    next.forEach(key => { if (!activeKeys.has(key)) setKey(key, true); });
    activeKeys.clear();
    next.forEach(key => activeKeys.add(key));
  }
  function move(event) {
    const bounds = joystick.getBoundingClientRect();
    let x = (event.clientX - (bounds.left + bounds.width / 2)) / (bounds.width * 0.34);
    let y = (event.clientY - (bounds.top + bounds.height / 2)) / (bounds.height * 0.34);
    const magnitude = Math.hypot(x, y);
    if (magnitude > 1) { x /= magnitude; y /= magnitude; }
    render(x, y);
    updateDirections(x, y);
  }
  function reset() {
    activeKeys.forEach(key => setKey(key, false));
    activeKeys.clear();
    render(0, 0);
  }

  joystick.addEventListener('pointerdown', event => {
    event.preventDefault();
    joystick.setPointerCapture(event.pointerId);
    move(event);
  });
  joystick.addEventListener('pointermove', event => {
    if (joystick.hasPointerCapture(event.pointerId)) move(event);
  });
  for (const name of ['pointerup','pointercancel','lostpointercapture']) joystick.addEventListener(name, reset);
  joystickResets.push(reset);
});

window.addEventListener('keydown', event => { const key = event.key.toLowerCase(); if (controlKeys.has(key)) { event.preventDefault(); setKey(key, true); } });
window.addEventListener('keyup', event => { const key = event.key.toLowerCase(); if (controlKeys.has(key)) { event.preventDefault(); setKey(key, false); } });
window.addEventListener('blur', releaseAll);
window.addEventListener('orientationchange', releaseAll);
window.matchMedia('(orientation: portrait)').addEventListener?.('change', releaseAll);
document.addEventListener('visibilitychange', () => { if (document.hidden) releaseAll(); });
document.querySelectorAll('[data-key]').forEach(button => {
  const key = button.dataset.key;
  button.addEventListener('pointerdown', event => { event.preventDefault(); button.setPointerCapture(event.pointerId); setKey(key, true); });
  for (const name of ['pointerup','pointercancel','lostpointercapture']) button.addEventListener(name, () => setKey(key, false));
});
setInterval(() => send({type: 'heartbeat'}), 250);
connect();
`
