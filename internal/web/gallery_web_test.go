package web

import (
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

func TestOwnerCreatesPaleSageGalleryAndAddsArtist(t *testing.T) {
	srv, st, ix, dir := newTestServer(t)
	ts := serve(t, srv)
	birth := time.Date(2018, 3, 3, 0, 0, 0, 0, time.UTC)
	owner := testMember(t, st, "Ana", "cred-ana-create")
	contributor := testMember(t, st, "Ciro", "cred-ciro-create")
	viewer := testMember(t, st, "Vera", "cred-vera-create")
	existing, err := st.CreateArtist("Ines", birth)
	if err != nil {
		t.Fatal(err)
	}
	home, err := st.CreateGallery("Already", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, owner.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, contributor.ID, store.RoleContributor); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, viewer.ID, store.RoleViewer); err != nil {
		t.Fatal(err)
	}
	// Ines already hangs in a gallery Ana owns; the manage list only offers those.
	if err := st.AddGalleryArtist(home.Slug, existing.Slug); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}

	ownerC := memberClient(t, ix, ts.URL, owner.ID)
	es, esBody := getStatus(t, ownerC, ts.URL+"/galleries/new?lang=es")
	if es != http.StatusOK || !strings.Contains(esBody, "Nueva galería") || !strings.Contains(esBody, "Salvia pálida") {
		t.Fatalf("spanish gallery form: %d\n%s", es, esBody)
	}
	csrf := csrfFrom(t, esBody)
	status, loc, body := postForm(t, ownerC, ts.URL+"/galleries/new", csrf, map[string]string{
		"name":        "Sage Room",
		"wall_colour": "pale-sage",
	})
	if status != http.StatusSeeOther {
		t.Fatalf("create gallery: got %d, want 303\n%s", status, body)
	}
	slug := gallerySlugFromLocation(t, loc)
	g, err := st.LoadGallery(slug)
	if err != nil {
		t.Fatal(err)
	}
	if g.WallColour != "pale-sage" {
		t.Fatalf("wall colour: %s", g.WallColour)
	}
	if len(g.Members) != 1 || g.Members[0].ID != owner.ID || g.Members[0].Role != store.RoleOwner {
		t.Fatalf("creator is not the only owner: %+v", g.Members)
	}

	manage, manageBody := getStatus(t, ownerC, ts.URL+"/galleries/"+slug)
	if manage != http.StatusOK || !strings.Contains(manageBody, "Ines") || !strings.Contains(manageBody, "Pale sage") {
		t.Fatalf("manage page: %d\n%s", manage, manageBody)
	}
	status, _, body = postForm(t, ownerC, ts.URL+"/galleries/"+slug+"/artists", csrfFrom(t, manageBody), map[string]string{
		"artist_slug": existing.Slug,
	})
	if status != http.StatusSeeOther {
		t.Fatalf("add artist: got %d, want 303\n%s", status, body)
	}
	g, err = st.LoadGallery(slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Artists) != 1 || g.Artists[0] != existing.Slug {
		t.Fatalf("artists after add: %+v", g.Artists)
	}

	form, formBody := getStatus(t, ownerC, ts.URL+"/artists/new?gallery="+slug)
	if form != http.StatusOK || !strings.Contains(formBody, "Sage Room") {
		t.Fatalf("new artist form: %d\n%s", form, formBody)
	}
	status, _, body = postForm(t, ownerC, ts.URL+"/artists/new", csrfFrom(t, formBody), map[string]string{
		"gallery":    slug,
		"name":       "Nico",
		"birth_date": "2019-04-04",
	})
	if status != http.StatusSeeOther {
		t.Fatalf("create artist: got %d, want 303\n%s", status, body)
	}
	g, err = st.LoadGallery(slug)
	if err != nil {
		t.Fatal(err)
	}
	if !galleryHasArtistNamed(t, st, g, "Nico") {
		t.Fatalf("new artist missing from gallery.json: %+v", g.Artists)
	}

	for _, who := range []struct {
		name string
		id   string
	}{
		{"contributor", contributor.ID},
		{"viewer", viewer.ID},
	} {
		c := memberClient(t, ix, ts.URL, who.id)
		token := csrfOf(t, c, ts.URL+"/")
		if status, body := getStatus(t, c, ts.URL+"/galleries/new"); status != http.StatusNotFound {
			t.Fatalf("%s GET new gallery: got %d, want 404\n%s", who.name, status, body)
		}
		status, _, body = postForm(t, c, ts.URL+"/galleries/new", token, map[string]string{
			"name":        "Sneaky",
			"wall_colour": "pale-sage",
		})
		if status != http.StatusNotFound {
			t.Fatalf("%s create gallery: got %d, want 404\n%s", who.name, status, body)
		}
		if status, body := getStatus(t, c, ts.URL+"/galleries/"+slug); status != http.StatusNotFound {
			t.Fatalf("%s GET gallery: got %d, want 404\n%s", who.name, status, body)
		}
		status, _, body = postForm(t, c, ts.URL+"/galleries/"+slug+"/artists", token, map[string]string{
			"artist_slug": existing.Slug,
		})
		if status != http.StatusNotFound {
			t.Fatalf("%s add artist: got %d, want 404\n%s", who.name, status, body)
		}
	}
	if sneaky, err := st.LoadGallery("sneaky"); err == nil {
		t.Fatalf("non-owner created a gallery: %+v", sneaky)
	}

	ix2 := reopenIndex(t, srv, st, dir)
	again := memberClient(t, ix2, ts.URL, owner.ID)
	if status, body := getStatus(t, again, ts.URL+"/galleries/"+slug); status != http.StatusOK || !strings.Contains(body, "Sage Room") || !strings.Contains(body, "Pale sage") {
		t.Fatalf("manage after reindex: %d\n%s", status, body)
	}
	g, err = st.LoadGallery(slug)
	if err != nil {
		t.Fatal(err)
	}
	if g.WallColour != "pale-sage" || len(g.Members) != 1 || g.Members[0].Role != store.RoleOwner || len(g.Artists) != 2 {
		t.Fatalf("gallery.json after reindex: %+v", g)
	}
	blocked := memberClient(t, ix2, ts.URL, contributor.ID)
	if status, body := getStatus(t, blocked, ts.URL+"/galleries/"+slug); status != http.StatusNotFound {
		t.Fatalf("contributor after reindex: got %d, want 404\n%s", status, body)
	}
}

func TestViewerInviteSeesOnlyThatGallery(t *testing.T) {
	srv, st, ix, dir := newTestServer(t)
	ts := serve(t, srv)
	owner := testMember(t, st, "Ana", "cred-ana-view")
	birth := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	drawn := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	ownArtist, err := st.CreateArtist("Lucia", birth)
	if err != nil {
		t.Fatal(err)
	}
	otherArtist, err := st.CreateArtist("Mateo", birth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: ownArtist.Slug, Title: "OnlyHere", Date: drawn, ImageBytes: tinyPNG(t),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateArtwork(store.CreateArtworkInput{
		ArtistSlug: otherArtist.Slug, Title: "NotHere", Date: drawn, ImageBytes: tinyPNG(t),
	}); err != nil {
		t.Fatal(err)
	}
	home, err := st.CreateGallery("Sala", "pale-sage")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateGallery("Otra", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(home.Slug, ownArtist.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(other.Slug, otherArtist.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, owner.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}

	ownerC := memberClient(t, ix, ts.URL, owner.ID)
	token, linkToken := issueInvite(t, ownerC, ts.URL, home.Slug, "Vera", store.RoleViewer)
	if strings.Contains(manageBody(t, ownerC, ts.URL, home.Slug), linkToken) {
		t.Fatal("invite link is shown again on the gallery page")
	}
	if inviteFileContains(t, st, linkToken) {
		t.Fatal("invite file stores the token")
	}

	guest := testClient(t)
	auth := registerPasskey(t, guest, ts.URL, token)
	assertViewer(t, guest, ts.URL, st)
	g, err := st.LoadGallery(home.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if countRole(g, store.RoleViewer) != 1 || countRole(g, store.RoleOwner) != 1 {
		t.Fatalf("gallery.json roles: %+v", g.Members)
	}

	reopenIndex(t, srv, st, dir)
	loginPasskey(t, guest, ts.URL, auth)
	assertViewer(t, guest, ts.URL, st)
	g, err = st.LoadGallery(home.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if countRole(g, store.RoleViewer) != 1 {
		t.Fatalf("viewer role missing after reindex: %+v", g.Members)
	}
}

func TestContributorInviteUploadsOnlyThatGallery(t *testing.T) {
	srv, st, ix, dir := newTestServer(t)
	ts := serve(t, srv)
	owner := testMember(t, st, "Ana", "cred-ana-contrib")
	birth := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	ownArtist, err := st.CreateArtist("Lucia", birth)
	if err != nil {
		t.Fatal(err)
	}
	otherArtist, err := st.CreateArtist("Mateo", birth)
	if err != nil {
		t.Fatal(err)
	}
	home, err := st.CreateGallery("Sala", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateGallery("Otra", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(home.Slug, ownArtist.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(other.Slug, otherArtist.Slug); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, owner.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}

	ownerC := memberClient(t, ix, ts.URL, owner.ID)
	token, _ := issueInvite(t, ownerC, ts.URL, home.Slug, "Ciro", store.RoleContributor)
	guest := testClient(t)
	auth := registerPasskey(t, guest, ts.URL, token)
	assertContributor(t, guest, ts.URL, st, ownArtist.Slug, otherArtist.Slug, "NewBoat", "Intruder")
	g, err := st.LoadGallery(home.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if countRole(g, store.RoleContributor) != 1 {
		t.Fatalf("gallery.json roles: %+v", g.Members)
	}

	reopenIndex(t, srv, st, dir)
	loginPasskey(t, guest, ts.URL, auth)
	assertContributor(t, guest, ts.URL, st, ownArtist.Slug, otherArtist.Slug, "AfterIndex", "AfterIntruder")
}

func TestUsedOrExpiredGalleryInviteGrantsNothing(t *testing.T) {
	srv, st, ix, dir := newTestServer(t)
	ts := serve(t, srv)
	owner := testMember(t, st, "Ana", "cred-ana-used")
	home, err := st.CreateGallery("Sala", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, owner.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	expiredLink, err := st.CreateGalleryInvite("Old", home.Slug, store.RoleViewer, testOrigin, time.Now().Add(-8*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	expiredToken := expiredLink[strings.LastIndex(expiredLink, "/")+1:]
	before := memberDirs(t, st)
	sameClient := testClient(t)
	expired := invitePage(t, sameClient, ts.URL+"/invite/"+expiredToken)
	if expired.status != http.StatusNotFound {
		t.Fatalf("expired invite: got %d, want 404\n%s", expired.status, expired.body)
	}
	res, beginBody := postCSRF(t, sameClient, ts.URL+"/invite/"+expiredToken+"/begin", csrfFrom(t, expired.body), "", nil)
	if res.StatusCode == http.StatusOK {
		t.Fatalf("expired invite began a ceremony: %s", beginBody)
	}
	if memberDirs(t, st) != before {
		t.Fatalf("expired invite created a member: %d to %d", before, memberDirs(t, st))
	}

	ownerC := memberClient(t, ix, ts.URL, owner.ID)
	token, _ := issueInvite(t, ownerC, ts.URL, home.Slug, "Vera", store.RoleViewer)
	guest := testClient(t)
	registerPasskey(t, guest, ts.URL, token)
	used := invitePage(t, sameClient, ts.URL+"/invite/"+token)
	if used.status != http.StatusNotFound || used.body != expired.body {
		t.Fatalf("used invite: %d\nused:\n%s\nexpired:\n%s", used.status, used.body, expired.body)
	}
	res, beginBody = postCSRF(t, sameClient, ts.URL+"/invite/"+token+"/begin", csrfFrom(t, used.body), "", nil)
	if res.StatusCode == http.StatusOK {
		t.Fatalf("used invite began a ceremony: %s", beginBody)
	}
	g, err := st.LoadGallery(home.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if countRole(g, store.RoleViewer) != 1 || len(g.Members) != 2 {
		t.Fatalf("used invite added another member: %+v", g.Members)
	}

	reopenIndex(t, srv, st, dir)
	again := invitePage(t, sameClient, ts.URL+"/invite/"+token)
	if again.status != http.StatusNotFound || again.body != expired.body {
		t.Fatalf("used invite after reindex: %d\n%s", again.status, again.body)
	}
	g, err = st.LoadGallery(home.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Members) != 2 || countRole(g, store.RoleViewer) != 1 {
		t.Fatalf("roles after reindex: %+v", g.Members)
	}
}

func TestArtistsNewRequiresGalleryOwner(t *testing.T) {
	srv, st, ix, dir := newTestServer(t)
	ts := serve(t, srv)
	owner := testMember(t, st, "Ana", "cred-ana-artists")
	contributor := testMember(t, st, "Ciro", "cred-ciro-artists")
	viewer := testMember(t, st, "Vera", "cred-vera-artists")
	outsider := testMember(t, st, "Nora", "cred-nora-artists")
	bea := testMember(t, st, "Bea", "cred-bea-artists")
	home, err := st.CreateGallery("Sala", "")
	if err != nil {
		t.Fatal(err)
	}
	otra, err := st.CreateGallery("Otra", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, owner.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(otra.Slug, bea.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, contributor.ID, store.RoleContributor); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(home.Slug, viewer.ID, store.RoleViewer); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	before := len(mustArtists(t, st))

	for _, id := range []string{contributor.ID, viewer.ID, outsider.ID} {
		c := memberClient(t, ix, ts.URL, id)
		if status, body := getStatus(t, c, ts.URL+"/artists/new"); status != http.StatusNotFound {
			t.Fatalf("GET /artists/new for %s: got %d, want 404 (200 means the owner check was removed)\n%s", id, status, body)
		}
		status, _, body := postForm(t, c, ts.URL+"/artists/new", csrfOf(t, c, ts.URL+"/"), map[string]string{
			"gallery":    home.Slug,
			"name":       "Sneaky",
			"birth_date": "2019-01-01",
		})
		if status != http.StatusNotFound {
			t.Fatalf("POST /artists/new for %s: got %d, want 404 (a created artist means the owner check was removed)\n%s", id, status, body)
		}
	}
	beaC := memberClient(t, ix, ts.URL, bea.ID)
	status, _, body := postForm(t, beaC, ts.URL+"/artists/new", csrfOf(t, beaC, ts.URL+"/artists/new"), map[string]string{
		"gallery":    home.Slug,
		"name":       "Sneaky",
		"birth_date": "2019-01-01",
	})
	if status != http.StatusNotFound {
		t.Fatalf("owner of another gallery: got %d, want 404 (303 means artistNewPOST does not check ownership of that gallery)\n%s", status, body)
	}
	if got := len(mustArtists(t, st)); got != before {
		t.Fatalf("artists on disk: got %d, want %d", got, before)
	}
	ownerC := memberClient(t, ix, ts.URL, owner.ID)
	if status, body := getStatus(t, ownerC, ts.URL+"/artists/new"); status != http.StatusOK || !strings.Contains(body, "Sala") {
		t.Fatalf("owner form: %d\n%s", status, body)
	}

	ix2 := reopenIndex(t, srv, st, dir)
	viewerC := memberClient(t, ix2, ts.URL, viewer.ID)
	if status, body := getStatus(t, viewerC, ts.URL+"/artists/new"); status != http.StatusNotFound {
		t.Fatalf("viewer after reindex: got %d, want 404\n%s", status, body)
	}
	ownerC = memberClient(t, ix2, ts.URL, owner.ID)
	if status, body := getStatus(t, ownerC, ts.URL+"/artists/new"); status != http.StatusOK {
		t.Fatalf("owner after reindex: %d\n%s", status, body)
	}
}

func TestOnlyOwnerOfThatGalleryCanIssueInvite(t *testing.T) {
	srv, st, ix, _ := newTestServer(t)
	ts := serve(t, srv)
	ana := testMember(t, st, "Ana", "cred-ana-bite")
	ciro := testMember(t, st, "Ciro", "cred-ciro-bite")
	bea := testMember(t, st, "Bea", "cred-bea-bite")
	sala, err := st.CreateGallery("Sala", "")
	if err != nil {
		t.Fatal(err)
	}
	otra, err := st.CreateGallery("Otra", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(sala.Slug, ana.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(sala.Slug, ciro.ID, store.RoleContributor); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(otra.Slug, bea.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	before := inviteFiles(t, st)
	fields := map[string]string{"name": "Niece", "role": store.RoleViewer}
	target := ts.URL + "/galleries/" + sala.Slug + "/invite"

	ciroC := memberClient(t, ix, ts.URL, ciro.ID)
	status, _, body := postForm(t, ciroC, target, csrfOf(t, ciroC, ts.URL+"/"), fields)
	if status != http.StatusNotFound {
		t.Fatalf("contributor invite: got %d, want 404 (200 means the owner check in galleryInvitePOST was removed)\n%s", status, body)
	}
	beaC := memberClient(t, ix, ts.URL, bea.ID)
	status, _, body = postForm(t, beaC, target, csrfOf(t, beaC, ts.URL+"/"), fields)
	if status != http.StatusNotFound {
		t.Fatalf("owner of another gallery: got %d, want 404 (200 means galleryInvitePOST does not check ownership of that gallery)\n%s", status, body)
	}
	if got := inviteFiles(t, st); got != before {
		t.Fatalf("invite files: got %d, want %d", got, before)
	}

	anaC := memberClient(t, ix, ts.URL, ana.ID)
	status, _, body = postForm(t, anaC, target, csrfOf(t, anaC, ts.URL+"/galleries/"+sala.Slug), fields)
	if status != http.StatusOK || !strings.Contains(body, "/invite/") {
		t.Fatalf("owner invite: %d\n%s", status, body)
	}
	if strings.Contains(manageBody(t, anaC, ts.URL, sala.Slug), tokenFromLink(t, body)) {
		t.Fatal("invite link is shown again on the gallery page")
	}
	if got := inviteFiles(t, st); got != before+1 {
		t.Fatalf("invite files after owner: got %d, want %d", got, before+1)
	}
}

func TestOnlyOwnerOfThatGalleryCanAddArtist(t *testing.T) {
	srv, st, ix, _ := newTestServer(t)
	ts := serve(t, srv)
	ana := testMember(t, st, "Ana", "cred-ana-add")
	bea := testMember(t, st, "Bea", "cred-bea-add")
	birth := time.Date(2018, 3, 3, 0, 0, 0, 0, time.UTC)
	nia, err := st.CreateArtist("Nia", birth)
	if err != nil {
		t.Fatal(err)
	}
	sala, err := st.CreateGallery("Sala", "")
	if err != nil {
		t.Fatal(err)
	}
	otra, err := st.CreateGallery("Otra", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(sala.Slug, ana.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(otra.Slug, bea.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(otra.Slug, nia.Slug); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}

	beaC := memberClient(t, ix, ts.URL, bea.ID)
	status, _, body := postForm(t, beaC, ts.URL+"/galleries/"+sala.Slug+"/artists", csrfOf(t, beaC, ts.URL+"/"), map[string]string{
		"artist_slug": nia.Slug,
	})
	if status != http.StatusNotFound {
		t.Fatalf("owner of another gallery: got %d, want 404 (303 means galleryArtistPOST checks ownsAny instead of ownsGallery)\n%s", status, body)
	}
	g, err := st.LoadGallery(sala.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Artists) != 0 {
		t.Fatalf("sala gained an artist: %+v", g.Artists)
	}
}

func TestOwnerCannotAddArtistFromAnotherGallery(t *testing.T) {
	srv, st, ix, _ := newTestServer(t)
	ts := serve(t, srv)
	ana := testMember(t, st, "Ana", "cred-ana-cross")
	bea := testMember(t, st, "Bea", "cred-bea-cross")
	birth := time.Date(2018, 3, 3, 0, 0, 0, 0, time.UTC)
	mateo, err := st.CreateArtist("Mateo", birth)
	if err != nil {
		t.Fatal(err)
	}
	sala, err := st.CreateGallery("Sala", "")
	if err != nil {
		t.Fatal(err)
	}
	otra, err := st.CreateGallery("Otra", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(sala.Slug, ana.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryMember(otra.Slug, bea.ID, store.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGalleryArtist(otra.Slug, mateo.Slug); err != nil {
		t.Fatal(err)
	}
	if err := ix.Rebuild(st); err != nil {
		t.Fatal(err)
	}

	anaC := memberClient(t, ix, ts.URL, ana.ID)
	status, body := getStatus(t, anaC, ts.URL+"/galleries/"+sala.Slug)
	if status != http.StatusOK || strings.Contains(body, "Mateo") {
		t.Fatalf("manage lists an artist from another gallery: %d\n%s", status, body)
	}
	postStatus, _, postBody := postForm(t, anaC, ts.URL+"/galleries/"+sala.Slug+"/artists", csrfFrom(t, body), map[string]string{
		"artist_slug": mateo.Slug,
	})
	if postStatus != http.StatusNotFound {
		t.Fatalf("owner of A adding an artist that only lives in B: got %d, want 404\n%s", postStatus, postBody)
	}
	g, err := st.LoadGallery(sala.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Artists) != 0 {
		t.Fatalf("sala gained an artist from otra: %+v", g.Artists)
	}
}

func serve(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func reopenIndex(t *testing.T, srv *Server, st *store.Store, dir string) *index.Index {
	t.Helper()
	if err := srv.Index.Close(); err != nil {
		t.Fatal(err)
	}
	db := index.DBPath(dir)
	if err := os.Remove(db); err != nil {
		t.Fatal(err)
	}
	for _, suf := range []string{"-wal", "-shm"} {
		_ = os.Remove(db + suf)
	}
	ix2, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix2.Close() })
	if err := ix2.Rebuild(st); err != nil {
		t.Fatal(err)
	}
	srv.Index = ix2
	return ix2
}

func csrfOf(t *testing.T, c *http.Client, rawURL string) string {
	t.Helper()
	status, body := getStatus(t, c, rawURL)
	if status != http.StatusOK {
		t.Fatalf("csrf %s: %d %s", rawURL, status, body)
	}
	return csrfFrom(t, body)
}

func postForm(t *testing.T, c *http.Client, rawURL, csrf string, fields map[string]string) (int, string, string) {
	t.Helper()
	vals := url.Values{}
	vals.Set("csrf", csrf)
	vals.Set("lang", "en")
	for k, v := range fields {
		vals.Set(k, v)
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(vals.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header.Get("Location"), readBody(t, res)
}

func gallerySlugFromLocation(t *testing.T, loc string) string {
	t.Helper()
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "/galleries/"
	if !strings.HasPrefix(u.Path, prefix) || strings.Contains(strings.TrimPrefix(u.Path, prefix), "/") {
		t.Fatalf("location %q", loc)
	}
	return strings.TrimPrefix(u.Path, prefix)
}

func galleryHasArtistNamed(t *testing.T, st *store.Store, g *store.Gallery, name string) bool {
	t.Helper()
	for _, slug := range g.Artists {
		a, err := st.LoadArtist(slug)
		if err != nil {
			t.Fatal(err)
		}
		if a.Name == name {
			return true
		}
	}
	return false
}

func issueInvite(t *testing.T, c *http.Client, base, slug, name, role string) (token, raw string) {
	t.Helper()
	page := manageBody(t, c, base, slug)
	status, _, body := postForm(t, c, base+"/galleries/"+slug+"/invite", csrfFrom(t, page), map[string]string{
		"name": name,
		"role": role,
	})
	if status != http.StatusOK || !strings.Contains(body, "/invite/") {
		t.Fatalf("issue invite: %d\n%s", status, body)
	}
	raw = tokenFromLink(t, body)
	return raw, raw
}

func manageBody(t *testing.T, c *http.Client, base, slug string) string {
	t.Helper()
	status, body := getStatus(t, c, base+"/galleries/"+slug)
	if status != http.StatusOK {
		t.Fatalf("manage %s: %d\n%s", slug, status, body)
	}
	return body
}

func tokenFromLink(t *testing.T, body string) string {
	t.Helper()
	const marker = "/invite/"
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("invite link missing:\n%s", body)
	}
	rest := body[i+len(marker):]
	end := len(rest)
	for j, r := range rest {
		if r == '<' || r == '"' || r == ' ' || r == '\n' || r == '&' || r == '\r' {
			end = j
			break
		}
	}
	if end == 0 {
		t.Fatalf("empty token in %s", body)
	}
	return rest[:end]
}

func inviteFileContains(t *testing.T, st *store.Store, token string) bool {
	t.Helper()
	entries, err := os.ReadDir(st.InvitesRoot())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		body, err := os.ReadFile(st.InvitesRoot() + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), token) {
			return true
		}
	}
	return false
}

func inviteFiles(t *testing.T, st *store.Store) int {
	t.Helper()
	entries, err := os.ReadDir(st.InvitesRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

func memberDirs(t *testing.T, st *store.Store) int {
	t.Helper()
	entries, err := os.ReadDir(st.MembersRoot())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

func mustArtists(t *testing.T, st *store.Store) []store.Artist {
	t.Helper()
	all, err := st.ListArtists()
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func countRole(g *store.Gallery, role string) int {
	n := 0
	for _, m := range g.Members {
		if m.Role == role {
			n++
		}
	}
	return n
}

func assertViewer(t *testing.T, c *http.Client, base string, st *store.Store) {
	t.Helper()
	status, body := getStatus(t, c, base+"/")
	if status != http.StatusOK || !strings.Contains(body, "OnlyHere") || strings.Contains(body, "NotHere") {
		t.Fatalf("viewer home: %d\n%s", status, body)
	}
	if status, body = getStatus(t, c, base+"/artworks/new"); status != http.StatusNotFound {
		t.Fatalf("viewer upload form: got %d, want 404\n%s", status, body)
	}
	if status, body = postArtwork(t, c, base, "SneakyView", ""); status != http.StatusNotFound {
		t.Fatalf("viewer upload: got %d, want 404\n%s", status, body)
	}
	if titleOnDisk(t, st, "SneakyView") {
		t.Fatal("viewer upload was stored")
	}
}

func assertContributor(t *testing.T, c *http.Client, base string, st *store.Store, ownArtist, otherArtist, okTitle, blockedTitle string) {
	t.Helper()
	status, body := postArtwork(t, c, base, blockedTitle, otherArtist)
	if status != http.StatusNotFound {
		t.Fatalf("upload outside the gallery: got %d, want 404\n%s", status, body)
	}
	if titleOnDisk(t, st, blockedTitle) {
		t.Fatal("upload outside the gallery was stored")
	}
	status, body = postArtwork(t, c, base, okTitle, ownArtist)
	if status != http.StatusSeeOther {
		t.Fatalf("upload inside the gallery: got %d, want 303\n%s", status, body)
	}
	if !titleOnDisk(t, st, okTitle) {
		t.Fatal("upload inside the gallery was not stored")
	}
	status, body = getStatus(t, c, base+"/")
	if status != http.StatusOK || !strings.Contains(body, okTitle) {
		t.Fatalf("contributor home: %d\n%s", status, body)
	}
}
