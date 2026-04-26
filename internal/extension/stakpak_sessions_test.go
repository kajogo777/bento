package extension

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withStakpakHome redirects stakpak's global data directory to a temp path
// for the duration of a test via the STAKPAK_HOME env var (honored by
// stakpakHomeDir).
func withStakpakHome(t *testing.T, dir string) {
	t.Helper()
	prev, set := os.LookupEnv("STAKPAK_HOME")
	if err := os.Setenv("STAKPAK_HOME", dir); err != nil {
		t.Fatalf("setenv STAKPAK_HOME: %v", err)
	}
	t.Cleanup(func() {
		if set {
			_ = os.Setenv("STAKPAK_HOME", prev)
		} else {
			_ = os.Unsetenv("STAKPAK_HOME")
		}
	})
}

// createStakpakTestDB populates a fresh SQLite DB matching stakpak's schema
// with one session + one checkpoint (holding the provided messages).
// Returns the DB path.
func createStakpakTestDB(t *testing.T, home string, session stakpakTestSession) string {
	t.Helper()
	dataDir := filepath.Join(home, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "local.db")

	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Schema mirrors stakpak's real one (only fields we read).
	stmts := []string{
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			agent_id TEXT,
			visibility TEXT NOT NULL DEFAULT 'PRIVATE',
			status TEXT DEFAULT 'ACTIVE',
			cwd TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE checkpoints (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			status TEXT,
			execution_depth INTEGER,
			parent_id TEXT,
			state TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	_, err = db.Exec(
		`INSERT INTO sessions (id, title, cwd, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		session.id, session.title, session.cwd, session.createdAt, session.updatedAt,
	)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	stateJSON, err := json.Marshal(stakpakCheckpointState{Messages: session.messages})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(
		`INSERT INTO checkpoints (id, session_id, state, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"ckpt-1", session.id, string(stateJSON), session.createdAt, session.updatedAt,
	)
	if err != nil {
		t.Fatalf("insert checkpoint: %v", err)
	}
	return dbPath
}

type stakpakTestSession struct {
	id        string
	title     string
	cwd       string
	createdAt string
	updatedAt string
	messages  []stakpakMessage
}

// --- tests ---------------------------------------------------------------

func TestStakpakParseSessions_NoDB(t *testing.T) {
	home := t.TempDir()
	withStakpakHome(t, home)

	// No DB file exists — parser should return (nil, nil).
	sessions, err := Stakpak{}.ParseSessions(t.TempDir())
	if err != nil {
		t.Fatalf("ParseSessions: %v", err)
	}
	if sessions != nil {
		t.Errorf("expected nil sessions when no DB is present, got %+v", sessions)
	}
}

func TestStakpakParseSessions_DBOnly(t *testing.T) {
	home := t.TempDir()
	withStakpakHome(t, home)

	workDir := t.TempDir()
	absWorkDir := resolveAbsDir(workDir)

	ts := stakpakTestSession{
		id:        "db-session-uuid-1234567890",
		title:     "local-profile historical session",
		cwd:       absWorkDir,
		createdAt: "2026-02-23T16:40:00.000000+00:00",
		updatedAt: "2026-02-23T16:45:00.000000+00:00",
		messages: []stakpakMessage{
			{Role: "user", Content: mustRaw("first query")},
			{Role: "assistant", Content: mustRaw("answer")},
		},
	}
	createStakpakTestDB(t, home, ts)

	sessions, err := Stakpak{}.ParseSessions(workDir)
	if err != nil {
		t.Fatalf("ParseSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.SessionID != ts.id {
		t.Errorf("sessionID: got %q, want %q", s.SessionID, ts.id)
	}
	if s.MessageCount != 2 {
		t.Errorf("messageCount: got %d", s.MessageCount)
	}
	if s.Title != ts.title {
		t.Errorf("title: got %q, want %q", s.Title, ts.title)
	}
	if s.Created == "" || s.Updated == "" {
		t.Errorf("expected Created/Updated, got created=%q updated=%q", s.Created, s.Updated)
	}
	// Normalized to RFC3339 (no fractional seconds).
	if _, err := time.Parse(time.RFC3339, s.Updated); err != nil {
		t.Errorf("Updated is not RFC3339: %q (%v)", s.Updated, err)
	}
}

func TestStakpakParseSessions_DBScopedByCwd(t *testing.T) {
	home := t.TempDir()
	withStakpakHome(t, home)

	myWorkDir := t.TempDir()
	otherWorkDir := t.TempDir()

	// Session belongs to a *different* workspace.
	createStakpakTestDB(t, home, stakpakTestSession{
		id:        "other-session",
		title:     "elsewhere",
		cwd:       resolveAbsDir(otherWorkDir),
		createdAt: "2026-01-01T00:00:00.000000+00:00",
		updatedAt: "2026-01-02T00:00:00.000000+00:00",
		messages: []stakpakMessage{
			{Role: "user", Content: mustRaw("hi")},
			{Role: "assistant", Content: mustRaw("hi back")},
		},
	})

	sessions, err := Stakpak{}.ParseSessions(myWorkDir)
	if err != nil {
		t.Fatalf("ParseSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions (cwd mismatch), got %d", len(sessions))
	}
}

func TestStakpakReadSession_FromDB(t *testing.T) {
	home := t.TempDir()
	withStakpakHome(t, home)

	workDir := t.TempDir()
	absWorkDir := resolveAbsDir(workDir)
	createStakpakTestDB(t, home, stakpakTestSession{
		id:        "db-session-abcdef",
		title:     "historic",
		cwd:       absWorkDir,
		createdAt: "2025-12-01T00:00:00.000000+00:00",
		updatedAt: "2025-12-01T01:00:00.000000+00:00",
		messages: []stakpakMessage{
			{Role: "user", Content: mustRaw("old question")},
			{
				Role:    "assistant",
				Content: mustRaw("calling tool"),
				ToolCalls: []stakpakToolCall{{
					ID:   "tool-1",
					Type: "function",
					Function: stakpakToolFunction{
						Name:      "fetch_url",
						Arguments: `{"url":"https://example.com"}`,
					},
				}},
			},
			{
				Role:       "tool",
				ToolCallID: "tool-1",
				Content:    mustRaw("<html>hello</html>"),
			},
		},
	})

	ns, err := Stakpak{}.ReadSession(workDir, "db-session-abc")
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if ns == nil {
		t.Fatal("ReadSession returned nil")
	}
	if len(ns.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(ns.Messages))
	}

	// Assistant turn must carry both a text block and a tool_use block.
	assistant := ns.Messages[1]
	var sawText, sawToolUse bool
	for _, b := range assistant.Content {
		switch b.Type {
		case "text":
			sawText = true
			if b.Text != "calling tool" {
				t.Errorf("text: %q", b.Text)
			}
		case "tool_use":
			sawToolUse = true
			if b.Name != "fetch_url" {
				t.Errorf("tool name: %q", b.Name)
			}
			if b.ToolUseID != "tool-1" {
				t.Errorf("tool id: %q", b.ToolUseID)
			}
			// Arguments should be reified from JSON-encoded string to object.
			if string(b.Input) == `"{\"url\":\"https://example.com\"}"` {
				t.Errorf("Input was not reified from JSON-string to object")
			}
		}
	}
	if !sawText || !sawToolUse {
		t.Errorf("assistant should have text and tool_use blocks; got %+v", assistant.Content)
	}

	// Tool turn produces a tool_result block referencing the originating call.
	toolTurn := ns.Messages[2]
	if len(toolTurn.Content) != 1 || toolTurn.Content[0].Type != "tool_result" {
		t.Fatalf("expected single tool_result block, got %+v", toolTurn.Content)
	}
	if toolTurn.Content[0].ForToolUseID != "tool-1" {
		t.Errorf("tool_result referenced wrong call: %q", toolTurn.Content[0].ForToolUseID)
	}
	if toolTurn.Content[0].Output != "<html>hello</html>" {
		t.Errorf("tool_result output: %q", toolTurn.Content[0].Output)
	}
}

func TestStakpakRawSessionPath(t *testing.T) {
	home := t.TempDir()
	withStakpakHome(t, home)

	workDir := t.TempDir()
	absWorkDir := resolveAbsDir(workDir)
	createStakpakTestDB(t, home, stakpakTestSession{
		id:        "history-1",
		title:     "old",
		cwd:       absWorkDir,
		createdAt: "2025-01-01T00:00:00.000000+00:00",
		updatedAt: "2025-01-02T00:00:00.000000+00:00",
		messages: []stakpakMessage{
			{Role: "user", Content: mustRaw("h")},
			{Role: "assistant", Content: mustRaw("h")},
		},
	})

	s := Stakpak{}
	got := s.RawSessionPath(workDir, "history-1")
	wantDB := filepath.Join(home, "data", "local.db")
	if got != wantDB {
		// macOS /var vs /private/var symlink normalization.
		if resolved, err := filepath.EvalSymlinks(got); err != nil || resolved != wantDB {
			if resolvedW, err2 := filepath.EvalSymlinks(wantDB); err2 != nil || resolved != resolvedW {
				t.Errorf("historic raw path: got %q, want %q", got, wantDB)
			}
		}
	}

	// Unknown session → empty string.
	if got := s.RawSessionPath(workDir, "nope"); got != "" {
		t.Errorf("unknown session should return empty path, got %q", got)
	}
}

// TestStakpakIsSessionParser ensures the compile-time assertion matches
// the live registry; missing this would drop Stakpak from the save flow.
func TestStakpakIsSessionParser(t *testing.T) {
	var _ SessionParser = Stakpak{}
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".stakpak"), 0o755); err != nil {
		t.Fatal(err)
	}
	exts := Resolve(workDir, nil)
	var found bool
	for _, e := range exts {
		if e.Name() == "stakpak" {
			found = true
			if _, ok := e.(SessionParser); !ok {
				t.Fatal("stakpak extension does not implement SessionParser")
			}
		}
	}
	if !found {
		t.Fatal("stakpak extension not found in Resolve output for a .stakpak workspace")
	}
}

// --- helpers --------------------------------------------------------------

func mustRaw(s string) json.RawMessage {
	raw, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("mustRaw(%q): %v", s, err))
	}
	return raw
}
