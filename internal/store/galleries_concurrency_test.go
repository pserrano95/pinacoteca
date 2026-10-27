package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGalleryConcurrency_AddMembersAndArtists(t *testing.T) {
	dir := t.TempDir()
	st1 := New(dir)
	st2 := New(dir)
	if err := st1.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	gallery, err := st1.CreateGallery("Principal", "")
	if err != nil {
		t.Fatal(err)
	}

	artists := make([]string, 50)
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("Artist %02d", i)
		a, err := st1.CreateArtist(name, time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		artists[i] = a.Slug
	}

	members := make([]string, 50)
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("Member %02d", i)
		m, err := st1.CreateMember(name, []Passkey{{
			ID:        fmt.Sprintf("cred-%02d", i),
			PublicKey: fmt.Sprintf("pk-%02d", i),
			CreatedAt: time.Now().UTC(),
		}}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		members[i] = m.ID
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	start := make(chan struct{})

	for i := 0; i < 50; i++ {
		i := i
		st := st1
		if i%2 == 1 {
			st = st2
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := st.AddGalleryArtist(gallery.Slug, artists[i]); err != nil {
				errCh <- fmt.Errorf("add artist %s: %w", artists[i], err)
			}
		}()
	}

	for i := 0; i < 50; i++ {
		i := i
		st := st1
		if i%2 == 1 {
			st = st2
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			role := RoleContributor
			if i%3 == 0 {
				role = RoleViewer
			}
			if err := st.AddGalleryMember(gallery.Slug, members[i], role); err != nil {
				errCh <- fmt.Errorf("add member %s: %w", members[i], err)
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}

	loaded, err := st1.LoadGallery(gallery.Slug)
	if err != nil {
		t.Fatalf("LoadGallery failed: %v", err)
	}
	if len(loaded.Artists) != 50 {
		t.Fatalf("expected 50 artists, got %d", len(loaded.Artists))
	}
	if len(loaded.Members) != 50 {
		t.Fatalf("expected 50 members, got %d", len(loaded.Members))
	}

	assertNoTempsInGalleries(t, st1.GalleriesRoot())
}

func TestGalleryConcurrency_MixAddAndRemoveMembers(t *testing.T) {
	dir := t.TempDir()
	st1 := New(dir)
	st2 := New(dir)
	if err := st1.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	gallery, err := st1.CreateGallery("Mezcla", "")
	if err != nil {
		t.Fatal(err)
	}

	// Create 60 members: 30 initial (0..29), 30 to add concurrently (30..59)
	allMembers := make([]string, 60)
	for i := 0; i < 60; i++ {
		name := fmt.Sprintf("Member %02d", i)
		m, err := st1.CreateMember(name, []Passkey{{
			ID:        fmt.Sprintf("cred-mix-%02d", i),
			PublicKey: fmt.Sprintf("pk-mix-%02d", i),
			CreatedAt: time.Now().UTC(),
		}}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		allMembers[i] = m.ID
	}

	// Seed first 30 members into gallery
	for i := 0; i < 30; i++ {
		if err := st1.AddGalleryMember(gallery.Slug, allMembers[i], RoleContributor); err != nil {
			t.Fatal(err)
		}
	}

	// Concurrently:
	// - remove first 15 members (0..14)
	// - add 30 new members (30..59)
	var wg sync.WaitGroup
	errCh := make(chan error, 45)
	start := make(chan struct{})

	// 15 removals
	for i := 0; i < 15; i++ {
		i := i
		st := st1
		if i%2 == 1 {
			st = st2
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := st.removeGalleryMember(gallery.Slug, allMembers[i]); err != nil {
				errCh <- fmt.Errorf("remove member %s: %w", allMembers[i], err)
			}
		}()
	}

	// 30 additions
	for i := 30; i < 60; i++ {
		i := i
		st := st1
		if i%2 == 1 {
			st = st2
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := st.AddGalleryMember(gallery.Slug, allMembers[i], RoleViewer); err != nil {
				errCh <- fmt.Errorf("add member %s: %w", allMembers[i], err)
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}

	loaded, err := st1.LoadGallery(gallery.Slug)
	if err != nil {
		t.Fatalf("LoadGallery failed: %v", err)
	}

	// Members 0..14 should be removed.
	// Members 15..29 should remain.
	// Members 30..59 should be added.
	// Total expected: 15 + 30 = 45 members.
	if len(loaded.Members) != 45 {
		t.Fatalf("expected 45 members, got %d", len(loaded.Members))
	}

	present := make(map[string]bool)
	for _, m := range loaded.Members {
		present[m.ID] = true
	}

	for i := 0; i < 15; i++ {
		if present[allMembers[i]] {
			t.Fatalf("member %s should have been removed", allMembers[i])
		}
	}
	for i := 15; i < 60; i++ {
		if !present[allMembers[i]] {
			t.Fatalf("member %s should be present", allMembers[i])
		}
	}

	assertNoTempsInGalleries(t, st1.GalleriesRoot())
}

func TestGalleryConcurrency_CreateGallerySameName(t *testing.T) {
	dir := t.TempDir()
	st1 := New(dir)
	st2 := New(dir)
	if err := st1.EnsureLayout(); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var g1, g2 *Gallery
	var err1, err2 error
	start := make(chan struct{})

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		g1, err1 = st1.CreateGallery("Abuelos", "")
	}()
	go func() {
		defer wg.Done()
		<-start
		g2, err2 = st2.CreateGallery("Abuelos", "")
	}()

	close(start)
	wg.Wait()

	if err1 != nil {
		t.Fatalf("create gallery 1: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("create gallery 2: %v", err2)
	}
	if g1.Slug == g2.Slug {
		t.Fatalf("expected distinct slugs, got %q for both", g1.Slug)
	}
	expectedSlugs := map[string]bool{"abuelos": true, "abuelos-2": true}
	if !expectedSlugs[g1.Slug] || !expectedSlugs[g2.Slug] {
		t.Fatalf("unexpected slugs: %q and %q", g1.Slug, g2.Slug)
	}

	loaded1, err := st1.LoadGallery(g1.Slug)
	if err != nil {
		t.Fatalf("LoadGallery(%q): %v", g1.Slug, err)
	}
	if loaded1.Name != "Abuelos" {
		t.Fatalf("expected name Abuelos, got %q", loaded1.Name)
	}

	loaded2, err := st2.LoadGallery(g2.Slug)
	if err != nil {
		t.Fatalf("LoadGallery(%q): %v", g2.Slug, err)
	}
	if loaded2.Name != "Abuelos" {
		t.Fatalf("expected name Abuelos, got %q", loaded2.Name)
	}

	assertNoTempsInGalleries(t, st1.GalleriesRoot())
}

func assertNoTempsInGalleries(t *testing.T, galleriesRoot string) {
	t.Helper()
	err := filepath.Walk(galleriesRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			name := filepath.Base(path)
			if strings.HasSuffix(name, ".tmp") || strings.HasPrefix(name, ".") || name != GalleryFile {
				return fmt.Errorf("unexpected file in galleries: %s", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("check leftover temps: %v", err)
	}
}
