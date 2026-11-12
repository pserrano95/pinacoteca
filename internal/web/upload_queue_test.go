package web

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pserrano95/pinacoteca/internal/store"
)

func postUpload(t *testing.T, c *http.Client, rawURL string, fields map[string]string, accept string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("image", "drawing.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(tinyPNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, readBody(t, res)
}

func uploadFields(t *testing.T, c *http.Client, base, artist, title, lang string, extra map[string]string) map[string]string {
	t.Helper()
	status, page := getStatus(t, c, base+"/?lang="+lang)
	if status != http.StatusOK {
		t.Fatalf("csrf page: %d %s", status, page)
	}
	fields := map[string]string{
		"csrf":        csrfFrom(t, page),
		"lang":        lang,
		"artist_slug": artist,
		"title":       title,
		"date":        "2024-06-01",
	}
	for k, v := range extra {
		fields[k] = v
	}
	return fields
}

func titles(t *testing.T, st *store.Store, title string) int {
	t.Helper()
	recs, err := st.WalkArtworks()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, rec := range recs {
		if rec.Meta.Title == title {
			n++
		}
	}
	return n
}

func decodeObject(t *testing.T, body string) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("json: %v body %s", err, body)
	}
	return raw
}

func artistSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestUploadJSON_Create(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	id := strings.Repeat("ab", 16)
	status, body := postUpload(t, c, w.ts.URL+"/artworks/new", uploadFields(t, c, w.ts.URL, w.ownArtist, "QueuedSun", "en", map[string]string{"upload_id": id}), "application/json")
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, body)
	}
	raw := decodeObject(t, body)
	if len(raw) != 2 || raw["artist"] != w.ownArtist || raw["folder"] == "" {
		t.Fatalf("create body: %#v", raw)
	}
	if titles(t, w.st, "QueuedSun") != 1 {
		t.Fatal("create did not write one artwork")
	}
	rec, err := w.st.LoadArtwork(w.ownArtist, raw["folder"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Meta.UploadID != id || rec.Meta.SchemaVersion != store.SchemaVersion {
		t.Fatalf("stored meta: %+v", rec.Meta)
	}
	got, err := os.ReadFile(rec.OriginalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, tinyPNG(t)) {
		t.Fatal("original bytes changed")
	}
}

func TestUploadJSON_Duplicate(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	id := strings.Repeat("cd", 16)
	fields := uploadFields(t, c, w.ts.URL, w.ownArtist, "QueuedTwice", "en", map[string]string{"upload_id": id})
	status, body := postUpload(t, c, w.ts.URL+"/artworks/new", fields, "application/json")
	if status != http.StatusCreated {
		t.Fatalf("first: %d %s", status, body)
	}
	first := decodeObject(t, body)
	folder, _ := first["folder"].(string)
	before := artistSnapshot(t, filepath.Join(w.st.ArtistsRoot(), w.ownArtist))
	fields = uploadFields(t, c, w.ts.URL, w.ownArtist, "QueuedTwice", "en", map[string]string{"upload_id": id})
	status, body = postUpload(t, c, w.ts.URL+"/artworks/new", fields, "application/json")
	if status != http.StatusOK {
		t.Fatalf("duplicate: %d %s", status, body)
	}
	second := decodeObject(t, body)
	if second["artist"] != w.ownArtist || second["folder"] != folder {
		t.Fatalf("duplicate body: %#v want folder %s", second, folder)
	}
	after := artistSnapshot(t, filepath.Join(w.st.ArtistsRoot(), w.ownArtist))
	if len(before) != len(after) {
		t.Fatalf("duplicate wrote files: %d then %d", len(before), len(after))
	}
	for path, raw := range before {
		if after[path] != raw {
			t.Fatalf("duplicate changed %s", path)
		}
	}
	if titles(t, w.st, "QueuedTwice") != 1 {
		t.Fatal("duplicate created a second artwork")
	}
}

func TestUploadJSON_Validation(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	cases := []struct {
		lang, title, want string
		extra             map[string]string
	}{
		{"en", "BadDate", "Please fill in the required fields.", map[string]string{"date": ""}},
		{"es", "BadDate", "Rellena los campos obligatorios.", map[string]string{"date": ""}},
		{"en", "BadCrop", "Invalid crop. Choose the image and crop again.", map[string]string{"crop_quarter_turns": "1", "crop_straighten_deg": "10", "crop_x": "-0.1", "crop_y": "0", "crop_w": "0.5", "crop_h": "0.5"}},
		{"es", "BadCrop", "Recorte no válido. Vuelve a elegir la imagen y el recorte.", map[string]string{"crop_quarter_turns": "1", "crop_straighten_deg": "10", "crop_x": "-0.1", "crop_y": "0", "crop_w": "0.5", "crop_h": "0.5"}},
	}
	for _, tc := range cases {
		t.Run(tc.lang+"/"+tc.title, func(t *testing.T) {
			before := titles(t, w.st, tc.title)
			status, body := postUpload(t, c, w.ts.URL+"/artworks/new", uploadFields(t, c, w.ts.URL, w.ownArtist, tc.title, tc.lang, tc.extra), "application/json")
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status %d %s", status, body)
			}
			raw := decodeObject(t, body)
			if len(raw) != 1 || raw["error"] != tc.want {
				t.Fatalf("body %#v", raw)
			}
			if titles(t, w.st, tc.title) != before {
				t.Fatal("validation wrote an artwork")
			}
		})
	}
}

func TestUploadJSON_BadID(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	wants := map[string]string{
		"en": "The upload id is not valid.",
		"es": "El identificador de la subida no es válido.",
	}
	ids := []string{"xyz", strings.Repeat("a", 31), strings.Repeat("AB", 16)}
	for _, lang := range []string{"en", "es"} {
		for _, id := range ids {
			t.Run(lang+"/"+id, func(t *testing.T) {
				before := titles(t, w.st, "BadID")
				status, body := postUpload(t, c, w.ts.URL+"/artworks/new", uploadFields(t, c, w.ts.URL, w.ownArtist, "BadID", lang, map[string]string{"upload_id": id}), "application/json")
				if status != http.StatusUnprocessableEntity {
					t.Fatalf("status %d %s", status, body)
				}
				raw := decodeObject(t, body)
				if len(raw) != 1 || raw["error"] != wants[lang] {
					t.Fatalf("body %#v", raw)
				}
				if titles(t, w.st, "BadID") != before {
					t.Fatal("bad id wrote an artwork")
				}
			})
		}
	}
}

func TestUploadWithoutAcceptStaysRedirect(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	status, body := postArtwork(t, c, w.ts.URL, "ClassicUpload", w.ownArtist, nil)
	if status != http.StatusSeeOther {
		t.Fatalf("without Accept: %d %s", status, body)
	}
	if titles(t, w.st, "ClassicUpload") != 1 {
		t.Fatal("classic upload did not store the artwork")
	}
}

func TestUploadJSON_Auth(t *testing.T) {
	w := newGalleryWorld(t)
	before, err := w.st.WalkArtworks()
	if err != nil {
		t.Fatal(err)
	}

	anon := testClient(t)
	req, err := http.NewRequest(http.MethodPost, w.ts.URL+"/artworks/new", strings.NewReader("nope"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := anon.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readBody(t, res)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no session: %d %s", res.StatusCode, body)
	}

	c := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	fields := uploadFields(t, c, w.ts.URL, w.ownArtist, "NoCSRF", "en", nil)
	delete(fields, "csrf")
	status, body := postUpload(t, c, w.ts.URL+"/artworks/new", fields, "application/json")
	if status != http.StatusForbidden {
		t.Fatalf("no csrf: %d %s", status, body)
	}

	status, body = postUpload(t, c, w.ts.URL+"/artworks/new", uploadFields(t, c, w.ts.URL, w.otherArtist, "NotMine", "en", nil), "application/json")
	if status != http.StatusNotFound {
		t.Fatalf("other artist: %d %s", status, body)
	}

	after, err := w.st.WalkArtworks()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("auth failures wrote artworks: %d then %d", len(before), len(after))
	}
}

func TestUploadQueueScriptInHead(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	for _, path := range []string{"/", "/artworks/new"} {
		status, body := getStatus(t, c, w.ts.URL+path)
		if status != http.StatusOK {
			t.Fatalf("%s: %d", path, status)
		}
		if !strings.Contains(body, `<script src="/static/upload-queue.js" defer></script>`) {
			t.Fatalf("%s missing deferred upload-queue.js", path)
		}
		if !strings.Contains(body, `data-pending="{n} uploads pending"`) || !strings.Contains(body, `data-kept="The upload stays queued and will be sent when it can."`) {
			t.Fatalf("%s missing queue copy", path)
		}
	}
	status, body := getStatus(t, c, w.ts.URL+"/artworks/new?lang=es")
	if status != http.StatusOK || !strings.Contains(body, `data-pending="{n} subidas pendientes"`) {
		t.Fatalf("es queue copy: %d %s", status, body)
	}
	js, err := os.ReadFile("static/upload-queue.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(js)
	for _, visible := range []string{"subidas pendientes", "uploads pending", "sigue en cola", "stays queued"} {
		if strings.Contains(text, visible) {
			t.Fatalf("upload-queue.js hard-codes %q", visible)
		}
	}
}
