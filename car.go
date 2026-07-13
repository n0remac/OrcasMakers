package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/n0remac/GoDom/auth"
	. "github.com/n0remac/GoDom/html"
)

const (
	maxCarBodyBytes      = 64 << 10
	carOnlineTimeout     = 2 * time.Second
	carControllerTimeout = 500 * time.Millisecond
	carMinCommandGap     = 25 * time.Millisecond
	carMinArmGap         = 500 * time.Millisecond
)

// CarTelemetry is the version 1 wire contract sent by CarDashboard. All speeds
// are km/h, temperatures are Celsius, pressure is hPa, altitude is metres,
// angles are degrees, acceleration is m/s², and gyro values are degrees/second.
type CarTelemetry struct {
	ProtocolVersion int       `json:"protocol_version"`
	Speed           float64   `json:"speed"`
	RPM             int       `json:"rpm"`
	Gear            string    `json:"gear"`
	FuelPercent     float64   `json:"fuel_percent"`
	Headlights      bool      `json:"headlights"`
	TurnSignal      string    `json:"turn_signal"`
	Temperature     float64   `json:"temperature"`
	Humidity        float64   `json:"humidity"`
	Pressure        float64   `json:"pressure"`
	Altitude        float64   `json:"altitude"`
	Pitch           float64   `json:"pitch"`
	Roll            float64   `json:"roll"`
	Acceleration    CarVector `json:"acceleration"`
	Gyro            CarVector `json:"gyro"`
	GPS             CarGPS    `json:"gps"`
	Receiver        string    `json:"receiver"`
}

type CarVector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}
type CarGPS struct {
	Fix        bool    `json:"fix"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	Satellites int     `json:"satellites"`
	Speed      float64 `json:"speed"`
}

type carCommand struct {
	Armed      bool   `json:"armed"`
	SessionID  string `json:"session_id,omitempty"`
	Sequence   uint64 `json:"sequence"`
	Generation uint64 `json:"generation"`
	Steering   int    `json:"steering"`
	Throttle   int    `json:"throttle"`
}

type carRelay struct {
	mu                 sync.RWMutex
	now                func() time.Time
	telemetry          CarTelemetry
	telemetryAt        time.Time
	hasTelemetry       bool
	sessionID          string
	controllerID       string
	controllerEmail    string
	sequence           uint64
	generation         uint64
	steering           int
	throttle           int
	heartbeatAt        time.Time
	lastCommandRequest time.Time
	lastArmRequest     time.Time
}

func newCarRelay() *carRelay { return &carRelay{now: time.Now} }

func Car(mux *http.ServeMux, authApp *auth.AuthApp) {
	relay := newCarRelay()
	mux.HandleFunc("/car", requireCarUser(authApp, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		ServeNode(CarPage())(w, r)
	}))
	mux.HandleFunc("/car/app.js", serveCarApp)
	mux.HandleFunc("/api/car/device/sync", relay.deviceSync)
	mux.HandleFunc("/api/car/state", requireCarUser(authApp, relay.state))
	mux.HandleFunc("/api/car/control/state", requireCarUser(authApp, relay.controlState))
	mux.HandleFunc("/api/car/control/arm", requireCarAdmin(authApp, relay.arm))
	mux.HandleFunc("/api/car/control/command", requireCarAdmin(authApp, relay.command))
	mux.HandleFunc("/api/car/control/stop", requireCarAdmin(authApp, relay.stop))
}

func requireCarUser(a *auth.AuthApp, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a == nil {
			http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
			return
		}
		user, ok := a.CurrentUser(r)
		if !ok {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeCarError(w, http.StatusUnauthorized, "authentication required")
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), carUserContextKey{}, user)))
	}
}

func requireCarAdmin(a *auth.AuthApp, next http.HandlerFunc) http.HandlerFunc {
	return requireCarUser(a, func(w http.ResponseWriter, r *http.Request) {
		if !a.IsAdmin(r) {
			writeCarError(w, http.StatusForbidden, "administrator access required")
			return
		}
		if r.Method == http.MethodPost && !sameOrigin(r) {
			writeCarError(w, http.StatusForbidden, "cross-site request rejected")
			return
		}
		next(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host) && (u.Scheme == "http" || u.Scheme == "https")
}

func (c *carRelay) deviceSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !validCarToken(r) {
		writeCarError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var telemetry CarTelemetry
	if err := decodeCarJSON(w, r, &telemetry); err != nil {
		writeCarError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateTelemetry(telemetry); err != nil {
		writeCarError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	now := c.now()
	c.mu.Lock()
	wasOffline := !c.hasTelemetry || now.Sub(c.telemetryAt) > carOnlineTimeout
	c.telemetry, c.telemetryAt, c.hasTelemetry = telemetry, now, true
	command := c.currentCommandLocked(now)
	c.mu.Unlock()
	if wasOffline {
		log.Printf("car device connected from %s", r.RemoteAddr)
	}
	writeCarJSON(w, http.StatusOK, command)
}

func (c *carRelay) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	now := c.now()
	c.mu.Lock()
	command := c.currentCommandLocked(now)
	online := c.onlineLocked(now)
	age := int64(-1)
	var telemetry any
	if c.hasTelemetry {
		age = now.Sub(c.telemetryAt).Milliseconds()
		telemetry = c.telemetry
	}
	owner := c.controllerEmail
	c.mu.Unlock()
	writeCarJSON(w, http.StatusOK, map[string]any{"online": online, "telemetry_age_ms": age, "controller_active": command.Armed, "controller": owner, "telemetry": telemetry})
}

func (c *carRelay) controlState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	c.mu.Lock()
	command := c.currentCommandLocked(c.now())
	c.mu.Unlock()
	writeCarJSON(w, http.StatusOK, command)
}

func (c *carRelay) arm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	user, _ := carUser(r)
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if now.Sub(c.lastArmRequest) < carMinArmGap {
		writeCarError(w, http.StatusTooManyRequests, "arm rate limit exceeded")
		return
	}
	c.lastArmRequest = now
	c.currentCommandLocked(now)
	if !c.onlineLocked(now) {
		writeCarError(w, http.StatusConflict, "car is offline")
		return
	}
	if c.sessionID != "" {
		writeCarError(w, http.StatusConflict, "another controller is active")
		return
	}
	sessionID, err := randomCarID()
	if err != nil {
		writeCarError(w, http.StatusInternalServerError, "could not create control session")
		return
	}
	c.generation++
	c.sessionID, c.controllerID, c.controllerEmail = sessionID, user.ID, user.Email
	c.sequence, c.steering, c.throttle, c.heartbeatAt = 0, 0, 0, now
	log.Printf("car control armed by %s generation=%d", user.Email, c.generation)
	writeCarJSON(w, http.StatusOK, c.commandLocked(true))
}

func (c *carRelay) command(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request struct {
		SessionID string `json:"session_id"`
		Sequence  uint64 `json:"sequence"`
		Steering  int    `json:"steering"`
		Throttle  int    `json:"throttle"`
	}
	if err := decodeCarJSON(w, r, &request); err != nil {
		writeCarError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.SessionID == "" || request.Sequence == 0 || request.Steering < -100 || request.Steering > 100 || request.Throttle < -100 || request.Throttle > 100 {
		writeCarError(w, http.StatusUnprocessableEntity, "invalid session, sequence, steering, or throttle")
		return
	}
	user, _ := carUser(r)
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.currentCommandLocked(now)
	if !c.lastCommandRequest.IsZero() && now.Sub(c.lastCommandRequest) < carMinCommandGap {
		writeCarError(w, http.StatusTooManyRequests, "command rate limit exceeded")
		return
	}
	c.lastCommandRequest = now
	if c.sessionID == "" || request.SessionID != c.sessionID || user.ID != c.controllerID {
		writeCarError(w, http.StatusConflict, "control session is not active")
		return
	}
	if !c.onlineLocked(now) {
		c.disarmLocked("device offline")
		writeCarError(w, http.StatusConflict, "car is offline")
		return
	}
	if request.Sequence <= c.sequence {
		writeCarError(w, http.StatusConflict, "sequence must strictly increase")
		return
	}
	c.sequence, c.steering, c.throttle, c.heartbeatAt = request.Sequence, request.Steering, request.Throttle, now
	writeCarJSON(w, http.StatusOK, c.commandLocked(true))
}

func (c *carRelay) stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var request struct {
		SessionID string `json:"session_id"`
	}
	// sendBeacon may use text/plain; JSON is still required. An empty body is an
	// idempotent no-op so a stale navigation beacon cannot stop a newer owner.
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeCarJSON(w, r, &request); err != nil {
			writeCarError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	user, _ := carUser(r)
	c.mu.Lock()
	if c.sessionID != "" && request.SessionID == c.sessionID && user.ID == c.controllerID {
		c.disarmLocked("stop requested")
	}
	command := c.commandLocked(c.sessionID != "")
	c.mu.Unlock()
	writeCarJSON(w, http.StatusOK, command)
}

func (c *carRelay) onlineLocked(now time.Time) bool {
	return c.hasTelemetry && now.Sub(c.telemetryAt) <= carOnlineTimeout
}
func (c *carRelay) currentCommandLocked(now time.Time) carCommand {
	if c.sessionID != "" && (!c.onlineLocked(now) || now.Sub(c.heartbeatAt) > carControllerTimeout) {
		c.disarmLocked("controller or device timeout")
	}
	return c.commandLocked(c.sessionID != "")
}
func (c *carRelay) commandLocked(armed bool) carCommand {
	return carCommand{Armed: armed, SessionID: c.sessionID, Sequence: c.sequence, Generation: c.generation, Steering: c.steering, Throttle: c.throttle}
}
func (c *carRelay) disarmLocked(reason string) {
	if c.sessionID == "" {
		return
	}
	log.Printf("car control stopped: %s generation=%d", reason, c.generation)
	c.generation++
	c.sessionID, c.controllerID, c.controllerEmail = "", "", ""
	c.sequence, c.steering, c.throttle = 0, 0, 0
	c.heartbeatAt = time.Time{}
}

func carUser(r *http.Request) (*auth.User, bool) {
	value := r.Context().Value(carUserContextKey{})
	user, ok := value.(*auth.User)
	return user, ok
}

type carUserContextKey struct{}

func validCarToken(r *http.Request) bool {
	want := strings.TrimSpace(os.Getenv("CAR_DEVICE_TOKEN"))
	if want == "" {
		file := os.Getenv("CAR_DEVICE_TOKEN_FILE")
		if file == "" {
			file = "car-device-token"
		}
		if contents, err := os.ReadFile(file); err == nil {
			want = strings.TrimSpace(string(contents))
		}
	}
	if want == "" {
		return os.Getenv("ENVIRONMENT") != "production"
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func validateTelemetry(t CarTelemetry) error {
	if t.ProtocolVersion != 1 {
		return errors.New("unsupported protocol_version")
	}
	numbers := []float64{t.Speed, t.FuelPercent, t.Temperature, t.Humidity, t.Pressure, t.Altitude, t.Pitch, t.Roll, t.Acceleration.X, t.Acceleration.Y, t.Acceleration.Z, t.Gyro.X, t.Gyro.Y, t.Gyro.Z, t.GPS.Latitude, t.GPS.Longitude, t.GPS.Speed}
	for _, n := range numbers {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return errors.New("telemetry values must be finite")
		}
	}
	if t.RPM < 0 || t.RPM > 100000 || t.FuelPercent < 0 || t.FuelPercent > 100 || t.Humidity < 0 || t.Humidity > 100 || t.GPS.Latitude < -90 || t.GPS.Latitude > 90 || t.GPS.Longitude < -180 || t.GPS.Longitude > 180 || t.GPS.Satellites < 0 {
		return errors.New("telemetry value outside allowed range")
	}
	if t.TurnSignal != "" && t.TurnSignal != "off" && t.TurnSignal != "left" && t.TurnSignal != "right" && t.TurnSignal != "hazard" {
		return errors.New("invalid turn_signal")
	}
	return nil
}

func decodeCarJSON(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxCarBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("invalid JSON body")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
func writeCarJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeCarError(w http.ResponseWriter, status int, message string) {
	writeCarJSON(w, status, map[string]string{"error": message})
}
func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST")
	writeCarError(w, http.StatusMethodNotAllowed, "method not allowed")
}
func randomCarID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func CarPage() *Node {
	metric := func(label, id string) *Node {
		return Div(Class("rounded-box bg-base-200 p-3"), Div(Class("text-xs uppercase text-base-content/60"), T(label)), Div(Id(id), Class("text-lg font-semibold"), T("—")))
	}
	return DefaultLayout(
		Div(Class("min-h-screen bg-base-100"), NavBar(),
			Main(Class("mx-auto w-full max-w-6xl space-y-6 p-4 md:p-6"),
				Div(Class("flex flex-wrap items-center justify-between gap-3"), H1(Class("text-2xl font-bold"), T("Car Dashboard")), Span(Id("car-status"), Class("badge badge-warning badge-lg"), T("Connecting…"))),
				Div(Class("grid grid-cols-2 gap-3 md:grid-cols-4"), metric("Speed km/h", "speed"), metric("RPM", "rpm"), metric("Gear", "gear"), metric("Fuel %", "fuel"), metric("Temperature °C", "temperature"), metric("Humidity %", "humidity"), metric("Pressure hPa", "pressure"), metric("Altitude m", "altitude"), metric("Pitch °", "pitch"), metric("Roll °", "roll"), metric("GPS", "gps"), metric("Receiver", "receiver")),
				Div(Class("grid gap-4 lg:grid-cols-2"),
					Section(Class("rounded-box bg-base-200 p-4 space-y-3"), H2(Class("text-lg font-bold"), T("Sensors")), Pre(Id("sensor-details"), Class("whitespace-pre-wrap text-sm"), T("Waiting for telemetry…"))),
					Section(Class("rounded-box bg-base-200 p-4 space-y-4"),
						Div(Class("flex items-center justify-between"), H2(Class("text-lg font-bold"), T("Control")), Span(Id("control-status"), Class("badge"), T("Disarmed"))),
						Div(Id("car-joystick"), Class("relative mx-auto h-64 w-64 touch-none rounded-full border-4 border-base-300 bg-base-100"), Div(Id("car-stick"), Class("absolute left-1/2 top-1/2 h-16 w-16 -translate-x-1/2 -translate-y-1/2 rounded-full bg-primary shadow-lg"))),
						Div(Class("grid grid-cols-2 gap-3"), metric("Steering", "steering"), metric("Throttle", "throttle")),
						Div(Class("flex gap-3"), Button(Id("arm-car"), Class("btn btn-success flex-1"), T("Arm")), Button(Id("stop-car"), Class("btn btn-error flex-1"), T("STOP"))),
					),
				),
			),
		), Script(Src("/car/app.js")),
	)
}

func serveCarApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.WriteString(w, carAppJS)
}

const carAppJS = `
const $ = id => document.getElementById(id);
let session = null, sequence = 0, steering = 0, throttle = 0, timer = null, inFlight = false, queued = false;
const fields = ['speed','rpm','gear','fuel','temperature','humidity','pressure','altitude','pitch','roll','receiver'];
async function api(path, options={}) { const response = await fetch(path, {cache:'no-store', ...options, headers:{'Content-Type':'application/json', ...(options.headers||{})}}); const body = await response.json(); if (!response.ok) throw new Error(body.error || 'request failed'); return body; }
function setArmed(value) { session = value ? session : null; $('control-status').textContent = value ? 'Armed' : 'Disarmed'; $('control-status').className = 'badge ' + (value ? 'badge-error' : 'badge-ghost'); $('arm-car').disabled = value; if (!value) { clearInterval(timer); timer=null; neutral(); } }
function renderState(state) { const t=state.telemetry; $('car-status').textContent=state.online ? 'Online · '+state.telemetry_age_ms+' ms' : 'Offline'; $('car-status').className='badge badge-lg '+(state.online?'badge-success':'badge-warning'); $('arm-car').disabled=!state.online || !!session; if(!t)return; fields.forEach(k=>{let v=t[k]; if(k==='fuel')v=t.fuel_percent; $(k).textContent=v ?? '—'}); $('gps').textContent=t.gps?.fix ? t.gps.latitude.toFixed(6)+', '+t.gps.longitude.toFixed(6)+' · '+t.gps.satellites+' sat' : 'No fix'; $('sensor-details').textContent='Lights: '+(t.headlights?'on':'off')+' · signal: '+(t.turn_signal||'off')+'\nAcceleration: '+vector(t.acceleration)+' m/s²\nGyro: '+vector(t.gyro)+' °/s\nGPS speed: '+(t.gps?.speed??'—')+' km/h\nController: '+(state.controller||'none'); if(session && !state.controller_active)setArmed(false); }
function vector(v){return v ? [v.x,v.y,v.z].map(n=>Number(n).toFixed(2)).join(', ') : '—'}
async function poll(){try{renderState(await api('/api/car/state'))}catch(e){$('car-status').textContent='Disconnected'; if(session)setArmed(false)}}
async function arm(){if(!confirm('Arm remote vehicle control? Keep the car raised or drive hardware disconnected for initial testing.'))return; try{const r=await api('/api/car/control/arm',{method:'POST',body:'{}'});session=r.session_id;sequence=r.sequence;setArmed(true);timer=setInterval(sendCommand,100);await sendCommand()}catch(e){alert(e.message);setArmed(false)}}
async function sendCommand(){if(!session)return;if(inFlight){queued=true;return}inFlight=true;try{const r=await api('/api/car/control/command',{method:'POST',body:JSON.stringify({session_id:session,sequence:++sequence,steering,throttle})});if(!r.armed)throw new Error('control session ended')}catch(e){setArmed(false)}finally{inFlight=false;if(queued){queued=false;sendCommand()}}}
async function stop(){const old=session;setArmed(false);if(!old)return;try{await api('/api/car/control/stop',{method:'POST',body:JSON.stringify({session_id:old})})}catch(e){}}
function emergencyStop(){const old=session;if(!old)return;setArmed(false);navigator.sendBeacon('/api/car/control/stop',new Blob([JSON.stringify({session_id:old})],{type:'text/plain'}))}
function neutral(){steering=throttle=0;moveStick(0,0);$('steering').textContent='0';$('throttle').textContent='0'}
function moveStick(x,y){$('car-stick').style.transform='translate(calc(-50% + '+x*88+'px), calc(-50% + '+y*88+'px))'}
function point(e){const r=$('car-joystick').getBoundingClientRect();let x=(e.clientX-(r.left+r.width/2))/(r.width*.35),y=(e.clientY-(r.top+r.height/2))/(r.height*.35);const m=Math.hypot(x,y);if(m>1){x/=m;y/=m}steering=Math.round(x*100);throttle=Math.round(-y*100);$('steering').textContent=steering;$('throttle').textContent=throttle;moveStick(x,y);sendCommand()}
const joy=$('car-joystick');joy.addEventListener('pointerdown',e=>{if(!session)return;joy.setPointerCapture(e.pointerId);point(e)});joy.addEventListener('pointermove',e=>{if(session&&joy.hasPointerCapture(e.pointerId))point(e)});for(const n of ['pointerup','pointercancel','lostpointercapture'])joy.addEventListener(n,()=>{if(session){neutral();sendCommand()}});
$('arm-car').addEventListener('click',arm);$('stop-car').addEventListener('click',stop);document.addEventListener('visibilitychange',()=>{if(document.hidden)emergencyStop()});window.addEventListener('pagehide',emergencyStop);window.addEventListener('blur',()=>{if(session){neutral();sendCommand()}});neutral();poll();setInterval(poll,500);
`
