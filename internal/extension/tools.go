package extension

import (
	"os"
	"path/filepath"
)

// ToolVersions detects .tool-versions (asdf) or .mise.toml (mise) files
// and adds them to the deps layer.
type ToolVersions struct{}

func (t ToolVersions) Name() string                                       { return "tool-versions" }
func (t ToolVersions) NormalizePath(_ string) func(path string) string     { return nil }
func (t ToolVersions) ResolvePath(_ string) func(path string) string       { return nil }

func (t ToolVersions) Detect(workDir string) bool {
	if _, err := os.Stat(filepath.Join(workDir, ".tool-versions")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, ".mise.toml")); err == nil {
		return true
	}
	return false
}

func (t ToolVersions) Contribute(_ string) Contribution {
	return Contribution{
		Layers: map[string][]string{
			"deps": {".tool-versions", ".mise.toml"},
		},
	}
}
