package secrets

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
)

// DefaultPatterns are the built-in secret detection patterns used when a harness
// does not provide its own.
var DefaultPatterns = []string{
	`(?i)AKIA[0-9A-Z]{16}`,                      // AWS access key
	`(?i)sk-[a-zA-Z0-9]{20,}`,                    // OpenAI/Anthropic API key
	`ghp_[a-zA-Z0-9]{36}`,                        // GitHub PAT
	`glpat-[a-zA-Z0-9\-]{20,}`,                   // GitLab PAT
	`-----BEGIN (RSA |EC )?PRIVATE KEY`,           // Private keys
	`(?i)(password|passwd|pwd)\s*[:=]\s*\S+`,      // Password assignments
}

// ScanResult represents a single secret match found in a file.
type ScanResult struct {
	File    string
	Line    int
	Pattern string
	Match   string
}

// Scanner scans files for secret patterns.
type Scanner struct {
	patterns []*regexp.Regexp
}

// NewSecretScanner creates a Scanner from the given regex pattern strings. If
// compilation of any pattern fails, an error is returned.
func NewSecretScanner(patterns []string) (*Scanner, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compiling secret pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return &Scanner{patterns: compiled}, nil
}

// isBinary checks if a file appears to be binary by looking for null bytes
// in the first 512 bytes.
func isBinary(f *os.File) (bool, error) {
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	return bytes.ContainsRune(buf[:n], 0), nil
}

// ScanFile scans a single file line by line and returns any secret matches.
// Binary files are skipped.
func (s *Scanner) ScanFile(path string) ([]ScanResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if binary, err := isBinary(f); err != nil {
		return nil, fmt.Errorf("checking file %s: %w", path, err)
	} else if binary {
		return nil, nil
	}

	var results []ScanResult
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		for _, re := range s.patterns {
			if match := re.FindString(line); match != "" {
				results = append(results, ScanResult{
					File:    path,
					Line:    lineNum,
					Pattern: re.String(),
					Match:   match,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return results, fmt.Errorf("scanning file %s: %w", path, err)
	}

	return results, nil
}

// ScanFiles scans multiple files and returns all secret matches found.
func (s *Scanner) ScanFiles(files []string) ([]ScanResult, error) {
	var allResults []ScanResult
	for _, file := range files {
		results, err := s.ScanFile(file)
		if err != nil {
			return allResults, err
		}
		allResults = append(allResults, results...)
	}
	return allResults, nil
}
