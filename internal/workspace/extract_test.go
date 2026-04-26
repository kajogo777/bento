package workspace

import (
	"bytes"
	"os"
	"testing"
)

func TestExtractFileContentFromLayer(t *testing.T) {
	dir := t.TempDir()

	// Create test files
	_ = os.WriteFile(dir+"/hello.txt", []byte("hello world\n"), 0644)
	_ = os.WriteFile(dir+"/other.txt", []byte("other\n"), 0644)

	// Pack into a layer
	data := packToBytes(t, dir, []string{"hello.txt", "other.txt"}, nil)

	// Extract specific file
	content, err := ExtractFileContentFromLayer(bytes.NewReader(data), "hello.txt", 1<<20)
	if err != nil {
		t.Fatalf("ExtractFileContentFromLayer: %v", err)
	}
	if string(content) != "hello world\n" {
		t.Errorf("expected 'hello world\\n', got %q", string(content))
	}

	// File not found
	_, err = ExtractFileContentFromLayer(bytes.NewReader(data), "missing.txt", 1<<20)
	if !os.IsNotExist(err) {
		t.Errorf("expected os.ErrNotExist for missing file, got: %v", err)
	}
}
