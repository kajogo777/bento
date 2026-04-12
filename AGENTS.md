# AGENTS.md - Bento Development Guide

## Project Overview

Bento packages AI agent workspace state into portable, layered OCI artifacts. It captures everything git doesn't: agent memory, installed dependencies, build caches, conversation history, and session state. Checkpoints are stored as standard OCI images in any container registry.

**Repository:** `github.com/kajogo777/bento`
**Language:** Go (1.25+)
**License:** Apache 2.0

## Architecture

```
cmd/bento/main.go           → entrypoint, version injection
internal/
  cli/                       → cobra commands (init, save, open, list, diff, etc.)
  config/                    → bento.yaml parsing, validation, platform defaults
  extension/                 → composable extensions (agent, deps, tool detection)
  workspace/                 → file scanning, layer packing (tar+gzip), .bentoignore
  registry/                  → OCI store (local image layout + remote push)
  manifest/                  → OCI manifest/config construction, annotations, DAG
  secrets/                   → secret scanning (gitleaks), env hydration, providers, scrubbing, backends
  hooks/                     → lifecycle hook execution (pre_save, post_restore, etc.)
  policy/                    → garbage collection (retention tiers, blob pruning)
  watcher/                   → file-system watcher for auto-checkpointing
```

### Key Design Decisions

1. **Standard OCI media types** - All layers use `application/vnd.oci.image.layer.v1.tar+gzip` so `docker pull`, `COPY --from`, and containerd work natively. Layer semantics are carried by `org.opencontainers.image.title` annotations.

2. **Shared blob store** - Local OCI store at `~/.bento/store/` uses a shared content-addressed blob pool. Identical layers across workspaces are stored once.

3. **Composable extensions** - Each agent framework, language, and tool gets a small extension that contributes patterns to the right layer. Extensions auto-detect and merge. No monolithic harnesses.

4. **Three core layers** - deps (rarely changes, large), agent (changes often, small), project (catch-all). Extensions can add more layers (e.g., `build-cache`).

5. **Secret safety** - Pre-save scanning via [gitleaks](https://github.com/zricethezav/gitleaks) (~200+ rules), automatic scrubbing of detected secrets (replaced with unique placeholders in OCI layers), secrets stored locally and encrypted with a one-time key per checkpoint, `.gitleaksignore` for false positives, SHA256 scan cache, credential file exclusion. Key is shown at push/export time, not save time. See `specs/secret-scrubbing.md` for the full design.

## Extension System

Extensions are the core abstraction. Each extension has one concern and implements three methods:

```go
type Extension interface {
    Name() string                          // e.g., "claude-code", "node", "python"
    Detect(workDir string) bool            // check if relevant to this workspace
    Contribute(workDir string) Contribution // return patterns, ignore, hooks
}

type Contribution struct {
    Layers      map[string][]string // layer name → patterns to add
    ExtraLayers []LayerDef          // new layers (e.g., "build-cache")
    Ignore      []string            // patterns to exclude
    Hooks       map[string]string   // default lifecycle hooks
}
```

On every `save`/`diff`/`watch`, all extensions are auto-detected, their contributions are merged into a unified set of layer definitions, and the workspace is scanned against those patterns.

### Built-in Extensions

**Agent extensions** (contribute to `agent` layer):

| Extension | Detects | Patterns |
|-----------|---------|----------|
| `claude-code` | `.claude/` or `CLAUDE.md` | `CLAUDE.md`, `CLAUDE.local.md`, `.claude/**`, `.mcp.json`, `.worktreeinclude`, `~/.claude/projects/<path-with-dashes>/`, `~/.claude/{CLAUDE.md, settings.json, keybindings.json, rules/, skills/, commands/, agents/, agent-memory/, output-styles/}`, `~/.claude.json` |
| `codex` | `.codex/` | `.codex/**`, `~/.codex/sessions/` (workspace-scoped), `~/.codex/state_N.sqlite` (global), `~/.codex/memories/`, `~/.codex/{AGENTS.md, config.yaml, config.json}` |
| `opencode` | `.opencode/` or `opencode.json` | `.opencode/**`, `opencode.json`, `~/.local/share/opencode/opencode.db` (global), `~/.local/share/opencode/storage/` (legacy), `~/.config/opencode/commands/`, `~/.opencode/commands/` |
| `openclaw` | `SOUL.md` or `IDENTITY.md` | `SOUL.md`, `MEMORY.md`, `memory/**`, `skills/**`, `canvas/**`, `~/.openclaw/openclaw.json`, `~/.openclaw/agents/<id>/sessions/`, `~/.openclaw/workspace/skills/` |
| `cursor` | `.cursor/` or `.cursorrules` | `.cursor/rules/**`, `.cursor/mcp.json`, `.cursorrules`, `.cursorignore`, `~/.cursor/mcp.json`, `~/Library/.../Cursor/User/workspaceStorage/<hash>/` |
| `stakpak` | `.stakpak/` | `.stakpak/config.toml`, `.stakpak/session/**`, `~/.stakpak/data/local.db`, `~/.stakpak/checks/`, `~/.stakpak/triggers/`, `~/.stakpak/sessions/` |
| `pi` | `.pi/` | `.pi/**`, `~/.pi/agent/{settings.json, auth.json}`, `~/.pi/agent/{extensions/, skills/, prompts/, themes/}`, `~/.pi/agent/sessions/<hash>/`, `~/.agents/skills/` |
| `agents-md` | `AGENTS.md` | `AGENTS.md` |

**Deps extensions** (contribute to `deps` layer):

| Extension | Detects | Patterns |
|-----------|---------|----------|
| `node` | `package.json`, `bun.lockb`, `bun.lock`, `bunfig.toml`, `deno.json`, `deno.jsonc`, `deno.lock` | `node_modules/**`, `vendor/**` (deno only) |
| `python` | `pyproject.toml`, `requirements*.txt`, `.venv/`, `Pipfile`, `setup.py`, `uv.lock`, `.python-version` | `.venv/**` |
| `go-mod` | `go.mod` | `vendor/**` |
| `rust` | `Cargo.toml` | `target/**` (as extra `build-cache` layer) |
| `ruby` | `Gemfile`, `Gemfile.lock`, `Rakefile`, `*.gemspec` | `vendor/bundle/**`, `.bundle/**` |
| `elixir` | `mix.exs` | `deps/**`, `_build/**` (as extra `build-cache` layer) |
| `ocaml` | `dune-project`, `dune-workspace`, `*.opam` | `_opam/**`, `_esy/**`, `_build/**` (as extra `build-cache` layer) |

**Tool extensions** (contribute to `deps` layer):

| Extension | Detects | Patterns |
|-----------|---------|----------|
| `tool-versions` | `.tool-versions` or `.mise.toml` | `.tool-versions`, `.mise.toml` |

### How Merge Works

All contributions are merged generically - no special cases for "agent" vs "deps":

1. Seed core layers: `deps`, `agent` (always exist, even if empty)
2. For each active extension's contribution, add patterns to the named layer (deduped)
3. Extra layers from extensions are appended in first-seen order
4. Project layer is always last, always catch-all
5. Ignore patterns and hooks are unioned across all extensions

## Configuration (`bento.yaml`)

```yaml
id: ws-<random>            # auto-generated workspace identifier
task: "description"        # optional task description
store: ~/.bento/store      # local store path
remote: ghcr.io/org/repo   # optional remote registry

# Extensions auto-detect by default. List explicitly to override:
# extensions: [claude-code, node]

# Full layer override (bypasses extensions entirely):
# layers:
#   - name: deps
#     patterns: [".venv/**", "node_modules/**"]
#   - name: agent
#     patterns: [".my-agent/**"]
#   - name: project
#     catch_all: true

env:
  NODE_ENV: development
  DATABASE_URL:
    source: env
    var: DATABASE_URL

ignore: ["*.log", "tmp/"]

hooks:
  pre_save: "make clean"
  post_restore: "npm install"
  timeout: 300

retention:
  keep_last: 10
  keep_tagged: true

watch:
  debounce: 10
  message: "auto-save"
```

## Save Flow

```
1. Load bento.yaml
2. Resolve extensions (auto-detect or explicit list)
3. Merge contributions → layer definitions + ignore + hooks
4. Run pre_save hook (abort on failure)
5. Collect ignore patterns (common + extensions + config + .bentoignore)
6. Scan workspace - assign files to layers
7. Secret scan (gitleaks, ~200+ rules, concurrent, cached)
   - If secrets found: scrub them (replace with __BENTO_SCRUBBED[id]__ placeholders)
   - Write scrubbed content to temp files for packing (real files on disk untouched)
   - Store secret values locally (plaintext + encrypted envelope)
   - Store scrub records in OCI manifest metadata
8. Acquire file lock (.save-lock)
9. Pack layers concurrently (tar+gzip, parallel up to NumCPU) using scrubbed overrides
10. Compare layer digests with parent - skip if all unchanged
11. Build OCI config + manifest (includes scrub records, backend name, restore hint)
12. Store to local OCI layout, tag as cp-N and latest
13. Run post_save hook (warn on failure, don't abort)
```

## CLI Commands

| Command | Purpose |
|---------|---------|
| `bento init` | Initialize workspace tracking |
| `bento save` | Save a checkpoint |
| `bento open` | Restore a checkpoint (persists remote on first open from registry) |
| `bento list` | List checkpoints |
| `bento status` | Show workspace status, head, changes, and remote sync state |
| `bento diff` | Compare checkpoints or workspace vs latest |
| `bento fork` | Branch from a checkpoint |
| `bento tag` | Tag a checkpoint |
| `bento inspect` | Show metadata and layer summary |
| `bento push` | Push to OCI registry (persists remote on first push with URL) |
| `bento pull` | Pull checkpoints from remote registry (persists remote on first pull with URL) |
| `bento gc` | Garbage collection |
| `bento env` | Manage env vars and secret refs |
| `bento watch` | Auto-checkpoint on file changes |
| `bento add` | Add a file to a layer |
| `bento secrets export` | Export encrypted secret envelope for a checkpoint |

## Testing

```bash
make test               # unit tests (go test ./... -race)
make test-integration   # E2E tests (build-tagged: integration)
make lint               # golangci-lint
```

E2E tests in `e2e/` compile the binary and exercise full workflows. They create isolated temp workspaces - never pollute the real store.

## Build & Release

```bash
make build    # → bin/bento
make release  # goreleaser
make clean    # rm -rf bin/ dist/
```

Releases via GoReleaser on tag push. Builds for linux/darwin/windows × amd64/arm64. Distributes via GitHub Releases and Homebrew (`kajogo777/bento`).

## Development Principles

### 1. Always Test End-to-End

Every feature needs an E2E test in `e2e/` that exercises the real binary. Follow the existing pattern:

- Build binary fresh in `TestMain`
- Create isolated temp workspaces with their own store paths
- Run the real `bento` binary and assert on output
- Verify file-level fidelity after restore (byte-for-byte)
- Test the full cycle: `save → inspect → open → verify → save → verify unchanged layers reuse digests`

### 2. Strong Types and Validation for All Configuration

Configuration errors must be caught at parse time, not at runtime.

- New config fields get a stanza in `BentoConfig.Validate()` with actionable error messages
- Use typed structs over `map[string]interface{}`
- If a field has a fixed set of values, define constants and validate against them
- Error messages say what's wrong and how to fix it

### 3. Simple, Obvious Names

- **CLI commands** - short verbs: `save`, `open`, `list`, `diff`, `fork`, `tag`, `inspect`
- **Config fields** - plain English, `snake_case`: `keep_last`, `catch_all`, `post_restore`
- **Go types** - say what they do: `Extension`, `Contribution`, `Merge`, `LayerDef`
- **Vocabulary** - use project terms: checkpoint (not snapshot), layer (not partition), extension (not plugin/adapter), open (not restore/extract)
- Flag names mirror config: `--keep-last` ↔ `keep_last:`

### 4. Error Handling

1. **Never lose data silently** - fail loudly with a clear message
2. **Partial results > no results** - especially for restore
3. **Idempotency** - same inputs produce same digests
4. **Non-interactive by default** - `--force` for unattended operation
5. **Pre-hooks abort; post-hooks warn**
6. **Actionable errors** - say what happened, why, and how to fix it

### 5. OCI Compatibility First

Bento artifacts must remain valid OCI images. Never break `docker pull`, `COPY --from`, `crane`, or `cosign` compatibility.

## Development Cookbook

### Adding a New Extension

1. Create `internal/extension/<name>.go` implementing `Extension`
2. Add to `allBuiltinExtensions()` in `internal/extension/registry.go`
3. Define detection (file or directory existence check)
4. Return `Contribution` with patterns for the right layer
5. Add E2E test: `save → inspect → open → verify → save → verify unchanged layers reuse digests`

### Adding a New CLI Command

1. Create `internal/cli/<command>.go` with `newXxxCmd() *cobra.Command`
2. Register in `NewRootCmd()` in `internal/cli/root.go`
3. Use `resolveExtensions()` for commands that need layer definitions
4. Add E2E test covering happy path and at least one error case

### Adding a New Config Field

1. Add to the appropriate struct in `internal/config/config.go`
2. Add validation in `BentoConfig.Validate()`
3. Add unit tests for valid and invalid values
4. If it affects the OCI manifest, update `internal/manifest/config.go`

### Secret Scrubbing Internals

Secret scrubbing requires no configuration. Secrets detected by gitleaks are
automatically scrubbed during save and restored during open. Key components:

- `internal/secrets/scrub.go` — `ScrubFile()` and `HydrateFile()` core functions
- `internal/secrets/backend/oci.go` — `EncryptSecrets()`, `DecryptSecrets()`, key wrapping (Curve25519)
- `internal/cli/secrets_layer.go` — `extractSecretsEnvelope()` shared helper for reading secrets from OCI layers
- Encrypted secrets stored as an OCI layer in the manifest (single source of truth)
- Placeholder IDs are stable across saves (reused from parent checkpoint via content hash matching)
- Push strips the secrets layer by default; `--include-secrets` keeps/re-wraps it
- See `specs/secret-scrubbing.md` for the full design

### Cross-Platform

- All archive paths use forward slashes
- External paths use portable format: `__external__/~/path`
- Permissions: store POSIX bits on Linux/macOS, apply defaults on Windows
- Symlinks: create on Linux/macOS, copy-fallback on Windows
- Store location: `~/.bento/store` (macOS), `~/.local/share/bento/store` (Linux), `%LOCALAPPDATA%\bento\store` (Windows)
