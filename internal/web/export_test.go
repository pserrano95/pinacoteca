package web

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/i18n"
	"github.com/pserrano95/pinacoteca/internal/store"
)

// Include directories as well as every file (including the index) so even an
// empty temporary directory accidentally left under data changes the snapshot.
func exportSnapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	mustGuardian(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			files[rel+"/"] = [32]byte{}
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[rel] = sha256.Sum256(raw)
		return nil
	}))
	return files
}

func exportUnchanged(t *testing.T, w *guardianWorld, tmp string, before map[string][32]byte) {
	t.Helper()
	if after := exportSnapshot(t, w.dir); !reflect.DeepEqual(before, after) {
		t.Error("download changed files or bytes under data")
	}
	entries, err := os.ReadDir(tmp)
	mustGuardian(t, err)
	if len(entries) != 0 {
		t.Errorf("TMPDIR not empty after download: %v", entries)
	}
}

func exportGET(t *testing.T, c *http.Client, target string) (*http.Response, []byte) {
	t.Helper()
	res, err := c.Get(target)
	mustGuardian(t, err)
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	mustGuardian(t, err)
	return res, raw
}

func exportZIP(t *testing.T, res *http.Response, raw []byte, slug string) map[string][]byte {
	t.Helper()
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("download: %d %v %s", res.StatusCode, res.Header, raw)
	}
	if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="pinacoteca-`+slug+`.zip"` {
		t.Fatalf("disposition: %q", got)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	mustGuardian(t, err)
	files := map[string][]byte{}
	for _, f := range zr.File {
		if !fs.ValidPath(f.Name) || strings.Contains(f.Name, `\`) || !strings.HasPrefix(f.Name, slug+"/") {
			t.Fatalf("unsafe ZIP path: %q", f.Name)
		}
		if _, exists := files[f.Name]; exists {
			t.Fatalf("duplicate ZIP path: %q", f.Name)
		}
		in, err := f.Open()
		mustGuardian(t, err)
		b, err := io.ReadAll(in)
		in.Close()
		mustGuardian(t, err)
		files[f.Name] = b
	}
	return files
}

func TestArtistExportArchive(t *testing.T) {
	for _, scenario := range []string{"complete", "hidden", "empty", "english", "missing_thumbnail"} {
		t.Run(scenario, func(t *testing.T) {
			w := visibleWorld(t)
			slug := w.artist
			lang := "es"
			if scenario == "hidden" {
				w.setHidden(t, "true")
			}
			if scenario == "empty" {
				a, err := w.st.CreateArtistWithGuardian("Empty", time.Now(), w.guardianID)
				mustGuardian(t, err)
				slug = a.Slug
			} else {
				// More than one artwork prevents an implementation that exports only one.
				_, err := w.st.CreateArtwork(store.CreateArtworkInput{ArtistSlug: slug, Title: "Moon", Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), ImageBytes: tinyPNG(t)})
				mustGuardian(t, err)
			}
			if scenario == "english" {
				lang = "en"
			}
			if scenario == "missing_thumbnail" {
				art, err := w.st.LoadArtwork(slug, w.folder)
				mustGuardian(t, err)
				mustGuardian(t, os.Remove(art.ThumbPath))
			}
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			before := exportSnapshot(t, w.dir)
			defer exportUnchanged(t, w, tmp, before)
			res, raw := exportGET(t, w.guardian, w.base+"/artists/"+slug+"/export?lang="+lang)
			files := exportZIP(t, res, raw, slug)
			html := string(files[slug+"/index.html"])
			if !strings.Contains(html, `<html lang="`+lang+`">`) {
				t.Fatalf("HTML language: %s", html)
			}
			artistRaw, err := os.ReadFile(filepath.Join(w.st.ArtistsRoot(), slug, store.ArtistFile))
			mustGuardian(t, err)
			if !bytes.Equal(files[slug+"/artist.json"], artistRaw) {
				t.Error("missing or changed artist.json")
			}
			arts, err := w.st.WalkArtworks()
			mustGuardian(t, err)
			expectedFiles := 2
			for _, art := range arts {
				if art.ArtistSlug != slug {
					continue
				}
				prefix := slug + "/" + art.Folder + "/"
				original, err := os.ReadFile(art.OriginalPath)
				mustGuardian(t, err)
				zipped, ok := files[prefix+art.OriginalName]
				if !ok || sha256.Sum256(original) != sha256.Sum256(zipped) {
					t.Error("original SHA-256 mismatch")
				}
				meta, err := os.ReadFile(filepath.Join(art.Dir, store.ArtworkFile))
				mustGuardian(t, err)
				if !bytes.Equal(meta, files[prefix+store.ArtworkFile]) {
					t.Error("missing or changed artwork.json")
				}
				if scenario == "hidden" && art.Folder == w.folder && !bytes.Contains(meta, []byte(`"hidden": true`)) {
					t.Error("hidden fixture not hidden")
				}
				expectedFiles += 2
				if thumb, err := os.ReadFile(art.ThumbPath); err == nil {
					expectedFiles++
					if !bytes.Equal(thumb, files[prefix+"derived/thumb.jpg"]) {
						t.Error("missing or changed thumbnail")
					}
				}
			}
			if len(files) != expectedFiles {
				t.Errorf("ZIP entries: got %d want %d", len(files), expectedFiles)
			}
			for _, match := range regexp.MustCompile(`(?:href|src)="([^"]+)"`).FindAllStringSubmatch(html, -1) {
				u, err := url.Parse(match[1])
				mustGuardian(t, err)
				target := path.Join(slug, u.Path)
				if u.IsAbs() || strings.HasPrefix(u.Path, "/") || files[target] == nil {
					t.Errorf("HTML link does not resolve inside ZIP: %q", match[1])
				}
			}
			if scenario == "empty" && (!strings.Contains(html, i18n.T(lang, "export_empty", nil)) || strings.Contains(html, `<article`)) {
				t.Error("empty artist index contains artworks or lacks empty message")
			}
			if scenario == "english" && (!strings.Contains(html, "<dt>Date</dt>") || !strings.Contains(html, "Artworks by Ada")) {
				t.Error("export HTML not translated to English")
			}
		})
	}
}

func TestArtistExportDenied(t *testing.T) {
	for _, scenario := range []string{"owner", "viewer", "other_guardian", "missing_artist", "anonymous"} {
		t.Run(scenario, func(t *testing.T) {
			w := visibleWorld(t)
			c := w.guardian
			slug := w.artist
			switch scenario {
			case "owner":
				c = w.owner
			case "viewer":
				c = w.other
			case "other_guardian":
				_, err := w.st.CreateArtistWithGuardian("Bo", time.Now(), w.ownerID)
				mustGuardian(t, err)
				c = w.owner
			case "missing_artist":
				slug = "missing"
			case "anonymous":
				c = testClient(t)
			}
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			before := exportSnapshot(t, w.dir)
			defer exportUnchanged(t, w, tmp, before)
			res, raw := exportGET(t, c, w.base+"/artists/"+slug+"/export")
			want := 403
			if scenario == "anonymous" {
				page, _ := exportGET(t, c, w.page())
				want = page.StatusCode
				if res.Header.Get("Location") != page.Header.Get("Location") {
					t.Error("anonymous redirect differs from guardian page")
				}
			}
			if res.StatusCode != want {
				t.Errorf("%s: got %d want %d", scenario, res.StatusCode, want)
			}
			if res.Header.Get("Content-Type") == "application/zip" || res.Header.Get("Content-Disposition") != "" || bytes.HasPrefix(raw, []byte("PK")) {
				t.Error("denied download returned ZIP")
			}
		})
	}
}

func TestArtistExportFailure(t *testing.T) {
	for _, scenario := range []string{"temporary_inside_data", "thumbnail_directory", "temporary_creation"} {
		t.Run(scenario, func(t *testing.T) {
			w := visibleWorld(t)
			tmp := t.TempDir()
			if scenario == "temporary_inside_data" {
				tmp = w.dir
			}
			if scenario == "thumbnail_directory" {
				art, err := w.st.LoadArtwork(w.artist, w.folder)
				mustGuardian(t, err)
				mustGuardian(t, os.Remove(art.ThumbPath))
				mustGuardian(t, os.Mkdir(art.ThumbPath, 0o755))
			}
			tempRoot := tmp
			if scenario == "temporary_creation" {
				tempRoot = filepath.Join(tmp, "nonexistent")
			}
			t.Setenv("TMPDIR", tempRoot)
			before := exportSnapshot(t, w.dir)
			defer func() {
				if !reflect.DeepEqual(before, exportSnapshot(t, w.dir)) {
					t.Error("failed export changed data")
				}
				if tmp != w.dir {
					exportUnchanged(t, w, tmp, before)
				}
			}()
			res, raw := exportGET(t, w.guardian, w.base+"/artists/"+w.artist+"/export?lang=es")
			if res.StatusCode != 500 || string(raw) != i18n.T("es", "error_generic", nil)+"\n" {
				t.Errorf("failure: %d %q", res.StatusCode, raw)
			}
			if res.Header.Get("Content-Type") == "application/zip" || res.Header.Get("Content-Disposition") != "" {
				t.Error("export failure sent ZIP headers")
			}
		})
	}
}

type brokenExportWriter struct {
	header http.Header
	writes int
}

func (w *brokenExportWriter) Header() http.Header { return w.header }
func (w *brokenExportWriter) WriteHeader(int)     {}
func (w *brokenExportWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New("client disconnected")
}

func TestArtistExportInterrupted(t *testing.T) {
	for _, scenario := range []string{"write_failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			w := visibleWorld(t)
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			before := exportSnapshot(t, w.dir)
			defer exportUnchanged(t, w, tmp, before)
			req := httptest.NewRequest("GET", w.base+"/artists/"+w.artist+"/export", nil)
			for _, cookie := range w.guardian.Jar.Cookies(req.URL) {
				req.AddCookie(cookie)
			}
			if scenario == "cancelled" {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			dst := &brokenExportWriter{header: make(http.Header)}
			func() {
				defer func() {
					if got := recover(); got != http.ErrAbortHandler {
						t.Errorf("interrupted ZIP: got panic %v want ErrAbortHandler", got)
					}
				}()
				w.srv.Handler().ServeHTTP(dst, req)
			}()
			if scenario == "write_failure" && dst.writes == 0 {
				t.Error("did not exercise failed response write")
			}
		})
	}
}

func TestArtistExportLinkTranslated(t *testing.T) {
	w := visibleWorld(t)
	for _, lang := range []string{"es", "en"} {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		before := exportSnapshot(t, w.dir)
		res, raw := exportGET(t, w.guardian, w.page()+"?lang="+lang)
		want := `<a href="/artists/` + w.artist + `/export?lang=` + lang + `">` + i18n.T(lang, "guardian_export", nil) + `</a>`
		if res.StatusCode != 200 || !strings.Contains(string(raw), want) {
			t.Errorf("missing translated download link for %s", lang)
		}
		exportUnchanged(t, w, tmp, before)
	}
}
