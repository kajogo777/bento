package extension

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kajogo777/bento/internal/manifest"
	_ "modernc.org/sqlite"
)

// Stakpak session handling — SQLite backend only.
//
// Stakpak's agent client (libs/api/src/client/mod.rs) picks a session-storage
// backend at startup based on the active profile in ~/.stakpak/config.toml:
//
//   - If the profile has a non-empty stakpak.api_key (remote profile):
//     session history lives on the stakpak server behind that API. The local
//     machine keeps no conversation data for these sessions.
//
//   - Otherwise (local profile, provider = "local"): session history lives
//     in ~/.stakpak/data/local.db, a SQLite database. This is what we read.
//
// Both backends implement the same SessionStorage trait in the agent code,
// but only the local one exposes data we can introspect without an API call.
// Bento does not reach out to external APIs during `bento save`, so remote-
// profile sessions don't produce SessionMeta entries today. The raw files
// under <workDir>/.stakpak/session/ (file backups, auto-approve rules, tool
// outputs, pause state, etc.) are still captured in the agent layer via
// Stakpak.Contribute, so workspace state is preserved across save/open; only
// the `bento sessions` / `bento inspect --sessions` metadata listing is
// affected.
//
// Note: .stakpak/session/messages.json and .stakpak/session/checkpoint are
// NOT authoritative session state. They are one-shot dumps written by the
// non-interactive `stakpak agent run` path (cli/src/commands/agent/run/
// mode_async.rs) for resume-from-last-run convenience. Interactive TUI
// sessions persist via SessionStorage (either the API or the local DB) and
// never write those files.
//
// TODO: extend this parser to also emit minimal SessionMeta for remote-
// profile sessions once we have a story for calling the stakpak API from
// within save (or an opt-in flag). The active profile and its provider
// type are read from ProfileConfig { api_key, provider: Remote|Local } in
// cli/src/config/profile.rs of the agent repo.
//
// The SQLite schema we read from:
//
//   sessions(id, title, agent_id, visibility, status, cwd, created_at, updated_at)
//   checkpoints(id, session_id, status, execution_depth, parent_id, state,
//               created_at, updated_at)
//
// Historical sessions are scoped to a workspace by the cwd column. The full
// message history for a session is stored as JSON in the state column of
// its latest checkpoint (ordered by created_at).

// stakpakDBPath returns the path to stakpak's local SQLite database, or ""
// if the database does not exist.
func stakpakDBPath() string {
	p := fmt.Sprintf("%s/data/local.db", stakpakHomeDir())
	if !fileExists(p) {
		return ""
	}
	return p
}

// ParseSessions extracts metadata for all stakpak sessions that belong to
// the given workspace and are stored in the local SQLite database. Sessions
// created under a remote profile are not returned — see the package doc
// comment for details.
func (s Stakpak) ParseSessions(workDir string) ([]manifest.SessionMeta, error) {
	absDir := resolveAbsDir(workDir)
	if absDir == "" {
		return nil, nil
	}
	return parseStakpakDBSessions(absDir), nil
}

// RawSessionPath returns the local SQLite database path if the sessionID
// refers to a local-profile session stored in the DB, or "" otherwise.
// The state itself lives inside the DB; raw-content inspection is handled
// by ReadSession.
func (s Stakpak) RawSessionPath(workDir string, sessionID string) string {
	absDir := resolveAbsDir(workDir)
	if absDir == "" {
		return ""
	}
	dbPath := stakpakDBPath()
	if dbPath == "" {
		return ""
	}
	if id := findStakpakDBSessionID(dbPath, absDir, sessionID); id != "" {
		return dbPath
	}
	return ""
}

// ReadSession loads the full normalized message history for a stakpak
// local-profile session. Returns (nil, nil) if the session does not exist
// in the local DB (which is also the result for remote-profile sessions —
// see package doc).
func (s Stakpak) ReadSession(workDir string, sessionID string) (*manifest.NormalizedSession, error) {
	absDir := resolveAbsDir(workDir)
	if absDir == "" {
		return nil, nil
	}
	dbPath := stakpakDBPath()
	if dbPath == "" {
		return nil, nil
	}
	fullID := findStakpakDBSessionID(dbPath, absDir, sessionID)
	if fullID == "" {
		return nil, nil
	}
	state, err := stakpakLatestCheckpointState(dbPath, fullID)
	if err != nil {
		return nil, fmt.Errorf("reading stakpak checkpoint: %w", err)
	}
	if state == nil {
		return nil, nil
	}
	return stakpakBuildNormalizedSession(fullID, state)
}

// --- SQLite-backed sessions --------------------------------------------

// parseStakpakDBSessions returns metadata for every session in the DB whose
// cwd equals absDir. Message counts come from decoding the latest
// checkpoint's state JSON.
func parseStakpakDBSessions(absDir string) []manifest.SessionMeta {
	dbPath := stakpakDBPath()
	if dbPath == "" {
		return nil
	}

	// Open in read-only + immutable mode so parsing is safe while stakpak
	// is writing and we don't take even a shared lock.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&immutable=1")
	if err != nil {
		return nil
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`
		SELECT id, COALESCE(title,''), COALESCE(created_at,''), COALESCE(updated_at,'')
		FROM sessions
		WHERE cwd = ?
		ORDER BY updated_at DESC
	`, absDir)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var metas []manifest.SessionMeta
	for rows.Next() {
		var id, title, created, updated string
		if err := rows.Scan(&id, &title, &created, &updated); err != nil {
			continue
		}
		meta := manifest.SessionMeta{
			Agent:     "stakpak",
			SessionID: id,
			Title:     title,
			Created:   stakpakNormalizeTimestamp(created),
			Updated:   stakpakNormalizeTimestamp(updated),
		}

		// Look up the latest checkpoint's state to count messages.
		// Errors here are non-fatal: we still report the session, just
		// with MessageCount=0.
		state, _ := stakpakLatestCheckpointState(dbPath, id)
		if state != nil {
			var wrap stakpakCheckpointState
			if err := json.Unmarshal(state, &wrap); err == nil {
				count, derivedTitle := stakpakCountAndTitle(wrap.Messages)
				meta.MessageCount = count
				if meta.Title == "" {
					meta.Title = derivedTitle
				}
			}
		}
		metas = append(metas, meta)
	}
	return metas
}

// stakpakLatestCheckpointState returns the most recent checkpoint's state JSON
// for the given session ID. Returns nil if no checkpoint exists.
func stakpakLatestCheckpointState(dbPath, sessionID string) ([]byte, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	var state sql.NullString
	err = db.QueryRow(`
		SELECT state FROM checkpoints
		WHERE session_id = ? AND state IS NOT NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, sessionID).Scan(&state)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if !state.Valid {
		return nil, nil
	}
	return []byte(state.String), nil
}

// findStakpakDBSessionID resolves a prefix-match against session IDs whose
// cwd equals absDir. Returns the full ID, or "" if no match. An ambiguous
// prefix (multiple matches) returns "" — callers should use a longer prefix.
func findStakpakDBSessionID(dbPath, absDir, prefix string) string {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&immutable=1")
	if err != nil {
		return ""
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query(`SELECT id FROM sessions WHERE cwd = ?`, absDir)
	if err != nil {
		return ""
	}
	defer func() { _ = rows.Close() }()

	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		if id == prefix {
			return id // exact match wins
		}
		if strings.HasPrefix(id, prefix) {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

// --- message decoding --------------------------------------------------

// stakpakCheckpointState is the schema of the `state` column in the
// checkpoints table. Only the subset needed for session decoding is modelled.
type stakpakCheckpointState struct {
	Messages []stakpakMessage `json:"messages"`
}

// stakpakMessage mirrors the OpenAI-compatible wire format stakpak uses
// in its persisted checkpoint state.
type stakpakMessage struct {
	Role       string              `json:"role"` // "user", "assistant", "tool", "system"
	Content    json.RawMessage     `json:"content,omitempty"`
	ToolCalls  []stakpakToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Name       string              `json:"name,omitempty"`
	Usage      *stakpakUsage       `json:"usage,omitempty"`
}

type stakpakToolCall struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"` // "function"
	Function stakpakToolFunction `json:"function"`
}

type stakpakToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string
}

type stakpakUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	PromptDetails    *struct {
		InputTokens      int `json:"input_tokens"`
		OutputTokens     int `json:"output_tokens"`
		CacheReadTokens  int `json:"cache_read_input_tokens"`
		CacheWriteTokens int `json:"cache_write_input_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// stakpakCountAndTitle counts user+assistant turns and returns the first
// user message as a fallback title. Tool messages are conversation plumbing
// and are not counted as turns, matching how other extensions count messages.
func stakpakCountAndTitle(msgs []stakpakMessage) (count int, title string) {
	for _, m := range msgs {
		switch m.Role {
		case "user", "assistant":
			count++
		}
		if title == "" && m.Role == "user" {
			if text := stakpakContentText(m.Content); text != "" && !isLikelySystemPrompt(text) {
				// Raw title — the save boundary sanitizes it.
				title = text
			}
		}
	}
	return count, title
}

// stakpakContentText extracts a plain text string from a stakpak message
// Content field. Most stakpak content is a plain string, but future versions
// may use typed content blocks like OpenAI vision messages.
func stakpakContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Array of typed blocks (OpenAI vision format).
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

// stakpakBuildNormalizedSession decodes a checkpoint state payload into a
// NormalizedSession.
func stakpakBuildNormalizedSession(sessionID string, raw []byte) (*manifest.NormalizedSession, error) {
	var wrap stakpakCheckpointState
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("decoding stakpak session: %w", err)
	}
	return stakpakMessagesToSession(sessionID, wrap.Messages), nil
}

// stakpakMessagesToSession maps native stakpak messages to NormalizedMessages.
func stakpakMessagesToSession(sessionID string, msgs []stakpakMessage) *manifest.NormalizedSession {
	session := &manifest.NormalizedSession{
		Agent:     "stakpak",
		SessionID: sessionID,
	}
	for _, m := range msgs {
		nm := manifest.NormalizedMessage{Role: m.Role}
		if m.Usage != nil {
			nm.Usage = &manifest.TokenUsage{
				InputTokens:  m.Usage.PromptTokens,
				OutputTokens: m.Usage.CompletionTokens,
			}
			if m.Usage.PromptDetails != nil {
				nm.Usage.CacheRead = m.Usage.PromptDetails.CacheReadTokens
				nm.Usage.CacheCreate = m.Usage.PromptDetails.CacheWriteTokens
			}
		}

		// Content text.
		if text := stakpakContentText(m.Content); text != "" {
			nm.Content = append(nm.Content, manifest.ContentBlock{Type: "text", Text: text})
		}

		// Assistant tool_calls → one tool_use block per call.
		for _, tc := range m.ToolCalls {
			args := json.RawMessage(tc.Function.Arguments)
			// arguments is a JSON-encoded *string* — try to re-decode to raw
			// JSON when possible so downstream renderers can pretty-print it.
			var decoded json.RawMessage
			if json.Unmarshal([]byte(tc.Function.Arguments), &decoded) == nil {
				args = decoded
			}
			nm.Content = append(nm.Content, manifest.ContentBlock{
				Type:      "tool_use",
				ToolUseID: tc.ID,
				Name:      tc.Function.Name,
				Input:     args,
			})
		}

		// tool role → tool_result block referencing the originating call.
		if m.Role == "tool" && m.ToolCallID != "" {
			output := stakpakContentText(m.Content)
			// Replace the plain text block we just emitted with a tool_result.
			nm.Content = []manifest.ContentBlock{{
				Type:         "tool_result",
				ForToolUseID: m.ToolCallID,
				Output:       output,
			}}
		}

		session.Messages = append(session.Messages, nm)
	}
	return session
}

// --- small helpers -----------------------------------------------------

// (resolveAbsDir is defined in opencode_sessions.go and shared across
// extensions.)

// stakpakNormalizeTimestamp converts SQLite ISO8601-with-microseconds strings
// into RFC3339 form. Pass-through on parse failure.
func stakpakNormalizeTimestamp(s string) string {
	if s == "" {
		return ""
	}
	// Stakpak stores timestamps like "2026-02-23T16:40:26.987493+00:00".
	layouts := []string{
		"2006-01-02T15:04:05.999999-07:00",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return s
}

// Ensure Stakpak implements SessionParser.
var _ SessionParser = Stakpak{}
