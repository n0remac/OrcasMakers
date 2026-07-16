package main

import (
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

func TestOpenSaucePageContainsProjectsMediaAndControls(t *testing.T) {
	page := OpenSaucePage(map[string][]string{
		"truck": {"/open-sauce/media/RCTruck/truck.jpg", "/open-sauce/media/RCTruck/drive.mp4"},
	}).Render()

	for _, want := range []string{
		"Open Sauce</a>",
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
	} {
		if !strings.Contains(page, want) {
			t.Errorf("Open Sauce page does not contain %q", want)
		}
	}
	if !strings.Contains(page, "No media found for this project.") {
		t.Error("Open Sauce page does not render empty project media states")
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

	robotCard := httptest.NewRecorder()
	mux.ServeHTTP(robotCard, httptest.NewRequest(http.MethodGet, "/open-sauce/robot-controller", nil))
	if robotCard.Code != http.StatusOK || !strings.Contains(robotCard.Body.String(), `class="hidden"`) {
		t.Fatalf("GET robot controller card = %d, body %q", robotCard.Code, robotCard.Body.String())
	}
}
