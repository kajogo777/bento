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

	// File with a Stripe test key
	f3 := filepath.Join(dir, "c.env")
	if err := os.WriteFile(f3, []byte("STRIPE_KEY=sk_test_51H3gKLM2eZvKYlo2CjFHcJN8kOPqRsTuVwXyZ0123456789abcdef\n"), 0o644); err != nil {
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

func TestScanFile_Fingerprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(path, []byte("AWS_ACCESS_KEY_ID=AKIAZ5GMXQ3TCBFHTKO7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	// Fingerprint should be file:ruleID:line
	fp := results[0].Fingerprint
	if fp == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if !strings.HasPrefix(fp, path+":") {
		t.Errorf("fingerprint should start with file path, got %q", fp)
	}
}

func TestGitleaksIgnore(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(secretFile, []byte("AWS_ACCESS_KEY_ID=AKIAZ5GMXQ3TCBFHTKO7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// First scan to get the fingerprint.
	s1, _ := NewSecretScanner(nil)
	results, _ := s1.ScanFile(secretFile)
	if len(results) == 0 {
		t.Fatal("expected findings to build ignore file")
	}
	fp := results[0].Fingerprint

	// Write .gitleaksignore with that fingerprint.
	ignorePath := filepath.Join(dir, ".gitleaksignore")
	ignoreContent := "# Suppress known false positive\n" + fp + "\n"
	if err := os.WriteFile(ignorePath, []byte(ignoreContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second scan with ignore loaded — should suppress the finding.
	s2, _ := NewSecretScanner(nil)
	if err := s2.LoadGitleaksIgnore(ignorePath); err != nil {
		t.Fatalf("loading .gitleaksignore: %v", err)
	}
	results2, err := s2.ScanFile(secretFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(results2) != 0 {
		t.Fatalf("expected 0 results after ignore, got %d", len(results2))
	}
}

func TestScanCache(t *testing.T) {
	dir := t.TempDir()
	cleanFile := filepath.Join(dir, "clean.txt")
	if err := os.WriteFile(cleanFile, []byte("nothing secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(dir, "cache.json")

	// First scan — no cache exists, should scan and create cache.
	s1, _ := NewSecretScanner(nil)
	s1.SetCachePath(cachePath)
	results, err := s1.ScanFiles([]string{cleanFile})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
	// Cache file should exist now.
	if _, statErr := os.Stat(cachePath); statErr != nil {
		t.Fatal("expected cache file to be created")
	}

	// Second scan — file unchanged, should use cache (we verify by
	// checking progress callback count; the file is "scanned" but
	// the gitleaks detector is not invoked).
	var progressCount int
	s2, _ := NewSecretScanner(nil)
	s2.SetCachePath(cachePath)
	s2.SetProgressFunc(func(scanned, total int) {
		progressCount = scanned
	})
	results2, err := s2.ScanFiles([]string{cleanFile})
	if err != nil {
		t.Fatal(err)
	}
	if len(results2) != 0 {
		t.Fatalf("expected 0 results on cached scan, got %d", len(results2))
	}
	if progressCount != 1 {
		t.Fatalf("expected progress to reach 1, got %d", progressCount)
	}
}
