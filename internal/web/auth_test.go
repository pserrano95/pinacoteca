package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

const (
	testOrigin = "http://localhost:8080"
	testRPID   = "localhost"
)

func newTestServer(t *testing.T) (*Server, *store.Store, *index.Index, string) {
	t.Helper()
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	cfg, err := ConfigFromBase(testOrigin, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RPID != testRPID || cfg.Origin != testOrigin {
		t.Fatalf("rp config: %+v", cfg)
	}
	srv, err := New(st, ix, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return srv, st, ix, dir
}

func testClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func csrfFrom(t *testing.T, body string) string {
	t.Helper()
	const key = `name="csrf-token" content="`
	i := strings.Index(body, key)
	if i < 0 {
		t.Fatalf("csrf token missing from page: %s", body)
	}
	rest := body[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatal("csrf token not closed")
	}
	return rest[:j]
}

func postCSRF(t *testing.T, client *http.Client, url, csrf, contentType string, body []byte) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-CSRF-Token", csrf)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res, readBody(t, res)
}

func registerPasskey(t *testing.T, client *http.Client, base, token string) *softAuthn {
	t.Helper()
	res, err := client.Get(base + "/invite/" + token)
	if err != nil {
		t.Fatal(err)
	}
	page := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("invite page: %d %s", res.StatusCode, page)
	}
	csrf := csrfFrom(t, page)
	res, beginBody := postCSRF(t, client, base+"/invite/"+token+"/begin", csrf, "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("register begin: %d %s", res.StatusCode, beginBody)
	}
	var opts struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			User      struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal([]byte(beginBody), &opts); err != nil {
		t.Fatal(err)
	}
	auth := newSoftAuthn(t)
	user, err := base64.RawURLEncoding.DecodeString(opts.PublicKey.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	auth.user = user
	payload := auth.registrationBody(opts.PublicKey.Challenge, testOrigin, testRPID)
	res, fin := postCSRF(t, client, base+"/invite/"+token+"/finish", csrf, "application/json", payload)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("register finish: %d %s", res.StatusCode, fin)
	}
	return auth
}

func loginPasskey(t *testing.T, client *http.Client, base string, auth *softAuthn) {
	t.Helper()
	res, err := client.Get(base + "/login")
	if err != nil {
		t.Fatal(err)
	}
	page := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login page: %d %s", res.StatusCode, page)
	}
	csrf := csrfFrom(t, page)
	res, beginBody := postCSRF(t, client, base+"/login/begin", csrf, "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login begin: %d %s", res.StatusCode, beginBody)
	}
	var opts struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal([]byte(beginBody), &opts); err != nil {
		t.Fatal(err)
	}
	payload := auth.assertionBody(opts.PublicKey.Challenge, testOrigin, testRPID)
	res, fin := postCSRF(t, client, base+"/login/finish", csrf, "application/json", payload)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login finish: %d %s", res.StatusCode, fin)
	}
}

func inviteToken(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	link, err := st.CreateInvite(name, testOrigin, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	i := strings.LastIndex(link, "/invite/")
	if i < 0 {
		t.Fatal(link)
	}
	return link[i+len("/invite/"):]
}

func TestEmptyInstanceShowsNothing(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	client := testClient(t)

	res, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "/login") {
		t.Fatalf("gallery without session: %d %s %s", res.StatusCode, res.Header.Get("Location"), body)
	}
	res, err = client.Get(ts.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	page := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", res.StatusCode)
	}
	for _, hidden := range []string{"Upload artwork", "New artist", "No artworks yet", "Subir obra"} {
		if strings.Contains(page, hidden) {
			t.Fatalf("login page shows gallery content %q", hidden)
		}
	}
	res, err = client.Get(ts.URL + "/static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("static: %d", res.StatusCode)
	}
}

func TestInviteRegisterLoginLogoutAndReindex(t *testing.T) {
	srv, st, ix, dir := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	c1 := testClient(t)
	token1 := inviteToken(t, st, "Ana")
	auth1 := registerPasskey(t, c1, ts.URL, token1)
	res, err := c1.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	gallery := readBody(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(gallery, "Gallery") {
		t.Fatalf("gallery after register: %d %s", res.StatusCode, gallery)
	}

	c2 := testClient(t)
	token2 := inviteToken(t, st, "Luis")
	auth2 := registerPasskey(t, c2, ts.URL, token2)

	used := invitePage(t, c1, ts.URL+"/invite/"+token1)
	expiredToken := inviteTokenAt(t, st, "Old", time.Now().Add(-8*24*time.Hour))
	expired := invitePage(t, c1, ts.URL+"/invite/"+expiredToken)
	invented := invitePage(t, c1, ts.URL+"/invite/not-a-real-token")
	if used.status != http.StatusNotFound || expired.status != used.status || invented.status != used.status {
		t.Fatalf("invalid invite statuses: used %d expired %d invented %d", used.status, expired.status, invented.status)
	}
	if used.body != expired.body || used.body != invented.body {
		t.Fatalf("invalid invite pages differ:\nused:\n%s\nexpired:\n%s\ninvented:\n%s", used.body, expired.body, invented.body)
	}

	csrf := csrfFrom(t, gallery)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/artists/new", strings.NewReader("name=Nope&birth_date=2020-01-01&lang=en"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err = c1.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	noCSRF := readBody(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without CSRF: %d %s", res.StatusCode, noCSRF)
	}

	res, beginBody := postCSRF(t, testClient(t), ts.URL+"/login/begin", "", "", nil)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("public POST without CSRF: %d %s", res.StatusCode, beginBody)
	}

	req, err = http.NewRequest(http.MethodPost, ts.URL+"/logout", strings.NewReader("csrf="+csrf+"&lang=en"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err = c1.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = readBody(t, res)
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "/login") {
		t.Fatalf("logout: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	res, err = c1.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = readBody(t, res)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("after logout: %d", res.StatusCode)
	}

	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	db := index.DBPath(dir)
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	ix2, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ix2.Close()
	if err := ix2.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	srv.Index = ix2

	loginPasskey(t, c1, ts.URL, auth1)
	res, err = c1.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Ana after reindex: %d %s", res.StatusCode, readBody(t, res))
	}
	_ = readBody(t, res)
	loginPasskey(t, c2, ts.URL, auth2)
	res, err = c2.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("Luis after reindex: %d %s", res.StatusCode, readBody(t, res))
	}
	_ = readBody(t, res)
}

type pageResult struct {
	status int
	body   string
}

func invitePage(t *testing.T, client *http.Client, url string) pageResult {
	t.Helper()
	res, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return pageResult{status: res.StatusCode, body: readBody(t, res)}
}

func inviteTokenAt(t *testing.T, st *store.Store, name string, now time.Time) string {
	t.Helper()
	link, err := st.CreateInvite(name, testOrigin, now)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.LastIndex(link, "/invite/")
	return link[i+len("/invite/"):]
}

func TestOriginalAndThumbRequireSession(t *testing.T) {
	srv, st, ix, _ := newTestServer(t)
	artist, err := st.CreateArtist("Lucía", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	art, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: artist.Slug,
		Title:      "Sun",
		Date:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: tinyPNG(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.RebuildKeepingSessions(st); err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	for _, suffix := range []string{"/original", "/thumb"} {
		path := "/artists/" + art.ArtistSlug + "/artworks/" + art.Folder + suffix
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s served the image with no session", path)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: got %d, want 401 (404 means the session check was removed)", path, rec.Code)
		}
	}
}

func TestNonPublicRoutesRequireSession(t *testing.T) {
	public := map[string]bool{
		"GET /static/":                true,
		"GET /login":                  true,
		"GET /invite/{token}":         true,
		"POST /login/begin":           true,
		"POST /login/finish":          true,
		"POST /invite/{token}/begin":  true,
		"POST /invite/{token}/finish": true,
	}
	srv, _, _, _ := newTestServer(t)
	h := srv.Handler()
	var checked int
	for _, rt := range srv.routeSpecs() {
		key := rt.Method + " " + rt.Pattern
		if public[key] {
			if !rt.Public {
				t.Errorf("%s is in the public allowlist but the server marks it closed", key)
			}
			continue
		}
		if rt.Public {
			t.Errorf("%s is public on the server and not in the allowlist", key)
		}
		path := concretePath(rt.Pattern)
		req := httptest.NewRequest(rt.Method, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if !closedResponse(rec) {
			t.Errorf("%s %s: got %d location %q, want 401 or a redirect to /login", rt.Method, path, rec.Code, rec.Header().Get("Location"))
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no closed routes were checked")
	}
}

func concretePath(pattern string) string {
	if pattern == "/{$}" {
		return "/"
	}
	r := strings.NewReplacer("{artist}", "someone", "{folder}", "somefolder", "{token}", "sometoken")
	return r.Replace(pattern)
}

func closedResponse(rec *httptest.ResponseRecorder) bool {
	if rec.Code == http.StatusUnauthorized {
		return true
	}
	return rec.Code >= 300 && rec.Code < 400 && strings.Contains(rec.Header().Get("Location"), "/login")
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
