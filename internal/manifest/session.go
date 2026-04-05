package manifest

import "encoding/json"

// SessionMeta holds lightweight metadata about an agent session.
// Extracted at save-time and stored in BentoConfigObj for fast listing
// without downloading layer blobs.
//
// Security note: the Title field contains the first user message (truncated
// to ~80 chars) and is stored as plaintext in the OCI config object. The
// secret scanner only scrubs layer file contents, not config metadata. If
// a user's first message contains sensitive data, it will be embedded in
// the checkpoint metadata. This is a known limitation.
type SessionMeta struct {
	Agent        string `json:"agent"`                  // extension name ("claude-code", "codex", etc.)
	SessionID    string `json:"sessionId"`              // UUID or agent-specific identifier
	Title        string `json:"title,omitempty"`        // first user message, truncated to ~80 chars
	MessageCount int    `json:"messageCount,omitempty"` // number of user+assistant turns
	Created      string `json:"created,omitempty"`      // RFC3339 timestamp of first message
	Updated      string `json:"updated,omitempty"`      // RFC3339 timestamp of last message
	Model        string `json:"model,omitempty"`        // primary model used (e.g., "claude-opus-4-6")
}

// NormalizedSession is the full content of an agent session in a
// format-agnostic schema. All agents (Claude Code, Codex, Aider, etc.)
// map their native formats into this common representation.
//
// Used by `bento sessions inspect <id>` to display full session data.
type NormalizedSession struct {
	Agent     string              `json:"agent"`
	SessionID string              `json:"sessionId"`
	Messages  []NormalizedMessage `json:"messages"`
}

// NormalizedMessage represents a single turn in a conversation.
// Content is an array of ContentBlock discriminated on Type.
type NormalizedMessage struct {
	ID         string         `json:"id,omitempty"`
	Timestamp  string         `json:"timestamp,omitempty"` // RFC3339
	Role       string         `json:"role"`                // "user", "assistant", "tool", "system"
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model,omitempty"`
	Usage      *TokenUsage    `json:"usage,omitempty"`
	StopReason string         `json:"stopReason,omitempty"` // "end_turn", "tool_use", "max_tokens"
}

// ContentBlock is a discriminated union on the Type field.
// Only the fields relevant to the given Type are populated.
//
// Supported types:
//   - "text"        — plain text (Text field)
//   - "thinking"    — model reasoning/chain-of-thought (Thinking field)
//   - "tool_use"    — function/tool call (ToolUseID, Name, Input)
//   - "tool_result" — tool execution result (ForToolUseID, Output, IsError)
//   - "image"       — inline image (MediaType, Source, Data)
type ContentBlock struct {
	Type string `json:"type"` // discriminator

	// type: "text"
	Text string `json:"text,omitempty"`

	// type: "thinking"
	Thinking string `json:"thinking,omitempty"`

	// type: "tool_use"
	ToolUseID string          `json:"toolUseId,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`

	// type: "tool_result"
	ForToolUseID string `json:"forToolUseId,omitempty"`
	Output       string `json:"output,omitempty"`
	IsError      bool   `json:"isError,omitempty"`

	// type: "image"
	MediaType string `json:"mediaType,omitempty"` // "image/jpeg", "image/png"
	Source    string `json:"source,omitempty"`     // "base64" or "url"
	Data      string `json:"data,omitempty"`       // base64 data or URL
}

// TokenUsage records token consumption for a single message.
type TokenUsage struct {
	InputTokens  int `json:"inputTokens,omitempty"`
	OutputTokens int `json:"outputTokens,omitempty"`
	CacheRead    int `json:"cacheRead,omitempty"`
	CacheCreate  int `json:"cacheCreate,omitempty"`
}
