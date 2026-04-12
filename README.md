# 🍱 bento

> Portable checkpoints for AI agent workspaces. Save code, agent memory, and dependencies as OCI artifacts. Push to any container registry. Resume anywhere.

<!-- Keywords: AI agent, workspace checkpoint, OCI artifact, container registry, Claude Code, Codex, Cursor, OpenCode, developer tools, CLI, portable workspace, agent memory, session state -->

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.25+-00ADD8.svg)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/kajogo777/bento)](https://github.com/kajogo777/bento/releases)

**Pack, ship, resume.**

![bento demo](docs/demo.gif)

Git tracks your code. Bento tracks everything else: agent memory, installed dependencies, build caches, conversation history. Checkpoints are standard OCI images you can push to any container registry.

One binary. Any agent. macOS, Linux, Windows. Works offline.

```bash
bento init                          # start tracking
bento save -m "auth module done"    # checkpoint
bento open cp-3                     # restore
bento push ghcr.io/myorg/project    # push (remembers remote)
bento pull                          # sync from remote
bento status                        # what's changed? am I in sync?
```

## Install

```bash
# Homebrew
brew tap kajogo777/bento https://github.com/kajogo777/bento
brew install kajogo777/bento/bento

# From source
go install github.com/kajogo777/bento@latest
```

> A different `bento` package exists in homebrew-core. Always use `kajogo777/bento/bento`.

## Quick Start

```bash
cd my-project
bento init
# Detected: claude-code, node

bento save -m "refactored auth module"
#   deps:     1204 files, 89MB (unchanged, reusing)
#   agent:    8 files, 64KB
#   project:  42 files, 128KB
# Tagged: cp-1

bento open cp-1                     # restore any checkpoint
bento open undo                     # changed your mind? undo it
bento open cp-1 ~/workspace-b      # parallel workspace
```

## Move Between Machines

```bash
# Laptop: leaving
bento save -m "moving to server"
bento push ghcr.io/me/myproject       # first push remembers the remote

# Server: cold start
bento open ghcr.io/me/myproject:latest ~/myproject
cd ~/myproject                        # remote is remembered here too
bento save -m "done on server"
bento push                            # no URL needed

# Laptop: back
bento status                          # "1 checkpoint behind"
bento pull
bento open latest
```

## Undo Agent Mistakes

```bash
bento watch --debounce 5              # auto-checkpoints as you work

# Agent went off the rails? Roll back.
bento open cp-12
# To undo: bento open undo

# Restore just the deps layer, keep your code
bento open cp-12 --layers deps
```

## How It Works

Each save creates a layered OCI artifact:

| Layer | Contents | Changes |
|-------|----------|---------|
| **deps** | `node_modules`, `.venv`, `vendor` | Rarely |
| **agent** | Agent memory, plans, skills, configs | Often |
| **project** | Everything else | Often |

Unchanged layers are deduplicated. Your 200MB `node_modules` is stored once, not once per checkpoint.

Bento auto-detects what's in your workspace and assigns files to the right layer:

**Agents:** Claude Code · Codex · OpenCode · OpenClaw · Cursor · Stakpak · Pi · AGENTS.md
**Deps:** Node · Python · Go · Rust · Ruby · Elixir · OCaml
**Tools:** asdf / mise

No configuration needed. Add a new agent or framework mid-project and bento picks it up on the next save.

## Secrets

Bento never stores secrets. A pre-save [gitleaks](https://github.com/zricethezav/gitleaks) scan (~200 rules) catches credentials before they're packed. Environment variables can reference external sources:

```yaml
env:
  DATABASE_URL:
    source: env
    var: DATABASE_URL
```

## More Use Cases

- **[Hand off between agents](docs/tutorials/hand-off-between-agents.md)** Save with Claude Code, open with Cursor
- **[Parallel exploration](docs/tutorials/parallel-exploration.md)** Fork a checkpoint, try different approaches
- **[Warm-start CI](docs/tutorials/warm-start-ci.md)** Pull a checkpoint instead of cold-installing deps
- **[Share workspaces](docs/tutorials/share-workspaces.md)** Push a checkpoint, teammate pulls the exact state
- **[Workspace templates](docs/tutorials/workspace-templates.md)** Publish starter checkpoints for new projects
- **[Portable sandboxes](docs/tutorials/portable-sandboxes.md)** Save in E2B, open in Docker, push to Fly.io
- **[Audit agent work](docs/tutorials/audit-agent-work.md)** `bento diff cp-3 cp-5` across code, deps, and agent state

## Comparison

Agent checkpoints (Claude Code, Cursor) save code changes via git or internal diffs. Cloud agent platforms run stateful sessions but lock you into their infrastructure. Bento captures the full workspace as a portable artifact.

| | Agent checkpoints | Cloud agents* | git | Docker checkpoint | Sandbox snapshots** | Bento |
|---|---|---|---|---|---|---|
| Source code | ✓ | ✓ | ✓ | | | ✓ |
| Agent memory | | ✓ | | | ✓ | ✓ |
| Dependencies | | ✓ | | ✓ | ✓ | ✓ |
| Portable | | | ✓ | | | ✓ |
| Inspectable | | | ✓ | | | ✓ |
| Branching | | | ✓ | | | ✓ |
| Works with any agent | | | ✓ | | | ✓ |
| Docker interop | | | | ✓ | | ✓ |
| Works offline | ✓ | | ✓ | ✓ | | ✓ |

\*Claude Managed Agents (Anthropic) runs stateful sessions with persistent filesystems on Anthropic's cloud.
\*\*E2B, Vercel, and Daytona offer sandbox snapshots within their platform but snapshots are not portable across providers.

## FAQ

**Why not git?** Git doesn't track dependencies, agent memory, or build caches.

**Why not Docker commit?** Opaque process memory dumps. Bento captures semantic file layers you can inspect and diff.

**Why OCI?** Every cloud runs a registry. No new accounts needed. `COPY --from=<bento-ref>` works in Dockerfiles.

## Learn More

- [CLI reference](docs/cli.md) All commands and flags
- [Configuration](docs/configuration.md) `bento.yaml` reference
- [Artifact format](specs/SPEC.md) OCI spec details
- [Architecture](AGENTS.md) Internals and extension system
- [Roadmap](#roadmap)

## Roadmap

- [x] Core CLI, local store, deduplication
- [x] Remote push/pull with status sync
- [x] Secret scanning and hydration
- [x] Watch mode (auto-checkpointing)
- [x] Agents: Claude Code, Codex, OpenCode, OpenClaw, Cursor, Stakpak, Pi
- [ ] GitHub Copilot
- [ ] MCP server (agents checkpoint themselves)
- [ ] Docker sandbox integration

## License

Apache 2.0. See [LICENSE](LICENSE).
