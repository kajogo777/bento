package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/kajogo777/bento/internal/extension"
)

func TestPeriodicDirsFromLayer_WorkspaceDirs(t *testing.T) {
	dir := t.TempDir()
	// Create dirs that match patterns.
	_ = os.MkdirAll(filepath.Join(dir, "node_modules"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, ".venv"), 0755)

	layer := extension.LayerDef{
		Name:        "deps",
		Patterns:    []string{"node_modules/**", ".venv/**", "*.go"},
		WatchMethod: extension.WatchPeriodic,
	}

	dirs := periodicDirsFromLayer(dir, layer)
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d: %v", len(dirs), dirs)
	}
}

func TestPeriodicDirsFromLayer_MissingDir(t *testing.T) {
	dir := t.TempDir()
	// Don't create node_modules — should be skipped.
	layer := extension.LayerDef{
		Name:        "deps",
		Patterns:    []string{"node_modules/**"},
		WatchMethod: extension.WatchPeriodic,
	}

	dirs := periodicDirsFromLayer(dir, layer)
	if len(dirs) != 0 {
		t.Fatalf("expected 0 dirs for missing dir, got %d: %v", len(dirs), dirs)
	}
}

func TestPeriodicDirsFromLayer_GlobOnly(t *testing.T) {
	dir := t.TempDir()
	// Patterns with wildcards in the directory part are not extractable.
	layer := extension.LayerDef{
		Name:        "custom",
		Patterns:    []string{"**/*.go", "src/**/test"},
		WatchMethod: extension.WatchPeriodic,
	}

	dirs := periodicDirsFromLayer(dir, layer)
	if len(dirs) != 0 {
		t.Fatalf("expected 0 dirs for glob-only patterns, got %d: %v", len(dirs), dirs)
	}
}

func TestLayerFingerprint_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	h, err := layerFingerprint(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h == 0 {
		t.Log("fingerprint is zero — unusual but not an error for empty dir")
	}
}

func TestLayerFingerprint_NonExistent(t *testing.T) {
	h, err := layerFingerprint("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("nonexistent dir should return 0, not error: %v", err)
	}
	if h != 0 {
		t.Errorf("expected 0 for nonexistent dir, got %d", h)
	}
}

func TestLayerFingerprint_DetectsChange(t *testing.T) {
	dir := t.TempDir()

	h1, _ := layerFingerprint(dir)

	// Add a file.
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0644)
	// Need a small sleep to ensure mtime differs.
	time.Sleep(10 * time.Millisecond)

	h2, _ := layerFingerprint(dir)

	if h1 == h2 {
		t.Error("fingerprint should change after adding a file")
	}
}

func TestIsRelevantEvent(t *testing.T) {
	cases := []struct {
		name string
		op   fsnotify.Op
		want bool
	}{
		{"Create", fsnotify.Create, true},
		{"Write", fsnotify.Write, true},
		{"Remove", fsnotify.Remove, true},
		{"Rename", fsnotify.Rename, true},
		{"Chmod", fsnotify.Chmod, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := fsnotify.Event{Name: "test.txt", Op: tc.op}
			if got := isRelevantEvent(e); got != tc.want {
				t.Errorf("isRelevantEvent(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsPeriodicDir(t *testing.T) {
	base := filepath.FromSlash("/workspace")
	w := &Watcher{
		periodicDirs: []string{
			filepath.Join(base, "node_modules"),
			filepath.Join(base, ".venv"),
		},
	}

	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(base, "node_modules"), true},
		{filepath.Join(base, "node_modules", "express"), true},
		{filepath.Join(base, ".venv"), true},
		{filepath.Join(base, ".venv", "lib", "python3.12"), true},
		{filepath.Join(base, "src"), false},
		{filepath.Join(base, "node_modulesX"), false}, // not a prefix match on path separator
	}

	for _, tc := range cases {
		if got := w.isPeriodicDir(tc.path); got != tc.want {
			t.Errorf("isPeriodicDir(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
