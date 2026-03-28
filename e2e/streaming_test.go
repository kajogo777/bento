//go:build integration

package e2e_test

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeLargeFile creates a file of exactly size bytes filled with pseudo-random
// text lines. The content is varied enough to prevent trivial gzip compression,
// ensuring the on-disk and in-memory representations are both large.
func writeLargeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	rng := rand.New(rand.NewSource(42))
	written := 0
	// Each line is ~80 bytes of pseudo-random hex + newline.
	var line [80]byte
	for written < size {
		for i := range line[:79] {
			line[i] = "0123456789abcdef"[rng.Intn(16)]
		}
		line[79] = '\n'
		n, err := f.Write(line[:])
		if err != nil {
			t.Fatal(err)
		}
		written += n
	}
}

// runMeasured executes a bento command in dir, returning the combined output,
// elapsed wall-clock time, and peak RSS in bytes. The test fails on non-zero
// exit code.
func runMeasured(t *testing.T, dir string, args ...string) (out string, elapsed time.Duration, peakRSSBytes int64) {
	t.Helper()
	cmd := exec.Command(bento, args...)
	cmd.Dir = dir
	start := time.Now()
	combined, err := cmd.CombinedOutput()
	elapsed = time.Since(start)
	if err != nil {
		t.Fatalf("bento %s failed (%v):\n%s", strings.Join(args, " "), err, combined)
	}
	peakRSSBytes = maxRSS(cmd)
	return string(combined), elapsed, peakRSSBytes
}

// maxRSS returns the peak resident-set size of a completed process in bytes.
// Returns 0 if the platform does not support it.
func maxRSS(cmd *exec.Cmd) int64 {
	if cmd.ProcessState == nil {
		return 0
	}
	ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}
	// Linux reports Maxrss in kilobytes; macOS/BSD reports bytes.
	if runtime.GOOS == "linux" {
		return ru.Maxrss * 1024
	}
	return ru.Maxrss
}

// makeLargeWorkspace creates a workspace containing one large file.
// fileSize is in bytes. Returns the workspace directory.
func makeLargeWorkspace(t *testing.T, fileSize int) string {
	t.Helper()
	dir := makeWorkspace(t) // uses existing helper (sets up bento.yaml + small files)
	writeLargeFile(t, filepath.Join(dir, "large_data.txt"), fileSize)
	return dir
}

// assertRSSBounded fails the test if peakRSSBytes exceeds maxMB megabytes,
// providing a diagnostic message.
func assertRSSBounded(t *testing.T, peakRSSBytes int64, maxMB int, context string) {
	t.Helper()
	if peakRSSBytes == 0 {
		t.Logf("warning: could not measure peak RSS for %s (platform unsupported)", context)
		return
	}
	limitBytes := int64(maxMB) << 20
	if peakRSSBytes > limitBytes {
		t.Errorf("%s: peak RSS %d MiB exceeded limit %d MiB — possible in-memory file loading",
			context, peakRSSBytes>>20, maxMB)
	}
}

// ---------------------------------------------------------------------------
// TestLargeFileSave: saving a 100 MiB workspace must not load it into memory
// ---------------------------------------------------------------------------

func TestLargeFileSave(t *testing.T) {
	const fileMB = 100
	dir := makeLargeWorkspace(t, fileMB<<20)

	_, elapsed, rss := runMeasured(t, dir, "save", "--skip-secret-scan")

	t.Logf("save %d MiB: wall=%v peakRSS=%d MiB", fileMB, elapsed.Round(time.Millisecond), rss>>20)

	// Peak RSS must stay well below the file size. If the implementation
	// loaded the entire layer into memory, RSS would exceed fileMB.
	// We allow 3× Go runtime overhead (typically ~30-40 MiB) above what
	// a streaming implementation needs — but still far below fileMB.
	assertRSSBounded(t, rss, 90, fmt.Sprintf("save %d MiB file", fileMB))
}

// ---------------------------------------------------------------------------
// TestLargeFileOpen: restoring a 100 MiB checkpoint must not load it into memory
// ---------------------------------------------------------------------------

func TestLargeFileOpen(t *testing.T) {
	const fileMB = 100
	dir := makeLargeWorkspace(t, fileMB<<20)

	// Save first (pre-condition).
	run(t, dir, "save", "--skip-secret-scan")

	// Delete the large file so open has real work to do.
	if err := os.Remove(filepath.Join(dir, "large_data.txt")); err != nil {
		t.Fatal(err)
	}

	_, elapsed, rss := runMeasured(t, dir, "open", "latest")

	t.Logf("open %d MiB: wall=%v peakRSS=%d MiB", fileMB, elapsed.Round(time.Millisecond), rss>>20)

	assertRSSBounded(t, rss, 90, fmt.Sprintf("open %d MiB checkpoint", fileMB))

	// Verify the file was actually restored.
	if _, err := os.Stat(filepath.Join(dir, "large_data.txt")); err != nil {
		t.Errorf("large_data.txt not restored: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestLargeFileDiff: diffing a workspace with a 100 MiB file must be memory-bounded
// ---------------------------------------------------------------------------

func TestLargeFileDiff(t *testing.T) {
	const fileMB = 100
	dir := makeLargeWorkspace(t, fileMB<<20)

	// Save so there is a checkpoint to diff against.
	run(t, dir, "save", "--skip-secret-scan")

	// Append a few lines to the large file to create a real change.
	f, err := os.OpenFile(filepath.Join(dir, "large_data.txt"), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintln(f, "modified line appended for diff test")
	_ = f.Close()

	_, elapsed, rss := runMeasured(t, dir, "diff")

	t.Logf("diff %d MiB: wall=%v peakRSS=%d MiB", fileMB, elapsed.Round(time.Millisecond), rss>>20)

	assertRSSBounded(t, rss, 90, fmt.Sprintf("diff %d MiB file", fileMB))
}

// ---------------------------------------------------------------------------
// TestLargeFileCheckpointDiff: comparing two checkpoints with a 100 MiB layer
// ---------------------------------------------------------------------------

func TestLargeFileCheckpointDiff(t *testing.T) {
	const fileMB = 100
	dir := makeLargeWorkspace(t, fileMB<<20)

	run(t, dir, "save", "--skip-secret-scan") // cp-1

	// Mutate and save again.
	writeLargeFile(t, filepath.Join(dir, "large_data.txt"), fileMB<<20)
	run(t, dir, "save", "--skip-secret-scan") // cp-2

	_, elapsed, rss := runMeasured(t, dir, "diff", "cp-1", "cp-2")

	t.Logf("diff cp-1..cp-2 %d MiB: wall=%v peakRSS=%d MiB", fileMB, elapsed.Round(time.Millisecond), rss>>20)

	assertRSSBounded(t, rss, 90, fmt.Sprintf("checkpoint diff %d MiB layers", fileMB))
}

// ---------------------------------------------------------------------------
// TestStreamingScaling: RSS must not scale linearly with file size
//
// Saves two workspaces — one with a 50 MiB file, one with a 200 MiB file —
// and asserts that peak RSS of the larger save is less than 2× the smaller.
// A non-streaming implementation would show ~4× scaling.
// ---------------------------------------------------------------------------

func TestStreamingScaling(t *testing.T) {
	sizes := []int{50, 200} // MiB
	var rssMeasurements []int64

	for _, mb := range sizes {
		dir := makeLargeWorkspace(t, mb<<20)
		_, elapsed, rss := runMeasured(t, dir, "save", "--skip-secret-scan")
		t.Logf("save %d MiB: wall=%v peakRSS=%d MiB", mb, elapsed.Round(time.Millisecond), rss>>20)
		rssMeasurements = append(rssMeasurements, rss)
	}

	if rssMeasurements[0] == 0 || rssMeasurements[1] == 0 {
		t.Skip("RSS measurement not supported on this platform")
	}

	// RSS for the 200 MiB case should be less than 2× the 50 MiB case.
	// A streaming implementation should show roughly constant RSS regardless
	// of file size. We allow 2× headroom for GC/runtime variance.
	ratio := float64(rssMeasurements[1]) / float64(rssMeasurements[0])
	t.Logf("RSS scaling ratio (200MiB / 50MiB): %.2f×", ratio)
	if ratio > 2.0 {
		t.Errorf("RSS scaled %.2f× from 50MiB to 200MiB (expected <2×) — possible in-memory file loading", ratio)
	}
}
