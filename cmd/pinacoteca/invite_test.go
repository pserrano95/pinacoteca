package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestInvitePrintsLink(t *testing.T) {
	dir := t.TempDir()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = runInvite([]string{"--data", dir, "--name", "Ana", "--base-url", "http://localhost:8080"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if err != nil {
		t.Fatal(err)
	}
	link := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(link, "http://localhost:8080/invite/") {
		t.Fatalf("stdout: %q", link)
	}
	if strings.Count(link, "\n") > 0 {
		t.Fatalf("expected a single line, got %q", link)
	}
}
