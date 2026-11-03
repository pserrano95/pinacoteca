package store_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pserrano95/pinacoteca/internal/store"
)

func TestLegacyArtistWithoutGuardianFieldsLoads(t *testing.T) {
	st := store.New(t.TempDir())
	a, err := st.CreateArtist("Ada", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(st.ArtistsRoot(), a.Slug, store.ArtistFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "guardians") || strings.Contains(string(raw), "approved_galleries") {
		t.Fatalf("legacy fixture includes fields: %s", raw)
	}
	got, err := st.LoadArtist(a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Guardians) != 0 || len(got.ApprovedGalleries) != 0 || got.SchemaVersion != 1 {
		t.Fatalf("legacy: %+v", got)
	}
}

func TestArtistRejectsInvalidGuardianAndApprovalFiles(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, tc := range []struct {
		name                 string
		guardians, approvals []string
	}{
		{"invalid_member_id", []string{"../member"}, nil},
		{"duplicate_member_id", []string{id, id}, nil},
		{"invalid_gallery_slug", nil, []string{"../gallery"}},
		{"duplicate_gallery_slug", nil, []string{"home", "home"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := store.New(t.TempDir())
			a, err := st.CreateArtist("Ada", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			a.Guardians = tc.guardians
			a.ApprovedGalleries = tc.approvals
			raw, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(st.ArtistsRoot(), a.Slug, store.ArtistFile), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := st.LoadArtist(a.Slug); err == nil {
				t.Fatal("invalid permissions loaded")
			}
			if _, err := st.ListArtists(); err == nil {
				t.Fatal("invalid permissions silently omitted by archive scan")
			}
		})
	}
}

func TestConcurrentGuardianApprovalsPreserveBothGalleries(t *testing.T) {
	st := store.New(t.TempDir())
	m, err := st.CreateMember("Parent", []store.Passkey{{ID: "guardian-test", PublicKey: "pk-test", CreatedAt: time.Now()}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a, err := st.CreateArtistWithGuardian("Ada", time.Now(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, name := range []string{"One", "Two"} {
		g, err := st.CreateGallery(name, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AddGalleryArtist(g.Slug, a.Slug); err != nil {
			t.Fatal(err)
		}
		slugs = append(slugs, g.Slug)
	}
	var wg sync.WaitGroup
	for _, slug := range slugs {
		wg.Add(1)
		go func(slug string) {
			defer wg.Done()
			if err := store.New(st.DataDir).ApproveArtistGallery(a.Slug, slug, m.ID); err != nil {
				t.Error(err)
			}
		}(slug)
	}
	wg.Wait()
	got, err := st.LoadArtist(a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ApprovedGalleries) != 2 {
		t.Fatalf("lost approval: %+v", got)
	}
}
