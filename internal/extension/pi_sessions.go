package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/manifest"
)

// ParseSessions extracts metadata from Pi session files.
// Sessions are JSONL files at ~/.pi/agent/sessions/<hash>/<timestamp>_<uuid>.jsonl.
//
// The <hash> is derived from the absolute workspace path (wrapped in dashes
// with separators replaced). This couples session lookup to the workspace's
// filesystem location.
func (p Pi) ParseSessions(workDir string) ([]manifest.SessionMeta, error) {
	projectDir := piProjectDir(workDir)
	if projectDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}

	var sessions []manifest.SessionMeta
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		// Pi session filenames: <timestamp>_<uuid>.jsonl
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		path := filepath.Join(projectDir, entry.Name())

		meta := manifest.SessionMeta{
			Agent:     "pi",
			SessionID: sessionID,
		}

		// Parse the session header (first line) and count messages.
		var msgCount int
		var firstTimestamp, lastTimestamp string
		var firstUserText string
		var sessionName string
		var model string
		gotTitle := false
		gotModel := false

		scanErr := StreamLines(path, func(line []byte) error {
			var rec piRecord
			if json.Unmarshal(line, &rec) != nil {
				return nil // skip malformed lines
			}

			switch rec.Type {
			case "session":
				// Session header — extract creation timestamp.
				if rec.Timestamp != "" && firstTimestamp == "" {
					firstTimestamp = rec.Timestamp
				}

			case "session_info":
				// Session display name.
				if rec.Name != "" {
					sessionName = rec.Name
				}

			case "message":
				if rec.Message == nil {
					return nil
				}
				var msg piMessage
				if json.Unmarshal(rec.Message, &msg) != nil {
					return nil
				}

				if msg.Role == "user" || msg.Role == "assistant" {
					msgCount++
				}

				if rec.Timestamp != "" {
					if firstTimestamp == "" {
						firstTimestamp = rec.Timestamp
					}
					lastTimestamp = rec.Timestamp
				}

				if msg.Role == "user" && !gotTitle {
					text := piResolveContentToText(msg.Content)
					if text != "" {
						firstUserText = text
						gotTitle = true
					}
				}

				if msg.Role == "assistant" && !gotModel {
					if msg.Model != "" {
						model = msg.Model
						gotModel = true
					}
				}

		case "model_change":
			if !gotModel && rec.ModelID != "" {
				model = rec.ModelID
				gotModel = true
			}
			}

			return nil
		})
		if scanErr != nil && firstTimestamp == "" {
			continue // total failure with no data, skip
		}

		meta.MessageCount = msgCount
		meta.Created = firstTimestamp
		meta.Updated = lastTimestamp
		meta.Model = model

		if sessionName != "" {
			meta.Title = sessionName
		} else if firstUserText != "" {
			runes := []rune(firstUserText)
			if len(runes) > 80 {
				firstUserText = string(runes[:77]) + "..."
			}
			meta.Title = firstUserText
		}

		if msgCount == 0 {
			continue
		}

		sessions = append(sessions, meta)
	}

	return sessions, nil
}

// RawSessionPath returns the path to the raw JSONL file for a given session ID.
func (p Pi) RawSessionPath(workDir string, sessionID string) string {
	projectDir := piProjectDir(workDir)
	if projectDir == "" {
		return ""
	}
	path, _, _ := findPiSessionFile(projectDir, sessionID)
	return path
}

// ReadSession reads a full Pi session and returns it in normalized format.
// The sessionID is matched by prefix for convenience.
func (p Pi) ReadSession(workDir string, sessionID string) (*manifest.NormalizedSession, error) {
	projectDir := piProjectDir(workDir)
	if projectDir == "" {
		return nil, nil
	}

	path, fullID, err := findPiSessionFile(projectDir, sessionID)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil // not found
	}

	session := &manifest.NormalizedSession{
		Agent:     "pi",
		SessionID: fullID,
	}

	err = StreamLines(path, func(line []byte) error {
		var rec piRecord
		if json.Unmarshal(line, &rec) != nil {
			return nil
		}

		if rec.Type != "message" || rec.Message == nil {
			return nil
		}

		var msg piMessage
		if json.Unmarshal(rec.Message, &msg) != nil {
			return nil
		}

		// Only include user, assistant, and toolResult messages.
		switch msg.Role {
		case "user", "assistant", "toolResult":
		default:
			return nil
		}

		nm := manifest.NormalizedMessage{
			ID:        rec.ID,
			Timestamp: rec.Timestamp,
			Role:      msg.Role,
			Model:     msg.Model,
		}

		if msg.StopReason != "" {
			nm.StopReason = msg.StopReason
		}

		if msg.Usage != nil {
			nm.Usage = &manifest.TokenUsage{
				InputTokens:  msg.Usage.Input,
				OutputTokens: msg.Usage.Output,
				CacheRead:    msg.Usage.CacheRead,
				CacheCreate:  msg.Usage.CacheWrite,
			}
		}

		nm.Content = parsePiContent(msg.Content, msg.Role)

		session.Messages = append(session.Messages, nm)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return session, nil
}

// findPiSessionFile finds a session JSONL file by prefix-matching the session ID.
func findPiSessionFile(projectDir, sessionID string) (path, fullID string, err error) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", "", err
	}

	var matchPath, matchID string
	var matchCount int
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		if id == sessionID {
			return filepath.Join(projectDir, entry.Name()), id, nil
		}
		if strings.HasPrefix(id, sessionID) {
			matchPath = filepath.Join(projectDir, entry.Name())
			matchID = id
			matchCount++
		}
	}

	if matchCount > 1 {
		return "", "", fmt.Errorf("ambiguous session prefix %q matches %d sessions — use a longer prefix", sessionID, matchCount)
	}
	return matchPath, matchID, nil
}

// parsePiContent converts Pi content to normalized ContentBlocks.
// Pi content can be a plain string (user input) or an array of typed blocks.
func parsePiContent(raw json.RawMessage, role string) []manifest.ContentBlock {
	if raw == nil {
		return nil
	}

	// For user messages, content can be a plain string.
	var plainStr string
	if json.Unmarshal(raw, &plainStr) == nil {
		if plainStr == "" {
			return nil
		}
		return []manifest.ContentBlock{{Type: "text", Text: plainStr}}
	}

	// Parse as array of typed content blocks.
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}

	var blocks []manifest.ContentBlock
	for _, item := range items {
		var block piContentBlock
		if json.Unmarshal(item, &block) != nil {
			continue
		}

		switch block.Type {
		case "text":
			if block.Text != "" {
				blocks = append(blocks, manifest.ContentBlock{Type: "text", Text: block.Text})
			}

		case "thinking":
			if block.Thinking != "" {
				blocks = append(blocks, manifest.ContentBlock{Type: "thinking", Thinking: block.Thinking})
			}

		case "toolCall":
			inputJSON, _ := json.Marshal(block.Arguments)
			blocks = append(blocks, manifest.ContentBlock{
				Type:      "tool_use",
				ToolUseID: block.ID,
				Name:      block.Name,
				Input:     inputJSON,
			})

		case "image":
			blocks = append(blocks, manifest.ContentBlock{
				Type:      "image",
				MediaType: block.MimeType,
				Data:      block.Data,
			})
		}
	}

	return blocks
}

// piResolveContentToText extracts plain text from Pi content for title extraction.
func piResolveContentToText(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}

	// Try as plain string.
	var plainStr string
	if json.Unmarshal(raw, &plainStr) == nil {
		return plainStr
	}

	// Try as array of content blocks; return first text block.
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}

	for _, item := range items {
		var block piContentBlock
		if json.Unmarshal(item, &block) == nil && block.Type == "text" && block.Text != "" {
			return block.Text
		}
	}

	return ""
}

// -- Pi JSONL schema types (internal) --

// piRecord is the top-level structure of each JSONL line.
type piRecord struct {
	Type      string          `json:"type"`      // "session", "message", "model_change", "thinking_level_change", "compaction", "branch_summary", "custom", "custom_message", "label", "session_info"
	ID        string          `json:"id"`        // 8-char hex entry ID
	ParentID  *string         `json:"parentId"`  // parent entry ID (null for first entry)
	Timestamp string          `json:"timestamp"` // ISO 8601
	Message   json.RawMessage `json:"message,omitempty"` // for type: "message"

	// type: "session" fields
	Version int    `json:"version,omitempty"`
	CWD     string `json:"cwd,omitempty"`

	// type: "model_change" fields
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"modelId,omitempty"`

	// type: "session_info" fields
	Name string `json:"name,omitempty"`
}

// piMessage is the message payload within a "message" entry.
type piMessage struct {
	Role       string          `json:"role"` // "user", "assistant", "toolResult", "bashExecution", "custom", "branchSummary", "compactionSummary"
	Content    json.RawMessage `json:"content,omitempty"`
	Model      string          `json:"model,omitempty"`
	Provider   string          `json:"provider,omitempty"`
	StopReason string          `json:"stopReason,omitempty"`
	Usage      *piUsage        `json:"usage,omitempty"`
	Timestamp  int64           `json:"timestamp,omitempty"` // Unix ms
}

// piUsage holds token usage.
type piUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cacheRead"`
	CacheWrite int `json:"cacheWrite"`
}

// piContentBlock represents a typed content block in Pi's format.
type piContentBlock struct {
	Type      string                 `json:"type"`                // "text", "image", "thinking", "toolCall"
	Text      string                 `json:"text,omitempty"`      // text content
	Thinking  string                 `json:"thinking,omitempty"`  // thinking content
	ID        string                 `json:"id,omitempty"`        // toolCall ID
	Name      string                 `json:"name,omitempty"`      // toolCall name
	Arguments map[string]interface{} `json:"arguments,omitempty"` // toolCall arguments
	Data      string                 `json:"data,omitempty"`      // image base64 data
	MimeType  string                 `json:"mimeType,omitempty"`  // image mime type
}

