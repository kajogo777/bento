package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ClaudeCowork detects the Claude Cowork agent (part of Claude Desktop).
// Cowork stores sessions and workspace state under:
//   - macOS: ~/Library/Application Support/Claude/local-agent-mode-sessions/<user>/<org>/
//   - Linux: ~/.config/Claude/local-agent-mode-sessions/<user>/<org>/
//
// Each session has a metadata JSON and a workspace directory containing
// .claude/ state, audit.jsonl, outputs, and uploads.
type ClaudeCowork struct{}

func (c ClaudeCowork) Name() string { return "claude-cowork" }

func (c ClaudeCowork) Detect(workDir string) bool {
	sessionsBase := coworkSessionsBasePath()
	if sessionsBase == "" {
		return false
	}
	absWork := resolveAbsPath(workDir)
	if absWork == "" {
		return false
	}

	// Quick check: if the base directory doesn't exist, skip all I/O.
	if info, err := os.Stat(sessionsBase); err != nil || !info.IsDir() {
		return false
	}

	_, _, found := coworkScanOrgDir(sessionsBase, absWork)
	return found
}

func (c ClaudeCowork) Contribute(workDir string) Contribution {
	sessionsBase := coworkSessionsBasePath()
	if sessionsBase == "" {
		return Contribution{Layers: map[string][]string{"agent": {}}}
	}
	absWork := resolveAbsPath(workDir)
	if absWork == "" {
		return Contribution{Layers: map[string][]string{"agent": {}}}
	}

	orgDir, matchedSessions, found := coworkScanOrgDir(sessionsBase, absWork)
	if !found {
		return Contribution{Layers: map[string][]string{"agent": {}}}
	}

	var agentPatterns []string

	// Capture workspace-scoped session directories and metadata files.
	// Each session that references this workspace has a directory
	// with .claude/ state, audit.jsonl, outputs, and uploads,
	// plus a session metadata JSON file.
	for _, meta := range matchedSessions {
		sessionDirName := strings.TrimSuffix(meta.fileName, ".json")
		sessionDir := filepath.Join(orgDir, sessionDirName)
		if info, err := os.Stat(sessionDir); err == nil && info.IsDir() {
			agentPatterns = append(agentPatterns, sessionDir+"/")
		}
		// Also include the session metadata JSON itself.
		agentPatterns = append(agentPatterns, filepath.Join(orgDir, meta.fileName))
	}

	// Capture cowork_settings.json (plugin enablement config).
	settingsPath := filepath.Join(orgDir, "cowork_settings.json")
	if fileExists(settingsPath) {
		agentPatterns = append(agentPatterns, settingsPath)
	}

	// Capture spaces.json (workspace/folder groupings).
	spacesPath := filepath.Join(orgDir, "spaces.json")
	if fileExists(spacesPath) {
		agentPatterns = append(agentPatterns, spacesPath)
	}

	// Capture installed plugins. Note: plugin .mcp.json files may contain
	// API keys for third-party services — the gitleaks secret scanner
	// covers these during save.
	pluginsDir := filepath.Join(orgDir, "cowork_plugins")
	if info, err := os.Stat(pluginsDir); err == nil && info.IsDir() {
		agentPatterns = append(agentPatterns, pluginsDir+"/")
	}

	return Contribution{
		Layers: map[string][]string{
			"agent": agentPatterns,
		},
		Ignore: []string{
			// Exclude audit signing keys from all session directories.
			// The scanner matches this against relative paths within each
			// external directory, so the basename pattern works correctly.
			// .audit-key is Cowork-specific and won't collide with project files.
			".audit-key",
		},
	}
}

// coworkPlaceholder is the stable, platform-agnostic placeholder used in
// archive paths to replace the user/org-specific Cowork session directory.
// The placeholder is opaque — it is never resolved against the filesystem
// directly. NormalizePath replaces the real path with this placeholder at
// save time, and ResolvePath expands it back at restore time.
const coworkPlaceholder = "/~/cowork-sessions/__BENTO_WORKSPACE__"

func (c ClaudeCowork) NormalizePath(workDir string) func(path string) string {
	orgDir := coworkResolveOrgDir(workDir)
	if orgDir == "" {
		return nil
	}
	return PrefixReplacer(PortablePath(orgDir), coworkPlaceholder)
}

func (c ClaudeCowork) ResolvePath(workDir string) func(path string) string {
	orgDir := coworkResolveOrgDir(workDir)
	if orgDir == "" {
		return nil
	}
	return PrefixReplacer(coworkPlaceholder, PortablePath(orgDir))
}

// coworkResolveOrgDir is a convenience that resolves the org directory for
// NormalizePath/ResolvePath. Separated to keep those methods concise.
func coworkResolveOrgDir(workDir string) string {
	sessionsBase := coworkSessionsBasePath()
	if sessionsBase == "" {
		return ""
	}
	absWork := resolveAbsPath(workDir)
	if absWork == "" {
		return ""
	}
	orgDir, _, found := coworkScanOrgDir(sessionsBase, absWork)
	if !found {
		return ""
	}
	return orgDir
}

// coworkSessionsBasePath returns the base directory for Cowork sessions.
func coworkSessionsBasePath() string {
	switch runtime.GOOS {
	case "darwin":
		return ExpandHome("~/Library/Application Support/Claude/local-agent-mode-sessions")
	case "linux":
		// Linux uses the same path structure as the unofficial port.
		return ExpandHome("~/.config/Claude/local-agent-mode-sessions")
	default:
		// Windows: not yet supported by Claude Desktop Cowork.
		appData := os.Getenv("LOCALAPPDATA")
		if appData != "" {
			return filepath.Join(appData, "Claude", "local-agent-mode-sessions")
		}
		return ""
	}
}

// coworkMatchedSession holds a parsed session metadata entry matched to the workspace.
type coworkMatchedSession struct {
	fileName string           // e.g., "local_<uuid>.json"
	meta     coworkSessionMeta
}

// coworkScanOrgDir finds the org directory containing sessions for the given
// workspace and returns all matched (non-archived) sessions in one pass.
// This is the single scan point — all callers share this result to avoid
// redundant I/O across Detect/Contribute/ParseSessions.
//
// Directory structure: <base>/<user-uuid>/<org-uuid>/local_<session-uuid>.json
func coworkScanOrgDir(sessionsBase, absWork string) (orgDir string, matched []coworkMatchedSession, found bool) {
	if info, err := os.Stat(sessionsBase); err != nil || !info.IsDir() {
		return "", nil, false
	}

	userDirs, err := os.ReadDir(sessionsBase)
	if err != nil {
		return "", nil, false
	}

	for _, userEntry := range userDirs {
		if !userEntry.IsDir() || userEntry.Name() == "skills-plugin" {
			continue
		}
		userDir := filepath.Join(sessionsBase, userEntry.Name())
		orgDirs, err := os.ReadDir(userDir)
		if err != nil {
			continue
		}
		for _, orgEntry := range orgDirs {
			if !orgEntry.IsDir() {
				continue
			}
			dir := filepath.Join(userDir, orgEntry.Name())
			sessions := coworkMatchSessions(dir, absWork)
			if len(sessions) > 0 {
				return dir, sessions, true
			}
		}
	}
	return "", nil, false
}

// coworkMatchSessions reads all session JSON files in an org directory and
// returns those whose userSelectedFolders references the given workspace.
// Archived sessions are excluded.
func coworkMatchSessions(orgDir, absWork string) []coworkMatchedSession {
	entries, err := os.ReadDir(orgDir)
	if err != nil {
		return nil
	}

	var matched []coworkMatchedSession
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "local_") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(orgDir, entry.Name()))
		if err != nil {
			continue
		}
		var meta coworkSessionMeta
		if json.Unmarshal(data, &meta) != nil {
			continue
		}
		// Skip archived sessions — they are no longer active.
		if meta.IsArchived {
			continue
		}
		if coworkFolderMatchesWorkspace(meta.UserSelectedFolders, absWork) {
			matched = append(matched, coworkMatchedSession{
				fileName: entry.Name(),
				meta:     meta,
			})
		}
	}
	return matched
}

// coworkFolderMatchesWorkspace checks if any folder in the list matches the workspace path.
func coworkFolderMatchesWorkspace(folders []string, absWork string) bool {
	for _, folder := range folders {
		resolved := folder
		if r, err := filepath.EvalSymlinks(folder); err == nil {
			resolved = r
		}
		if resolved == absWork {
			return true
		}
	}
	return false
}

// resolveAbsPath returns the absolute, symlink-resolved path for a directory.
// Returns "" if resolution fails.
func resolveAbsPath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs
}

// coworkSessionMeta is the minimal set of fields we need from the session JSON.
//
// Note: session metadata JSON files may also contain fields like
// enabledMcpTools, remoteMcpServersConfig, and egressAllowedDomains.
// These are not security-sensitive (no tokens), but the gitleaks scanner
// provides a safety net during save regardless.
type coworkSessionMeta struct {
	SessionID           string   `json:"sessionId"`
	UserSelectedFolders []string `json:"userSelectedFolders"`
	CreatedAt           int64    `json:"createdAt"`
	LastActivityAt      int64    `json:"lastActivityAt"`
	Model               string   `json:"model"`
	Title               string   `json:"title"`
	InitialMessage      string   `json:"initialMessage"`
	IsArchived          bool     `json:"isArchived"`
}

// coworkFindSessionAudit finds the audit.jsonl for a session by exact or prefix match.
// Returns an error if the prefix matches multiple sessions (ambiguous).
func coworkFindSessionAudit(orgDir, sessionID string) (auditPath, fullID string, err error) {
	entries, err := os.ReadDir(orgDir)
	if err != nil {
		return "", "", err
	}

	var matchPath, matchID string
	var matchCount int

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "local_") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")

		// Check for exact match first.
		if id == sessionID {
			auditFile := filepath.Join(orgDir, id, "audit.jsonl")
			if fileExists(auditFile) {
				return auditFile, id, nil
			}
			return "", "", nil
		}

		// Prefix match: allow matching with or without the "local_" prefix.
		if strings.HasPrefix(id, sessionID) || strings.HasPrefix(id, "local_"+sessionID) {
			auditFile := filepath.Join(orgDir, id, "audit.jsonl")
			if fileExists(auditFile) {
				matchPath = auditFile
				matchID = id
				matchCount++
			}
		}
	}

	if matchCount > 1 {
		return "", "", fmt.Errorf("ambiguous session prefix %q matches %d sessions — use a longer prefix", sessionID, matchCount)
	}
	return matchPath, matchID, nil
}

// coworkCountMessages counts user and assistant messages in an audit.jsonl.
// Uses minimal JSON parsing (single-field struct) to extract only the type
// field from each line without allocating the full record.
func coworkCountMessages(path string) int {
	var count int
	_ = StreamLines(path, func(line []byte) error {
		var rec struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &rec) == nil {
			if rec.Type == "user" || rec.Type == "assistant" {
				count++
			}
		}
		return nil
	})
	return count
}
