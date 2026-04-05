# BENTO-008: Restorable Open

**Version:** 0.4.0
**Status:** Proposal
**Authors:** George Fahmy
**Repository:** github.com/kajogo777/bento
**Related:** `SPEC.md` §3 (Artifact Structure), `BENTO-009-head-tracking.md` (prerequisite)

## Abstract

`bento open` is destructive and irreversible. This proposal adds a save-
before-open: run `ExecuteSave` with a `pre-open` tag before overwriting any
files. Undo with `bento open undo`.

The backup is just a regular checkpoint. Same `ExecuteSave`, same store, same
OCI deduplication. No new packing code, no new storage format.

A prerequisite change ensures fresh-directory opens preserve the source
workspace ID rather than generating a new one, so the new directory shares
the checkpoint history and store — enabling parallel agent workspaces from
the same checkpoint (like git worktrees).

## Motivation

`bento open` overwrites workspace files, overwrites external files
(`~/.claude/projects/...`, `~/.codex/sessions/...`), and deletes workspace
files not in the checkpoint. There is no backup and no way to reverse it.

The primary danger is the **agent layer** — external files outside the
workspace directory. Opening a checkpoint into a fresh directory can
overwrite `~/.claude/projects/<hash>/session.jsonl` (weeks of conversation
history) or `~/.codex/state_5.sqlite` (session metadata for every project on
the machine). The user has no way to recover.

## Design

1. **Save before open** — call `ExecuteSave` with `Tag: "pre-open"` before
   any files are overwritten
2. **Reuse everything** — same save path, same store, same blob dedup; no
   new code for packing or storing
3. **Fail-safe** — if the save fails, open aborts; workspace untouched
4. **One undo** — single `pre-open` tag, overwritten each open;
   `bento open undo` is the shorthand
5. **Preserve workspace identity** — fresh-directory opens inherit the
   source checkpoint's workspace ID, keeping the full checkpoint history
   accessible

## Workspace Identity on Fresh-Directory Open

Today `configFromArtifact` (open.go L420) generates a new workspace ID for
every fresh-directory open. This severs the checkpoint lineage — the new
workspace cannot access cp-1 through cp-N from `bento list`, and
`bento open cp-3` fails because that tag lives in a different store.

This proposal changes `configFromArtifact` to **preserve the source
workspace ID** from the checkpoint's OCI config:

```go
// internal/cli/open.go — configFromArtifact (L420)
// Before:
newID, err := config.GenerateWorkspaceID()

// After:
newID := obj.WorkspaceID
if newID == "" {
    newID, _ = config.GenerateWorkspaceID()
}
```

This means multiple directories can share one store — the same model as git
worktrees. Each directory has its own `bento.yaml` pointing to the same
store path, and `bento save` from any directory creates checkpoints in the
shared history.

**Use case: parallel agent workspaces.** Open the same checkpoint into
multiple directories so different agents can work in parallel:

```
$ bento open cp-5 ~/workspace-a    # agent A works here
$ bento open cp-5 ~/workspace-b    # agent B works here

# Both save to the same store:
# workspace-a: bento save → cp-6
# workspace-b: bento save → cp-7
```

The checkpoint DAG naturally captures both branches:

```
cp-1 → cp-2 → ... → cp-5 → cp-6 (from workspace-a)
                         ↘→ cp-7 (from workspace-b)
```

## Current open.go Flow

```
L39-74    Parse ref, resolve store name + tag
L77-88    Resolve store path (from bento.yaml or DefaultStorePath)
L90-119   Load checkpoint (local or pull from remote)
L128-131  Parse manifest → CheckpointInfo
L133-141  Filter layers
L145-173  Build keepFiles map
L175-264  Pre-check scrubbed secrets
L267-278  Unpack layers                                              ← destructive
L280-285  CleanStaleFiles                                            ← destructive
L291-315  Regenerate bento.yaml if missing
L317-342  Hydrate scrubbed secrets
L354-363  Run post_restore hook
```

## Proposed Flow

```
1.  Parse ref (resolve "undo" → "pre-open")
2.  Locate store, load checkpoint
3.  If no bento.yaml: generate from checkpoint metadata      ← MOVED from L291
4.  ExecuteSave(Tag: "pre-open", Quiet: true)                ← NEW
5.  Parse manifest, filter layers
6.  Build keepFiles map
7.  Pre-check secrets
8.  Unpack layers                                            (unchanged)
9.  Clean stale files                                        (unchanged)
10. Hydrate secrets
11. Run post_restore hook
12. Print undo hint                                          ← NEW
```

Step 3 is the key enabler: by generating `bento.yaml` (with the preserved
workspace ID) before the save, `ExecuteSave` knows which extensions are
active and which external paths to scan. The agent layer captures existing
external files. Deps and project layers are empty or tiny for a fresh dir —
OCI dedup makes this near-zero overhead.

If step 4 fails, the open aborts. No files have been touched.

If step 4 returns `Skipped: true` (nothing changed since last save), no
`pre-open` tag is created. The undo hint points to the existing latest
checkpoint instead.

## Implementation

### Preserve Workspace ID (configFromArtifact)

```go
// internal/cli/open.go — configFromArtifact (L420)

func configFromArtifact(obj *manifest.BentoConfigObj) *config.BentoConfig {
    // Preserve the source workspace ID so the new directory shares
    // the same store and checkpoint history.
    newID := obj.WorkspaceID
    if newID == "" {
        var err error
        newID, err = config.GenerateWorkspaceID()
        if err != nil {
            newID = "ws-restored"
        }
    }

    cfg := &config.BentoConfig{
        ID:     newID,
        Store:  config.DefaultStorePath(),
        Task:   obj.Task,
        Remote: obj.Remote,
    }
    // ... rest unchanged
}
```

### The `undo` Alias

```go
// internal/cli/open.go — L39, before ref enters ParseRef
ref := args[0]
if ref == "undo" {
    ref = "pre-open"
}
```

`ParseRef("pre-open")` returns `storeName="", tag="pre-open"` — valid.
The open flow creates a new `pre-open` backup before restoring, so
`bento open undo` twice is a two-state toggle.

### Early bento.yaml Generation

Move the existing `configFromArtifact` logic (L291-315) to run before the
save. Same code, just earlier:

```go
// internal/cli/open.go — after loading checkpoint, before ExecuteSave

bentoCfg, parseErr := manifest.UnmarshalConfig(configBytes)
if _, statErr := os.Stat(filepath.Join(targetDir, "bento.yaml")); os.IsNotExist(statErr) {
    if parseErr == nil {
        _ = os.MkdirAll(targetDir, 0755)
        newCfg := configFromArtifact(bentoCfg)
        if err := config.Save(targetDir, newCfg); err != nil {
            fmt.Printf("Warning: generating bento.yaml: %v\n", err)
        }
    }
}
```

### Save Before Open

```go
// internal/cli/open.go — after bento.yaml is ensured, before unpack

var backedUp bool
if !flagNoBackup {
    result, err := ExecuteSave(SaveOptions{
        Dir:                  targetDir,
        Tag:                  "pre-open",
        Message:              fmt.Sprintf("pre-open backup before restoring %s", tag),
        SkipSecretScan:       true,
        AllowMissingExternal: true,
        Quiet:                true,
    })
    if err != nil {
        return fmt.Errorf("pre-open backup failed (no files modified): %w", err)
    }
    if result != nil && !result.Skipped {
        backedUp = true
        fmt.Printf("  Saved pre-open backup (%s)\n", result.Tag)
    }
}
```

- `SkipSecretScan: true` — no need to scrub secrets in a backup; speed.
- `AllowMissingExternal: true` — don't fail if external files are gone.
- `Quiet: true` — suppress per-layer output.

### Undo Hint

```go
// internal/cli/open.go — after restore completes

fmt.Printf("Restored to %s\n", targetDir)
if backedUp {
    fmt.Printf("\n  To undo: bento open undo\n")
}
```

### New Flag

```go
var flagNoBackup bool
cmd.Flags().BoolVar(&flagNoBackup, "no-backup", false,
    "skip pre-open backup (faster, but no undo)")
```

## UX

### Open (existing workspace)

```
$ bento open cp-5
  Saved pre-open backup (pre-open)
Restoring checkpoint cp-5 (sequence 5)...
  deps       142 files (unchanged)
  agent       23 files (+8 external)
  project     67 files
Restored to /Users/alice/projects/bento

  To undo: bento open undo
```

### Open (fresh directory)

```
$ bento open ghcr.io/org/project:cp-5 ~/workspace-a
  Saved pre-open backup (pre-open)
Restoring checkpoint cp-5 (sequence 5)...
  agent       23 files (+8 external)
  project     67 files
Restored to /Users/alice/workspace-a

  To undo: bento open undo
```

### Parallel workspaces

```
$ bento open cp-5 ~/workspace-a
$ bento open cp-5 ~/workspace-b

# Both share the same store and checkpoint history.
# Agent A and Agent B work independently.
# bento save from either dir creates the next cp-N.
```

### Undo

```
$ bento open undo
  Saved pre-open backup (pre-open)
Restoring checkpoint pre-open...
  ...
Restored to /Users/alice/projects/bento

  To undo: bento open undo
```

Two-state toggle. Each undo saves current state as new `pre-open`.

### No backup exists

```
$ bento open undo
Error: checkpoint "pre-open" not found
Hint: no previous open to undo
```

### bento list

```
$ bento list
  cp-8      2026-04-04 16:00  "pre-open backup before restoring cp-5"
  cp-7      2026-04-04 15:47  "fix lint errors"
  cp-5      2026-04-04 13:15  "refactor extension interface"
  pre-open  2026-04-04 16:00  "pre-open backup before restoring cp-5"
```

The pre-open save creates both a `cp-N` tag (regular sequence) and a
`pre-open` tag (undo alias). `cp-N` keeps the DAG linear; `pre-open`
provides the undo shorthand.

## Edge Cases

| Case | Behavior |
|------|----------|
| Undo when no backup exists | Error: `checkpoint "pre-open" not found` |
| Undo the undo | Two-state toggle — each undo saves current state as new `pre-open` |
| Workspace already checkpointed (nothing changed) | `ExecuteSave` returns `Skipped: true`; no backup; no undo hint |
| `--layers` / `--skip-layers` | Pre-open captures full state; partial open only affects restore |
| Agent writing during backup | Same risk as `bento save`; JSONL tolerates partial lines |
| Disk space | OCI dedup reuses existing blobs; one `pre-open` tag at a time |
| `--no-backup` flag | Skip `ExecuteSave` entirely; no undo |
| Parallel opens to same store | Last `pre-open` wins; each dir's undo restores its own last state only if no other open happened since |

## Files to Modify

| File | Change |
|------|--------|
| `internal/cli/open.go` | `undo` alias (L39); preserve workspace ID in `configFromArtifact` (L420); move bento.yaml generation earlier (from L291); call `ExecuteSave` before unpack; `--no-backup` flag; undo hint. |

One file. ~30 lines of new code plus the moved bento.yaml generation.

## Testing

### E2E Tests

1. **Fresh dir + external files:** create Claude Code session files on disk →
   `bento open cp-1 ~/fresh` → verify `pre-open` tag exists → verify
   external files overwritten → `bento open undo` → external files restored.

2. **Existing workspace:** save → modify files → `bento open cp-1` → verify
   `pre-open` exists → `bento open undo` → workspace matches pre-open state.

3. **Toggle:** `bento open cp-1` → `bento open undo` → `bento open undo` →
   workspace matches cp-1 state.

4. **Already checkpointed:** `bento save` → immediately `bento open cp-1` →
   `ExecuteSave` returns `Skipped` → no `pre-open` tag → no undo hint.

5. **--no-backup:** `bento open --no-backup cp-1` → no `pre-open` tag.

6. **No undo available:** `bento open undo` with no `pre-open` tag → error.

7. **Workspace ID preserved:** `bento open cp-1 ~/fresh` → check
   `~/fresh/bento.yaml` has same `id` as source checkpoint's `workspaceId` →
   `bento list` shows full checkpoint history.

8. **Parallel workspaces:** open cp-5 into two dirs → save from each →
   `bento list` shows both new checkpoints in shared history.
