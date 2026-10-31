package export_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/export"
	"github.com/pserrano95/pinacoteca/internal/store"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	m := make(map[string]string)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h := sha256.Sum256(data)
			m[rel] = hex.EncodeToString(h[:])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestExportArtist(t *testing.T) {
	dataDir := t.TempDir()
	st := store.New(dataDir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	// Artist 1 with 3 artworks
	a1, err := st.CreateArtist("Ada Lovelace", time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	// Artwork 1: with tags and description, script tag in title to verify escaping
	art1, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  a1.Slug,
		Title:       "Artwork One <script>alert('xss')</script>",
		Date:        time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC),
		Description: "A lovely drawing of a cat in the garden",
		Tags:        []string{"cat", "garden", "outdoor"},
		ImageBytes:  pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Artwork 2: without tags, without description, with thumbnail
	art2, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  a1.Slug,
		Title:       "Artwork Two",
		Date:        time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
		Description: "",
		Tags:        nil,
		ImageBytes:  pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Artwork 3: without thumbnail
	art3, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  a1.Slug,
		Title:       "Artwork Three",
		Date:        time.Date(2022, 7, 1, 0, 0, 0, 0, time.UTC),
		Description: "Drawing with no thumbnail",
		Tags:        []string{"nothumb"},
		ImageBytes:  pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Remove thumbnail for art3
	if err := os.Remove(art3.ThumbPath); err != nil {
		t.Fatal(err)
	}

	// Artist 2 with 1 artwork
	a2, err := st.CreateArtist("Bob Smith", time.Date(2016, 2, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	art4, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a2.Slug,
		Title:      "Bob Artwork",
		Date:       time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Snapshot data directory before export
	beforeSnap := snapshotTree(t, dataDir)

	// 1. Export in Spanish
	outDirES := filepath.Join(t.TempDir(), "export-es")
	if err := export.Artist(st, a1.Slug, outDirES, "es"); err != nil {
		t.Fatalf("export artist in es: %v", err)
	}

	// Verify data tree is identical after export
	afterSnap := snapshotTree(t, dataDir)
	if !reflect.DeepEqual(beforeSnap, afterSnap) {
		t.Fatal("data directory modified during export")
	}

	// Verify exported folders: exactly the 3 of artist 1, none of artist 2
	entries, err := os.ReadDir(outDirES)
	if err != nil {
		t.Fatal(err)
	}
	subdirs := make(map[string]bool)
	var fileNames []string
	for _, e := range entries {
		if e.IsDir() {
			subdirs[e.Name()] = true
		} else {
			fileNames = append(fileNames, e.Name())
		}
	}

	if subdirs[art4.Folder] {
		t.Fatalf("segundo artista en exportación (%s): caso «ninguna del segundo» violado", art4.Folder)
	}
	if len(subdirs) != 3 {
		t.Fatalf("expected 3 artwork directories, got %d (%v)", len(subdirs), subdirs)
	}
	for _, expected := range []string{art1.Folder, art2.Folder, art3.Folder} {
		if !subdirs[expected] {
			t.Fatalf("expected artwork folder %q in export", expected)
		}
	}

	// Verify root files
	if _, err := os.Stat(filepath.Join(outDirES, "artist.json")); err != nil {
		t.Fatalf("artist.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDirES, "index.html")); err != nil {
		t.Fatalf("index.html missing: %v", err)
	}

	// Verify sha256 of exported originals matches --data
	for _, art := range []*store.ArtworkRecord{art1, art2, art3} {
		dataSHA := fileSHA256(t, art.OriginalPath)
		exportedPath := filepath.Join(outDirES, art.Folder, art.OriginalName)
		exportedSHA := fileSHA256(t, exportedPath)
		if dataSHA != exportedSHA {
			t.Fatalf("sha256 mismatch for %s: data %s, exported %s", art.OriginalName, dataSHA, exportedSHA)
		}

		// Check artwork.json exists
		if _, err := os.Stat(filepath.Join(outDirES, art.Folder, store.ArtworkFile)); err != nil {
			t.Fatalf("artwork.json missing in %s: %v", art.Folder, err)
		}
	}

	// Check thumb.jpg: exists in art1 and art2, absent in art3
	if _, err := os.Stat(filepath.Join(outDirES, art1.Folder, store.DerivedDir, store.ThumbName)); err != nil {
		t.Fatalf("thumb.jpg missing in %s", art1.Folder)
	}
	if _, err := os.Stat(filepath.Join(outDirES, art2.Folder, store.DerivedDir, store.ThumbName)); err != nil {
		t.Fatalf("thumb.jpg missing in %s", art2.Folder)
	}
	if _, err := os.Stat(filepath.Join(outDirES, art3.Folder, store.DerivedDir, store.ThumbName)); err == nil {
		t.Fatalf("thumb.jpg should not exist in %s", art3.Folder)
	}

	// Read and verify index.html in Spanish
	htmlBytesES, err := os.ReadFile(filepath.Join(outDirES, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	htmlES := string(htmlBytesES)

	// Titles: art1 escaped, art2 and art3 present, art4 absent
	if strings.Contains(htmlES, "<script>") {
		t.Fatalf("index.html contains unescaped <script>: %s", htmlES)
	}
	if !strings.Contains(htmlES, "&lt;script&gt;") {
		t.Fatalf("index.html does not contain escaped &lt;script&gt;: %s", htmlES)
	}
	if !strings.Contains(htmlES, "Artwork Two") {
		t.Fatal("index.html missing Artwork Two")
	}
	if !strings.Contains(htmlES, "Artwork Three") {
		t.Fatal("index.html missing Artwork Three")
	}
	if strings.Contains(htmlES, "Bob Artwork") {
		t.Fatal("index.html contains second artist's artwork title")
	}

	// Age for each artwork: 5, 6, 7
	for _, ageStr := range []string{"Edad 5", "Edad 6", "Edad 7"} {
		if !strings.Contains(htmlES, ageStr) {
			t.Fatalf("index.html missing age %s", ageStr)
		}
	}

	// Tags: cat, garden, outdoor, nothumb
	for _, tag := range []string{"cat", "garden", "outdoor", "nothumb"} {
		if !strings.Contains(htmlES, tag) {
			t.Fatalf("index.html missing tag %s", tag)
		}
	}

	// Verify all src and href are relative and point to an existing file
	uriRe := regexp.MustCompile(`(?:src|href)="([^"]+)"`)
	matches := uriRe.FindAllStringSubmatch(htmlES, -1)
	if len(matches) == 0 {
		t.Fatal("expected at least one src or href in index.html")
	}
	for _, m := range matches {
		target := m[1]
		if strings.HasPrefix(target, "/") || strings.HasPrefix(target, "http:") || strings.HasPrefix(target, "https:") {
			t.Fatalf("target %q is not relative", target)
		}
		fullPath := filepath.Join(outDirES, filepath.FromSlash(target))
		info, err := os.Stat(fullPath)
		if err != nil {
			t.Fatalf("target %q does not exist on disk: %v", target, err)
		}
		if info.IsDir() {
			t.Fatalf("target %q points to a directory, not a file", target)
		}
	}

	// 2. Export in English to compare labels
	outDirEN := filepath.Join(t.TempDir(), "export-en")
	if err := export.Artist(st, a1.Slug, outDirEN, "en"); err != nil {
		t.Fatalf("export artist in en: %v", err)
	}

	htmlBytesEN, err := os.ReadFile(filepath.Join(outDirEN, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	htmlEN := string(htmlBytesEN)

	// Spanish labels present in ES, absent in EN
	for _, label := range []string{"Fecha", "Edad", "Etiquetas", "Descripción", "Obras de"} {
		if !strings.Contains(htmlES, label) {
			t.Fatalf("index.html (es) missing label %q", label)
		}
		if strings.Contains(htmlEN, label) {
			t.Fatalf("index.html (en) should not contain Spanish label %q", label)
		}
	}

	// English labels present in EN, absent in ES
	for _, label := range []string{"Date", "Age", "Tags", "Description", "Artworks by"} {
		if !strings.Contains(htmlEN, label) {
			t.Fatalf("index.html (en) missing label %q", label)
		}
		if strings.Contains(htmlES, label) {
			t.Fatalf("index.html (es) should not contain English label %q", label)
		}
	}

	// 3. Fails without writing if --out exists and is not empty
	nonEmptyDir := t.TempDir()
	probeFile := filepath.Join(nonEmptyDir, "probe.txt")
	if err := os.WriteFile(probeFile, []byte("probe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := export.Artist(st, a1.Slug, nonEmptyDir, "en"); err == nil {
		t.Fatal("expected error when outDir is not empty, got nil")
	}
	dirEntries, err := os.ReadDir(nonEmptyDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirEntries) != 1 || dirEntries[0].Name() != "probe.txt" {
		t.Fatalf("files written despite error: %v", dirEntries)
	}

	// 4. Fails without writing if --out falls inside --data
	insideDataDir := filepath.Join(dataDir, "export-inside")
	if err := export.Artist(st, a1.Slug, insideDataDir, "en"); err == nil {
		t.Fatal("expected error when outDir is inside dataDir, got nil")
	}
	if _, err := os.Stat(insideDataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outDir inside data was created: %v", err)
	}

	// 5. Fails without writing if slug does not exist
	nonExistentOut := filepath.Join(t.TempDir(), "export-ghost")
	if err := export.Artist(st, "ghost-slug", nonExistentOut, "en"); err == nil {
		t.Fatal("expected error for nonexistent artist slug, got nil")
	}
	if _, err := os.Stat(nonExistentOut); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outDir created for nonexistent slug: %v", err)
	}
}

func TestExportArtist_InvalidSlug(t *testing.T) {
	dataDir := t.TempDir()
	st := store.New(dataDir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	ada, err := st.CreateArtist("Ada", time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	_, err = st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: ada.Slug,
		Title:      "Drawing",
		Date:       time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, invalidSlug := range []string{"ada/", "../artists/ada"} {
		outDir := filepath.Join(t.TempDir(), "export-invalid")
		err := export.Artist(st, invalidSlug, outDir, "en")
		if err == nil {
			t.Fatalf("expected error for invalid slug %q, got nil", invalidSlug)
		}
		if _, err := os.Stat(outDir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("outDir created for invalid slug %q: %v", invalidSlug, err)
		}
	}
}
