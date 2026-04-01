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
	if !strings.HasPrefix(fp, filepath.ToSlash(path)+":") {
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

func TestScanFile_SkipsBinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary.dat")

	// Write a file with NUL bytes (binary indicator).
	content := []byte("some text\x00more binary\x00data")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("binary file should not cause error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("binary file should return no results, got %d", len(results))
	}
}

func TestScanFile_SkipsLargeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")

	// Create a file just over the limit.
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Write maxScanFileSize + 1 bytes of text content.
	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = 'A'
	}
	written := int64(0)
	for written <= maxScanFileSize {
		n, wErr := f.Write(buf)
		if wErr != nil {
			_ = f.Close()
			t.Fatal(wErr)
		}
		written += int64(n)
	}
	_ = f.Close()

	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("large file should not cause error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("large file should return no results, got %d", len(results))
	}
}

func TestScanFile_SkipsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	results, err := s.ScanFile(path)
	if err != nil {
		t.Fatalf("empty file should not cause error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("empty file should return no results, got %d", len(results))
	}
}

func TestScanFiles_SkipsBinaryAndLarge(t *testing.T) {
	dir := t.TempDir()

	// Text file with a secret — should be scanned.
	secretFile := filepath.Join(dir, "config.env")
	os.WriteFile(secretFile, []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7FSECRET\n"), 0644)

	// Binary file — should be skipped.
	binaryFile := filepath.Join(dir, "image.png")
	os.WriteFile(binaryFile, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), 0644)

	// Large text file — should be skipped.
	largeFile := filepath.Join(dir, "huge.log")
	f, _ := os.Create(largeFile)
	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = 'X'
	}
	written := int64(0)
	for written <= maxScanFileSize {
		n, _ := f.Write(buf)
		written += int64(n)
	}
	_ = f.Close()

	s, err := NewSecretScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	var progressCount int
	s.SetProgressFunc(func(scanned, total int) {
		progressCount = scanned
	})

	results, err := s.ScanFiles([]string{secretFile, binaryFile, largeFile})
	if err != nil {
		t.Fatal(err)
	}

	// Only the secret file should produce results.
	if len(results) == 0 {
		t.Error("expected at least 1 result from the text file with a secret")
	}
	for _, r := range results {
		if r.File == binaryFile {
			t.Error("binary file should not produce results")
		}
		if r.File == largeFile {
			t.Error("large file should not produce results")
		}
	}

	// Progress should account for all 3 files.
	if progressCount != 3 {
		t.Errorf("progress should reach 3 (all files), got %d", progressCount)
	}
}

func TestShouldSkipFile_Heuristics(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content []byte
		size    int64 // if >0, create file of this size instead of using content
		want    bool
		reason  string
	}{
		{"text file", []byte("hello world\nline2\n"), 0, false, ""},
		{"binary with NUL", []byte("text\x00binary"), 0, true, "binary file"},
		{"empty file", []byte{}, 0, true, "empty"},
		{"json config", []byte(`{"key": "value"}`), 0, false, ""},
		{"yaml config", []byte("key: value\nlist:\n  - item\n"), 0, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".dat")
			if err := os.WriteFile(path, tt.content, 0644); err != nil {
				t.Fatal(err)
			}

			skip, reason := shouldSkipFile(path)
			if skip != tt.want {
				t.Errorf("shouldSkipFile(%q) = %v, want %v (reason: %s)", tt.name, skip, tt.want, reason)
			}
			if tt.want && tt.reason != "" && reason != tt.reason {
				t.Errorf("reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}
