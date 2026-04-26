package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/extension"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/spf13/cobra"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions [ref]",
		Short: "List agent sessions in a checkpoint",
		Long: `List agent sessions detected in the current or specified checkpoint.

Sessions are extracted from agent state files (e.g., Claude Code JSONL,
Codex session logs) at save-time and stored as metadata in the checkpoint.

Examples:
  bento sessions                  # sessions in current checkpoint (head)
  bento sessions cp-3             # sessions in a specific checkpoint
  bento sessions --agent codex    # filter by agent
  bento sessions --json           # machine-readable output`,
		Args: cobra.MaximumNArgs(1),
		RunE: runSessionsList,
	}

	cmd.Flags().String("agent", "", "filter sessions by agent name")
	cmd.Flags().Bool("json", false, "output as JSON")

	cmd.AddCommand(newSessionsInspectCmd())

	return cmd
}

func runSessionsList(cmd *cobra.Command, args []string) error {
	dir, err := filepath.Abs(flagDir)
	if err != nil {
		return err
	}

	flagAgent, _ := cmd.Flags().GetString("agent")
	flagJSON, _ := cmd.Flags().GetBool("json")

	sessions, err := loadSessionsFromRef(dir, args)
	if err != nil {
		return err
	}

	// Filter by agent if requested.
	if flagAgent != "" {
		var filtered []manifest.SessionMeta
		for _, s := range sessions {
			if s.Agent == flagAgent {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}

	// Sort by updated time descending (most recent first).
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Updated > sessions[j].Updated
	})

	if len(sessions) == 0 {
		if !flagJSON {
			fmt.Println("No sessions found.")
		} else {
			fmt.Println("[]")
		}
		return nil
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(sessions)
	}

	// Table output.
	fmt.Printf("%-14s %-38s %5s  %-20s  %s\n", "AGENT", "SESSION", "MSGS", "UPDATED", "TITLE")
	for _, s := range sessions {
		updated := formatLocalTime(s.Updated)
		// Sanitize defensively in case the manifest was authored by an
		// older bento version that didn't normalize titles at save time.
		// Then truncate to the table's column budget on rune boundaries.
		title := manifest.TruncateRunes(manifest.SanitizeTitle(s.Title), 50)
		fmt.Printf("%-14s %-38s %5d  %-20s  %s\n", s.Agent, s.SessionID, s.MessageCount, updated, title)
	}

	return nil
}

func newSessionsInspectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inspect <session-id>",
		Short: "Inspect full session content",
		Long: `Show the full content of an agent session in normalized format.

Outputs all messages with text, tool calls, thinking, images, and usage data.

Use --raw to output the original agent-specific format instead.

Note: This command reads session files from the live filesystem using
the current workspace path. If you restored a checkpoint into a different
directory, session files may not be found (the path hash changes). Use
'bento sessions' (listing) instead, which always works from OCI metadata.

Examples:
  bento sessions inspect abc12345          # normalized JSON
  bento sessions inspect abc12345 --raw    # original JSONL
  bento sessions inspect abc12345 --text   # human-readable`,
		Args: cobra.ExactArgs(1),
		RunE: runSessionsInspect,
	}

	cmd.Flags().Bool("raw", false, "output original agent-specific format")
	cmd.Flags().Bool("text", false, "human-readable text output")

	return cmd
}

func runSessionsInspect(cmd *cobra.Command, args []string) error {
	dir, err := filepath.Abs(flagDir)
	if err != nil {
		return err
	}

	sessionID := args[0]
	flagRaw, _ := cmd.Flags().GetBool("raw")
	flagText, _ := cmd.Flags().GetBool("text")

	// For --raw, find the session file and dump it.
	if flagRaw {
		return dumpRawSession(dir, sessionID)
	}

	// Read session via SessionParser from the live filesystem.
	cfg, err := config.Load(dir)
	if err != nil {
		return fmt.Errorf("loading bento.yaml: %w", err)
	}

	exts := extension.Resolve(dir, cfg.Extensions)
	for _, ext := range exts {
		sp, ok := ext.(extension.SessionParser)
		if !ok {
			continue
		}
		session, readErr := sp.ReadSession(dir, sessionID)
		if readErr != nil {
			return fmt.Errorf("reading session from %s: %w", ext.Name(), readErr)
		}
		if session == nil {
			continue
		}

		if flagText {
			return printSessionText(session)
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(session)
	}

	return fmt.Errorf("session %s not found", sessionID)
}

// dumpRawSession finds and streams the raw session file for the given ID.
func dumpRawSession(dir, sessionID string) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return fmt.Errorf("loading bento.yaml: %w", err)
	}

	exts := extension.Resolve(dir, cfg.Extensions)
	for _, ext := range exts {
		sp, ok := ext.(extension.SessionParser)
		if !ok {
			continue
		}
		path := sp.RawSessionPath(dir, sessionID)
		if path == "" {
			continue
		}
		// Stream the file to stdout instead of loading it all into memory.
		f, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer f.Close()
		_, copyErr := io.Copy(os.Stdout, f)
		return copyErr
	}

	return fmt.Errorf("session %s not found", sessionID)
}

// printSessionText renders a session in human-readable text format.
func printSessionText(session *manifest.NormalizedSession) error {
	fmt.Printf("Agent: %s\n", session.Agent)
	fmt.Printf("Session: %s\n\n", session.SessionID)

	for _, msg := range session.Messages {
		role := strings.ToUpper(msg.Role[:1]) + msg.Role[1:]
		ts := formatLocalTime(msg.Timestamp)
		if msg.Model != "" {
			fmt.Printf("--- %s (%s) %s ---\n", role, msg.Model, ts)
		} else {
			fmt.Printf("--- %s %s ---\n", role, ts)
		}

		for _, block := range msg.Content {
			switch block.Type {
			case "text":
				fmt.Println(block.Text)
			case "thinking":
				fmt.Printf("[thinking] %s\n", block.Thinking)
			case "tool_use":
				fmt.Printf("[tool: %s] id=%s\n", block.Name, block.ToolUseID)
				if block.Input != nil {
					fmt.Printf("  input: %s\n", string(block.Input))
				}
			case "tool_result":
				label := "result"
				if block.IsError {
					label = "error"
				}
				fmt.Printf("[%s for %s] %s\n", label, block.ForToolUseID, block.Output)
			case "image":
				fmt.Printf("[image: %s]\n", block.MediaType)
			}
		}

		if msg.Usage != nil {
			fmt.Printf("[tokens: in=%d out=%d", msg.Usage.InputTokens, msg.Usage.OutputTokens)
			if msg.Usage.CacheRead > 0 {
				fmt.Printf(" cache_read=%d", msg.Usage.CacheRead)
			}
			fmt.Println("]")
		}
		fmt.Println()
	}

	return nil
}

// loadSessionsFromRef loads session metadata from a checkpoint ref or the current head.
func loadSessionsFromRef(dir string, args []string) ([]manifest.SessionMeta, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("loading bento.yaml: %w", err)
	}

	ref := "latest"
	if len(args) > 0 {
		ref = args[0]
	} else if cfg.Head != "" {
		ref = cfg.Head
	}

	storeName, tag, parseErr := registry.ParseRef(ref)
	if parseErr != nil {
		return nil, parseErr
	}
	if storeName == "" {
		storeName = cfg.ID
	}

	storePath := filepath.Join(cfg.Store, storeName)
	store, err := registry.NewStore(storePath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}

	_, configBytes, err := store.LoadManifest(tag)
	if err != nil {
		return nil, fmt.Errorf("loading checkpoint %s: %w", ref, err)
	}

	cfgObj, err := manifest.UnmarshalConfig(configBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfgObj.Sessions, nil
}

// formatLocalTime converts an RFC3339/ISO8601 timestamp to local timezone
// in "2006-01-02 15:04:05" format. Falls back to the original string if parsing fails.
func formatLocalTime(ts string) string {
	if ts == "" {
		return ""
	}
	// RFC3339Nano handles both "2006-01-02T15:04:05Z" and
	// "2006-01-02T15:04:05.999999999Z" with any sub-second precision.
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		// Fallback: return as-is, trimmed.
		if len(ts) > 19 {
			return ts[:19]
		}
		return ts
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

