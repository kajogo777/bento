package workspace

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/bentoci/bento/internal/harness"
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
