package extension

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/kajogo777/bento/internal/manifest"
)

// ParseSessions extracts metadata from Claude Cowork session files.
// Sessions are JSON metadata files at:
//
//	~/Library/Application Support/Claude/local-agent-mode-sessions/<user>/<org>/local_<uuid>.json
//
// The actual conversation logs are JSONL files within the session workspace
// directory, using the same format as Claude Code.
func (c ClaudeCowork) ParseSessions(workDir string) ([]manifest.SessionMeta, error) {
	sessionsBase := coworkSessionsBasePath()
	if sessionsBase == "" {
		return nil, nil
	}
	absWork := resolveAbsPath(workDir)
	if absWork == "" {
		return nil, nil
	}

	orgDir, matchedSessions, found := coworkScanOrgDir(sessionsBase, absWork)
	if !found {
		return nil, nil
	}

	var sessions []manifest.SessionMeta
	for _, ms := range matchedSessions {
		meta := ms.meta

		sm := manifest.SessionMeta{
			Agent:     "claude-cowork",
			SessionID: meta.SessionID,
			Title:     meta.Title,
			Model:     meta.Model,
		}

		if meta.CreatedAt > 0 {
			sm.Created = time.UnixMilli(meta.CreatedAt).UTC().Format(time.RFC3339)
		}
		if meta.LastActivityAt > 0 {
			sm.Updated = time.UnixMilli(meta.LastActivityAt).UTC().Format(time.RFC3339)
		}

		// If no title, use the initial message (truncated).
		if sm.Title == "" && meta.InitialMessage != "" {
			runes := []rune(meta.InitialMessage)
			if len(runes) > 80 {
				sm.Title = string(runes[:77]) + "..."
			} else {
				sm.Title = meta.InitialMessage
			}
		}

		// Count messages from the audit.jsonl if available.
		sessionDirName := strings.TrimSuffix(ms.fileName, ".json")
		auditPath := filepath.Join(orgDir, sessionDirName, "audit.jsonl")
		if fileExists(auditPath) {
			sm.MessageCount = coworkCountMessages(auditPath)
		}

		if sm.MessageCount == 0 && sm.Title == "" {
			continue
		}

		sessions = append(sessions, sm)
	}

	return sessions, nil
}

// RawSessionPath returns the path to the audit.jsonl for a given session ID.
func (c ClaudeCowork) RawSessionPath(workDir string, sessionID string) string {
	orgDir := coworkResolveOrgDir(workDir)
	if orgDir == "" {
		return ""
	}
	path, _, _ := coworkFindSessionAudit(orgDir, sessionID)
	return path
}

// ReadSession reads a full Claude Cowork session and returns it in normalized format.
func (c ClaudeCowork) ReadSession(workDir string, sessionID string) (*manifest.NormalizedSession, error) {
	orgDir := coworkResolveOrgDir(workDir)
	if orgDir == "" {
		return nil, nil
	}

	auditPath, fullID, err := coworkFindSessionAudit(orgDir, sessionID)
	if err != nil {
		return nil, err
	}
	if auditPath == "" {
		return nil, nil
	}

	session := &manifest.NormalizedSession{
		Agent:     "claude-cowork",
		SessionID: fullID,
	}

	err = StreamLines(auditPath, func(line []byte) error {
		var rec coworkAuditRecord
		if json.Unmarshal(line, &rec) != nil {
			return nil
		}

		switch rec.Type {
		case "user":
			msg := manifest.NormalizedMessage{
				ID:        rec.UUID,
				Timestamp: rec.Timestamp,
				Role:      "user",
			}
			msg.Content = parseCoworkAuditContent(rec.Message)
			session.Messages = append(session.Messages, msg)

		case "assistant":
			msg := manifest.NormalizedMessage{
				ID:        rec.UUID,
				Timestamp: rec.Timestamp,
				Role:      "assistant",
			}

			if rec.Message != nil {
				var assistantMsg coworkAssistantMessage
				if json.Unmarshal(rec.Message, &assistantMsg) == nil {
					msg.Model = assistantMsg.Model
					msg.StopReason = assistantMsg.StopReason
					if assistantMsg.Usage != nil {
						msg.Usage = &manifest.TokenUsage{
							InputTokens:  assistantMsg.Usage.InputTokens,
							OutputTokens: assistantMsg.Usage.OutputTokens,
							CacheRead:    assistantMsg.Usage.CacheReadInputTokens,
							CacheCreate:  assistantMsg.Usage.CacheCreationInputTokens,
						}
					}
					msg.Content = parseClaudeContent(assistantMsg.Content)
				}
			}
			session.Messages = append(session.Messages, msg)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return session, nil
}

// parseCoworkAuditContent parses the message field from a user audit record.
func parseCoworkAuditContent(raw json.RawMessage) []manifest.ContentBlock {
	if raw == nil {
		return nil
	}

	var msg struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &msg) != nil {
		return nil
	}

	// Content can be a string or an array.
	var text string
	if json.Unmarshal(msg.Content, &text) == nil {
		if text == "" {
			return nil
		}
		return []manifest.ContentBlock{{Type: "text", Text: text}}
	}

	// Delegate to the shared Claude content parser.
	return parseClaudeContent(msg.Content)
}

// -- Cowork audit JSONL schema types (internal) --

// coworkAuditRecord is the top-level structure of each audit.jsonl line.
type coworkAuditRecord struct {
	Type      string          `json:"type"`      // "user", "assistant", "system"
	UUID      string          `json:"uuid"`      // unique record ID
	Timestamp string          `json:"timestamp"` // ISO 8601 (only on some records)
	SessionID string          `json:"session_id"`
	Message   json.RawMessage `json:"message"`
}

// coworkAssistantMessage is the message payload in an assistant audit record.
type coworkAssistantMessage struct {
	Model      string          `json:"model"`
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	StopReason string          `json:"stop_reason"`
	Usage      *coworkUsage    `json:"usage"`
}

// coworkUsage holds token usage from Cowork's audit format.
type coworkUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// Ensure ClaudeCowork implements SessionParser.
var _ SessionParser = ClaudeCowork{}
