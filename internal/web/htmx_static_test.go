package web

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestHtmxMinJS runs the behavior suite next to the vendored htmx script.
// Node's built-in runner is started from here so `go test` stays the command
// CI already runs; the suite does not add a module dependency.
func TestHtmxMinJS(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required to run htmx.min.test.js: %v", err)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	dir := filepath.Join(filepath.Dir(file), "static")
	cmd := exec.Command(node, "--test", "--test-concurrency=1", filepath.Join(dir, "htmx.min.test.js"))
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("htmx.min.js behavior:\n%s", out)
	}
}
