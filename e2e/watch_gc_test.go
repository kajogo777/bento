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
// TestWatchGCPrunesAfterSave: regression test for stale OCI index in watch GC.
//
// The bug: watch opened a single oras oci.Store at startup for GC. That store
// loaded the OCI index into memory once and never re-read from disk. When
// ExecuteSave (which opens its own store) created new checkpoints, the watch
// store's ListCheckpoints still returned the stale startup-time index. GC ran
// after every save but against the old view, so it never found anything to
// prune.
//
// This test verifies that bento watch actually prunes old checkpoints after
// a successful auto-save, using an aggressive retention policy that makes
// the pruning observable within seconds.
// ---------------------------------------------------------------------------

func TestWatchGCPrunesAfterSave(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()
	// Resolve macOS symlinks (/var -> /private/var).
	dir, _ = filepath.EvalSymlinks(dir)
	storeDir, _ = filepath.EvalSymlinks(storeDir)

	// Configure aggressive retention:
	//   tier 1: <3s  → keep all (protects the very newest checkpoint)
	//   tier 2: <1h, resolution 30m → keep one per 30min bucket
	//
	// Checkpoints created by watch that age past the 3s boundary fall into
	// tier 2. Multiple checkpoints in the same 30min bucket get pruned down
	// to the newest.
	bentoYAML := `id: ws-test-watch-gc
store: ` + storeDir + `
retention:
    tiers:
        - max_age: 3s
        - max_age: 1h
          resolution: 30m
watch:
    debounce: 1
    message: auto-save
`
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Seed workspace files.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "README.md", "# Test\n")

	// Do a single initial save so the workspace has a parent checkpoint.
	run(t, dir, "save", "--skip-secret-scan")

	// Start bento watch — at this point the store has only cp-1.
	// The watch opens its oras oci.Store which loads the index into memory.
	cmd := exec.Command(bento, "watch", "--debounce", "1", "--skip-secret-scan")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+os.Getenv("HOME"))

	outBuf := &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bento watch: %v", err)
	}

	// Give the watcher time to initialize.
	time.Sleep(2 * time.Second)

	// Trigger multiple saves through the watch by making successive changes.
	// Each change → debounce (1s) → ExecuteSave creates a new checkpoint.
	// These new checkpoints are written to the on-disk OCI index by
	// ExecuteSave's own store instance.
	for i := 0; i < 4; i++ {
		writeFile(t, dir, "file.txt", strings.Repeat("x", i+1)+"\n")
		// Wait for debounce (1s) + save time + small buffer.
		time.Sleep(3 * time.Second)
	}

	// At this point, watch has created cp-2..cp-5 (4 new checkpoints).
	// The earliest ones are now >3s old (outside tier 1) and fall into tier 2.
	// They all share the same 30min bucket → GC should prune all but the newest.
	//
	// THE BUG: If the watch's store instance has a stale index, it only sees
	// cp-1 (from startup). It doesn't know about cp-2..cp-5, so TieredGC
	// finds nothing to prune. The "gc: pruned" message never appears.

	// One final change to trigger a save+GC cycle after the old checkpoints
	// have aged past the tier-1 boundary.
	time.Sleep(4 * time.Second)
	writeFile(t, dir, "file.txt", "final trigger\n")
	time.Sleep(5 * time.Second)

	// Stop the watcher.
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	output := outBuf.String()
	t.Logf("watch output:\n%s", output)

	// Verify the watch created checkpoints.
	cpCount := strings.Count(output, "OK cp-")
	if cpCount < 3 {
		t.Fatalf("expected watch to create at least 3 checkpoints, got %d:\n%s", cpCount, output)
	}

	// The key assertion: at least one GC cycle should have pruned checkpoints.
	// Without the fix, the stale store sees only the initial cp-1 and never
	// finds anything to prune — "gc: pruned" never appears.
	if !strings.Contains(output, "gc: pruned") {
		t.Errorf("REGRESSION: watch GC did not prune any checkpoints.\n"+
			"This means TieredGC is operating on a stale OCI index.\n"+
			"The oras oci.Store loads the index into memory at construction\n"+
			"time and doesn't see checkpoints created by ExecuteSave.\n"+
			"Watch output:\n%s", output)
	}
}
