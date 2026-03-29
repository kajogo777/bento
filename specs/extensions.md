# Extensions Design

**Status:** Proposal
**Replaces:** Harness system (`internal/harness/`)

## Problem

The current harness system has a 1:1 mapping between AI agents and layer definitions. Each harness (Claude Code, Codex, etc.) owns the *entire* layer configuration — agent patterns, deps patterns, ignore patterns, everything. This causes:

1. **Pattern duplication** — Every harness repeats `node_modules/**`, `.venv/**`, `vendor/**` for deps. Every harness repeats `CommonIgnorePatterns`.
2. **Cross-cutting concerns can't be shared** — `AGENTS.md` is a cross-agent convention but each harness must add it independently. If Cursor users want `AGENTS.md` in the agent layer, Cursor's Go code must change.
3. **Agent/framework coupling** — A Claude Code user with a Python project gets Claude Code's deps patterns (which include `node_modules` but not Python-specific patterns like `__pycache__`). The harness conflates "which agent" with "which language/framework".
4. **Hard to contribute** — Adding support for a new agent requires implementing a Go interface with 7 methods. Should be a YAML file.

## Design

Replace monolithic harnesses with composable **extensions**. Each extension has a single concern:

```
┌─────────────────────────────────────────────────┐
│               Extension Registry                 │
├──────────┬──────────┬──────────┬────────────────┤
│  Agent   │  Agent   │  Deps    │  Deps          │
│          │          │          │                │
│  claude  │  codex   │  node    │  python        │
│  -code   │          │          │                │
│          │          │          │                │
│ .claude/ │ .codex/  │ node_    │ .venv/         │
│ CLAUDE.md│ AGENTS.md│ modules/ │ __pycache__/   │
│ sessions │ sessions │          │ *.pyc          │
└──────────┴──────────┴──────────┴────────────────┘
           ↓ merged at save time ↓
┌─────────────┬───────────┬─────────────┐
│    deps     │   agent   │   project   │
└─────────────┴───────────┴─────────────┘
```

### Extension Interface

```go
type Extension interface {
    // Name returns the extension identifier (e.g., "claude-code", "node", "python").
    Name() string

    // Detect returns true if this extension is relevant to the workspace.
    Detect(workDir string) bool

    // Contribute returns the patterns this extension adds to the build.
    // Extensions don't own layers — they contribute patterns TO layers.
    Contribute(workDir string) Contribution
}

type Contribution struct {
    // Patterns to add to existing layers (keyed by layer name: "agent", "deps", "project").
    Layers map[string][]string

    // Additional layers to create (e.g., "build-cache" for Rust target/).
    // These are appended after the core three.
    ExtraLayers []LayerDef

    // Patterns to ignore (added to the global ignore list).
    Ignore []string

    // Default hooks (user hooks in bento.yaml override these).
    Hooks map[string]string

    // Session metadata (agent name, version). Only agent extensions set this.
    Session *SessionContribution
}

type SessionContribution struct {
    Agent        string // e.g., "claude-code"
    AgentVersion string // e.g., "1.2.3" (optional, from --version)
}
```

### Built-in Extensions

#### Agent extensions (contribute to `agent` layer)

| Extension | Detects | Agent layer patterns | Ignore | Session |
|-----------|---------|---------------------|--------|---------|
| `claude-code` | `.claude/` or `CLAUDE.md` | `CLAUDE.md`, `.claude/**`, `~/.claude/projects/<hash>/` | `.claude/credentials`, `.claude/oauth_tokens` | `claude --version` |
| `codex` | `.codex/` | `AGENTS.md`, `.codex/**`, `~/.codex/sessions/` (filtered) | - | - |
| `opencode` | `.opencode/` or `opencode.json` | `AGENTS.md`, `.opencode/**`, `opencode.json`, `~/.local/share/opencode/...` | - | `opencode --version` |
| `openclaw` | `SOUL.md` or `IDENTITY.md` | `SOUL.md`, `AGENTS.md`, `USER.md`, `IDENTITY.md`, `MEMORY.md`, `memory/**`, `skills/**`, `canvas/**`, `~/.openclaw/...` | - | - |
| `cursor` | `.cursor/` or `.cursorrules` | `.cursor/rules/**`, `.cursor/mcp.json`, `.cursorrules`, `~/Library/.../Cursor/workspaceStorage/<hash>/` | - | - |
| `agents-md` | `AGENTS.md` | `AGENTS.md` | - | - |

Note: `agents-md` is a standalone extension that ensures `AGENTS.md` always lands in the agent layer regardless of which agent is active. Agent extensions that already include `AGENTS.md` won't duplicate it (patterns are deduped during merge).

#### Deps extensions (contribute to `deps` layer)

| Extension | Detects | Deps layer patterns | Ignore |
|-----------|---------|---------------------|--------|
| `node` | `package.json` | `node_modules/**` | - |
| `python` | `requirements*.txt`, `pyproject.toml`, `Pipfile`, or `.venv/` | `.venv/**`, `__pycache__/**` | `*.pyc` |
| `go-mod` | `go.mod` | `vendor/**` | - |
| `rust` | `Cargo.toml` | `target/**` (as extra `build-cache` layer) | - |
| `ruby` | `Gemfile` | `vendor/bundle/**` | - |

#### Tool extensions (contribute to various layers)

| Extension | Detects | Contribution |
|-----------|---------|-------------|
| `tool-versions` | `.tool-versions` or `.mise.toml` | deps: `.tool-versions`, `.mise.toml` |
| `docker` | `Dockerfile` or `docker-compose*.yaml` | ignore: `*.log` |

### Resolution Flow

On `bento save` (or `diff`, `watch`, etc.):

```
1. Load bento.yaml
2. If extensions: listed explicitly → use those
   Else → run all built-in extensions' Detect() against workspace
3. Collect Contribution from each active extension
4. Merge contributions:
   a. Union all agent patterns → agent layer
   b. Union all deps patterns → deps layer
   c. Append extra layers (dedup by name)
   d. Union all ignore patterns
   e. Merge hooks (first-defined wins per lifecycle point)
   f. Collect session metadata from agent extensions
5. Project layer = catch-all (always present, captures everything else)
6. Proceed with scan → pack → store
```

### Config

```yaml
# No agent: field. Extensions auto-detect.
# Explicitly list to override auto-detection:
extensions:
  - claude-code
  - node
  - python
```

When `extensions:` is omitted, all built-in extensions auto-detect. When listed, only those extensions are used.

`layers:` still works as a full override — bypasses extensions entirely.

### File Structure

```
internal/extension/
  extension.go          # Extension interface, Contribution type, merge logic
  registry.go           # built-in extension registry, Detect-all, Resolve
  merge.go              # merge Contributions into final LayerDefs
  merge_test.go
  # Agent extensions
  claude_code.go
  codex.go
  opencode.go
  openclaw.go
  cursor.go
  agents_md.go          # cross-agent AGENTS.md
  # Deps extensions
  node.go
  python.go
  gomod.go
  rust.go
  # Tool extensions
  tool_versions.go
  # Inline/YAML extensions
  yaml.go               # parse inline extension definitions from bento.yaml
  yaml_test.go
```

### What This Fixes

1. **AGENTS.md bug** — `agents-md` extension auto-detects `AGENTS.md` and adds it to agent layer, regardless of which agent is active.
2. **No more pattern duplication** — `node` extension owns `node_modules/**`. Claude Code doesn't need to know about it.
3. **Easy contributions** — Adding a new agent or framework is a single Go file with ~30 lines implementing `Extension`. Or a YAML block in `bento.yaml`.
4. **Correct deps detection** — A Claude Code + Python project gets both `.claude/**` in agent and `.venv/**` in deps, because they're independent extensions.
5. **Custom layers** — A Rust extension can add a `build-cache` layer via `ExtraLayers`. No special handling needed.

### Open Questions

1. Should inline YAML extensions support external session capture? (Probably not in v1 — that needs Go code.)
2. Extension ordering: does it matter for pattern priority? (Probably not — patterns are unioned, and first-match-wins is within a single layer.)
3. Should we support disabling auto-detected extensions? e.g., `extensions: [!python, claude-code]` to explicitly exclude python.
