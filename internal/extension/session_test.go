package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestClaudeCodeParseSessions(t *testing.T) {
	cc := ClaudeCode{}
	workDir := "/Users/georgefahmy/Desktop/projects/bento"

	// Check if the project dir exists (sessions may exist even without .claude/ in workspace).
	if claudeProjectDir(workDir) == "" {
		t.Skip("no Claude Code project directory found")
	}

	sessions, err := cc.ParseSessions(workDir)
	if err != nil {
		t.Fatalf("ParseSessions error: %v", err)
	}

	if len(sessions) == 0 {
		t.Fatal("expected at least one session")
	}

	for _, s := range sessions {
		fmt.Printf("  %s  %s  %3d msgs  %s  %q\n", s.Agent, s.SessionID[:8], s.MessageCount, s.Updated, s.Title)
	}
}

func TestClaudeCodeReadSession(t *testing.T) {
	cc := ClaudeCode{}
	workDir := "/Users/georgefahmy/Desktop/projects/bento"

	if claudeProjectDir(workDir) == "" {
		t.Skip("no Claude Code project directory found")
	}

	sessions, err := cc.ParseSessions(workDir)
	if err != nil || len(sessions) == 0 {
		t.Skip("no sessions to read")
	}

	// Read the first session (use prefix).
	session, err := cc.ReadSession(workDir, sessions[0].SessionID[:8])
	if err != nil {
		t.Fatalf("ReadSession error: %v", err)
	}
	if session == nil {
		t.Fatal("expected session, got nil")
	}

	fmt.Printf("Session %s: %d messages\n", session.SessionID[:8], len(session.Messages))

	// Verify message structure.
	emptyCount := 0
	for i, msg := range session.Messages {
		if msg.Role == "" {
			t.Errorf("message %d has empty role", i)
		}
		if len(msg.Content) == 0 {
			emptyCount++
		}
		for _, block := range msg.Content {
			if block.Type == "" {
				t.Errorf("message %d has content block with empty type", i)
			}
		}
	}
	fmt.Printf("  Messages with empty content: %d/%d\n", emptyCount, len(session.Messages))

	// Print first 3 messages for visual inspection.
	limit := 3
	if len(session.Messages) < limit {
		limit = len(session.Messages)
	}
	for i := 0; i < limit; i++ {
		msg := session.Messages[i]
		fmt.Printf("\n  [%d] role=%s ts=%s model=%s blocks=%d\n", i, msg.Role, msg.Timestamp, msg.Model, len(msg.Content))
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

func TestCountLines(t *testing.T) {
	// Create a temp file with known line count.
	f, err := os.CreateTemp("", "test-lines-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	for i := 0; i < 5; i++ {
		fmt.Fprintln(f, `{"line":`+fmt.Sprint(i)+`}`)
	}
	f.Close()

	count, err := CountLines(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("expected 5 lines, got %d", count)
	}
}

func TestReadFirstLastLine(t *testing.T) {
	f, err := os.CreateTemp("", "test-firstlast-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	fmt.Fprintln(f, `{"first":true}`)
	fmt.Fprintln(f, `{"middle":true}`)
	fmt.Fprintln(f, `{"last":true}`)
	f.Close()

	first, err := ReadFirstLine(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != `{"first":true}` {
		t.Errorf("unexpected first line: %s", first)
	}

	last, err := ReadLastLine(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(last) != `{"last":true}` {
		t.Errorf("unexpected last line: %s", last)
	}
}
