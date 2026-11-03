package main

import (
	"github.com/pserrano95/pinacoteca/internal/index"
	"github.com/pserrano95/pinacoteca/internal/store"
	"testing"
	"time"
)

func TestArtistAddGuardianCommandPreservesCLILegacyAndIndexesAssignment(t *testing.T) {
	dir := t.TempDir()
	st := store.New(dir)
	a, err := st.CreateArtist("Ada", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Guardians) != 0 {
		t.Fatal("CLI/store creation assigned guardian")
	}
	m, err := st.CreateMember("Parent", []store.Passkey{{ID: "guardian-test", PublicKey: "pk-test", CreatedAt: time.Now()}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := runArtist([]string{"add-guardian", "--data", dir, a.Slug, m.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadArtist(a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsGuardian(m.ID) || len(got.ApprovedGalleries) != 0 {
		t.Fatalf("assignment: %+v", got)
	}
	ix, err := index.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	row, err := ix.GetArtist(a.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(row.Guardians) != 1 || row.Guardians[0] != m.ID {
		t.Fatalf("index: %+v", row)
	}
	for _, args := range [][]string{{}, {"add-guardian"}, {"add-guardian", "--data", dir, a.Slug, "bad"}, {"add-guardian", "--data", dir, "../bad", m.ID}} {
		if err := runArtist(args); err == nil {
			t.Fatalf("accepted invalid command %v", args)
		}
	}
}
