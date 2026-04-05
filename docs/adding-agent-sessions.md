# Adding Agent Session Support

Bento can introspect agent sessions within checkpoints — listing sessions,
showing message counts, and displaying full conversation content in a
normalized format. This guide explains how to add session support for a
new coding agent.

## Architecture

Session introspection is built on Bento's extension system:

1. **Extensions** detect agents and contribute file patterns to layers
2. **SessionParser** is an optional interface extensions can implement
3. At save-time, Bento calls `ParseSessions` on each implementing extension
4. Session metadata is stored in the OCI config (no extra layer downloads needed)
5. `bento sessions inspect` calls `ReadSession` on-demand for full content

## Step-by-step

### 1. Ensure your extension exists

Your agent needs an extension in `internal/extension/` that implements
the `Extension` interface (`Name`, `Detect`, `Contribute`). If it doesn't
exist yet, create one following the pattern in existing extensions like
`claude_code.go` or `codex.go`.

### 2. Implement `SessionParser`

Add two methods to your extension struct:

```go
// internal/extension/myagent.go (or myagent_sessions.go)

func (m MyAgent) ParseSessions(workDir string) ([]manifest.SessionMeta, error) {
    // Find session files using your agent's known paths.
    // For each session, extract lightweight metadata.
    // This runs at save-time — keep it fast.
}

func (m MyAgent) ReadSession(workDir string, sessionID string) (*manifest.NormalizedSession, error) {
    // Find and parse the full session content.
    // Map native format → NormalizedMessage with ContentBlock array.
    // Return (nil, nil) if session not found.
}
```

No registration step needed. The save flow discovers implementations via
Go type assertion:

```go
if sp, ok := ext.(SessionParser); ok {
    sessions, _ := sp.ParseSessions(workDir)
}
```

### 3. Implement `ParseSessions` (fast, save-time)

Extract metadata without reading full file contents:

```go
func (m MyAgent) ParseSessions(workDir string) ([]manifest.SessionMeta, error) {
    // 1. Find session files
    sessionDir := findMyAgentSessionDir(workDir)

    // 2. For each session file, extract:
    return []manifest.SessionMeta{{
        Agent:        "myagent",              // must match ext.Name()
        SessionID:    "unique-id",            // UUID, filename, etc.
        Title:        "first user message",   // truncated to ~80 chars
        MessageCount: 42,                     // user + assistant turns
        Created:      "2026-01-01T00:00:00Z", // RFC3339
        Updated:      "2026-01-01T01:00:00Z", // RFC3339
        Model:        "model-name",           // primary model if known
    }}, nil
}
```

**Performance**: Use the JSONL helpers from `session.go`:
- `CountLines(path)` — fast line count
- `ReadFirstLine(path)` / `ReadLastLine(path)` — for timestamps
- `StreamLines(path, fn)` — streaming reader

### 4. Implement `ReadSession` (on-demand, full content)

Map your agent's native format to the normalized schema:

```go
func (m MyAgent) ReadSession(workDir string, sessionID string) (*manifest.NormalizedSession, error) {
    session := &manifest.NormalizedSession{
        Agent:     "myagent",
        SessionID: fullSessionID,
    }

    // For each message in the session:
    session.Messages = append(session.Messages, manifest.NormalizedMessage{
        ID:         "msg-id",
        Timestamp:  "2026-01-01T00:00:00Z",
        Role:       "user",  // or "assistant", "tool", "system"
        Content:    blocks,  // []manifest.ContentBlock
        Model:      "model-name",
        Usage:      &manifest.TokenUsage{InputTokens: 100, OutputTokens: 50},
        StopReason: "end_turn",
    })

    return session, nil
}
```

### 5. Content block mapping

Map your agent's content to `ContentBlock` (discriminated union on `Type`):

| Your agent's content | ContentBlock |
|---------------------|--------------|
| Plain text | `{Type: "text", Text: "..."}` |
| Model reasoning | `{Type: "thinking", Thinking: "..."}` |
| Function/tool call | `{Type: "tool_use", ToolUseID: "id", Name: "fn", Input: json}` |
| Tool execution output | `{Type: "tool_result", ForToolUseID: "id", Output: "...", IsError: false}` |
| Inline image | `{Type: "image", MediaType: "image/png", Source: "base64", Data: "..."}` |

### 6. Error handling

- `ParseSessions` errors are treated as warnings — they never block a save
- Return partial results when possible (skip unparseable files)
- `ReadSession` should return `(nil, nil)` if the session ID is not found

## Known limitation: workspace path coupling

Some agents store sessions at user-global paths derived from the workspace's
absolute filesystem path. For example, Claude Code uses
`~/.claude/projects/<path-hash>/` where the hash is the workspace path with
separators replaced by dashes.

This creates a coupling between the workspace location and session lookup:

- **`bento sessions` (listing) always works** — it reads from OCI config
  metadata embedded at save-time. The metadata travels with the checkpoint
  regardless of where it's restored.

- **`bento sessions inspect <id>` may fail after cross-directory restore** —
  it reads from the live filesystem using the current workspace path to
  derive the session directory. If the checkpoint was restored to a different
  directory via `bento open cp-N <new-dir>`, the path hash changes and
  `ReadSession` won't find the files.

  The session files ARE in the checkpoint's agent layer and get restored to
  their original external path (e.g., `~/.claude/projects/-Users-alice-original-path/`),
  but the parser looks them up using the new directory's hash.

**Workarounds:**
- Use `bento sessions` (listing) which always works from any directory
- Use `bento sessions inspect <id> --raw` from the original workspace path
- Agents that store sessions relative to the workspace (e.g., `.aider.chat.history.md`)
  don't have this issue

**Future improvements:**
- Extract session content from the agent layer blob instead of the live filesystem
- Store the original workspace path in `SessionMeta` for re-derivation
- Add a `--from-layer` flag to `bento sessions inspect` for checkpoint-based reading

When implementing a new agent parser, document whether your agent uses
workspace-relative or absolute-path-based session storage, as it affects
the behavior of `ReadSession` after cross-directory restores.

## Testing

```bash
# After implementing, test in any workspace where your agent has been used:
bento save
bento sessions              # should list your agent's sessions
bento sessions inspect ID   # should show full normalized content
bento sessions inspect ID --raw   # should dump original format
bento sessions inspect ID --text  # should show human-readable output
```

## Reference implementations

- **Claude Code**: `claude_code_sessions.go` — JSONL parser, handles char-by-char
  user input, plain string content, typed content blocks
- **Session helpers**: `session.go` — `CountLines`, `ReadFirstLine`, `ReadLastLine`,
  `StreamLines`

## Format-specific notes

### JSONL-based agents (Claude Code, Codex)

Use `StreamLines` for efficient parsing. Each line is a complete JSON object.

### SQLite-based agents (Cursor, OpenCode)

Use `database/sql` with a read-only connection (`?mode=ro`). Query the
relevant tables for conversation data.

### Markdown-based agents (Aider)

Parse section headers (e.g., `####`) as turn boundaries. Extract text
between headers as message content.
