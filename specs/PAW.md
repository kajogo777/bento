# PAW Protocol Specification

**PAW: Portable Agent Workspace**

**Version:** 0.1.0
**Status:** Draft
**Authors:** [TBD]

## Abstract

The PAW Protocol defines an open standard for packaging, versioning, and transporting AI agent workspace state as OCI (Open Container Initiative) artifacts. A PAW artifact contains semantically typed layers representing different aspects of a workspace (project files, agent state, dependencies) bundled with structured metadata into a standard OCI image manifest.

PAW makes agent workspace state portable, inspectable, efficient, and extensible. Any PAW-compliant tool can produce and consume PAW artifacts. Any OCI registry can store and distribute them.

## 1. The Challenge

AI coding agents accumulate valuable state during a session: installed dependencies, agent memory, conversation history, tool configurations, build caches, learned patterns, and plans. None of this is tracked by git.

When a session ends, this state is lost. When a workspace moves between machines, this state doesn't follow. When an agent hands off to another agent, this state can't be transferred.

The result:

- **Sessions can't resume.** Agents start cold every time. Users re-explain context. Dependencies are reinstalled. Build caches are rebuilt.
- **Workspaces aren't portable.** Moving from a laptop to a cloud VM means rsync, reinstalling deps, and reconfiguring the agent. Moving between cloud providers is worse.
- **Mistakes can't be undone.** An agent that trashes a build cache or goes off the rails leaves no way to roll back beyond file-level undo. Full workspace state (deps, agent memory, caches) has no checkpoint mechanism.
- **Handoffs between agents don't work.** Switching from one agent to another means starting over. The new agent gets the code but not the context, deps, or reasoning.
- **Platforms can't interoperate.** Every sandbox provider (E2B, Fly.io, Modal, Daytona) invents its own persistence mechanism. Workspaces are locked to the provider.

These problems share a root cause: there is no standard format for capturing and transporting agent workspace state.

## 2. Goals

### 2.1 Portable workspace snapshots

Define a format for capturing the complete state of an agent workspace (code, dependencies, agent memory, build caches, tool configs) as a single, self-describing artifact that can be stored, transported, and restored on any machine.

### 2.2 Built on OCI

Use the OCI Image Manifest and OCI Distribution specifications as the foundation. PAW artifacts are valid OCI images. They push and pull with existing tools (docker, crane, skopeo, oras). They work with any OCI registry (ghcr.io, ECR, Artifactory, Harbor). No new registry protocol or infrastructure is needed.

### 2.3 Semantic layers

Organize workspace files into semantic layers (deps, agent, project) rather than opaque filesystem snapshots. Layers have meaning: deps change rarely and are large, agent state changes often and is small, project files are the catch-all. This enables content deduplication across checkpoints, selective restore of individual layers, and meaningful diffs.

### 2.4 Agent-agnostic

The protocol does not require or favor any specific AI agent. A PAW artifact created by one agent (Claude Code, Codex, Cursor, or a custom agent) should be consumable by any other PAW-compliant tool. Each tool reads the layers it understands and ignores the rest.

### 2.5 Runtime-agnostic

The protocol does not require or favor any specific runtime or sandbox. A PAW artifact created locally can be opened in Docker, E2B, Fly.io, or bare metal. The artifact carries its own metadata (OS, arch, extensions, env vars) so the target environment knows what the workspace needs.

### 2.6 Secret-safe by default

Artifacts must never contain secret values unless explicitly opted in. The protocol defines a scrubbing contract and a secret reference model so that workspace snapshots can be shared safely. Actual secret values are resolved on demand from external providers.

### 2.7 Checkpoint history

Artifacts carry parent references, forming a directed acyclic graph (DAG). This enables workspace branching (try two approaches from the same starting point), history traversal, and lineage tracking. Each workspace directory tracks its own position in the DAG independently.

### 2.8 Extensible

New agents, package managers, and tools can participate by defining an extension that contributes file patterns to layers. Extensions are composable, auto-detectable, and merge without conflicts. No central coordination is required.

## 3. Non-Goals

### 3.1 Process execution

PAW is not a container runtime. It does not define how to run processes, isolate workloads, or manage networking. It defines how to snapshot and transport workspace files and metadata.

### 3.2 Real-time collaboration

PAW does not define how multiple agents communicate in real-time. That is the domain of protocols like MCP and ACP. PAW handles the "save, ship, resume" lifecycle, not live coordination.

### 3.3 Agent memory format

PAW does not define how agents store their memory, conversation history, or plans. Each agent has its own format (CLAUDE.md, AGENTS.md, session JSONL files, etc.). PAW captures these files as-is in the agent layer. A future companion RFC (PAW-Sessions) will address session normalization for cross-agent portability.

### 3.4 Encryption and secret handling specifics

PAW requires that secrets MUST NOT appear in plain text in artifact layers. It does not mandate a specific detection method, scrubbing strategy, encryption scheme, or key management approach. These are defined in a companion RFC (PAW-Secrets) with Bento as the reference implementation.

### 3.5 Garbage collection policy

PAW defines that content is addressable and deduplicated. It does not mandate when or how old checkpoints are pruned. Retention policies are implementation-specific.

### 3.6 CLI or UX

PAW defines the artifact format, metadata schema, and protocol semantics. It does not define CLI commands, terminal UI, or editor integrations.

### 3.7 Concurrent access

Multiple agents writing to the same workspace simultaneously is undefined behavior. Use separate workspace directories for parallel agents.

---

## 4. Terminology

**Workspace**: A directory tree containing all files relevant to an agent's task: code, agent memory, dependencies, tool configs, and build artifacts.

**Checkpoint**: An immutable, content-addressed snapshot of a workspace at a point in time, stored as a tagged OCI artifact.

**Layer**: A tar+gzip archive containing a subset of workspace files, categorized by semantic type.

**Extension**: A composable unit that detects a tool, agent, or language in a workspace and contributes file patterns to the layer model.

**Store**: An OCI Target (local OCI layout directory or remote registry) where checkpoints are stored.

**Checkpoint DAG**: The directed acyclic graph formed by parent references between checkpoints, enabling branching and lineage tracking.

**Head**: The digest of a workspace directory's current checkpoint. Tracked per-directory to enable parallel workspaces from the same store.

**Hook**: An optional shell command that runs at a specific lifecycle point (pre-save, post-restore, etc.).

## 5. Relationship to OCI Specifications

PAW artifacts conform to:

- [OCI Image Manifest Specification v1.1](https://github.com/opencontainers/image-spec/blob/main/manifest.md)
- [OCI Distribution Specification v1.1](https://github.com/opencontainers/distribution-spec)
- [OCI Image Layout Specification](https://github.com/opencontainers/image-spec/blob/main/image-layout.md) (for local stores)

PAW uses the standard OCI image manifest with `artifactType` set to `application/vnd.paw.workspace.v1`. It follows the OCI 1.1 convention of reusing image manifests with a typed config descriptor for workspace metadata.

## 6. Artifact Structure

### 6.1 Manifest

A PAW checkpoint is represented as an OCI image manifest. PAW uses standard OCI media types for config and layers, making artifacts natively compatible with Docker, containerd, buildkit, and all OCI tooling. The `artifactType` field and `dev.paw.*` annotations distinguish PAW artifacts from regular container images.

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.paw.workspace.v1",
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
        "dev.paw.layer.file-count": "1204"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:222...",
      "size": 65536,
      "annotations": {
        "org.opencontainers.image.title": "agent",
        "dev.paw.layer.file-count": "8"
      }
    },
    {
      "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
      "digest": "sha256:111...",
      "size": 131072,
      "annotations": {
        "org.opencontainers.image.title": "project",
        "dev.paw.layer.file-count": "42"
      }
    }
  ],
  "annotations": {
    "org.opencontainers.image.created": "2026-03-26T10:00:00Z",
    "dev.paw.checkpoint.message": "refactored auth module",
    "dev.paw.checkpoint.sequence": "3",
    "dev.paw.checkpoint.parent": "sha256:def456...",
    "dev.paw.extensions": "claude-code,node",
    "dev.paw.task": "refactor auth module"
  }
}
```

**Design decision: standard OCI media types.** PAW uses standard OCI types (`application/vnd.oci.image.layer.v1.tar+gzip`, `application/vnd.oci.image.config.v1+json`) because PAW layers are structurally identical to OCI image layers (tar+gzip filesystem archives). This enables native Docker interop: `COPY --from` in Dockerfiles, `docker pull`, and containerd extraction all work without any PAW-specific tooling. Layer semantics are preserved through annotations, and the `artifactType` field identifies PAW artifacts for tools that need to distinguish them from container images.

### 6.2 Config Object

Media type: `application/vnd.oci.image.config.v1+json`

The config object is a valid OCI image config. PAW metadata is stored in `config.Labels` for Docker compatibility, with the full PAW config serialized in the `dev.paw.config` label for lossless round-trip:

```json
{
  "architecture": "amd64",
  "os": "linux",
  "created": "2026-03-26T10:00:00Z",
  "config": {
    "Labels": {
      "dev.paw.extensions": "claude-code,node",
      "dev.paw.checkpoint.sequence": "3",
      "dev.paw.format.version": "0.1.0",
      "dev.paw.config": "{\"schemaVersion\":\"1.0.0\",\"extensions\":[\"claude-code\",\"node\"],\"task\":\"refactor auth module\",\"checkpoint\":3,\"created\":\"2026-03-26T10:00:00Z\"}"
    }
  },
  "rootfs": {
    "type": "layers",
    "diff_ids": ["sha256:333...", "sha256:222...", "sha256:111..."]
  }
}
```

The `dev.paw.config` label contains the full workspace metadata as JSON:

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
  "environment": { "os": "linux", "arch": "amd64" },
  "externalPaths": {
    "home": "/Users/alice",
    "mappings": [...]
  }
}
```

#### 6.2.1 Self-Describing Artifacts

PAW artifacts MUST be self-describing. The config object carries enough information to restore the workspace on a new machine without requiring the original workspace configuration file. This includes: active extensions, task description, git repo state, environment variables, secret references, external path mappings, and the OS/arch the artifact was created on.

When an artifact is opened into a directory that has no existing workspace configuration, implementations MUST generate the configuration from the artifact metadata. OCI handles the structural restoration (layers, digests, content). The PAW config handles the semantic restoration (where external files go, what extensions apply, what env vars are needed).

### 6.3 Layer Types

#### 6.3.1 Core Layers

All layers use the standard OCI media type `application/vnd.oci.image.layer.v1.tar+gzip`. The layer's semantic role is identified by the `org.opencontainers.image.title` annotation.

| Name | Title annotation | Description |
|---|---|---|
| deps | `deps` | Installed packages, virtual environments, build caches, compiled artifacts |
| agent | `agent` | Agent memory, conversation history, plans, skills, commands, session state |
| project | `project` | Source code, tests, build definitions, configs, and any other workspace files |

Layers are ordered from bottom (least-changing) to top (most-changing), following OCI convention. Deps change rarely and are large, so they sit at the bottom for maximum cache reuse. The project layer is a catch-all: any workspace file not matched by agent or deps patterns is captured here.

#### 6.3.2 Custom Layers

Implementations MAY define additional layers beyond the three core layers. Custom layers MUST use the same OCI media type and identify themselves via the `org.opencontainers.image.title` annotation.

Well-known custom layer names:

| Title | Description |
|---|---|
| `build-cache` | Incremental compilation state, webpack cache, .tsbuildinfo |
| `data` | SQLite databases, local data files, seed data |
| `runtime` | Pinned agent CLI binaries and MCP server binaries |

#### 6.3.3 Layer Content Format

Each layer is a gzip-compressed tar archive. Implementations SHOULD preserve timestamps. File permissions and symlinks are handled according to the cross-platform rules in Section 15.

**Workspace files** are stored with paths relative to the workspace root.

**External files** are files that live outside the workspace directory (e.g., agent sessions in the user's home directory). These are stored with paths relative to a virtual `__external__/` prefix within the archive. See Section 6.5 for the full external file specification.

Layers MUST NOT contain:
- Paths containing backslashes (use forward slashes)
- Symlinks pointing outside the archive
- Files matching ignore patterns
- Content matching known secret patterns (see Section 9)
- Content from external paths without proper `__external__/` prefix (see Section 6.5)

#### 6.3.4 Layer Assignment Rules

When a file matches patterns in multiple layers, the first matching layer in the extension definition order wins. The project layer is a catch-all: any workspace file not matched by agent or deps patterns (and not in the ignore list) is captured in the project layer. This ensures no workspace file is silently excluded.

### 6.4 Annotations

PAW uses the `dev.paw.*` annotation namespace on both manifests and layer descriptors.

#### 6.4.1 Manifest Annotations

| Key | Required | Description |
|---|---|---|
| `org.opencontainers.image.created` | REQUIRED | RFC 3339 timestamp |
| `dev.paw.checkpoint.sequence` | REQUIRED | Monotonically increasing checkpoint number |
| `dev.paw.checkpoint.parent` | RECOMMENDED | Digest of parent checkpoint |
| `dev.paw.checkpoint.message` | OPTIONAL | Human-readable description |
| `dev.paw.extensions` | RECOMMENDED | Comma-separated list of active extensions |
| `dev.paw.task` | OPTIONAL | Task description |
| `dev.paw.format.version` | RECOMMENDED | Spec version (e.g., "0.1.0") |

#### 6.4.2 Layer Annotations

| Key | Required | Description |
|---|---|---|
| `org.opencontainers.image.title` | REQUIRED | Layer name (project, agent, deps, or custom) |
| `dev.paw.layer.file-count` | OPTIONAL | Number of files in layer |

### 6.5 External Files

Agent state often lives outside the workspace directory. Claude Code stores sessions in `~/.claude/projects/*/`, Codex in `~/.codex/sessions/`, and so on. PAW must capture these files to make workspaces truly portable.

#### 6.5.1 External Path Patterns

Extension patterns MAY reference files outside the workspace using two prefix forms:

- `~/` resolves to the user's home directory on the save host
- Absolute paths (e.g., `/opt/tools/config`) reference fixed filesystem locations

Examples:
```
~/.claude/projects/*/          # Claude Code sessions
~/.codex/sessions/             # Codex sessions
~/.cache/pip/                  # pip cache
```

#### 6.5.2 Storage Layout in Archives

External files are stored under a virtual `__external__/` prefix in the tar archive. The original absolute path is preserved as a path relative to the filesystem root:

| Original path | Path in archive |
|---|---|
| `/Users/alice/.claude/projects/abc/session.jsonl` | `__external__/Users/alice/.claude/projects/abc/session.jsonl` |
| `/home/bob/.codex/sessions/s1.json` | `__external__/home/bob/.codex/sessions/s1.json` |

#### 6.5.3 Path Mapping Manifest

Because home directories and usernames differ between hosts, the artifact config MUST include an `externalPaths` mapping that records how external paths were resolved at save time:

```json
{
  "externalPaths": {
    "home": "/Users/alice",
    "mappings": [
      {
        "pattern": "~/.claude/projects/*/",
        "resolved": "/Users/alice/.claude/projects/my-project/",
        "archivePath": "__external__/Users/alice/.claude/projects/my-project/"
      }
    ]
  }
}
```

#### 6.5.4 Restore Behavior

On restore, implementations MUST remap external file paths to the target host:

1. Read the `externalPaths.home` value from the artifact config.
2. For each external file, replace the original home directory prefix with the current user's home directory.
3. Create parent directories as needed.
4. Restore files to the remapped paths.

Example: An artifact saved on `/Users/alice` restored on `/home/bob`:
- Archive path: `__external__/Users/alice/.claude/projects/abc/session.jsonl`
- Restored to: `/home/bob/.claude/projects/abc/session.jsonl`

For non-home absolute paths (e.g., `/opt/tools/config`), implementations SHOULD restore to the same absolute path and SHOULD warn if the path is not writable.

#### 6.5.5 Missing External Files

On save, if an external path pattern matches no files (e.g., the agent hasn't created session files yet), the pattern is silently skipped. Implementations SHOULD NOT treat missing external files as errors.

## 7. Checkpoint DAG

Checkpoints form a directed acyclic graph through the `dev.paw.checkpoint.parent` annotation. The value is the digest of the parent checkpoint's manifest.

### 7.1 Head Tracking

Each workspace directory tracks its own position in the DAG via a head reference (the manifest digest of the directory's current checkpoint). On save, the parent is derived from head. After save, head is updated to the new checkpoint's digest. After open, head is updated to the opened checkpoint's digest.

This enables multiple directories to share one store while maintaining independent positions in the DAG.

### 7.2 Linear History

```
cp-1 (parent: none) -> cp-2 (parent: cp-1) -> cp-3 (parent: cp-2)
```

### 7.3 Branching

```
cp-1 -> cp-2 -> cp-3 -> cp-4 (workspace A)
           \-> cp-5 -> cp-6 (workspace B)
```

Branching happens naturally when a checkpoint is opened into a new directory. Both directories share the same store. Each directory's head points to its own position, so saves from each directory get the correct parent. No explicit fork command is needed.

### 7.4 Restorable Open

Before overwriting files, an open operation SHOULD save the current workspace state as a backup checkpoint. The user can undo the last open by restoring this backup. The undo creates its own backup, making it a two-state toggle.

### 7.5 DAG Traversal

Implementations SHOULD provide commands to walk the checkpoint DAG. The DAG can be reconstructed from manifest annotations alone. No sidecar database is required.

### 7.6 Digest References

Checkpoint references may be tags (e.g., `cp-3`) or content digests (e.g., `sha256:abc123...`). Implementations MUST handle both formats.

## 8. Referrers (Attached Artifacts)

PAW uses the OCI 1.1 Referrers API to attach metadata artifacts to checkpoints without mutating them. Attached artifacts reference a checkpoint via the `subject` field in their manifest.

Common attachment types:

| Artifact Type | Use Case |
|---|---|
| `application/vnd.paw.attachment.diff.v1+patch` | Patch/diff showing what changed |
| `application/vnd.paw.attachment.test-results.v1+json` | Test run results |
| `application/vnd.paw.attachment.usage.v1+json` | Token usage / cost report |
| `application/vnd.paw.attachment.log.v1+jsonl` | Agent conversation log |

## 9. Secret Safety

### 9.1 Core Requirement

PAW artifacts MUST NOT contain secret values in plain text in any layer or config field. This is the single non-negotiable rule of the protocol.

How implementations enforce this requirement is flexible. Possible strategies include:

- **Scrubbing**: Replace detected secrets with stable placeholders before packing layers. Store actual values separately (encrypted, local-only, or via a key-wrapping scheme for sharing).
- **Blocking**: Abort the save if secrets are detected, requiring the user to remove them first.
- **Exclusion**: Exclude known secret file patterns from all layers by default.

Implementations SHOULD combine multiple strategies for defense in depth. The specific detection rules, scrubbing format, encryption scheme, and key management approach are defined in the companion RFC (PAW-Secrets), with Bento as the reference implementation.

### 9.2 Secret References

The config object MAY include secret references that store only pointers to external secret sources, never values:

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
    }
  }
}
```

Implementations MAY support any secret provider (env, file, exec, vault, cloud KMS, etc.). The reference schema is extensible.

### 9.3 Default Exclusion Patterns

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

Additional patterns can be specified via ignore files and extensions.

### 9.4 Environment Variables in Manifests

Non-sensitive key-value environment variables MAY be stored in the config object. Secret references are stored separately and contain only provider pointers, never values.

Rule: never store secret values in the config object. Only non-sensitive configuration belongs in the env field.

### 9.5 Secret Sharing

Sharing PAW artifacts between users or machines requires a mechanism for transporting secret values alongside the artifact. The protocol requires that:

- Secret values MUST be encrypted before transport.
- Implementations SHOULD support multi-recipient encryption so that a pushed artifact can be opened by any authorized recipient.
- The encrypted secret envelope MAY be stored as a separate OCI layer or as an out-of-band file. It MUST NOT be included by default; sharing secrets requires explicit opt-in.

The specific encryption scheme, key wrapping protocol, and recipient management are defined in the companion RFC (PAW-Secrets).

## 10. Hooks

Implementations SHOULD support lifecycle hooks: user-defined shell commands that run at key points in the save, restore, and push operations. Hooks allow users to integrate PAW with existing build systems, orchestration tools, and scripts.

The protocol defines the following hook points:

- **pre_save**: Before packing layers. Abort on failure.
- **post_save**: After storing. Warn on failure.
- **post_restore**: After restoring and hydrating secrets. Warn on failure.
- **pre_push**: Before pushing to remote. Abort on failure.
- **post_push**: After pushing to remote. Warn on failure.

Pre-hooks that exit with a non-zero status MUST abort the operation. Post-hooks that exit with a non-zero status MUST NOT abort; implementations SHOULD emit a warning.

Extensions MAY provide default hooks. User-defined hooks override extension defaults for the same lifecycle point.

The full hook specification (timeout behavior, platform-specific overrides, extension hook merging) is defined in the companion RFC (PAW-Hooks).

## 11. Ignore Patterns

Implementations MUST support a workspace ignore file (e.g., `.bentoignore`, `.pawignore`) that lists glob patterns of files to exclude from all layers. The syntax SHOULD follow `.gitignore` conventions:

- One pattern per line
- Lines starting with `#` are comments
- Lines starting with `!` negate a previous pattern
- Patterns are matched against paths relative to the workspace root

The ignore file is combined with the default exclusion patterns (Section 9.3) and any patterns contributed by extensions. The union of all ignore sources determines the final exclusion set.

## 12. Extension Interface

An extension is a composable unit that contributes patterns to the layer model. Each extension has a single concern: an agent framework, a language/framework, or a tool.

### 12.1 Extension Contract

An extension MUST provide:

1. **Name**: A unique identifier (e.g., "claude-code", "node").
2. **Detect**: A function that returns true if the extension is relevant to the workspace.
3. **Contribute**: A function that returns the patterns, layers, ignore rules, and hooks this extension adds.

### 12.2 Contribution Model

An extension contributes:

- **Layer patterns**: File glob patterns mapped to layer names (e.g., `"deps": ["node_modules/**"]`).
- **Extra layers**: New layer definitions beyond the three core layers.
- **Ignore patterns**: Patterns to exclude from all layers.
- **Default hooks**: Lifecycle hooks (overridable by user config).

### 12.3 Merge Rules

When multiple extensions contribute to the same layer, their patterns are unioned. Contributions merge without conflicts. Patterns are deduplicated. Hooks override only if not already set by a higher-priority source (user config > extension default).

### 12.4 Auto-Detection

Implementations SHOULD auto-detect applicable extensions on every save and diff operation. If a user adds a new agent or framework mid-project, it should be picked up automatically.

## 13. Store Behavior

### 13.1 Local Store (OCI Image Layout)

The default store is a local OCI image layout directory. Layer blobs SHOULD be shared across all workspaces via a content-addressed blob pool at the store root. Each workspace retains its own index (tags, manifests). Identical layers across workspaces are stored once.

### 13.2 Remote Store (OCI Registry)

Any OCI 1.1-compliant registry. Implementations SHOULD use existing Docker credential helpers for authentication.

### 13.3 Push/Pull

Push copies artifacts from local to remote using OCI Distribution. Cross-repository blob mounting SHOULD be used when the registry supports it, avoiding redundant uploads.

## 14. Tagging Convention

Implementations SHOULD follow this tagging convention:

- `cp-N`: sequential checkpoint number (e.g., `cp-1`, `cp-2`, `cp-3`)
- `latest`: always points to the most recent checkpoint
- User-defined tags

Tags are mutable references to immutable digests, following standard OCI tag semantics.

## 15. Cross-Platform Behavior

PAW checkpoints are portable across macOS, Linux, and Windows.

### 15.1 Path Handling

All file paths within tar archives MUST use forward slashes, regardless of the platform that created the archive. Implementations MUST normalize backslashes to forward slashes on save and convert to the native separator on restore.

### 15.2 File Permissions

Implementations MUST store POSIX permission bits in tar headers on platforms that support them (Linux, macOS). On restore:

- On Linux/macOS: apply the stored permission bits as-is.
- On Windows: ignore stored permission bits. Apply default ACLs.

When creating archives on Windows, implementations SHOULD write 0644 for regular files and 0755 for directories and executable files.

### 15.3 Symlinks

Implementations MUST store symlinks as symlinks in tar archives. On restore:

- On Linux/macOS: create symlinks as-is.
- On Windows: attempt to create symlinks. If creation fails, silently copy the target instead. This MUST NOT produce an error.

### 15.4 Line Endings

Implementations MUST NOT normalize line endings on save or restore. Files are stored byte-for-byte as they exist on disk.

### 15.5 Case Sensitivity

On save, implementations SHOULD warn if two or more files in the same directory differ only by case. On restore to a case-insensitive filesystem, the last file in archive order wins. Implementations SHOULD warn when this occurs.

### 15.6 Hook Execution

Implementations MUST execute hooks using the platform's native shell:

- Linux/macOS: `sh -c "<command>"`
- Windows: `cmd /c "<command>"`

## 16. Selective Restore

Implementations SHOULD support restoring a subset of layers. This enables fast partial restores when only certain state is needed (e.g., pulling just the agent layer to inspect memory without downloading a multi-gigabyte dependency tree).

## 17. Watch Mode

Implementations MAY provide a watch mode that automatically creates checkpoints as the workspace changes.

### 17.1 Behavior

- Monitor the workspace directory for file-system events.
- Debounce changes: wait until no events have occurred for a configurable quiet period before triggering a checkpoint.
- Create a checkpoint using the same logic as a manual save, including secret scanning and hooks.
- Run until terminated.

### 17.2 Layer Watch Tiers

Different layers warrant different monitoring strategies:

| Layer | Watch method | Rationale |
|---|---|---|
| project | Realtime (fsnotify) | Small, frequent changes |
| deps | Periodic (fingerprint check) | Large directories, avoids FD exhaustion |
| agent | Periodic (fingerprint check) | Moderate size, moderate change frequency |

### 17.3 Retention

Watch mode SHOULD apply tiered retention to auto-checkpoints:

- Full granularity for the last hour
- Hourly for the last 24 hours
- Daily for the last 7 days

## 18. Compatibility

### 18.1 Registry Compatibility

PAW artifacts are standard OCI 1.1 artifacts. They work with any registry that supports:
- OCI Image Manifest v2 schema 2
- `artifactType` field (OCI 1.1)
- Referrers API (for attached artifacts)

### 18.2 Tool Compatibility

PAW artifacts can be inspected and manipulated by any OCI-aware tool:
- `docker manifest inspect`, `docker pull`
- `crane manifest`, `crane pull`
- `skopeo inspect`, `skopeo copy`
- `cosign sign`, `cosign verify`
- `oras discover` (for referrers)
- `COPY --from=<paw-ref>` in Dockerfiles

## 19. Security Considerations

- **Secret scanning**: Pre-store scans are a defense-in-depth measure, not a guarantee. Users are responsible for ensuring sensitive data is excluded via ignore patterns.
- **Registry access control**: PAW inherits the access control model of the underlying registry.
- **Content integrity**: All content is addressed by SHA-256 digest. Implementations SHOULD verify digests on restore.
- **Signing**: PAW artifacts can be signed using Notation or cosign. The protocol does not define signing workflows but is fully compatible with existing OCI signing tools.
- **Env file safety**: Populated `.env` files MUST never be included in any layer.

## 20. Versioning

This specification uses semantic versioning. The format version is tracked in the `dev.paw.format.version` annotation and the config label.

- **Major version**: Breaking changes to the artifact format
- **Minor version**: Backwards-compatible additions (new layer types, annotations)
- **Patch version**: Clarifications, typo fixes

Implementations MUST reject artifacts with an unrecognized major version and SHOULD handle unknown minor-version additions gracefully.

## 21. Out of Scope

The following are explicitly out of scope for this version of the specification:

- **Concurrent access**: Multiple agents saving to the same workspace simultaneously is undefined behavior.
- **Agent identity and roles**: Tracking which agent created a checkpoint in a multi-agent team.
- **Supply chain security**: Signing checkpoints and verifying provenance. PAW artifacts are compatible with cosign/Notation but signing workflows are not specified.
- **Context summarization**: Generating handoff documents for agents on restore.

## 22. Companion RFCs

The following topics are closely related to the PAW Protocol but are specified separately to allow independent iteration. Bento serves as the reference implementation and testbed for these RFCs.

| RFC | Status | Description |
|---|---|---|
| **PAW-Secrets** | Draft | Secret detection, scrubbing, encryption, key wrapping, multi-recipient sharing |
| **PAW-Sessions** | Planned | Agent session metadata normalization for cross-agent portability |
| **PAW-Hooks** | Planned | Full hook specification: timeouts, platform overrides, extension merging |

## 23. Future Extensions

The following are anticipated but not yet specified in any RFC:

- **Streaming restore**: Progressive layer extraction during restore
- **Concurrent access protocol**: Locking or conflict resolution for shared workspaces
- **Agent identity and provenance**: Structured agent role metadata and signed checkpoint chains
- **MCP integration**: Standard MCP tools for agents to create and restore checkpoints programmatically
- **Workspace merging**: Combining changes from divergent branches of the checkpoint DAG

---

## Appendix A: Media Types

### PAW-specific types

```
application/vnd.paw.workspace.v1              (manifest artifactType)
```

### Standard OCI types (used for Docker compatibility)

```
application/vnd.oci.image.config.v1+json      (config)
application/vnd.oci.image.layer.v1.tar+gzip   (all layers)
```

### Attachment types

```
application/vnd.paw.attachment.diff.v1+patch
application/vnd.paw.attachment.test-results.v1+json
application/vnd.paw.attachment.usage.v1+json
application/vnd.paw.attachment.log.v1+jsonl
```

## Appendix B: Relationship to Implementations

PAW is a protocol. Implementations are tools that produce and consume PAW artifacts. The relationship is analogous to OCI (the standard) and Docker (an implementation).

Known implementations:

- **Bento** (github.com/kajogo777/bento): CLI tool for saving, restoring, and pushing PAW artifacts. Includes built-in extensions for Claude Code, Codex, OpenCode, Cursor, and common language ecosystems.
