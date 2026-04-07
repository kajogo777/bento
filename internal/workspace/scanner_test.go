package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/extension"
)

func createFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestScanAssignsFilesToLayers(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "src/main.go", "package main")
	createFile(t, dir, "src/util.go", "package main")
	createFile(t, dir, "go.mod", "module test")
	createFile(t, dir, "go.sum", "hash")
	createFile(t, dir, "README.md", "# readme")

	layers := []extension.LayerDef{
		{Name: "source", Patterns: []string{"src/**"}},
		{Name: "deps", Patterns: []string{"go.mod", "go.sum"}},
		{Name: "docs", Patterns: []string{"*.md"}},
	}

	s := NewScanner(dir, layers, nil, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["source"].WorkspaceFiles) != 2 {
		t.Errorf("source layer has %d files, want 2", len(result["source"].WorkspaceFiles))
	}
	if len(result["deps"].WorkspaceFiles) != 2 {
		t.Errorf("deps layer has %d files, want 2", len(result["deps"].WorkspaceFiles))
	}
	if len(result["docs"].WorkspaceFiles) != 1 {
		t.Errorf("docs layer has %d files, want 1", len(result["docs"].WorkspaceFiles))
	}
}

func TestScanIgnoredFilesExcluded(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "src/main.go", "package main")
	createFile(t, dir, "src/debug.log", "log data")
	createFile(t, dir, ".DS_Store", "")

	layers := []extension.LayerDef{
		{Name: "source", Patterns: []string{"src/**"}},
		{Name: "meta", Patterns: []string{".DS_Store"}},
	}

	s := NewScanner(dir, layers, []string{"*.log", ".DS_Store"}, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["source"].WorkspaceFiles) != 1 {
		t.Errorf("source layer has %d files, want 1 (debug.log should be ignored)", len(result["source"].WorkspaceFiles))
	}
	if len(result["meta"].WorkspaceFiles) != 0 {
		t.Errorf("meta layer has %d files, want 0 (.DS_Store should be ignored)", len(result["meta"].WorkspaceFiles))
	}
}

func TestScanUnmatchedFilesExcluded(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "src/main.go", "package main")
	createFile(t, dir, "random.xyz", "unknown")

	layers := []extension.LayerDef{
		{Name: "source", Patterns: []string{"src/**"}},
	}

	s := NewScanner(dir, layers, nil, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["source"].WorkspaceFiles) != 1 {
		t.Errorf("source layer has %d files, want 1", len(result["source"].WorkspaceFiles))
	}

	for layer, sr := range result {
		for _, f := range sr.WorkspaceFiles {
			if f == "random.xyz" {
				t.Errorf("random.xyz should not appear in any layer, found in %q", layer)
			}
		}
	}
}

func TestScanFirstLayerWins(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "src/main.go", "package main")

	layers := []extension.LayerDef{
		{Name: "first", Patterns: []string{"src/**"}},
		{Name: "second", Patterns: []string{"src/**"}},
	}

	s := NewScanner(dir, layers, nil, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["first"].WorkspaceFiles) != 1 {
		t.Errorf("first layer has %d files, want 1", len(result["first"].WorkspaceFiles))
	}
	if len(result["second"].WorkspaceFiles) != 0 {
		t.Errorf("second layer has %d files, want 0 (first layer should win)", len(result["second"].WorkspaceFiles))
	}
}

func TestScanExternalPatterns(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	createFile(t, dir, "main.go", "package main")
	createFile(t, extDir, "session.jsonl", "data")
	createFile(t, extDir, "sub/notes.txt", "notes")

	layers := []extension.LayerDef{
		{Name: "agent", Patterns: []string{extDir + "/"}},
		{Name: "project", Patterns: []string{"**"}, CatchAll: true},
	}

	s := NewScanner(dir, layers, nil, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["agent"].ExternalFiles) != 2 {
		t.Errorf("agent layer has %d external files, want 2", len(result["agent"].ExternalFiles))
	}
	if len(result["project"].WorkspaceFiles) != 1 {
		t.Errorf("project layer has %d workspace files, want 1", len(result["project"].WorkspaceFiles))
	}

	// Verify external files have __external__ prefix in archive path
	for _, ef := range result["agent"].ExternalFiles {
		if ef.AbsPath == "" {
			t.Error("external file has empty AbsPath")
		}
		if len(ef.ArchivePath) < 13 || ef.ArchivePath[:13] != "__external__/" {
			t.Errorf("external file archive path missing __external__ prefix: %s", ef.ArchivePath)
		}
	}
}

func TestScanRejectsPathTraversalInExternalPatterns(t *testing.T) {
	dir := t.TempDir()

	layers := []extension.LayerDef{
		{Name: "agent", Patterns: []string{"~/../../etc/passwd"}},
	}

	s := NewScanner(dir, layers, nil, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	if len(result["agent"].ExternalFiles) != 0 {
		t.Errorf("agent layer should have 0 external files (.. rejected), got %d", len(result["agent"].ExternalFiles))
	}
}

func TestScanWithNormalizePath(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	createFile(t, extDir, "session.jsonl", "data")

	layers := []extension.LayerDef{
		{Name: "agent", Patterns: []string{extDir + "/"}},
		{Name: "project", Patterns: []string{"**"}, CatchAll: true},
	}

	// Build a normalizer that replaces the extDir's portable path with a placeholder
	portableExtDir := extension.PortablePath(extDir)
	placeholder := "/~/test/__BENTO_WORKSPACE__"
	normalizer := func(path string) string {
		if strings.HasPrefix(path, portableExtDir+"/") {
			return placeholder + path[len(portableExtDir):]
		}
		if path == portableExtDir {
			return placeholder
		}
		return path
	}

	s := NewScanner(dir, layers, nil, normalizer)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	// Verify archive paths use the placeholder, not the real path
	for _, ef := range result["agent"].ExternalFiles {
		if strings.Contains(ef.ArchivePath, portableExtDir) {
			t.Errorf("archive path should not contain real path: %s", ef.ArchivePath)
		}
		if !strings.Contains(ef.ArchivePath, "__BENTO_WORKSPACE__") {
			t.Errorf("archive path should contain __BENTO_WORKSPACE__ placeholder: %s", ef.ArchivePath)
		}
	}
}

func TestScanGitNotIgnoredByDefault(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, ".git/HEAD", "ref: refs/heads/main")
	createFile(t, dir, ".git/config", "[core]\n\tbare = false")
	createFile(t, dir, "src/main.go", "package main")

	layers := []extension.LayerDef{
		{Name: "project", Patterns: []string{"**"}, CatchAll: true},
	}

	// Use only the actual DefaultIgnorePatterns — .git should NOT be in them
	s := NewScanner(dir, layers, config.DefaultIgnorePatterns, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	files := result["project"].WorkspaceFiles
	hasGitHead := false
	hasGitConfig := false
	for _, f := range files {
		if f == ".git/HEAD" {
			hasGitHead = true
		}
		if f == ".git/config" {
			hasGitConfig = true
		}
	}
	if !hasGitHead {
		t.Error(".git/HEAD should be included in scan (not ignored)")
	}
	if !hasGitConfig {
		t.Error(".git/config should be included in scan (not ignored)")
	}
}

func TestScanEnvAndDbNotIgnoredByDefault(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, ".env", "SECRET=foo")
	createFile(t, dir, ".env.local", "LOCAL=bar")
	createFile(t, dir, "app.sqlite", "sqlite data")
	createFile(t, dir, "data.db", "db data")
	createFile(t, dir, "creds.pem", "pem data")

	layers := []extension.LayerDef{
		{Name: "project", Patterns: []string{"**"}, CatchAll: true},
	}

	s := NewScanner(dir, layers, config.DefaultIgnorePatterns, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	files := make(map[string]bool)
	for _, f := range result["project"].WorkspaceFiles {
		files[f] = true
	}

	for _, expected := range []string{".env", ".env.local", "app.sqlite", "data.db", "creds.pem"} {
		if !files[expected] {
			t.Errorf("%s should be included in scan (not ignored)", expected)
		}
	}
}

func TestScanWithNilNormalizePath(t *testing.T) {
	dir := t.TempDir()
	extDir := t.TempDir()

	createFile(t, extDir, "session.jsonl", "data")

	layers := []extension.LayerDef{
		{Name: "agent", Patterns: []string{extDir + "/"}},
	}

	// nil normalizer = no transformation
	s := NewScanner(dir, layers, nil, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	// Verify archive paths use the real portable path (no placeholder)
	portableExtDir := extension.PortablePath(extDir)
	for _, ef := range result["agent"].ExternalFiles {
		if !strings.Contains(ef.ArchivePath, portableExtDir) {
			t.Errorf("archive path should contain real portable path when normalizer is nil: %s", ef.ArchivePath)
		}
	}
}
