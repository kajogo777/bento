# Bento Artifact Format Specification

**Version:** 0.2.0-draft
**Status:** Draft
**Authors:** [TBD]
**Repository:** github.com/bentoci/bento

## Abstract

This specification defines the **Bento Artifact Format**, an open standard for packaging AI agent workspace state as OCI (Open Container Initiative) artifacts. A bento artifact consists of semantically typed layers representing different aspects of a workspace -- project files, agent state, and dependencies -- bundled with structured metadata into a standard OCI image manifest.

The format is designed to be portable (any OCI registry), inspectable (semantic layer types), efficient (content-deduplicated), and extensible (custom layers via harness adapters).

## 1. Terminology

**Workspace**: A directory tree containing all files relevant to an agent's task -- code, agent memory, dependencies, tool configs, and build artifacts.

**Checkpoint**: An immutable, content-addressed snapshot of a workspace at a point in time, stored as a tagged OCI artifact.

**Layer**: A tar+gzip archive containing a subset of workspace files, identified by a media type that declares what kind of content it holds.

**Harness**: An adapter that maps a specific agent framework's file layout to bento's layer taxonomy.

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

A bento checkpoint is represented as an OCI image manifest:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.bento.workspace.v1",
  "config": {
    "mediaType": "application/vnd.bento.config.v1+json",
    "digest": "sha256:abc123...",
    "size": 256
  },
  "layers": [
    {
      "mediaType": "application/vnd.bento.layer.project.v1.tar+gzip",
      "digest": "sha256:111...",
      "size": 131072,
      "annotations": {
        "org.opencontainers.image.title": "project",
        "dev.bento.layer.file-count": "42"
      }
    },
    {
      "mediaType": "application/vnd.bento.layer.agent.v1.tar+gzip",
      "digest": "sha256:222...",
      "size": 65536,
      "annotations": {
        "org.opencontainers.image.title": "agent",
        "dev.bento.layer.file-count": "8"
      }
    },
    {
      "mediaType": "application/vnd.bento.layer.deps.v1.tar+gzip",
      "digest": "sha256:333...",
      "size": 93323264,
      "annotations": {
        "org.opencontainers.image.title": "deps",
        "dev.bento.layer.file-count": "1204",
        "dev.bento.layer.change-frequency": "rarely"
      }
    }
  ],
  "annotations": {
    "org.opencontainers.image.created": "2026-03-26T10:00:00Z",
    "dev.bento.checkpoint.message": "refactored auth module",
    "dev.bento.checkpoint.sequence": "3",
    "dev.bento.checkpoint.parent": "sha256:def456...",
    "dev.bento.agent": "claude-code",
    "dev.bento.task": "refactor auth module"
  }
}
```

### 3.2 Config Object

Media type: `application/vnd.bento.config.v1+json`

The config object contains structured session metadata:

```json
{
  "schemaVersion": "1.0.0",
  "agent": "claude-code",
  "agentVersion": "1.2.3",
  "task": "refactor auth module",
  "sessionId": "abc123",
  "parentCheckpoint": "sha256:def456...",
  "checkpoint": 3,
  "created": "2026-03-26T10:00:00Z",
  "status": "paused",
  "harness": "claude-code",
  "gitSha": "a1b2c3d",
  "gitBranch": "main",
  "envFiles": {
    ".env": {
      "template": ".env.example",
      "secrets": ["DATABASE_URL", "GITHUB_TOKEN"]
    }
  },
  "metrics": {
    "tokenUsage": 45000,
    "duration": "1h23m",
    "layerCount": 3
  },
  "environment": {
    "os": "linux",
    "arch": "amd64"
  }
}
```

**Required fields:** `schemaVersion`, `created`, `checkpoint`, `harness`
**Optional fields:** All others. Implementations SHOULD include as many fields as available.

The `envFiles` section maps `.env` file paths to their templates and required secrets. On restore, implementations SHOULD populate `.env` files by reading the template from the project layer and substituting secret values resolved from the secrets manifest.

### 3.3 Layer Types

#### 3.3.1 Core Layer Types

Implementations MUST support these three layers. Together they cover the common case for most agent workspaces.

| Media Type | Name | Description |
|---|---|---|
| `application/vnd.bento.layer.project.v1.tar+gzip` | project | Source code, tests, build definitions, configs that belong in version control |
| `application/vnd.bento.layer.agent.v1.tar+gzip` | agent | Agent memory, conversation history, plans, skills, commands, session state |
| `application/vnd.bento.layer.deps.v1.tar+gzip` | deps | Installed packages, virtual environments, build caches, compiled artifacts |

**Design rationale:** Real-world agent workspaces contain state that ranges from tiny source files to multi-gigabyte dependency trees. Three core layers provide the right default decomposition: project files change every checkpoint and are small, agent state changes every checkpoint and is medium, deps change rarely and are large. This maximizes deduplication -- the deps layer digest is reused across many checkpoints while only the small project and agent layers are re-uploaded.

#### 3.3.2 Well-Known Custom Layer Types

Harnesses MAY use these registered types for common additional layers. These are not required but provide consistent media types when multiple harnesses need the same concept.

| Media Type | Name | Description |
|---|---|---|
| `application/vnd.bento.layer.build-cache.v1.tar+gzip` | build-cache | Incremental compilation state, webpack cache, .tsbuildinfo |
| `application/vnd.bento.layer.data.v1.tar+gzip` | data | SQLite databases, local data files, seed data |
| `application/vnd.bento.layer.runtime.v1.tar+gzip` | runtime | Pinned agent CLI binaries and MCP server binaries |
| `application/vnd.bento.layer.custom.v1.tar+gzip` | custom | Any harness-specific content not covered above |

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
- Files matching patterns in `.bentoignore` or the harness `Ignore()` list
- Content matching known secret patterns (see Section 6)

#### 3.3.5 Layer Assignment Rules

When a file matches patterns in multiple layers, the **first matching layer** in the harness definition order wins. Files that match no layer pattern are **excluded by default**. This is a deliberate design choice -- it keeps checkpoints clean and forces explicit decisions about what state matters.

If a harness needs to capture files that don't fit any defined layer, it should define an additional custom layer with appropriate patterns rather than relying on a catch-all.

### 3.4 Annotations

Bento uses the `dev.bento.*` annotation namespace on both manifests and layer descriptors.

#### 3.4.1 Manifest Annotations

| Key | Required | Description |
|---|---|---|
| `org.opencontainers.image.created` | REQUIRED | RFC 3339 timestamp |
| `dev.bento.checkpoint.sequence` | REQUIRED | Monotonically increasing checkpoint number |
| `dev.bento.checkpoint.parent` | RECOMMENDED | Digest of parent checkpoint |
| `dev.bento.checkpoint.message` | OPTIONAL | Human-readable description |
| `dev.bento.agent` | RECOMMENDED | Agent framework identifier |
| `dev.bento.task` | OPTIONAL | Task description |
| `dev.bento.harness` | RECOMMENDED | Harness that produced this artifact |
| `dev.bento.format.version` | RECOMMENDED | Spec version (e.g., "0.2.0") |

#### 3.4.2 Layer Annotations

| Key | Required | Description |
|---|---|---|
| `org.opencontainers.image.title` | REQUIRED | Layer name (project, agent, deps, etc.) |
| `dev.bento.layer.file-count` | OPTIONAL | Number of files in layer |
| `dev.bento.layer.change-frequency` | OPTIONAL | Hint: "often" or "rarely" |

## 4. Checkpoint DAG

Checkpoints form a directed acyclic graph through the `dev.bento.checkpoint.parent` annotation and the `parentCheckpoint` field in the config object. The value is the digest of the parent checkpoint's manifest.

### 4.1 Linear History

```
cp-1 (parent: none) → cp-2 (parent: cp-1) → cp-3 (parent: cp-2)
```

### 4.2 Branching (Fork)

```
cp-1 → cp-2 → cp-3
                ↘
                 cp-3-alt (parent: cp-2)
```

When forking, the new checkpoint's parent is set to the fork point, not to the latest checkpoint.

### 4.3 DAG Traversal

Implementations SHOULD provide commands to walk the checkpoint DAG (e.g., `bento log`, `bento graph`). The DAG can be reconstructed from manifest annotations alone -- no sidecar database is required.

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

### 6.2 Env File Templates

The config object's `envFiles` section maps `.env` file paths to templates:

```json
{
  "envFiles": {
    ".env": {
      "template": ".env.example",
      "secrets": ["DATABASE_URL", "GITHUB_TOKEN"]
    }
  }
}
```

On restore, implementations SHOULD:
1. Read the template file from the project layer
2. Resolve each referenced secret from the secrets manifest
3. Write the populated `.env` file to disk

The template file (e.g., `.env.example`) is captured in the project layer with placeholder values. The populated `.env` file is excluded from all layers.

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

Additional patterns can be specified in `.bentoignore` and via the harness `Ignore()` method.

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

### 7.4 Harness Default Hooks

Harnesses MAY provide default hooks via the `DefaultHooks()` method. User-defined hooks in `bento.yaml` override harness defaults for the same lifecycle point.

## 8. Harness Interface

A harness maps an agent framework's workspace layout to bento's layer taxonomy.

### 8.1 Go Interface

```go
type Harness interface {
    // Name returns the harness identifier.
    Name() string

    // Detect returns true if this harness is active in the workspace.
    Detect(workDir string) bool

    // Layers returns the layer definitions for this harness.
    Layers() []LayerDef

    // SessionConfig extracts session metadata from the workspace.
    SessionConfig(workDir string) (*SessionConfig, error)

    // Ignore returns additional exclude patterns beyond .bentoignore.
    Ignore() []string

    // SecretPatterns returns patterns that should be flagged
    // if found in content (pre-push safety check).
    SecretPatterns() []string

    // DefaultHooks returns suggested hooks for this agent framework.
    // Users can override any of these in bento.yaml.
    DefaultHooks() map[string]string
}

type LayerDef struct {
    Name      string
    Patterns  []string         // glob patterns for files in this layer
    MediaType string           // OCI media type
    Frequency ChangeFrequency  // ChangesOften | ChangesRarely
}
```

### 8.2 YAML Definition

For harnesses defined without Go code:

```yaml
name: string                    # required
detect: string                  # file/dir that indicates this harness
layers:                         # required, at least one
  - name: string                # required
    patterns: [string]          # required, glob patterns
    media_type: string          # optional, defaults based on name
    frequency: often | rarely   # optional, defaults to "often"
ignore: [string]                # optional, additional exclude patterns
secret_patterns: [string]       # optional, regex patterns for secrets
hooks:                          # optional, default hooks
  pre_save: string
  post_restore: string
  pre_push: string
  post_fork: string
```

### 8.3 Registered Harnesses

The following harness names are reserved for official implementations:

- `claude-code` -- Anthropic Claude Code
- `openclaw` -- OpenClaw
- `opencode` -- OpenCode
- `cursor` -- Cursor
- `codex` -- OpenAI Codex CLI / Desktop
- `github-copilot` -- GitHub Copilot
- `windsurf` -- Windsurf
- `aider` -- Aider
- `docker-agent` -- Docker Agent
- `swe-bench` -- SWE-bench evaluation harness
- `openhands` -- OpenHands (formerly OpenDevin)
- `custom` -- User-defined via YAML

## 9. Store Behavior

### 9.1 Local Store (OCI Image Layout)

The default store is a local OCI image layout directory at `~/.bento/store/`. Each workspace is a subdirectory following the OCI Image Layout specification.

```
~/.bento/store/
├── myproject/
│   ├── oci-layout          # {"imageLayoutVersion": "1.0.0"}
│   ├── index.json          # tags → manifest digests
│   └── blobs/
│       └── sha256/
│           ├── abc123...   # manifests
│           ├── def456...   # config objects
│           └── 789012...   # layer tarballs
└── another-project/
    └── ...
```

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

Glob patterns in harness definitions and `.bentoignore` MUST use forward slashes. Implementations MUST match these patterns against normalized forward-slash paths.

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

Harnesses that need cross-platform hooks should use platform-agnostic commands (e.g., `make`, `npm run`, `go run`) or define platform-specific hooks:

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
- **Agent identity and roles**: Tracking which agent in a multi-agent team created a checkpoint (e.g., investigator vs. coder vs. reviewer). The `dev.bento.agent` annotation provides basic identification but role-based attribution is not specified.
- **Supply chain security**: Signing checkpoints with Sigstore/cosign/Notation and verifying provenance. Bento artifacts are compatible with these tools but this spec does not define signing workflows or trust policies.
- **Context summarization**: Generating compact handoff documents for agents on restore. Agents load their session history directly from the agent layer. Users can add summarization via hooks if needed.

## 17. Future Extensions

The following are anticipated but not yet specified:

- **Encryption**: Client-side encryption of layer content before push
- **Streaming restore**: Progressive layer extraction during restore
- **Garbage collection protocol**: Standardized retention policies and GC triggers
- **Concurrent access protocol**: Locking or conflict resolution for shared workspaces
- **Agent identity and provenance**: Structured agent role metadata and signed checkpoint chains

---

## Appendix A: IANA Media Type Registrations

The following media types are defined by this specification:

### Core types

```
application/vnd.bento.workspace.v1
application/vnd.bento.config.v1+json
application/vnd.bento.layer.project.v1.tar+gzip
application/vnd.bento.layer.agent.v1.tar+gzip
application/vnd.bento.layer.deps.v1.tar+gzip
```

### Well-known custom layer types

```
application/vnd.bento.layer.build-cache.v1.tar+gzip
application/vnd.bento.layer.data.v1.tar+gzip
application/vnd.bento.layer.runtime.v1.tar+gzip
application/vnd.bento.layer.custom.v1.tar+gzip
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

This table documents where major agent frameworks store their state on disk. Harness authors should use this as a reference when defining layer patterns.

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
    "mediaType": "application/vnd.bento.config.v1+json",
    "digest": "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
    "size": 412
  },
  "layers": [
    {
      "mediaType": "application/vnd.bento.layer.project.v1.tar+gzip",
      "digest": "sha256:a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447",
      "size": 131072,
      "annotations": {
        "org.opencontainers.image.title": "project",
        "dev.bento.layer.file-count": "42",
        "dev.bento.layer.change-frequency": "often"
      }
    },
    {
      "mediaType": "application/vnd.bento.layer.agent.v1.tar+gzip",
      "digest": "sha256:b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c",
      "size": 65536,
      "annotations": {
        "org.opencontainers.image.title": "agent",
        "dev.bento.layer.file-count": "8",
        "dev.bento.layer.change-frequency": "often"
      }
    },
    {
      "mediaType": "application/vnd.bento.layer.deps.v1.tar+gzip",
      "digest": "sha256:7d865e959b2466918c9863afca942d0fb89d7c9ac0c99bafc3749504ded97730",
      "size": 93323264,
      "annotations": {
        "org.opencontainers.image.title": "deps",
        "dev.bento.layer.file-count": "1204",
        "dev.bento.layer.change-frequency": "rarely"
      }
    }
  ],
  "annotations": {
    "org.opencontainers.image.created": "2026-03-26T10:00:00Z",
    "org.opencontainers.image.authors": "developer@example.com",
    "dev.bento.checkpoint.sequence": "3",
    "dev.bento.checkpoint.parent": "sha256:abc123def456...",
    "dev.bento.checkpoint.message": "refactored auth module",
    "dev.bento.agent": "claude-code",
    "dev.bento.agent.version": "1.2.3",
    "dev.bento.task": "refactor auth module",
    "dev.bento.harness": "claude-code",
    "dev.bento.format.version": "0.2.0"
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

If `BENTO_WORKSPACE` is not set, the server uses the current working directory. The server reads `bento.yaml` from the workspace root for store and harness configuration.

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
