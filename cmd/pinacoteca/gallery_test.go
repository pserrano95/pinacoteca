package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/store"
)

func TestGalleryCommand(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	artist, err := st.CreateArtist("Lucía", time.Date(2019, 3, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateMember("Ana", []store.Passkey{{
		ID: "cred-ana", PublicKey: "pk-ana", CreatedAt: time.Now().UTC(),
	}}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	slug := galleryStdout(t, []string{"create", "--data", dir, "--name", "Abuelos", "--wall-colour", "pale-sage"})
	if slug != "abuelos" {
		t.Fatalf("stdout slug: %q", slug)
	}
	if err := runGallery([]string{"add-artist", "--data", dir, "--gallery", slug, "--artist", artist.Slug}); err != nil {
		t.Fatal(err)
	}
	if err := runGallery([]string{"add-member", "--data", dir, "--gallery", slug, "--member", member.ID, "--role", "owner"}); err != nil {
		t.Fatal(err)
	}
	g, err := st.LoadGallery(slug)
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "Abuelos" || g.WallColour != "pale-sage" || g.SchemaVersion != store.SchemaVersion {
		t.Fatalf("gallery: %+v", g)
	}
	if len(g.Artists) != 1 || g.Artists[0] != artist.Slug {
		t.Fatalf("artists: %+v", g.Artists)
	}
	if len(g.Members) != 1 || g.Members[0].ID != member.ID || g.Members[0].Role != store.RoleOwner {
		t.Fatalf("members: %+v", g.Members)
	}
	if err := runGallery([]string{"create", "--data", dir, "--name", "X", "--wall-colour", "neon"}); err == nil {
		t.Fatal("unknown wall colour was accepted")
	}
}

func galleryStdout(t *testing.T, args []string) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := runGallery(args)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if runErr != nil {
		t.Fatal(runErr)
	}
	return strings.TrimSpace(buf.String())
}
