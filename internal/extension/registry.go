package extension

import (
	"os/exec"
	"strings"
)

// allBuiltinExtensions returns every built-in extension in detection order.
func allBuiltinExtensions() []Extension {
	return []Extension{
		// Agent extensions
		ClaudeCode{},
		Codex{},
		OpenCode{},
		OpenClaw{},
		Cursor{},
		AgentsMD{}, // cross-agent AGENTS.md (always checked)

		// Deps extensions
		Node{},
		Python{},
		GoMod{},
		Rust{},

		// Tool extensions
		ToolVersions{},
	}
}

// DetectAll probes the workspace and returns all matching extensions.
func DetectAll(workDir string) []Extension {
	var matched []Extension
	for _, ext := range allBuiltinExtensions() {
		if ext.Detect(workDir) {
			matched = append(matched, ext)
		}
	}
	return matched
}

// FindByName returns the built-in extension with the given name, or nil.
func FindByName(name string) Extension {
	for _, ext := range allBuiltinExtensions() {
		if ext.Name() == name {
			return ext
		}
	}
	return nil
}

// Resolve returns extensions based on config. If explicit is non-empty, only those
// extensions are used (looked up by name). Otherwise, auto-detect all.
func Resolve(workDir string, explicit []string) []Extension {
	if len(explicit) == 0 {
		return DetectAll(workDir)
	}
	var result []Extension
	for _, name := range explicit {
		if ext := FindByName(name); ext != nil {
			result = append(result, ext)
		}
	}
	return result
}

// ResolveAndMerge is the main entry point: resolve extensions, collect contributions, merge.
func ResolveAndMerge(workDir string, explicit []string) MergeResult {
	exts := Resolve(workDir, explicit)
	var contributions []Contribution
	for _, ext := range exts {
		contributions = append(contributions, ext.Contribute(workDir))
	}
	return Merge(contributions)
}

// ActiveExtensionNames returns the names of all active extensions for the workspace.
func ActiveExtensionNames(workDir string, explicit []string) []string {
	exts := Resolve(workDir, explicit)
	names := make([]string, len(exts))
	for i, ext := range exts {
		names[i] = ext.Name()
	}
	return names
}

// execGit runs a git command in the given directory and returns trimmed output.
func execGit(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
