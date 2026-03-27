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
	if _, err := os.Stat(filepath.Join(workDir, "SOUL.md")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "IDENTITY.md")); err == nil {
		return true
	}
	return false
}

func (o OpenClaw) Layers(workDir string) []LayerDef {
	agentPatterns := []string{
		"SOUL.md", "AGENTS.md", "USER.md", "IDENTITY.md",
		"TOOLS.md", "HEARTBEAT.md", "BOOTSTRAP.md", "MEMORY.md",
		"memory/**", "skills/**", "canvas/**",
	}

	// Add external sessions if we can find the agent scoped to this workspace
	if sessDir := openClawSessionDir(workDir); sessDir != "" {
		agentPatterns = append(agentPatterns, sessDir+"/")
	}

	return []LayerDef{
		DepsLayer(CommonDepsPatterns),
		AgentLayer(agentPatterns),
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

func openClawSessionDir(workDir string) string {
	openclawHome := os.Getenv("OPENCLAW_STATE_DIR")
	if openclawHome == "" {
		openclawHome = ExpandHome("~/.openclaw")
	}
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
