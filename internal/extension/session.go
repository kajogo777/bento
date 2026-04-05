package extension

import (
	"bufio"
	"io"
	"os"

	"github.com/kajogo777/bento/internal/manifest"
)

// SessionParser is an optional interface that agent extensions can implement
// to enable session introspection in Bento.
//
// When an extension implements SessionParser, Bento will:
//   - Call ParseSessions at save-time to embed session metadata in the OCI config
//   - Call ReadSession on-demand for `bento sessions inspect <id>`
//
// Implementing SessionParser requires no registration step. The save flow
// discovers implementations via Go type assertion:
//
//	if sp, ok := ext.(SessionParser); ok { ... }
//
// # Adding session support for a new agent
//
// 1. Ensure your extension already implements the Extension interface.
//
// 2. Add ParseSessions and ReadSession methods to your extension struct.
//    See the ClaudeCode implementation in claude_code_sessions.go for a reference.
//
// 3. ParseSessions should be fast (read first/last lines, count lines).
//    ReadSession can be slower (reads full file content).
//
// 4. Use the JSONL helpers below (CountLines, ReadFirstLine, ReadLastLine,
//    StreamLines) for JSONL-based agents.
//
// 5. Errors from ParseSessions are treated as warnings — they never block
//    a save. Return partial results when possible.
//
// 6. ReadSession should return (nil, nil) if the sessionID is not found.
//
// # Known limitation: workspace path coupling
//
// Some agents (notably Claude Code) store sessions at user-global paths
// derived from the workspace's absolute path (e.g., ~/.claude/projects/<hash>/).
// This means:
//
//   - `bento sessions` (listing) always works — it reads from OCI config
//     metadata embedded at save-time, independent of the current path.
//
//   - `bento sessions inspect <id>` reads from the live filesystem. If the
//     workspace was restored to a different directory via `bento open cp-N <dir>`,
//     the path hash changes and ReadSession won't find sessions on disk.
//     The session data IS in the checkpoint's agent layer (and gets restored
//     to the original external path), but the parser looks up sessions using
//     the current directory's path hash.
//
// This is a fundamental constraint of agents that use absolute-path-based
// storage. A future improvement could resolve this by extracting session
// content from the agent layer blob rather than the live filesystem, or by
// storing the original workspace path in SessionMeta for re-derivation.
// See docs/adding-agent-sessions.md for the full guide.
type SessionParser interface {
	// ParseSessions extracts lightweight metadata for all sessions in the workspace.
	// Called at save-time. Should be fast (avoid reading full file contents).
	ParseSessions(workDir string) ([]manifest.SessionMeta, error)

	// ReadSession reads the full content of a single session by ID and returns
	// it in normalized format. Returns (nil, nil) if the session is not found.
	// Called on-demand by `bento sessions inspect <id>`.
	ReadSession(workDir string, sessionID string) (*manifest.NormalizedSession, error)

	// RawSessionPath returns the filesystem path to the raw session file for
	// the given session ID, or empty string if not found. Used by
	// `bento sessions inspect --raw` to stream the original file format
	// without agent-specific logic in the CLI layer.
	RawSessionPath(workDir string, sessionID string) string
}

// CountLines counts the number of lines in a file without loading it into memory.
func CountLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	// Increase buffer size for long lines (e.g., large tool outputs).
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}

// ReadFirstLine reads the first line of a file.
func ReadFirstLine(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	if scanner.Scan() {
		return scanner.Bytes(), nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

// ReadLastLine reads the last non-empty line of a file.
func ReadLastLine(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var last []byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(b) > 0 {
			last = make([]byte, len(b))
			copy(last, b)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if last == nil {
		return nil, io.EOF
	}
	return last, nil
}

// StreamLines calls fn for each line in the file. If fn returns an error,
// streaming stops and that error is returned.
func StreamLines(path string, fn func(line []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		if err := fn(scanner.Bytes()); err != nil {
			return err
		}
	}
	return scanner.Err()
}
