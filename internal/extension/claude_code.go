package extension

import (
	"os"
	"path/filepath"
	"strings"
)

// ClaudeCode detects the Claude Code agent framework.
type ClaudeCode struct{}

func (c ClaudeCode) Name() string { return "claude-code" }

func (c ClaudeCode) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".claude")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "CLAUDE.md")); err == nil {
		return true
	}
	return false
}

func (c ClaudeCode) Contribute(workDir string) Contribution {
	agentPatterns := []string{
		// Project-local state (always included if present).
		"CLAUDE.md",
		"CLAUDE.local.md", // private project preferences (not committed)
		".claude/**",      // rules, settings, skills, commands, agents, etc.
		".mcp.json",       // project-level MCP server configuration
		".worktreeinclude", // gitignored files to copy into new worktrees
	}

	// Per-project auto-memory and sessions under ~/.claude/projects/<hash>/.
	// Claude Code derives the directory name by replacing path separators
	// with dashes in the absolute workspace path.
	if projectDir := claudeProjectDir(workDir); projectDir != "" {
		agentPatterns = append(agentPatterns, projectDir+"/")
	}

	// User-global state under ~/.claude/. These directories contain
	// cross-project configuration that applies to every workspace.
	// We capture each known subdirectory individually so the superset
	// covers all Claude Code versions without pulling in unrelated files.
	claudeHome := ExpandHome("~/.claude")
	if info, err := os.Stat(claudeHome); err == nil && info.IsDir() {
		globalPaths := []struct {
			rel   string // relative to ~/.claude/
			isDir bool
		}{
			{"CLAUDE.md", false},        // user-level instructions
			{"settings.json", false},    // user settings (permissions, hooks, env)
			{"keybindings.json", false}, // custom keyboard shortcuts
			{"rules", true},             // user-level topic rules
			{"skills", true},            // user-level reusable prompts
			{"commands", true},          // user-level custom commands
			{"agents", true},            // user-level subagent definitions
			{"agent-memory", true},      // user-level subagent memory
			{"output-styles", true},     // user-level output styles
		}
		for _, p := range globalPaths {
			full := filepath.Join(claudeHome, p.rel)
			if p.isDir {
				if info, err := os.Stat(full); err == nil && info.IsDir() {
					agentPatterns = append(agentPatterns, full+"/")
				}
			} else {
				if fileExists(full) {
					agentPatterns = append(agentPatterns, full)
				}
			}
		}
	}

	// ~/.claude.json stores app state, OAuth tokens, UI toggles, and
	// personal MCP server configs. Including it captures MCP setup;
	// OAuth tokens are short-lived and not a secret-scan concern.
	claudeJSON := ExpandHome("~/.claude.json")
	if fileExists(claudeJSON) {
		agentPatterns = append(agentPatterns, claudeJSON)
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
	}
}

// claudeProjectDir returns the external directory that Claude Code uses for
// this workspace's project-specific data (auto-memory, sessions, etc.), or
// empty string if it doesn't exist.
//
// Claude Code derives the directory name by replacing path separators with
// dashes in the absolute workspace path. For example:
//
//	/Users/alice/projects/myapp → -Users-alice-projects-myapp
//
// The directory is walked recursively by the scanner, so the memory/
// subdirectory is captured automatically.
func claudeProjectDir(workDir string) string {
	absDir, err := filepath.Abs(workDir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}

	hash := strings.ReplaceAll(absDir, string(filepath.Separator), "-")
	projectDir := ExpandHome("~/.claude/projects/" + hash)
	if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
		return ""
	}
	return projectDir
}
