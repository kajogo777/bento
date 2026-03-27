package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpenCode detects and configures the OpenCode agent framework.
type OpenCode struct{}

func (o OpenCode) Name() string { return "opencode" }

func (o OpenCode) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".opencode")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "opencode.json")); err == nil {
		return true
	}
	return false
}

func (o OpenCode) Layers() []LayerDef {
	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer([]string{
			"AGENTS.md",
			".opencode/**",
			"opencode.json",
		}),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (o OpenCode) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := BaseSessionConfig(o.Name(), workDir)
	if out, err := exec.Command("opencode", "--version").Output(); err == nil {
		cfg.AgentVersion = strings.TrimSpace(string(out))
	}
	return cfg, nil
}

func (o OpenCode) Ignore() []string {
	return append(CommonIgnorePatterns, CommonCredentialFiles...)
}

func (o OpenCode) SecretPatterns() []string  { return CommonSecretPatterns }
func (o OpenCode) DefaultHooks() map[string]string { return nil }

// openCodeProjectHash returns the git root commit hash used by OpenCode
// to scope sessions per-project: git rev-list --max-parents=0 HEAD
func openCodeProjectHash(workDir string) string {
	out, err := execGit(workDir, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return ""
	}
	// If multiple root commits, take the first (sorted alphabetically)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func (o OpenCode) ExternalPaths(workDir string) []ExternalPathDef {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		dataDir = ExpandHome("~/.local/share")
	}
	base := filepath.Join(dataDir, "opencode")
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		return nil
	}

	hash := openCodeProjectHash(workDir)
	if hash == "" {
		return nil
	}

	var defs []ExternalPathDef

	// Project-scoped sessions
	sessionDir := filepath.Join(base, "storage", "session", hash)
	if info, err := os.Stat(sessionDir); err == nil && info.IsDir() {
		defs = append(defs, ExternalPathDef{
			Source:        sessionDir,
			ArchivePrefix: "__external__/opencode/sessions/",
		})
	}

	// Project metadata
	projectFile := filepath.Join(base, "storage", "project", hash+".json")
	if _, err := os.Stat(projectFile); err == nil {
		defs = append(defs, ExternalPathDef{
			Source:        projectFile,
			ArchivePrefix: "__external__/opencode/",
		})
	}

	// Project snapshots
	snapshotDir := filepath.Join(base, "snapshot", hash)
	if info, err := os.Stat(snapshotDir); err == nil && info.IsDir() {
		defs = append(defs, ExternalPathDef{
			Source:        snapshotDir,
			ArchivePrefix: "__external__/opencode/snapshots/",
		})
	}

	return defs
}
