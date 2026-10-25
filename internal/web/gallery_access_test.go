package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

type galleryWorld struct {
	srv           *Server
	st            *store.Store
	ix            *index.Index
	dir           string
	ts            *httptest.Server
	ownArtist     string
	ownFolder     string
	otherArtist   string
	otherFolder   string
	viewerID      string
	contributorID string
	outsiderID    string
}

func newGalleryWorld(t *testing.T) *galleryWorld {
	t.Helper()
	srv, st, ix, dir := newTestServer(t)
	birth := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	drawn := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	own, err := st.CreateArtist("Lucía", birth)
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateArtist("Mateo", birth)
	if err != nil {
		t.Fatal(err)
	}
	ownArt, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: own.Slug,
		Title:      "FamilySun",
		Date:       drawn,
		ImageBytes: tinyPNG(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	otherArt, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: other.Slug,
		Title:      "CousinMoon",
		Date:       drawn,
		ImageBytes: tinyPNG(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	viewer := testMember(t, st, "Vera", "cred-vera")
	contributor := testMember(t, st, "Ciro", "cred-ciro")
	outsider := testMember(t, st, "Nora", "cred-nora")
	cousin := testMember(t, st, "Cora", "cred-cora")
	home, err := st.CreateGallery("Familia", "")
	if err != nil {
		t.Fatal(err)
	}
	cousins, err := st.CreateGallery("Primos", "pale-sage")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(home.Slug, own.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(cousins.Slug, other.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, viewer.ID, store.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, contributor.ID, store.RoleContributor); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(cousins.Slug, cousin.ID, store.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &galleryWorld{
		srv: srv, st: st, ix: ix, dir: dir, ts: ts,
		ownArtist: own.Slug, ownFolder: ownArt.Folder,
		otherArtist: other.Slug, otherFolder: otherArt.Folder,
		viewerID: viewer.ID, contributorID: contributor.ID, outsiderID: outsider.ID,
	}
}

func testMember(t *testing.T, st *store.Store, name, credID string) *store.Member {
	t.Helper()
	when := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	m, err := st.CreateMember(name, []store.Passkey{{
		ID: credID, PublicKey: "pk-" + credID, CreatedAt: when,
	}}, when)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func memberClient(t *testing.T, ix *index.Index, base, memberID string) *http.Client {
	t.Helper()
	c := testClient(t)
	now := time.Now().UTC().Truncate(time.Second)
	id := "session-" + memberID
	if err := ix.CreateSession(id, memberID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	c.Jar.SetCookies(u, []*http.Cookie{{Name: sessionCookie, Value: id, Path: "/"}})
	return c
}

func getStatus(t *testing.T, c *http.Client, rawURL string) (int, string) {
	t.Helper()
	res, err := c.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, readBody(t, res)
}

func TestViewerSeesOwnGalleryNotAnother(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.viewerID)
	status, body := getStatus(t, c, w.ts.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("home: %d %s", status, body)
	}
	if !strings.Contains(body, "FamilySun") || strings.Contains(body, "CousinMoon") {
		t.Fatalf("home leaked or hid the wrong works:\n%s", body)
	}
	ownPath := "/artists/" + w.ownArtist + "/artworks/" + w.ownFolder
	otherPath := "/artists/" + w.otherArtist + "/artworks/" + w.otherFolder
	if strings.Contains(body, otherPath) {
		t.Fatalf("home links the other gallery: %s", otherPath)
	}

	status, body = getStatus(t, c, w.ts.URL+ownPath)
	if status != http.StatusOK || !strings.Contains(body, "FamilySun") {
		t.Fatalf("own artwork: %d %s", status, body)
	}
	status, body = getStatus(t, c, w.ts.URL+otherPath)
	if status != http.StatusNotFound {
		t.Fatalf("other artwork: got %d, want 404\n%s", status, body)
	}
	for _, suffix := range []string{"/original", "/thumb"} {
		status, _ = getStatus(t, c, w.ts.URL+ownPath+suffix)
		if status != http.StatusOK {
			t.Fatalf("own %s: got %d, want 200", suffix, status)
		}
		status, _ = getStatus(t, c, w.ts.URL+otherPath+suffix)
		if status != http.StatusNotFound {
			t.Fatalf("other %s: got %d, want 404 (200 means the membership check in serveOriginal was removed)", suffix, status)
		}
	}
}

func TestViewerCannotUploadContributorCan(t *testing.T) {
	w := newGalleryWorld(t)
	viewer := memberClient(t, w.ix, w.ts.URL, w.viewerID)
	status, body := getStatus(t, viewer, w.ts.URL+"/artworks/new")
	if status != http.StatusNotFound {
		t.Fatalf("viewer upload form: got %d, want 404\n%s", status, body)
	}
	status, body = postArtwork(t, viewer, w.ts.URL, "Sneaky", w.ownArtist)
	if status != http.StatusNotFound {
		t.Fatalf("viewer upload: got %d, want 404\n%s", status, body)
	}
	if titleOnDisk(t, w.st, "Sneaky") {
		t.Fatal("viewer upload was stored")
	}

	contributor := memberClient(t, w.ix, w.ts.URL, w.contributorID)
	status, body = getStatus(t, contributor, w.ts.URL+"/artworks/new")
	if status != http.StatusOK || !strings.Contains(body, "Lucía") || strings.Contains(body, "Mateo") {
		t.Fatalf("contributor form: %d\n%s", status, body)
	}
	status, body = postArtwork(t, contributor, w.ts.URL, "Intruder", w.otherArtist)
	if status != http.StatusNotFound {
		t.Fatalf("contributor upload to the other gallery: got %d, want 404\n%s", status, body)
	}
	if titleOnDisk(t, w.st, "Intruder") {
		t.Fatal("upload outside the contributor's gallery was stored")
	}
	status, body = postArtwork(t, contributor, w.ts.URL, "NewBoat", w.ownArtist)
	if status != http.StatusSeeOther {
		t.Fatalf("contributor upload: got %d, want 303\n%s", status, body)
	}
	homeStatus, home := getStatus(t, contributor, w.ts.URL+"/")
	if homeStatus != http.StatusOK || !strings.Contains(home, "NewBoat") {
		t.Fatalf("contributor home after upload: %d\n%s", homeStatus, home)
	}
	viewStatus, viewHome := getStatus(t, viewer, w.ts.URL+"/")
	if viewStatus != http.StatusOK || !strings.Contains(viewHome, "NewBoat") || strings.Contains(viewHome, "CousinMoon") {
		t.Fatalf("viewer should see the new work of their gallery:\n%s", viewHome)
	}
}

func TestMemberWithoutGallerySeesEmptyHome(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.outsiderID)
	status, body := getStatus(t, c, w.ts.URL+"/")
	if status != http.StatusOK {
		t.Fatalf("home: %d %s", status, body)
	}
	if !strings.Contains(body, "No artworks yet") {
		t.Fatalf("empty home missing: %s", body)
	}
	if strings.Contains(body, "FamilySun") || strings.Contains(body, "CousinMoon") {
		t.Fatalf("member without a gallery sees works:\n%s", body)
	}
}

func TestArtistInTwoGalleriesShowsInBoth(t *testing.T) {
	srv, st, ix, _ := newTestServer(t)
	birth := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	artist, err := st.CreateArtist("Lucía", birth)
	if err != nil {
		t.Fatal(err)
	}
	art, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: artist.Slug,
		Title:      "SharedKite",
		Date:       time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC),
		ImageBytes: tinyPNG(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	ana := testMember(t, st, "Ana", "cred-ana-share")
	luis := testMember(t, st, "Luis", "cred-luis-share")
	home, err := st.CreateGallery("Familia", "")
	if err != nil {
		t.Fatal(err)
	}
	cousins, err := st.CreateGallery("Primos", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(home.Slug, artist.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(cousins.Slug, artist.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, ana.ID, store.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(cousins.Slug, luis.ID, store.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(st.ArtistsRoot())
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
		t.Fatalf("artist directories: got %d, want 1", dirs)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	for _, id := range []string{ana.ID, luis.ID} {
		c := memberClient(t, ix, ts.URL, id)
		status, body := getStatus(t, c, ts.URL+"/")
		if status != http.StatusOK || !strings.Contains(body, "SharedKite") {
			t.Fatalf("member %s: %d\n%s", id, status, body)
		}
		if !strings.Contains(body, "/artists/"+artist.Slug+"/artworks/"+art.Folder) {
			t.Fatalf("member %s home does not link the single folder", id)
		}
	}
}

func TestGalleryPermissionsSurviveReindex(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.viewerID)
	own := w.ts.URL + "/artists/" + w.ownArtist + "/artworks/" + w.ownFolder + "/original"
	other := w.ts.URL + "/artists/" + w.otherArtist + "/artworks/" + w.otherFolder + "/original"
	if status, body := getStatus(t, c, own); status != http.StatusOK {
		t.Fatalf("own original before reindex: %d %s", status, body)
	}
	if status, body := getStatus(t, c, other); status != http.StatusNotFound {
		t.Fatalf("other original before reindex: got %d, want 404\n%s", status, body)
	}
	if err := w.ix.Close(); err != nil {
		t.Fatal(err)
	}
	db := index.DBPath(w.dir)
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	for _, suf := range []string{"-wal", "-shm"} {
		_ = os.Remove(db + suf)
	}
	ix2, err := index.Open(w.dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix2.Close() })
	if err := ix2.Rebuild(w.st); err != nil {
		t.Fatal(err)
	}
	w.srv.Index = ix2
	c2 := memberClient(t, ix2, w.ts.URL, w.viewerID)
	if status, body := getStatus(t, c2, own); status != http.StatusOK {
		t.Fatalf("own original after reindex: %d %s", status, body)
	}
	if status, body := getStatus(t, c2, other); status != http.StatusNotFound {
		t.Fatalf("other original after reindex: got %d, want 404\n%s", status, body)
	}
}

func postArtwork(t *testing.T, c *http.Client, base, title, artist string) (int, string) {
	t.Helper()
	status, page := getStatus(t, c, base+"/")
	if status != http.StatusOK {
		t.Fatalf("csrf page: %d %s", status, page)
	}
	csrf := csrfFrom(t, page)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{
		"csrf":        csrf,
		"lang":        "en",
		"artist_slug": artist,
		"title":       title,
		"date":        "2024-06-01",
	} {
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
	req, err := http.NewRequest(http.MethodPost, base+"/artworks/new", &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, readBody(t, res)
}

func titleOnDisk(t *testing.T, st *store.Store, title string) bool {
	t.Helper()
	recs, err := st.WalkArtworks()
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if rec.Meta.Title == title {
			return true
		}
	}
	return false
}
