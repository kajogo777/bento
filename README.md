# 🍱 bento

**Portable agent workspaces. Pack, ship, resume.**

Bento packages AI agent workspace state into portable, layered OCI artifacts. Save a checkpoint of your code, agent memory, and dependencies. Push it to any container registry. Open it anywhere.

Works with any agent. Works on macOS, Linux, and Windows. Works offline. One binary.

```bash
bento init                          # start tracking a workspace
bento save -m "auth module done"    # checkpoint
bento open cp-3                     # restore
bento fork cp-3                     # try a different approach
bento push                          # share via registry
```

## The Problem

AI coding agents checkpoint code via git, but lose everything else when the session ends: installed dependencies, agent memory, tool configurations, build caches, conversation history.

Git tracks your source code. **Bento tracks everything git doesn't.**

## How It Works

Bento decomposes your workspace into semantic layers based on what the files are and how often they change:

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
```

Layers that haven't changed share digests and aren't re-uploaded. Your 200MB `node_modules` is stored once, not once per checkpoint.

Bento artifacts are standard OCI artifacts. They work with any OCI-compatible registry (GHCR, Docker Hub, ECR) and interoperate with `docker`, `crane`, and `cosign`.

## Install

```bash
# macOS / Linux
brew install kajogo777/tap/bento

# Windows
scoop install bento

# From source
go install github.com/kajogo777/bento@latest
```

## Quick Start

```bash
cd my-project
bento init
# Detected agent: claude-code

bento save -m "refactored auth module"
# Scanning workspace...
#   deps:     1204 files, 89MB (unchanged, reusing)
#   agent:    8 files, 64KB (changed)
#   project:  42 files, 128KB (changed)
# Tagged: cp-1, latest

# Keep working, save more checkpoints...
bento save -m "added tests"
# Tagged: cp-2, latest

# Something went wrong? Restore an earlier checkpoint
bento open cp-1

# Try a different approach
bento fork cp-1 -m "trying redis instead"

# Push to a registry
bento push ghcr.io/myorg/workspaces/my-project
```

## Core Concepts

### Checkpoints

Immutable, content-addressed snapshots of your workspace. Checkpoints form a DAG through parent references:

```
cp-1 → cp-2 → cp-3
                ↘
                 cp-4 (forked) → cp-5 → latest
```

### Layers

Three core layers, ordered bottom to top:

| Layer | Contents | Change frequency |
|-------|----------|-----------------|
| **deps** | Installed packages, build caches | Rarely |
| **agent** | Agent memory, plans, skills, commands | Often |
| **project** | Everything else (source, tests, configs, binaries) | Often |

The project layer is a catch-all. Any workspace file not matched by agent or deps patterns is captured here.

Harnesses can define additional custom layers. Unchanged layers are deduplicated automatically.

### Harnesses

A harness maps an agent framework's file layout to bento's layers. Bento auto-detects your agent:

```bash
bento init
# Detected agent: claude-code
```

Multiple agents in the same workspace are combined automatically:

```bash
bento init
# Detected agent: claude-code+codex
```

Use `--harness <name>` to force a single agent.

**Supported:** Claude Code, Codex, Aider, Cursor, Windsurf

Define a custom harness in `bento.yaml` for unsupported agents:

```yaml
harness_config:
  name: my-agent
  detect: ".my-agent/config.json"
  layers:
    - name: deps
      patterns: [".venv/**"]
      frequency: rarely
    - name: agent
      patterns: [".my-agent/**"]
    - name: project
      patterns: ["src/**", "*.py"]
  ignore:
    - "*.log"
  hooks:
    post_restore: "make setup"
```

### Hooks

Optional shell commands at lifecycle points:

```yaml
hooks:
  pre_save: "make clean-temp"
  post_restore: "make setup"
  pre_push: "npm test"
```

Pre-hooks abort the operation on failure. Post-hooks warn but continue.

### Secrets

Bento never stores secrets. It stores references that are resolved at restore time:

```yaml
env:
  NODE_ENV: development

secrets:
  DATABASE_URL:
    source: env
    var: DATABASE_URL
  API_KEY:
    source: file
    path: /run/secrets/api-key

env_files:
  ".env":
    template: ".env.example"    # optional, generates directly if omitted
    secrets: ["DATABASE_URL", "API_KEY"]
```

On `bento open`, env vars and resolved secrets are written to `.env` (0600 permissions, excluded from checkpoints). A pre-save scan catches credentials before they're stored.

## CLI Reference

```
bento init [--task <desc>] [--harness <n>]    Initialize workspace tracking
bento save [-m <message>] [--tag <tag>]       Save a checkpoint
bento open <ref> [<target-dir>]               Restore a checkpoint
bento list                                    List checkpoints
bento diff <ref1> <ref2>                      Compare two checkpoints
bento fork <ref> [-m <message>]               Branch from a checkpoint
bento tag <ref> <new-tag>                     Tag a checkpoint
bento inspect [ref]                           Show metadata and file tree
bento push [<remote>]                         Push to registry
bento gc [--keep-last <n>] [--keep-tagged]    Clean up old checkpoints
bento env show                                Show env vars and secret refs
bento env set <key> <value>                   Set an env var
```

## Configuration

`bento.yaml` at your workspace root:

```yaml
store: ~/.bento/store
remote: ghcr.io/myorg/workspaces
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
    secrets: ["DATABASE_URL"]

ignore:
  - "*.log"
  - "tmp/"

hooks:
  post_restore: "make setup"

retention:
  keep_last: 10
  keep_tagged: true
```

## Artifact Format

Bento artifacts follow the [OCI Image Spec v1.1](https://github.com/opencontainers/image-spec). Each checkpoint is an OCI manifest with typed layer descriptors:

Bento uses standard OCI media types for native Docker compatibility:

| Component | Media Type | Identified by |
|-----------|-----------|--------------|
| Config | `application/vnd.oci.image.config.v1+json` | - |
| All layers | `application/vnd.oci.image.layer.v1.tar+gzip` | `org.opencontainers.image.title` annotation |
| Artifact type | `application/vnd.bento.workspace.v1` | manifest `artifactType` field |

This means `COPY --from=<bento-ref>` works natively in Dockerfiles.

Full format details in [SPEC.md](specs/SPEC.md).

## Architecture

```
├── cmd/bento/            # entrypoint
├── internal/
│   ├── cli/              # cobra commands
│   ├── workspace/        # scanning, layer packing, .bentoignore
│   ├── registry/         # OCI image layout store
│   ├── manifest/         # OCI manifest construction
│   ├── secrets/          # scanning, hydration, .env population
│   ├── harness/          # agent detection and layer definitions
│   ├── hooks/            # lifecycle hook execution
│   └── policy/           # retention and GC
```

## Comparison

| | git | Docker checkpoint | E2B pause | Bento |
|---|---|---|---|---|
| Tracks source code | yes | - | - | yes |
| Tracks agent memory | - | - | yes | yes |
| Tracks dependencies | - | yes | yes | yes |
| Portable | yes | - | - | yes |
| Deduplication | yes | - | - | yes |
| Inspectable | yes | - | - | yes |
| Branching | yes | - | - | yes |
| Works offline | yes | yes | - | yes |
| Open standard | yes | - | - | yes |

## FAQ

**Why not just use git?**
Git doesn't track dependencies, agent memory, build caches, or conversation history. Bento tracks everything git doesn't.

**Why not Docker commit / CRIU?**
Those capture raw process memory: opaque, architecture-dependent, uninspectable. Bento captures semantic file layers you can inspect, diff, and partially restore.

**Why OCI?**
The infrastructure exists. Every cloud runs an OCI registry. No new accounts or tools needed.

**Can I use this without an AI agent?**
Yes. Bento works on any directory.

**Cross-platform?**
Yes. Checkpoints are portable across macOS, Linux, and Windows.

## Roadmap

- [x] Core CLI (init, save, open, list, diff, fork, tag, inspect, gc)
- [x] Local OCI store
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
- [x] Remote registry push/pull
- [ ] Store schemes (`oci://`, `file://`)
- [ ] `bento attach` (OCI referrers for diffs, test results, logs)
- [ ] MCP server (agents checkpoint themselves)
- [ ] `bento watch` (auto-checkpointing)
- [ ] Docker sandbox integration

## License

Apache 2.0. See [LICENSE](LICENSE).
