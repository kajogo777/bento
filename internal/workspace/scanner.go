package workspace

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kajogo777/bento/internal/harness"
)

// Scanner walks a workspace directory tree and assigns files to layers.
type Scanner struct {
	workDir string
	layers  []harness.LayerDef
	ignore  *IgnoreMatcher
}

// NewScanner creates a new Scanner for the given workspace directory.
func NewScanner(workDir string, layers []harness.LayerDef, ignorePatterns []string) *Scanner {
	return &Scanner{
		workDir: workDir,
		layers:  layers,
		ignore:  NewIgnoreMatcher(ignorePatterns),
	}
}

// Scan walks the workspace directory tree and assigns each file to the first
// matching layer. It returns a map of layer name to relative file paths.
// Files that match no layer or match an ignore pattern are excluded.
func (s *Scanner) Scan() (map[string][]string, error) {
	result := make(map[string][]string)

	err := filepath.WalkDir(s.workDir, func(absPath string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(s.workDir, absPath)
		if err != nil {
			return err
		}
		rel = NormalizePath(rel)

		if s.ignore.Match(rel) {
			return nil
		}

		matched := false
		for _, layer := range s.layers {
			for _, pattern := range layer.Patterns {
				if matchesPattern(pattern, rel) {
					result[layer.Name] = append(result[layer.Name], rel)
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		// Assign unmatched files to the catch-all layer (typically project).
		if !matched {
			for _, layer := range s.layers {
				if layer.CatchAll {
					result[layer.Name] = append(result[layer.Name], rel)
					break
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort file lists for deterministic output.
	for name := range result {
		sort.Strings(result[name])
	}

	return result, nil
}

// ExternalPathDef maps an external directory to an archive prefix.
type ExternalPathDef struct {
	Source        string
	ArchivePrefix string
}

// ExternalFile represents a file from outside the workspace to be included in an archive.
type ExternalFile struct {
	AbsPath     string // real path on disk
	ArchivePath string // path to store in the tar archive
}

// ScanExternalPaths walks external directories and returns files ready for packing.
// Each file's ArchivePath is prefixed with the ExternalPathDef's ArchivePrefix.
// ignorePatterns are applied to the relative path within each external source.
func ScanExternalPaths(defs []ExternalPathDef, ignorePatterns []string) ([]ExternalFile, error) {
	ignore := NewIgnoreMatcher(ignorePatterns)
	var files []ExternalFile

	for _, def := range defs {
		source := def.Source
		// Expand ~ prefix
		if strings.HasPrefix(source, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				source = filepath.Join(home, source[2:])
			}
		}

		info, err := os.Stat(source)
		if err != nil || !info.IsDir() {
			continue // skip non-existent directories
		}

		err = filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil // skip unreadable entries
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(source, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if ignore.Match(rel) {
				return nil
			}
			archivePath := def.ArchivePrefix + rel
			files = append(files, ExternalFile{
				AbsPath:     path,
				ArchivePath: archivePath,
			})
			return nil
		})
		if err != nil {
			return files, fmt.Errorf("scanning external path %s: %w", source, err)
		}
	}

	return files, nil
}
