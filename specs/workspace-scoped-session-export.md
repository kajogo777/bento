# Workspace-Scoped Session Export

**Status:** Proposal
**Related:** `extensions.md`, `SPEC.md` §3.3 (Layer Types)

## Problem

Several AI coding agents store session history in a **single global database** shared across all projects:

| Agent | Storage | Workspace isolation? |
|-------|---------|---------------------|
| **Codex** | `~/.codex/state_N.sqlite` — SQLite with WAL, 22 migrations, versioned schema (`state_5.sqlite` as of v2026.3) | **Partial.** `threads.cwd` column stores the working directory per session. Foreign-keyed tables (`stage1_outputs`, `thread_spawn_edges`, `thread_dynamic_tools`, `agent_jobs`) can be joined via `thread_id`. Separate `logs_1.sqlite` DB also has `thread_id`. |
| **OpenCode** | `~/.local/share/opencode/opencode.db` — SQLite with WAL, Goose migrations, 3 tables (`sessions`, `messages`, `files`) | **No.** `sessions` table has no `cwd`, `project_id`, or any workspace identifier. Sessions are bare UUIDs with titles. |
| **Claude Code** | `~/.claude/projects/<sha256>/` — file-based, one directory per project keyed by SHA-256 of absolute workspace path | **Yes.** Already workspace-scoped by design. No action needed. |
| **OpenClaw** | Gateway-managed sessions + `~/.openclaw/agents/<id>/sessions/` — file-based, scoped by agent ID mapped to workspace in `openclaw.json` | **Yes.** Already workspace-scoped by design. No action needed. |

When Bento checkpoints a workspace today, it must choose between:

1. **Include the entire global DB** — captures unrelated sessions from other projects, wastes space, potential data leakage between workspaces.
2. **Skip the DB** — loses session metadata, agent memories, conversation history.

Neither is acceptable for a tool whose purpose is portable, scoped workspace snapshots.

## Goals

1. Checkpoint only the session data relevant to the current workspace.
2. Restore cleanly — the exported data must be usable on a fresh machine.
3. No upstream agent modifications required — work with agents as they ship today.
4. Agent-specific logic is fine — each agent stores data differently, and the export strategy should match.
5. Never include credentials or secrets in exported data.

## Non-Goals

- Real-time sync or incremental replication of the global DB.
- Modifying the upstream agent's database schema.
- Supporting agents that haven't been analyzed yet (new agents get their own export strategy when added).

## Current State of Each Agent's Schema

### Codex (filterable)

The `threads` table has a `cwd` TEXT column containing the absolute working directory at session start. This is the primary filter key.

```
threads.cwd = "/Users/alice/projects/myapp"
  → stage1_outputs.thread_id  (memories, CASCADE DELETE)
  → thread_spawn_edges        (subagent parent-child)
  → thread_dynamic_tools      (per-thread tool specs)
  → agent_jobs                (batch jobs — linked via agent_job_items.assigned_thread_id)

logs (separate DB: logs_1.sqlite)
  → logs.thread_id            (structured logs per session)
```

**Filter query:**
```sql
SELECT id FROM threads
WHERE cwd = ?
   OR cwd LIKE ? || '/%'
```

This gives us the set of thread IDs. All related tables can be filtered by joining on `thread_id`.

### OpenCode (not filterable)

```
sessions (id TEXT PK, parent_session_id, title, message_count, tokens, cost, ...)
messages (id TEXT PK, session_id FK → sessions, role, parts JSON, model, ...)
files    (id TEXT PK, session_id FK → sessions, path, content, version, ...)
```

No `cwd`, no `project_id`, no git info. The `title` field is auto-generated or user-provided — not reliable for filtering.

The `files` table stores full file content with paths. These paths *could* be used as a heuristic (if a session's files reference paths within the workspace, it's likely related), but this is fragile and expensive to scan.

## Proposed Approaches

### Approach A: Export-at-Save with Agent-Specific Extractors

Add an optional `Export(workDir string) ([]ExportedFile, error)` method to the Extension interface. Extensions that manage global databases implement this to produce workspace-scoped sidecar files at save time.

```go
// ExportedFile represents a file produced by an extension's export step.
// It is packed into the agent layer alongside pattern-matched files.
type ExportedFile struct {
    // ArchivePath is the path within the layer archive (e.g., ".bento/exports/codex-sessions.db").
    ArchivePath string
    // SourcePath is the absolute path to the temporary file on disk.
    SourcePath  string
}

// Exporter is an optional interface extensions can implement.
// If implemented, Export() is called during save after Contribute().
type Exporter interface {
    Export(workDir string) ([]ExportedFile, error)
}
```

**Codex implementation sketch:**
1. Open `~/.codex/state_N.sqlite` read-only.
2. Query thread IDs matching the workspace `cwd`.
3. Create a temp SQLite DB with the same schema.
4. Copy matching rows (threads + related tables via foreign keys).
5. Optionally copy matching rows from `logs_1.sqlite`.
6. Return the temp DB as an `ExportedFile` at `.bento/exports/codex-state.db`.

**OpenCode implementation sketch:**
Since there's no workspace filter column, two sub-options:

- **A1: File-path heuristic.** Query the `files` table for rows where `path` starts with or is relative to the workspace. Collect the `session_id` values. Export those sessions + their messages + files. This works if the agent wrote/read files in the workspace during the session, which is the common case for a coding agent.

- **A2: Timestamp correlation.** Use the workspace's git log to find commit timestamps, then match sessions whose `created_at` falls within the project's active time range. Fragile — doesn't work for new repos or repos with gaps.

- **A3: Full DB copy.** Accept that OpenCode can't be filtered and include the whole DB. It's typically small (< 10MB for moderate use).

### Approach B: Shadow Database per Workspace

Bento maintains a per-workspace mirror DB at `.bento/agent-state/<agent>.db`. On each save, Bento diffs the global DB against the shadow and pulls in new matching rows.

**Pros:** Incremental, fast after first sync, clean restore story.
**Cons:** Must track upstream schema migrations. Shadow can drift if the user runs Bento infrequently. Complex state management. Essentially building a replication system.

### Approach C: Filesystem-Level Copy-on-Write

Use filesystem snapshots (APFS clones on macOS, reflinks on Linux) to create a zero-cost copy of the global DB, then prune non-matching rows in the copy.

**Pros:** Fast, no extra disk space for the initial copy.
**Cons:** Platform-specific. Pruning still requires knowing which rows to keep (same filtering problem). Doesn't help with the OpenCode no-filter issue.

### Approach D: Agent-Side Hooks

Ask the agent to export its own workspace-scoped state via a pre-save hook. For example, if Codex ever ships a `codex export --cwd /path/to/workspace` command, Bento could call it.

**Pros:** Agent knows its own schema best. No SQLite dependency in Bento.
**Cons:** Depends on upstream agent support that doesn't exist today. Each agent would need a different command. Fragile across agent versions.

## Comparison

| | A: Export-at-Save | B: Shadow DB | C: CoW + Prune | D: Agent Hooks |
|---|---|---|---|---|
| **Complexity** | Medium | High | Medium | Low (but external) |
| **Codex** | ✓ Clean filter via `cwd` | ✓ | ✓ | ✗ No export command |
| **OpenCode** | △ Heuristic via `files.path` | △ Same heuristic | △ Same heuristic | ✗ No export command |
| **Claude Code** | N/A (already scoped) | N/A | N/A | N/A |
| **OpenClaw** | N/A (already scoped) | N/A | N/A | N/A |
| **Restore** | Drop sidecar into `.bento/exports/`, hook merges back | Replace shadow DB | Replace pruned copy | Agent-specific |
| **Schema drift** | Must handle new columns/tables | Must replicate migrations | Same as A | Agent handles it |
| **Dependencies** | SQLite driver in Go | SQLite driver in Go | Platform APIs + SQLite | None |
| **Disk overhead** | Temp file during save | Persistent shadow copy | CoW clone (near-zero) | None |

## Open Questions

1. **SQLite dependency.** Bento is currently pure Go with no CGo. Adding SQLite querying requires either `modernc.org/sqlite` (pure Go, ~15MB binary increase), `ncruces/go-sqlite3` (WASM-based, no CGo), or shelling out to the `sqlite3` CLI. Which trade-off is acceptable?

2. **OpenCode filtering.** The file-path heuristic (Approach A1) is the best available option but isn't guaranteed to work for sessions that only chatted without touching files. Is "best effort" acceptable, or should OpenCode fall back to full DB copy?

3. **Restore merge strategy.** When restoring a checkpoint on a machine that already has a global DB with existing sessions, how should the exported sidecar be merged?
   - **Insert-ignore:** Skip rows that already exist (by primary key). Safe but may miss updates.
   - **Upsert:** Replace existing rows with checkpoint data. Could overwrite newer local state.
   - **Separate DB:** Don't merge — let the agent read from the sidecar via config override (e.g., `CODEX_SQLITE_HOME`). Cleanest but requires agent support.
   - **Manual:** Provide a `bento restore-agent-state` command that the user runs explicitly.

4. **Schema versioning.** Codex's DB schema is at v5 with 22 migrations. If Bento exports a v5 DB and the user later upgrades Codex to v6, the exported sidecar won't have the new columns. Should Bento store the schema version in the export metadata? Should restore skip merging if versions don't match?

5. **WAL files.** SQLite WAL mode means the DB state is split across `.sqlite`, `.sqlite-wal`, and `.sqlite-shm` files. Bento must either checkpoint the WAL before copying (ensuring all data is in the main file) or copy all three files atomically. The `PRAGMA wal_checkpoint(TRUNCATE)` approach is safest but requires write access to the DB, which conflicts with read-only access.

6. **Concurrent access.** The agent may be running and writing to the DB while Bento is saving. SQLite WAL mode allows concurrent readers, but Bento must handle the case where the DB is modified mid-export. A snapshot isolation approach (open a read transaction, hold it for the duration of the export) would work but requires a proper SQLite connection, not just file copying.
