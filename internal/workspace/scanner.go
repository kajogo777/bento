package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kajogo777/bento/internal/harness"
)

// ExternalFile represents a file from outside the workspace.
type ExternalFile struct {
	AbsPath     string
	ArchivePath string
}

// ScanResult holds the scan output for a single layer.
type ScanResult struct {
	WorkspaceFiles []string       // relative to workDir
	ExternalFiles  []ExternalFile // absolute paths with archive mappings
}

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

// Scan walks the workspace directory and external paths, assigning files to layers.
func (s *Scanner) Scan() (map[string]*ScanResult, error) {
	result := make(map[string]*ScanResult)
	for _, layer := range s.layers {
		result[layer.Name] = &ScanResult{}
	}

	// Split layer patterns into workspace and external
	type layerPatterns struct {
		workspace []string
		external  []string
	}
	lp := make(map[string]*layerPatterns)
	for _, layer := range s.layers {
		lp[layer.Name] = &layerPatterns{}
		for _, p := range layer.Patterns {
			if harness.IsExternalPattern(p) {
				// Reject path traversal
				if strings.Contains(p, "..") {
					continue
				}
				lp[layer.Name].external = append(lp[layer.Name].external, p)
			} else {
				lp[layer.Name].workspace = append(lp[layer.Name].workspace, p)
			}
		}
	}

	// Scan workspace files
	err := filepath.WalkDir(s.workDir, func(absPath string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
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
			for _, pattern := range lp[layer.Name].workspace {
				if matchesPattern(pattern, rel) {
					result[layer.Name].WorkspaceFiles = append(result[layer.Name].WorkspaceFiles, rel)
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		if !matched {
			for _, layer := range s.layers {
				if layer.CatchAll {
					result[layer.Name].WorkspaceFiles = append(result[layer.Name].WorkspaceFiles, rel)
					break
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Scan external paths for each layer
	for _, layer := range s.layers {
		for _, extPattern := range lp[layer.Name].external {
			source := harness.ExpandHome(extPattern)
			source = strings.TrimSuffix(source, "/")

			info, err := os.Stat(source)
			if err != nil {
				continue
			}

			prefix := sanitizePrefix(extPattern)

			if !info.IsDir() {
				rel := filepath.Base(source)
				if !s.ignore.Match(rel) {
					result[layer.Name].ExternalFiles = append(result[layer.Name].ExternalFiles, ExternalFile{
						AbsPath:     source,
						ArchivePath: "__external__/" + prefix + "/" + rel,
					})
				}
				continue
			}

			_ = filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil || d.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(source, path)
				if err != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				if s.ignore.Match(rel) {
					return nil
				}
				result[layer.Name].ExternalFiles = append(result[layer.Name].ExternalFiles, ExternalFile{
					AbsPath:     path,
					ArchivePath: "__external__/" + prefix + "/" + rel,
				})
				return nil
			})
		}
	}

	// Sort for deterministic output
	for _, sr := range result {
		sort.Strings(sr.WorkspaceFiles)
		sort.Slice(sr.ExternalFiles, func(i, j int) bool {
			return sr.ExternalFiles[i].ArchivePath < sr.ExternalFiles[j].ArchivePath
		})
	}

	return result, nil
}

// BuildExternalPathMap creates the archive-prefix -> source-path mapping
// needed to restore external files to their original locations.
// Paths are stored with ~/ prefix for portability when possible.
func BuildExternalPathMap(extFiles []ExternalFile) map[string]string {
	home, _ := os.UserHomeDir()
	pathMap := make(map[string]string)
	for _, ef := range extFiles {
		parts := strings.SplitN(ef.ArchivePath, "/", 3)
		if len(parts) < 3 {
			continue
		}
		archivePrefix := parts[0] + "/" + parts[1] + "/"
		if _, ok := pathMap[archivePrefix]; ok {
			continue
		}
		// Reconstruct source dir
		relPortion := parts[2]
		sourceDir := strings.TrimSuffix(ef.AbsPath, string(filepath.Separator)+filepath.FromSlash(relPortion))
		// Store as ~/... for portability
		if home != "" && strings.HasPrefix(sourceDir, home) {
			sourceDir = "~" + sourceDir[len(home):]
		}
		pathMap[archivePrefix] = sourceDir
	}
	return pathMap
}

// sanitizePrefix creates a safe archive prefix from an external pattern path.
func sanitizePrefix(pattern string) string {
	p := strings.TrimPrefix(pattern, "~/")
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	p = strings.ReplaceAll(p, "/", "-")
	p = strings.ReplaceAll(p, ".", "-")
	if p == "" {
		p = "external"
	}
	return p
}
