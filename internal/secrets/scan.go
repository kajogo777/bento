package secrets

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/zricethezav/gitleaks/v8/config"
	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
)

// ScanResult represents a single secret match found in a file.
type ScanResult struct {
	File        string
	Line        int
	Pattern     string
	Match       string
	Fingerprint string // file:ruleID:line — used for .gitleaksignore
}

// ProgressFunc is called after each file is scanned or skipped.
// scanned is the number of files completed so far, total is the total count.
type ProgressFunc func(scanned, total int)

// Scanner scans files for secret patterns using gitleaks.
type Scanner struct {
	detector   *detect.Detector
	onProgress ProgressFunc
	cachePath  string
	baseDir    string                // workspace root for relative fingerprints
	ignore     map[string]struct{}   // fingerprints to suppress
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

// SetProgressFunc registers a callback invoked after each file is processed.
func (s *Scanner) SetProgressFunc(fn ProgressFunc) {
	s.onProgress = fn
}

// SetCachePath sets the path for the scan cache file. When set, files whose
// content hash has not changed since the last clean scan are skipped.
// The cache is saved after a successful scan with no findings.
func (s *Scanner) SetCachePath(path string) {
	s.cachePath = path
}

// SetBaseDir sets the workspace root directory. When set, fingerprints use
// paths relative to this directory, making .gitleaksignore portable.
func (s *Scanner) SetBaseDir(dir string) {
	s.baseDir = dir
}

// LoadGitleaksIgnore reads a .gitleaksignore file and registers its
// fingerprints. Lines are in the format "file:ruleID:line". Blank lines
// and lines starting with '#' are skipped.
func (s *Scanner) LoadGitleaksIgnore(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	ignore := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ignore[line] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	s.ignore = ignore
	return nil
}

// scanCache maps absolute file paths to their SHA256 content hash at the time
// of the last clean scan.
type scanCache map[string]string

func loadCache(path string) scanCache {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c scanCache
	if json.Unmarshal(data, &c) != nil {
		return nil
	}
	return c
}

func saveCache(path string, c scanCache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// hashFile computes the SHA256 of a file by streaming.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// relativePath returns path relative to baseDir using forward slashes.
// If baseDir is empty, the relative computation fails, or the result
// escapes the base (starts with ".."), the original path is returned
// with forward slashes.
func relativePath(baseDir, path string) string {
	if baseDir == "" {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(baseDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// fingerprint builds a gitleaks-compatible fingerprint for a finding.
func (s *Scanner) fingerprint(file, ruleID string, line int) string {
	return fmt.Sprintf("%s:%s:%d", relativePath(s.baseDir, file), ruleID, line)
}

// findingsToResults converts gitleaks findings to ScanResult values,
// filtering out any whose fingerprint is in the ignore set.
func (s *Scanner) findingsToResults(findings []report.Finding, path string) []ScanResult {
	results := make([]ScanResult, 0, len(findings))
	for _, f := range findings {
		file := f.File
		if file == "" {
			file = path
		}
		fp := s.fingerprint(file, f.RuleID, f.StartLine)
		if s.ignore != nil {
			if _, ok := s.ignore[fp]; ok {
				continue
			}
		}
		results = append(results, ScanResult{
			File:        file,
			Line:        f.StartLine,
			Pattern:     f.RuleID,
			Match:       f.Secret,
			Fingerprint: fp,
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

	findings := s.detector.DetectString(string(content))
	return s.findingsToResults(findings, path), nil
}

// ScanFiles scans multiple files concurrently and returns all secret matches found.
// Large files are scheduled first so they don't become tail-end stragglers.
// When a cache path is set, files whose SHA256 has not changed since the last
// clean scan are skipped entirely.
func (s *Scanner) ScanFiles(files []string) ([]ScanResult, error) {
	if len(files) == 0 {
		return nil, nil
	}

	// Load previous scan cache.
	var cache scanCache
	if s.cachePath != "" {
		cache = loadCache(s.cachePath)
	}

	type fileResult struct {
		results []ScanResult
		hash    string
		err     error
	}

	workers := runtime.NumCPU()
	if workers > len(files) {
		workers = len(files)
	}

	// Build an index sorted by file size descending so large files start
	// processing first, avoiding tail-end stalls.
	type indexedFile struct {
		idx  int
		size int64
	}
	ordered := make([]indexedFile, len(files))
	for i, f := range files {
		size := int64(0)
		if info, err := os.Stat(f); err == nil {
			size = info.Size()
		}
		ordered[i] = indexedFile{idx: i, size: size}
	}
	sort.Slice(ordered, func(a, b int) bool {
		return ordered[a].size > ordered[b].size
	})

	resultsCh := make([]fileResult, len(files))
	fileCh := make(chan int, len(files))
	for _, of := range ordered {
		fileCh <- of.idx
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
				path := files[i]

				// Hash the file and check cache.
				h, hashErr := hashFile(path)
				if hashErr == nil && cache != nil && cache[path] == h {
					// File unchanged since last clean scan — skip.
					resultsCh[i] = fileResult{hash: h}
				} else {
					results, err := s.ScanFile(path)
					resultsCh[i] = fileResult{results: results, hash: h, err: err}
				}

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
	newCache := make(scanCache, len(files))
	for i, r := range resultsCh {
		if r.err != nil {
			return allResults, r.err
		}
		allResults = append(allResults, r.results...)
		if r.hash != "" {
			newCache[files[i]] = r.hash
		}
	}

	// Persist cache only when the scan is clean (no findings).
	if s.cachePath != "" && len(allResults) == 0 {
		saveCache(s.cachePath, newCache)
	}

	return allResults, nil
}
