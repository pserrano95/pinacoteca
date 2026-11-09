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

	"github.com/pserrano95/pinacoteca/internal/imageutil"
	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

func cropFields() map[string]string {
	return map[string]string{"crop_quarter_turns": "1", "crop_straighten_deg": "10", "crop_x": "0", "crop_y": "0", "crop_w": "0.5", "crop_h": "0.5"}
}

func validCrop() map[string]string {
	return cropFields()
}

func newMultipartCropRequest(t *testing.T, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "/artworks", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		t.Fatal(err)
	}
	return req
}

func archiveBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	mustGuardian(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			files[path] = string(raw)
		}
		return err
	}))
	return files
}

func TestCropUploadInvalidWritesNothing(t *testing.T) {
	w := newGalleryWorld(t)
	client := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	for _, tc := range []struct{ key, value string }{
		{"crop_x", "-0.1"}, {"crop_y", "1.1"}, {"crop_w", "0"}, {"crop_h", "0"}, {"crop_w", "1.1"}, {"crop_quarter_turns", "4"}, {"crop_quarter_turns", "0.5"}, {"crop_straighten_deg", "20"}, {"crop_straighten_deg", "0.25"}, {"crop_x", "NaN"}, {"crop_h", ""}, {"crop_x", ""},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			fields := cropFields()
			fields[tc.key] = tc.value
			before := archiveBytes(t, w.st.ArtistsRoot())
			status, body := postArtwork(t, client, w.ts.URL, "InvalidCrop", w.ownArtist, fields)
			if status != 200 || !strings.Contains(body, "Invalid crop.") {
				t.Fatalf("invalid response: %d %s", status, body)
			}
			after := archiveBytes(t, w.st.ArtistsRoot())
			if len(before) != len(after) {
				t.Fatal("invalid crop created files")
			}
			for path, raw := range before {
				if after[path] != raw {
					t.Fatal("invalid crop modified archive", path)
				}
			}
		})
	}
}

func TestCropUploadMissingKeyWritesNothing(t *testing.T) {
	w := newGalleryWorld(t)
	client := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	keys := []string{
		"crop_quarter_turns",
		"crop_straighten_deg",
		"crop_x",
		"crop_y",
		"crop_w",
		"crop_h",
	}
	for _, missing := range keys {
		t.Run(missing, func(t *testing.T) {
			fields := validCrop()
			delete(fields, missing)

			req := newMultipartCropRequest(t, fields)
			if crop, err := parseCrop(req); err == nil || !strings.Contains(err.Error(), "incomplete crop") {
				t.Fatalf("parseCrop with missing %s: got crop=%v err=%v; want incomplete crop", missing, crop, err)
			}

			before := archiveBytes(t, w.st.ArtistsRoot())
			status, body := postArtwork(t, client, w.ts.URL, "Missing_"+missing, w.ownArtist, fields)
			if status != 200 || !strings.Contains(body, "Invalid crop.") {
				t.Fatalf("invalid response: %d %s", status, body)
			}
			after := archiveBytes(t, w.st.ArtistsRoot())
			if len(before) != len(after) {
				t.Fatal("missing crop created files")
			}
			for path, raw := range before {
				if after[path] != raw {
					t.Fatal("missing crop modified archive", path)
				}
			}
		})
	}
	t.Run("validCrop", func(t *testing.T) {
		status, body := postArtwork(t, client, w.ts.URL, "ValidCropControl", w.ownArtist, validCrop())
		if status != 303 {
			t.Fatalf("valid crop rejected: %d %s", status, body)
		}
	})
}

func TestCropUploadAndReindex(t *testing.T) {
	w := newGalleryWorld(t)
	client := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	for _, fields := range []map[string]string{nil, cropFields()} {
		status, body := postArtwork(t, client, w.ts.URL, "CropUpload", w.ownArtist, fields)
		if status != 303 {
			t.Fatalf("%d %s", status, body)
		}
	}
	recs, err := w.st.WalkArtworks()
	mustGuardian(t, err)
	cropped := 0
	for _, rec := range recs {
		if rec.Meta.Title != "CropUpload" {
			continue
		}
		raw, err := os.ReadFile(rec.OriginalPath)
		mustGuardian(t, err)
		if !bytes.Equal(raw, tinyPNG(t)) {
			t.Fatal("upload changed original")
		}
		if rec.Meta.Crop != nil {
			cropped++
			status, body := getStatus(t, client, w.ts.URL+"/artists/"+rec.ArtistSlug+"/artworks/"+rec.Folder)
			if status != 200 || !strings.Contains(body, rec.Folder+"/display") || !strings.Contains(body, rec.Folder+"/original") {
				t.Fatal("display/original link missing")
			}
		}
	}
	if cropped != 1 {
		t.Fatal("optional crop not preserved")
	}
	before, err := w.ix.Snapshot()
	mustGuardian(t, err)
	// Delete and recreate the index, rather than merely refreshing it.
	ix := reopenIndex(t, w.srv, w.st, w.dir)
	after, err := ix.Snapshot()
	mustGuardian(t, err)
	mustGuardian(t, index.EqualSnapshots(before, after))
}

func TestCropDoesNotGrantUploadPermission(t *testing.T) {
	w := newGalleryWorld(t)
	for _, tc := range []struct{ id, artist string }{{w.viewerID, w.ownArtist}, {w.contributorID, w.otherArtist}} {
		client := memberClient(t, w.ix, w.ts.URL, tc.id)
		before := archiveBytes(t, w.st.ArtistsRoot())
		status, _ := postArtwork(t, client, w.ts.URL, "ForbiddenCrop", tc.artist, cropFields())
		if status != 404 {
			t.Fatalf("forbidden upload: %d", status)
		}
		after := archiveBytes(t, w.st.ArtistsRoot())
		if len(before) != len(after) {
			t.Fatal("forbidden crop wrote files")
		}
	}
}

func TestDisplayAccessMatchesOriginal(t *testing.T) {
	w := visibleWorld(t)
	rec, err := w.st.LoadArtwork(w.artist, w.folder)
	mustGuardian(t, err)
	rec.Meta.Crop = &imageutil.Crop{Rect: imageutil.Rect{W: .5, H: .5}}
	mustGuardian(t, writeCaptureMeta(rec))
	mustGuardian(t, w.st.RegenerateDerived(w.artist, w.folder))
	url := w.base + "/artists/" + w.artist + "/artworks/" + w.folder
	// Owner/viewer share the approved gallery; guardian has no membership in it.
	for _, tc := range []struct {
		name string
		c    *http.Client
		want int
	}{
		{"viewer", w.other, 200}, {"owner", w.owner, 200}, {"other-gallery-guardian", w.guardian, 404}, {"anonymous", testClient(t), 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := getStatus(t, tc.c, url+"/display")
			if status != tc.want {
				t.Fatalf("display: %d want %d", status, tc.want)
			}
		})
	}
	w.setHidden(t, "true")
	for _, c := range []*http.Client{w.owner, w.other, w.guardian} {
		status, _ := getStatus(t, c, url+"/display")
		if status != 404 {
			t.Fatalf("hidden display: %d", status)
		}
	}
	// A guardian who also belongs to the gallery may read the hidden derivative.
	mustGuardian(t, w.st.AddGalleryMember(w.home, w.guardianID, store.RoleViewer))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	status, _ := getStatus(t, w.guardian, url+"/display")
	if status != 200 {
		t.Fatalf("guardian member hidden display: %d", status)
	}
}

func writeCaptureMeta(rec *store.ArtworkRecord) error {
	raw, err := json.Marshal(rec.Meta)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(rec.Dir, store.ArtworkFile), raw, 0644)
}
