# Getting Started with Bento

Bento saves your entire AI agent workspace (code, agent memory, dependencies) so you can pause, resume, and share your work. Think of it like git, but for everything git doesn't track.

## Install

```bash
# macOS / Linux (Homebrew)
brew tap kajogo777/bento https://github.com/kajogo777/bento
brew install --cask kajogo777/bento/bento

# From source
go install github.com/kajogo777/bento@latest

# Or download a binary from GitHub Releases
# https://github.com/kajogo777/bento/releases
```

> **macOS note:** If you see "bento Not Opened" from Gatekeeper, run:
> `xattr -d com.apple.quarantine "$(which bento)"`

Verify it works:

```bash
bento --version
```

## Your First Checkpoint

Open any project where you're working with an AI agent (Claude Code, Codex, Aider, Cursor, or Windsurf).

### Initialize

```bash
cd my-project
bento init
```

```
Detected agent: claude-code
Created bento.yaml
Store: ~/.bento/store (local)
Created .bentoignore
```

Bento auto-detects your agent and creates a `bento.yaml` config file. You can optionally describe what you're working on:

```bash
bento init --task "refactor auth module"
```

### Save a checkpoint

After doing some work with your agent, save a snapshot:

```bash
bento save -m "auth refactor complete"
```

```
Scanning workspace...
  deps:      1204 files, 89MB (unchanged, reusing)
  agent:     8 files, 64KB (changed)
  project:   42 files, 128KB (changed)
Secret scan: clean
Tagged: cp-1, latest
```

Bento splits your workspace into three layers:

- **deps** - installed packages like `node_modules` or `.venv` (changes rarely)
- **agent** - your agent's memory, plans, and settings
- **project** - everything else (source code, tests, configs, binaries)

Layers that haven't changed are reused automatically. All file types are captured.

### Keep working, keep saving

```bash
bento save -m "added tests"
# Tagged: cp-2, latest

bento save -m "fixed edge cases"
# Tagged: cp-3, latest
```

### See your checkpoints

```bash
bento list
```

```
TAG                  CREATED              DIGEST                 MESSAGE
cp-1                 2026-03-26T10:00:00Z sha256:abc123...       auth refactor complete
cp-2                 2026-03-26T11:30:00Z sha256:def456...       added tests
cp-3, latest         2026-03-26T14:15:00Z sha256:789abc...       fixed edge cases
```

### Look at checkpoint details

```bash
bento inspect cp-2
```

```
Checkpoint: cp-2 (sequence 2)
Digest:     sha256:def456...
Created:    2026-03-26T11:30:00Z
Agent:      claude-code
Message:    added tests

Config:
  Task:      refactor auth module
  Harness:   claude-code
  Git:       main (a1b2c3d)
  Platform:  darwin/arm64

Layers:

  deps (1204 files, 89MB, sha256:333a...)
    node_modules/express/lib/express.js
    node_modules/express/package.json
    ...

  agent (8 files, 64KB, sha256:222b...)
    .claude/plans/auth-refactor.md
    .claude/settings.json
    .claude/todos/current.json
    CLAUDE.md

  project (42 files, 128KB, sha256:111a...)
    package.json
    src/auth.ts
    src/index.ts
    tests/auth.test.ts
    ...
```

## Going Back in Time

Something went wrong at cp-3? Restore to cp-2:

```bash
bento open cp-2
```

Your workspace is now exactly as it was at cp-2: code, agent memory, and all. Files from later checkpoints are cleaned up.

### Restore to a separate directory

```bash
bento open cp-1 ~/old-checkpoint
```

### Skip the big stuff

```bash
bento open cp-2 --skip-layers deps
```

## Trying Different Approaches

### Fork from a checkpoint

```bash
bento fork cp-1 -m "trying redis instead of postgres"
```

This restores your workspace to cp-1 so you can take a different path:

```
cp-1 → cp-2 → cp-3 (postgres approach)
  ↘
   cp-4 (redis approach) → cp-5 → ...
```

### Compare approaches

```bash
bento diff cp-3 cp-5
```

```
Comparing cp-3 → cp-5

  deps: unchanged (sha256:4f4f...)
  agent: changed (64KB → 68KB)
    ~ .claude/plans/auth-refactor.md
  project: changed (128KB → 135KB)
    + src/redis-client.ts
    - src/postgres-client.ts
    ~ src/auth.ts
```

Green `+` for added files, red `-` for removed, yellow `~` for modified.

## Organizing with Tags

```bash
bento tag cp-3 postgres-done
bento tag cp-5 redis-done

bento open postgres-done
bento diff postgres-done redis-done
```

## Cleaning Up

```bash
bento gc --keep-last 5
bento gc --keep-last 5 --keep-tagged
```

## Secrets

Bento never stores secrets. It stores references that get resolved when you restore:

```yaml
secrets:
  DATABASE_URL:
    source: env
    var: DATABASE_URL
  API_KEY:
    source: file
    path: /run/secrets/api-key

env_files:
  ".env":
    secrets: ["DATABASE_URL", "API_KEY"]
```

On `bento open`, your `.env` file is populated with resolved values from your local machine. The secrets never leave your system. The `.env` file is written with 0600 permissions and excluded from checkpoints.

If you have a `.env.example` template, reference it:

```yaml
env_files:
  ".env":
    template: ".env.example"
    secrets: ["DATABASE_URL", "API_KEY"]
```

## Hooks

Run commands at lifecycle points:

```yaml
hooks:
  pre_save: "make clean"
  post_restore: "npm install"
  pre_push: "npm test"
```

## Multiple Agents

If your project uses multiple agents, bento detects all of them:

```
Detected agent: claude-code+codex
```

```
Scanning workspace...
  deps:              0 files, 32B (empty)
  agent-claude-code: 3 files, 2KB (changed)
  agent-codex:       4 files, 3KB (changed)
  project:           12 files, 45KB (changed)
```

Each agent's state is tracked independently. Use `--harness <name>` to force a single agent.

## Supported Agents

| Agent | Detection |
|-------|-----------|
| Claude Code | `.claude/` or `CLAUDE.md` |
| Codex | `.codex/` or `AGENTS.md` |
| Aider | `.aider.conf.yml` or `.aider.chat.history.md` |
| Cursor | `.cursor/` directory |
| Windsurf | `.windsurf/` directory |

## Quick Reference

| Action | Command |
|--------|---------|
| Start tracking | `bento init` |
| Save checkpoint | `bento save -m "description"` |
| List checkpoints | `bento list` |
| Restore | `bento open cp-3` |
| Restore elsewhere | `bento open cp-3 ~/other-dir` |
| Fork | `bento fork cp-1 -m "new idea"` |
| Compare | `bento diff cp-1 cp-3` |
| Tag | `bento tag cp-3 milestone` |
| Inspect | `bento inspect cp-3` |
| Clean up | `bento gc --keep-last 10` |
| Env config | `bento env show` |
