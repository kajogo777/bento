# Error Handling and Edge Cases

This document covers how bento should handle errors, edge cases, and unexpected states. Implementations should use this as a reference for consistent behavior.

## Save Errors

### No harness detected

If `bento init` or `bento save` cannot detect an agent framework and no harness is defined in `bento.yaml`:

- Action: fall back to a minimal default harness that puts everything in the project layer except common dependency directories
- Default ignore: `.git/`, `node_modules/`, `.venv/`, `__pycache__/`, `.DS_Store`
- Message: warn the user that no agent was detected and suggest defining a harness in `bento.yaml`

### Empty layers

If a layer has zero matching files:

- Action: include it in the manifest as an empty tar+gzip archive (just the gzip header, no entries)
- Rationale: keeps layer count consistent across checkpoints, avoids breaking tooling that expects a fixed number of layers

### Dirty git state

If the workspace has uncommitted changes:

- Action: save anyway. Bento captures filesystem state, not git state. Uncommitted changes are part of the workspace.
- Message: no warning needed. This is normal -- the whole point of bento is capturing state that isn't committed.

### Files changed during save

If files are modified while bento is scanning and packing layers (e.g., a running dev server writes to a log file):

- Action: best-effort consistency. Pack each layer atomically (scan + tar in one pass). Accept that files modified mid-scan may be captured in an inconsistent state.
- Message: no warning unless the inconsistency is detectable (e.g., a file was deleted between scan and tar).
- Future: consider filesystem snapshots (btrfs/APFS snapshots) for strict consistency.

### Secret scan finds potential secrets

- Action: abort the save. Print the file path, line number, and matched pattern.
- Message: explain what was found and how to fix it (add to `.bentoignore` or remove the secret).
- Override: `bento save --skip-secret-scan` to bypass (for CI or when the match is a false positive). Log a warning.

### Layer exceeds size threshold

If a single layer exceeds a configurable size threshold (default: 1 GB):

- Action: warn but don't block. Large deps layers are expected.
- Message: suggest splitting the layer or adding large directories to ignore if they're not needed.

### Workspace is not initialized

If `bento save` is called without a prior `bento init` (no `bento.yaml`):

- Action: error with a clear message.
- Message: "No bento.yaml found. Run `bento init` first."

## Restore Errors

### Target directory is not empty

If `bento open <ref> <dir>` targets a non-empty directory:

- Action: warn and ask for confirmation (interactive) or require `--force` (non-interactive).
- With `--force`: overwrite existing files. Do not delete files that aren't in the checkpoint (bento restores, it doesn't clean).
- Without `--force`: error with message.

### Layer not found in store

If a checkpoint references a layer digest that doesn't exist in the store (e.g., after garbage collection):

- Action: error with the missing digest and layer name.
- Message: suggest re-pushing from a remote or restoring a different checkpoint.

### Parent checkpoint not found

If inspecting a checkpoint's DAG and the parent digest doesn't exist:

- Action: warn but don't block. The checkpoint itself is still valid -- the lineage is just incomplete.
- Message: "Parent checkpoint sha256:abc123... not found (may have been garbage collected)."

### Secret hydration fails

If restore cannot resolve a secret reference (vault unreachable, env var not set, 1password not logged in):

- Action: warn for each failed secret but continue the restore. Write the `.env` file with the placeholder from the template instead of the resolved value.
- Message: list failed secrets and their sources. Suggest checking the secret backend.
- Rationale: a partial restore is more useful than a failed restore. The user can fix secrets and re-run hydration.

### Platform mismatch

If restoring a checkpoint created on a different OS:

- Action: apply cross-platform rules from spec Section 15 silently. No warning unless a specific issue occurs (symlink creation failure on Windows, case collision on macOS).
- Message: only warn on concrete problems, not on the mere fact of cross-platform restore.

### Selective restore with unknown layer name

If `bento open myproject:cp-3 --layers foo` references a layer name that doesn't exist in the checkpoint:

- Action: error listing the available layer names.
- Message: "Layer 'foo' not found. Available layers: project, agent, deps"

## Push Errors

### Registry unreachable

- Action: error with the connection details.
- Message: suggest checking network, registry URL, and credentials.
- Retry: implementations should retry with exponential backoff (3 attempts, 1s/2s/4s).

### Authentication failure

- Action: error with the registry URL.
- Message: suggest running `docker login <registry>` or checking credential helpers.

### Pre-push hook fails

- Action: abort the push.
- Message: show the hook command and its exit code/output.

### Blob already exists

If the registry reports that a blob digest already exists during push:

- Action: skip the upload for that blob. This is expected behavior (deduplication).
- No message needed. Optionally show "reused" in verbose output.

## Fork Errors

### Fork point doesn't exist

If `bento fork cp-99` references a checkpoint that doesn't exist:

- Action: error listing available checkpoints.
- Message: "Checkpoint 'cp-99' not found. Run `bento list` to see available checkpoints."

### Fork with uncommitted bento state

If the current workspace has changes since the last `bento save`:

- Action: auto-save the current state before forking. The fork creates a save of current state, then restores to the fork point.
- Message: "Saving current state as cp-N before forking from cp-M."

## Hook Errors

### Pre-hook failure

If a `pre_save` or `pre_push` hook exits non-zero:

- Action: abort the operation.
- Message: show the hook command, exit code, and stderr output.

### Post-hook failure

If a `post_restore`, `post_save`, or `post_fork` hook exits non-zero:

- Action: complete the operation but warn.
- Message: show the hook command, exit code, and stderr. Suggest fixing the hook or running the command manually.

### Hook times out

If a hook runs for more than 5 minutes (configurable):

- Action: kill the process and warn.
- Message: suggest optimizing the hook or increasing the timeout via `bento.yaml`.

```yaml
hooks:
  post_restore: "make setup"
  timeout: 600  # seconds, default 300
```

### Hook not found

If the hook command is not found in PATH:

- Action: error for pre-hooks, warn for post-hooks.
- Message: show the command that was not found.

## Watch Mode Errors

### File system event overflow

If `bento watch` receives more filesystem events than it can process:

- Action: trigger a full scan instead of incremental. Debounce saves.
- Message: log at debug level.

### Checkpoint fails during watch

If an automatic save triggered by `bento watch` fails:

- Action: log the error and continue watching. Retry on the next interval.
- Message: log the error. Don't crash the watcher.

## MCP Server Errors

### Debounced save

If `bento_save` is called within 10 seconds of the previous save:

- Action: skip the save. Return the previous checkpoint reference.
- Response: include a note that the save was debounced and the time until the next save is allowed.

### Restore during active agent session

If an agent calls `bento_restore` while it's actively modifying files:

- Action: perform the restore. The agent is expected to re-read its state after calling restore.
- Warning: this will overwrite files the agent may have open. The agent should save first if it wants to preserve current state.

### MCP server loses workspace

If the workspace directory is deleted or unmounted while the MCP server is running:

- Action: return errors for all tool calls until the workspace is available again.
- Message: "Workspace directory not found: /path/to/workspace"

## Garbage Collection Edge Cases

### Checkpoint referenced by tag and by parent

If `bento gc` would delete a checkpoint that is another checkpoint's parent:

- Action: do not delete it. Parent references must remain valid.
- Rule: only delete checkpoints that are not referenced as parents by any surviving checkpoint.

### Concurrent GC and save

If `bento gc` runs while `bento save` is in progress:

- Action: GC should skip any checkpoint whose tag was created within the last 60 seconds. This prevents racing with an in-progress save.

## General Principles

1. **Never lose data silently.** If bento can't complete an operation, fail loudly with a clear message. Never silently skip files or layers.

2. **Partial results are better than no results.** For restore operations, deliver as much as possible even if some parts fail (secret hydration, individual hooks). Let the user fix the remaining issues.

3. **Idempotency.** `bento save` called twice with no changes should produce the same digests. `bento open` called twice to the same directory should produce the same result. This is critical for CI and automation.

4. **Non-interactive by default.** All commands should work without TTY input when `--force` or equivalent flags are provided. Interactive prompts are for safety in manual use only.

5. **Verbose mode.** All commands should support `--verbose` / `-v` for detailed output including layer diffs, skipped files, and timing information. Default output should be concise.
