package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/store"
)

type roomWorld struct {
	*guardianWorld
	contributor       *http.Client
	legacy, unrelated string
}

func newRoomWorld(t *testing.T) *roomWorld {
	t.Helper()
	w := newGuardianWorld(t)
	mustGuardian(t, w.st.AddGalleryArtist(w.second, w.artist))
	mustGuardian(t, w.st.ApproveArtistGallery(w.artist, w.second, w.guardianID))
	mustGuardian(t, w.st.AddGalleryMember(w.home, w.guardianID, store.RoleViewer))
	contributor := testMember(t, w.st, "Contributor", "room-contributor")
	mustGuardian(t, w.st.AddGalleryMember(w.home, contributor.ID, store.RoleContributor))
	birth := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	legacy, err := w.st.CreateArtist("Legacy", birth)
	mustGuardian(t, err)
	unrelated, err := w.st.CreateArtist("Unrelated", birth)
	mustGuardian(t, err)
	for _, tc := range []struct{ artist, gallery, title string }{{legacy.Slug, w.home, "LegacyLandscape"}, {unrelated.Slug, w.second, "UnrelatedPortrait"}} {
		mustGuardian(t, w.st.AddGalleryArtist(tc.gallery, tc.artist))
		_, err := w.st.CreateArtwork(store.CreateArtworkInput{ArtistSlug: tc.artist, Title: tc.title, Date: birth.AddDate(6, 0, 0), ImageBytes: tinyPNG(t), GiftedTo: "Grandma"})
		mustGuardian(t, err)
	}
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	return &roomWorld{guardianWorld: w, contributor: memberClient(t, w.srv.Index, w.base, contributor.ID), legacy: legacy.Slug, unrelated: unrelated.Slug}
}
func (w *roomWorld) room(g string) string { return w.base + "/galleries/" + g + "/room" }
func assertRoomArt(t *testing.T, c *http.Client, url, title string, want bool) {
	t.Helper()
	status, body := getStatus(t, c, url)
	if status != 200 || strings.Contains(body, title) != want {
		t.Fatalf("room %s: status=%d, %s present=%v, want=%v", url, status, title, strings.Contains(body, title), want)
	}
}
func TestRoomMemberRoles(t *testing.T) {
	w := newRoomWorld(t)
	for _, tc := range []struct {
		name string
		c    *http.Client
	}{{"owner", w.owner}, {"contributor", w.contributor}, {"viewer", w.other}} {
		t.Run(tc.name, func(t *testing.T) { assertRoomArt(t, tc.c, w.room(w.home), "LegacyLandscape", true) })
	}
}
func TestRoomMembershipBoundary(t *testing.T) {
	w := newRoomWorld(t)
	// Parent is a member of both rooms; this member belongs only to H.
	outsider := testMember(t, w.st, "Only H", "room-only-h")
	mustGuardian(t, w.st.AddGalleryMember(w.second, outsider.ID, store.RoleViewer))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	c := memberClient(t, w.srv.Index, w.base, outsider.ID)
	for _, g := range []string{w.home, "missing"} {
		status, _ := getStatus(t, c, w.room(g))
		if status != 404 {
			t.Fatalf("nonmember room %s: got %d want 404", g, status)
		}
	}
	visitor := testClient(t)
	home, _ := getStatus(t, visitor, w.base+"/")
	room, _ := getStatus(t, visitor, w.room(w.home))
	if home != 303 || room != home {
		t.Fatalf("visitor: home=%d room=%d", home, room)
	}
}
func TestRoomApprovalIsPerGallery(t *testing.T) {
	w := newRoomWorld(t)
	assertRoomArt(t, w.owner, w.room(w.home), "GuardedSun", false)
	assertRoomArt(t, w.owner, w.room(w.second), "GuardedSun", true)
}
func TestRoomExcludesArtistsFromOtherMemberships(t *testing.T) {
	w := newRoomWorld(t)
	assertRoomArt(t, w.owner, w.room(w.home), "UnrelatedPortrait", false)
	assertRoomArt(t, w.owner, w.room(w.second), "UnrelatedPortrait", true)
}
func TestRoomHiddenOnlyForGuardian(t *testing.T) {
	w := newRoomWorld(t)
	mustGuardian(t, w.st.ApproveArtistGallery(w.artist, w.home, w.guardianID))
	mustGuardian(t, w.st.SetArtworkHidden(w.artist, w.folder, w.guardianID, true))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	for _, tc := range []struct {
		name string
		c    *http.Client
		want bool
	}{{"owner", w.owner, false}, {"viewer", w.other, false}, {"contributor", w.contributor, false}, {"guardian", w.guardian, true}} {
		t.Run(tc.name, func(t *testing.T) { assertRoomArt(t, tc.c, w.room(w.home), "GuardedSun", tc.want) })
	}
}
func TestRoomWallColoursAndInvalidArchiveFallback(t *testing.T) {
	w := newRoomWorld(t)
	file := filepath.Join(w.st.GalleriesRoot(), w.home, store.GalleryFile)
	original, err := os.ReadFile(file)
	mustGuardian(t, err)
	for _, colour := range append(append([]string{}, store.WallColours...), "invalid") {
		t.Run(colour, func(t *testing.T) {
			// Write the archive directly: a future/unknown colour must not break reads.
			mustGuardian(t, os.WriteFile(file, []byte(strings.Replace(string(original), `"off-white"`, `"`+colour+`"`, 1)), 0600))
			mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
			want := colour
			if colour == "invalid" {
				want = "off-white"
			}
			status, body := getStatus(t, w.owner, w.room(w.home))
			if status != 200 || !strings.Contains(body, `data-wall="`+want+`"`) {
				t.Fatalf("colour %s: status %d, wall missing", colour, status)
			}
			after, err := os.ReadFile(file)
			mustGuardian(t, err)
			expected := strings.Replace(string(original), `"off-white"`, `"`+colour+`"`, 1)
			if string(after) != expected {
				t.Fatal("reading the room rewrote gallery.json")
			}

		})
	}
}
func TestRoomEmptyBilingualAndHomeLinks(t *testing.T) {
	w := newRoomWorld(t)
	empty, err := w.st.CreateGallery("Empty room", "")
	mustGuardian(t, err)
	mustGuardian(t, w.st.AddGalleryMember(empty.Slug, w.otherID, store.RoleViewer))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	for _, tc := range []struct{ lang, text string }{{"en", "No artworks are on display in this room yet."}, {"es", "Todavía no hay obras expuestas en esta sala."}} {
		t.Run(tc.lang, func(t *testing.T) {
			status, body := getStatus(t, w.other, w.room(empty.Slug)+"?lang="+tc.lang)
			if status != 200 || !strings.Contains(body, tc.text) {
				t.Fatalf("empty room %s: %d %s", tc.lang, status, body)
			}
			_, body = getStatus(t, w.other, w.base+"/?lang="+tc.lang)
			for _, g := range []string{w.home, empty.Slug} {
				if !strings.Contains(body, `href="/galleries/`+g+`/room?lang=`+tc.lang+`"`) {
					t.Fatalf("missing member room link %s", g)
				}
			}
			if strings.Contains(body, `href="/galleries/`+w.second+`/room`) {
				t.Fatal("home links an unrelated gallery")
			}
		})
	}
}
func TestRoomAfterCLIReindex(t *testing.T) {
	w := newRoomWorld(t)
	_, before := getStatus(t, w.owner, w.room(w.home))
	// Reindex intentionally closes sessions; authenticate again afterward.
	cmd := exec.Command("go", "run", "../../cmd/pinacoteca", "reindex", "--data", w.dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("reindex: %v\n%s", err, output)
	}
	t.Logf("pinacoteca reindex: %s", output)
	c := memberClient(t, w.srv.Index, w.base, w.ownerID)
	_, after := getStatus(t, c, w.room(w.home))
	// CSRF is per client; compare the exhibition rather than the session header.
	before = strings.Split(before, `<main class="gallery-room"`)[1]
	after = strings.Split(after, `<main class="gallery-room"`)[1]
	if before != after {
		t.Fatal("room changed after CLI reindex")
	}
}
