package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "github.com/n0remac/GoDom/html"
)

const openSauceMediaDir = "open-sauce-media"

var openSauceImageExtensions = map[string]bool{
	".avif": true,
	".gif":  true,
	".jpeg": true,
	".jpg":  true,
	".png":  true,
	".webp": true,
}

var openSauceVideoExtensions = map[string]bool{
	".mov":  true,
	".mp4":  true,
	".webm": true,
}

type openSauceProject struct {
	id        string
	directory string
	name      string
	summary   string
	link      string
}

var openSauceProjects = []openSauceProject{
	{id: "truck", directory: "RCTruck", name: "RC Truck Dashboard", summary: "An RC truck with a custom ESP32 dashboard, browser controls, live sensor feedback, lighting, and audio."},
	{id: "robot", directory: "RedBot", name: "3D-Printed Robot", summary: "A compact tracked robot with a 3D-printed chassis, movable camera, and articulated grabbing claw."},
	{id: "solar", directory: "SolarHat", name: "Solar Hat", summary: "A solar wearable with phone charging, Bluetooth audio, lighting, and a neck fan.", link: "https://solarhat.pro/"},
	{id: "cooler", directory: "CoolerMobile", name: "CoolerMobile", summary: "A rideable full-size cooler mounted to a modified electric scooter with custom controls and fabrication."},
	{id: "trailer", directory: "BikeTrailer", name: "Bike Trailer", summary: "A custom-welded electric bike trailer with drive assistance, a cargo platform, and a purpose-built bicycle connection."},
}

func OpenSauce(mux *http.ServeMux) {
	mediaDir := resolveOpenSauceMediaDir()
	log.Printf("Open Sauce media directory: %s", mediaDir)
	mountOpenSauce(mux, mediaDir)
}

func resolveOpenSauceMediaDir() string {
	executable, _ := os.Executable()
	return resolveOpenSauceMediaDirFrom(os.Getenv("OPEN_SAUCE_MEDIA_DIR"), executable)
}

func resolveOpenSauceMediaDirFrom(configured, executable string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	if executable != "" {
		besideExecutable := filepath.Join(filepath.Dir(executable), openSauceMediaDir)
		if info, statErr := os.Stat(besideExecutable); statErr == nil && info.IsDir() {
			return besideExecutable
		}
	}
	return openSauceMediaDir
}

func mountOpenSauce(mux *http.ServeMux, mediaDir string) {
	mediaServer := http.StripPrefix("/open-sauce/media/", http.FileServer(http.Dir(mediaDir)))
	mux.Handle("/open-sauce/media/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		mediaServer.ServeHTTP(w, r)
	}))

	mux.HandleFunc("/open-sauce", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		media, err := listOpenSauceMedia(mediaDir)
		if err != nil {
			http.Error(w, "could not load Open Sauce media", http.StatusInternalServerError)
			return
		}

		ServeNode(OpenSaucePage(media))(w, r)
	})

	mux.HandleFunc("/open-sauce/robot-controller", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ServeNode(openSauceRobotControllerCard(robots.connected()))(w, r)
	})
}

func listOpenSauceMedia(root string) (map[string][]string, error) {
	media := make(map[string][]string, len(openSauceProjects))
	for _, project := range openSauceProjects {
		entries, err := os.ReadDir(filepath.Join(root, project.directory))
		if err != nil {
			if os.IsNotExist(err) {
				media[project.id] = []string{}
				continue
			}
			return nil, fmt.Errorf("read %s media: %w", project.name, err)
		}

		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			extension := strings.ToLower(filepath.Ext(entry.Name()))
			if !entry.IsDir() && (openSauceImageExtensions[extension] || openSauceVideoExtensions[extension]) {
				names = append(names, entry.Name())
			}
		}
		sort.Slice(names, func(i, j int) bool {
			return strings.ToLower(names[i]) < strings.ToLower(names[j])
		})

		items := make([]string, 0, len(names))
		for _, name := range names {
			items = append(items, "/open-sauce/media/"+url.PathEscape(project.directory)+"/"+url.PathEscape(name))
		}
		media[project.id] = items
	}
	return media, nil
}

func OpenSaucePage(media map[string][]string) *Node {
	cards := make([]*Node, 0, len(openSauceProjects))
	for index, project := range openSauceProjects {
		cards = append(cards, openSauceProjectCard(project, media[project.id], index))
	}

	return Html(
		Attr("lang", "en"),
		Head(
			Meta(Charset("utf-8")),
			Meta(Attrs(map[string]string{
				"name":    "viewport",
				"content": "width=device-width, initial-scale=1",
			})),
			Meta(Attrs(map[string]string{
				"name":    "description",
				"content": "Alex and Cameron's Open Sauce project board.",
			})),
			Title(T("Alex + Cameron — Open Sauce")),
			DaisyUI,
			Script(Src("https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4")),
			HTMX,
			Style(Raw(openSauceCSS)),
		),
		Body(
			Class("open-sauce-shell"),
			Attr("data-theme", "dark"),
			NavBar(),
			Main(
				Class("open-sauce-board"),
				Section(
					Class("open-sauce-projects"),
					AriaLabel("Open Sauce projects"),
					Ch(cards),
				),
			),
			openSauceRobotControllerCard(false),
			Script(Raw(openSauceCarouselJS)),
		),
	)
}

func openSauceRobotControllerCard(online bool) *Node {
	polling := []*Node{
		Id("open-sauce-robot-controller"),
		Attr("hx-get", "/open-sauce/robot-controller"),
		Attr("hx-trigger", "every 2s"),
		Attr("hx-swap", "outerHTML"),
	}
	if !online {
		return Div(append(polling, Class("hidden"))...)
	}
	return Aside(
		append(polling,
			Class("open-sauce-robot-controller"),
			AriaLabel("Robot controller QR code"),
			H2(T("Control the robot")),
			Img(
				Src("/robot/controller-qr.png"),
				Alt("QR code linking to the robot control page"),
				Attr("width", "200"),
				Attr("height", "200"),
			),
		)...,
	)
}

func openSauceProjectCard(project openSauceProject, items []string, position int) *Node {
	summary := P(Class("open-sauce-project-summary"), T(project.summary))
	if project.link != "" {
		summary = P(
			Class("open-sauce-project-summary"),
			T(project.summary+" "),
			A(
				Href(project.link),
				Target("_blank"),
				Rel("noopener noreferrer"),
				T("solarhat.pro ↗"),
			),
		)
	}

	return Article(
		Class(fmt.Sprintf("open-sauce-project %s panel-position-%d", project.id, position+1)),
		Div(
			Class("open-sauce-project-header"),
			H2(T(project.name)),
			summary,
		),
		openSauceSlideshow(project, items, position),
	)
}

func openSauceSlideshow(project openSauceProject, items []string, carouselIndex int) *Node {
	children := make([]*Node, 0, len(items)+1)
	if len(items) == 0 {
		children = append(children, Div(Class("open-sauce-slide-empty"), T("No media found for this project.")))
	} else {
		for index, source := range items {
			children = append(children, openSauceMediaNode(project.name, source, index))
		}
		children = append(children,
			Div(
				Class("open-sauce-slide-controls"),
				Button(
					Class("open-sauce-slide-arrow previous"),
					Type("button"),
					AriaLabel("Previous "+project.name+" item"),
					T("←"),
				),
				Span(
					Class("open-sauce-slide-counter"),
					Attr("aria-live", "polite"),
					T(fmt.Sprintf("1 / %d", len(items))),
				),
				Button(
					Class("open-sauce-slide-arrow next"),
					Type("button"),
					AriaLabel("Next "+project.name+" item"),
					T("→"),
				),
			),
		)
	}

	return Div(
		Class("open-sauce-slideshow"),
		Attr("data-carousel", fmt.Sprintf("%d", carouselIndex)),
		Attr("tabindex", "0"),
		AriaLabel(project.name+" media slideshow"),
		Ch(children),
	)
}

func openSauceMediaNode(projectName, source string, index int) *Node {
	extension := strings.ToLower(filepath.Ext(source))
	activeClass := "open-sauce-slide"
	if index == 0 {
		activeClass += " active"
	}

	if openSauceVideoExtensions[extension] {
		attributes := []*Node{
			Class(activeClass),
			Src(source),
			AriaLabel(fmt.Sprintf("%s video %d", projectName, index+1)),
			Attr("controls", "controls"),
			Attr("muted", "muted"),
			Attr("loop", "loop"),
			Attr("playsinline", "playsinline"),
			Attr("preload", "metadata"),
		}
		if index == 0 {
			attributes = append(attributes, Attr("autoplay", "autoplay"))
		}
		return Video(attributes...)
	}

	loading := "lazy"
	if index == 0 {
		loading = "eager"
	}
	return Img(
		Class(activeClass),
		Src(source),
		Alt(fmt.Sprintf("%s photo %d", projectName, index+1)),
		Attr("loading", loading),
		Attr("decoding", "async"),
	)
}

const openSauceCSS = `
.open-sauce-shell {
  --os-bg: #071019;
  --os-panel: #0f1824;
  --os-panel-2: #131f2e;
  --os-line: rgba(255, 255, 255, 0.11);
  --os-text: #f5f7fb;
  --os-muted: #a6b2c4;
  --os-truck: #67e5d8;
  --os-robot: #ff8765;
  --os-solar: #ffd44d;
  --os-cooler: #72b7ff;
  --os-trailer: #bb8cff;
  --os-radius: 20px;
  min-width: 980px;
  min-height: 100vh;
  margin: 0;
  overflow: auto;
  background:
    radial-gradient(circle at 12% 5%, rgba(103, 229, 216, 0.12), transparent 27rem),
    radial-gradient(circle at 88% 12%, rgba(255, 212, 77, 0.12), transparent 25rem),
    var(--os-bg);
  color: var(--os-text);
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}

.open-sauce-shell *, .open-sauce-shell *::before, .open-sauce-shell *::after { box-sizing: border-box; }
.open-sauce-shell button, .open-sauce-shell a { font: inherit; }
.open-sauce-board { width: 100%; min-height: calc(100vh - 4.75rem); padding: clamp(12px, 1.5vw, 22px); }
.open-sauce-projects {
  height: calc(100vh - 4.75rem - clamp(24px, 3vw, 44px));
  min-height: 520px;
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  grid-template-rows: repeat(2, minmax(0, 1fr));
  gap: clamp(10px, 1.1vw, 16px);
}

.open-sauce-project {
  --accent: white;
  position: relative;
  min-width: 0;
  min-height: 0;
  grid-column: span 2;
  overflow: hidden;
  padding: clamp(10px, 1vw, 15px);
  display: flex;
  flex-direction: column;
  border: 1px solid var(--os-line);
  border-radius: var(--os-radius);
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.035), transparent 42%), var(--os-panel);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.035);
}
.open-sauce-project::after {
  content: "";
  position: absolute;
  width: 160px;
  height: 160px;
  right: -68px;
  bottom: -72px;
  border-radius: 50%;
  background: var(--accent);
  opacity: 0.08;
  pointer-events: none;
}
.open-sauce-project.truck { --accent: var(--os-truck); }
.open-sauce-project.robot { --accent: var(--os-robot); }
.open-sauce-project.solar { --accent: var(--os-solar); }
.open-sauce-project.cooler { --accent: var(--os-cooler); }
.open-sauce-project.trailer { --accent: var(--os-trailer); }
.open-sauce-project.panel-position-4 { grid-column: 2 / span 2; }
.open-sauce-project.panel-position-5 { grid-column: 4 / span 2; }

.open-sauce-project-header {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
  align-items: center;
  gap: clamp(10px, 1.2vw, 18px);
  min-height: 3.2rem;
}
.open-sauce-project h2 { margin: 0; font-size: clamp(1.15rem, 1.55vw, 1.95rem); line-height: 1; letter-spacing: -0.045em; }
.open-sauce-project-summary { margin: 0; color: var(--os-muted); font-size: clamp(0.62rem, 0.72vw, 0.78rem); line-height: 1.35; }
.open-sauce-project-summary a { color: var(--accent); font-weight: 800; text-decoration: none; }

.open-sauce-slideshow {
  position: relative;
  height: auto;
  min-height: 150px;
  flex: 1 1 auto;
  margin: clamp(10px, 1.2vh, 14px) 0 0;
  overflow: hidden;
  border: 1px solid var(--os-line);
  border-radius: 14px;
  background: var(--os-panel-2);
}
.open-sauce-slide {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  display: none;
  object-fit: contain;
  object-position: center;
  background: #050b11;
}
.open-sauce-slide.active { display: block; animation: open-sauce-fade 250ms ease; }
.open-sauce-slide-controls {
  position: absolute;
  z-index: 2;
  right: 8px;
  bottom: 8px;
  left: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  pointer-events: none;
}
.open-sauce-slide-arrow, .open-sauce-slide-counter {
  border: 1px solid rgba(255, 255, 255, 0.22);
  background: rgba(7, 16, 25, 0.78);
  color: var(--os-text);
  backdrop-filter: blur(5px);
}
.open-sauce-slide-arrow {
  width: 32px;
  height: 32px;
  padding: 0;
  border-radius: 50%;
  cursor: pointer;
  pointer-events: auto;
}
.open-sauce-slide-arrow:hover, .open-sauce-slide-arrow:focus-visible { background: color-mix(in srgb, var(--accent) 35%, #071019); }
.open-sauce-slide-counter { padding: 4px 8px; border-radius: 999px; font-size: 0.68rem; font-variant-numeric: tabular-nums; }
.open-sauce-slide-empty { position: absolute; inset: 0; display: grid; place-items: center; padding: 18px; color: var(--os-muted); font-size: 0.75rem; text-align: center; }
.open-sauce-robot-controller {
  position: fixed;
  z-index: 10;
  right: 14px;
  bottom: 14px;
  width: 152px;
  padding: 9px;
  border: 1px solid var(--os-line);
  border-radius: 14px;
  background: rgba(15, 24, 36, 0.94);
  box-shadow: 0 12px 35px rgba(0, 0, 0, 0.42);
  backdrop-filter: blur(8px);
}
.open-sauce-robot-controller h2 {
  margin: 0 0 7px;
  color: var(--os-text);
  font-size: 0.76rem;
  font-weight: 850;
  letter-spacing: 0.02em;
  text-align: center;
}
.open-sauce-robot-controller img {
  display: block;
  width: 100%;
  height: auto;
  border-radius: 8px;
  background: white;
}

@keyframes open-sauce-fade {
  from { opacity: 0.35; transform: scale(1.01); }
  to { opacity: 1; transform: scale(1); }
}
@media (max-height: 684px) {
  .open-sauce-projects { height: 520px; }
}
@media (prefers-reduced-motion: reduce) {
  .open-sauce-slide.active { animation: none; }
}
`

const openSauceCarouselJS = `
(() => {
  document.querySelectorAll("[data-carousel]").forEach((stage) => {
    const slides = Array.from(stage.querySelectorAll(".open-sauce-slide"));
    if (!slides.length) return;

    const counter = stage.querySelector(".open-sauce-slide-counter");
    const previous = stage.querySelector(".previous");
    const next = stage.querySelector(".next");
    const carouselIndex = Number(stage.dataset.carousel) || 0;
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    let current = Math.max(0, slides.findIndex((slide) => slide.classList.contains("active")));
    let timer;

    const nextDelay = () => 15000 + Math.floor(Math.random() * 15001);
    const restartTimer = (delay = nextDelay()) => {
      window.clearTimeout(timer);
      if (slides.length > 1 && !reducedMotion.matches) {
        timer = window.setTimeout(() => {
          show(current + 1);
          restartTimer();
        }, delay);
      }
    };
    const show = (index) => {
      const outgoing = slides[current];
      if (outgoing instanceof HTMLVideoElement) outgoing.pause();
      current = (index + slides.length) % slides.length;
      slides.forEach((slide, slideIndex) => slide.classList.toggle("active", slideIndex === current));
      const active = slides[current];
      if (active instanceof HTMLVideoElement) {
        active.currentTime = 0;
        active.play().catch(() => {});
      }
      counter.textContent = (current + 1) + " / " + slides.length;
    };
    const move = (delta) => {
      show(current + delta);
      restartTimer();
    };

    previous.addEventListener("click", () => move(-1));
    next.addEventListener("click", () => move(1));
    stage.addEventListener("keydown", (event) => {
      if (event.key === "ArrowLeft") move(-1);
      if (event.key === "ArrowRight") move(1);
    });
    stage.addEventListener("mouseenter", () => window.clearTimeout(timer));
    stage.addEventListener("mouseleave", () => restartTimer());
    reducedMotion.addEventListener?.("change", () => restartTimer());

    show(current);
    restartTimer(15000 + carouselIndex * 7500);
  });
})();
`
