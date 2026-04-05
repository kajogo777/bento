# BENTO-009: Head Tracking and Workspace Identity

**Version:** 0.2.0
**Status:** Implemented
**Authors:** George Fahmy
**Repository:** github.com/kajogo777/bento
**Related:** `SPEC.md` §3, `BENTO-008-restorable-open.md`

## Abstract

Add a `head` field to `bento.yaml` that tracks which checkpoint each
workspace directory is currently at. Preserve the source workspace ID when
opening into a fresh directory so multiple directories share one store —
enabling parallel agent workspaces (like git worktrees). Remove the
redundant `fork` command since `bento open <ref> <dir>` is the fork
operation.

## Motivation

Bento's checkpoint DAG relies on a global `latest` tag for parent tracking.
This breaks when multiple directories share one store:

- Workspace A saves cp-5, `latest` → cp-5
- Workspace B saves cp-6, `latest` → cp-6
- Workspace A saves cp-7 with `parent = cp-6` (wrong — should be cp-5)

The same issue affects skip-if-unchanged detection (compares against wrong
parent) and `bento open undo` (BENTO-008).

A secondary problem: `bento open <ref> <fresh-dir>` generates a new
workspace ID, disconnecting the new directory from the source checkpoint
history. The user cannot `bento list` the original checkpoints or open
earlier checkpoints from the new directory.

## Design

### 1. Add `head` to bento.yaml

```yaml
id: ws-abc123
store: ~/.bento/store
head: sha256:abc123def456...   # this directory's current checkpoint digest
```

Each directory tracks its own position in the DAG. No shared pointer.

### 2. Preserve workspace ID on open

When opening into a fresh directory, inherit the source checkpoint's
`workspaceId` from the OCI config instead of generating a new one. Multiple
directories sharing one store is the intended model — like git worktrees.

### 3. Remove fork command

`bento fork <ref>` currently just restores files without creating a
checkpoint or recording the fork point. It's `bento open` with a different
name. Remove it. `bento open <ref> <dir>` is the fork operation — the new
directory gets the same workspace ID, its own `head`, and branches the DAG
naturally on next save.

## Implementation

### config.go: Add Head Field

```go
// BentoConfig represents the bento.yaml configuration file.
type BentoConfig struct {
    ID     string `yaml:"id,omitempty"`
    Head   string `yaml:"head,omitempty"` // manifest digest of current checkpoint
    // ... rest unchanged
}
```

### save_core.go: Use Head for Parent

Replace the `latest` tag lookup with `head` from bento.yaml:

```go
// Before (L299-305):
parentDigest := ""
if len(existing) > 0 {
    if d, err := store.ResolveTag("latest"); err == nil {
        parentDigest = d
    }
}

// After:
parentDigest := cfg.Head
```

After saving, update `head` in bento.yaml:

```go
// After (L600-607), replace latest tag with head update:
cfg.Head = manifestDigest
if err := config.Save(opts.Dir, cfg); err != nil {
    return nil, fmt.Errorf("updating head in bento.yaml: %w", err)
}
```

The `latest` tag is still maintained for convenience (used by push defaults
and backward compatibility). Parent tracking uses `head`, not `latest`.

### open.go: Update Head After Restore

After unpacking files, set `head` to the opened checkpoint's digest via
`config.UpdateHead` (avoids triggering BackfillDefaults):

```go
manifestDigest := digest.FromBytes(manifestBytes).String()
_ = config.UpdateHead(targetDir, manifestDigest)
```

### inspect.go / diff.go: Default to Head

When no ref is given, `inspect` and `diff` now resolve the default ref from
`head` in bento.yaml (this directory's position) instead of the global
`latest` tag. Falls back to `latest` if head is not set.

### resolve.go: Digest References

`ParseRef` now recognizes `sha256:` prefix and passes digest refs through
directly (oras-go's `Resolve` handles both tags and digests natively).

### open.go: Preserve Workspace ID

```go
func configFromArtifact(obj *manifest.BentoConfigObj) *config.BentoConfig {
    newID := obj.WorkspaceID
    if newID == "" {
        var err error
        newID, err = config.GenerateWorkspaceID()
        if err != nil {
            newID = "ws-restored"
        }
    }
    // ... rest unchanged
}
```

### open.go: Set Head in Generated Config

When generating bento.yaml for a fresh directory, set `head` to the
checkpoint being opened:

```go
newCfg := configFromArtifact(bentoCfg)
newCfg.Head = info.Digest  // track which checkpoint we opened
if err := config.Save(targetDir, newCfg); err != nil { ... }
```

### root.go: Remove Fork Command

Remove `newForkCmd()` registration from `NewRootCmd`. Delete `fork.go`.

## Checkpoint DAG Behavior

### Single workspace (unchanged)

```
bento save → cp-1 (head: cp-1)
bento save → cp-2 (head: cp-2, parent: cp-1)
bento save → cp-3 (head: cp-3, parent: cp-2)
```

### Parallel workspaces (new behavior)

```
# Workspace A: save three checkpoints
~/ws-a$ bento save → cp-1 (head: sha256:aaa)
~/ws-a$ bento save → cp-2 (head: sha256:bbb, parent: sha256:aaa)
~/ws-a$ bento save → cp-3 (head: sha256:ccc, parent: sha256:bbb)

# Open cp-2 into workspace B
~/ws-a$ bento open cp-2 ~/ws-b
# ws-b/bento.yaml: id=ws-abc123 (same), head=sha256:bbb (cp-2's digest)

# Both save independently — correct parents:
~/ws-a$ bento save → cp-4 (parent: sha256:ccc ← ws-a's head)
~/ws-b$ bento save → cp-5 (parent: sha256:bbb ← ws-b's head)
```

DAG:
```
cp-1 → cp-2 → cp-3 → cp-4 (workspace A)
          ↘→ cp-5 (workspace B)
```

### Open (restore older checkpoint)

```
~/ws-a$ bento open cp-1
# head updated to cp-1's digest
~/ws-a$ bento save → cp-6 (parent: cp-1's digest)
```

DAG:
```
cp-1 → cp-2 → cp-3
  ↘→ cp-6
```

## Sequence Numbers

The `cp-N` sequence remains global per store. The file lock
(`StorePath()/.save-lock`) prevents concurrent saves, so two directories
won't race on the same number. Numbers interleave across workspaces but
never collide:

```
ws-a saves → cp-4
ws-b saves → cp-5
ws-a saves → cp-6
```

This is correct. Sequence numbers are a convenience label, not a semantic
ordering. The DAG parent references are the source of truth for lineage.

## Migration

Existing workspaces have no `head` field. On first save after upgrade:

- `cfg.Head` is empty string
- `parentDigest` = `""` (no parent, same as first-ever save)
- Save creates the checkpoint normally
- `head` is set to the new digest

This means the first save after migration has no parent link — a one-time
break in the DAG chain. This is acceptable: the alternative (falling back
to `latest` when `head` is empty) would perpetuate the broken behavior for
workspaces that happen to have a `latest` tag from another directory.

If the user wants to preserve continuity, they can manually set `head` in
bento.yaml to their last checkpoint's digest before upgrading.

## Files Modified

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `Head string` field to `BentoConfig`. Add `UpdateHead()` for lightweight head-only updates. |
| `internal/cli/save_core.go` | Use `cfg.Head` for parent. Update `cfg.Head` after save. Keep `latest` tag for convenience. Add `ForceSave` option. |
| `internal/cli/open.go` | Update head via `config.UpdateHead` after restore. Preserve workspace ID and store root in `configFromArtifact`. Fix pull ref construction for preserved workspace IDs. |
| `internal/cli/inspect.go` | Default to `head` digest instead of `latest` tag. |
| `internal/cli/diff.go` | Default to `head` digest instead of `latest` tag (both `diffWorkspace` and `diffFileWorkspace`). |
| `internal/registry/resolve.go` | Handle `sha256:` digest refs in `ParseRef`. |
| `internal/cli/root.go` | Remove `newForkCmd()` registration. |
| `internal/cli/fork.go` | **Deleted.** |

## Testing

### Unit Tests

- `TestHeadTracking_SingleWorkspace`: save → verify head set → save again →
  verify parent matches previous head.
- `TestHeadTracking_EmptyHead`: first save with no head → parent is empty →
  head set after save.

### E2E Tests

1. **Head updated on save:** save → read bento.yaml → verify `head` field
   matches checkpoint digest.

2. **Head updated on open:** save cp-1, save cp-2 → open cp-1 → verify
   `head` is cp-1's digest → save → verify parent is cp-1's digest.

3. **Parallel workspaces:** save cp-1 → open cp-1 into dir-b → save from
   both → verify each checkpoint has correct parent (cp-1 for both).

4. **Workspace ID preserved:** save → open into fresh dir → verify
   bento.yaml has same workspace ID → bento list shows all checkpoints.

5. **Fork removed:** `bento fork` → command not found / error.

6. **Sequence interleaving:** save from dir-a (cp-2) → save from dir-b
   (cp-3) → save from dir-a (cp-4) → no collisions.

7. **Migration:** workspace with no `head` field → save → head is set →
   parent is empty (clean break).
