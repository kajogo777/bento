package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewSecretScanner_Valid(t *testing.T) {
	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("scanner should not be nil")
	}
}

func TestScanFile_DetectsAWSKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	// Use a realistic-looking AWS key (not the well-known EXAMPLE key which
	// gitleaks correctly filters out as a known test value).
	content := "AWS_ACCESS_KEY_ID=AKIAZ5GMXQ3TCBFHTKO7\nother stuff\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for AWS key, got 0")
	}

	// Verify the finding references the correct file.
	found := false
	for _, r := range results {
		if r.File == path {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected finding for file %q", path)
	}
}

func TestScanFile_CleanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(path, []byte("nothing secret here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(nil)
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
	s, err := NewSecretScanner(nil)
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

	// File with an AWS key
	f1 := filepath.Join(dir, "a.env")
	if err := os.WriteFile(f1, []byte("AWS_KEY=AKIAZ5GMXQ3TCBFHTKO7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clean file
	f2 := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(f2, []byte("clean content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// File with a Slack bot token
	f3 := filepath.Join(dir, "c.env")
	if err := os.WriteFile(f3, []byte("SLACK_TOKEN=sk_test_51H3gKLM2eZvKYlo2CjFHcJN8kOPqRsTuVwXyZ0123456789abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(nil)
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
		t.Error("expected results from a.env")
	}
	if !files[f3] {
		t.Error("expected results from c.env")
	}
}

func TestScanFile_PrivateKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.pem")
	content := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/ygWyF8PbnGy0AHB7MhgHcTz6sE2I2yPB\n-----END RSA PRIVATE KEY-----\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for private key, got 0")
	}
}

func TestScanFile_LongLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "longline.env")

	// Build a file with a line exceeding bufio.MaxScanTokenSize (64KB).
	// This verifies that gitleaks handles arbitrarily long lines without
	// the "bufio.Scanner: token too long" error from the old implementation.
	// The secret is on a second line; the first line is just padding.
	longLine := "PADDING=" + strings.Repeat("x", 128*1024) + "\nAWS_ACCESS_KEY_ID=AKIAZ5GMXQ3TCBFHTKO7\n"
	if err := os.WriteFile(path, []byte(longLine), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("unexpected error scanning file with long line: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result from file with long line, got 0")
	}
}
