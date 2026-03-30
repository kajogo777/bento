package secrets

import (
	"fmt"
	"os"
	"runtime"
	"sync"

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

// ProgressFunc is called after each file is scanned.
// scanned is the number of files completed so far, total is the total count.
type ProgressFunc func(scanned, total int)

// Scanner scans files for secret patterns using gitleaks.
type Scanner struct {
	detector   *detect.Detector
	onProgress ProgressFunc
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

// SetProgressFunc registers a callback invoked after each file is scanned.
func (s *Scanner) SetProgressFunc(fn ProgressFunc) {
	s.onProgress = fn
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

// ScanFiles scans multiple files concurrently and returns all secret matches found.
func (s *Scanner) ScanFiles(files []string) ([]ScanResult, error) {
	if len(files) == 0 {
		return nil, nil
	}

	type fileResult struct {
		results []ScanResult
		err     error
	}

	workers := runtime.NumCPU()
	if workers > len(files) {
		workers = len(files)
	}

	resultsCh := make([]fileResult, len(files))
	fileCh := make(chan int, len(files))
	for i := range files {
		fileCh <- i
	}
	close(fileCh)

	var scanned int
	var progressMu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range fileCh {
				results, err := s.ScanFile(files[i])
				resultsCh[i] = fileResult{results: results, err: err}

				if s.onProgress != nil {
					progressMu.Lock()
					scanned++
					s.onProgress(scanned, len(files))
					progressMu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	// Collect results in original file order.
	var allResults []ScanResult
	for _, r := range resultsCh {
		if r.err != nil {
			return allResults, r.err
		}
		allResults = append(allResults, r.results...)
	}
	return allResults, nil
}
