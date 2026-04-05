package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/manifest"
)

// ParseSessions extracts metadata from Claude Code session files.
// Sessions are JSONL files at ~/.claude/projects/<hash>/<sessionId>.jsonl.
//
// The <hash> is derived from the absolute workspace path (separators replaced
// with dashes). This couples session lookup to the workspace's filesystem
// location — see SessionParser docs for the known limitation around
// `bento open` to a different directory.
func (c ClaudeCode) ParseSessions(workDir string) ([]manifest.SessionMeta, error) {
	projectDir := claudeProjectDir(workDir)
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

		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		path := filepath.Join(projectDir, entry.Name())

		meta := manifest.SessionMeta{
			Agent:     "claude-code",
			SessionID: sessionID,
		}

		// Single-pass scan: parse each line's top-level type field to count
		// user/assistant messages and extract metadata. We need full JSON
		// parsing because naive string search for "type":"user" matches
		// inside nested content (tool results containing conversation text).
		var msgCount int
		var firstTimestamp, lastTimestamp string
		var firstUserText string
		var model string
		gotTitle := false
		gotModel := false

		scanErr := StreamLines(path, func(line []byte) error {
			var rec ccRecord
			if json.Unmarshal(line, &rec) != nil {
				return nil // skip malformed lines
			}

			if rec.Type != "user" && rec.Type != "assistant" {
				return nil
			}

			msgCount++

			if rec.Timestamp != "" {
				if firstTimestamp == "" {
					firstTimestamp = rec.Timestamp
				}
				lastTimestamp = rec.Timestamp
			}

			// Only parse content for title/model extraction (first few messages).
			if rec.Type == "user" && !gotTitle {
				text := resolveContentToText(rec.Message.Content)
				if text != "" && !isLikelySystemPrompt(text) {
					firstUserText = text
					gotTitle = true
				}
			}

			if rec.Type == "assistant" && !gotModel && rec.Message.Model != "" {
				model = rec.Message.Model
				gotModel = true
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

		if firstUserText != "" {
			if len(firstUserText) > 80 {
				firstUserText = firstUserText[:77] + "..."
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
func (c ClaudeCode) RawSessionPath(workDir string, sessionID string) string {
	projectDir := claudeProjectDir(workDir)
	if projectDir == "" {
		return ""
	}
	path, _, _ := findSessionFile(projectDir, sessionID)
	return path
}

// ReadSession reads a full Claude Code session and returns it in normalized format.
// The sessionID is matched by prefix for convenience.
func (c ClaudeCode) ReadSession(workDir string, sessionID string) (*manifest.NormalizedSession, error) {
	projectDir := claudeProjectDir(workDir)
	if projectDir == "" {
		return nil, nil
	}

	// Find matching session file (prefix match).
	path, fullID, err := findSessionFile(projectDir, sessionID)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil // not found
	}

	session := &manifest.NormalizedSession{
		Agent:     "claude-code",
		SessionID: fullID,
	}

	err = StreamLines(path, func(line []byte) error {
		var rec ccRecord
		if json.Unmarshal(line, &rec) != nil {
			return nil
		}

		if rec.Type != "user" && rec.Type != "assistant" {
			return nil
		}

		msg := manifest.NormalizedMessage{
			ID:        rec.UUID,
			Timestamp: rec.Timestamp,
			Role:      rec.Type,
			Model:     rec.Message.Model,
		}

		if rec.Message.StopReason != "" {
			msg.StopReason = rec.Message.StopReason
		}

		if rec.Message.Usage != nil {
			msg.Usage = &manifest.TokenUsage{
				InputTokens:  rec.Message.Usage.InputTokens,
				OutputTokens: rec.Message.Usage.OutputTokens,
				CacheRead:    rec.Message.Usage.CacheReadInputTokens,
				CacheCreate:  rec.Message.Usage.CacheCreationInputTokens,
			}
		}

		// Parse content blocks.
		msg.Content = parseClaudeContent(rec.Message.Content)

		session.Messages = append(session.Messages, msg)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return session, nil
}

// findSessionFile finds a session JSONL file by prefix-matching the session ID.
// Returns an error if the prefix matches multiple sessions (ambiguous).
//
// Security note: the search is constrained to the project directory
// (derived from the workspace path hash) and requires a .jsonl suffix,
// so path traversal via session IDs is not possible.
func findSessionFile(projectDir, sessionID string) (path, fullID string, err error) {
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
			// Exact match — return immediately.
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

// parseClaudeContent converts Claude Code content blocks to normalized ContentBlocks.
// Content can be:
//   - A plain string (user input stored as a single string)
//   - A list of single-character strings (user input, joined into one text block)
//   - A list of typed objects (text, tool_use, tool_result, thinking, image, etc.)
//   - A mixed list
func parseClaudeContent(raw json.RawMessage) []manifest.ContentBlock {
	if raw == nil {
		return nil
	}

	// Try as plain string (user input stored as a single string).
	var plainStr string
	if json.Unmarshal(raw, &plainStr) == nil {
		if plainStr == "" {
			return nil
		}
		return []manifest.ContentBlock{{Type: "text", Text: plainStr}}
	}

	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}

	// Check if all items are strings (user char-by-char input).
	if text := tryJoinStringArray(items); text != "" {
		return []manifest.ContentBlock{{Type: "text", Text: text}}
	}

	// Parse as typed content blocks.
	var blocks []manifest.ContentBlock
	for _, item := range items {
		// Try as string first (can appear mixed with objects).
		var s string
		if json.Unmarshal(item, &s) == nil {
			blocks = append(blocks, manifest.ContentBlock{Type: "text", Text: s})
			continue
		}

		var block ccContentBlock
		if json.Unmarshal(item, &block) != nil {
			continue
		}

		switch block.Type {
		case "text":
			blocks = append(blocks, manifest.ContentBlock{
				Type: "text",
				Text: block.Text,
			})

		case "thinking":
			blocks = append(blocks, manifest.ContentBlock{
				Type:     "thinking",
				Thinking: block.Thinking,
			})

		case "tool_use":
			blocks = append(blocks, manifest.ContentBlock{
				Type:      "tool_use",
				ToolUseID: block.ID,
				Name:      block.Name,
				Input:     block.Input,
			})

		case "tool_result":
			output := extractToolResultContent(block.Content)
			blocks = append(blocks, manifest.ContentBlock{
				Type:         "tool_result",
				ForToolUseID: block.ToolUseID,
				Output:       output,
				IsError:      block.IsError,
			})

		case "image":
			cb := manifest.ContentBlock{Type: "image"}
			if block.Source != nil {
				cb.Source = block.Source.Type
				cb.MediaType = block.Source.MediaType
				cb.Data = block.Source.Data
			}
			blocks = append(blocks, cb)

		default:
			// Unknown type — preserve as text if possible.
			if block.Text != "" {
				blocks = append(blocks, manifest.ContentBlock{
					Type: "text",
					Text: block.Text,
				})
			}
		}
	}

	return blocks
}

// resolveContentToText extracts a plain text string from Claude Code's
// content field, which can be a plain string, an array of single-character
// strings, or an array of typed content blocks.
// Shared by ParseSessions (title extraction) and parseClaudeContent.
func resolveContentToText(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}

	// Try as plain string first.
	var plainStr string
	if json.Unmarshal(raw, &plainStr) == nil {
		return plainStr
	}

	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}

	// Try as all strings (char-by-char input).
	var chars []string
	allStrings := true
	for _, item := range items {
		var s string
		if json.Unmarshal(item, &s) != nil {
			allStrings = false
			break
		}
		chars = append(chars, s)
	}
	if allStrings {
		return strings.Join(chars, "")
	}

	// Try text blocks.
	for _, item := range items {
		var block ccContentBlock
		if json.Unmarshal(item, &block) == nil && block.Type == "text" && block.Text != "" {
			return block.Text
		}
	}

	return ""
}

// isLikelySystemPrompt returns true if the text looks like an injected system
// prompt rather than a real user message. Heuristics:
//   - Starts with whitespace (template preambles)
//   - Starts with XML-like tags (e.g., "<command-name>")
//   - Very short generic text that isn't useful as a title
func isLikelySystemPrompt(text string) bool {
	if len(text) == 0 {
		return false
	}
	first := text[0]
	return first == ' ' || first == '\t' || first == '\n' || first == '<'
}

// tryJoinStringArray checks if all items in a JSON array are strings and
// joins them. Returns empty string if any item is not a string or the array is empty.
func tryJoinStringArray(items []json.RawMessage) string {
	if len(items) == 0 {
		return ""
	}
	var chars []string
	for _, item := range items {
		var s string
		if json.Unmarshal(item, &s) != nil {
			return ""
		}
		chars = append(chars, s)
	}
	return strings.Join(chars, "")
}



// extractToolResultContent converts tool_result content to a string.
// Content can be a string, or a list of content blocks.
func extractToolResultContent(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}

	// Try as string.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}

	// Try as array of content blocks.
	var blocks []ccContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return string(raw)
}

// -- Claude Code JSONL schema types (internal) --

// ccRecord is the top-level structure of each JSONL line.
type ccRecord struct {
	Type      string    `json:"type"`      // "user", "assistant", "queue-operation", "last-prompt"
	UUID      string    `json:"uuid"`      // unique record ID
	Timestamp string    `json:"timestamp"` // ISO 8601
	SessionID string    `json:"sessionId"`
	Message   ccMessage `json:"message"`
}

// ccMessage is the message payload within a record.
type ccMessage struct {
	Role       string          `json:"role"` // "user", "assistant"
	Content    json.RawMessage `json:"content"`
	Model      string          `json:"model"`
	StopReason string          `json:"stop_reason"`
	Usage      *ccUsage        `json:"usage"`
}

// ccUsage holds token usage from the API response.
type ccUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// ccContentBlock represents a content block in Claude Code's format.
type ccContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`         // tool_use ID
	Name      string          `json:"name,omitempty"`       // tool_use name
	Input     json.RawMessage `json:"input,omitempty"`      // tool_use input
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result reference
	Content   json.RawMessage `json:"content,omitempty"`    // tool_result content
	IsError   bool            `json:"is_error,omitempty"`   // tool_result error flag
	Source    *ccImageSource  `json:"source,omitempty"`     // image source
}

// ccImageSource holds image data in Claude Code's format.
type ccImageSource struct {
	Type      string `json:"type"`       // "base64", "url"
	MediaType string `json:"media_type"` // "image/jpeg", "image/png"
	Data      string `json:"data"`       // base64 or URL
}
