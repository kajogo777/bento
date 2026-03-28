# Shared Local Blob Store

**Version:** 0.1.0
**Status:** Draft
**Repository:** github.com/kajogo777/bento

## Abstract

This specification proposes restructuring the local OCI store so that layer blobs are shared across workspaces via content-addressing, while each workspace retains its own index (tags, manifests). This eliminates redundant disk usage when multiple workspaces contain identical layers — a common scenario after `bento open` creates a new workspace from an existing checkpoint.

## 1. Problem

Today each workspace gets an isolated OCI image layout at `~/.bento/store/<workspace-id>/`:

```
~/.bento/store/
├── ws-aaa/
│   ├── oci-layout
│   ├── index.json
│   └── blobs/sha256/
│       ├── <manifest>
│       ├── <config>
│       ├── <deps-layer>       ← 200 MB
│       ├── <agent-layer>
│       └── <project-layer>
└── ws-bbb/                    (opened from ws-aaa)
    ├── oci-layout
    ├── index.json
    └── blobs/sha256/
        ├── <manifest>
        ├── <config>
        ├── <deps-layer>       ← 200 MB (identical content, stored twice)
        ├── <agent-layer>
        └── <project-layer>
```

When `bento open` restores a checkpoint into a new workspace (ws-bbb), the new workspace gets a fresh empty store. The first `save` packs all layers from scratch and writes them to `ws-bbb/blobs/`. Even though the deps layer is byte-for-byte identical to the one in `ws-aaa`, it's stored again — doubling disk usage.

This compounds with every fork, every `open` on the same machine, and every workspace that shares dependencies.

### 1.1 Where Dedup Already Works

- **Remote registries**: `bento push` uses `oras.Copy()` which skips uploading blobs that already exist at the remote. Two workspaces pushing to the same registry share layers automatically.
- **Within a single workspace**: Unchanged layers between checkpoints share the same blob via content-addressed digests in the OCI layout.

### 1.2 Where Dedup Does Not Work

- **Across local workspaces**: Each workspace has its own `blobs/` directory. Identical content is duplicated on disk.

## 2. Proposed Architecture

Split the store into a shared content-addressed blob pool and per-workspace indexes:

```
~/.bento/store/
├── blobs/                     ← shared across all workspaces
│   └── sha256/
│       ├── <deps-layer>       ← 200 MB (stored once)
│       ├── <agent-layer-a>
│       ├── <agent-layer-b>
│       ├── <project-layer-a>
│       ├── <project-layer-b>
│       ├── <manifest-a>
│       ├── <manifest-b>
│       ├── <config-a>
│       └── <config-b>
├── ws-aaa/
│   ├── oci-layout
│   └── index.json             ← tags → manifest digests
└── ws-bbb/
    ├── oci-layout
    └── index.json
```

### 2.1 Key Properties

- **Blobs are content-addressed**: The path `blobs/sha256/<hex>` is determined entirely by the content hash. Two workspaces producing the same layer get the same path — no duplication.
- **Indexes are per-workspace**: Each workspace has its own `index.json` mapping tags to manifest digests. Workspace isolation is preserved — `bento list`, `bento gc`, and sequence numbers are scoped to a single workspace.
- **Manifests and configs are blobs too**: They live in the shared pool alongside layers. Only `index.json` and `oci-layout` are workspace-specific.

### 2.2 Disk Savings

| Scenario | Current | Shared |
|----------|---------|--------|
| 2 workspaces, identical deps (200 MB) | 400 MB | 200 MB |
| 5 forks from same checkpoint | 5× full size | 1× full + deltas |
| open → save with no changes | 2× full size | 1× full size |

## 3. OCI Compatibility

The OCI Image Layout spec requires `blobs/` to be a sibling of `index.json` within the layout directory. A shared blob pool breaks this assumption.

### 3.1 Option A: Symlinks

Each workspace directory contains a symlink `blobs → ../../blobs`:

```
~/.bento/store/
├── blobs/sha256/...           ← real blobs
├── ws-aaa/
│   ├── oci-layout
│   ├── index.json
│   └── blobs → ../blobs       ← symlink
└── ws-bbb/
    ├── oci-layout
    ├── index.json
    └── blobs → ../blobs
```

**Pros**: oras-go and other OCI tools work unmodified. Standard OCI layout from each workspace's perspective.
**Cons**: Symlinks have edge cases on Windows. GC becomes harder — need refcounting across workspaces.

### 3.2 Option B: Custom Store Implementation

Replace the oras-go `oci.Store` with a custom implementation that reads/writes blobs from the shared pool while maintaining per-workspace indexes.

**Pros**: Full control. No symlink issues. Can implement refcounting natively.
**Cons**: More code to maintain. Must reimplement blob push/fetch/exists.

### 3.3 Option C: Hardlinks

Same as Option A but using hardlinks instead of symlinks. Each blob file has multiple hardlinks — one in the shared pool, one in each workspace that references it.

**Pros**: Works transparently with oras-go. No symlink issues on Windows. Each workspace looks like a complete OCI layout.
**Cons**: Hardlinks don't work across filesystem boundaries. Refcounting is implicit (link count) but GC still needs to scan all workspaces.

### 3.4 Recommendation

Option A (symlinks) for the initial implementation. It's the simplest path that works with the existing oras-go store. Windows support can use junction points or fall back to Option B.

## 4. Garbage Collection

With shared blobs, GC becomes a two-phase process:

### 4.1 Phase 1: Workspace GC (existing)

`bento gc` within a workspace removes old manifests from that workspace's `index.json` based on retention policy. This is unchanged.

### 4.2 Phase 2: Blob GC (new)

After workspace GC, orphaned blobs — blobs not referenced by any manifest in any workspace — can be reclaimed.

Algorithm:
1. Walk all workspace `index.json` files
2. For each manifest, collect all referenced blob digests (config + layers)
3. Walk `blobs/sha256/`
4. Delete any blob not in the referenced set

This is analogous to `git gc` pruning unreachable objects.

### 4.3 Safety

- Blob GC MUST NOT run concurrently with save/push operations
- A lock file (`~/.bento/store/.gc-lock`) prevents concurrent GC
- Blob GC SHOULD be opt-in (`bento gc --prune-blobs`) rather than automatic

## 5. Migration

### 5.1 Detection

On startup, check if `~/.bento/store/blobs/` exists at the store root level. If not, the store is in the old per-workspace layout.

### 5.2 Lazy Migration

Migrate workspaces on first access rather than all at once:

1. When opening a workspace store, check if `blobs` is a symlink
2. If not, move `ws-xxx/blobs/sha256/*` to `store/blobs/sha256/` (skip if blob already exists — content-addressed, so identical)
3. Replace `ws-xxx/blobs` with a symlink to `../blobs`
4. Write a marker file to avoid re-migration

### 5.3 Backward Compatibility

Old bento versions that don't understand the shared layout will follow the symlink transparently — `oci.Store` resolves symlinks when reading. No breakage for read operations. Write operations from old versions would create blobs in the shared pool (via the symlink), which is correct behavior.

## 6. Scope and Non-Goals

### In Scope
- Shared blob storage across local workspaces
- Blob-level GC
- Migration from per-workspace layout

### Not In Scope
- Remote registry dedup (already works)
- Compression or dedup within blobs (e.g., block-level dedup)
- Cross-machine blob sharing (use a registry for that)

## 7. Related Specifications

- [SPEC.md](./SPEC.md) Section 9 — Store Behavior
- [portable-config.md](./portable-config.md) — Workspace ID and store path handling
