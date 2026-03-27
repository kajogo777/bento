# 🍱 bento

**Portable agent workspaces. Pack, ship, resume.**

Bento packages AI agent workspace state into portable, layered OCI artifacts. Save a checkpoint of your code, agent memory, and dependencies. Push it to any container registry. Open it anywhere.

Works with any agent. Works with any sandbox. Works on macOS, Linux, and Windows. Works offline. One binary.

```bash
bento init                          # start tracking a workspace
bento save -m "auth module done"    # pack it up
bento open myproject:cp-3           # open it later
bento fork cp-3                     # try a different approach
bento push                          # share it
```

---

## The Problem

AI coding agents checkpoint code via git, but lose everything else when the session ends: installed dependencies, agent memory, tool configurations, build caches, conversation history.

Git tracks your source code. **Bento tracks everything git doesn't.**

## How It Works

Bento decomposes your workspace into **semantic layers** based on what the files are and how often they change:

```
┌───────────────────────────────────────┐
│           🍱 bento artifact           │
├─────────────┬───────────┬─────────────┤
│    deps     │   agent   │   project   │
│             │           │             │
│ node_modules│  memory,  │  your code, │
│ .venv,      │  plans,   │  tests,     │
│ build cache │  history, │  configs    │
│             │  skills   │             │
│   rarely    │  changes  │  changes    │
│   changes   │  often    │  often      │
└─────────────┴───────────┴─────────────┘
         + any custom layers
         you define in bento.yaml
```

Three core layers handle the common case. Harnesses can add more for specific needs (build caches, database snapshots, runtime binaries). You define what gets captured in `bento.yaml`.

Layers that haven't changed between checkpoints share digests and aren't re-uploaded. Your 200MB `node_modules` is stored once, not once per checkpoint.

Bento artifacts are standard OCI artifacts. They work with any OCI-compatible registry (GitHub Container Registry, Docker Hub, Amazon ECR, or a local registry) and interoperate with `docker`, `crane`, `cosign`, and the rest of the container ecosystem.

## Install

```bash
# macOS / Linux
brew install bentoci/tap/bento

# Windows
scoop install bento
# or
winget install bentoci.bento

# From source
go install github.com/bentoci/bento@latest

# Or grab a binary
curl -fsSL https://bento.dev/install.sh | sh
```

## Quick Start

```bash
# 1. Initialize in any project directory
cd my-project
bento init
# Detected agent: claude-code
# Created bento.yaml
# Store: ~/.bento/store (local)

# 2. Do some work with your agent...

# 3. Save a checkpoint
bento save -m "refactored auth module"
# Scanning workspace...
#   deps:     1204 files, 89MB (unchanged, reusing sha256:a1b2...)
#   agent:    8 files, 64KB (changed)
#   project:  42 files, 128KB (changed)
# Secret scan: clean
# Tagged: cp-1, latest

# 4. Keep working, save more checkpoints...
bento save -m "added tests"
# Tagged: cp-2, latest

# 5. Something went wrong? Open an earlier checkpoint
bento open cp-1

# 6. Want to try a different approach?
bento fork cp-1 -m "trying redis instead"

# 7. Ready to share? Push to a registry
bento push ghcr.io/myorg/workspaces/my-project
```

## Core Concepts

### Checkpoints

A point-in-time snapshot of your workspace. Immutable, content-addressed, tagged. Checkpoints form a DAG, each records its parent so you can trace the full history.

```
cp-1 → cp-2 → cp-3 (dead end)
                ↘
                 cp-3-alt (new approach) → cp-4 → latest
```

### Layers

Bento defines three core layers, ordered bottom to top in the OCI artifact:

| Layer | What's in it | Change frequency |
|-------|-------------|-----------------|
| **deps** | Installed packages, build caches, compiled artifacts | Rarely |
| **agent** | Agent memory, conversation history, plans, skills, commands | Every checkpoint |
| **project** | Source code, tests, build files, configs, and all other workspace files | Every checkpoint |

The project layer is a catch-all: any file not matched by agent or deps patterns is captured here. Nothing is silently excluded.

Harnesses can define additional custom layers for specific needs. Unchanged layers are deduplicated at the registry level.

### Harnesses

A harness tells bento how a specific agent framework organizes its workspace. Bento auto-detects your agent and applies the right layer decomposition:

```bash
bento init
# Detected agent: claude-code
```

If multiple agents are detected, bento combines them:

```bash
bento init
# Detected agent: claude-code+codex
```

Use `--harness <name>` to force a single agent.

Supported harnesses:

- [x] Claude Code
- [x] Codex
- [x] Aider
- [x] Cursor
- [x] Windsurf
- [ ] OpenClaw
- [ ] OpenCode
- [ ] GitHub Copilot

Don't see your agent? Define a custom harness in `bento.yaml`:

```yaml
harness:
  name: my-custom-agent
  detect: ".my-agent/config.json"
  layers:
    - name: deps
      patterns: [".venv/**"]
      frequency: rarely
    - name: agent
      patterns: [".my-agent/**"]
    - name: project
      patterns: ["src/**", "*.py", "Makefile", "pyproject.toml"]
  ignore:
    - "*.log"
    - "*.pyc"
  hooks:
    pre_save: "python -m my_agent flush_state"
    post_restore: "make setup"
```

### Hooks

Bento runs optional shell commands at lifecycle points:

```yaml
hooks:
  pre_save: "make clean-temp"
  post_save: "echo 'saved'"
  post_restore: "make setup"
  pre_push: "npm test"
  post_fork: "./scripts/seed-db.sh"
```

All optional. If you don't define any, bento just unpacks files and hydrates secrets. Built-in harnesses provide sensible defaults that you can override.

### Stores

Checkpoints live in a store, either a local OCI layout directory or a remote OCI registry. Local by default, no account required.

```yaml
# bento.yaml
store: ~/.bento/store
remote: ghcr.io/myorg/workspaces
sync: manual
```

```bash
# Explicit store references
bento open oci://~/.bento/store/myproject:cp-3       # local
bento open ghcr.io/myorg/workspaces/myproject:cp-3   # remote
bento open file://~/backups/myproject.tar             # tarball
```

## Secrets

**Bento never stores secrets.** Three tiers:

1. **Plain env vars** pushed as-is (`NODE_ENV=development`, `LOG_LEVEL=debug`)
2. **Secret references** are pointers to where secrets live, resolved at restore time
3. **Excluded** files like `.env.local`, credentials, keys never leave your machine

```yaml
# bento.yaml
env:
  NODE_ENV: development
  LOG_LEVEL: debug

secrets:
  DATABASE_URL:
    source: vault
    path: secret/data/myapp/db
    key: url
  GITHUB_TOKEN:
    source: env
    var: GITHUB_TOKEN

env_files:
  ".env":
    template: ".env.example"
    secrets: ["DATABASE_URL", "GITHUB_TOKEN"]
```

A pre-save secret scan catches credentials before they're pushed. On restore, bento hydrates secret refs from your local backends and populates `.env` files from their templates.

## Sandbox Integration

Bento makes ephemeral sandboxes resumable:

```bash
# Docker sandbox
bento sandbox start --task "fix auth bug"
bento sandbox resume auth-bug

# Or wire it up yourself:
#   on start:  bento open <ref> --target /workspace
#   on stop:   bento save --dir /workspace
```

### Periodic Checkpointing (Sidecar)

```yaml
# docker-compose.yaml
services:
  agent:
    image: ghcr.io/myorg/agent-runner:latest
    volumes: [workspace:/workspace]

  checkpointer:
    image: ghcr.io/bentoci/bento:latest
    command: ["bento", "watch", "--dir", "/workspace", "--interval", "5m"]
    volumes: [workspace:/workspace]

volumes:
  workspace:
```

### Multi-Agent Handoff

```bash
# Investigator checkpoints findings
bento sandbox start --task "debug auth" --agent investigator

# Fixer picks up in a fresh sandbox
bento sandbox start --resume auth-bug --agent fixer
```

### Parallel Exploration

```bash
# Fork from the same checkpoint, try different approaches
bento sandbox start --resume auth-bug:cp-3 --tag approach-a &
bento sandbox start --resume auth-bug:cp-3 --tag approach-b &

# Compare and promote the winner
bento diff auth-bug:approach-a auth-bug:approach-b
bento tag auth-bug:approach-b auth-bug:latest
```

## Agents Using Bento

Bento includes an MCP server so agents can manage their own checkpoints mid-session:

```json
{
  "mcpServers": {
    "bento": {
      "command": "bento",
      "args": ["mcp-server"]
    }
  }
}
```

The agent gets six tools: `bento_save`, `bento_list`, `bento_restore`, `bento_fork`, `bento_diff`, and `bento_inspect`. See [Appendix D of the spec](specs/SPEC.md) for the full tool schema.

## Docker & Kubernetes

Everything bento produces is a standard OCI artifact:

```bash
# Inspect with docker
docker manifest inspect ghcr.io/myorg/ws/myproject:cp-3

# Copy between registries with crane
crane copy ghcr.io/myorg/ws/myproject:cp-3 ecr.aws/myorg/ws/myproject:cp-3

# Sign with cosign
cosign sign ghcr.io/myorg/ws/myproject:cp-3
```

### Kubernetes Init Container

```yaml
initContainers:
  - name: workspace-init
    image: ghcr.io/bentoci/bento:latest
    command: ["bento", "open", "ghcr.io/myorg/ws/myproject:latest", "--target=/workspace"]
containers:
  - name: agent
    image: ghcr.io/myorg/claude-code-runner:latest
    workingDir: /workspace
  - name: checkpointer
    image: ghcr.io/bentoci/bento:latest
    command: ["bento", "watch", "--dir=/workspace", "--interval=5m"]
```

## CLI Reference

```
bento init [--task <desc>] [--harness <n>]      Initialize workspace tracking
bento save [-m <message>] [--tag <tag>]            Save a checkpoint
bento open <ref> [<target-dir>]                    Restore a checkpoint
bento list                                         List checkpoints
bento diff <ref1> <ref2>                           Compare two checkpoints
bento fork <ref> [-m <message>]                    Branch from a checkpoint
bento tag <ref> <new-tag>                          Tag a checkpoint
bento inspect <ref> [--referrers]                  Show checkpoint metadata & layers
bento attach <ref> --type <media-type> <file>      Attach artifacts to a checkpoint
bento push [<remote>]                              Push local checkpoints to registry
bento gc [--keep-last <n>] [--keep-tagged]         Clean up old checkpoints
bento env show                                     Show tracked env vars & secret refs
bento env set <key> <value>                        Set an env var
bento watch --dir <path> [--interval <duration>]   Auto-checkpoint on changes
bento sandbox start [--task | --resume] [--agent]  Create/resume a sandbox
```

## Configuration

`bento.yaml` at your workspace root:

```yaml
store: ~/.bento/store
remote: ghcr.io/myorg/workspaces
sync: manual
harness: auto
task: "refactor auth module"

env:
  NODE_ENV: development

secrets:
  DATABASE_URL:
    source: vault
    path: secret/data/myapp/db
    key: url

env_files:
  ".env":
    template: ".env.example"
    secrets: ["DATABASE_URL"]

ignore:
  - "*.log"
  - "tmp/"
  - ".DS_Store"

hooks:
  post_restore: "make setup"

retention:
  keep_last: 10
  keep_tagged: true
```

## The Bento Artifact Format

Bento artifacts follow the [OCI Image Spec v1.1](https://github.com/opencontainers/image-spec) with custom media types.

### Core Layers

| Component | Media Type |
|-----------|-----------|
| Manifest config | `application/vnd.bento.config.v1+json` |
| Deps layer | `application/vnd.bento.layer.deps.v1.tar+gzip` |
| Agent layer | `application/vnd.bento.layer.agent.v1.tar+gzip` |
| Project layer | `application/vnd.bento.layer.project.v1.tar+gzip` |

### Well-Known Custom Layers

| Component | Media Type |
|-----------|-----------|
| Build cache | `application/vnd.bento.layer.build-cache.v1.tar+gzip` |
| Data (SQLite, etc.) | `application/vnd.bento.layer.data.v1.tar+gzip` |
| Runtime (pinned agent CLI) | `application/vnd.bento.layer.runtime.v1.tar+gzip` |
| Custom (anything else) | `application/vnd.bento.layer.custom.v1.tar+gzip` |

### Attachments (via OCI Referrers)

| Component | Media Type |
|-----------|-----------|
| Diff/patch | `application/vnd.bento.attachment.diff.v1+patch` |
| Test results | `application/vnd.bento.attachment.test-results.v1+json` |
| Usage report | `application/vnd.bento.attachment.usage.v1+json` |

The manifest config contains session metadata:

```json
{
  "schemaVersion": "1.0.0",
  "agent": "claude-code",
  "task": "refactor auth module",
  "sessionId": "abc123",
  "parentCheckpoint": "sha256:def456...",
  "checkpoint": 3,
  "created": "2026-03-26T10:00:00Z",
  "status": "paused"
}
```

Full format details in [SPEC.md](specs/SPEC.md).

## Architecture

```
bento CLI (Go)
├── cmd/                  # cobra commands
├── internal/
│   ├── cli/              # command definitions
│   ├── workspace/        # filesystem scanning, .bentoignore
│   ├── registry/         # OCI image layout store
│   ├── manifest/         # config schema, annotations
│   ├── secrets/          # ref model, hydration, env file templates
│   ├── harness/          # agent adapter interface
│   ├── hooks/            # lifecycle hook execution
│   ├── mcp/              # MCP server (stdio JSON-RPC)
│   └── policy/           # retention, GC
```

Built on [`oras-go v2`](https://github.com/oras-project/oras-go), the official CNCF Go library for OCI registry operations.

## Harness Development

To add support for a new agent framework, implement the `Harness` interface:

```go
type Harness interface {
    Name() string
    Detect(workDir string) bool
    Layers() []LayerDef
    SessionConfig(workDir string) (*SessionConfig, error)
    Ignore() []string
    SecretPatterns() []string
    DefaultHooks() map[string]string
}
```

Or define one entirely in YAML. See [Harness Development Guide](specs/harness-dev.md).

## Comparison

| Capability | git | Docker checkpoint | E2B pause | Fly.io Sprites | Bento |
|---|---|---|---|---|---|
| Tracks source code | yes | - | - | - | yes |
| Tracks agent memory | - | - | yes | yes | yes |
| Tracks environment | - | yes | yes | yes | yes |
| Portable across platforms | yes | - | - | - | yes |
| Content deduplication | yes | - | - | - | yes |
| Inspectable / diffable | yes | - | - | - | yes |
| Branching / forking | yes | - | - | yes | yes |
| Works with any registry | - | - | - | - | yes |
| Works offline | yes | yes | - | - | yes |
| Open standard | yes | - | - | - | yes |

## FAQ

**Why not just use git?**
Git tracks source code. It doesn't track installed dependencies, agent memory, tool configurations, build caches, or conversation history. `node_modules` is in your `.gitignore` for good reason. Bento tracks everything git doesn't.

**Why not use Docker commit / CRIU?**
Those capture raw process memory state: opaque binary blobs that are architecture-dependent, uninspectable, and fragile. Bento captures semantic file layers that you can inspect, diff, partially restore, and compose.

**Why OCI?**
The infrastructure already exists. Every cloud provider runs an OCI registry. Docker Hub, GHCR, ECR, Artifact Registry all speak the same protocol. No new infrastructure, accounts, or tools needed.

**Does this replace my agent's built-in checkpointing?**
No. Your agent's `/undo` and `/rewind` commands handle file-level rollback during a session. Bento handles workspace-level checkpointing across sessions, machines, and sandboxes.

**Can I use this without an AI agent?**
Yes. Bento works on any directory.

**Can I checkpoint on Linux and restore on macOS or Windows?**
Yes. Bento handles path separators, file permissions, and symlinks across platforms. See the [spec](specs/SPEC.md) for details.

**What about running services (dev servers, databases)?**
Bento captures filesystem state, not process state. Use the `post_restore` hook to restart services.

## Roadmap

- [x] Design spec and artifact format
- [x] Core CLI (`init`, `save`, `open`, `list`, `diff`, `fork`, `tag`, `inspect`, `gc`)
- [x] Local OCI layout store
- [x] Secret scanning and hydration
- [x] Harnesses:
  - [x] Claude Code
  - [x] Codex
  - [x] Aider
  - [x] Cursor
  - [x] Windsurf
  - [ ] OpenClaw
  - [ ] OpenCode
  - [ ] GitHub Copilot
- [ ] Remote registry push/pull
- [ ] MCP server (agents checkpoint themselves)
- [ ] Docker Sandbox integration
- [ ] `bento watch` (sidecar auto-checkpointing)
- [ ] Kubernetes init container image
- [ ] Web UI for browsing checkpoints

## Contributing

Bento is open source under the Apache 2.0 license. See [CONTRIBUTING.md](CONTRIBUTING.md).

The most impactful contributions right now:
- **Harness adapters** for agent frameworks you use
- **Testing** against real-world workspaces
- **Feedback** on the artifact format

## License

Apache 2.0. See [LICENSE](LICENSE).
