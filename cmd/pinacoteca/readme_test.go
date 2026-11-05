package main

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// findRepoRoot searches upwards from current directory for go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repository root (go.mod) from %s", dir)
		}
		dir = parent
	}
}

var targetRegex = regexp.MustCompile(`^([a-z][a-z-]*):`)

// parseMakefileTargets extracts all target names matching ^[a-z-]*: from Makefile content.
func parseMakefileTargets(content string) []string {
	var targets []string
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		matches := targetRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			targets = append(targets, matches[1])
		}
	}
	return targets
}

// checkReadmeDocumentsTargets verifies that each target t appears as "make <t>" in readmeContent.
func checkReadmeDocumentsTargets(readmeContent string, targets []string) []string {
	var missing []string
	for _, target := range targets {
		needle := "make " + target
		if !strings.Contains(readmeContent, needle) {
			missing = append(missing, target)
		}
	}
	return missing
}

// TestReadmeDocumentsMakefileTargets verifies that every Makefile target is documented in README.md.
// If no Makefile exists (empty or non-make repository), it exits cleanly without false positives.
func TestReadmeDocumentsMakefileTargets(t *testing.T) {
	root := findRepoRoot(t)
	makefilePath := filepath.Join(root, "Makefile")
	readmePath := filepath.Join(root, "README.md")

	makeData, err := os.ReadFile(makefilePath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Log("no Makefile found; skipping target documentation check")
			return
		}
		t.Fatalf("read Makefile: %v", err)
	}

	readmeData, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}

	targets := parseMakefileTargets(string(makeData))
	if len(targets) == 0 {
		t.Log("no Makefile targets found")
		return
	}

	missing := checkReadmeDocumentsTargets(string(readmeData), targets)
	for _, target := range missing {
		t.Errorf("Makefile target %q is not documented in README.md (expected 'make %s')", target, target)
	}
}

// TestReadmeDetectorEmptyRepo verifies that a repository without a Makefile does not trigger a false red.
func TestReadmeDetectorEmptyRepo(t *testing.T) {
	targets := parseMakefileTargets("")
	if len(targets) != 0 {
		t.Fatalf("expected 0 targets from empty content, got %d", len(targets))
	}
	missing := checkReadmeDocumentsTargets("Any README content", targets)
	if len(missing) != 0 {
		t.Fatalf("expected 0 missing targets for empty makefile, got %v", missing)
	}
}

// TestReadmeDetectorMissingTargetFails verifies that missing Makefile targets are correctly detected.
func TestReadmeDetectorMissingTargetFails(t *testing.T) {
	sampleMakefile := "verify: fmt\nfmt:\n\t@gofmt\nvet:\n\t@go vet\n"
	sampleReadme := "# Project\nRun `make verify` to check code.\n"

	targets := parseMakefileTargets(sampleMakefile)
	missing := checkReadmeDocumentsTargets(sampleReadme, targets)

	expectedMissing := map[string]bool{"fmt": true, "vet": true}
	if len(missing) != len(expectedMissing) {
		t.Fatalf("expected %d missing targets, got %v", len(expectedMissing), missing)
	}
	for _, m := range missing {
		if !expectedMissing[m] {
			t.Errorf("unexpected missing target reported: %s", m)
		}
	}
}

// TestDocumentationCoversCodebaseFiles verifies that files added across merged PRs
// are referenced in documentation (README.md, CONTEXT.md, or docs/*.md).
func TestDocumentationCoversCodebaseFiles(t *testing.T) {
	root := findRepoRoot(t)

	var docBuilder strings.Builder
	docFiles := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "CONTEXT.md"),
	}
	docsDir := filepath.Join(root, "docs")
	entries, err := os.ReadDir(docsDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
				docFiles = append(docFiles, filepath.Join(docsDir, e.Name()))
			}
		}
	}

	for _, df := range docFiles {
		data, err := os.ReadFile(df)
		if err == nil {
			docBuilder.Write(data)
			docBuilder.WriteString("\n")
		}
	}
	allDocs := docBuilder.String()

	filesToCheck := []string{
		"internal/imageutil/imageutil.go",
		"internal/imageutil/imageutil_test.go",
		"internal/store/store.go",
		"internal/store/store_test.go",
		"cmd/pinacoteca/artist_test.go",
		"cmd/pinacoteca/export_test.go",
		"cmd/pinacoteca/gallery_test.go",
		"cmd/pinacoteca/invite_test.go",
		"cmd/pinacoteca/main.go",
		"internal/i18n/locales/en.json",
		"internal/i18n/locales/es.json",
		"internal/index/index.go",
		"internal/index/reindex_test.go",
		"internal/store/guardians.go",
		"internal/store/guardians_test.go",
		"internal/store/types.go",
		"internal/web/auth.go",
		"internal/web/auth_test.go",
		"internal/web/gallery_access_test.go",
		"internal/web/gallery_web_test.go",
		"internal/web/guardians_test.go",
		"internal/web/htmx_static_test.go",
		"internal/web/pwa_test.go",
		"internal/web/server.go",
		"internal/web/softauth_test.go",
		"internal/web/templates/pages.html",
		"internal/export/export.go",
		"internal/export/export_test.go",
		"internal/web/static/icons/apple-touch-icon.png",
		"internal/web/static/icons/icon-180.png",
		"internal/web/static/icons/icon-192.png",
		"internal/web/static/icons/icon-512-maskable.png",
		"internal/web/static/icons/icon-512.png",
		"internal/web/static/manifest.webmanifest",
		"internal/web/static/pwa.js",
		"internal/web/static/sw.js",
		"internal/store/galleries.go",
		"internal/store/galleries_concurrency_test.go",
		"internal/store/galleries_test.go",
		"internal/store/invites.go",
		"internal/store/members.go",
		"internal/age/age.go",
		"internal/age/age_test.go",
		"internal/i18n/i18n.go",
		"internal/slug/slug.go",
		"internal/web/static/auth.js",
		"internal/web/static/htmx.min.js",
		"internal/web/static/style.css",
		"internal/web/testdata/htmx.min.test.js",
		"scripts/check-runtime-alpine.sh",
		".dockerignore",
		".github/dependabot.yml",
		".github/workflows/arranque.yml",
		".github/workflows/ci.yml",
		".gitignore",
		"Dockerfile",
		"Makefile",
		"go.mod",
		"go.sum",
	}

	for _, f := range filesToCheck {
		if !strings.Contains(allDocs, f) {
			t.Errorf("file %s is not mentioned in documentation (README.md, CONTEXT.md, docs/*.md)", f)
		}
	}
}
