package web

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/export"
	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

func visibleWorld(t *testing.T) *guardianWorld {
	t.Helper()
	w := newGuardianWorld(t)
	mustGuardian(t, w.st.ApproveArtistGallery(w.artist, w.home, w.guardianID))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	return w
}
func (w *guardianWorld) visibilityURL() string {
	return w.base + "/artists/" + w.artist + "/artworks/" + w.folder + "/visibility"
}
func (w *guardianWorld) setHidden(t *testing.T, hidden string) {
	t.Helper()
	status, _, body := postForm(t, w.guardian, w.visibilityURL(), csrfOf(t, w.guardian, w.page()), map[string]string{"hidden": hidden})
	if status != 303 {
		t.Fatalf("visibility POST: %d %s", status, body)
	}
}
func (w *guardianWorld) artworkFile() string {
	return filepath.Join(w.st.ArtistsRoot(), w.artist, w.folder, store.ArtworkFile)
}

func TestHiddenArtworkHideShowForViewerAndOwner(t *testing.T) {
	w := visibleWorld(t)
	for _, c := range []*http.Client{w.other, w.owner} {
		w.assertVisible(t, c, true)
	}
	for _, lang := range []string{"es", "en"} {
		_, body := getStatus(t, w.guardian, w.page()+"?lang="+lang)
		label := "Hide"
		if lang == "es" {
			label = "Ocultar"
		}
		if !strings.Contains(body, label) || !strings.Contains(body, "GuardedSun") {
			t.Fatalf("missing %s control: %s", label, body)
		}
	}
	w.setHidden(t, "true")
	for _, tc := range []struct {
		name   string
		client *http.Client
	}{{"viewer", w.other}, {"owner", w.owner}} {
		t.Run("hidden/"+tc.name, func(t *testing.T) { w.assertVisible(t, tc.client, false) })
	}
	for _, suffix := range []string{"", "/original", "/thumb"} {
		status, _ := getStatus(t, w.guardian, strings.TrimSuffix(w.visibilityURL(), "/visibility")+suffix)
		if status != 404 {
			t.Fatalf("guardian without gallery artwork%s: %d", suffix, status)
		}
	}
	for _, lang := range []string{"es", "en"} {
		_, body := getStatus(t, w.guardian, w.page()+"?lang="+lang)
		label := "Show"
		if lang == "es" {
			label = "Mostrar"
		}
		if !strings.Contains(body, label) {
			t.Fatalf("missing %s control", label)
		}
	}
	// Home remains a gallery of memberships, even for a guardian.
	_, body := getStatus(t, w.guardian, w.base+"/")
	if strings.Contains(body, "GuardedSun") {
		t.Fatal("guardian gained gallery membership")
	}
	w.setHidden(t, "false")
	for _, c := range []*http.Client{w.other, w.owner} {
		w.assertVisible(t, c, true)
	}
}

func TestHiddenArtworkUnauthorizedPOSTLeavesMetadataUnchanged(t *testing.T) {
	w := visibleWorld(t)
	for _, hidden := range []string{"true", "false"} {
		for _, tc := range []struct {
			name string
			c    *http.Client
			want int
		}{{"owner", w.owner, 403}, {"viewer", w.other, 403}, {"visitor", testClient(t), 303}} {
			t.Run(hidden+"/"+tc.name, func(t *testing.T) {
				before, err := os.ReadFile(w.artworkFile())
				mustGuardian(t, err)
				path := w.base + "/"
				if tc.name == "visitor" {
					path = w.base + "/login"
				}
				status, _, _ := postForm(t, tc.c, w.visibilityURL(), csrfOf(t, tc.c, path), map[string]string{"hidden": hidden})
				if status != tc.want {
					t.Fatalf("status %d want %d", status, tc.want)
				}
				after, err := os.ReadFile(w.artworkFile())
				mustGuardian(t, err)
				if !bytes.Equal(before, after) {
					t.Fatal("unauthorized write to artwork.json")
				}
			})
		}
		w.setHidden(t, "true")
	}
}

func TestHiddenArtworkRequiresCSRF(t *testing.T) {
	w := visibleWorld(t)
	before, err := os.ReadFile(w.artworkFile())
	mustGuardian(t, err)
	status, _, _ := postForm(t, w.guardian, w.visibilityURL(), "", map[string]string{"hidden": "true"})
	if status != 403 {
		t.Fatalf("missing CSRF: %d", status)
	}
	after, err := os.ReadFile(w.artworkFile())
	mustGuardian(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("missing CSRF wrote metadata")
	}
}

func TestHiddenArtworkRejectsCrossArtist(t *testing.T) {
	w := visibleWorld(t)
	bo, err := w.st.CreateArtistWithGuardian("Bo", time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), w.ownerID)
	mustGuardian(t, err)
	art, err := w.st.CreateArtwork(store.CreateArtworkInput{ArtistSlug: bo.Slug, Title: "BoOnly", Date: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), ImageBytes: tinyPNG(t)})
	mustGuardian(t, err)
	path := filepath.Join(art.Dir, store.ArtworkFile)
	before, err := os.ReadFile(path)
	mustGuardian(t, err)
	for _, artist := range []string{bo.Slug, w.artist} {
		status, _, _ := postForm(t, w.guardian, w.base+"/artists/"+artist+"/artworks/"+art.Folder+"/visibility", csrfOf(t, w.guardian, w.page()), map[string]string{"hidden": "true"})
		if status != 403 {
			t.Fatalf("cross artist %s: %d", artist, status)
		}
	}
	after, err := os.ReadFile(path)
	mustGuardian(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("cross artist wrote metadata")
	}
}

func TestHiddenArtworkSurvivesDeletingIndex(t *testing.T) {
	w := visibleWorld(t)
	w.setHidden(t, "true")
	rowBefore, err := w.srv.Index.GetArtwork(w.artist, w.folder)
	mustGuardian(t, err)
	if !rowBefore.Hidden {
		t.Fatal("hide did not update index")
	}
	before, err := w.srv.Index.Snapshot()
	mustGuardian(t, err)
	ix := reopenIndex(t, w.srv, w.st, w.dir)
	row, err := ix.GetArtwork(w.artist, w.folder)
	mustGuardian(t, err)
	if !row.Hidden {
		t.Fatal("hidden artwork became visible after deleting index.db and reindexing")
	}
	after, err := ix.Snapshot()
	mustGuardian(t, err)
	mustGuardian(t, index.EqualSnapshots(before, after))
	w.other = memberClient(t, ix, w.base, w.otherID)
	w.assertVisible(t, w.other, false)
}

func TestHiddenArtworkExportIncludesArchive(t *testing.T) {
	w := visibleWorld(t)
	w.setHidden(t, "true")
	out := t.TempDir()
	mustGuardian(t, export.ExportArtist(w.st, w.artist, out, "en"))
	// Export copies the complete artist archive, including hidden metadata and original.
	rec, err := w.st.LoadArtwork(w.artist, w.folder)
	mustGuardian(t, err)
	var metadata, original bool
	mustGuardian(t, filepath.Walk(out, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.Name() == store.ArtworkFile {
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			metadata = bytes.Contains(raw, []byte(`"hidden": true`))
		}
		if info.Name() == rec.OriginalName {
			raw, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			source, e := os.ReadFile(rec.OriginalPath)
			if e != nil {
				return e
			}
			original = bytes.Equal(raw, source)
		}
		return nil
	}))
	if !metadata || !original {
		t.Fatalf("export missing hidden archive: metadata %v original %v", metadata, original)
	}
}
