package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kajogo777/bento/internal/extension"
)

// ---------------------------------------------------------------------------
// DisplayPath
// ---------------------------------------------------------------------------

func TestDisplayPath_RelativeWorkspaceFile(t *testing.T) {
	// Relative workspace paths pass through unchanged.
	for _, p := range []string{
		"src/main.go",
		"README.md",
		"deep/nested/file.txt",
	} {
		if got := DisplayPath(p); got != p {
			t.Errorf("DisplayPath(%q) = %q, want unchanged", p, got)
		}
	}
}

func TestDisplayPath_HomeRelativeExternal(t *testing.T) {
	// __external__/~/... strips sentinel and keeps ~/
	cases := []struct {
		archive string
		want    string
	}{
		{"__external__/~/.claude/session.jsonl", "~/.claude/session.jsonl"},
		{"__external__/~/foo/bar/baz.txt", "~/foo/bar/baz.txt"},
	}
	for _, c := range cases {
		if got := DisplayPath(c.archive); got != c.want {
			t.Errorf("DisplayPath(%q) = %q, want %q", c.archive, got, c.want)
		}
	}
}

func TestDisplayPath_AbsoluteExternal(t *testing.T) {
	// __external__/abs/path strips sentinel and keeps /path
	cases := []struct {
		archive string
		want    string
	}{
		{"__external__/var/cache/foo.txt", "/var/cache/foo.txt"},
		{"__external__/tmp/data/session.db", "/tmp/data/session.db"},
	}
	for _, c := range cases {
		if got := DisplayPath(c.archive); got != c.want {
			t.Errorf("DisplayPath(%q) = %q, want %q", c.archive, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// portablePath / absFromArchivePath roundtrip
// ---------------------------------------------------------------------------

func TestPortablePath_HomeRelative(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	abs := filepath.Join(home, ".claude", "projects", "session.jsonl")
	p := portablePath(abs)
	if !strings.HasPrefix(p, "/~/") {
		t.Errorf("portablePath(%q) = %q, expected /~/ prefix", abs, p)
	}
}

func TestPortablePath_Absolute_NonHome(t *testing.T) {
	// t.TempDir() on macOS/Linux is outside home; path should be returned
	// with forward slashes and a leading "/" for consistent archive naming.
	// On Unix, paths already start with "/". On Windows, portablePath
	// prepends "/" to "C:/..." → "/C:/...".
	dir := t.TempDir()
	abs := filepath.Join(dir, "data.txt")
	p := portablePath(abs)
	want := NormalizePath(abs)
	if !strings.HasPrefix(want, "/") {
		want = "/" + want
	}
	if p != want {
		t.Errorf("portablePath(%q) = %q, want %q", abs, p, want)
	}
}

func TestAbsFromArchivePath_HomeRelative(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	archivePath := "__external__/~/.claude/session.jsonl"
	got := absFromArchivePath(archivePath)
	want := filepath.Join(home, ".claude", "session.jsonl")
	if got != want {
		t.Errorf("absFromArchivePath(%q) = %q, want %q", archivePath, got, want)
	}
}

func TestAbsFromArchivePath_Absolute(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "sub", "data.txt")
	archivePath := "__external__" + abs
	got := absFromArchivePath(archivePath)
	if got != abs {
		t.Errorf("absFromArchivePath(%q) = %q, want %q", archivePath, got, abs)
	}
}

func TestPortableAbsRoundtrip_HomeRelative(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	abs := filepath.Join(home, ".claude", "projects", "abc123", "session.jsonl")
	archive := "__external__" + portablePath(abs)
	got := absFromArchivePath(archive)
	if got != abs {
		t.Errorf("roundtrip home path: got %q, want %q", got, abs)
	}
}

func TestPortableAbsRoundtrip_Absolute(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "nested", "data.db")
	archive := "__external__" + portablePath(abs)
	got := absFromArchivePath(archive)
	if got != abs {
		t.Errorf("roundtrip absolute path: got %q, want %q", got, abs)
	}
}

// ---------------------------------------------------------------------------
// Pack / unpack external file roundtrip
// ---------------------------------------------------------------------------

// writeFileAt creates a file at the given absolute path.
func writeFileAt(t *testing.T, abs, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPackUnpackExternal_AbsolutePath(t *testing.T) {
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "sub", "data.txt")
	const content = "external absolute content\n"
	writeFileAt(t, srcFile, content)

	extFiles := []ExternalFile{{
		AbsPath:     srcFile,
		ArchivePath: "__external__" + portablePath(srcFile),
	}}

	// Verify archive entry name is the expected display path.
	wantDisplay := DisplayPath(extFiles[0].ArchivePath)
	if !strings.HasPrefix(wantDisplay, "/") {
		t.Errorf("display path for absolute external should start with /, got %q", wantDisplay)
	}

	data, err := PackLayerWithExternal("", nil, extFiles, false)
	if err != nil {
		t.Fatalf("PackLayerWithExternal: %v", err)
	}

	// Verify the archive contains the correct entry name.
	hashes, err := ListLayerFilesWithHashesFromReader(newReader(t, data))
	if err != nil {
		t.Fatalf("ListLayerFilesWithHashesFromReader: %v", err)
	}
	if _, ok := hashes[wantDisplay]; !ok {
		t.Errorf("archive missing key %q; keys: %v", wantDisplay, mapKeys(hashes))
	}

	// Delete source, then unpack — file should be restored to original location.
	if err := os.Remove(srcFile); err != nil {
		t.Fatalf("removing source: %v", err)
	}
	if err := UnpackLayerWithExternal(data, t.TempDir(), nil); err != nil {
		t.Fatalf("UnpackLayerWithExternal: %v", err)
	}
	got, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("reading restored file %s: %v", srcFile, err)
	}
	if string(got) != content {
		t.Errorf("restored content = %q, want %q", string(got), content)
	}
}

func TestPackUnpackExternal_HomeRelativePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}

	// Create a temp dir under home for this test.
	srcDir, err := os.MkdirTemp(home, "bento-ext-test-*")
	if err != nil {
		t.Skipf("cannot create temp dir under home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(srcDir) })

	srcFile := filepath.Join(srcDir, "session.jsonl")
	const content = "external home-relative content\n"
	writeFileAt(t, srcFile, content)

	extFiles := []ExternalFile{{
		AbsPath:     srcFile,
		ArchivePath: "__external__" + portablePath(srcFile),
	}}

	// Archive path must start with __external__/~/ since it's under home.
	if !strings.HasPrefix(extFiles[0].ArchivePath, "__external__/~/") {
		t.Errorf("archive path = %q, want __external__/~/ prefix", extFiles[0].ArchivePath)
	}

	// Display path must start with ~/
	wantDisplay := DisplayPath(extFiles[0].ArchivePath)
	if !strings.HasPrefix(wantDisplay, "~/") {
		t.Errorf("display path = %q, want ~/ prefix", wantDisplay)
	}

	data, err := PackLayerWithExternal("", nil, extFiles, false)
	if err != nil {
		t.Fatalf("PackLayerWithExternal: %v", err)
	}

	// Archive must contain the display path as key.
	hashes, err := ListLayerFilesWithHashesFromReader(newReader(t, data))
	if err != nil {
		t.Fatalf("ListLayerFilesWithHashesFromReader: %v", err)
	}
	if _, ok := hashes[wantDisplay]; !ok {
		t.Errorf("archive missing key %q; keys: %v", wantDisplay, mapKeys(hashes))
	}

	// Delete source, unpack, verify restored.
	if err := os.Remove(srcFile); err != nil {
		t.Fatalf("removing source: %v", err)
	}
	if err := UnpackLayerWithExternal(data, t.TempDir(), nil); err != nil {
		t.Fatalf("UnpackLayerWithExternal: %v", err)
	}
	got, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatalf("reading restored file %s: %v", srcFile, err)
	}
	if string(got) != content {
		t.Errorf("restored content = %q, want %q", string(got), content)
	}
}

// ---------------------------------------------------------------------------
// Diff key consistency: scanner DisplayPath(ArchivePath) == ListLayerFiles key
// ---------------------------------------------------------------------------

// TestDiffKeyConsistency verifies that the key used when hashing workspace
// external files (DisplayPath(ef.ArchivePath)) is identical to the key that
// ListLayerFilesWithHashesFromReader returns after the file has been packed.
// This is the exact invariant required for correct diff behaviour.
func TestDiffKeyConsistency_RelativePath(t *testing.T) {
	workDir := t.TempDir()
	createFile(t, workDir, "src/main.go", "package main\n")

	layers := []extension.LayerDef{
		{Name: "project", Patterns: []string{"src/**"}},
	}
	s := NewScanner(workDir, layers, nil, nil)
	result, _ := s.Scan()

	data, err := PackLayerWithExternal(workDir, result["project"].WorkspaceFiles, nil, false)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	hashes, err := ListLayerFilesWithHashesFromReader(newReader(t, data))
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// The key for a workspace file is just its relative path.
	if _, ok := hashes["src/main.go"]; !ok {
		t.Errorf("expected key %q in checkpoint hashes, got: %v", "src/main.go", mapKeys(hashes))
	}
}

func TestDiffKeyConsistency_AbsolutePath(t *testing.T) {
	workDir := t.TempDir()
	extDir := t.TempDir() // absolute, not under home

	createFile(t, extDir, "session.db", "data")

	layers := []extension.LayerDef{
		{Name: "agent", Patterns: []string{extDir + "/"}},
	}
	s := NewScanner(workDir, layers, nil, nil)
	result, _ := s.Scan()

	if len(result["agent"].ExternalFiles) == 0 {
		t.Fatal("expected at least one external file")
	}
	ef := result["agent"].ExternalFiles[0]

	// Key used by diff for workspace side.
	wsKey := DisplayPath(ef.ArchivePath)

	// Pack and read back — key used by diff for checkpoint side.
	data, err := PackLayerWithExternal(workDir, nil, result["agent"].ExternalFiles, false)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	hashes, err := ListLayerFilesWithHashesFromReader(newReader(t, data))
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if _, ok := hashes[wsKey]; !ok {
		t.Errorf("key mismatch: workspace key %q not found in checkpoint keys %v", wsKey, mapKeys(hashes))
	}
}

func TestDiffKeyConsistency_HomeRelativePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	workDir := t.TempDir()
	extDir, err := os.MkdirTemp(home, "bento-diffkey-test-*")
	if err != nil {
		t.Skipf("cannot create temp dir under home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(extDir) })

	writeFileAt(t, filepath.Join(extDir, "session.jsonl"), "content")

	layers := []extension.LayerDef{
		{Name: "agent", Patterns: []string{extDir + "/"}},
	}
	s := NewScanner(workDir, layers, nil, nil)
	result, _ := s.Scan()

	if len(result["agent"].ExternalFiles) == 0 {
		t.Fatal("expected at least one external file")
	}
	ef := result["agent"].ExternalFiles[0]

	// Archive path must use ~/
	if !strings.HasPrefix(ef.ArchivePath, "__external__/~/") {
		t.Errorf("archive path %q does not start with __external__/~/", ef.ArchivePath)
	}

	wsKey := DisplayPath(ef.ArchivePath)
	if !strings.HasPrefix(wsKey, "~/") {
		t.Errorf("display key %q does not start with ~/", wsKey)
	}

	data, err := PackLayerWithExternal(workDir, nil, result["agent"].ExternalFiles, false)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	hashes, err := ListLayerFilesWithHashesFromReader(newReader(t, data))
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if _, ok := hashes[wsKey]; !ok {
		t.Errorf("key mismatch: workspace key %q not found in checkpoint keys %v", wsKey, mapKeys(hashes))
	}
}

// ---------------------------------------------------------------------------
// Change type coverage: added / removed / modified / unchanged
// for each path type
// ---------------------------------------------------------------------------

// buildHashes packs the given external files and returns the hashes map
// (keyed by display path, as diff uses it).
func buildExternalHashes(t *testing.T, workDir string, extFiles []ExternalFile) map[string]string {
	t.Helper()
	data, err := PackLayerWithExternal(workDir, nil, extFiles, false)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	h, err := ListLayerFilesWithHashesFromReader(newReader(t, data))
	if err != nil {
		t.Fatalf("list hashes: %v", err)
	}
	return h
}

func TestDiffChangeTypes_Relative(t *testing.T) {
	workDir := t.TempDir()
	createFile(t, workDir, "unchanged.go", "same")
	createFile(t, workDir, "modified.go", "old")
	createFile(t, workDir, "removed.go", "gone")

	oldData, err := PackLayerWithExternal(workDir, []string{"unchanged.go", "modified.go", "removed.go"}, nil, false)
	if err != nil {
		t.Fatalf("pack old: %v", err)
	}
	oldHashes, _ := ListLayerFilesWithHashesFromReader(newReader(t, oldData))

	// Update workspace: modify one, remove one, add one.
	createFile(t, workDir, "modified.go", "new content")
	createFile(t, workDir, "added.go", "new file")
	_ = os.Remove(filepath.Join(workDir, "removed.go"))

	newHashes := map[string]string{}
	for _, f := range []string{"unchanged.go", "modified.go", "added.go"} {
		h, err := HashFileStreaming(filepath.Join(workDir, f))
		if err == nil {
			newHashes[f] = h
		}
	}

	added, removed, modified := hashDiff(oldHashes, newHashes)
	assertContains(t, "added", added, "added.go")
	assertContains(t, "removed", removed, "removed.go")
	assertContains(t, "modified", modified, "modified.go")
	assertNotContains(t, "added", added, "unchanged.go")
	assertNotContains(t, "removed", removed, "unchanged.go")
	assertNotContains(t, "modified", modified, "unchanged.go")
}

func TestDiffChangeTypes_Absolute(t *testing.T) {
	workDir := t.TempDir()
	extDir := t.TempDir()

	unchanged := filepath.Join(extDir, "unchanged.db")
	modified := filepath.Join(extDir, "modified.db")
	removed := filepath.Join(extDir, "removed.db")
	writeFileAt(t, unchanged, "same")
	writeFileAt(t, modified, "old")
	writeFileAt(t, removed, "gone")

	makeExt := func(abs string) ExternalFile {
		return ExternalFile{AbsPath: abs, ArchivePath: "__external__" + portablePath(abs)}
	}

	oldHashes := buildExternalHashes(t, workDir, []ExternalFile{makeExt(unchanged), makeExt(modified), makeExt(removed)})

	// Mutate: modify one, remove one, add one.
	added := filepath.Join(extDir, "added.db")
	writeFileAt(t, modified, "new content")
	writeFileAt(t, added, "brand new")
	_ = os.Remove(removed)

	extFiles := []ExternalFile{makeExt(unchanged), makeExt(modified), makeExt(added)}
	newHashes := map[string]string{}
	for _, ef := range extFiles {
		h, err := HashFileStreaming(ef.AbsPath)
		if err == nil {
			newHashes[DisplayPath(ef.ArchivePath)] = h
		}
	}

	a, r, m := hashDiff(oldHashes, newHashes)
	assertContains(t, "added", a, DisplayPath(makeExt(added).ArchivePath))
	assertContains(t, "removed", r, DisplayPath(makeExt(removed).ArchivePath))
	assertContains(t, "modified", m, DisplayPath(makeExt(modified).ArchivePath))
	assertNotContains(t, "added", a, DisplayPath(makeExt(unchanged).ArchivePath))
}

func TestDiffChangeTypes_HomeRelative(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	workDir := t.TempDir()
	extDir, err := os.MkdirTemp(home, "bento-diffchange-test-*")
	if err != nil {
		t.Skipf("cannot create temp dir under home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(extDir) })

	unchanged := filepath.Join(extDir, "unchanged.jsonl")
	modified := filepath.Join(extDir, "modified.jsonl")
	removed := filepath.Join(extDir, "removed.jsonl")
	writeFileAt(t, unchanged, "same")
	writeFileAt(t, modified, "old")
	writeFileAt(t, removed, "gone")

	makeExt := func(abs string) ExternalFile {
		return ExternalFile{AbsPath: abs, ArchivePath: "__external__" + portablePath(abs)}
	}

	oldHashes := buildExternalHashes(t, workDir, []ExternalFile{makeExt(unchanged), makeExt(modified), makeExt(removed)})

	// All keys should use ~/ since extDir is under home.
	for k := range oldHashes {
		if !strings.HasPrefix(k, "~/") {
			t.Errorf("checkpoint key %q does not start with ~/", k)
		}
	}

	// Mutate: modify one, remove one, add one.
	added := filepath.Join(extDir, "added.jsonl")
	writeFileAt(t, modified, "new content")
	writeFileAt(t, added, "brand new")
	_ = os.Remove(removed)

	extFiles := []ExternalFile{makeExt(unchanged), makeExt(modified), makeExt(added)}
	newHashes := map[string]string{}
	for _, ef := range extFiles {
		h, err := HashFileStreaming(ef.AbsPath)
		if err == nil {
			newHashes[DisplayPath(ef.ArchivePath)] = h
		}
	}

	a, r, m := hashDiff(oldHashes, newHashes)
	assertContains(t, "added", a, DisplayPath(makeExt(added).ArchivePath))
	assertContains(t, "removed", r, DisplayPath(makeExt(removed).ArchivePath))
	assertContains(t, "modified", m, DisplayPath(makeExt(modified).ArchivePath))
	assertNotContains(t, "added", a, DisplayPath(makeExt(unchanged).ArchivePath))
}

// ---------------------------------------------------------------------------
// Scanner archive path format
// ---------------------------------------------------------------------------

func TestScannerArchivePaths_Absolute(t *testing.T) {
	workDir := t.TempDir()
	extDir := t.TempDir() // absolute, not under home

	createFile(t, extDir, "a.txt", "a")
	createFile(t, extDir, "sub/b.txt", "b")

	layers := []extension.LayerDef{
		{Name: "agent", Patterns: []string{extDir + "/"}},
	}
	s := NewScanner(workDir, layers, nil, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	for _, ef := range result["agent"].ExternalFiles {
		if !strings.HasPrefix(ef.ArchivePath, "__external__/") {
			t.Errorf("archive path %q missing __external__/ prefix", ef.ArchivePath)
		}
		display := DisplayPath(ef.ArchivePath)
		if !strings.HasPrefix(display, "/") {
			t.Errorf("display path %q for absolute external should start with /", display)
		}
	}
}

func TestScannerArchivePaths_HomeRelative(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	workDir := t.TempDir()
	extDir, err := os.MkdirTemp(home, "bento-scanner-test-*")
	if err != nil {
		t.Skipf("cannot create temp dir under home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(extDir) })

	createFile(t, extDir, "session.jsonl", "data")

	layers := []extension.LayerDef{
		{Name: "agent", Patterns: []string{extDir + "/"}},
	}
	s := NewScanner(workDir, layers, nil, nil)
	result, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(result["agent"].ExternalFiles) == 0 {
		t.Fatal("expected external files")
	}
	for _, ef := range result["agent"].ExternalFiles {
		if !strings.HasPrefix(ef.ArchivePath, "__external__/~/") {
			t.Errorf("archive path %q should start with __external__/~/ for home-relative file", ef.ArchivePath)
		}
		display := DisplayPath(ef.ArchivePath)
		if !strings.HasPrefix(display, "~/") {
			t.Errorf("display path %q should start with ~/", display)
		}
		// AbsPath must match absFromArchivePath
		if got := absFromArchivePath(ef.ArchivePath); got != ef.AbsPath {
			t.Errorf("absFromArchivePath(%q) = %q, want %q", ef.ArchivePath, got, ef.AbsPath)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newReader(t *testing.T, data []byte) *bytes.Reader {
	t.Helper()
	return bytes.NewReader(data)
}

// hashDiff mirrors the logic in cli.diffFileMaps so tests in this package
// don't depend on the cli package.
func hashDiff(old, new map[string]string) (added, removed, modified []string) {
	for f, h := range new {
		if _, ok := old[f]; !ok {
			added = append(added, f)
		} else if old[f] != h {
			modified = append(modified, f)
		}
	}
	for f := range old {
		if _, ok := new[f]; !ok {
			removed = append(removed, f)
		}
	}
	return
}

func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func assertContains(t *testing.T, label string, slice []string, want string) {
	t.Helper()
	for _, s := range slice {
		if s == want {
			return
		}
	}
	t.Errorf("%s: expected %q in %v", label, want, slice)
}

func assertNotContains(t *testing.T, label string, slice []string, notWant string) {
	t.Helper()
	for _, s := range slice {
		if s == notWant {
			t.Errorf("%s: %q should not be in %v", label, notWant, slice)
			return
		}
	}
}

func TestCrossMachineDiffConsistency(t *testing.T) {
	// Two scanners on different workDirs with different real paths but
	// the same placeholder should produce identical archive paths.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	workDirA := t.TempDir()
	workDirB := t.TempDir()

	// Create external dirs that simulate different path-hashes
	extDirA := filepath.Join(home, ".testAgent", "projectA")
	extDirB := filepath.Join(home, ".testAgent", "projectB")
	os.MkdirAll(extDirA, 0755)
	os.MkdirAll(extDirB, 0755)
	t.Cleanup(func() { os.RemoveAll(filepath.Join(home, ".testAgent")) })

	writeFileAt(t, filepath.Join(extDirA, "session.jsonl"), "data-a")
	writeFileAt(t, filepath.Join(extDirB, "session.jsonl"), "data-b")

	placeholder := "/~/.testAgent/__BENTO_WORKSPACE__"
	portableA := extension.PortablePath(extDirA)
	portableB := extension.PortablePath(extDirB)

	normalizerA := func(path string) string {
		if strings.HasPrefix(path, portableA+"/") {
			return placeholder + path[len(portableA):]
		}
		return path
	}
	normalizerB := func(path string) string {
		if strings.HasPrefix(path, portableB+"/") {
			return placeholder + path[len(portableB):]
		}
		return path
	}

	layersA := []extension.LayerDef{{Name: "agent", Patterns: []string{extDirA + "/"}}}
	layersB := []extension.LayerDef{{Name: "agent", Patterns: []string{extDirB + "/"}}}

	scannerA := NewScanner(workDirA, layersA, nil, normalizerA)
	scannerB := NewScanner(workDirB, layersB, nil, normalizerB)

	resultA, err := scannerA.Scan()
	if err != nil {
		t.Fatal(err)
	}
	resultB, err := scannerB.Scan()
	if err != nil {
		t.Fatal(err)
	}

	if len(resultA["agent"].ExternalFiles) != 1 || len(resultB["agent"].ExternalFiles) != 1 {
		t.Fatal("expected 1 external file from each scanner")
	}

	archiveA := resultA["agent"].ExternalFiles[0].ArchivePath
	archiveB := resultB["agent"].ExternalFiles[0].ArchivePath

	if archiveA != archiveB {
		t.Errorf("archive paths should be identical across machines:\n  A: %s\n  B: %s", archiveA, archiveB)
	}
	if !strings.Contains(archiveA, "__BENTO_WORKSPACE__") {
		t.Errorf("archive path should contain placeholder: %s", archiveA)
	}
}
