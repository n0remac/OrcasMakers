package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestListOpenSauceMediaFiltersAndSorts(t *testing.T) {
	root := t.TempDir()
	for _, project := range openSauceProjects {
		if err := os.MkdirAll(filepath.Join(root, project.directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	truck := filepath.Join(root, "RCTruck")
	for name, content := range map[string]string{
		"02-drive.MP4":   "video",
		"01-Chassis.jpg": "image",
		"notes.txt":      "ignore",
	} {
		if err := os.WriteFile(filepath.Join(truck, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(truck, "03-directory.png"), 0o755); err != nil {
		t.Fatal(err)
	}

	media, err := listOpenSauceMedia(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/open-sauce/media/RCTruck/01-Chassis.jpg",
		"/open-sauce/media/RCTruck/02-drive.MP4",
	}
	if !reflect.DeepEqual(media["truck"], want) {
		t.Fatalf("truck media = %#v, want %#v", media["truck"], want)
	}
	if len(media["solar"]) != 0 {
		t.Fatalf("solar media = %#v, want none", media["solar"])
	}
}

func TestResolveOpenSauceMediaDirUsesConfiguredPath(t *testing.T) {
	want := filepath.Join(t.TempDir(), "showcase-media")
	if got := resolveOpenSauceMediaDirFrom("  "+want+"  ", ""); got != want {
		t.Fatalf("media directory = %q, want %q", got, want)
	}
}

func TestResolveOpenSauceMediaDirBesideExecutable(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, openSauceMediaDir)
	if err := os.Mkdir(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveOpenSauceMediaDirFrom("", filepath.Join(root, "orcasmakers")); got != want {
		t.Fatalf("media directory = %q, want %q", got, want)
	}
}

func TestOpenSaucePageContainsProjectsMediaAndControls(t *testing.T) {
	page := OpenSaucePage(map[string][]string{
		"truck": {"/open-sauce/media/RCTruck/truck.jpg", "/open-sauce/media/RCTruck/drive.mp4"},
	}).Render()

	for _, want := range []string{
		"<h1>Orcas Makers</h1>",
		"RC Truck Dashboard",
		"3D-Printed Robot",
		"Solar Hat",
		"CoolerMobile",
		"Bike Trailer",
		`src="/open-sauce/media/RCTruck/truck.jpg"`,
		`src="/open-sauce/media/RCTruck/drive.mp4"`,
		`aria-label="Previous RC Truck Dashboard item"`,
		"https://solarhat.pro/",
		"prefers-reduced-motion: reduce",
		`id="open-sauce-robot-controller"`,
		`hx-get="/open-sauce/robot-controller"`,
		"Visit our makerspace",
		`src="/open-sauce/makerspace-qr.png"`,
		"Open the Center for Creative Repair website",
		"Watch us on YouTube",
		`src="/open-sauce/youtube-qr.png"`,
		"Open the Orcas Makers YouTube page",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("Open Sauce page does not contain %q", want)
		}
	}
	if !strings.Contains(page, "No media found for this project.") {
		t.Error("Open Sauce page does not render empty project media states")
	}
	makerspaceIndex := strings.Index(page, "Visit our makerspace")
	youtubeIndex := strings.Index(page, "Watch us on YouTube")
	if makerspaceIndex < 0 || youtubeIndex < 0 || makerspaceIndex > youtubeIndex {
		t.Error("makerspace link must render above the YouTube link")
	}
}

func TestOpenSauceMakerspaceCard(t *testing.T) {
	card := openSauceMakerspaceCard().Render()
	for _, want := range []string{
		"open-sauce-makerspace",
		"Visit our makerspace",
		`href="` + openSauceMakerspaceURL + `"`,
		`src="/open-sauce/makerspace-qr.png"`,
		"QR code linking to the Center for Creative Repair website",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("makerspace card does not contain %q", want)
		}
	}
}

func TestOpenSauceYouTubeCard(t *testing.T) {
	card := openSauceYouTubeCard().Render()
	for _, want := range []string{
		"open-sauce-youtube",
		"Watch us on YouTube",
		`href="` + openSauceYouTubeURL + `"`,
		`src="/open-sauce/youtube-qr.png"`,
		"QR code linking to the Orcas Makers YouTube page",
	} {
		if !strings.Contains(card, want) {
			t.Errorf("YouTube card does not contain %q", want)
		}
	}
}

func TestOpenSauceRobotControllerCardVisibility(t *testing.T) {
	offline := openSauceRobotControllerCard(false).Render()
	if !strings.Contains(offline, `class="hidden"`) || strings.Contains(offline, "/robot/controller-qr.png") {
		t.Fatalf("offline robot card = %q", offline)
	}

	online := openSauceRobotControllerCard(true).Render()
	for _, want := range []string{
		"Control the robot",
		"open-sauce-robot-controller",
		`href="/robot"`,
		"Open the robot control page",
		"/robot/controller-qr.png",
		"QR code linking to the robot control page",
	} {
		if !strings.Contains(online, want) {
			t.Errorf("online robot card does not contain %q", want)
		}
	}
}

func TestOpenSauceRoutes(t *testing.T) {
	root := t.TempDir()
	truck := filepath.Join(root, "RCTruck")
	if err := os.MkdirAll(truck, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(truck, "truck.jpg"), []byte("photo"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mountOpenSauce(mux, root)

	page := httptest.NewRecorder()
	mux.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/open-sauce", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "RC Truck Dashboard") {
		t.Fatalf("GET /open-sauce = %d, body %q", page.Code, page.Body.String())
	}

	post := httptest.NewRecorder()
	mux.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/open-sauce", nil))
	if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("POST /open-sauce = %d Allow %q", post.Code, post.Header().Get("Allow"))
	}

	asset := httptest.NewRecorder()
	mux.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/open-sauce/media/RCTruck/truck.jpg", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "photo" {
		t.Fatalf("GET media = %d, body %q", asset.Code, asset.Body.String())
	}

	assetPost := httptest.NewRecorder()
	mux.ServeHTTP(assetPost, httptest.NewRequest(http.MethodPost, "/open-sauce/media/RCTruck/truck.jpg", nil))
	if assetPost.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST media = %d", assetPost.Code)
	}

	makerspaceQR := httptest.NewRecorder()
	mux.ServeHTTP(makerspaceQR, httptest.NewRequest(http.MethodGet, "/open-sauce/makerspace-qr.png", nil))
	if makerspaceQR.Code != http.StatusOK || makerspaceQR.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GET makerspace QR = %d Content-Type %q", makerspaceQR.Code, makerspaceQR.Header().Get("Content-Type"))
	}
	if !bytes.Equal(makerspaceQR.Body.Bytes(), openSauceMakerspaceQR) {
		t.Fatal("makerspace QR response does not match the embedded image")
	}

	makerspaceQRPost := httptest.NewRecorder()
	mux.ServeHTTP(makerspaceQRPost, httptest.NewRequest(http.MethodPost, "/open-sauce/makerspace-qr.png", nil))
	if makerspaceQRPost.Code != http.StatusMethodNotAllowed || makerspaceQRPost.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST makerspace QR = %d Allow %q", makerspaceQRPost.Code, makerspaceQRPost.Header().Get("Allow"))
	}

	youtubeQR := httptest.NewRecorder()
	mux.ServeHTTP(youtubeQR, httptest.NewRequest(http.MethodGet, "/open-sauce/youtube-qr.png", nil))
	if youtubeQR.Code != http.StatusOK || youtubeQR.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("GET YouTube QR = %d Content-Type %q", youtubeQR.Code, youtubeQR.Header().Get("Content-Type"))
	}
	if !bytes.Equal(youtubeQR.Body.Bytes(), openSauceYouTubeQR) {
		t.Fatal("YouTube QR response does not match the embedded image")
	}

	youtubeQRPost := httptest.NewRecorder()
	mux.ServeHTTP(youtubeQRPost, httptest.NewRequest(http.MethodPost, "/open-sauce/youtube-qr.png", nil))
	if youtubeQRPost.Code != http.StatusMethodNotAllowed || youtubeQRPost.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST YouTube QR = %d Allow %q", youtubeQRPost.Code, youtubeQRPost.Header().Get("Allow"))
	}

	robotCard := httptest.NewRecorder()
	mux.ServeHTTP(robotCard, httptest.NewRequest(http.MethodGet, "/open-sauce/robot-controller", nil))
	if robotCard.Code != http.StatusOK || !strings.Contains(robotCard.Body.String(), `class="hidden"`) {
		t.Fatalf("GET robot controller card = %d, body %q", robotCard.Code, robotCard.Body.String())
	}
}
