package web

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
)

type guardianWorld struct {
	srv                                     *Server
	st                                      *store.Store
	base, dir, artist, folder, home, second string
	guardian, owner, other                  *http.Client
	ownerID, guardianID, otherID            string
}

func newGuardianWorld(t *testing.T) *guardianWorld {
	t.Helper()
	srv, st, ix, dir := newTestServer(t)
	parent := testMember(t, st, "Parent", "guardian-parent")
	owner := testMember(t, st, "Owner", "guardian-owner")
	other := testMember(t, st, "Viewer", "guardian-viewer")
	a, err := st.CreateArtistWithGuardian("Ada", time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	art, err := st.CreateArtwork(store.CreateArtworkInput{ArtistSlug: a.Slug, Title: "GuardedSun", Date: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), ImageBytes: tinyPNG(t)})
	if err != nil {
		t.Fatal(err)
	}
	home, err := st.CreateGallery("Home", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateGallery("Second", "")
	if err != nil {
		t.Fatal(err)
	}
	// The owner already knows the artist through one gallery. Adding another
	// must remain possible without weakening the unrelated-artist IDOR boundary.
	mustGuardian(t, st.AddGalleryArtist(home.Slug, a.Slug))
	mustGuardian(t, st.AddGalleryMember(home.Slug, owner.ID, store.RoleOwner))
	mustGuardian(t, st.AddGalleryMember(home.Slug, other.ID, store.RoleViewer))
	mustGuardian(t, st.AddGalleryMember(second.Slug, owner.ID, store.RoleOwner))
	mustGuardian(t, st.AddGalleryMember(second.Slug, parent.ID, store.RoleOwner))
	mustGuardian(t, ix.Rebuild(st))
	ts := serve(t, srv)
	return &guardianWorld{srv: srv, st: st, base: ts.URL, dir: dir, artist: a.Slug, folder: art.Folder, home: home.Slug, second: second.Slug,
		guardian: memberClient(t, ix, ts.URL, parent.ID), owner: memberClient(t, ix, ts.URL, owner.ID), other: memberClient(t, ix, ts.URL, other.ID), ownerID: owner.ID, guardianID: parent.ID, otherID: other.ID}
}

func mustGuardian(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func (w *guardianWorld) page() string            { return w.base + "/artists/" + w.artist + "/galleries" }
func (w *guardianWorld) approve(g string) string { return w.page() + "/" + g + "/approve" }
func (w *guardianWorld) artistFile() string {
	return filepath.Join(w.st.ArtistsRoot(), w.artist, store.ArtistFile)
}
func (w *guardianWorld) assertVisible(t *testing.T, c *http.Client, want bool) {
	t.Helper()
	status, body := getStatus(t, c, w.base+"/")
	if status != http.StatusOK || strings.Contains(body, "GuardedSun") != want {
		t.Fatalf("gallery visibility: status %d, artwork present %v, want %v", status, strings.Contains(body, "GuardedSun"), want)
	}
	for _, suffix := range []string{"", "/original", "/thumb"} {
		status, _ := getStatus(t, c, w.base+"/artists/"+w.artist+"/artworks/"+w.folder+suffix)
		expected := http.StatusNotFound
		if want {
			expected = http.StatusOK
		}
		if status != expected {
			t.Fatalf("artwork%s: got %d want %d", suffix, status, expected)
		}
	}
}

func TestNonGuardianOwnerAdditionPendingUntilGuardianApproves(t *testing.T) {
	w := newGuardianWorld(t)
	status, _, body := postForm(t, w.owner, w.base+"/galleries/"+w.second+"/artists", csrfOf(t, w.owner, w.base+"/"), map[string]string{"artist_slug": w.artist})
	if status != http.StatusSeeOther {
		t.Fatalf("owner addition: %d %s", status, body)
	}
	g, err := w.st.LoadGallery(w.second)
	mustGuardian(t, err)
	if !slices.Contains(g.Artists, w.artist) {
		t.Fatal("pending request lost from gallery.json")
	}
	w.assertVisible(t, w.owner, false)
	if can, err := w.srv.Index.CanUploadArtist(w.ownerID, w.artist); err != nil || can {
		t.Fatalf("pending upload permission: %v %v", can, err)
	}
	status, body = getStatus(t, w.guardian, w.page())
	if status != http.StatusOK || !strings.Contains(body, "Home") || !strings.Contains(body, "Second") || !strings.Contains(body, "/approve") {
		t.Fatalf("guardian pending page: %d %s", status, body)
	}
	status, _, body = postForm(t, w.guardian, w.approve(w.second), csrfFrom(t, body), nil)
	if status != http.StatusSeeOther {
		t.Fatalf("guardian approval: %d %s", status, body)
	}
	w.assertVisible(t, w.owner, true)
	a, err := w.st.LoadArtist(w.artist)
	mustGuardian(t, err)
	if !slices.Contains(a.ApprovedGalleries, w.second) || slices.Contains(a.ApprovedGalleries, w.home) {
		t.Fatalf("approval on disk: %+v", a)
	}
}

func TestGuardianAdditionApprovesImmediately(t *testing.T) {
	w := newGuardianWorld(t)
	status, _, body := postForm(t, w.guardian, w.base+"/galleries/"+w.second+"/artists", csrfOf(t, w.guardian, w.base+"/"), map[string]string{"artist_slug": w.artist})
	if status != http.StatusSeeOther {
		t.Fatalf("guardian addition: %d %s", status, body)
	}
	a, err := w.st.LoadArtist(w.artist)
	mustGuardian(t, err)
	if !slices.Contains(a.ApprovedGalleries, w.second) {
		t.Fatal("guardian addition not approved")
	}
	w.assertVisible(t, w.guardian, true)
}

func TestApprovalPOSTRejectsNonGuardianOwnerMemberAndVisitorWithoutWrites(t *testing.T) {
	w := newGuardianWorld(t)
	for _, tc := range []struct {
		name string
		c    *http.Client
		want int
	}{{"owner", w.owner, 403}, {"other_member", w.other, 403}, {"visitor", testClient(t), 303}} {
		t.Run(tc.name, func(t *testing.T) {
			before, err := os.ReadFile(w.artistFile())
			mustGuardian(t, err)
			csrfURL := w.base + "/"
			if tc.name == "visitor" {
				csrfURL = w.base + "/login"
			}
			csrf := csrfOf(t, tc.c, csrfURL)
			status, _, body := postForm(t, tc.c, w.approve(w.home), csrf, nil)
			if status != tc.want {
				t.Fatalf("unauthorized approval: got %d want %d %s", status, tc.want, body)
			}
			after, err := os.ReadFile(w.artistFile())
			mustGuardian(t, err)
			if !bytes.Equal(before, after) {
				t.Fatal("unauthorized POST wrote artist.json")
			}
		})
	}
	for _, c := range []*http.Client{w.owner, w.other} {
		status, _ := getStatus(t, c, w.page())
		if status != 403 {
			t.Fatalf("guardian page accessible: %d", status)
		}
		_, body := getStatus(t, c, w.base+"/")
		if strings.Contains(body, w.page()[len(w.base):]) {
			t.Fatal("guardian navigation exposed to non-guardian")
		}
	}
}

func TestGuardianApprovalRequiresCSRFAndRequestedGallery(t *testing.T) {
	w := newGuardianWorld(t)
	before, err := os.ReadFile(w.artistFile())
	mustGuardian(t, err)
	status, _, _ := postForm(t, w.guardian, w.approve(w.home), "", nil)
	if status != 403 {
		t.Fatalf("missing CSRF: %d", status)
	}
	// Second exists but has not requested this artist.
	status, _, _ = postForm(t, w.guardian, w.approve(w.second), csrfOf(t, w.guardian, w.page()), nil)
	if status != 403 {
		t.Fatalf("unrequested gallery: %d", status)
	}
	after, err := os.ReadFile(w.artistFile())
	mustGuardian(t, err)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid approval wrote artist.json")
	}
}

func TestTwoGalleriesOnlyApprovedOneVisibleAfterDeletingIndex(t *testing.T) {
	w := newGuardianWorld(t)
	mustGuardian(t, w.st.AddGalleryArtist(w.second, w.artist))
	mustGuardian(t, w.st.ApproveArtistGallery(w.artist, w.second, w.guardianID))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	w.assertVisible(t, w.guardian, true)
	w.assertVisible(t, w.other, false) // Only in the pending Home gallery.
	before, err := w.srv.Index.Snapshot()
	mustGuardian(t, err)
	ix := reopenIndex(t, w.srv, w.st, w.dir)
	after, err := ix.Snapshot()
	mustGuardian(t, err)
	mustGuardian(t, index.EqualSnapshots(before, after))
	w.guardian = memberClient(t, ix, w.base, w.guardianID)
	w.other = memberClient(t, ix, w.base, w.otherID)
	w.assertVisible(t, w.guardian, true)
	w.assertVisible(t, w.other, false)
	status, body := getStatus(t, w.guardian, w.page())
	if status != 200 || !strings.Contains(body, "Approved galleries") || !strings.Contains(body, "Home") || !strings.Contains(body, "Second") {
		t.Fatalf("rebuilt guardian page: %d %s", status, body)
	}
}

func TestArtistWithoutGuardiansRetainsGalleryVisibility(t *testing.T) {
	w := newGalleryWorld(t)
	c := memberClient(t, w.ix, w.ts.URL, w.viewerID)
	status, body := getStatus(t, c, w.ts.URL+"/")
	if status != 200 || !strings.Contains(body, "FamilySun") {
		t.Fatalf("legacy visibility: %d", status)
	}
	status, _ = getStatus(t, c, w.ts.URL+"/artists/"+w.ownArtist+"/artworks/"+w.ownFolder+"/original")
	if status != 200 {
		t.Fatalf("legacy original: %d", status)
	}
}

func TestWebCreationAssignsProvisionalGuardianAndApprovesGallery(t *testing.T) {
	w := newGuardianWorld(t)
	status, _, body := postForm(t, w.owner, w.base+"/artists/new", csrfOf(t, w.owner, w.base+"/artists/new"), map[string]string{"name": "NewChild", "birth_date": "2019-01-01", "gallery": w.home})
	if status != 303 {
		t.Fatalf("web creation: %d %s", status, body)
	}
	a, err := w.st.LoadArtist("newchild")
	mustGuardian(t, err)
	if !a.IsGuardian(w.ownerID) || !slices.Contains(a.ApprovedGalleries, w.home) {
		t.Fatalf("web creator permissions: %+v", a)
	}
	for _, lang := range []string{"en", "es"} {
		status, body := getStatus(t, w.owner, w.base+"/artists/newchild/galleries?lang="+lang)
		expected := "Approved galleries"
		if lang == "es" {
			expected = "Galerías aprobadas"
		}
		if status != 200 || !strings.Contains(body, expected) {
			t.Fatalf("locale %s: %d %s", lang, status, body)
		}
	}
}

func TestGuardianWithoutGalleryMembershipCanApproveFromHomeLink(t *testing.T) {
	w := newGuardianWorld(t)
	parent := testMember(t, w.st, "Independent parent", "independent-guardian")
	mustGuardian(t, w.st.AddArtistGuardian(w.artist, parent.ID))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	c := memberClient(t, w.srv.Index, w.base, parent.ID)
	status, body := getStatus(t, c, w.base+"/")
	if status != 200 || !strings.Contains(body, "/artists/"+w.artist+"/galleries") {
		t.Fatalf("guardian without memberships cannot find artist: %d", status)
	}
	status, body = getStatus(t, c, w.page())
	if status != 200 {
		t.Fatalf("guardian page: %d", status)
	}
	status, _, body = postForm(t, c, w.approve(w.home), csrfFrom(t, body), nil)
	if status != 303 {
		t.Fatalf("guardian without membership approval: %d %s", status, body)
	}
	w.assertVisible(t, w.other, true)
	w.assertVisible(t, c, false)
}
