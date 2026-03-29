# Session Summary — Bento Agent Extension Revisions

**Date:** 2026-03-29  
**Profile:** default  
**CWD:** `/Users/georgefahmy/Desktop/projects/bento`  
**Branch:** `main`  
**Repository:** `git@github.com:kajogo777/bento.git`

---

## Overview

This session focused on researching how four AI coding agents (Claude Code, Codex CLI, OpenCode, OpenClaw) store session history on disk, then revising Bento's extension system to accurately capture that state. The work included code changes, a code review cycle, bug fixes, E2E test creation, a design spec for future workspace-scoped SQLite export, and source-level verification of each agent's actual storage implementation.

---

## Key Accomplishments

- **Researched all four agents' storage implementations** via official docs, GitHub source code, npm package inspection, and empirical disk inspection
- **Revised four extension files** (`claude_code.go`, `codex.go`, `opencode.go`, `openclaw.go`) to accurately track agent state
- **Created 7 E2E tests** in `e2e/extensions_test.go` covering all four agents, credential exclusion, multi-agent detection, and layer digest reuse
- **Performed a full code review** that caught 5 issues (1 high-severity bug, 4 medium/low)
- **Fixed all review findings**: reverted incorrect SHA-256 hash, fixed lexicographic sort bug, removed duplicate memory dir, cleaned unused params, scoped credential ignore pattern
- **Created a design spec** at `specs/workspace-scoped-session-export.md` for future per-workspace SQLite filtering
- **Verified all implementations against actual source code** of latest versions via container-based inspection and local npm package analysis
- **Updated AGENTS.md** to reflect accurate extension patterns

---

## Key Decisions & Rationale

| Decision | Rationale |
|----------|-----------|
| **Option A (include whole global DB) for Codex/OpenCode** | Per-workspace SQLite export is complex; full DB is small and OCI dedup prevents redundant storage |
| **Never include credentials** | Security requirement — OpenClaw `credentials/` excluded via scoped ignore pattern |
| **Separator-replacement hash for Claude Code** | Empirically verified on disk (`~/.claude/projects/-Users-...`); SHA-256 was wrong |
| **Numeric version parsing for Codex DB** | Lexicographic sort fails for `state_10.sqlite` vs `state_9.sqlite`; `strconv.Atoi` is correct |
| **Scoped credential ignore pattern** | Broad `credentials/**` affected all extensions globally; changed to absolute path `~/.openclaw/credentials/**` only when dir exists |
| **OpenCode: keep `openCodeDBPath()` for now** | Two storage backends exist (older file-based at `~/.local/share/opencode/`, newer SQLite at `.opencode/opencode.db`); `.opencode/**` already captures the newer one |

---

## Commands & Tools

```bash
# Build & validate
cd /Users/georgefahmy/Desktop/projects/bento
go build ./...
go vet ./internal/extension/...

# Run unit tests
go test ./... -count=1

# Run extension E2E tests
go test -tags integration -run 'TestClaudeCodeExtension|TestCodexExtension|TestOpenCodeExtension|TestOpenClawExtension|TestOpenClawCredentialsExcluded|TestMultiAgentDetection|TestAgentLayerReuse' ./e2e/ -v -count=1

# Run ALL E2E tests
go test -tags integration ./e2e/ -v -count=1

# Verify Claude Code hash format empirically
ls ~/.claude/projects/ | head -5
# Shows: -Users-georgefahmy-Desktop-projects-bento (separator replacement, NOT SHA-256)

# Inspect Codex source
cat /tmp/codex-src/codex-rs/utils/home-dir/src/lib.rs  # CODEX_HOME env var
cat /tmp/codex-src/codex-rs/state/src/lib.rs            # STATE_DB_VERSION = 5

# Inspect Claude Code npm source
grep -o 'function QM(q){[^}]*}' ~/.nvm/.../cli.js
# Shows: q.replace(/[^a-zA-Z0-9]/g, "-") — replaces ALL non-alphanumeric with dashes
```

---

## Files Modified/Created

### Modified
| File | Changes |
|------|---------|
| `internal/extension/claude_code.go` | Reverted SHA-256 to separator-replacement hash; removed duplicate `memory/` subdir; single `claudeProjectDir()` function |
| `internal/extension/codex.go` | Added `codexLatestStateDB()` with numeric version parsing; added `codexMemoriesDir()`; removed unused `workDir` params; extracted `codexHome()` helper |
| `internal/extension/opencode.go` | Replaced fake subdirectory logic with `openCodeDBPath()` for SQLite DB; added Windows `LOCALAPPDATA` support |
| `internal/extension/openclaw.go` | Added `openclaw.json` config capture; added default workspace skills; scoped credential exclusion to absolute `~/.openclaw/credentials/` path; extracted `openClawHome()` helper |
| `AGENTS.md` | Updated extension patterns table to match actual implementations |

### Created
| File | Purpose |
|------|---------|
| `e2e/extensions_test.go` | 7 E2E tests for all agent extensions |
| `specs/workspace-scoped-session-export.md` | Design spec for future per-workspace SQLite export |

---

## Tests & Verification

- **All unit tests pass**: `go test ./... -count=1` — 0 failures
- **All 7 new E2E tests pass**: Claude Code, Codex, OpenCode, OpenClaw, credentials exclusion, multi-agent, layer reuse
- **All existing E2E tests pass** (except pre-existing flaky `TestWatchCustomLayerWatchOff`)
- **`go vet`** passes clean on extension package
- **Empirical verification**: Claude Code hash format confirmed on disk; Codex schema confirmed from Rust source; OpenCode dual-backend discovered

---

## Issues/Blockers

### Resolved
1. **Claude Code SHA-256 hash was wrong** — reverted to separator replacement (verified empirically + in npm source)
2. **Codex lexicographic sort bug** — replaced with `strconv.Atoi` numeric parsing
3. **Duplicate memory/ subdirectory** — removed (recursive walk already covers it)
4. **Broad `credentials/**` ignore pattern** — scoped to absolute OpenClaw path only
5. **Unused `workDir` parameters** — removed from `codexLatestStateDB()` and `codexMemoriesDir()`

### Outstanding
- **OpenCode has two storage backends**: Older file-based (`~/.local/share/opencode/storage/`) and newer SQLite (`.opencode/opencode.db`). Current code captures the newer one via `.opencode/**` and attempts the older via `openCodeDBPath()`. The `openCodeDBPath()` function targets `~/.local/share/opencode/opencode.db` which doesn't exist on the test machine — it should be removed or the older file-based paths should be added back.
- **Claude Code long path edge case**: Paths > 200 chars get a hash suffix in Claude Code (`${truncated}-${hash}`). Our Go code doesn't handle this. Low priority — unlikely in practice.

---

## Next Steps

1. **Fix OpenCode extension**: Either remove `openCodeDBPath()` (since `.opencode/**` covers the SQLite case) or add back the older file-based storage paths (`~/.local/share/opencode/storage/session/`, `storage/message/`, etc.) for backward compatibility with the pre-archive version
2. **Run E2E tests in containers**: The credential exclusion test uses `t.Setenv("OPENCLAW_STATE_DIR", ...)` which works but ideally should run in an isolated container to avoid any env pollution
3. **Implement workspace-scoped SQLite export** (spec at `specs/workspace-scoped-session-export.md`): Requires choosing a Go SQLite driver and implementing the `Exporter` interface for Codex (filterable via `threads.cwd`) and OpenCode (heuristic via `files.path`)
4. **Add unit tests for extension package**: Currently `internal/extension/` has no test files — the E2E tests cover behavior but unit tests would catch regressions faster
5. **Consider Claude Code path > 200 char handling**: Add hash suffix logic matching `QM()` function from Claude Code source
