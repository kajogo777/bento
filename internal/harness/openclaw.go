package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OpenClaw detects and configures the OpenClaw agent framework.
type OpenClaw struct{}

func (o OpenClaw) Name() string { return "openclaw" }

func (o OpenClaw) Detect(workDir string) bool {
	// SOUL.md is the most distinctive OpenClaw marker
	if _, err := os.Stat(filepath.Join(workDir, "SOUL.md")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "IDENTITY.md")); err == nil {
		return true
	}
	return false
}

func (o OpenClaw) Layers() []LayerDef {
	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer([]string{
			"SOUL.md", "AGENTS.md", "USER.md", "IDENTITY.md",
			"TOOLS.md", "HEARTBEAT.md", "BOOTSTRAP.md", "MEMORY.md",
			"memory/**",
			"skills/**",
			"canvas/**",
		}),
		ProjectLayer(CommonSourcePatterns),
	}
}

func (o OpenClaw) SessionConfig(workDir string) (*SessionConfig, error) {
	return BaseSessionConfig(o.Name(), workDir), nil
}

func (o OpenClaw) Ignore() []string {
	return append(CommonIgnorePatterns, CommonCredentialFiles...)
}

func (o OpenClaw) SecretPatterns() []string  { return CommonSecretPatterns }
func (o OpenClaw) DefaultHooks() map[string]string { return nil }

// openClawAgentForWorkspace tries to find the OpenClaw agent whose workspace
// matches the given workDir by parsing ~/.openclaw/openclaw.json.
func openClawAgentForWorkspace(openclawHome, workDir string) string {
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absWork); err == nil {
		absWork = resolved
	}

	configFile := filepath.Join(openclawHome, "openclaw.json")
	data, err := os.ReadFile(configFile)
	if err != nil {
		return ""
	}

	// OpenClaw config has agents with workspace paths
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

func (o OpenClaw) ExternalPaths(workDir string) []ExternalPathDef {
	openclawHome := os.Getenv("OPENCLAW_STATE_DIR")
	if openclawHome == "" {
		openclawHome = ExpandHome("~/.openclaw")
	}
	if info, err := os.Stat(openclawHome); err != nil || !info.IsDir() {
		return nil
	}

	// Try to find the agent scoped to this workspace
	agentID := openClawAgentForWorkspace(openclawHome, workDir)
	if agentID == "" {
		// Can't determine project scope; don't capture everything blindly.
		// Users can use external_paths in bento.yaml as escape hatch.
		return nil
	}

	sessionsDir := filepath.Join(openclawHome, "agents", agentID, "sessions")
	if info, err := os.Stat(sessionsDir); err != nil || !info.IsDir() {
		return nil
	}

	return []ExternalPathDef{{
		Source:        sessionsDir,
		ArchivePrefix: "__external__/openclaw/sessions/",
	}}
}
