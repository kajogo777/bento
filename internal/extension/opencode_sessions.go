package extension

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kajogo777/bento/internal/manifest"
	_ "modernc.org/sqlite"
)

// ParseSessions extracts metadata from OpenCode sessions for the given workspace.
//
// OpenCode has two storage backends:
//   - SQLite (v1.4+): ~/.local/share/opencode/opencode.db
//   - File-based (older): ~/.local/share/opencode/storage/
//
// We try SQLite first, then fall back to file-based storage. Sessions from
// both sources are merged and deduplicated by session ID.
func (o OpenCode) ParseSessions(workDir string) ([]manifest.SessionMeta, error) {
	absDir := resolveAbsDir(workDir)
	if absDir == "" {
		return nil, nil
	}

	// Try SQLite first (v1.4+).
	dbSessions := parseSessionsFromDB(absDir)

	// Try file-based storage (older versions).
	fileSessions := parseSessionsFromFiles(absDir)

	// Merge, preferring SQLite (newer data).
	seen := make(map[string]bool)
	var sessions []manifest.SessionMeta
	for _, s := range dbSessions {
		seen[s.SessionID] = true
		sessions = append(sessions, s)
	}
	for _, s := range fileSessions {
		if !seen[s.SessionID] {
			sessions = append(sessions, s)
		}
	}

	return sessions, nil
}

// RawSessionPath returns the path to the raw session file for a given session ID.
// For SQLite sessions, returns the DB path itself.
func (o OpenCode) RawSessionPath(workDir string, sessionID string) string {
	absDir := resolveAbsDir(workDir)
	if absDir == "" {
		return ""
	}

	// Check file-based storage first (returns a specific JSON file).
	if storageDir := openCodeStorageDir(); storageDir != "" {
		if projectHash := fileProjectHash(storageDir, absDir); projectHash != "" {
			path, _, _ := findOCSessionFile(filepath.Join(storageDir, "session", projectHash), sessionID)
			if path != "" {
				return path
			}
		}
	}

	// For SQLite sessions, return the DB path.
	if dbPath := openCodeDBPath(); dbPath != "" {
		return dbPath
	}

	return ""
}

// ReadSession reads a full OpenCode session and returns it in normalized format.
func (o OpenCode) ReadSession(workDir string, sessionID string) (*manifest.NormalizedSession, error) {
	absDir := resolveAbsDir(workDir)
	if absDir == "" {
		return nil, nil
	}

	// Try SQLite first.
	if session, err := readSessionFromDB(absDir, sessionID); session != nil || err != nil {
		return session, err
	}

	// Fall back to file-based.
	return readSessionFromFiles(absDir, sessionID)
}

// ---- SQLite backend (v1.4+) ----

func openOCDB() (*sql.DB, error) {
	dbPath := openCodeDBPath()
	if dbPath == "" {
		return nil, fmt.Errorf("no opencode database found")
	}
	// Open read-only with WAL mode.
	return sql.Open("sqlite", dbPath+"?mode=ro&_journal_mode=WAL")
}

func dbProjectID(db *sql.DB, absDir string) string {
	var id string
	err := db.QueryRow("SELECT id FROM project WHERE worktree = ?", absDir).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

func parseSessionsFromDB(absDir string) []manifest.SessionMeta {
	db, err := openOCDB()
	if err != nil {
		return nil
	}
	defer db.Close()

	projectID := dbProjectID(db, absDir)
	if projectID == "" {
		return nil
	}

	rows, err := db.Query(`
		SELECT s.id, s.title, s.time_created, s.time_updated,
		       (SELECT COUNT(*) FROM message m WHERE m.session_id = s.id) as msg_count,
		       (SELECT json_extract(m2.data, '$.modelID')
		        FROM message m2
		        WHERE m2.session_id = s.id AND json_extract(m2.data, '$.role') = 'assistant'
		        ORDER BY m2.time_created LIMIT 1) as model
		FROM session s
		WHERE s.project_id = ? AND s.time_archived IS NULL
		ORDER BY s.time_created DESC
	`, projectID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var sessions []manifest.SessionMeta
	for rows.Next() {
		var id, title string
		var created, updated int64
		var msgCount int
		var model sql.NullString
		if rows.Scan(&id, &title, &created, &updated, &msgCount, &model) != nil {
			continue
		}

		if msgCount == 0 && title == "" {
			continue
		}

		meta := manifest.SessionMeta{
			Agent:        "opencode",
			SessionID:    id,
			Title:        title,
			MessageCount: msgCount,
		}
		if created > 0 {
			meta.Created = time.UnixMilli(created).UTC().Format(time.RFC3339)
		}
		if updated > 0 {
			meta.Updated = time.UnixMilli(updated).UTC().Format(time.RFC3339)
		}
		if model.Valid && model.String != "" {
			meta.Model = model.String
		}
		sessions = append(sessions, meta)
	}

	return sessions
}

func readSessionFromDB(absDir, sessionID string) (*manifest.NormalizedSession, error) {
	db, err := openOCDB()
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	projectID := dbProjectID(db, absDir)
	if projectID == "" {
		return nil, nil
	}

	// Find session by exact or prefix match.
	fullID, err := findDBSession(db, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	if fullID == "" {
		return nil, nil
	}

	// Load messages.
	msgRows, err := db.Query(`
		SELECT id, time_created, data FROM message
		WHERE session_id = ? ORDER BY time_created, id
	`, fullID)
	if err != nil {
		return nil, err
	}
	defer msgRows.Close()

	session := &manifest.NormalizedSession{
		Agent:     "opencode",
		SessionID: fullID,
	}

	type msgInfo struct {
		id      string
		created int64
	}
	var msgIDs []msgInfo

	for msgRows.Next() {
		var id string
		var created int64
		var dataJSON string
		if msgRows.Scan(&id, &created, &dataJSON) != nil {
			continue
		}

		var msg ocMessage
		if json.Unmarshal([]byte(dataJSON), &msg) != nil {
			continue
		}
		msg.ID = id
		if msg.Time.Created == 0 {
			msg.Time.Created = created
		}

		nm := buildNormalizedMessage(msg)
		session.Messages = append(session.Messages, nm)
		msgIDs = append(msgIDs, msgInfo{id: id, created: created})
	}

	// Load parts for all messages.
	for i, mi := range msgIDs {
		parts := loadDBParts(db, mi.id)
		if len(parts) > 0 {
			session.Messages[i].Content = parts
		}
	}

	return session, nil
}

func findDBSession(db *sql.DB, projectID, sessionID string) (string, error) {
	// Exact match first.
	var id string
	err := db.QueryRow("SELECT id FROM session WHERE id = ? AND project_id = ?", sessionID, projectID).Scan(&id)
	if err == nil {
		return id, nil
	}

	// Prefix match.
	rows, err := db.Query("SELECT id FROM session WHERE id LIKE ? AND project_id = ?", sessionID+"%", projectID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var match string
		if rows.Scan(&match) == nil {
			matches = append(matches, match)
		}
	}

	if len(matches) == 0 {
		return "", nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous session prefix %q matches %d sessions — use a longer prefix", sessionID, len(matches))
	}
	return matches[0], nil
}

func loadDBParts(db *sql.DB, messageID string) []manifest.ContentBlock {
	rows, err := db.Query("SELECT data FROM part WHERE message_id = ? ORDER BY id", messageID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var blocks []manifest.ContentBlock
	for rows.Next() {
		var dataJSON string
		if rows.Scan(&dataJSON) != nil {
			continue
		}
		var p ocPart
		if json.Unmarshal([]byte(dataJSON), &p) != nil {
			continue
		}
		blocks = append(blocks, convertPart(p)...)
	}
	return blocks
}

// ---- File-based backend (older versions) ----

func parseSessionsFromFiles(absDir string) []manifest.SessionMeta {
	storageDir := openCodeStorageDir()
	if storageDir == "" {
		return nil
	}

	projectHash := fileProjectHash(storageDir, absDir)
	if projectHash == "" {
		return nil
	}

	sessionDir := filepath.Join(storageDir, "session", projectHash)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil
	}

	var sessions []manifest.SessionMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessionDir, entry.Name()))
		if err != nil {
			continue
		}

		var ses ocSession
		if json.Unmarshal(data, &ses) != nil {
			continue
		}

		meta := manifest.SessionMeta{
			Agent:     "opencode",
			SessionID: ses.ID,
			Title:     ses.Title,
		}

		if ses.Time.Created > 0 {
			meta.Created = time.UnixMilli(ses.Time.Created).UTC().Format(time.RFC3339)
		}
		if ses.Time.Updated > 0 {
			meta.Updated = time.UnixMilli(ses.Time.Updated).UTC().Format(time.RFC3339)
		}

		// Count messages and extract model from first assistant message.
		msgDir := filepath.Join(storageDir, "message", ses.ID)
		if msgEntries, err := os.ReadDir(msgDir); err == nil {
			meta.MessageCount = len(msgEntries)
			for _, me := range msgEntries {
				if me.IsDir() || !strings.HasSuffix(me.Name(), ".json") {
					continue
				}
				msgData, err := os.ReadFile(filepath.Join(msgDir, me.Name()))
				if err != nil {
					continue
				}
				var msg ocMessage
				if json.Unmarshal(msgData, &msg) != nil {
					continue
				}
				if msg.Role == "assistant" && msg.ModelID != "" {
					meta.Model = msg.ModelID
					break
				}
			}
		}

		if meta.MessageCount == 0 && meta.Title == "" {
			continue
		}
		sessions = append(sessions, meta)
	}

	return sessions
}

func readSessionFromFiles(absDir, sessionID string) (*manifest.NormalizedSession, error) {
	storageDir := openCodeStorageDir()
	if storageDir == "" {
		return nil, nil
	}

	projectHash := fileProjectHash(storageDir, absDir)
	if projectHash == "" {
		return nil, nil
	}

	sessionDir := filepath.Join(storageDir, "session", projectHash)
	_, fullID, err := findOCSessionFile(sessionDir, sessionID)
	if err != nil {
		return nil, err
	}
	if fullID == "" {
		return nil, nil
	}

	// Load all messages for this session.
	msgDir := filepath.Join(storageDir, "message", fullID)
	msgEntries, err := os.ReadDir(msgDir)
	if err != nil {
		return nil, nil
	}

	type msgWithTime struct {
		msg     ocMessage
		created int64
	}
	var msgs []msgWithTime
	for _, entry := range msgEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(msgDir, entry.Name()))
		if err != nil {
			continue
		}
		var msg ocMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		msgs = append(msgs, msgWithTime{msg: msg, created: msg.Time.Created})
	}

	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].created < msgs[j].created
	})

	session := &manifest.NormalizedSession{
		Agent:     "opencode",
		SessionID: fullID,
	}

	for _, mwt := range msgs {
		nm := buildNormalizedMessage(mwt.msg)

		// Load parts from file storage.
		partDir := filepath.Join(storageDir, "part", mwt.msg.ID)
		nm.Content = loadFileParts(partDir)

		session.Messages = append(session.Messages, nm)
	}

	return session, nil
}

func loadFileParts(partDir string) []manifest.ContentBlock {
	entries, err := os.ReadDir(partDir)
	if err != nil {
		return nil
	}

	type partWithID struct {
		part ocPart
		id   string
	}
	var parts []partWithID
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(partDir, entry.Name()))
		if err != nil {
			continue
		}
		var p ocPart
		if json.Unmarshal(data, &p) != nil {
			continue
		}
		parts = append(parts, partWithID{part: p, id: p.ID})
	}

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].id < parts[j].id
	})

	var blocks []manifest.ContentBlock
	for _, pwi := range parts {
		blocks = append(blocks, convertPart(pwi.part)...)
	}
	return blocks
}

// ---- Shared helpers ----

func resolveAbsDir(workDir string) string {
	absDir, err := filepath.Abs(workDir)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}
	return absDir
}

func buildNormalizedMessage(msg ocMessage) manifest.NormalizedMessage {
	nm := manifest.NormalizedMessage{
		ID:   msg.ID,
		Role: msg.Role,
	}

	if msg.Time.Created > 0 {
		nm.Timestamp = time.UnixMilli(msg.Time.Created).UTC().Format(time.RFC3339)
	}

	if msg.ModelID != "" {
		nm.Model = msg.ModelID
	} else if msg.Model.ModelID != "" {
		nm.Model = msg.Model.ModelID
	}

	if msg.Tokens.Input > 0 || msg.Tokens.Output > 0 {
		nm.Usage = &manifest.TokenUsage{
			InputTokens:  msg.Tokens.Input,
			OutputTokens: msg.Tokens.Output,
			CacheRead:    msg.Tokens.Cache.Read,
			CacheCreate:  msg.Tokens.Cache.Write,
		}
	}

	if msg.Finish != "" {
		nm.StopReason = msg.Finish
	}

	return nm
}

// convertPart maps an opencode part to normalized ContentBlocks.
func convertPart(p ocPart) []manifest.ContentBlock {
	switch p.Type {
	case "text":
		if p.Text != "" {
			return []manifest.ContentBlock{{Type: "text", Text: p.Text}}
		}
	case "reasoning":
		if p.Text != "" {
			return []manifest.ContentBlock{{Type: "thinking", Thinking: p.Text}}
		}
	case "tool":
		var blocks []manifest.ContentBlock
		inputJSON, _ := json.Marshal(p.State.Input)
		blocks = append(blocks, manifest.ContentBlock{
			Type:      "tool_use",
			ToolUseID: p.CallID,
			Name:      p.Tool,
			Input:     inputJSON,
		})
		if p.State.Output != "" || p.State.Status == "error" {
			blocks = append(blocks, manifest.ContentBlock{
				Type:         "tool_result",
				ForToolUseID: p.CallID,
				Output:       p.State.Output,
				IsError:      p.State.Status == "error",
			})
		}
		return blocks
	// step-start, step-finish, snapshot — metadata, skip.
	}
	return nil
}

// openCodeProjectHash finds the project hash from either SQLite or file storage.
func openCodeProjectHash(workDir string) string {
	absDir := resolveAbsDir(workDir)
	if absDir == "" {
		return ""
	}

	// Try SQLite first.
	if db, err := openOCDB(); err == nil {
		defer db.Close()
		if id := dbProjectID(db, absDir); id != "" {
			return id
		}
	}

	// Fall back to file-based.
	if storageDir := openCodeStorageDir(); storageDir != "" {
		return fileProjectHash(storageDir, absDir)
	}

	return ""
}

// fileProjectHash finds the project hash by scanning file-based project JSON files.
func fileProjectHash(storageDir, absDir string) string {
	projectDir := filepath.Join(storageDir, "project")
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(projectDir, entry.Name()))
		if err != nil {
			continue
		}
		var proj ocProject
		if json.Unmarshal(data, &proj) != nil {
			continue
		}
		projPath := proj.Worktree
		if resolved, err := filepath.EvalSymlinks(projPath); err == nil {
			projPath = resolved
		}
		if projPath == absDir {
			return proj.ID
		}
	}

	return ""
}

// findOCSessionFile finds a session JSON file by prefix-matching the session ID.
func findOCSessionFile(sessionDir, sessionID string) (path, fullID string, err error) {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return "", "", err
	}

	var matchPath, matchID string
	var matchCount int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if id == sessionID {
			return filepath.Join(sessionDir, entry.Name()), id, nil
		}
		if strings.HasPrefix(id, sessionID) {
			matchPath = filepath.Join(sessionDir, entry.Name())
			matchID = id
			matchCount++
		}
	}

	if matchCount > 1 {
		return "", "", fmt.Errorf("ambiguous session prefix %q matches %d sessions — use a longer prefix", sessionID, matchCount)
	}
	return matchPath, matchID, nil
}

// -- OpenCode storage schema types (internal) --
// Used for both file-based JSON and SQLite data column parsing.

type ocProject struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
}

type ocSession struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectID"`
	Version   string `json:"version"`
	Title     string `json:"title"`
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
	Summary struct {
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
		Files     int `json:"files"`
	} `json:"summary"`
}

type ocMessage struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Role      string `json:"role"` // "user", "assistant"
	ModelID   string `json:"modelID"`
	Model     struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
	Tokens struct {
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
		Cache     struct {
			Read  int `json:"read"`
			Write int `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Finish string  `json:"finish"` // "tool-calls", "stop", etc.
	Agent  string  `json:"agent"`
	Mode   string  `json:"mode"`
	Cost   float64 `json:"cost"`
}

type ocPart struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
	Type      string `json:"type"` // "text", "reasoning", "tool", "step-start", "step-finish", "snapshot"

	// type: "text" or "reasoning"
	Text string `json:"text,omitempty"`

	// type: "tool"
	CallID string  `json:"callID,omitempty"`
	Tool   string  `json:"tool,omitempty"`
	State  ocState `json:"state,omitempty"`

	// type: "snapshot"
	Snapshot string `json:"snapshot,omitempty"`
}

type ocState struct {
	Status string          `json:"status"` // "completed", "error", "running"
	Input  json.RawMessage `json:"input,omitempty"`
	Output string          `json:"output,omitempty"`
	Title  string          `json:"title,omitempty"`
	Time   struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	} `json:"time"`
}
