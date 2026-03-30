package extension

import (
	"os"
	"path/filepath"
)

// Stakpak detects the Stakpak AI agent framework.
type Stakpak struct{}

func (s Stakpak) Name() string { return "stakpak" }

func (s Stakpak) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".stakpak")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func (s Stakpak) Contribute(workDir string) Contribution {
	agentPatterns := []string{
		// Project-local state (always included if present).
		".stakpak/config.toml", // project-level configuration
		".stakpak/session/**",  // session state: checkpoints, subagent prompts, backups, messages
	}

	stakpakHome := stakpakHomeDir()

	// Agent memory database. This is the primary knowledge store that
	// persists across sessions — search results, findings, and context.
	memoryDB := filepath.Join(stakpakHome, "data", "local.db")
	if fileExists(memoryDB) {
		agentPatterns = append(agentPatterns, memoryDB)
	}

	// Health check scripts used by autopilot schedules.
	checksDir := filepath.Join(stakpakHome, "checks")
	if info, err := os.Stat(checksDir); err == nil && info.IsDir() {
		agentPatterns = append(agentPatterns, checksDir+"/")
	}

	// Trigger scripts used by autopilot.
	triggersDir := filepath.Join(stakpakHome, "triggers")
	if info, err := os.Stat(triggersDir); err == nil && info.IsDir() {
		agentPatterns = append(agentPatterns, triggersDir+"/")
	}

	// Historical session backups from previous sessions.
	sessionsDir := filepath.Join(stakpakHome, "sessions")
	if info, err := os.Stat(sessionsDir); err == nil && info.IsDir() {
		agentPatterns = append(agentPatterns, sessionsDir+"/")
	}

	// Build ignore list: exclude secrets, credentials, runtime state, and
	// platform-specific binaries. We use specific absolute paths rather than
	// broad patterns because ignore patterns are merged globally across all
	// extensions and could affect unrelated directories.
	var ignorePatterns []string

	// Project-local secrets: the secrets.json file contains plaintext values
	// of redacted secrets (API keys, tokens, passwords). Must never be captured.
	ignorePatterns = append(ignorePatterns, ".stakpak/session/secrets.json")

	// Project-local warden CA: contains private keys for sandbox TLS.
	ignorePatterns = append(ignorePatterns, ".stakpak/warden/**")

	// Global secrets and credentials.
	for _, secretFile := range []string{
		"auth.toml",     // API keys and auth tokens
		"auth.toml.bak", // backup of auth tokens
		"config.toml",   // contains API keys in profile definitions
		"autopilot.toml", // contains Slack/bot tokens
	} {
		p := filepath.Join(stakpakHome, secretFile)
		if fileExists(p) {
			ignorePatterns = append(ignorePatterns, p)
		}
	}

	// Global runtime state and platform binaries — not portable, regenerable.
	for _, runtimeDir := range []string{
		"plugins",   // platform-specific binaries (browser, warden, agent-board)
		"daemon",    // daemon runtime (PID, logs, DB)
		"autopilot", // autopilot runtime (logs, DBs)
		"server",    // server checkpoints (runtime)
		"warden",    // CA private keys for sandbox TLS
		"cache",     // model cache (regenerable)
		"watch",     // file watcher DB (runtime)
	} {
		d := filepath.Join(stakpakHome, runtimeDir)
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			ignorePatterns = append(ignorePatterns, d+"/**")
		}
	}

	// Global session secrets.json (same concern as project-local).
	globalSecrets := filepath.Join(stakpakHome, "session", "secrets.json")
	if fileExists(globalSecrets) {
		ignorePatterns = append(ignorePatterns, globalSecrets)
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
		Ignore: ignorePatterns,
	}
}

// stakpakHomeDir returns the Stakpak home directory.
func stakpakHomeDir() string {
	if h := os.Getenv("STAKPAK_HOME"); h != "" {
		return h
	}
	return ExpandHome("~/.stakpak")
}
