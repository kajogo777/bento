package secrets

import (
	"fmt"
	"os"

	"github.com/zricethezav/gitleaks/v8/config"
	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
	"github.com/zricethezav/gitleaks/v8/sources"
)

// ScanResult represents a single secret match found in a file.
type ScanResult struct {
	File    string
	Line    int
	Pattern string
	Match   string
}

// Scanner scans files for secret patterns using gitleaks.
type Scanner struct {
	detector *detect.Detector
}

// NewSecretScanner creates a Scanner backed by gitleaks. The patterns argument
// is accepted for API compatibility but ignored — gitleaks' ~200+ built-in
// rules are used instead. Pass nil or an empty slice; the result is the same.
func NewSecretScanner(patterns []string) (*Scanner, error) {
	detector, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("initializing gitleaks detector: %w", err)
	}
	return &Scanner{detector: detector}, nil
}

// NewSecretScannerWithConfig creates a Scanner with a custom gitleaks config.
// This allows callers to extend or override the default ruleset.
func NewSecretScannerWithConfig(cfg config.Config) *Scanner {
	return &Scanner{detector: detect.NewDetector(cfg)}
}

// findingsToResults converts gitleaks findings to ScanResult values.
func findingsToResults(findings []report.Finding, path string) []ScanResult {
	results := make([]ScanResult, 0, len(findings))
	for _, f := range findings {
		file := f.File
		if file == "" {
			file = path
		}
		results = append(results, ScanResult{
			File:    file,
			Line:    f.StartLine,
			Pattern: f.RuleID,
			Match:   f.Secret,
		})
	}
	return results
}

// ScanFile scans a single file and returns any secret matches.
// Binary files are automatically skipped by gitleaks.
func (s *Scanner) ScanFile(path string) ([]ScanResult, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("opening file %s: %w", path, err)
	}

	fragment := sources.Fragment{
		Raw:      string(content),
		FilePath: path,
	}
	findings := s.detector.Detect(detect.Fragment(fragment))
	return findingsToResults(findings, path), nil
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
