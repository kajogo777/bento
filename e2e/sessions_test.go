//go:build integration

package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kajogo777/bento/internal/manifest"
)

// ---------------------------------------------------------------------------
// TestSessionsBasic: save embeds session metadata, list/inspect work
// ---------------------------------------------------------------------------

func TestSessionsBasic(t *testing.T) {
	dir := makeWorkspace(t)

	// Create Claude Code markers so the extension is detected.
	writeFile(t, dir, "CLAUDE.md", "# Claude Instructions\n")
	writeFile(t, dir, ".claude/settings.json", `{"model":"claude-sonnet-4-20250514"}`)

	// Create synthetic session JSONL files inside .claude/projects/<hash>/.
	projectDir := makeClaudeProjectDir(t, dir)

	// Write a synthetic session file.
	sessionID := "test-e2e-00000000-1111-2222-3333-444444444444"
	sessionFile := filepath.Join(projectDir, sessionID+".jsonl")
	writeSessionJSONL(t, sessionFile, []sessionLine{
		{Type: "user", Role: "user", Timestamp: "2026-04-01T10:00:00Z",
			Content: `"implement auth module"`, Model: ""},
		{Type: "assistant", Role: "assistant", Timestamp: "2026-04-01T10:00:05Z",
			Content: `[{"type":"text","text":"I'll create the auth module."}]`,
			Model:   "claude-opus-4-6", StopReason: "end_turn",
			InputTokens: 100, OutputTokens: 50},
		{Type: "assistant", Role: "assistant", Timestamp: "2026-04-01T10:00:10Z",
			Content: `[{"type":"tool_use","id":"toolu_01","name":"Write","input":{"path":"auth.go","content":"package auth"}}]`,
			Model:   "claude-opus-4-6", StopReason: "tool_use",
			InputTokens: 150, OutputTokens: 30},
		{Type: "user", Role: "user", Timestamp: "2026-04-01T10:00:15Z",
			Content: `[{"type":"tool_result","tool_use_id":"toolu_01","content":"File written"}]`},
		{Type: "assistant", Role: "assistant", Timestamp: "2026-04-01T10:00:20Z",
			Content: `[{"type":"text","text":"Done! The auth module is ready."}]`,
			Model:   "claude-opus-4-6", StopReason: "end_turn",
			InputTokens: 200, OutputTokens: 20},
	})

	// Write a second session.
	sessionID2 := "test-e2e-55555555-6666-7777-8888-999999999999"
	writeSessionJSONL(t, filepath.Join(projectDir, sessionID2+".jsonl"), []sessionLine{
		{Type: "user", Role: "user", Timestamp: "2026-04-02T10:00:00Z",
			Content: `"fix the bug in login"`, Model: ""},
		{Type: "assistant", Role: "assistant", Timestamp: "2026-04-02T10:00:05Z",
			Content: `[{"type":"thinking","thinking":"Let me analyze the issue."},{"type":"text","text":"Found the bug."}]`,
			Model:   "claude-opus-4-6", StopReason: "end_turn",
			InputTokens: 50, OutputTokens: 25},
	})

	// Save checkpoint (sessions should be extracted).
	out := run(t, dir, "save", "--skip-secret-scan", "-m", "with sessions")
	if !strings.Contains(out, "cp-1") {
		t.Fatalf("expected cp-1 in output:\n%s", out)
	}

	// -- Test: bento sessions lists from checkpoint metadata --
	sessOut := run(t, dir, "sessions")
	if !strings.Contains(sessOut, sessionID) {
		t.Errorf("sessions output should contain session ID %s:\n%s", sessionID, sessOut)
	}
	if !strings.Contains(sessOut, sessionID2) {
		t.Errorf("sessions output should contain session ID %s:\n%s", sessionID2, sessOut)
	}
	if !strings.Contains(sessOut, "implement auth module") {
		t.Errorf("sessions output should contain title:\n%s", sessOut)
	}
	if !strings.Contains(sessOut, "fix the bug in login") {
		t.Errorf("sessions output should contain second title:\n%s", sessOut)
	}
	if !strings.Contains(sessOut, "claude-code") {
		t.Errorf("sessions output should contain agent name:\n%s", sessOut)
	}

	// -- Test: bento sessions --json --
	jsonOut := run(t, dir, "sessions", "--json")
	var sessionsList []manifest.SessionMeta
	if err := json.Unmarshal([]byte(jsonOut), &sessionsList); err != nil {
		t.Fatalf("sessions --json is not valid JSON: %v\n%s", err, jsonOut)
	}
	if len(sessionsList) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessionsList))
	}

	// Find our first session and verify metadata.
	var found *manifest.SessionMeta
	for i, s := range sessionsList {
		if s.SessionID == sessionID {
			found = &sessionsList[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("session %s not found in JSON output", sessionID)
	}
	if found.Agent != "claude-code" {
		t.Errorf("expected agent=claude-code, got %s", found.Agent)
	}
	if found.MessageCount != 5 {
		t.Errorf("expected 5 messages, got %d", found.MessageCount)
	}
	if found.Title != "implement auth module" {
		t.Errorf("expected title 'implement auth module', got %q", found.Title)
	}
	if found.Model != "claude-opus-4-6" {
		t.Errorf("expected model claude-opus-4-6, got %s", found.Model)
	}
	if found.Created != "2026-04-01T10:00:00Z" {
		t.Errorf("expected created 2026-04-01T10:00:00Z, got %s", found.Created)
	}

	// -- Test: bento sessions --agent filter --
	filteredOut := run(t, dir, "sessions", "--agent", "nonexistent")
	if !strings.Contains(filteredOut, "No sessions") {
		t.Errorf("expected no sessions for nonexistent agent:\n%s", filteredOut)
	}

	// -- Test: bento inspect --sessions --
	inspectOut := run(t, dir, "inspect", "--sessions")
	if !strings.Contains(inspectOut, "Sessions:") {
		t.Errorf("inspect --sessions should show Sessions section:\n%s", inspectOut)
	}
	if !strings.Contains(inspectOut, "claude-code") {
		t.Errorf("inspect --sessions should show agent name:\n%s", inspectOut)
	}

	// -- Test: bento inspect (without --sessions) shows summary --
	inspectBrief := run(t, dir, "inspect")
	if !strings.Contains(inspectBrief, "use --sessions for details") {
		t.Errorf("inspect without --sessions should show summary hint:\n%s", inspectBrief)
	}
}

// ---------------------------------------------------------------------------
// TestSessionsInspect: full session content via bento sessions inspect
// ---------------------------------------------------------------------------

func TestSessionsInspect(t *testing.T) {
	dir := makeWorkspace(t)
	writeFile(t, dir, "CLAUDE.md", "# Claude Instructions\n")

	projectDir := makeClaudeProjectDir(t, dir)

	sessionID := "test-e2e-inspect-aaaa-bbbb-cccc-dddddddddddd"
	writeSessionJSONL(t, filepath.Join(projectDir, sessionID+".jsonl"), []sessionLine{
		{Type: "user", Role: "user", Timestamp: "2026-04-01T10:00:00Z",
			Content: `"hello world"`, Model: ""},
		{Type: "assistant", Role: "assistant", Timestamp: "2026-04-01T10:00:05Z",
			Content: `[{"type":"thinking","thinking":"The user said hello."},{"type":"text","text":"Hi there!"}]`,
			Model:   "claude-opus-4-6", StopReason: "end_turn",
			InputTokens: 10, OutputTokens: 5},
	})

	// Save checkpoint.
	run(t, dir, "save", "--skip-secret-scan")

	// -- Test: normalized JSON output --
	jsonOut := run(t, dir, "sessions", "inspect", sessionID)
	var session manifest.NormalizedSession
	if err := json.Unmarshal([]byte(jsonOut), &session); err != nil {
		t.Fatalf("sessions inspect output is not valid JSON: %v\n%s", err, jsonOut)
	}

	if session.Agent != "claude-code" {
		t.Errorf("expected agent=claude-code, got %s", session.Agent)
	}
	if session.SessionID != sessionID {
		t.Errorf("expected sessionId=%s, got %s", sessionID, session.SessionID)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(session.Messages))
	}

	// Verify user message.
	userMsg := session.Messages[0]
	if userMsg.Role != "user" {
		t.Errorf("expected first message role=user, got %s", userMsg.Role)
	}
	if len(userMsg.Content) != 1 || userMsg.Content[0].Type != "text" {
		t.Errorf("expected first message to have one text block, got %d blocks", len(userMsg.Content))
	}
	if userMsg.Content[0].Text != "hello world" {
		t.Errorf("expected text 'hello world', got %q", userMsg.Content[0].Text)
	}

	// Verify assistant message with thinking + text.
	assistMsg := session.Messages[1]
	if assistMsg.Role != "assistant" {
		t.Errorf("expected second message role=assistant, got %s", assistMsg.Role)
	}
	if assistMsg.Model != "claude-opus-4-6" {
		t.Errorf("expected model=claude-opus-4-6, got %s", assistMsg.Model)
	}
	if assistMsg.StopReason != "end_turn" {
		t.Errorf("expected stopReason=end_turn, got %s", assistMsg.StopReason)
	}
	if assistMsg.Usage == nil {
		t.Error("expected usage data")
	} else {
		if assistMsg.Usage.InputTokens != 10 {
			t.Errorf("expected inputTokens=10, got %d", assistMsg.Usage.InputTokens)
		}
	}

	// Should have thinking + text blocks.
	if len(assistMsg.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(assistMsg.Content))
	}
	if assistMsg.Content[0].Type != "thinking" {
		t.Errorf("expected first block type=thinking, got %s", assistMsg.Content[0].Type)
	}
	if assistMsg.Content[1].Type != "text" || assistMsg.Content[1].Text != "Hi there!" {
		t.Errorf("expected second block text='Hi there!', got type=%s text=%q",
			assistMsg.Content[1].Type, assistMsg.Content[1].Text)
	}

	// -- Test: prefix matching --
	prefixOut := run(t, dir, "sessions", "inspect", "test-e2e-inspect")
	var prefixSession manifest.NormalizedSession
	if err := json.Unmarshal([]byte(prefixOut), &prefixSession); err != nil {
		t.Fatalf("prefix match inspect should work: %v", err)
	}
	if prefixSession.SessionID != sessionID {
		t.Errorf("prefix match should find full session, got %s", prefixSession.SessionID)
	}

	// -- Test: --raw output --
	rawOut := run(t, dir, "sessions", "inspect", sessionID, "--raw")
	lines := strings.Split(strings.TrimSpace(rawOut), "\n")
	if len(lines) < 2 {
		t.Errorf("raw output should have multiple JSONL lines, got %d", len(lines))
	}
	// Each line should be valid JSON.
	for i, line := range lines {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("raw line %d is not valid JSON: %v", i, err)
		}
	}

	// -- Test: --text output --
	textOut := run(t, dir, "sessions", "inspect", sessionID, "--text")
	if !strings.Contains(textOut, "Agent: claude-code") {
		t.Errorf("text output should contain agent header:\n%s", textOut)
	}
	if !strings.Contains(textOut, "hello world") {
		t.Errorf("text output should contain user message:\n%s", textOut)
	}
	if !strings.Contains(textOut, "Hi there!") {
		t.Errorf("text output should contain assistant response:\n%s", textOut)
	}
	if !strings.Contains(textOut, "[thinking]") {
		t.Errorf("text output should show thinking block:\n%s", textOut)
	}

	// -- Test: nonexistent session --
	notFoundOut := runExpectFail(t, dir, "sessions", "inspect", "nonexistent-session-id")
	if !strings.Contains(notFoundOut, "not found") {
		t.Errorf("expected 'not found' error:\n%s", notFoundOut)
	}
}

// ---------------------------------------------------------------------------
// TestSessionsPreFeatureCheckpoint: old checkpoints show no sessions
// ---------------------------------------------------------------------------

func TestSessionsPreFeatureCheckpoint(t *testing.T) {
	dir := makeWorkspace(t)

	// Save without any agent markers.
	run(t, dir, "save", "--skip-secret-scan")

	// Sessions should return empty.
	out := run(t, dir, "sessions")
	if !strings.Contains(out, "No sessions") {
		t.Errorf("expected 'No sessions' for checkpoint without session data:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// TestSessionsMultipleSaves: sessions update across checkpoints
// ---------------------------------------------------------------------------

func TestSessionsMultipleSaves(t *testing.T) {
	dir := makeWorkspace(t)
	writeFile(t, dir, "CLAUDE.md", "# Claude Instructions\n")

	projectDir := makeClaudeProjectDir(t, dir)

	sessionID := "test-e2e-multi-aaaa-bbbb-cccc-dddddddddddd"

	// First save: session with 2 messages.
	writeSessionJSONL(t, filepath.Join(projectDir, sessionID+".jsonl"), []sessionLine{
		{Type: "user", Role: "user", Timestamp: "2026-04-01T10:00:00Z",
			Content: `"start here"`, Model: ""},
		{Type: "assistant", Role: "assistant", Timestamp: "2026-04-01T10:00:05Z",
			Content: `[{"type":"text","text":"OK."}]`,
			Model:   "claude-opus-4-6", StopReason: "end_turn"},
	})
	run(t, dir, "save", "--skip-secret-scan", "-m", "save 1")

	jsonOut1 := run(t, dir, "sessions", "--json")
	var sessions1 []manifest.SessionMeta
	json.Unmarshal([]byte(jsonOut1), &sessions1)
	if len(sessions1) != 1 || sessions1[0].MessageCount != 2 {
		t.Fatalf("expected 1 session with 2 msgs in cp-1, got %d sessions", len(sessions1))
	}

	// Second save: same session now has 4 messages (conversation continued).
	writeSessionJSONL(t, filepath.Join(projectDir, sessionID+".jsonl"), []sessionLine{
		{Type: "user", Role: "user", Timestamp: "2026-04-01T10:00:00Z",
			Content: `"start here"`, Model: ""},
		{Type: "assistant", Role: "assistant", Timestamp: "2026-04-01T10:00:05Z",
			Content: `[{"type":"text","text":"OK."}]`,
			Model:   "claude-opus-4-6", StopReason: "end_turn"},
		{Type: "user", Role: "user", Timestamp: "2026-04-01T10:05:00Z",
			Content: `"now add tests"`, Model: ""},
		{Type: "assistant", Role: "assistant", Timestamp: "2026-04-01T10:05:05Z",
			Content: `[{"type":"text","text":"Done."}]`,
			Model:   "claude-opus-4-6", StopReason: "end_turn"},
	})
	// Touch a workspace file to trigger change detection.
	writeFile(t, dir, "main.go", "package main\n\nfunc main() { /* v2 */ }\n")
	run(t, dir, "save", "--skip-secret-scan", "-m", "save 2")

	jsonOut2 := run(t, dir, "sessions", "--json")
	var sessions2 []manifest.SessionMeta
	json.Unmarshal([]byte(jsonOut2), &sessions2)
	if len(sessions2) != 1 || sessions2[0].MessageCount != 4 {
		t.Fatalf("expected 1 session with 4 msgs in cp-2, got %d sessions with %d msgs",
			len(sessions2), sessions2[0].MessageCount)
	}

	// Verify old checkpoint still has 2 messages.
	jsonOut1Old := run(t, dir, "sessions", "cp-1", "--json")
	var sessions1Old []manifest.SessionMeta
	json.Unmarshal([]byte(jsonOut1Old), &sessions1Old)
	if len(sessions1Old) != 1 || sessions1Old[0].MessageCount != 2 {
		t.Fatalf("cp-1 should still show 2 msgs, got %d", sessions1Old[0].MessageCount)
	}
}

// ---------------------------------------------------------------------------
// TestPiSessionsBasic: Pi sessions are extracted and listed
// ---------------------------------------------------------------------------

func TestPiSessionsBasic(t *testing.T) {
	dir := makeWorkspace(t)

	// Seed Pi marker.
	writeFile(t, dir, ".pi/settings.json", `{"defaultProvider":"anthropic"}`)

	// Create Pi session directory matching this workspace.
	projectDir := makePiProjectDir(t, dir)

	// Write a synthetic session file.
	sessionID := "2026-04-01T10-00-00-000Z_test-e2e-0000-1111-2222-333333333333"
	writePiSessionJSONL(t, filepath.Join(projectDir, sessionID+".jsonl"), []piSessionLine{
		{Type: "session", Timestamp: "2026-04-01T10:00:00.000Z"},
		{Type: "message", ID: "a1b2c3d4", Role: "user", Timestamp: "2026-04-01T10:00:01.000Z",
			Content: `"implement auth module"`},
		{Type: "message", ID: "b2c3d4e5", ParentID: "a1b2c3d4", Role: "assistant", Timestamp: "2026-04-01T10:00:05.000Z",
			Content:      `[{"type":"text","text":"I'll create the auth module."}]`,
			Model:        "claude-sonnet-4-20250514",
			Provider:     "anthropic",
			StopReason:   "stop",
			InputTokens:  100,
			OutputTokens: 50},
		{Type: "message", ID: "c3d4e5f6", ParentID: "b2c3d4e5", Role: "user", Timestamp: "2026-04-01T10:00:10.000Z",
			Content: `"add tests"`},
		{Type: "message", ID: "d4e5f6g7", ParentID: "c3d4e5f6", Role: "assistant", Timestamp: "2026-04-01T10:00:15.000Z",
			Content:    `[{"type":"text","text":"Done!"}]`,
			Model:      "claude-sonnet-4-20250514",
			Provider:   "anthropic",
			StopReason: "stop"},
	})

	// Write a second session.
	sessionID2 := "2026-04-02T10-00-00-000Z_test-e2e-4444-5555-6666-777777777777"
	writePiSessionJSONL(t, filepath.Join(projectDir, sessionID2+".jsonl"), []piSessionLine{
		{Type: "session", Timestamp: "2026-04-02T10:00:00.000Z"},
		{Type: "message", ID: "e5f6g7h8", Role: "user", Timestamp: "2026-04-02T10:00:01.000Z",
			Content: `"fix the bug"`},
		{Type: "message", ID: "f6g7h8i9", ParentID: "e5f6g7h8", Role: "assistant", Timestamp: "2026-04-02T10:00:05.000Z",
			Content:  `[{"type":"thinking","thinking":"Analyzing..."},{"type":"text","text":"Found the bug."}]`,
			Model:    "claude-sonnet-4-20250514",
			Provider: "anthropic",
			StopReason: "stop"},
	})

	// Save checkpoint (sessions should be extracted).
	out := run(t, dir, "save", "--skip-secret-scan", "-m", "with pi sessions")
	if !strings.Contains(out, "cp-1") {
		t.Fatalf("expected cp-1 in output:\n%s", out)
	}

	// -- Test: bento sessions lists from checkpoint metadata --
	sessOut := run(t, dir, "sessions")
	if !strings.Contains(sessOut, "pi") {
		t.Errorf("sessions output should contain agent name 'pi':\n%s", sessOut)
	}
	if !strings.Contains(sessOut, "implement auth module") {
		t.Errorf("sessions output should contain title:\n%s", sessOut)
	}
	if !strings.Contains(sessOut, "fix the bug") {
		t.Errorf("sessions output should contain second title:\n%s", sessOut)
	}

	// -- Test: bento sessions --json --
	jsonOut := run(t, dir, "sessions", "--json")
	var sessionsList []manifest.SessionMeta
	if err := json.Unmarshal([]byte(jsonOut), &sessionsList); err != nil {
		t.Fatalf("sessions --json is not valid JSON: %v\n%s", err, jsonOut)
	}
	if len(sessionsList) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessionsList))
	}

	// Find first session and verify metadata.
	var found *manifest.SessionMeta
	for i, s := range sessionsList {
		if s.SessionID == sessionID {
			found = &sessionsList[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("session %s not found in JSON output", sessionID)
	}
	if found.Agent != "pi" {
		t.Errorf("expected agent=pi, got %s", found.Agent)
	}
	if found.MessageCount != 4 {
		t.Errorf("expected 4 messages, got %d", found.MessageCount)
	}
	if found.Title != "implement auth module" {
		t.Errorf("expected title 'implement auth module', got %q", found.Title)
	}
	if found.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model claude-sonnet-4-20250514, got %s", found.Model)
	}

	// -- Test: bento sessions --agent filter --
	filteredOut := run(t, dir, "sessions", "--agent", "pi")
	if strings.Contains(filteredOut, "No sessions") {
		t.Errorf("filtering by agent=pi should find sessions:\n%s", filteredOut)
	}
	filteredNone := run(t, dir, "sessions", "--agent", "nonexistent")
	if !strings.Contains(filteredNone, "No sessions") {
		t.Errorf("expected no sessions for nonexistent agent:\n%s", filteredNone)
	}
}

// ---------------------------------------------------------------------------
// TestPiSessionsInspect: full Pi session content via bento sessions inspect
// ---------------------------------------------------------------------------

func TestPiSessionsInspect(t *testing.T) {
	dir := makeWorkspace(t)
	writeFile(t, dir, ".pi/settings.json", `{}`)

	projectDir := makePiProjectDir(t, dir)

	sessionID := "2026-04-01T10-00-00-000Z_test-e2e-inspect-aaaa-bbbb-cccccccccccc"
	writePiSessionJSONL(t, filepath.Join(projectDir, sessionID+".jsonl"), []piSessionLine{
		{Type: "session", Timestamp: "2026-04-01T10:00:00.000Z"},
		{Type: "message", ID: "a1000001", Role: "user", Timestamp: "2026-04-01T10:00:01.000Z",
			Content: `"hello world"`},
		{Type: "message", ID: "a1000002", ParentID: "a1000001", Role: "assistant", Timestamp: "2026-04-01T10:00:05.000Z",
			Content:      `[{"type":"thinking","thinking":"The user said hello."},{"type":"text","text":"Hi there!"}]`,
			Model:        "claude-sonnet-4-20250514",
			Provider:     "anthropic",
			StopReason:   "stop",
			InputTokens:  10,
			OutputTokens: 5},
	})

	// Save checkpoint.
	run(t, dir, "save", "--skip-secret-scan")

	// -- Test: normalized JSON output --
	jsonOut := run(t, dir, "sessions", "inspect", sessionID)
	var session manifest.NormalizedSession
	if err := json.Unmarshal([]byte(jsonOut), &session); err != nil {
		t.Fatalf("sessions inspect output is not valid JSON: %v\n%s", err, jsonOut)
	}

	if session.Agent != "pi" {
		t.Errorf("expected agent=pi, got %s", session.Agent)
	}
	if session.SessionID != sessionID {
		t.Errorf("expected sessionId=%s, got %s", sessionID, session.SessionID)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(session.Messages))
	}

	// Verify user message.
	userMsg := session.Messages[0]
	if userMsg.Role != "user" {
		t.Errorf("expected first message role=user, got %s", userMsg.Role)
	}
	if len(userMsg.Content) != 1 || userMsg.Content[0].Type != "text" {
		t.Errorf("expected first message to have one text block, got %d blocks", len(userMsg.Content))
	}
	if userMsg.Content[0].Text != "hello world" {
		t.Errorf("expected text 'hello world', got %q", userMsg.Content[0].Text)
	}

	// Verify assistant message with thinking + text.
	assistMsg := session.Messages[1]
	if assistMsg.Role != "assistant" {
		t.Errorf("expected second message role=assistant, got %s", assistMsg.Role)
	}
	if assistMsg.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model=claude-sonnet-4-20250514, got %s", assistMsg.Model)
	}
	if assistMsg.Usage == nil {
		t.Error("expected usage data")
	} else if assistMsg.Usage.InputTokens != 10 {
		t.Errorf("expected inputTokens=10, got %d", assistMsg.Usage.InputTokens)
	}

	if len(assistMsg.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(assistMsg.Content))
	}
	if assistMsg.Content[0].Type != "thinking" {
		t.Errorf("expected first block type=thinking, got %s", assistMsg.Content[0].Type)
	}
	if assistMsg.Content[1].Type != "text" || assistMsg.Content[1].Text != "Hi there!" {
		t.Errorf("expected second block text='Hi there!', got type=%s text=%q",
			assistMsg.Content[1].Type, assistMsg.Content[1].Text)
	}

	// -- Test: prefix matching --
	prefixOut := run(t, dir, "sessions", "inspect", "2026-04-01T10-00-00-000Z_test-e2e-inspect")
	var prefixSession manifest.NormalizedSession
	if err := json.Unmarshal([]byte(prefixOut), &prefixSession); err != nil {
		t.Fatalf("prefix match inspect should work: %v", err)
	}
	if prefixSession.SessionID != sessionID {
		t.Errorf("prefix match should find full session, got %s", prefixSession.SessionID)
	}

	// -- Test: --raw output --
	rawOut := run(t, dir, "sessions", "inspect", sessionID, "--raw")
	lines := strings.Split(strings.TrimSpace(rawOut), "\n")
	if len(lines) < 2 {
		t.Errorf("raw output should have multiple JSONL lines, got %d", len(lines))
	}
	for i, line := range lines {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("raw line %d is not valid JSON: %v", i, err)
		}
	}

	// -- Test: --text output --
	textOut := run(t, dir, "sessions", "inspect", sessionID, "--text")
	if !strings.Contains(textOut, "Agent: pi") {
		t.Errorf("text output should contain agent header:\n%s", textOut)
	}
	if !strings.Contains(textOut, "hello world") {
		t.Errorf("text output should contain user message:\n%s", textOut)
	}
	if !strings.Contains(textOut, "Hi there!") {
		t.Errorf("text output should contain assistant response:\n%s", textOut)
	}

	// -- Test: nonexistent session --
	notFoundOut := runExpectFail(t, dir, "sessions", "inspect", "nonexistent-session-id")
	if !strings.Contains(notFoundOut, "not found") {
		t.Errorf("expected 'not found' error:\n%s", notFoundOut)
	}
}

// ---------------------------------------------------------------------------
// TestPiSessionName: session_info entry provides display name
// ---------------------------------------------------------------------------

func TestPiSessionName(t *testing.T) {
	dir := makeWorkspace(t)
	writeFile(t, dir, ".pi/settings.json", `{}`)

	projectDir := makePiProjectDir(t, dir)

	sessionID := "2026-04-01T10-00-00-000Z_test-e2e-named-aaaa-bbbb-cccccccccccc"
	writePiSessionJSONL(t, filepath.Join(projectDir, sessionID+".jsonl"), []piSessionLine{
		{Type: "session", Timestamp: "2026-04-01T10:00:00.000Z"},
		{Type: "message", ID: "n1000001", Role: "user", Timestamp: "2026-04-01T10:00:01.000Z",
			Content: `"refactor auth"`},
		{Type: "message", ID: "n1000002", ParentID: "n1000001", Role: "assistant", Timestamp: "2026-04-01T10:00:05.000Z",
			Content: `[{"type":"text","text":"OK"}]`, Model: "claude-sonnet-4-20250514", Provider: "anthropic", StopReason: "stop"},
		// session_info entry sets the display name
		{Type: "session_info", ID: "n1000003", ParentID: "n1000002", Timestamp: "2026-04-01T10:00:06.000Z",
			SessionName: "Auth Refactor Project"},
	})

	run(t, dir, "save", "--skip-secret-scan")

	jsonOut := run(t, dir, "sessions", "--json")
	var sessions []manifest.SessionMeta
	if err := json.Unmarshal([]byte(jsonOut), &sessions); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Title != "Auth Refactor Project" {
		t.Errorf("expected title from session_info, got %q", sessions[0].Title)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type sessionLine struct {
	Type         string
	Role         string
	Timestamp    string
	Content      string // raw JSON for content field
	Model        string
	StopReason   string
	InputTokens  int
	OutputTokens int
}

// makeClaudeProjectDir creates a ~/.claude/projects/<hash>/ directory
// matching the given workspace path for testing. Returns the directory path.
// Registers cleanup to remove it even if the test panics.
func makeClaudeProjectDir(t *testing.T, workDir string) string {
	t.Helper()
	absDir, _ := filepath.Abs(workDir)
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}
	hash := strings.ReplaceAll(absDir, string(filepath.Separator), "-")
	projectDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects", hash)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(projectDir) })
	return projectDir
}

// writeSessionJSONL creates a Claude Code–format JSONL session file.
func writeSessionJSONL(t *testing.T, path string, lines []sessionLine) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	for _, l := range lines {
		content := l.Content
		if content == "" {
			content = `""`
		}

		record := fmt.Sprintf(
			`{"type":%q,"uuid":"uuid-%s","timestamp":%q,"sessionId":"test","message":{"role":%q,"content":%s`,
			l.Type, l.Timestamp, l.Timestamp, l.Role, content)

		if l.Model != "" {
			record += fmt.Sprintf(`,"model":%q`, l.Model)
		}
		if l.StopReason != "" {
			record += fmt.Sprintf(`,"stop_reason":%q`, l.StopReason)
		}
		if l.InputTokens > 0 || l.OutputTokens > 0 {
			record += fmt.Sprintf(`,"usage":{"input_tokens":%d,"output_tokens":%d}`, l.InputTokens, l.OutputTokens)
		}

		record += "}}"
		buf.WriteString(record)
		buf.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(buf.String()), 0644); err != nil {
		t.Fatal(err)
	}
}

// -- Pi session helpers --

type piSessionLine struct {
	Type         string
	ID           string
	ParentID     string
	Timestamp    string
	Role         string // for type: "message"
	Content      string // raw JSON for message content
	Model        string
	Provider     string
	StopReason   string
	InputTokens  int
	OutputTokens int
	SessionName  string // for type: "session_info"
	ModelID      string // for type: "model_change"
}

// makePiProjectDir creates a ~/.pi/agent/sessions/<hash>/ directory
// matching the given workspace path for testing. Returns the directory path.
func makePiProjectDir(t *testing.T, workDir string) string {
	t.Helper()
	absDir, _ := filepath.Abs(workDir)
	if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = resolved
	}
	// Pi hash: "--" + path-without-leading-sep-with-seps-replaced-by-dashes + "--"
	safe := strings.TrimLeft(absDir, string(filepath.Separator))
	safe = strings.ReplaceAll(safe, string(filepath.Separator), "-")
	hash := "--" + safe + "--"
	projectDir := filepath.Join(os.Getenv("HOME"), ".pi", "agent", "sessions", hash)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(projectDir) })
	return projectDir
}

// writePiSessionJSONL creates a Pi-format JSONL session file.
func writePiSessionJSONL(t *testing.T, path string, lines []piSessionLine) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	for _, l := range lines {
		switch l.Type {
		case "session":
			// Session header line.
			buf.WriteString(fmt.Sprintf(
				`{"type":"session","version":3,"id":"test-session","timestamp":%q,"cwd":"/tmp/test"}`,
				l.Timestamp))

		case "message":
			content := l.Content
			if content == "" {
				content = `""`
			}

			// Build message object.
			msg := fmt.Sprintf(`{"role":%q,"content":%s,"timestamp":%d`,
				l.Role, content, 0)
			if l.Model != "" {
				msg += fmt.Sprintf(`,"model":%q`, l.Model)
			}
			if l.Provider != "" {
				msg += fmt.Sprintf(`,"provider":%q`, l.Provider)
			}
			if l.StopReason != "" {
				msg += fmt.Sprintf(`,"stopReason":%q`, l.StopReason)
			}
			if l.InputTokens > 0 || l.OutputTokens > 0 {
				msg += fmt.Sprintf(`,"usage":{"input":%d,"output":%d,"cacheRead":0,"cacheWrite":0}`,
					l.InputTokens, l.OutputTokens)
			}
			msg += "}"

			// Build entry.
			parentField := `"parentId":null`
			if l.ParentID != "" {
				parentField = fmt.Sprintf(`"parentId":%q`, l.ParentID)
			}
			buf.WriteString(fmt.Sprintf(
				`{"type":"message","id":%q,%s,"timestamp":%q,"message":%s}`,
				l.ID, parentField, l.Timestamp, msg))

		case "session_info":
			parentField := `"parentId":null`
			if l.ParentID != "" {
				parentField = fmt.Sprintf(`"parentId":%q`, l.ParentID)
			}
			buf.WriteString(fmt.Sprintf(
				`{"type":"session_info","id":%q,%s,"timestamp":%q,"name":%q}`,
				l.ID, parentField, l.Timestamp, l.SessionName))

		case "model_change":
			parentField := `"parentId":null`
			if l.ParentID != "" {
				parentField = fmt.Sprintf(`"parentId":%q`, l.ParentID)
			}
			buf.WriteString(fmt.Sprintf(
				`{"type":"model_change","id":%q,%s,"timestamp":%q,"provider":%q,"modelId":%q}`,
				l.ID, parentField, l.Timestamp, l.Provider, l.ModelID))
		}

		buf.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(buf.String()), 0644); err != nil {
		t.Fatal(err)
	}
}
