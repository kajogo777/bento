package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSecretScanner_Valid(t *testing.T) {
	s, err := NewSecretScanner([]string{`AKIA[0-9A-Z]{16}`, `sk-[a-zA-Z0-9]{20,}`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("scanner should not be nil")
	}
}

func TestNewSecretScanner_InvalidPattern(t *testing.T) {
	_, err := NewSecretScanner([]string{`[invalid`})
	if err == nil {
		t.Fatal("expected error for invalid regex")
	}
}

func TestScanFile_DetectsAWSKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.txt")
	content := "AWS_KEY=AKIAIOSFODNN7EXAMPLE1\nother stuff\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner([]string{`AKIA[0-9A-Z]{16}`})
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Line != 1 {
		t.Errorf("expected line 1, got %d", results[0].Line)
	}
	if results[0].File != path {
		t.Errorf("expected file %q, got %q", path, results[0].File)
	}
	if results[0].Match != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("expected match AKIAIOSFODNN7EXAMPLE, got %q", results[0].Match)
	}
}

func TestScanFile_CleanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(path, []byte("nothing secret here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(DefaultPatterns)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for clean file, got %d", len(results))
	}
}

func TestScanFile_NonexistentFile(t *testing.T) {
	s, err := NewSecretScanner(DefaultPatterns)
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.ScanFile("/nonexistent/path/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestScanFiles_Multiple(t *testing.T) {
	dir := t.TempDir()

	// File with a secret
	f1 := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(f1, []byte("key=AKIAIOSFODNN7EXAMPLE1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clean file
	f2 := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(f2, []byte("clean content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// File with a different secret
	f3 := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(f3, []byte("password: hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(DefaultPatterns)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFiles([]string{f1, f2, f3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results across files, got %d", len(results))
	}

	// Verify results come from different files.
	files := make(map[string]bool)
	for _, r := range results {
		files[r.File] = true
	}
	if !files[f1] {
		t.Error("expected results from a.txt")
	}
	if !files[f3] {
		t.Error("expected results from c.txt")
	}
}

func TestScanFile_MultiplePatterns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.txt")
	content := "AKIAIOSFODNN7EXAMPLE1 password=secret123\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(DefaultPatterns)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 matches on same line, got %d", len(results))
	}
}

func TestScanFile_LongLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "longline.txt")

	// Build a file with a line exceeding bufio.MaxScanTokenSize (64KB).
	// Place a secret after the long padding to ensure the scanner reads
	// the entire line rather than erroring with "token too long".
	longLine := strings.Repeat("A", 128*1024) + " AKIAIOSFODNN7EXAMPLE1\n"
	if err := os.WriteFile(path, []byte(longLine), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner([]string{`AKIA[0-9A-Z]{16}`})
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("unexpected error scanning file with long line: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result from long line, got %d", len(results))
	}
	if results[0].Line != 1 {
		t.Errorf("expected match on line 1, got %d", results[0].Line)
	}
	if results[0].Match != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("expected match AKIAIOSFODNN7EXAMPLE, got %q", results[0].Match)
	}
}
