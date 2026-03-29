//go:build integration

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestWatchAutoSave: bento watch detects file changes and auto-saves
// ---------------------------------------------------------------------------

func TestWatchAutoSave(t *testing.T) {
	dir := makeWorkspace(t)

	// Start bento watch in the background with a short debounce.
	cmd := exec.Command(bento, "watch", "--debounce", "2", "--skip-secret-scan")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+os.Getenv("HOME"))

	outBuf := &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bento watch: %v", err)
	}

	// Give the watcher time to initialize.
	time.Sleep(1 * time.Second)

	// Modify a file — this should trigger an auto-save after debounce.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"hello\") }\n")

	// Wait for debounce (2s) + save time + buffer.
	time.Sleep(5 * time.Second)

	// Kill the watcher.
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	output := outBuf.String()
	t.Logf("watch output:\n%s", output)

	// Verify a checkpoint was created.
	if !strings.Contains(output, "cp-") {
		t.Errorf("expected auto-save checkpoint in output, got:\n%s", output)
	}

	// Verify with bento list.
	listOut := run(t, dir, "list")
	if !strings.Contains(listOut, "cp-1") {
		t.Errorf("expected cp-1 in list output, got:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestWatchSkipUnchanged: bento watch skips save when nothing changed
// ---------------------------------------------------------------------------

func TestWatchSkipUnchanged(t *testing.T) {
	dir := makeWorkspace(t)

	// Do an initial save so there's a parent to compare against.
	run(t, dir, "save", "--skip-secret-scan")

	// Start bento watch — don't modify any files.
	cmd := exec.Command(bento, "watch", "--debounce", "1", "--skip-secret-scan")
	cmd.Dir = dir

	outBuf := &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bento watch: %v", err)
	}

	// Wait a bit, then touch a file (mtime change but no content change won't
	// trigger fsnotify Write; we need an actual write to trigger the event).
	time.Sleep(1 * time.Second)

	// Write the exact same content — packing will produce identical digests.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")

	time.Sleep(4 * time.Second)

	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	output := outBuf.String()
	t.Logf("watch output:\n%s", output)

	// Should see the skip message.
	if !strings.Contains(output, "no changes") {
		t.Errorf("expected skip-if-unchanged message, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// TestWatchConcurrentSave: manual bento save while watch is running
// ---------------------------------------------------------------------------

func TestWatchConcurrentSave(t *testing.T) {
	dir := makeWorkspace(t)

	// Start bento watch.
	cmd := exec.Command(bento, "watch", "--debounce", "2", "--skip-secret-scan")
	cmd.Dir = dir

	outBuf := &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bento watch: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Run a manual save while watch is running.
	manualOut := run(t, dir, "save", "--skip-secret-scan", "-m", "manual save")
	if !strings.Contains(manualOut, "cp-") {
		t.Errorf("manual save should succeed, got:\n%s", manualOut)
	}

	// Modify a file to trigger a watch auto-save.
	writeFile(t, dir, "newfile.txt", "hello from watch test\n")
	time.Sleep(5 * time.Second)

	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	// Verify both saves succeeded by checking list.
	listOut := run(t, dir, "list")
	if !strings.Contains(listOut, "manual save") {
		t.Errorf("list should contain manual save, got:\n%s", listOut)
	}
	// Should have at least 2 checkpoints.
	if strings.Count(listOut, "cp-") < 2 {
		t.Errorf("expected at least 2 checkpoints, got:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestWatchMultipleChanges: rapid edits produce a single debounced checkpoint
// ---------------------------------------------------------------------------

func TestWatchMultipleChanges(t *testing.T) {
	dir := makeWorkspace(t)

	cmd := exec.Command(bento, "watch", "--debounce", "2", "--skip-secret-scan")
	cmd.Dir = dir

	outBuf := &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bento watch: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Rapid-fire 5 edits within the debounce window.
	for i := 0; i < 5; i++ {
		writeFile(t, dir, "main.go", "package main\n\n// edit "+strings.Repeat("x", i)+"\nfunc main() {}\n")
		time.Sleep(200 * time.Millisecond)
	}

	// Wait for debounce (2s) + save + buffer.
	time.Sleep(5 * time.Second)

	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	output := outBuf.String()
	t.Logf("watch output:\n%s", output)

	// Should have exactly 1 checkpoint (debounced).
	cpCount := strings.Count(output, "✓ cp-")
	if cpCount != 1 {
		t.Errorf("expected 1 debounced checkpoint, got %d. output:\n%s", cpCount, output)
	}
}

// ---------------------------------------------------------------------------
// TestWatchSubdirectoryChanges: changes in subdirs are detected
// ---------------------------------------------------------------------------

func TestWatchSubdirectoryChanges(t *testing.T) {
	dir := makeWorkspace(t)

	cmd := exec.Command(bento, "watch", "--debounce", "2", "--skip-secret-scan")
	cmd.Dir = dir

	outBuf := &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bento watch: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Create a new file in a subdirectory.
	writeFile(t, dir, "pkg/utils/helper.go", "package utils\n\nfunc Helper() {}\n")

	time.Sleep(5 * time.Second)

	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	output := outBuf.String()
	t.Logf("watch output:\n%s", output)

	if !strings.Contains(output, "✓ cp-") {
		t.Errorf("expected checkpoint from subdirectory change, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// TestWatchIgnoredDirNoTrigger: changes in .git don't trigger saves
// ---------------------------------------------------------------------------

func TestWatchIgnoredDirNoTrigger(t *testing.T) {
	dir := makeWorkspace(t)

	cmd := exec.Command(bento, "watch", "--debounce", "1", "--skip-secret-scan")
	cmd.Dir = dir

	outBuf := &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bento watch: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Write to a .git directory (should be ignored).
	writeFile(t, dir, ".git/test-object", "fake git object data\n")

	time.Sleep(4 * time.Second)

	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	output := outBuf.String()
	t.Logf("watch output:\n%s", output)

	// Should NOT have any checkpoints — .git is ignored.
	if strings.Contains(output, "✓ cp-") {
		t.Errorf("expected no checkpoint from .git change, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// TestSaveSkipUnchanged: bento save skips when nothing changed
// ---------------------------------------------------------------------------

func TestSaveSkipUnchanged(t *testing.T) {
	dir := makeWorkspace(t)

	// First save.
	out1 := run(t, dir, "save", "--skip-secret-scan", "-m", "first")
	if !strings.Contains(out1, "cp-1") {
		t.Fatalf("first save should create cp-1, got:\n%s", out1)
	}

	// Second save without any changes.
	out2 := run(t, dir, "save", "--skip-secret-scan", "-m", "second")

	// Should skip — no new checkpoint.
	if !strings.Contains(out2, "No changes detected") {
		t.Errorf("expected skip message on unchanged save, got:\n%s", out2)
	}

	// List should only have 1 checkpoint.
	listOut := run(t, dir, "list")
	if strings.Contains(listOut, "cp-2") {
		t.Errorf("should not have cp-2 (nothing changed), list:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestSaveAfterChange: bento save works after actual changes
// ---------------------------------------------------------------------------

func TestSaveAfterChange(t *testing.T) {
	dir := makeWorkspace(t)

	run(t, dir, "save", "--skip-secret-scan", "-m", "first")

	// Make a real change.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"changed\") }\n")

	out := run(t, dir, "save", "--skip-secret-scan", "-m", "second")
	if !strings.Contains(out, "cp-2") {
		t.Errorf("expected cp-2 after real change, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestWatchCustomLayerWatchOff: layer with watch: off doesn't trigger saves
// ---------------------------------------------------------------------------

func TestWatchCustomLayerWatchOff(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	// Custom config with a layer set to watch: off.
	bentoYAML := `id: ws-test-watchoff
store: ` + storeDir + `
agent: custom
layers:
  - name: project
    patterns: ["src/**"]
    watch: realtime
  - name: build
    patterns: ["dist/**"]
    watch: "off"
`
	if err := os.WriteFile(dir+"/bento.yaml", []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "src/main.go", "package main\n")
	os.MkdirAll(dir+"/dist", 0755)

	cmd := exec.Command(bento, "watch", "--debounce", "1", "--skip-secret-scan")
	cmd.Dir = dir

	outBuf := &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bento watch: %v", err)
	}

	time.Sleep(1 * time.Second)

	// Write to dist/ (watch: off layer) — should NOT trigger save.
	writeFile(t, dir, "dist/bundle.js", "console.log('built')\n")
	time.Sleep(3 * time.Second)

	// Now write to src/ (watch: realtime) — SHOULD trigger save.
	writeFile(t, dir, "src/main.go", "package main\n\nfunc main() {}\n")
	time.Sleep(4 * time.Second)

	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	output := outBuf.String()
	t.Logf("watch output:\n%s", output)

	// Should have exactly 1 checkpoint (from src/ change, not dist/).
	cpCount := strings.Count(output, "✓ cp-")
	if cpCount != 1 {
		t.Errorf("expected 1 checkpoint (from src/ only), got %d. output:\n%s", cpCount, output)
	}
}

// ---------------------------------------------------------------------------
// TestWatchHelp: bento watch --help works
// ---------------------------------------------------------------------------

func TestWatchHelp(t *testing.T) {
	out := run(t, t.TempDir(), "watch", "--help")
	if !strings.Contains(out, "debounce") {
		t.Errorf("watch help should mention debounce flag, got:\n%s", out)
	}
	if !strings.Contains(out, "Ctrl-C") {
		t.Errorf("watch help should mention Ctrl-C, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestConfigValidation_BadWatchValue: rejects invalid watch value
// ---------------------------------------------------------------------------

func TestConfigValidation_BadWatchValue(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()

	// Resolve macOS symlinks (/var -> /private/var).
	dir, _ = filepath.EvalSymlinks(dir)

	bentoYAML := `id: ws-test-validation
store: ` + storeDir + `
agent: custom
layers:
  - name: project
    patterns: ["**"]
    watch: instant
`
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	out := runExpectFail(t, dir, "save", "--dir", dir, "--skip-secret-scan")
	if !strings.Contains(out, "invalid watch value") {
		t.Errorf("expected validation error for bad watch value, got:\n%s", out)
	}
}
