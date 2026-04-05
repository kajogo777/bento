//go:build integration

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// bento holds the path to the compiled binary under test.
var bento string

func TestMain(m *testing.M) {
	// Build the binary into a temp dir so the test is always current.
	tmp, err := os.MkdirTemp("", "bento-e2e-bin-*")
	if err != nil {
		panic("MkdirTemp: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	bento = filepath.Join(tmp, "bento")
	out, err := exec.Command("go", "build", "-o", bento, "../cmd/bento").CombinedOutput()
	if err != nil {
		panic("build failed: " + string(out))
	}

	os.Exit(m.Run())
}

// run executes the bento binary with the given arguments in dir,
// returning combined stdout+stderr.  The test fails immediately on error.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bento, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bento %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runExpectFail(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bento, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("bento %s should have failed but succeeded:\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}



// makeWorkspace creates a temporary workspace with a bento.yaml and some files.
func makeWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Point store at a temp dir so tests don't pollute the real store.
	storeDir := t.TempDir()

	// Write bento.yaml manually so we can control the store path.
	bentoYAML := "store: " + storeDir + "\n"
	if err := os.WriteFile(filepath.Join(dir, "bento.yaml"), []byte(bentoYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Seed some workspace files.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "README.md", "# My Project\n")
	writeFile(t, dir, "src/lib.go", "package main\n")

	return dir
}

func writeFile(t *testing.T, base, rel, content string) {
	t.Helper()
	full := filepath.Join(base, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// TestSaveList: init + save + list
// ---------------------------------------------------------------------------

func TestSaveList(t *testing.T) {
	dir := makeWorkspace(t)

	out := run(t, dir, "save", "--skip-secret-scan", "-m", "first save")

	if !strings.Contains(out, "cp-1") {
		t.Errorf("save output should mention cp-1, got:\n%s", out)
	}
	if !strings.Contains(out, "Secret scan: off") {
		t.Errorf("save output should mention secret scan off, got:\n%s", out)
	}

	listOut := run(t, dir, "list")
	if !strings.Contains(listOut, "cp-1") {
		t.Errorf("list output should show cp-1, got:\n%s", listOut)
	}
	if !strings.Contains(listOut, "first save") {
		t.Errorf("list output should show message, got:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestSaveMultiple: sequential saves produce increasing cp tags
// ---------------------------------------------------------------------------

func TestSaveMultiple(t *testing.T) {
	dir := makeWorkspace(t)

	run(t, dir, "save", "--skip-secret-scan", "-m", "first")
	writeFile(t, dir, "newfile.txt", "hello")
	run(t, dir, "save", "--skip-secret-scan", "-m", "second")

	listOut := run(t, dir, "list")
	if !strings.Contains(listOut, "cp-1") {
		t.Errorf("expected cp-1 in list, got:\n%s", listOut)
	}
	if !strings.Contains(listOut, "cp-2") {
		t.Errorf("expected cp-2 in list, got:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestInspect: save + inspect shows checkpoint metadata
// ---------------------------------------------------------------------------

func TestInspect(t *testing.T) {
	dir := makeWorkspace(t)

	run(t, dir, "save", "--skip-secret-scan", "-m", "inspect me")

	// Default: summary only (no file listing).
	out := run(t, dir, "inspect")
	if !strings.Contains(out, "sequence 1") {
		t.Errorf("inspect should show sequence 1, got:\n%s", out)
	}
	if !strings.Contains(out, "inspect me") {
		t.Errorf("inspect should show message, got:\n%s", out)
	}
	if !strings.Contains(out, "Total size:") {
		t.Errorf("inspect should show total size, got:\n%s", out)
	}
	// File names should NOT appear without --files.
	if strings.Contains(out, "main.go") {
		t.Errorf("inspect without --files should not list individual files, got:\n%s", out)
	}

	// With --files: show file listing.
	outFiles := run(t, dir, "inspect", "--files")
	if !strings.Contains(outFiles, "main.go") {
		t.Errorf("inspect --files should list main.go, got:\n%s", outFiles)
	}
}

// ---------------------------------------------------------------------------
// TestSaveOpenRoundtrip: files restored exactly
// ---------------------------------------------------------------------------

func TestSaveOpenRoundtrip(t *testing.T) {
	src := makeWorkspace(t)
	run(t, src, "save", "--skip-secret-scan", "-m", "roundtrip")

	dst := t.TempDir()
	run(t, src, "open", "cp-1", dst)

	// Verify each file is present and identical.
	for _, rel := range []string{"main.go", "README.md", "src/lib.go"} {
		srcData, err := os.ReadFile(filepath.Join(src, rel))
		if err != nil {
			t.Fatalf("reading src %s: %v", rel, err)
		}
		dstData, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Fatalf("reading dst %s: %v", rel, err)
		}
		if string(srcData) != string(dstData) {
			t.Errorf("file %s mismatch after restore:\nsrc: %q\ndst: %q", rel, srcData, dstData)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDiffClean: no changes after save reports clean diff
// ---------------------------------------------------------------------------

func TestDiffClean(t *testing.T) {
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan")

	out := run(t, dir, "diff")
	// A clean workspace should produce no diff output or explicitly say "no changes"
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			t.Errorf("clean diff should have no +/- lines, got: %q", line)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDiffModified: modified file shows up in diff
// ---------------------------------------------------------------------------

func TestDiffModified(t *testing.T) {
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan")

	// Modify a file
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { println(\"changed\") }\n")

	out := run(t, dir, "diff")
	if !strings.Contains(out, "main.go") {
		t.Errorf("diff should mention modified main.go, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestDiffAdded: new file shows as added
// ---------------------------------------------------------------------------

func TestDiffAdded(t *testing.T) {
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan")

	writeFile(t, dir, "newfile.txt", "brand new\n")

	out := run(t, dir, "diff")
	if !strings.Contains(out, "newfile.txt") {
		t.Errorf("diff should mention added newfile.txt, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestTag: custom tag is preserved
// ---------------------------------------------------------------------------

func TestTag(t *testing.T) {
	dir := makeWorkspace(t)

	run(t, dir, "save", "--skip-secret-scan", "--tag", "my-tag", "-m", "tagged save")

	listOut := run(t, dir, "list")
	if !strings.Contains(listOut, "my-tag") {
		t.Errorf("list should show custom tag my-tag, got:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestGC: garbage collection respects keep_last
// ---------------------------------------------------------------------------

func TestGC(t *testing.T) {
	dir := makeWorkspace(t)

	// Save 3 checkpoints
	for i := 0; i < 3; i++ {
		writeFile(t, dir, "file.txt", strings.Repeat("x", i+1))
		run(t, dir, "save", "--skip-secret-scan")
	}

	listBefore := run(t, dir, "list")
	cpCount := strings.Count(listBefore, "cp-")
	if cpCount < 3 {
		t.Fatalf("expected at least 3 checkpoints before GC, found output:\n%s", listBefore)
	}

	// GC keeping only 1
	gcOut := run(t, dir, "gc", "--keep-last", "1")
	if !strings.Contains(gcOut, "Deleted") && !strings.Contains(gcOut, "deleted") && !strings.Contains(gcOut, "2") {
		t.Logf("gc output: %s", gcOut)
	}

	listAfter := run(t, dir, "list")
	// After GC with keep-last=1, should have at most 2 entries (cp-3 + latest tag)
	remaining := countCheckpointLines(listAfter)
	if remaining > 2 {
		t.Errorf("after GC keep-last=1, expected ≤2 entries, got %d:\n%s", remaining, listAfter)
	}
}

// ---------------------------------------------------------------------------
// TestSaveAfterGC_SequenceMonotonic: sequence numbers never go backwards
// after GC prunes old checkpoints. Regression test for the bug where
// seq was computed as len(unique digests)+1, causing tag collisions after GC.
// ---------------------------------------------------------------------------

func TestSaveAfterGC_SequenceMonotonic(t *testing.T) {
	dir := makeWorkspace(t)

	// Save 3 checkpoints with different content.
	for i := 0; i < 3; i++ {
		writeFile(t, dir, "file.txt", fmt.Sprintf("content-%d", i+1))
		out := run(t, dir, "save", "--skip-secret-scan", "-m", fmt.Sprintf("save-%d", i+1))
		expected := fmt.Sprintf("cp-%d", i+1)
		if !strings.Contains(out, expected) {
			t.Fatalf("save %d: expected %s in output, got:\n%s", i+1, expected, out)
		}
	}

	// Verify we have cp-1, cp-2, cp-3.
	listBefore := run(t, dir, "list")
	for i := 1; i <= 3; i++ {
		tag := fmt.Sprintf("cp-%d", i)
		if !strings.Contains(listBefore, tag) {
			t.Errorf("expected %s before GC, list:\n%s", tag, listBefore)
		}
	}

	// GC keeping only the last 1. Due to same-second timestamps, which
	// specific checkpoint survives is non-deterministic, but the key
	// invariant is: the next save must produce a tag HIGHER than any
	// surviving cp-N tag.
	run(t, dir, "gc", "--keep-last", "1")

	listAfterGC := run(t, dir, "list")
	t.Logf("list after GC:\n%s", listAfterGC)

	// Find the highest surviving cp-N tag.
	highestSurviving := 0
	for i := 1; i <= 3; i++ {
		if strings.Contains(listAfterGC, fmt.Sprintf("cp-%d", i)) {
			highestSurviving = i
		}
	}
	if highestSurviving == 0 {
		t.Fatalf("no cp-N tags survived GC:\n%s", listAfterGC)
	}

	// Count unique digests after GC — this is what the old buggy code used.
	// If the old code would produce a LOWER sequence than our fix, the test
	// validates the regression.
	digestCount := countCheckpointLines(listAfterGC)

	// Save a new checkpoint.
	writeFile(t, dir, "file.txt", "after-gc-content")
	out := run(t, dir, "save", "--skip-secret-scan", "-m", "after gc")

	expectedTag := fmt.Sprintf("cp-%d", highestSurviving+1)
	if !strings.Contains(out, expectedTag) {
		t.Errorf("save after GC should produce %s (monotonic from highest surviving tag), got:\n%s", expectedTag, out)
	}

	// The old buggy code would have produced cp-(digestCount+1). If that
	// differs from our expected tag, the test is actually catching the bug.
	buggyTag := fmt.Sprintf("cp-%d", digestCount+1)
	if buggyTag != expectedTag {
		t.Logf("regression validated: old code would produce %s, fixed code produces %s", buggyTag, expectedTag)
	}

	// Save once more to confirm continued monotonic behavior.
	writeFile(t, dir, "file.txt", "after-gc-content-2")
	out2 := run(t, dir, "save", "--skip-secret-scan", "-m", "after gc 2")
	expectedTag2 := fmt.Sprintf("cp-%d", highestSurviving+2)
	if !strings.Contains(out2, expectedTag2) {
		t.Errorf("second save after GC should produce %s, got:\n%s", expectedTag2, out2)
	}
}

// countCheckpointLines counts lines that look like checkpoint entries (non-header, non-empty).
func countCheckpointLines(listOutput string) int {
	count := 0
	for _, line := range strings.Split(listOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "TAG") || strings.HasPrefix(line, "No checkpoints") {
			continue
		}
		count++
	}
	return count
}

// ---------------------------------------------------------------------------
// TestIntegrityCheck: corrupting a blob causes restore to fail
// ---------------------------------------------------------------------------

func TestIntegrityCheck(t *testing.T) {
	src := makeWorkspace(t)
	run(t, src, "save", "--skip-secret-scan", "-m", "integrity test")

	// Find the OCI store path from bento.yaml (re-read after save since
	// save may have generated a workspace ID via migration).
	data, err := os.ReadFile(filepath.Join(src, "bento.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	storeRoot := extractYAMLField(string(data), "store")
	wsID := extractYAMLField(string(data), "id")
	if storeRoot == "" || wsID == "" {
		t.Fatalf("could not parse store/id from bento.yaml:\n%s", data)
	}

	storePath := filepath.Join(storeRoot, wsID, "blobs", "sha256")

	entries, err := os.ReadDir(storePath)
	if err != nil {
		t.Fatalf("reading blobs dir %s: %v", storePath, err)
	}

	// Find a layer blob (not the manifest or config — those are small JSON, layers are bigger)
	var layerBlob string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fi, _ := e.Info()
		// Layer blobs are gzip-compressed tar archives — larger than config/manifest JSON
		if fi.Size() > 500 {
			layerBlob = filepath.Join(storePath, e.Name())
			break
		}
	}
	if layerBlob == "" {
		t.Skip("no layer blob found to corrupt")
	}

	// OCI blob files are read-only; make writable before corrupting.
	if err := os.Chmod(layerBlob, 0644); err != nil {
		t.Fatalf("chmod blob: %v", err)
	}
	blobData, err := os.ReadFile(layerBlob)
	if err != nil {
		t.Fatal(err)
	}
	blobData[0] ^= 0xFF
	blobData[1] ^= 0xFF
	if err := os.WriteFile(layerBlob, blobData, 0644); err != nil {
		t.Fatal(err)
	}

	// Restore should fail with an integrity error
	dst := t.TempDir()
	out := runExpectFail(t, src, "open", "cp-1", dst)
	if !strings.Contains(strings.ToLower(out), "integrity") &&
		!strings.Contains(strings.ToLower(out), "digest") &&
		!strings.Contains(strings.ToLower(out), "corrupt") &&
		!strings.Contains(strings.ToLower(out), "mismatch") {
		t.Errorf("expected integrity error, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestFork: fork creates a new checkpoint derived from an existing one
// ---------------------------------------------------------------------------

func TestOpenIntoNewDir(t *testing.T) {
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "base")

	// Open cp-1 into a new directory (replaces old fork command)
	newDir := t.TempDir()
	run(t, dir, "open", "cp-1", newDir)

	// Verify files are there
	if _, err := os.Stat(filepath.Join(newDir, "main.go")); err != nil {
		t.Errorf("opened directory should contain main.go: %v", err)
	}

	// Verify workspace ID is preserved (same store)
	srcYAML, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	dstYAML, _ := os.ReadFile(filepath.Join(newDir, "bento.yaml"))
	srcID := extractYAMLField(string(srcYAML), "id")
	dstID := extractYAMLField(string(dstYAML), "id")
	if srcID != dstID {
		t.Errorf("opened workspace should preserve workspace ID: src=%s, dst=%s", srcID, dstID)
	}

	// Verify head is set to cp-1's digest
	dstHead := extractYAMLField(string(dstYAML), "head")
	if dstHead == "" {
		t.Error("opened workspace should have head set")
	}
}

// ---------------------------------------------------------------------------
// TestOpenIntoExistingDir: opening into a dir that already has bento.yaml
// ---------------------------------------------------------------------------

func TestOpenRestoresSpecificCheckpoint(t *testing.T) {
	dir := makeWorkspace(t)

	// Save cp-1 with original content
	run(t, dir, "save", "--skip-secret-scan", "-m", "v1")

	// Modify and save cp-2
	writeFile(t, dir, "main.go", "package main\n// v2\nfunc main() {}\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "v2")

	// Restore cp-1 into a fresh dir
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	content, err := os.ReadFile(filepath.Join(dst, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "// v2") {
		t.Errorf("cp-1 restore should not have v2 content, got: %q", content)
	}

	// Restore cp-2 into another fresh dir
	dst2 := t.TempDir()
	run(t, dir, "open", "cp-2", dst2)

	content2, err := os.ReadFile(filepath.Join(dst2, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content2), "// v2") {
		t.Errorf("cp-2 restore should have v2 content, got: %q", content2)
	}
}

// ---------------------------------------------------------------------------
// TestManifestContents: saved manifest has expected OCI structure
// ---------------------------------------------------------------------------

func TestManifestContents(t *testing.T) {
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "manifest check")

	// Parse bento.yaml store path and workspace ID (re-read after save
	// since save may have generated a workspace ID via migration).
	data, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	storeRoot := extractYAMLField(string(data), "store")
	wsID := extractYAMLField(string(data), "id")

	indexPath := filepath.Join(storeRoot, wsID, "index.json")

	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("reading index.json: %v", err)
	}

	var idx struct {
		Manifests []struct {
			Annotations map[string]string `json:"annotations"`
			Digest      string            `json:"digest"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(indexData, &idx); err != nil {
		t.Fatalf("parsing index.json: %v", err)
	}
	if len(idx.Manifests) == 0 {
		t.Fatal("index.json has no manifests")
	}

	// Read one manifest blob and verify it has the expected structure
	digestParts := strings.SplitN(idx.Manifests[0].Digest, ":", 2)
	if len(digestParts) != 2 {
		t.Fatalf("unexpected digest format: %q", idx.Manifests[0].Digest)
	}
	blobPath := filepath.Join(storeRoot, wsID, "blobs", digestParts[0], digestParts[1])
	blobData, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading manifest blob: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(blobData, &m); err != nil {
		t.Fatalf("parsing manifest blob: %v", err)
	}

	// Must have schemaVersion, artifactType, layers, annotations
	for _, field := range []string{"schemaVersion", "artifactType", "layers", "annotations"} {
		if _, ok := m[field]; !ok {
			t.Errorf("manifest missing field %q", field)
		}
	}

	var artifactType string
	_ = json.Unmarshal(m["artifactType"], &artifactType)
	if artifactType != "application/vnd.bento.workspace.v1" {
		t.Errorf("artifactType: got %q, want application/vnd.bento.workspace.v1", artifactType)
	}
}

// ---------------------------------------------------------------------------
// TestOpenGeneratesBentoYAML: open into empty dir generates bento.yaml
// ---------------------------------------------------------------------------

func TestOpenGeneratesBentoYAML(t *testing.T) {
	src := makeWorkspace(t)
	run(t, src, "save", "--skip-secret-scan", "-m", "portable")

	// Open into a fresh dir with no bento.yaml
	dst := t.TempDir()
	out := run(t, src, "open", "cp-1", dst)

	if !strings.Contains(out, "Generated bento.yaml") {
		t.Errorf("open should report generating bento.yaml, got:\n%s", out)
	}

	// bento.yaml must exist in the target
	bentoYAML, err := os.ReadFile(filepath.Join(dst, "bento.yaml"))
	if err != nil {
		t.Fatalf("bento.yaml not generated in target dir: %v", err)
	}

	content := string(bentoYAML)

	// Must have a workspace ID
	if !strings.Contains(content, "id: ws-") {
		t.Errorf("generated bento.yaml should have a workspace id, got:\n%s", content)
	}

	// Must have a store path
	if !strings.Contains(content, "store:") {
		t.Errorf("generated bento.yaml should have a store path, got:\n%s", content)
	}

	// Workspace ID must match the source (shared store, like git worktrees)
	srcYAML, _ := os.ReadFile(filepath.Join(src, "bento.yaml"))
	srcID := extractYAMLField(string(srcYAML), "id")
	dstID := extractYAMLField(content, "id")
	if srcID != dstID {
		t.Errorf("generated bento.yaml should preserve workspace id: src=%s, dst=%s", srcID, dstID)
	}

	// Head must be set
	dstHead := extractYAMLField(content, "head")
	if dstHead == "" {
		t.Error("generated bento.yaml should have head set")
	}
}

// ---------------------------------------------------------------------------
// TestOpenGeneratesBentoIgnore: open into empty dir generates .bentoignore
// ---------------------------------------------------------------------------

func TestOpenGeneratesBentoIgnore(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()

	// Write bento.yaml with ignore patterns
	bentoYAML := "store: " + storeDir + "\nignore:\n    - \"*.log\"\n    - tmp/\n"
	writeFile(t, dir, "bento.yaml", bentoYAML)
	writeFile(t, dir, "main.go", "package main\n")

	run(t, dir, "save", "--skip-secret-scan")

	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	ignoreData, err := os.ReadFile(filepath.Join(dst, ".bentoignore"))
	if err != nil {
		t.Fatalf(".bentoignore not generated in target dir: %v", err)
	}

	content := string(ignoreData)

	// Should contain the ignore patterns from bento.yaml
	for _, pattern := range []string{"*.log", "tmp/"} {
		if !strings.Contains(content, pattern) {
			t.Errorf(".bentoignore should contain %q, got:\n%s", pattern, content)
		}
	}
}

// ---------------------------------------------------------------------------
// TestOpenSkipsBentoYAMLWhenExists: open preserves existing bento.yaml
// ---------------------------------------------------------------------------

func TestOpenSkipsBentoYAMLWhenExists(t *testing.T) {
	src := makeWorkspace(t)
	run(t, src, "save", "--skip-secret-scan")

	// Create target dir with its own bento.yaml
	dst := t.TempDir()
	existingYAML := "store: /custom/store\nid: ws-existing\n"
	writeFile(t, dst, "bento.yaml", existingYAML)

	run(t, src, "open", "cp-1", dst)

	// bento.yaml should preserve id and store (head is updated by open)
	data, err := os.ReadFile(filepath.Join(dst, "bento.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "id: ws-existing") {
		t.Errorf("open should preserve existing workspace id, got:\n%s", content)
	}
	if !strings.Contains(content, "store: /custom/store") {
		t.Errorf("open should preserve existing store path, got:\n%s", content)
	}
	// head should be set after open
	if !strings.Contains(content, "head:") {
		t.Errorf("open should set head in existing bento.yaml, got:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// TestOpenThenSave: restored workspace can save immediately
// ---------------------------------------------------------------------------

func TestOpenThenSave(t *testing.T) {
	src := makeWorkspace(t)
	run(t, src, "save", "--skip-secret-scan", "-m", "original")

	// Open into fresh dir (generates bento.yaml)
	dst := t.TempDir()
	run(t, src, "open", "cp-1", dst)

	// Add a file and save from the restored workspace.
	// With preserved workspace ID, this continues the source's sequence (cp-2).
	writeFile(t, dst, "newfile.txt", "added after restore\n")
	out := run(t, dst, "save", "--skip-secret-scan", "-m", "continued")

	if !strings.Contains(out, "cp-2") {
		t.Errorf("save in restored workspace should produce cp-2 (continuing source sequence), got:\n%s", out)
	}

	// List should show both the original and continued checkpoints
	listOut := run(t, dst, "list")
	if !strings.Contains(listOut, "continued") {
		t.Errorf("list should show the new checkpoint message, got:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestPortableConfigRoundtrip: hooks, ignore, retention survive save→open
// ---------------------------------------------------------------------------

func TestPortableConfigRoundtrip(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()

	// Write a bento.yaml with hooks, ignore, and retention
	bentoYAML := strings.Join([]string{
		"store: " + storeDir,
		"task: test portable config",
		"ignore:",
		"    - \"*.log\"",
		"    - tmp/",
		"hooks:",
		"    post_restore: echo restored",
		"retention:",
		"    keep_last: 5",
		"    keep_tagged: true",
		"",
	}, "\n")
	writeFile(t, dir, "bento.yaml", bentoYAML)
	writeFile(t, dir, "main.go", "package main\n")

	run(t, dir, "save", "--skip-secret-scan", "-m", "with config")

	// Open into fresh dir
	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	data, err := os.ReadFile(filepath.Join(dst, "bento.yaml"))
	if err != nil {
		t.Fatalf("bento.yaml not generated: %v", err)
	}
	content := string(data)

	// Task should be preserved
	if !strings.Contains(content, "test portable config") {
		t.Errorf("task not preserved in generated bento.yaml:\n%s", content)
	}

	// Hooks should be preserved
	if !strings.Contains(content, "echo restored") {
		t.Errorf("post_restore hook not preserved in generated bento.yaml:\n%s", content)
	}

	// Ignore patterns should be preserved
	if !strings.Contains(content, "*.log") {
		t.Errorf("ignore pattern '*.log' not preserved in generated bento.yaml:\n%s", content)
	}

	// Retention should be preserved
	if !strings.Contains(content, "keep_last") || !strings.Contains(content, "5") {
		t.Errorf("retention keep_last not preserved in generated bento.yaml:\n%s", content)
	}
	if !strings.Contains(content, "keep_tagged") {
		t.Errorf("retention keep_tagged not preserved in generated bento.yaml:\n%s", content)
	}
}

// ---------------------------------------------------------------------------
// TestPortableConfigRemote: remote field survives save→open
// ---------------------------------------------------------------------------

func TestPortableConfigRemote(t *testing.T) {
	dir := t.TempDir()
	storeDir := t.TempDir()

	bentoYAML := "store: " + storeDir + "\nremote: ghcr.io/testorg/testrepo\n"
	writeFile(t, dir, "bento.yaml", bentoYAML)
	writeFile(t, dir, "main.go", "package main\n")

	run(t, dir, "save", "--skip-secret-scan")

	dst := t.TempDir()
	run(t, dir, "open", "cp-1", dst)

	data, err := os.ReadFile(filepath.Join(dst, "bento.yaml"))
	if err != nil {
		t.Fatalf("bento.yaml not generated: %v", err)
	}

	if !strings.Contains(string(data), "ghcr.io/testorg/testrepo") {
		t.Errorf("remote not preserved in generated bento.yaml:\n%s", string(data))
	}
}

// ---------------------------------------------------------------------------
// TestOpenThenSaveThenOpen: full round-trip chain
// ---------------------------------------------------------------------------

func TestOpenThenSaveThenOpen(t *testing.T) {
	// Create original workspace and save
	src := makeWorkspace(t)
	run(t, src, "save", "--skip-secret-scan", "-m", "original")

	// Open into workspace B
	wsB := t.TempDir()
	run(t, src, "open", "cp-1", wsB)

	// Modify and save in workspace B
	writeFile(t, wsB, "main.go", "package main\n// modified in B\nfunc main() {}\n")
	run(t, wsB, "save", "--skip-secret-scan", "-m", "modified in B")

	// Open workspace B's checkpoint into workspace C.
	// wsB's save is cp-2 (cp-1 was from src, shared store).
	wsC := t.TempDir()
	run(t, wsB, "open", "cp-2", wsC)

	// Verify the modification made it through
	content, err := os.ReadFile(filepath.Join(wsC, "main.go"))
	if err != nil {
		t.Fatalf("reading main.go from wsC: %v", err)
	}
	if !strings.Contains(string(content), "// modified in B") {
		t.Errorf("wsC should have wsB's modification, got: %q", content)
	}

	// Verify wsC shares the same workspace ID (same store, like git worktrees)
	dataB, _ := os.ReadFile(filepath.Join(wsB, "bento.yaml"))
	dataC, _ := os.ReadFile(filepath.Join(wsC, "bento.yaml"))
	idB := extractYAMLField(string(dataB), "id")
	idC := extractYAMLField(string(dataC), "id")
	if idB != idC {
		t.Errorf("workspace C should share workspace ID with B: B=%s, C=%s", idB, idC)
	}

	// Verify wsC can save
	writeFile(t, wsC, "extra.txt", "from C\n")
	out := run(t, wsC, "save", "--skip-secret-scan", "-m", "from C")
	if !strings.Contains(out, "cp-") {
		t.Errorf("save in wsC should produce a checkpoint, got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestHeadTracking: head field in bento.yaml tracks position in DAG
// ---------------------------------------------------------------------------

func TestHeadTracking(t *testing.T) {
	dir := makeWorkspace(t)

	// Save cp-1
	run(t, dir, "save", "--skip-secret-scan", "-m", "first")

	// Verify head is set after save
	data, _ := os.ReadFile(filepath.Join(dir, "bento.yaml"))
	head1 := extractYAMLField(string(data), "head")
	if head1 == "" {
		t.Fatal("head should be set after first save")
	}

	// Save cp-2
	writeFile(t, dir, "main.go", "package main\n// v2\nfunc main() {}\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "second")

	data, _ = os.ReadFile(filepath.Join(dir, "bento.yaml"))
	head2 := extractYAMLField(string(data), "head")
	if head2 == "" || head2 == head1 {
		t.Errorf("head should update after second save: head1=%s, head2=%s", head1, head2)
	}

	// Open cp-1 — head should update to cp-1's digest
	run(t, dir, "open", "cp-1")

	data, _ = os.ReadFile(filepath.Join(dir, "bento.yaml"))
	headAfterOpen := extractYAMLField(string(data), "head")
	if headAfterOpen == "" {
		t.Fatal("head should be set after open")
	}
	if headAfterOpen == head2 {
		t.Error("head should change after opening an older checkpoint")
	}
	if headAfterOpen != head1 {
		t.Errorf("head should match cp-1's digest after open: expected %s, got %s", head1, headAfterOpen)
	}
}

// ---------------------------------------------------------------------------
// TestParallelWorkspaces: two dirs sharing one store get correct parents
// ---------------------------------------------------------------------------

func TestParallelWorkspaces(t *testing.T) {
	dirA := makeWorkspace(t)

	// Save cp-1 from workspace A
	run(t, dirA, "save", "--skip-secret-scan", "-m", "from A")

	// Open cp-1 into workspace B
	dirB := t.TempDir()
	run(t, dirA, "open", "cp-1", dirB)

	// Both should have same workspace ID
	yamlA, _ := os.ReadFile(filepath.Join(dirA, "bento.yaml"))
	yamlB, _ := os.ReadFile(filepath.Join(dirB, "bento.yaml"))
	idA := extractYAMLField(string(yamlA), "id")
	idB := extractYAMLField(string(yamlB), "id")
	if idA != idB {
		t.Fatalf("workspaces should share ID: A=%s, B=%s", idA, idB)
	}

	// Modify and save from both — no sequence collision
	writeFile(t, dirA, "main.go", "package main\n// A-v2\nfunc main() {}\n")
	outA := run(t, dirA, "save", "--skip-secret-scan", "-m", "A-v2")

	writeFile(t, dirB, "main.go", "package main\n// B-v2\nfunc main() {}\n")
	outB := run(t, dirB, "save", "--skip-secret-scan", "-m", "B-v2")

	// Check sequence numbers don't collide
	if strings.Contains(outA, "cp-2") && strings.Contains(outB, "cp-2") {
		t.Error("both workspaces produced cp-2 — sequence collision")
	}

	// List from either workspace should show all checkpoints
	listOut := run(t, dirA, "list")
	if !strings.Contains(listOut, "A-v2") || !strings.Contains(listOut, "B-v2") {
		t.Errorf("list should show checkpoints from both workspaces:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestRestorableOpen: bento open creates a pre-open backup, undo restores it
// ---------------------------------------------------------------------------

func TestRestorableOpen(t *testing.T) {
	dir := makeWorkspace(t)

	// Save cp-1 with original content
	run(t, dir, "save", "--skip-secret-scan", "-m", "v1")

	// Modify and save cp-2
	writeFile(t, dir, "main.go", "package main\n// v2\nfunc main() {}\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "v2")

	// Open cp-1 — should create pre-open backup
	out := run(t, dir, "open", "cp-1")
	if !strings.Contains(out, "To undo: bento open undo") {
		t.Errorf("open should print undo hint, got:\n%s", out)
	}

	// Verify cp-1 content restored
	content, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(content), "// v2") {
		t.Error("cp-1 restore should not have v2 content")
	}

	// Verify pre-open tag exists
	listOut := run(t, dir, "list")
	if !strings.Contains(listOut, "pre-open") {
		t.Errorf("pre-open tag should exist after open:\n%s", listOut)
	}

	// Undo — should restore pre-open backup (v2 content)
	run(t, dir, "open", "undo")

	content, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(content), "// v2") {
		t.Errorf("undo should restore v2 content, got: %q", content)
	}
}

// ---------------------------------------------------------------------------
// TestRestorableOpenToggle: undo twice returns to the opened checkpoint
// ---------------------------------------------------------------------------

func TestRestorableOpenToggle(t *testing.T) {
	dir := makeWorkspace(t)

	run(t, dir, "save", "--skip-secret-scan", "-m", "v1")
	writeFile(t, dir, "main.go", "package main\n// v2\nfunc main() {}\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "v2")

	// Open cp-1
	run(t, dir, "open", "cp-1")

	// Undo (back to v2)
	run(t, dir, "open", "undo")
	content, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if !strings.Contains(string(content), "// v2") {
		t.Error("first undo should restore v2")
	}

	// Undo again (back to cp-1)
	run(t, dir, "open", "undo")
	content, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	if strings.Contains(string(content), "// v2") {
		t.Error("second undo should restore cp-1 (no v2)")
	}
}

// ---------------------------------------------------------------------------
// TestRestorableOpenNoBackup: --no-backup skips pre-open save
// ---------------------------------------------------------------------------

func TestRestorableOpenNoBackup(t *testing.T) {
	dir := makeWorkspace(t)

	run(t, dir, "save", "--skip-secret-scan", "-m", "v1")
	writeFile(t, dir, "main.go", "package main\n// v2\nfunc main() {}\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "v2")

	// Open with --no-backup
	out := run(t, dir, "open", "--no-backup", "cp-1")
	if strings.Contains(out, "To undo") {
		t.Errorf("--no-backup should suppress undo hint, got:\n%s", out)
	}

	// pre-open tag should not exist
	listOut := run(t, dir, "list")
	if strings.Contains(listOut, "pre-open") {
		t.Errorf("pre-open tag should not exist with --no-backup:\n%s", listOut)
	}
}

// ---------------------------------------------------------------------------
// TestRestorableOpenUndoNoBackup: undo with no pre-open tag fails
// ---------------------------------------------------------------------------

func TestRestorableOpenUndoNoBackup(t *testing.T) {
	dir := makeWorkspace(t)
	run(t, dir, "save", "--skip-secret-scan", "-m", "v1")

	// Undo should fail — no pre-open tag
	out := runExpectFail(t, dir, "open", "undo")
	if !strings.Contains(out, "not found") {
		t.Errorf("undo with no backup should fail with 'not found', got:\n%s", out)
	}
}

// extractYAMLField does a simple line-based extraction of a top-level YAML field.
func extractYAMLField(yaml, field string) string {
	for _, line := range strings.Split(yaml, "\n") {
		if strings.HasPrefix(line, field+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		}
	}
	return ""
}
