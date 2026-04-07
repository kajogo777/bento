package workspace

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kajogo777/bento/internal/extension"
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
	workDir       string
	layers        []extension.LayerDef
	ignore        *IgnoreMatcher
	normalizePath func(string) string // optional save-time path normalizer
}

// NewScanner creates a new Scanner for the given workspace directory.
// The optional normalizePath function replaces workspace-derived path
// components with portable placeholders in archive entry names.
func NewScanner(workDir string, layers []extension.LayerDef, ignorePatterns []string, normalizePath func(string) string) *Scanner {
	return &Scanner{
		workDir:       workDir,
		layers:        layers,
		ignore:        NewIgnoreMatcher(ignorePatterns),
		normalizePath: normalizePath,
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
			if extension.IsExternalPattern(p) {
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

	// Scan external paths for each layer.
	// Archive paths are stored as "__external__" + portable path so that:
	//   - paths look natural in diffs and inspect output
	//   - no separate annotation is needed for restore
	for _, layer := range s.layers {
		for _, extPattern := range lp[layer.Name].external {
			source := extension.ExpandHome(extPattern)
			source = strings.TrimSuffix(source, "/")

			info, err := os.Stat(source)
			if err != nil {
				continue
			}

			if !info.IsDir() {
				if !s.ignore.Match(filepath.Base(source)) {
					result[layer.Name].ExternalFiles = append(result[layer.Name].ExternalFiles, ExternalFile{
						AbsPath:     source,
						ArchivePath: "__external__" + s.applyNormalize(portablePath(source)),
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
				if s.ignore.Match(filepath.ToSlash(rel)) {
					return nil
				}
				result[layer.Name].ExternalFiles = append(result[layer.Name].ExternalFiles, ExternalFile{
					AbsPath:     path,
					ArchivePath: "__external__" + s.applyNormalize(portablePath(path)),
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

// applyNormalize applies the scanner's normalizePath function if set.
func (s *Scanner) applyNormalize(path string) string {
	if s.normalizePath != nil {
		return s.normalizePath(path)
	}
	return path
}

// portablePath converts an absolute path to a portable form for use in archive
// entry names. Delegates to extension.PortablePath.
func portablePath(absPath string) string {
	return extension.PortablePath(absPath)
}

// absFromArchivePath converts a portable archive path back to an absolute path
// by expanding ~/. Returns empty string if the path is invalid.
func absFromArchivePath(archivePath string) string {
	// Strip the __external__ sentinel
	p := strings.TrimPrefix(archivePath, "__external__")
	// Expand /~/ home prefix
	if strings.HasPrefix(p, "/~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		return filepath.Join(home, filepath.FromSlash(p[3:]))
	}
	// Plain absolute path — on Windows, portable paths look like /C:/...
	// so strip the leading / if a drive letter follows.
	native := filepath.FromSlash(p)
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		native = filepath.FromSlash(p[1:])
	}
	if filepath.IsAbs(native) {
		return native
	}
	return ""
}
