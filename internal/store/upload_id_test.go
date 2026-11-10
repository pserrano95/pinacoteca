package store_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

func uploadArtist(t *testing.T, st *store.Store, name string) *store.Artist {
	t.Helper()
	a, err := st.CreateArtist(name, time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func uploadInput(artist, title, id string, raw []byte) store.CreateArtworkInput {
	return store.CreateArtworkInput{
		ArtistSlug: artist,
		Title:      title,
		Date:       time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: raw,
		UploadID:   id,
	}
}

func artistTree(t *testing.T, root string) map[string]string {
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

func sameTree(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("archive changed: %d files then %d", len(before), len(after))
	}
	for path, raw := range before {
		if after[path] != raw {
			t.Fatalf("archive file changed: %s", path)
		}
	}
}

func TestUploadID_SameIDOneFolder(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	a := uploadArtist(t, st, "Lucía")
	id := strings.Repeat("ab", 16)
	raw := pngBytes(t)
	first, err := st.CreateArtwork(uploadInput(a.Slug, "Sun", id, raw))
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate {
		t.Fatal("first upload marked duplicate")
	}
	root := filepath.Join(st.ArtistsRoot(), a.Slug)
	before := artistTree(t, root)
	second, err := st.CreateArtwork(uploadInput(a.Slug, "Sun", id, raw))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.Folder != first.Folder {
		t.Fatalf("second upload: duplicate=%v folder=%s want %s", second.Duplicate, second.Folder, first.Folder)
	}
	sameTree(t, before, artistTree(t, root))
	got, err := os.ReadFile(first.OriginalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("original bytes changed")
	}
	if first.Meta.SchemaVersion != store.SchemaVersion || first.Meta.UploadID != id {
		t.Fatalf("meta: %+v", first.Meta)
	}
}

func TestUploadID_ConcurrentOneFolder(t *testing.T) {
	dir := t.TempDir()
	st1 := store.New(dir)
	st2 := store.New(dir)
	if err := st1.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	a := uploadArtist(t, st1, "Lucía")
	id := strings.Repeat("cd", 16)
	raw := pngBytes(t)
	const n = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	recs := make([]*store.ArtworkRecord, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			st := st1
			if i%2 == 1 {
				st = st2
			}
			recs[i], errs[i] = st.CreateArtwork(uploadInput(a.Slug, "Race", id, raw))
		}(i)
	}
	close(start)
	wg.Wait()

	created := 0
	folder := ""
	for i := 0; i < n; i++ {
		if errs[i] != nil || recs[i] == nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if folder == "" {
			folder = recs[i].Folder
		}
		if recs[i].Folder != folder {
			t.Fatalf("folder %s and %s", folder, recs[i].Folder)
		}
		if !recs[i].Duplicate {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("new writes: %d, want 1", created)
	}
	entries, err := os.ReadDir(filepath.Join(st1.ArtistsRoot(), a.Slug))
	if err != nil {
		t.Fatal(err)
	}
	dirs := 0
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		}
	}
	if dirs != 1 {
		t.Fatalf("folders on disk: %d, want 1", dirs)
	}
}

func TestUploadID_SameIDTwoArtists(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	a := uploadArtist(t, st, "Lucía")
	b := uploadArtist(t, st, "Mateo")
	id := strings.Repeat("ef", 16)
	raw := pngBytes(t)
	first, err := st.CreateArtwork(uploadInput(a.Slug, "Sun", id, raw))
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateArtwork(uploadInput(b.Slug, "Sun", id, raw))
	if err != nil {
		t.Fatal(err)
	}
	if first.Duplicate || second.Duplicate || first.Folder == "" || second.ArtistSlug == first.ArtistSlug {
		t.Fatalf("same id across artists collapsed: %+v %+v", first, second)
	}
	if first.Meta.UploadID != id || second.Meta.UploadID != id {
		t.Fatal("upload_id not stored on both")
	}
}

func TestUploadID_MissingMeansTwoArtworks(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	a := uploadArtist(t, st, "Lucía")
	raw := pngBytes(t)
	first, err := st.CreateArtwork(uploadInput(a.Slug, "Sun", "", raw))
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateArtwork(uploadInput(a.Slug, "Sun", "", raw))
	if err != nil {
		t.Fatal(err)
	}
	if first.Folder == second.Folder || first.Duplicate || second.Duplicate {
		t.Fatalf("empty id deduped: %s %s", first.Folder, second.Folder)
	}
	if first.Meta.UploadID != "" || second.Meta.UploadID != "" {
		t.Fatal("empty upload_id was stored")
	}
}

func TestUploadID_SurvivesRebuild(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	a := uploadArtist(t, st, "Lucía")
	id := strings.Repeat("a1", 16)
	rec, err := st.CreateArtwork(uploadInput(a.Slug, "Sun", id, pngBytes(t)))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rec.Dir, store.ArtworkFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(before, []byte(`"upload_id": "`+id+`"`)) || !bytes.Contains(before, []byte(`"schema_version": 1`)) {
		t.Fatalf("artwork.json missing upload_id or schema_version:\n%s", before)
	}
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ix.Close(); err != nil {
		t.Fatal(err)
	}
	dbPath := index.DBPath(dir)
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}
	for _, suf := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suf)
	}
	ix2, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ix2.Close()
	if err := ix2.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("rebuild rewrote artwork.json:\n%s", after)
	}
	loaded, err := st.LoadArtwork(rec.ArtistSlug, rec.Folder)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Meta.UploadID != id || loaded.Meta.SchemaVersion != store.SchemaVersion {
		t.Fatalf("after rebuild: %+v", loaded.Meta)
	}
}

func TestUploadID_RejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	a := uploadArtist(t, st, "Lucía")
	raw := pngBytes(t)
	for _, id := range []string{"xyz", strings.Repeat("a", 31), strings.Repeat("A", 32), strings.Repeat("a", 31) + "g"} {
		_, err := st.CreateArtwork(uploadInput(a.Slug, "Sun", id, raw))
		if !errors.Is(err, store.ErrInvalidUploadID) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(st.ArtistsRoot(), a.Slug))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Fatal("invalid upload_id created a folder")
		}
	}
}
