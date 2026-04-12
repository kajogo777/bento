package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeParseSessions(t *testing.T) {
	oc := OpenCode{}

	// Use a workspace that has opencode sessions.
	// The opencode data on this machine is under ~/.local/share/opencode/storage/.
	// Discover a valid workDir from project files.
	workDir := findOpenCodeWorkDir()
	if workDir == "" {
		t.Skip("no OpenCode project data found")
	}

	sessions, err := oc.ParseSessions(workDir)
	if err != nil {
		t.Fatalf("ParseSessions error: %v", err)
	}

	if len(sessions) == 0 {
		t.Fatal("expected at least one session")
	}

	for _, s := range sessions {
		id := s.SessionID
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Printf("  %s  %s  %3d msgs  model=%s  %s  %q\n",
			s.Agent, id, s.MessageCount, s.Model, s.Updated, s.Title)
	}
}

func TestOpenCodeReadSession(t *testing.T) {
	oc := OpenCode{}

	workDir := findOpenCodeWorkDir()
	if workDir == "" {
		t.Skip("no OpenCode project data found")
	}

	sessions, err := oc.ParseSessions(workDir)
	if err != nil || len(sessions) == 0 {
		t.Skip("no sessions to read")
	}

	// Read the first session (use prefix).
	prefix := sessions[0].SessionID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	session, err := oc.ReadSession(workDir, prefix)
	if err != nil {
		t.Fatalf("ReadSession error: %v", err)
	}
	if session == nil {
		t.Fatal("expected session, got nil")
	}

	fmt.Printf("Session %s: %d messages\n", session.SessionID[:12], len(session.Messages))

	for i, msg := range session.Messages {
		if msg.Role == "" {
			t.Errorf("message %d has empty role", i)
		}
		for _, block := range msg.Content {
			if block.Type == "" {
				t.Errorf("message %d has content block with empty type", i)
			}
		}
	}

	// Print first 3 messages for visual inspection.
	limit := 3
	if len(session.Messages) < limit {
		limit = len(session.Messages)
	}
	for i := 0; i < limit; i++ {
		msg := session.Messages[i]
		fmt.Printf("\n  [%d] role=%s ts=%s model=%s blocks=%d\n",
			i, msg.Role, msg.Timestamp, msg.Model, len(msg.Content))
		for j, block := range msg.Content {
			b, _ := json.Marshal(block)
			text := string(b)
			if len(text) > 150 {
				text = text[:150] + "..."
			}
			fmt.Printf("    block[%d]: %s\n", j, text)
		}
	}
}

func TestOpenCodeRawSessionPath(t *testing.T) {
	oc := OpenCode{}

	workDir := findOpenCodeWorkDir()
	if workDir == "" {
		t.Skip("no OpenCode project data found")
	}

	sessions, err := oc.ParseSessions(workDir)
	if err != nil || len(sessions) == 0 {
		t.Skip("no sessions to read")
	}

	path := oc.RawSessionPath(workDir, sessions[0].SessionID)
	if path == "" {
		t.Fatal("expected non-empty raw session path")
	}
	fmt.Printf("Raw session path: %s\n", path)
}

// TestOpenCodeSQLiteSessions tests session parsing from the SQLite database (v1.4+).
func TestOpenCodeSQLiteSessions(t *testing.T) {
	oc := OpenCode{}

	// The georgebuilds workspace has sessions only in SQLite (created with v1.4.x).
	workDir := "/Users/georgefahmy/Desktop/projects/georgebuilds"

	sessions, err := oc.ParseSessions(workDir)
	if err != nil {
		t.Fatalf("ParseSessions error: %v", err)
	}
	if len(sessions) == 0 {
		t.Skip("no SQLite sessions found for georgebuilds")
	}

	for _, s := range sessions {
		id := s.SessionID
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Printf("  [sqlite] %s  %s  %3d msgs  model=%s  %s  %q\n",
			s.Agent, id, s.MessageCount, s.Model, s.Updated, s.Title)
	}

	// Read the full session.
	prefix := sessions[0].SessionID[:8]
	session, err := oc.ReadSession(workDir, prefix)
	if err != nil {
		t.Fatalf("ReadSession error: %v", err)
	}
	if session == nil {
		t.Fatal("expected session, got nil")
	}

	fmt.Printf("Session %s: %d messages\n", session.SessionID[:12], len(session.Messages))

	// Check for reasoning blocks (v1.4+ feature).
	hasReasoning := false
	for i, msg := range session.Messages {
		if msg.Role == "" {
			t.Errorf("message %d has empty role", i)
		}
		for _, block := range msg.Content {
			if block.Type == "" {
				t.Errorf("message %d has content block with empty type", i)
			}
			if block.Type == "thinking" {
				hasReasoning = true
			}
		}
	}

	// Print messages for visual inspection.
	for i, msg := range session.Messages {
		fmt.Printf("\n  [%d] role=%s ts=%s model=%s blocks=%d\n",
			i, msg.Role, msg.Timestamp, msg.Model, len(msg.Content))
		for j, block := range msg.Content {
			b, _ := json.Marshal(block)
			text := string(b)
			if len(text) > 150 {
				text = text[:150] + "..."
			}
			fmt.Printf("    block[%d]: %s\n", j, text)
		}
	}

	if hasReasoning {
		fmt.Println("\n  (reasoning/thinking blocks detected)")
	}
}

// findOpenCodeWorkDir discovers a valid workspace directory from opencode project files.
func findOpenCodeWorkDir() string {
	storageDir := openCodeStorageDir()
	if storageDir == "" {
		return ""
	}

	entries, _ := os.ReadDir(filepath.Join(storageDir, "project"))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(storageDir, "project", entry.Name()))
		if err != nil {
			continue
		}
		var proj ocProject
		if json.Unmarshal(data, &proj) == nil && proj.Worktree != "" {
			// Verify this project has sessions.
			sessionDir := filepath.Join(storageDir, "session", proj.ID)
			if info, err := os.Stat(sessionDir); err == nil && info.IsDir() {
				return proj.Worktree
			}
		}
	}
	return ""
}
