package extension

import (
	"os"
	"path/filepath"
	"strings"
)

// Pi detects the Pi coding agent framework (github.com/badlogic/pi-mono).
type Pi struct{}

func (p Pi) Name() string { return "pi" }

func (p Pi) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".pi")); err == nil && info.IsDir() {
		return true
	}
	// Also detect via project-level sessions under ~/.pi/agent/sessions/<hash>/.
	if piProjectDir(workDir) != "" {
		return true
	}
	return false
}

func (p Pi) Contribute(workDir string) Contribution {
	agentPatterns := []string{
		// Project-local state.
		".pi/**", // settings, extensions, skills, prompts, themes
	}

	// Per-project sessions under ~/.pi/agent/sessions/<hash>/.
	// Pi derives the directory name by stripping the leading separator,
	// replacing remaining separators with dashes, and wrapping with "--":
	// /Users/alice/projects/myapp → --Users-alice-projects-myapp--
	if projectDir := piProjectDir(workDir); projectDir != "" {
		agentPatterns = append(agentPatterns, projectDir+"/")
	}

	// User-global state under ~/.pi/agent/.
	piHome := ExpandHome("~/.pi/agent")
	if info, err := os.Stat(piHome); err == nil && info.IsDir() {
		// Global settings and auth.
		for _, name := range []string{"settings.json", "auth.json"} {
			full := filepath.Join(piHome, name)
			if fileExists(full) {
				agentPatterns = append(agentPatterns, full)
			}
		}

		// Global resource directories: extensions, skills, prompts, themes.
		for _, dir := range []string{"extensions", "skills", "prompts", "themes"} {
			full := filepath.Join(piHome, dir)
			if info, err := os.Stat(full); err == nil && info.IsDir() {
				agentPatterns = append(agentPatterns, full+"/")
			}
		}
	}

	// Cross-agent skills directory (~/.agents/skills/).
	agentsSkills := ExpandHome("~/.agents/skills")
	if info, err := os.Stat(agentsSkills); err == nil && info.IsDir() {
		agentPatterns = append(agentPatterns, agentsSkills+"/")
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
	}
}

// piProjectPlaceholder is the stable placeholder used in archive paths
// to replace the workspace-derived session directory.
const piProjectPlaceholder = "/~/.pi/agent/sessions/__BENTO_WORKSPACE__"

func (p Pi) NormalizePath(workDir string) func(path string) string {
	projectDir := piProjectDir(workDir)
	if projectDir == "" {
		return nil
	}
	return PrefixReplacer(PortablePath(projectDir), piProjectPlaceholder)
}

func (p Pi) ResolvePath(workDir string) func(path string) string {
	hash := piProjectHash(workDir)
	if hash == "" {
		return nil
	}
	return PrefixReplacer(piProjectPlaceholder, "/~/.pi/agent/sessions/"+hash)
}

// piProjectHash computes the directory name that Pi uses for a workspace's
// session data. Pi derives the name by:
//  1. Stripping the leading path separator from the absolute path
//  2. Replacing all remaining separators (and colons on Windows) with dashes
//  3. Wrapping with "--" prefix and "--" suffix
//
// For example:
//
//	/Users/alice/projects/myapp → --Users-alice-projects-myapp--
//
// This matches Pi's getDefaultSessionDir() logic in session-manager.ts.
// This function does NOT check filesystem existence, making it safe for use
// at restore time when the target directory may not exist yet.
func piProjectHash(workDir string) string {
	absDir, err := filepath.Abs(workDir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}
	// Strip leading separator, then replace remaining separators with dashes.
	safe := strings.TrimLeft(absDir, string(filepath.Separator))
	safe = strings.ReplaceAll(safe, string(filepath.Separator), "-")
	// On Windows, also replace colons (e.g., C:\Users → C-Users).
	safe = strings.ReplaceAll(safe, ":", "-")
	return "--" + safe + "--"
}

// piProjectDir returns the external directory that Pi uses for this workspace's
// session data, or empty string if it doesn't exist.
func piProjectDir(workDir string) string {
	hash := piProjectHash(workDir)
	if hash == "" {
		return ""
	}
	projectDir := ExpandHome("~/.pi/agent/sessions/" + hash)
	if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
		return ""
	}
	return projectDir
}
