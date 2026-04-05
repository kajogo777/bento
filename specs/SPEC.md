# Bento Artifact Format Specification

**Version:** 0.3.0
**Status:** Draft
**Authors:** [TBD]
**Repository:** github.com/kajogo777/bento

## Abstract

This specification defines the **Bento Artifact Format**, an open standard for packaging AI agent workspace state as OCI (Open Container Initiative) artifacts. A bento artifact consists of semantically typed layers representing different aspects of a workspace -- project files, agent state, and dependencies -- bundled with structured metadata into a standard OCI image manifest.

The format is designed to be portable (any OCI registry), inspectable (semantic layer types), efficient (content-deduplicated), and extensible (custom layers via extensions).

## 1. Terminology

**Workspace**: A directory tree containing all files relevant to an agent's task -- code, agent memory, dependencies, tool configs, and build artifacts.

**Checkpoint**: An immutable, content-addressed snapshot of a workspace at a point in time, stored as a tagged OCI artifact.

**Layer**: A tar+gzip archive containing a subset of workspace files, identified by a media type that declares what kind of content it holds.

**Extension**: A composable unit that contributes file patterns to bento's layer model.

**Store**: An OCI Target (local OCI layout directory or remote registry) where checkpoints are stored.

**Checkpoint DAG**: The directed acyclic graph formed by parent references between checkpoints, enabling branching and lineage tracking.

**Hook**: An optional shell command that bento runs at a specific lifecycle point (pre-save, post-restore, etc.).

## 2. Relationship to OCI Specifications

Bento artifacts conform to:

- [OCI Image Manifest Specification v1.1](https://github.com/opencontainers/image-spec/blob/main/manifest.md)
- [OCI Distribution Specification v1.1](https://github.com/opencontainers/distribution-spec)
- [OCI Image Layout Specification](https://github.com/opencontainers/image-spec/blob/main/image-layout.md) (for local stores)

Bento uses the standard OCI image manifest with `artifactType` set to `application/vnd.bento.workspace.v1`. It follows the OCI 1.1 convention of reusing image manifests with a typed config descriptor for session metadata.

## 3. Artifact Structure

### 3.1 Manifest

A bento checkpoint is represented as an OCI image manifest. Bento uses **standard OCI media types** for the config and layers, making artifacts natively compatible with Docker, containerd, buildkit, and all OCI tooling. The `artifactType` field and `dev.bento.*` annotations distinguish bento artifacts from regular container images. Layer semantics (deps, agent, project) are carried by the `org.opencontainers.image.title` annotation on each layer descriptor.

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.bento.workspace.v1",
  "config": {
    "mediaType": "application/vnd.oci.image.config.v1+json",
    "digest": "sha256:abc123...",
    "size": 512
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:333...",
      "size": 93323264,
      "annotations": {
        "org.opencontainers.image.title": "deps",
        "dev.bento.layer.file-count": "1204"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:222...",
      "size": 65536,
      "annotations": {
        "org.opencontainers.image.title": "agent",
        "dev.bento.layer.file-count": "8"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:111...",
      "size": 131072,
      "annotations": {
        "org.opencontainers.image.title": "project",
        "dev.bento.layer.file-count": "42"
      }
    }
  ],
  "annotations": {
    "org.opencontainers.image.created": "2026-03-26T10:00:00Z",
    "dev.bento.checkpoint.message": "refactored auth module",
    "dev.bento.checkpoint.sequence": "3",
    "dev.bento.checkpoint.parent": "sha256:def456...",
    "dev.bento.extensions": "claude-code,node",
    "dev.bento.task": "refactor auth module"
  }
}
```

**Design decision: standard OCI media types.** Early drafts of this spec used custom media types (`application/vnd.bento.layer.*.v1.tar+gzip`) for layers and config. We switched to standard OCI types (`application/vnd.oci.image.layer.v1.tar+gzip`, `application/vnd.oci.image.config.v1+json`) because bento layers are structurally identical to OCI image layers (tar+gzip filesystem archives). Using standard types enables native Docker interop: `COPY --from` in Dockerfiles, `docker pull`, and containerd extraction all work without the bento binary. Layer semantics are fully preserved through annotations, and the `artifactType` field identifies bento artifacts for tools that need to distinguish them from container images.

### 3.2 Config Object

Media type: `application/vnd.oci.image.config.v1+json`

The config object is a valid OCI image config. Bento metadata is stored in `config.Labels` for Docker compatibility, with the full bento config serialized in the `dev.bento.config` label for lossless round-trip:

```json
{
  "architecture": "amd64",
  "os": "linux",
  "created": "2026-03-26T10:00:00Z",
  "config": {
    "Labels": {
      "dev.bento.extensions": "claude-code,node",
      "dev.bento.checkpoint.sequence": "3",
      "dev.bento.format.version": "0.3.0",
      "dev.bento.config": "{\"schemaVersion\":\"1.0.0\",\"extensions\":[\"claude-code\",\"node\"],\"task\":\"refactor auth module\",\"checkpoint\":3,\"created\":\"2026-03-26T10:00:00Z\"}"
    }
  },
  "rootfs": {
    "type": "layers",
    "diff_ids": ["sha256:333...", "sha256:222...", "sha256:111..."]
  }
}
```

The `dev.bento.config` label contains the full bento metadata as JSON:

```json
{
  "schemaVersion": "1.0.0",
  "extensions": ["claude-code", "node"],
  "task": "refactor auth module",
  "checkpoint": 3,
  "created": "2026-03-26T10:00:00Z",
  "status": "paused",
  "repos": [
    {"path": ".", "remote": "git@github.com:myorg/myapp.git", "branch": "main", "sha": "a1b2c3d"}
  ],
  "environment": { "os": "linux", "arch": "amd64" }
}
```

### 3.3 Layer Types

#### 3.3.1 Core Layer Types

All layers use the standard OCI media type `application/vnd.oci.image.layer.v1.tar+gzip`. The layer's role is identified by the `org.opencontainers.image.title` annotation.

| Name | Title annotation | Description |
|---|---|---|
| deps | `deps` | Installed packages, virtual environments, build caches, compiled artifacts |
| agent | `agent` | Agent memory, conversation history, plans, skills, commands, session state |
| project | `project` | Source code, tests, build definitions, configs, and any other workspace files |

**Design rationale:** Layers are ordered from bottom (least-changing) to top (most-changing), following OCI convention. Deps change rarely and are large, so they sit at the bottom for maximum cache reuse. The project layer acts as a catch-all: any workspace file not matched by agent or deps patterns is captured here.

#### 3.3.2 Well-Known Custom Layer Types

Extensions MAY use these registered types for common additional layers. These are not required but provide consistent media types when multiple extensions need the same concept.

| Media Type | Name | Description |
|---|---|---|
| `application/vnd.bento.layer.build-cache.v1.tar+gzip` | build-cache | Incremental compilation state, webpack cache, .tsbuildinfo |
| `application/vnd.bento.layer.data.v1.tar+gzip` | data | SQLite databases, local data files, seed data |
| `application/vnd.bento.layer.runtime.v1.tar+gzip` | runtime | Pinned agent CLI binaries and MCP server binaries |
| `application/vnd.bento.layer.custom.v1.tar+gzip` | custom | Any extension-specific content not covered above |

#### 3.3.3 Non-Layer Artifacts

| Media Type | Name | Description |
|---|---|---|
| `application/vnd.bento.secrets-manifest.v1+json` | secrets-manifest | Secret reference pointers (never contains actual secrets) |
| `application/vnd.bento.runtime-lock.v1+json` | runtime-lock | Pinned tool versions with integrity hashes |

#### 3.3.4 Layer Content Format

Each layer is a gzip-compressed tar archive. File paths within the archive MUST use forward slashes and be relative to the workspace root. Implementations SHOULD preserve timestamps. File permissions and symlinks are handled according to the cross-platform rules in Section 15.

Layers MUST NOT contain:
- Absolute paths
- Paths containing backslashes
- Symlinks pointing outside the archive
- Files matching patterns in `.bentoignore` or the extension list
- Content matching known secret patterns (see Section 6)

#### 3.3.5 Layer Assignment Rules

When a file matches patterns in multiple layers, the **first matching layer** in the extension definition order wins. The project layer is a **catch-all**: any workspace file not matched by agent or deps patterns (and not in the ignore list) is captured in the project layer. This ensures no workspace file is silently excluded.

### 3.4 Annotations

Bento uses the `dev.bento.*` annotation namespace on both manifests and layer descriptors.

#### 3.4.1 Manifest Annotations

| Key | Required | Description |
|---|---|---|
| `org.opencontainers.image.created` | REQUIRED | RFC 3339 timestamp |
| `dev.bento.checkpoint.sequence` | REQUIRED | Monotonically increasing checkpoint number |
| `dev.bento.checkpoint.parent` | RECOMMENDED | Digest of parent checkpoint |
| `dev.bento.checkpoint.message` | OPTIONAL | Human-readable description |
| `dev.bento.extensions` | RECOMMENDED | Comma-separated list of active extensions |
| `dev.bento.task` | OPTIONAL | Task description |
| `dev.bento.extensions` | RECOMMENDED | Extensions active when artifact was produced |
| `dev.bento.format.version` | RECOMMENDED | Spec version (e.g., "0.3.0") |

#### 3.4.2 Layer Annotations

| Key | Required | Description |
|---|---|---|
| `org.opencontainers.image.title` | REQUIRED | Layer name (project, agent, deps, etc.) |
| `dev.bento.layer.file-count` | OPTIONAL | Number of files in layer |

## 4. Checkpoint DAG

Checkpoints form a directed acyclic graph through the `dev.bento.checkpoint.parent` annotation and the `parentCheckpoint` field in the config object. The value is the digest of the parent checkpoint's manifest.

### 4.1 Head Tracking

Each workspace directory tracks its own position in the DAG via a `head` field in `bento.yaml`. This is the manifest digest of the directory's current checkpoint. On save, the parent is derived from `head` (not from a global tag). After save, `head` is updated to the new checkpoint's digest. After open, `head` is updated to the opened checkpoint's digest.

This enables multiple directories to share one store while maintaining independent positions in the DAG — like git worktrees.

### 4.2 Linear History

```
cp-1 (parent: none) → cp-2 (parent: cp-1) → cp-3 (parent: cp-2)
```

### 4.3 Branching

```
cp-1 → cp-2 → cp-3 → cp-4 (workspace A)
          ↘→ cp-5 → cp-6 (workspace B)
```

Branching happens naturally when a checkpoint is opened into a new directory. Both directories share the same store and workspace ID. Each directory's `head` points to its own position, so saves from each directory get the correct parent. No explicit fork command is needed.

### 4.4 Restorable Open

Before overwriting files, `bento open` saves the current workspace state as a `pre-open` checkpoint. The user can undo the last open with `bento open undo`. The undo creates its own `pre-open` backup, making it a two-state toggle.

### 4.5 DAG Traversal

Implementations SHOULD provide commands to walk the checkpoint DAG (e.g., `bento list`, `bento inspect`). The DAG can be reconstructed from manifest annotations alone -- no sidecar database is required.

### 4.6 Digest References

Checkpoint references may be tags (e.g., `cp-3`, `pre-open`) or content digests (e.g., `sha256:abc123...`). Implementations MUST handle both formats. The `head` field in `bento.yaml` stores a digest reference.

## 5. Referrers (Attached Artifacts)

Bento uses the OCI 1.1 Referrers API to attach metadata artifacts to checkpoints without mutating them. Attached artifacts reference a checkpoint via the `subject` field in their manifest.

Common attachment types:

| Artifact Type | Use Case |
|---|---|
| `application/vnd.bento.attachment.diff.v1+patch` | Patch/diff showing what changed |
| `application/vnd.bento.attachment.test-results.v1+json` | Test run results |
| `application/vnd.bento.attachment.usage.v1+json` | Token usage / cost report |
| `application/vnd.bento.attachment.log.v1+jsonl` | Agent conversation log |

Implementations SHOULD support `bento attach` and `bento inspect --referrers` for managing referrers.

## 6. Secret Safety

### 6.1 Secret References

The secrets manifest (`application/vnd.bento.secrets-manifest.v1+json`) contains only pointers to external secret sources:

```json
{
  "schemaVersion": "1.0.0",
  "secrets": {
    "DATABASE_URL": {
      "source": "vault",
      "path": "secret/data/myapp/db",
      "key": "url"
    },
    "GITHUB_TOKEN": {
      "source": "env",
      "var": "GITHUB_TOKEN"
    },
    "AWS_ACCESS_KEY_ID": {
      "source": "aws-sts",
      "role": "arn:aws:iam::123:role/agent-role"
    }
  }
}
```

Supported source types: `vault`, `env`, `aws-sts`, `1password`, `gcloud`, `azure-keyvault`, `file`. Implementations MAY add custom source types.

### 6.2 Env File Export

Implementations SHOULD provide a CLI command to resolve all env vars and secrets
and export them as a `.env` file:

```bash
bento env export -o .env                          # generate from resolved values
bento env export -o .env --template .env.example  # use a template file
```

When a template is provided, implementations SHOULD:
1. Read the template file
2. Resolve each secret from the secrets config
3. Substitute matching keys in the template
4. Write the populated `.env` file to disk with 0600 permissions

The template file (e.g., `.env.example`) is captured in the project layer with
placeholder values. The populated `.env` file is excluded from all layers.

### 6.3 Pre-Push Secret Scan

Implementations MUST scan all layer content for potential secrets before pushing to a store. The scan SHOULD check for:

- High-entropy strings matching common key formats
- Known secret patterns (AWS keys, GitHub tokens, private keys, etc.)
- Files matching common credential file names

If potential secrets are detected, the push MUST be aborted with a clear error message identifying the offending files.

### 6.4 Default Exclusion Patterns

The following patterns MUST be excluded from all layers by default:

```
.env.local
.env.*.local
*.pem
*.key
*.p12
*.pfx
token.json
credentials
.git/credentials
.aws/credentials
.ssh/*
```

Additional patterns can be specified in `.bentoignore` and via the extension method.

## 6.5 Environment Variables and Secret References in Manifests

Bento checkpoints are intended to be self-describing: a checkpoint pushed to a registry should carry enough information to restore the workspace on a new machine without requiring the original `bento.yaml`. To achieve this, env vars and secret references are embedded in the `dev.bento.config` label of the OCI image config.

### 6.5.1 Plain Environment Variables

Non-sensitive key-value environment variables are stored verbatim in the `env` field of the bento config object:

```json
{
  "env": {
    "NODE_ENV": "development",
    "PORT": "3000",
    "LOG_LEVEL": "debug"
  }
}
```

**Rule: never store secret values here.** Only non-sensitive configuration belongs in `env`.

### 6.5.2 Secret References

Secret references are stored in the `secrets` field. Only the reference (provider + path) is stored, never the value:

```json
{
  "secrets": {
    "DATABASE_URL": {
      "source": "vault",
      "path": "secret/data/myapp/db",
      "key": "url"
    },
    "GITHUB_TOKEN": {
      "source": "env",
      "var": "GITHUB_TOKEN"
    },
    "AWS_ACCESS_KEY_ID": {
      "source": "aws-sts",
      "role": "arn:aws:iam::123:role/agent-role"
    }
  }
}
```

Supported `source` values: `vault`, `env`, `aws-sts`, `1password`, `gcloud`, `azure`, `file`, `exec`.

### 6.5.3 Docker Compatibility

Docker and containerd ignore unknown labels in the OCI image config. Storing env vars and secret refs in `dev.bento.config` is therefore fully Docker-compatible: `docker pull`, `COPY --from`, and containerd extraction all work unmodified.

## 7. Hooks

Hooks are optional shell commands that run at lifecycle points. They allow users to integrate bento with their existing build systems, orchestration tools, and scripts without bento needing to understand or replicate that functionality.

### 7.1 Hook Lifecycle

```
save:     pre_save → scan → pack layers → secret scan → push → post_save
restore:  pull → unpack layers → hydrate secrets → populate env files → post_restore
push:     pre_push → copy to remote → post_push
fork:     restore from parent → post_fork
```

### 7.2 Hook Definition

Hooks are defined in `bento.yaml`:

```yaml
hooks:
  pre_save: "make clean-temp"
  post_save: "echo 'checkpoint saved'"
  post_restore: "make setup"
  pre_push: "npm test"
  post_fork: "./scripts/seed-db.sh"
```

All hooks are optional. Each hook value is a shell command string executed via `sh -c`. The working directory is the workspace root.

### 7.3 Hook Exit Codes

If a `pre_*` hook exits with a non-zero status, the operation is aborted. If a `post_*` hook exits with a non-zero status, the operation completes but a warning is emitted.

### 7.4 Extension Default Hooks

Extensions MAY provide default hooks via the `DefaultHooks()` method. User-defined hooks in `bento.yaml` override extension defaults for the same lifecycle point.

## 7.5 Watch Mode

`bento watch` runs a background file-system watcher that automatically creates checkpoints as the workspace changes. It is intended for long-running agent sessions where the user wants passive checkpointing without calling `bento save` manually.

### 7.5.1 Behavior

- Starts watching the workspace directory for file-system events.
- Debounces changes: waits until no file-system events have occurred for a configurable quiet period (default: 10 seconds) before triggering a checkpoint.
- Creates a checkpoint using the same logic as `bento save`, including secret scanning, ignore patterns, and hooks.
- Prints a one-line summary for each auto-checkpoint.
- Runs until terminated (Ctrl-C or SIGTERM).

### 7.5.2 Configuration

```yaml
watch:
  debounce: 10          # seconds to wait after last change before saving (default: 10)
  message: "auto-save"  # checkpoint message for auto-saves (default: "auto-save")
  skip_secret_scan: false
```

All `bento watch` flags mirror the `bento save` flags.

### 7.5.3 Interaction with Hooks

`bento watch` fires the same lifecycle hooks as `bento save` (`pre_save`, `post_save`). A `pre_save` hook failure aborts the auto-checkpoint and prints a warning, but does not stop the watcher.

### 7.5.4 Limitations

- Watch mode is not safe for concurrent access from multiple processes. Run one watcher per workspace.
- Events from dependency directories (e.g. `node_modules`) are ignored via the normal ignore pattern rules.

## 8. Extension Interface

An extension is a composable unit that contributes patterns to bento's layer model. Each extension has a single concern: an agent framework, a language/framework, or a tool.

### 8.1 Go Interface

```go
type Extension interface {
    // Name returns the extension identifier (e.g., "claude-code", "node").
    Name() string

    // Detect returns true if this extension is relevant to the workspace.
    Detect(workDir string) bool

    // Contribute returns the patterns and config this extension adds.
    Contribute(workDir string) Contribution
}

type Contribution struct {
    Layers      map[string][]string // layer name → patterns to add
    ExtraLayers []LayerDef          // new layers (e.g., "build-cache")
    Ignore      []string            // patterns to exclude
    Hooks       map[string]string   // default lifecycle hooks
}
```

### 8.2 Built-in Extensions

Agent extensions: `claude-code`, `codex`, `opencode`, `openclaw`, `cursor`, `agents-md`
Deps extensions: `node`, `python`, `go-mod`, `rust`
Tool extensions: `tool-versions`

All extensions auto-detect. When `extensions:` is listed in `bento.yaml`, only those extensions are used.

## 9. Store Behavior

### 9.1 Local Store (OCI Image Layout)

The default store is a local OCI image layout directory at `~/.bento/store/`. Layer blobs are shared across all workspaces via a content-addressed blob pool at the store root, while each workspace retains its own index (tags, manifests). This eliminates redundant disk usage when multiple workspaces contain identical layers.

```
~/.bento/store/
├── blobs/                     ← shared across all workspaces
│   └── sha256/
│       ├── abc123...          # manifests, configs, and layer tarballs
│       ├── def456...          # (content-addressed — identical content stored once)
│       └── 789012...
├── ws-aaa/
│   ├── oci-layout             # {"imageLayoutVersion": "1.0.0"}
│   ├── index.json             # tags → manifest digests
│   └── blobs → ../blobs       # symlink (junction on Windows)
└── ws-bbb/
    ├── oci-layout
    ├── index.json
    └── blobs → ../blobs
```

Each workspace directory contains a symlink `blobs → ../blobs` (or a directory junction on Windows) so that oras-go and other OCI tools see a standard OCI image layout. Manifests, configs, and layers all live in the shared pool; only `index.json` and `oci-layout` are workspace-specific.

#### 9.1.1 Garbage Collection

GC is a two-phase process:

- **Phase 1 (workspace GC):** `bento gc` removes old manifests from a workspace's `index.json` based on retention policy (`--keep-last`, `--keep-tagged`). This is scoped to a single workspace.
- **Phase 2 (blob GC):** Runs automatically after Phase 1. Scans all workspace indexes and deletes blobs from the shared pool that are no longer referenced by any manifest in any workspace. A lock file (`~/.bento/store/.gc-lock`) prevents concurrent blob GC. Blob GC MUST NOT run concurrently with save/push operations.

### 9.2 Remote Store (OCI Registry)

Any OCI 1.1-compliant registry. Bento uses Docker credential helpers for authentication, ensuring compatibility with existing `docker login` / `crane auth` workflows.

### 9.3 Sync

When both `store` and `remote` are configured, `bento push` copies artifacts from local to remote using `oras.Copy()`. Cross-repository blob mounting is used when the registry supports it, avoiding redundant uploads.

## 10. Tagging Convention

Implementations SHOULD follow this tagging convention:

- `cp-N` -- sequential checkpoint number (e.g., `cp-1`, `cp-2`, `cp-3`)
- `latest` -- always points to the most recent checkpoint
- `cp-N-fork-M` -- fork number M from checkpoint N
- User-defined tags via `bento tag`

Tags are mutable references to immutable digests, following standard OCI tag semantics.

## 11. Selective Restore

Implementations SHOULD support restoring a subset of layers:

```bash
bento open myproject:cp-3 --layers project,agent    # only these layers
bento open myproject:cp-3 --skip-layers deps         # everything except deps
```

This enables fast partial restores when only certain state is needed (e.g., pulling just the agent layer to inspect the agent's memory without downloading a multi-gigabyte dependency tree).

## 12. Compatibility

### 12.1 Registry Compatibility

Bento artifacts are standard OCI 1.1 artifacts. They work with any registry that supports:
- OCI Image Manifest v2 schema 2
- `artifactType` field (OCI 1.1)
- Referrers API (for attached artifacts)

Known-compatible registries: GitHub Container Registry, Docker Hub, Amazon ECR, Google Artifact Registry, Azure Container Registry, Red Hat Quay, Harbor, Zot, JFrog Artifactory.

### 12.2 Tool Compatibility

Bento artifacts can be inspected and manipulated by any OCI-aware tool:
- `docker manifest inspect`
- `crane manifest / crane pull`
- `skopeo inspect / skopeo copy`
- `cosign sign / cosign verify`
- `oras discover` (for referrers)
- `trivy image` (for scanning)

## 13. Versioning

This specification uses semantic versioning. The `schemaVersion` field in config objects tracks compatibility:

- **Major version**: Breaking changes to the artifact format
- **Minor version**: Backwards-compatible additions (new layer types, annotations)
- **Patch version**: Clarifications, typo fixes

Implementations MUST reject artifacts with an unrecognized major version and SHOULD handle unknown minor-version additions gracefully.

## 14. Security Considerations

- **Secret scanning**: Pre-push scans are a defense-in-depth measure, not a guarantee. Users are responsible for ensuring sensitive data is excluded via `.bentoignore`.
- **Registry access control**: Bento inherits the access control model of the underlying registry. Private artifacts require proper authentication and authorization.
- **Content integrity**: All content is addressed by SHA-256 digest. Implementations SHOULD verify digests on restore.
- **Signing**: Bento artifacts can be signed using Notation or cosign. Implementations SHOULD document how to verify signatures.
- **Env file safety**: Populated `.env` files MUST never be included in any layer. Only template files with placeholder values are captured.

## 15. Cross-Platform Behavior

Bento checkpoints are portable across macOS, Linux, and Windows. The following rules ensure that an artifact created on one platform can be restored on another without user intervention. These are implementation requirements, not user-facing configuration.

### 15.1 Path Handling

All file paths within tar archives MUST use forward slashes, regardless of the platform that created the archive. Implementations MUST normalize backslashes to forward slashes on save and convert to the native separator on restore.

Glob patterns in extension definitions and `.bentoignore` MUST use forward slashes. Implementations MUST match these patterns against normalized forward-slash paths.

### 15.2 File Permissions

Implementations MUST store POSIX permission bits in tar headers on platforms that support them (Linux, macOS). On restore:

- On Linux/macOS: apply the stored permission bits as-is.
- On Windows: ignore stored permission bits. Apply default ACLs appropriate for the file type.

When creating archives on Windows (where POSIX permissions are not available), implementations SHOULD write 0644 for regular files and 0755 for directories and files with executable extensions (`.sh`, `.bash`, `.py`, `.rb`, `.pl`, `.js` when preceded by a shebang, and any file in a `bin/` directory).

### 15.3 Symlinks

Implementations MUST store symlinks as symlinks in tar archives (using the tar symlink entry type). On restore:

- On Linux/macOS: create symlinks as-is.
- On Windows: attempt to create symlinks. If symlink creation fails (common without developer mode or admin privileges), silently copy the target file or directory instead. This MUST NOT produce an error.

### 15.4 Line Endings

Implementations MUST NOT normalize line endings on save or restore. Files are stored byte-for-byte as they exist on disk. This matches git's default behavior and avoids corrupting binary files.

If a team works across platforms and needs line ending normalization, they should handle it with their existing tools (`.gitattributes`, `.editorconfig`, etc.). This is not bento's responsibility.

### 15.5 Case Sensitivity

On save, implementations SHOULD warn if two or more files in the same directory differ only by case (e.g., `Config.json` and `config.json`). These files will collide on case-insensitive filesystems (macOS default, Windows). The warning SHOULD name the conflicting files but MUST NOT block the save.

On restore to a case-insensitive filesystem, if a collision is detected, the last file in archive order wins. Implementations SHOULD warn when this occurs.

### 15.6 Long Paths

On save, implementations SHOULD warn if any file path exceeds 260 characters. This is the default path length limit on Windows. The warning MUST NOT block the save, as long path support is available on Windows 10+ via manifest or registry settings.

### 15.7 Default Store Location

Implementations MUST use the platform-appropriate default store location:

- Linux: `$XDG_DATA_HOME/bento/store` (falls back to `~/.local/share/bento/store`)
- macOS: `~/.bento/store`
- Windows: `%LOCALAPPDATA%\bento\store`

The `store` field in `bento.yaml` overrides the default on all platforms. Path expansion (`~`, `$HOME`, `%USERPROFILE%`) MUST be handled by the implementation.

### 15.8 Hook Execution

Implementations MUST execute hooks using the platform's native shell:

- Linux/macOS: `sh -c "<command>"`
- Windows: `cmd /c "<command>"`

Extensions that need cross-platform hooks should use platform-agnostic commands (e.g., `make`, `npm run`, `go run`) or define platform-specific hooks:

```yaml
hooks:
  post_restore: "make setup"                  # cross-platform via Makefile
  post_restore.windows: "powershell setup.ps1"  # Windows-specific override
  post_restore.linux: "./setup.sh"              # Linux-specific override
```

When a platform-specific hook is defined, it takes precedence over the base hook on that platform.

## 16. Out of Scope

The following are explicitly out of scope for this version of the specification. They may be addressed in future versions.

- **Concurrent access**: Multiple agents or processes saving to the same workspace simultaneously is undefined behavior. Use separate workspaces or worktrees for parallel agents and coordinate via tags.
- **Agent identity and roles**: Tracking which agent in a multi-agent team created a checkpoint (e.g., investigator vs. coder vs. reviewer). The `dev.bento.extensions` annotation provides basic identification but role-based attribution is not specified.
- **Supply chain security**: Signing checkpoints with Sigstore/cosign/Notation and verifying provenance. Bento artifacts are compatible with these tools but this spec does not define signing workflows or trust policies.
- **Context summarization**: Generating compact handoff documents for agents on restore. Agents load their session history directly from the agent layer. Users can add summarization via hooks if needed.

## 17. Future Extensions

The following are anticipated but not yet specified:

- **Encryption**: Client-side encryption of layer content before push
- **Streaming restore**: Progressive layer extraction during restore
- **Concurrent access protocol**: Locking or conflict resolution for shared workspaces
- **Agent identity and provenance**: Structured agent role metadata and signed checkpoint chains

---

## Appendix A: IANA Media Type Registrations

The following media types are defined or used by this specification:

### Bento-specific types

```
application/vnd.bento.workspace.v1              (manifest artifactType)
```

### Standard OCI types (used for Docker compatibility)

```
application/vnd.oci.image.config.v1+json        (config)
application/vnd.oci.image.layer.v1.tar+gzip     (all layers)
```

### Non-layer artifacts

```
application/vnd.bento.secrets-manifest.v1+json
application/vnd.bento.runtime-lock.v1+json
```

### Attachment types

```
application/vnd.bento.attachment.diff.v1+patch
application/vnd.bento.attachment.test-results.v1+json
application/vnd.bento.attachment.usage.v1+json
application/vnd.bento.attachment.log.v1+jsonl
```

## Appendix B: Agent Configuration Locations

This table documents where major agent frameworks store their state on disk. Extension authors should use this as a reference when defining layer patterns.

| Agent | Project config | User config | Sessions | Memory / Plans |
|---|---|---|---|---|
| Claude Code | `.claude/`, `CLAUDE.md` | `~/.claude/` | `~/.claude/projects/*/` (JSONL) | `CLAUDE.md`, `.claude/plans/` |
| Codex | `.codex/`, `AGENTS.md` | `~/.codex/` | `~/.codex/sessions/` (JSONL) | `AGENTS.md` |
| Aider | `.aider.conf.yml` | `~/.aider.conf.yml` | `.aider.chat.history.md` | `.aider.tags.cache.v3/` |
| Cursor | `.cursor/rules/*.mdc` | `~/.cursor/` | VS Code state.vscdb | `.cursor/rules/` |
| Windsurf | `.windsurf/rules/` | `~/.codeium/windsurf/` | In-memory | `~/.codeium/windsurf/memories/` |
| Docker Agent | `agents.yaml` | N/A | N/A | Agent YAML configs |

| Agent | MCP config (project) | MCP config (user) |
|---|---|---|
| Claude Code | `.mcp.json` | `~/.claude.json` |
| Codex | `.codex/config.toml` | `~/.codex/config.toml` |
| Cursor | `.cursor/mcp.json` | `~/.cursor/mcp.json` |

## Appendix C: Example Manifest (Complete)

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.bento.workspace.v1",
  "config": {
    "mediaType": "application/vnd.oci.image.config.v1+json",
    "digest": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "size": 512
  },
  "layers": [
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:7d865e959b2466918c9863afca942d0fb89d7c9ac0c99bafc3749504ded97730",
      "size": 93323264,
      "annotations": {
        "org.opencontainers.image.title": "deps",
        "dev.bento.layer.file-count": "1204"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c",
      "size": 65536,
      "annotations": {
        "org.opencontainers.image.title": "agent",
        "dev.bento.layer.file-count": "8"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447",
      "size": 131072,
      "annotations": {
        "org.opencontainers.image.title": "project",
        "dev.bento.layer.file-count": "42"
      }
    }
  ],
  "annotations": {
    "org.opencontainers.image.created": "2026-03-26T10:00:00Z",
    "dev.bento.checkpoint.sequence": "3",
    "dev.bento.checkpoint.parent": "sha256:abc123def456...",
    "dev.bento.checkpoint.message": "refactored auth module",
    "dev.bento.extensions": "claude-code,node",
    "dev.bento.task": "refactor auth module",
    "dev.bento.format.version": "0.3.0"
  }
}
```

## Appendix D: MCP Server Specification

Bento exposes an MCP (Model Context Protocol) server that allows AI agents to manage checkpoints as tool calls during a session. This enables agents to checkpoint before risky operations, restore after failures, and fork to explore alternative approaches -- all without human intervention.

### D.1 Server Configuration

The bento MCP server runs as a stdio-based MCP server. Agents and MCP clients configure it as:

```json
{
  "mcpServers": {
    "bento": {
      "command": "bento",
      "args": ["mcp-server"],
      "env": {
        "BENTO_WORKSPACE": "/path/to/workspace"
      }
    }
  }
}
```

If `BENTO_WORKSPACE` is not set, the server uses the current working directory. The server reads `bento.yaml` from the workspace root for store and extension configuration.

### D.2 Tools

The MCP server exposes the following tools.

#### bento_save

Save a checkpoint of the current workspace state.

```json
{
  "name": "bento_save",
  "description": "Save a checkpoint of the current workspace. Use before risky changes, after completing a subtask, or when switching approaches.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "message": {
        "type": "string",
        "description": "Short description of what was accomplished or the current state"
      },
      "tag": {
        "type": "string",
        "description": "Optional tag name for this checkpoint"
      }
    }
  }
}
```

Returns: checkpoint tag, digest, layer summary (which layers changed, which were reused).

#### bento_list

List available checkpoints for the current workspace.

```json
{
  "name": "bento_list",
  "description": "List all checkpoints for the current workspace, showing tags, timestamps, messages, and parent relationships.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "limit": {
        "type": "integer",
        "description": "Maximum number of checkpoints to return. Default: 10"
      }
    }
  }
}
```

Returns: array of checkpoints with tag, digest, timestamp, message, and parent digest.

#### bento_restore

Restore the workspace to a previous checkpoint.

```json
{
  "name": "bento_restore",
  "description": "Restore the workspace to a previous checkpoint. This replaces the current workspace files with the checkpoint state. Use when the current approach has failed and you want to go back.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "ref": {
        "type": "string",
        "description": "Checkpoint reference -- a tag (e.g., 'cp-3') or digest"
      },
      "layers": {
        "type": "string",
        "description": "Comma-separated list of layer names to restore. If omitted, all layers are restored."
      }
    },
    "required": ["ref"]
  }
}
```

Returns: confirmation with restored checkpoint details and layer summary.

#### bento_fork

Create a new branch from an existing checkpoint.

```json
{
  "name": "bento_fork",
  "description": "Fork from a checkpoint to explore an alternative approach. The current workspace is saved first, then restored to the fork point. Use when you want to try a different strategy without losing current progress.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "ref": {
        "type": "string",
        "description": "Checkpoint to fork from"
      },
      "message": {
        "type": "string",
        "description": "Description of what this fork will try"
      }
    },
    "required": ["ref"]
  }
}
```

Returns: new fork checkpoint tag, digest, and parent reference.

#### bento_diff

Compare two checkpoints to see what changed.

```json
{
  "name": "bento_diff",
  "description": "Show the differences between two checkpoints. Useful for understanding what changed between approaches or reviewing progress.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "from": {
        "type": "string",
        "description": "Source checkpoint reference"
      },
      "to": {
        "type": "string",
        "description": "Target checkpoint reference. Defaults to current workspace state if omitted."
      },
      "layers": {
        "type": "string",
        "description": "Comma-separated list of layer names to diff. If omitted, all layers are compared."
      }
    },
    "required": ["from"]
  }
}
```

Returns: per-layer diff summary with added, modified, and removed files. For the project layer, includes a unified diff of changed text files.

#### bento_inspect

Show detailed metadata for a checkpoint.

```json
{
  "name": "bento_inspect",
  "description": "Show detailed metadata and layer information for a checkpoint.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "ref": {
        "type": "string",
        "description": "Checkpoint reference. Defaults to 'latest' if omitted."
      }
    }
  }
}
```

Returns: full config object, layer details (name, media type, size, file count), annotations, and parent lineage.

### D.3 Usage Patterns

Agents are encouraged to use bento tools in the following patterns:

**Checkpoint before risk**: Call `bento_save` with a descriptive message before attempting a large refactor, deleting files, or changing architecture. If the attempt fails, call `bento_restore` to return to the known-good state.

**Fork to explore**: When uncertain between two approaches, call `bento_save` to preserve current state, then `bento_fork` from the current checkpoint to try the alternative. Compare results with `bento_diff`.

**Periodic progress saves**: Call `bento_save` after completing each logical subtask to create a trail of progress that can be inspected or rolled back to.

### D.4 Rate Limiting

Implementations SHOULD debounce rapid successive `bento_save` calls. If `bento_save` is called within 10 seconds of the previous save, the implementation SHOULD skip the save and return the previous checkpoint reference with a note that the save was debounced. This prevents agents from creating excessive checkpoints during tight loops.
