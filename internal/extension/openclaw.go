package extension

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OpenClaw detects the OpenClaw agent framework.
type OpenClaw struct{}

func (o OpenClaw) Name() string { return "openclaw" }

func (o OpenClaw) Detect(workDir string) bool {
	if _, err := os.Stat(filepath.Join(workDir, "SOUL.md")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "IDENTITY.md")); err == nil {
		return true
	}
	return false
}

func (o OpenClaw) Contribute(workDir string) Contribution {
	agentPatterns := []string{
		"SOUL.md", "USER.md", "IDENTITY.md",
		"TOOLS.md", "HEARTBEAT.md", "BOOTSTRAP.md", "MEMORY.md",
		"memory/**", "skills/**", "canvas/**",
	}

	openclawHome := openClawHome()

	// Add external sessions if we can find the agent scoped to this workspace.
	if sessDir := openClawSessionDir(openclawHome, workDir); sessDir != "" {
		agentPatterns = append(agentPatterns, sessDir+"/")
	}

	// Include the agent config (openclaw.json) from the home directory. It
	// contains agent definitions, model settings, and workspace mappings.
	// Credentials are intentionally excluded for security.
	if configPath := filepath.Join(openclawHome, "openclaw.json"); fileExists(configPath) {
		agentPatterns = append(agentPatterns, configPath)
	}

	// Include the default workspace directory if it matches the current workDir.
	// This covers the case where the user's project IS the openclaw workspace.
	openClawAddDefaultWorkspace(openclawHome, workDir, &agentPatterns)

	// Build ignore list: exclude the OpenClaw credentials directory.
	// We use the specific ~/.openclaw/credentials path rather than a broad
	// "credentials/**" pattern, because a blanket pattern would affect all
	// extensions (ignore patterns are merged globally) and could exclude
	// legitimate credentials/ directories in unrelated projects.
	var ignorePatterns []string
	credsDir := filepath.Join(openclawHome, "credentials")
	if info, err := os.Stat(credsDir); err == nil && info.IsDir() {
		ignorePatterns = append(ignorePatterns, credsDir+"/**")
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
		Ignore: ignorePatterns,
	}
}

// openClawSessionPlaceholder is the stable placeholder for OpenClaw's
// workspace-derived agent session directory.
const openClawSessionPlaceholder = "/~/.openclaw/agents/__BENTO_WORKSPACE__/sessions"

func (o OpenClaw) NormalizePath(workDir string) func(path string) string {
	openclawHome := openClawHome()
	sessDir := openClawSessionDir(openclawHome, workDir)
	if sessDir == "" {
		return nil
	}
	return PrefixReplacer(PortablePath(sessDir), openClawSessionPlaceholder)
}

func (o OpenClaw) ResolvePath(workDir string) func(path string) string {
	openclawHome := openClawHome()
	agentID := openClawAgentForWorkspace(openclawHome, workDir)
	if agentID == "" {
		return nil
	}
	return PrefixReplacer(openClawSessionPlaceholder, PortablePath(filepath.Join(openclawHome, "agents", agentID, "sessions")))
}

// openClawHome returns the OpenClaw home directory.
func openClawHome() string {
	if h := os.Getenv("OPENCLAW_STATE_DIR"); h != "" {
		return h
	}
	return ExpandHome("~/.openclaw")
}

func openClawSessionDir(openclawHome, workDir string) string {
	if info, err := os.Stat(openclawHome); err != nil || !info.IsDir() {
		return ""
	}

	agentID := openClawAgentForWorkspace(openclawHome, workDir)
	if agentID == "" {
		return ""
	}

	sessionsDir := filepath.Join(openclawHome, "agents", agentID, "sessions")
	if info, err := os.Stat(sessionsDir); err != nil || !info.IsDir() {
		return ""
	}
	return sessionsDir
}

// openClawAddDefaultWorkspace adds the default workspace skills directory from
// ~/.openclaw/workspace/ if the workspace path matches the current workDir.
// This captures skills that live in the global workspace but belong to this project.
func openClawAddDefaultWorkspace(openclawHome, workDir string, patterns *[]string) {
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(absWork); err == nil {
		absWork = resolved
	}

	defaultWS := filepath.Join(openclawHome, "workspace")
	if resolved, err := filepath.EvalSymlinks(defaultWS); err == nil {
		defaultWS = resolved
	}

	// If the default workspace IS the current workDir, the in-project patterns
	// already cover it. If it's different, include the skills subdirectory.
	if defaultWS != absWork {
		skillsDir := filepath.Join(openclawHome, "workspace", "skills")
		if info, err := os.Stat(skillsDir); err == nil && info.IsDir() {
			*patterns = append(*patterns, skillsDir+"/")
		}
	}
}

func openClawAgentForWorkspace(openclawHome, workDir string) string {
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absWork); err == nil {
		absWork = resolved
	}

	data, err := os.ReadFile(filepath.Join(openclawHome, "openclaw.json"))
	if err != nil {
		return ""
	}

	var config struct {
		Agents map[string]struct {
			Workspace string `json:"workspace"`
		} `json:"agents"`
	}
	if json.Unmarshal(data, &config) != nil {
		return ""
	}

	for id, agent := range config.Agents {
		ws := agent.Workspace
		if ws == "" {
			ws = filepath.Join(openclawHome, "workspace")
		}
		ws = ExpandHome(ws)
		if resolved, err := filepath.EvalSymlinks(ws); err == nil {
			ws = resolved
		}
		if ws == absWork {
			return id
		}
	}
	return ""
}

// fileExists returns true if the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
