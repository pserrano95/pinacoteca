package web

import (
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/imageutil"
	"github.com/pserrano95/pinacoteca/internal/store"
)

type hung struct {
	href, title, path string
}

func parseExhibits(body string) []hung {
	const marker = `<a class="exhibit" href="`
	var out []hung
	rest := body
	for {
		i := strings.Index(rest, marker)
		if i < 0 {
			break
		}
		rest = rest[i+len(marker):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			break
		}
		href := rest[:end]
		strong := strings.Index(rest, "<strong>")
		if strong < 0 {
			break
		}
		rest = rest[strong+len("<strong>"):]
		titleEnd := strings.Index(rest, "</strong>")
		if titleEnd < 0 {
			break
		}
		out = append(out, hung{href: href, title: rest[:titleEnd], path: pathOnly(href)})
	}
	return out
}

func pathOnly(ref string) string {
	if i := strings.Index(ref, "?"); i >= 0 {
		return ref[:i]
	}
	return ref
}

func taggedHref(body, class string) string {
	marker := `<a class="` + class + `"`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	const href = `href="`
	j := strings.Index(rest, href)
	if j < 0 || j > 80 {
		return ""
	}
	rest = rest[j+len(href):]
	k := strings.Index(rest, `"`)
	if k < 0 {
		return ""
	}
	return rest[:k]
}

func imgSrcContaining(body, folder string) string {
	const marker = `src="`
	rest := body
	for {
		i := strings.Index(rest, marker)
		if i < 0 {
			return ""
		}
		rest = rest[i+len(marker):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			return ""
		}
		src := rest[:end]
		if strings.Contains(src, "/"+folder+"/") {
			return src
		}
	}
}

func folderOf(viewerPath string) string {
	parts := strings.Split(strings.Trim(viewerPath, "/"), "/")
	return parts[len(parts)-1]
}

func hangExtra(t *testing.T, w *roomWorld) {
	t.Helper()
	for _, tc := range []struct {
		title string
		date  time.Time
	}{
		{"OlderSketch", time.Date(2019, 3, 2, 0, 0, 0, 0, time.UTC)},
		{"NewerSketch", time.Date(2025, 5, 5, 0, 0, 0, 0, time.UTC)},
	} {
		_, err := w.st.CreateArtwork(store.CreateArtworkInput{
			ArtistSlug: w.legacy, Title: tc.title, Date: tc.date, ImageBytes: tinyPNG(t), GiftedTo: "Grandma",
		})
		mustGuardian(t, err)
	}
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
}

func roomHangs(t *testing.T, c *http.Client, roomURL string) []hung {
	t.Helper()
	status, body := getStatus(t, c, roomURL)
	if status != http.StatusOK {
		t.Fatalf("room %s: %d", roomURL, status)
	}
	hung := parseExhibits(body)
	if len(hung) == 0 {
		t.Fatalf("room %s hung nothing", roomURL)
	}
	return hung
}

func walkNext(t *testing.T, c *http.Client, base, firstHref string) []string {
	t.Helper()
	var got []string
	seen := map[string]bool{}
	href := firstHref
	for href != "" {
		path := pathOnly(href)
		if seen[path] {
			t.Fatalf("viewer wrapped around at %s", path)
		}
		seen[path] = true
		got = append(got, path)
		status, body := getStatus(t, c, base+href)
		if status != http.StatusOK {
			t.Fatalf("viewer %s: %d", href, status)
		}
		href = taggedHref(body, "viewer-next")
	}
	return got
}

func TestViewerMemberRoles(t *testing.T) {
	w := newRoomWorld(t)
	var href string
	for _, item := range roomHangs(t, w.owner, w.room(w.home)+"?lang=en") {
		if item.title == "LegacyLandscape" {
			href = item.href
		}
	}
	if href == "" {
		t.Fatal("home room did not hang LegacyLandscape")
	}
	for _, tc := range []struct {
		name string
		c    *http.Client
	}{{"owner", w.owner}, {"contributor", w.contributor}, {"viewer", w.other}} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := getStatus(t, tc.c, w.base+href)
			if status != http.StatusOK || !strings.Contains(body, "LegacyLandscape") {
				t.Fatalf("%s viewer: status %d", tc.name, status)
			}
		})
	}
}

func TestViewerClosedToOtherGalleryAndAnonymous(t *testing.T) {
	w := newRoomWorld(t)
	outsider := testMember(t, w.st, "Only H", "viewer-only-h")
	mustGuardian(t, w.st.AddGalleryMember(w.second, outsider.ID, store.RoleViewer))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	c := memberClient(t, w.srv.Index, w.base, outsider.ID)
	href := roomHangs(t, w.owner, w.room(w.home))[0].href
	if status, _ := getStatus(t, c, w.base+href); status != http.StatusNotFound {
		t.Fatalf("member of H only, viewer of G: got %d want 404", status)
	}
	visitor := testClient(t)
	roomStatus, _ := getStatus(t, visitor, w.room(w.home))
	viewStatus, _ := getStatus(t, visitor, w.base+href)
	if viewStatus != roomStatus {
		t.Fatalf("anonymous viewer %d, room %d", viewStatus, roomStatus)
	}
}

func TestViewerApprovalDoesNotLeakAcrossGalleries(t *testing.T) {
	w := newRoomWorld(t)
	direct := "/galleries/" + w.home + "/room/" + w.artist + "/" + w.folder
	if status, _ := getStatus(t, w.owner, w.base+direct); status != http.StatusNotFound {
		t.Fatalf("approved only for H, opened in G: got %d want 404", status)
	}
	for _, item := range roomHangs(t, w.owner, w.room(w.home)+"?lang=en") {
		if strings.Contains(item.href, "/"+w.artist+"/"+w.folder) {
			t.Fatal("room G links a work approved only for H")
		}
		_, page := getStatus(t, w.owner, w.base+item.href)
		if strings.Contains(taggedHref(page, "viewer-prev")+taggedHref(page, "viewer-next"), "/"+w.artist+"/"+w.folder) {
			t.Fatalf("work approved only for H is a neighbor in G of %s", item.title)
		}
	}
	opened, page := getStatus(t, w.owner, w.base+"/galleries/"+w.second+"/room/"+w.artist+"/"+w.folder+"?lang=en")
	if opened != http.StatusOK || !strings.Contains(page, "GuardedSun") {
		t.Fatalf("same work in H: got %d", opened)
	}
	neighbor := false
	for _, item := range roomHangs(t, w.owner, w.room(w.second)+"?lang=en") {
		if strings.Contains(item.path, "/"+w.artist+"/"+w.folder) {
			continue
		}
		_, other := getStatus(t, w.owner, w.base+item.href)
		if strings.Contains(taggedHref(other, "viewer-prev")+taggedHref(other, "viewer-next"), "/"+w.artist+"/"+w.folder) {
			neighbor = true
		}
	}
	if !neighbor {
		t.Fatal("H does not offer the approved work as previous or next")
	}
	for _, item := range roomHangs(t, w.owner, w.room(w.second)+"?lang=en") {
		if item.title != "UnrelatedPortrait" {
			continue
		}
		foreign := strings.Replace(item.href, "/galleries/"+w.second+"/", "/galleries/"+w.home+"/", 1)
		if status, _ := getStatus(t, w.owner, w.base+foreign); status != http.StatusNotFound {
			t.Fatalf("work from H via G's viewer: got %d want 404", status)
		}
	}
}

func TestViewerHiddenStaysOutOfTheWalk(t *testing.T) {
	w := newRoomWorld(t)
	mustGuardian(t, w.st.ApproveArtistGallery(w.artist, w.home, w.guardianID))
	mustGuardian(t, w.st.SetArtworkHidden(w.artist, w.folder, w.guardianID, true))
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	direct := w.base + "/galleries/" + w.home + "/room/" + w.artist + "/" + w.folder
	for _, tc := range []struct {
		name string
		c    *http.Client
	}{{"owner", w.owner}, {"viewer", w.other}, {"contributor", w.contributor}} {
		t.Run(tc.name, func(t *testing.T) {
			if status, _ := getStatus(t, tc.c, direct); status != http.StatusNotFound {
				t.Fatalf("hidden viewer: got %d want 404", status)
			}
		})
	}
	for _, item := range roomHangs(t, w.owner, w.room(w.home)) {
		_, page := getStatus(t, w.owner, w.base+item.href)
		if strings.Contains(page, "/"+w.artist+"/"+w.folder) {
			t.Fatalf("hidden work is a neighbor of %s for a member", item.title)
		}
	}
	status, page := getStatus(t, w.guardian, direct+"?lang=en")
	if status != http.StatusOK || !strings.Contains(page, "GuardedSun") {
		t.Fatalf("guardian viewer: got %d", status)
	}
	step := taggedHref(page, "viewer-next")
	if step == "" {
		step = taggedHref(page, "viewer-prev")
	}
	if step == "" {
		t.Fatal("guardian cannot walk to another drawing")
	}
	status, neighbor := getStatus(t, w.guardian, w.base+step)
	if status != http.StatusOK {
		t.Fatalf("guardian step: %d", status)
	}
	back := taggedHref(neighbor, "viewer-prev") + taggedHref(neighbor, "viewer-next")
	if !strings.Contains(back, "/"+w.artist+"/"+w.folder) {
		t.Fatal("the walk does not return to the hidden drawing for its guardian")
	}
}

func TestViewerWalkMatchesRoom(t *testing.T) {
	w := newRoomWorld(t)
	hangExtra(t, w)
	status, roomBody := getStatus(t, w.owner, w.room(w.home)+"?lang=en")
	if status != http.StatusOK {
		t.Fatalf("room: %d", status)
	}
	exhibits := parseExhibits(roomBody)
	if len(exhibits) < 3 {
		t.Fatalf("room hung %d works, want at least 3", len(exhibits))
	}
	var want []string
	for _, item := range exhibits {
		if !strings.HasPrefix(item.path, "/galleries/"+w.home+"/room/") {
			t.Fatalf("exhibit leaves this room: %s", item.href)
		}
		if strings.Contains(item.href, "/artists/") {
			t.Fatalf("exhibit still opens the loose card: %s", item.href)
		}
		want = append(want, item.path)
	}
	got := walkNext(t, w.owner, w.base, exhibits[0].href)
	if !slices.Equal(got, want) {
		t.Fatalf("walk\n got %v\nwant %v", got, want)
	}
	for i, item := range exhibits {
		_, page := getStatus(t, w.owner, w.base+item.href)
		prev := pathOnly(taggedHref(page, "viewer-prev"))
		next := pathOnly(taggedHref(page, "viewer-next"))
		if i == 0 && prev != "" {
			t.Fatalf("first drawing has a previous link %s", prev)
		}
		if i == len(exhibits)-1 && next != "" {
			t.Fatalf("last drawing has a next link %s", next)
		}
		if i > 0 && prev != exhibits[i-1].path {
			t.Fatalf("%s previous %s, want %s", item.title, prev, exhibits[i-1].path)
		}
		if i < len(exhibits)-1 && next != exhibits[i+1].path {
			t.Fatalf("%s next %s, want %s", item.title, next, exhibits[i+1].path)
		}
		folder := folderOf(item.path)
		roomSrc := imgSrcContaining(roomBody, folder)
		viewSrc := imgSrcContaining(page, folder)
		if roomSrc == "" || roomSrc != viewSrc {
			t.Fatalf("%s picture: room %q viewer %q", item.title, roomSrc, viewSrc)
		}
	}
}

func TestViewerSingleWorkHasNoNeighbors(t *testing.T) {
	w := newRoomWorld(t)
	solo, err := w.st.CreateGallery("Solo", "dusty-blue")
	mustGuardian(t, err)
	mustGuardian(t, w.st.AddGalleryMember(solo.Slug, w.ownerID, store.RoleOwner))
	birth := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	kid, err := w.st.CreateArtist("Solo Kid", birth)
	mustGuardian(t, err)
	mustGuardian(t, w.st.AddGalleryArtist(solo.Slug, kid.Slug))
	rec, err := w.st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: kid.Slug, Title: "OnlyDrawing", Date: birth.AddDate(4, 0, 0), ImageBytes: tinyPNG(t),
	})
	mustGuardian(t, err)
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	status, body := getStatus(t, w.owner, w.base+"/galleries/"+solo.Slug+"/room/"+rec.ArtistSlug+"/"+rec.Folder+"?lang=en")
	if status != http.StatusOK {
		t.Fatalf("single viewer: %d", status)
	}
	if strings.Contains(body, `class="viewer-prev"`) || strings.Contains(body, `class="viewer-next"`) {
		t.Fatal("a one-drawing room invented a neighbor")
	}
	if !strings.Contains(body, `class="viewer-back" href="/galleries/`+solo.Slug+`/room?lang=en"`) {
		t.Fatalf("missing back link:\n%s", body)
	}
	if !strings.Contains(body, `data-wall="dusty-blue"`) {
		t.Fatal("viewer lost the room wall")
	}
}

func TestViewerMissingWorkIs404(t *testing.T) {
	w := newRoomWorld(t)
	for _, tc := range []struct{ name, path string }{
		{"folder", "/galleries/" + w.home + "/room/" + w.legacy + "/no-such-folder"},
		{"artist", "/galleries/" + w.home + "/room/no-such-artist/2020-01-01-nope"},
		{"gallery", "/galleries/no-such-gallery/room/" + w.legacy + "/no-such-folder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := getStatus(t, w.owner, w.base+tc.path)
			if status != http.StatusNotFound {
				t.Fatalf("got %d, want 404", status)
			}
		})
	}
}

func TestViewerLabelsInBothLanguages(t *testing.T) {
	w := newRoomWorld(t)
	hangExtra(t, w)
	exhibits := roomHangs(t, w.owner, w.room(w.home)+"?lang=en")
	middle := exhibits[1]
	for _, tc := range []struct{ lang, prev, next, back, gifted, age string }{
		{"en", "Previous", "Next", "Back to the room", "Gifted to", "Age 6"},
		{"es", "Anterior", "Siguiente", "Volver a la sala", "Regalado a", "Edad 6"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			status, body := getStatus(t, w.owner, w.base+middle.path+"?lang="+tc.lang)
			if status != http.StatusOK {
				t.Fatalf("status %d", status)
			}
			for _, text := range []string{tc.prev, tc.next, tc.back, tc.gifted, tc.age, "Legacy", "LegacyLandscape", "Grandma"} {
				if !strings.Contains(body, text) {
					t.Fatalf("missing %q", text)
				}
			}
			if strings.Contains(body, "viewer_previous") || strings.Contains(body, "viewer_back") {
				t.Fatal("a translation key was shown as text")
			}
		})
	}
}

func TestViewerScriptIsOnlyOnTheViewer(t *testing.T) {
	w := newRoomWorld(t)
	hangExtra(t, w)
	exhibits := roomHangs(t, w.owner, w.room(w.home)+"?lang=en")
	item := exhibits[1]
	_, view := getStatus(t, w.owner, w.base+item.href)
	if !strings.Contains(view, `<script src="/static/viewer.js"`) {
		t.Fatal("viewer does not load viewer.js")
	}
	for _, class := range []string{"viewer-prev", "viewer-next", "viewer-back"} {
		if !strings.Contains(view, `class="`+class+`"`) {
			t.Fatalf("%s is missing from the HTML, so it depends on the script", class)
		}
	}
	parts := strings.Split(strings.Trim(item.path, "/"), "/")
	card := "/artists/" + parts[3] + "/artworks/" + parts[4]
	for _, tc := range []struct{ name, url string }{
		{"room", w.room(w.home)},
		{"home", w.base + "/"},
		{"card", w.base + card},
	} {
		_, body := getStatus(t, w.owner, tc.url)
		if strings.Contains(body, "/static/viewer.js") {
			t.Fatalf("%s loads viewer.js", tc.name)
		}
	}
	status, script := getStatus(t, testClient(t), w.base+"/static/viewer.js")
	if status != http.StatusOK || !strings.Contains(script, "pinchScale") {
		t.Fatalf("viewer.js: status %d", status)
	}
	if status, _ := getStatus(t, w.owner, w.base+card); status != http.StatusOK {
		t.Fatalf("loose card: got %d want 200", status)
	}
}

func TestViewerNeighborsAfterReindex(t *testing.T) {
	w := newRoomWorld(t)
	hangExtra(t, w)
	before := walkNext(t, w.owner, w.base, roomHangs(t, w.owner, w.room(w.home))[0].href)
	cmd := exec.Command("go", "run", "../../cmd/pinacoteca", "reindex", "--data", w.dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("reindex: %v\n%s", err, output)
	}
	t.Logf("pinacoteca reindex: %s", output)
	c := memberClient(t, w.srv.Index, w.base, w.ownerID)
	after := walkNext(t, c, w.base, roomHangs(t, c, w.room(w.home))[0].href)
	if !slices.Equal(before, after) {
		t.Fatalf("neighbors after reindex\n before %v\n after %v", before, after)
	}
}

func TestViewerUsesTheHungPicture(t *testing.T) {
	w := newRoomWorld(t)
	rec, err := w.st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: w.legacy, Title: "CroppedLandscape",
		Date: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), ImageBytes: tinyPNG(t),
		Crop: &imageutil.Crop{QuarterTurns: 1, Rect: imageutil.Rect{W: .5, H: .5}},
	})
	mustGuardian(t, err)
	mustGuardian(t, w.srv.Index.RebuildKeepingSessions(w.st))
	display := "/artists/" + rec.ArtistSlug + "/artworks/" + rec.Folder + "/display"
	original := "/artists/" + rec.ArtistSlug + "/artworks/" + rec.Folder + "/original"
	var href string
	_, roomBody := getStatus(t, w.owner, w.room(w.home))
	for _, item := range parseExhibits(roomBody) {
		if item.title == "CroppedLandscape" {
			href = item.href
		}
	}
	if href == "" || !strings.Contains(roomBody, `src="`+display+`"`) {
		t.Fatal("room did not hang the cropped display")
	}
	status, body := getStatus(t, w.owner, w.base+href)
	if status != http.StatusOK || !strings.Contains(body, `src="`+display+`"`) || strings.Contains(body, `src="`+original+`"`) {
		t.Fatalf("viewer picture: status %d, display present %v", status, strings.Contains(body, display))
	}
}
