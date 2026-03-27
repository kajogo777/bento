# 🍱 bento

**Portable agent workspaces. Pack, ship, resume.**

Bento packages AI agent workspace state into portable, layered artifacts -- like a bento box with compartments for your code, your agent's memory, and your dependencies. Save a checkpoint, push it to any container registry, open it anywhere.

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

Every AI coding agent can create a workspace. None of them can save it.

Claude Code, Codex, Aider, Docker Agent -- they all checkpoint code via git, but lose everything else when the session ends: installed dependencies, agent memory, tool configurations, build caches, conversation history. When the sandbox dies, the context dies with it.

Developers report spending 10-20 minutes per session rebuilding context that was lost to compaction or session restarts. A 4-hour auth refactor, a 6-session debugging marathon, a multi-day feature branch -- all reduced to a git diff and a vague memory of what the agent was thinking.

Git tracks your source code. **Bento tracks everything git doesn't.**

## How It Works

Bento decomposes your workspace into **semantic layers** based on what the files are and how often they change:

```
┌───────────────────────────────────────┐
│           🍱 bento artifact           │
├─────────────┬───────────┬─────────────┤
│   project   │   agent   │    deps     │
│             │           │             │
│  your code, │  memory,  │ node_modules│
│  tests,     │  plans,   │ .venv,      │
│  configs    │  history, │ build cache │
│             │  skills   │             │
│  changes    │  changes  │   rarely    │
│  often      │  often    │   changes   │
└─────────────┴───────────┴─────────────┘
         + any custom layers
         you define in bento.yaml
```

Three core layers handle the common case. Harnesses can add more for specific needs (build caches, database snapshots, runtime binaries). You define exactly what gets captured in `bento.yaml`.

Layers that haven't changed between checkpoints share digests and aren't re-uploaded. Your 200MB `node_modules` is stored once, not once per checkpoint. A 5 GB Rust `target/` directory that hasn't changed? Reused across every checkpoint for free.

Bento artifacts are **standard OCI artifacts** -- they work with any OCI-compatible registry (GitHub Container Registry, Docker Hub, Amazon ECR, or a local registry on your laptop) and interoperate with `docker`, `crane`, `cosign`, and the rest of the container ecosystem.

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
#   agent:    8 files, 64KB (changed)
#   project:  42 files, 128KB (changed)
#   deps:     1204 files, 89MB (unchanged, reusing sha256:a1b2...)
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

A point-in-time snapshot of your workspace. Immutable, content-addressed, tagged. Checkpoints form a **DAG** -- each records its parent, so you can trace the full history of how a task was explored.

```
cp-1 → cp-2 → cp-3 (dead end)
                ↘
                 cp-3-alt (new approach) → cp-4 → latest
```

### Layers

Bento defines three **core layers** that cover most workspaces:

| Layer | What's in it | Change frequency |
|-------|-------------|-----------------|
| **project** | Source code, tests, build files, configs you'd commit to git | Every checkpoint |
| **agent** | Agent memory, conversation history, plans, skills, commands | Every checkpoint |
| **deps** | Installed packages, build caches, compiled artifacts | Rarely |

Files that don't match any layer pattern are **excluded by default**. This keeps checkpoints clean -- no OS temp files, no editor swap files, no log noise.

Harnesses can define **additional custom layers** for specific needs. A Rust harness might add a build-cache layer for the `target/` directory. A Next.js harness might split out `.next/cache/`. A database-heavy project might add a data layer for SQLite files. You control the decomposition in `bento.yaml`.

Unchanged layers are **deduplicated** at the registry level -- the OCI content-addressable storage handles this automatically.

### Harnesses

A harness tells bento how a specific agent framework organizes its workspace. Bento auto-detects your agent and applies the right layer decomposition:

```bash
bento init
# Detected agent: claude-code
```

Supported harnesses:

- [x] Claude Code
- [x] Codex
- [x] Aider
- [x] Cursor
- [x] Windsurf
- [ ] OpenClaw
- [ ] OpenCode
- [ ] GitHub Copilot

Don't see your agent? Define a custom harness in `bento.yaml` -- no code required:

```yaml
harness:
  name: my-custom-agent
  detect: ".my-agent/config.json"
  layers:
    - name: project
      patterns: ["src/**", "*.py", "Makefile", "pyproject.toml"]
    - name: agent
      patterns: [".my-agent/**"]
    - name: deps
      patterns: [".venv/**"]
      frequency: rarely
    - name: build-cache
      patterns: [".mypy_cache/**", "__pycache__/**"]
      frequency: rarely
  ignore:
    - "*.log"
    - "*.pyc"
  hooks:
    pre_save: "python -m my_agent flush_state"
    post_restore: "make setup"
```

### Hooks

Bento runs optional shell commands at lifecycle points. You bring your own scripts, Makefiles, compose files -- whatever you already use. Bento just calls them at the right time.

```yaml
hooks:
  pre_save: "make clean-temp"          # tidy up before checkpointing
  post_save: "echo 'saved'"           # notify after save
  post_restore: "make setup"          # reinstall, rebuild, whatever you need
  pre_push: "npm test"                # gate pushes on passing tests
  post_fork: "./scripts/seed-db.sh"   # seed fresh data when forking
```

All optional. If you don't define any, bento does nothing beyond unpacking files and hydrating secrets. Built-in harnesses provide sensible defaults that you can override.

### Stores

Checkpoints live in a **store** -- either a local OCI layout directory on disk or a remote OCI registry. Local by default, no account required.

```yaml
# bento.yaml
store: ~/.bento/store                 # local (default)
remote: ghcr.io/myorg/workspaces      # optional remote
sync: manual                          # or "on-save"
```

```bash
# Explicit store references
bento open oci://~/.bento/store/myproject:cp-3       # local
bento open ghcr.io/myorg/workspaces/myproject:cp-3   # remote
bento open file://~/backups/myproject.tar             # tarball
```

## Secrets

**Bento never stores secrets.** Three tiers:

1. **Plain env vars** -- pushed as-is (`NODE_ENV=development`, `LOG_LEVEL=debug`)
2. **Secret references** -- pointers to where secrets live, resolved at restore time
3. **Excluded** -- `.env.local`, credentials, keys -- never leave your machine

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
    template: ".env.example"           # template captured in project layer
    secrets: ["DATABASE_URL", "GITHUB_TOKEN"]
```

A pre-save secret scan catches anything that looks like a credential before it's pushed. On restore, bento hydrates secret refs from your local backends and populates `.env` files from their templates. The `.env.example` file (with placeholder values) is safely captured. The `.env` file (with real values) is never stored.

## Sandbox Integration

Bento makes ephemeral sandboxes resumable. It hooks into the sandbox lifecycle -- restore at start, checkpoint at stop:

```bash
# Docker sandbox
bento sandbox start --task "fix auth bug"      # creates sandbox, restores workspace
bento sandbox resume auth-bug                   # picks up where you left off

# Or wire it up yourself -- bento just needs two hooks:
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
# Investigator works in sandbox, checkpoints findings
bento sandbox start --task "debug auth" --agent investigator
# → ghcr.io/myorg/ws/auth-bug:investigated

# Fixer picks up in a fresh sandbox
bento sandbox start --resume auth-bug --agent fixer
# → ghcr.io/myorg/ws/auth-bug:fixed
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

Bento includes an MCP server so agents can manage their own checkpoints mid-session. Add it to your agent's MCP config:

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

The agent gets six tools: `bento_save`, `bento_list`, `bento_restore`, `bento_fork`, `bento_diff`, and `bento_inspect`. This lets the agent do things like:

- "This refactor is risky, let me save a checkpoint first"
- "That approach failed, restore to cp-3"
- "I want to try two strategies, let me fork"
- "What did I change since the last checkpoint?"

The agent doesn't need to know about OCI, registries, or layers. It just calls `bento_save` and `bento_restore`. See [Appendix D of the spec](specs/SPEC.md) for the full tool schema.

## Docker & Kubernetes

Everything bento produces is a standard OCI artifact. The container ecosystem just works:

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
| Project layer | `application/vnd.bento.layer.project.v1.tar+gzip` |
| Agent layer | `application/vnd.bento.layer.agent.v1.tar+gzip` |
| Deps layer | `application/vnd.bento.layer.deps.v1.tar+gzip` |

### Well-Known Custom Layers

Harnesses can use these registered types for common additional layers:

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
├── pkg/
│   ├── workspace/        # filesystem scanning, .bentoignore
│   ├── registry/         # oras-go v2 wrapper (Target/Copy)
│   ├── manifest/         # config schema, annotations
│   ├── secrets/          # ref model, hydration, env file templates
│   ├── harness/          # agent adapter interface
│   ├── sandbox/          # Docker/K8s lifecycle hooks
│   └── policy/           # retention, GC
└── harnesses/
    ├── claude-code/
    ├── openclaw/
    ├── opencode/
    ├── cursor/
    ├── codex/
    ├── github-copilot/
    ├── windsurf/
    └── custom/           # YAML-defined
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

Or define one entirely in YAML -- see [Harness Development Guide](docs/harness-dev.md).

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
Those capture raw process memory state -- opaque binary blobs that are architecture-dependent, uninspectable, and fragile. Bento captures semantic file layers that you can inspect, diff, partially restore, and compose.

**Why OCI?**
Because the infrastructure already exists. Every cloud provider runs an OCI registry. Docker Hub, GHCR, ECR, Artifact Registry -- they all speak the same protocol. You don't need new infrastructure, new accounts, or new tools. Your artifacts get signing, scanning, replication, and RBAC for free.

**Does this replace my agent's built-in checkpointing?**
No. Your agent's `/undo` and `/rewind` commands handle file-level rollback during a session. Bento handles workspace-level checkpointing across sessions, machines, and sandboxes.

**Can I use this without an AI agent?**
Yes. Bento works on any directory. It's useful for any workflow where you want to snapshot and restore a working environment.

**Can I checkpoint on Linux and restore on macOS or Windows?**
Yes. Bento handles path separators, file permissions, and symlinks automatically across platforms. Checkpoints are fully portable. See the [spec](specs/SPEC.md) for details.

**What about running services (dev servers, databases)?**
Bento captures filesystem state, not process state. Use the `post_restore` hook to restart services with your existing scripts, Makefile, or docker-compose. Bento doesn't reinvent orchestration.

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

Bento is open source under the Apache 2.0 license. We welcome contributions -- see [CONTRIBUTING.md](CONTRIBUTING.md).

The most impactful contributions right now:
- **Harness adapters** for agent frameworks you use
- **Testing** against real-world workspaces
- **Feedback** on the artifact format before we lock it in

## License

Apache 2.0 -- see [LICENSE](LICENSE).

---

<p align="center">
  <strong>Your workspace, neatly packed. 🍱</strong>
</p>
