package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

type pwaManifest struct {
	Name            string        `json:"name"`
	ShortName       string        `json:"short_name"`
	StartURL        string        `json:"start_url"`
	Scope           string        `json:"scope"`
	Display         string        `json:"display"`
	BackgroundColor string        `json:"background_color"`
	ThemeColor      string        `json:"theme_color"`
	Icons           []pwaIconSpec `json:"icons"`
}

type pwaIconSpec struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

func TestPWAManifest(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /manifest.webmanifest without session got %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/manifest+json") {
		t.Fatalf("Content-Type got %q, want application/manifest+json", ct)
	}

	var m pwaManifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal manifest JSON: %v", err)
	}

	if m.Name != "Pinacoteca" {
		t.Errorf("manifest name = %q, want Pinacoteca", m.Name)
	}
	if m.ShortName != "Pinacoteca" {
		t.Errorf("manifest short_name = %q, want Pinacoteca", m.ShortName)
	}
	if m.StartURL != "/" {
		t.Errorf("manifest start_url = %q, want /", m.StartURL)
	}
	if m.Scope != "/" {
		t.Errorf("manifest scope = %q, want /", m.Scope)
	}
	if m.Display != "standalone" {
		t.Errorf("manifest display = %q, want standalone", m.Display)
	}
	if m.BackgroundColor != "#f7f3ec" {
		t.Errorf("manifest background_color = %q, want #f7f3ec", m.BackgroundColor)
	}
	if m.ThemeColor != "#f7f3ec" {
		t.Errorf("manifest theme_color = %q, want #f7f3ec", m.ThemeColor)
	}

	var has192, has512, hasMaskable bool
	for _, icon := range m.Icons {
		if icon.Sizes == "192x192" && (icon.Purpose == "any" || icon.Purpose == "") {
			has192 = true
		}
		if icon.Sizes == "512x512" && (icon.Purpose == "any" || icon.Purpose == "") {
			has512 = true
		}
		if icon.Purpose == "maskable" {
			hasMaskable = true
		}

		iconReq := httptest.NewRequest(http.MethodGet, icon.Src, nil)
		iconRec := httptest.NewRecorder()
		h.ServeHTTP(iconRec, iconReq)

		if iconRec.Code != http.StatusOK {
			t.Fatalf("icon %s without session returned status %d, want 200", icon.Src, iconRec.Code)
		}
		iconCT := iconRec.Header().Get("Content-Type")
		if !strings.HasPrefix(iconCT, "image/png") {
			t.Errorf("icon %s Content-Type = %q, want image/png", icon.Src, iconCT)
		}

		var declaredW, declaredH int
		if _, err := fmt.Sscanf(icon.Sizes, "%dx%d", &declaredW, &declaredH); err != nil {
			t.Fatalf("icon %s has invalid sizes %q: %v", icon.Src, icon.Sizes, err)
		}

		cfg, format, err := image.DecodeConfig(bytes.NewReader(iconRec.Body.Bytes()))
		if err != nil {
			t.Fatalf("image.DecodeConfig for %s: %v", icon.Src, err)
		}
		if format != "png" {
			t.Errorf("icon %s format = %q, want png", icon.Src, format)
		}
		if cfg.Width != declaredW || cfg.Height != declaredH {
			t.Fatalf("icon %s: image.DecodeConfig got %dx%d, want %dx%d declared by sizes %q", icon.Src, cfg.Width, cfg.Height, declaredW, declaredH, icon.Sizes)
		}
	}

	if !has192 {
		t.Error("manifest missing 192px icon")
	}
	if !has512 {
		t.Error("manifest missing 512px icon")
	}
	if !hasMaskable {
		t.Error("manifest missing maskable icon")
	}
}

func TestPWAServiceWorker(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/sw.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sw.js without session got %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/javascript") {
		t.Errorf("Content-Type got %q, want text/javascript", ct)
	}

	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "no-cache") {
		t.Errorf("Cache-Control got %q, want no-cache", cc)
	}

	body := rec.Body.String()
	if strings.Contains(body, "addEventListener('fetch'") {
		t.Error("sw.js must not contain addEventListener('fetch'")
	}
	if strings.Contains(body, "caches") {
		t.Error("sw.js must not contain caches")
	}
}

func TestPWAHeadTags(t *testing.T) {
	srv, st, ix, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	checkHeadTags := func(t *testing.T, body, pageDesc string) {
		t.Helper()
		if !strings.Contains(body, `rel="manifest"`) {
			t.Errorf("%s missing rel=\"manifest\"", pageDesc)
		}
		if !strings.Contains(body, `name="theme-color"`) {
			t.Errorf("%s missing name=\"theme-color\"", pageDesc)
		}
		if !strings.Contains(body, `rel="apple-touch-icon"`) {
			t.Errorf("%s missing rel=\"apple-touch-icon\"", pageDesc)
		}
		if !strings.Contains(body, `/static/pwa.js`) {
			t.Errorf("%s missing /static/pwa.js", pageDesc)
		}

		manifestRe := regexp.MustCompile(`<link[^>]*rel=["']manifest["'][^>]*href=["']([^"']+)["']`)
		manifestMatches := manifestRe.FindStringSubmatch(body)
		if len(manifestMatches) < 2 {
			manifestRe = regexp.MustCompile(`<link[^>]*href=["']([^"']+)["'][^>]*rel=["']manifest["']`)
			manifestMatches = manifestRe.FindStringSubmatch(body)
		}
		if len(manifestMatches) < 2 {
			t.Fatalf("%s: could not extract manifest href", pageDesc)
		}
		manifestURL := manifestMatches[1]

		appleRe := regexp.MustCompile(`<link[^>]*rel=["']apple-touch-icon["'][^>]*href=["']([^"']+)["']`)
		appleMatches := appleRe.FindStringSubmatch(body)
		if len(appleMatches) < 2 {
			appleRe = regexp.MustCompile(`<link[^>]*href=["']([^"']+)["'][^>]*rel=["']apple-touch-icon["']`)
			appleMatches = appleRe.FindStringSubmatch(body)
		}
		if len(appleMatches) < 2 {
			t.Fatalf("%s: could not extract apple-touch-icon href", pageDesc)
		}
		appleURL := appleMatches[1]

		for _, rawURL := range []string{manifestURL, appleURL} {
			fullURL := rawURL
			if !strings.HasPrefix(fullURL, "http://") && !strings.HasPrefix(fullURL, "https://") {
				fullURL = ts.URL + rawURL
			}
			res, err := http.Get(fullURL)
			if err != nil {
				t.Fatalf("GET %s (%s): %v", rawURL, pageDesc, err)
			}
			_ = res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Errorf("GET %s (%s) got status %d, want 200", rawURL, pageDesc, res.StatusCode)
			}
		}
	}

	// 1. GET /login without session
	res, err := http.Get(ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	loginBody, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /login status = %d, want 200", res.StatusCode)
	}
	checkHeadTags(t, string(loginBody), "GET /login without session")

	// 2. GET / with session
	m := testMember(t, st, "PWA User", "pwa-cred")
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	client := testClient(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessID := "session-pwa-user"
	if err := ix.CreateSession(sessID, m.ID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(ts.URL)
	client.Jar.SetCookies(u, []*http.Cookie{{Name: sessionCookie, Value: sessID, Path: "/"}})

	resAuth, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	homeBody, err := io.ReadAll(resAuth.Body)
	_ = resAuth.Body.Close()
	if resAuth.StatusCode != http.StatusOK {
		t.Fatalf("GET / with session status = %d, want 200", resAuth.StatusCode)
	}
	checkHeadTags(t, string(homeBody), "GET / with session")
}
