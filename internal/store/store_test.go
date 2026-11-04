package store_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/store"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCreateArtistEmptyGallery(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateArtist("Lucía", time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	recs, err := st.WalkArtworks()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected 0 artworks, got %d", len(recs))
	}
	if _, err := os.Stat(filepath.Join(dir, "artists", a.Slug, "artist.json")); err != nil {
		t.Fatal(err)
	}
}

func TestArtworkFullAndMinimal(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, err := st.CreateArtist("Maya", time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	full, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug:  a.Slug,
		Title:       "Sunset",
		Date:        time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		Description: "For grandma",
		Tags:        []string{"sun", "warm"},
		GiftedTo:    "Abuela",
		ImageBytes:  pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if full.Meta.GiftedTo != "Abuela" || full.Meta.Description == "" || len(full.Meta.Tags) != 2 {
		t.Fatalf("full fields not stored: %+v", full.Meta)
	}
	min, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Date:       time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if min.Meta.Title != "" || min.Meta.GiftedTo != "" {
		t.Fatalf("minimal should omit optional fields: %+v", min.Meta)
	}
}

func TestSameDaySameTitleNoClobber(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Sam", time.Date(2018, 5, 5, 0, 0, 0, 0, time.UTC))
	day := time.Date(2023, 8, 8, 0, 0, 0, 0, time.UTC)
	r1, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug, Title: "Cat", Date: day, ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug, Title: "Cat", Date: day, ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Folder == r2.Folder {
		t.Fatalf("folders collided: %s", r1.Folder)
	}
	if _, err := os.Stat(r1.OriginalPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r2.OriginalPath); err != nil {
		t.Fatal(err)
	}
}

func TestRejectNonImageLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Sam", time.Date(2018, 5, 5, 0, 0, 0, 0, time.UTC))
	before, _ := os.ReadDir(filepath.Join(dir, "artists", a.Slug))
	_, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Title:      "Nope",
		Date:       time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: []byte("not an image at all"),
	})
	if err == nil {
		t.Fatal("expected rejection")
	}
	after, _ := os.ReadDir(filepath.Join(dir, "artists", a.Slug))
	if len(after) != len(before) {
		t.Fatalf("leftover on disk: before %d after %d", len(before), len(after))
	}
}

func TestRejectBeforeBirth(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Sam", time.Date(2018, 5, 5, 0, 0, 0, 0, time.UTC))
	_, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Date:       time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	})
	if err == nil {
		t.Fatal("expected before-birth rejection")
	}
}

func TestOriginalBytesUnchanged(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	_ = st.EnsureLayout()
	a, _ := st.CreateArtist("Sam", time.Date(2018, 5, 5, 0, 0, 0, 0, time.UTC))
	raw := pngBytes(t)
	rec, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: a.Slug,
		Date:       time.Date(2022, 2, 2, 0, 0, 0, 0, time.UTC),
		ImageBytes: raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(rec.OriginalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatal("original was re-encoded")
	}
}

func TestInviteStoresOnlyTheHash(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	link, err := st.CreateInvite("Ana", "http://localhost:8080", now)
	if err != nil {
		t.Fatal(err)
	}
	token := link[strings.LastIndex(link, "/")+1:]
	entries, err := os.ReadDir(filepath.Join(dir, "invites"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("invites: %d", len(entries))
	}
	if entries[0].Name() == token || strings.Contains(entries[0].Name(), token) {
		t.Fatal("invite file is named with the token")
	}
	body, err := os.ReadFile(filepath.Join(dir, "invites", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), token) {
		t.Fatal("invite file contains the token")
	}
	if entries[0].Name() != store.InviteHash(token)+".json" {
		t.Fatalf("file %s is not the token hash", entries[0].Name())
	}
	inv, err := st.LookupInvite(token, now.Add(7*24*time.Hour-time.Second))
	if err != nil || inv.Name != "Ana" || inv.GallerySlug != "" || inv.Role != "" {
		t.Fatalf("lookup: %v %+v", err, inv)
	}
	if _, err := st.LookupInvite(token, now.Add(7*24*time.Hour)); err == nil {
		t.Fatal("invite still valid at expiry")
	}
	if err := st.MarkInviteUsed(token, now); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LookupInvite(token, now); err == nil {
		t.Fatal("used invite still valid")
	}
	if _, err := st.LookupInvite("nope", now); err == nil {
		t.Fatal("invented token accepted")
	}
}

func TestGalleryInviteStoresRoleNotToken(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	g, err := st.CreateGallery("Familia", "pale-sage")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	link, err := st.CreateGalleryInvite("Vera", g.Slug, store.RoleViewer, "http://localhost:8080", now)
	if err != nil {
		t.Fatal(err)
	}
	token := link[strings.LastIndex(link, "/")+1:]
	entries, err := os.ReadDir(filepath.Join(dir, "invites"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("invites: %d", len(entries))
	}
	body, err := os.ReadFile(filepath.Join(dir, "invites", entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), token) {
		t.Fatal("invite file contains the token")
	}
	if entries[0].Name() != store.InviteHash(token)+".json" {
		t.Fatalf("file %s is not the token hash", entries[0].Name())
	}
	inv, err := st.LookupInvite(token, now)
	if err != nil || inv.Name != "Vera" || inv.GallerySlug != g.Slug || inv.Role != store.RoleViewer {
		t.Fatalf("lookup: %v %+v", err, inv)
	}
	if _, err := st.CreateGalleryInvite("Ana", g.Slug, store.RoleOwner, "http://localhost:8080", now); err == nil {
		t.Fatal("owner role invite accepted")
	}
	if _, err := st.CreateGalleryInvite("Ana", "missing", store.RoleViewer, "http://localhost:8080", now); err == nil {
		t.Fatal("invite for a missing gallery accepted")
	}
}

func makeJPEGWithOrientation(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()

	var tiff bytes.Buffer
	tiff.WriteString("II")
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(42))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(8))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(1))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0x0112))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(3))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(1))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(orientation))
	_ = binary.Write(&tiff, binary.LittleEndian, uint16(0))
	_ = binary.Write(&tiff, binary.LittleEndian, uint32(0))

	tiffBytes := tiff.Bytes()
	payload := append([]byte("Exif\x00\x00"), tiffBytes...)
	segLen := uint16(2 + len(payload))
	var seg bytes.Buffer
	seg.WriteByte(0xff)
	seg.WriteByte(0xe1)
	_ = binary.Write(&seg, binary.BigEndian, segLen)
	seg.Write(payload)

	res := make([]byte, 0, len(raw)+seg.Len())
	res = append(res, raw[:2]...)
	res = append(res, seg.Bytes()...)
	res = append(res, raw[2:]...)
	return res
}

func TestCreateArtwork_ExifOrientationAndOriginalUnchanged(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	artist, err := st.CreateArtist("Leo", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	jpegData := makeJPEGWithOrientation(t, 40, 20, 6)
	expectedSHA := sha256.Sum256(jpegData)

	rec, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: artist.Slug,
		Title:      "Drawing",
		Date:       time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: jpegData,
	})
	if err != nil {
		t.Fatalf("CreateArtwork failed: %v", err)
	}

	// 1. Verify original on disk is identical byte-for-byte (sha256).
	diskBytes, err := os.ReadFile(rec.OriginalPath)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	diskSHA := sha256.Sum256(diskBytes)
	if diskSHA != expectedSHA {
		t.Fatalf("original on disk mutated: expected sha %x, got %x", expectedSHA, diskSHA)
	}

	// 2. Verify thumb.json records "orientation": 6.
	metaPath := filepath.Join(rec.Dir, store.DerivedDir, store.ThumbMetaName)
	metaRaw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read thumb.json: %v", err)
	}
	var thumbMeta map[string]any
	if err := json.Unmarshal(metaRaw, &thumbMeta); err != nil {
		t.Fatalf("parse thumb.json: %v", err)
	}
	orientVal, ok := thumbMeta["orientation"]
	if !ok {
		t.Fatalf("thumb.json missing 'orientation' field: %+v", thumbMeta)
	}
	if orientNum, ok := orientVal.(float64); !ok || int(orientNum) != 6 {
		t.Fatalf("thumb.json expected orientation 6, got %v", orientVal)
	}

	// 3. Verify thumbnail image is rotated (40x20 becomes 20x40 for orientation 6).
	thumbF, err := os.Open(rec.ThumbPath)
	if err != nil {
		t.Fatalf("open thumb: %v", err)
	}
	defer thumbF.Close()
	thumbImg, err := jpeg.Decode(thumbF)
	if err != nil {
		t.Fatalf("decode thumb: %v", err)
	}
	b := thumbImg.Bounds()
	if b.Dx() != 20 || b.Dy() != 40 {
		t.Fatalf("expected thumbnail 20x40, got %dx%d", b.Dx(), b.Dy())
	}

	// 4. Verify PNG gets orientation 1 in thumb.json.
	pngRec, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: artist.Slug,
		Date:       time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: pngBytes(t),
	})
	if err != nil {
		t.Fatalf("CreateArtwork with PNG failed: %v", err)
	}
	pngMetaPath := filepath.Join(pngRec.Dir, store.DerivedDir, store.ThumbMetaName)
	pngMetaRaw, err := os.ReadFile(pngMetaPath)
	if err != nil {
		t.Fatalf("read png thumb.json: %v", err)
	}
	var pngThumbMeta map[string]any
	if err := json.Unmarshal(pngMetaRaw, &pngThumbMeta); err != nil {
		t.Fatalf("parse png thumb.json: %v", err)
	}
	if v, ok := pngThumbMeta["orientation"].(float64); !ok || int(v) != 1 {
		t.Fatalf("expected orientation 1 for PNG, got %v", pngThumbMeta["orientation"])
	}
}
