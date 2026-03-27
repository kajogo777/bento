package workspace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackUnpackRoundtrip(t *testing.T) {
	workDir := t.TempDir()

	// Create some files to pack.
	files := map[string]string{
		"src/main.go":    "package main\n\nfunc main() {}\n",
		"src/util.go":    "package main\n\nfunc util() {}\n",
		"config/app.yml": "key: value\n",
	}
	for relPath, content := range files {
		abs := filepath.Join(workDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	fileList := make([]string, 0, len(files))
	for f := range files {
		fileList = append(fileList, f)
	}

	data, err := PackLayer(workDir, fileList)
	if err != nil {
		t.Fatalf("PackLayer returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("PackLayer returned empty data")
	}

	// Unpack to a new directory.
	targetDir := t.TempDir()
	if err := UnpackLayer(data, targetDir); err != nil {
		t.Fatalf("UnpackLayer returned error: %v", err)
	}

	// Verify contents match.
	for relPath, expectedContent := range files {
		abs := filepath.Join(targetDir, filepath.FromSlash(relPath))
		got, err := os.ReadFile(abs)
		if err != nil {
			t.Errorf("failed to read unpacked file %s: %v", relPath, err)
			continue
		}
		if string(got) != expectedContent {
			t.Errorf("file %s content = %q, want %q", relPath, string(got), expectedContent)
		}
	}
}

func TestUnpackLayerRejectsPathTraversal(t *testing.T) {
	data := buildTarGzBytes(t, map[string]string{
		"../escape.txt": "malicious",
	})

	targetDir := t.TempDir()
	err := UnpackLayer(data, targetDir)
	if err == nil {
		t.Fatal("UnpackLayer should reject paths with '..'")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Errorf("error should mention '..', got: %v", err)
	}
}

func TestPackLayerEmptyFileList(t *testing.T) {
	workDir := t.TempDir()

	data, err := PackLayer(workDir, nil)
	if err != nil {
		t.Fatalf("PackLayer with empty file list returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("PackLayer should return non-empty data even for empty file list")
	}

	// Unpack should succeed with no files extracted.
	targetDir := t.TempDir()
	if err := UnpackLayer(data, targetDir); err != nil {
		t.Fatalf("UnpackLayer of empty archive returned error: %v", err)
	}

	// Target dir should be empty.
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty target dir, got %d entries", len(entries))
	}
}

// buildTarGzBytes creates a tar.gz archive from the given filename->content map.
func buildTarGzBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
