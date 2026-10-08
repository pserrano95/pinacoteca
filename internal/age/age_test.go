package age

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAt(t *testing.T) {
	got, err := At(d("2020-06-15"), d("2024-06-14"))
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("got %d want 3", got)
	}
	got, err = At(d("2020-06-15"), d("2024-06-15"))
	if err != nil {
		t.Fatal(err)
	}
	if got != 4 {
		t.Fatalf("got %d want 4", got)
	}
}

func TestAtBeforeBirth(t *testing.T) {
	_, err := At(d("2020-01-01"), d("2019-12-31"))
	if err == nil {
		t.Fatal("expected error")
	}
}
