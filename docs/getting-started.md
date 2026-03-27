# Getting Started with Bento

Bento saves your entire AI agent workspace -- code, agent memory, dependencies -- so you can pause, resume, and share your work. Think of it like git, but for everything git doesn't track.

This guide walks you through the basics.

## Install

```bash
# macOS / Linux
brew install bentoci/tap/bento

# Windows
scoop install bento

# Or grab a binary directly
curl -fsSL https://bento.dev/install.sh | sh
```

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
  agent:     8 files, 64KB (changed)
  deps:      1204 files, 89MB (unchanged, reusing)
  project:   42 files, 128KB (changed)
Secret scan: clean
Tagged: cp-1, latest
```

Bento splits your workspace into layers:

- **agent** -- your agent's memory, plans, and settings
- **deps** -- installed packages like `node_modules` or `.venv`
- **project** -- your source code, tests, configs, and any other workspace files

Layers that haven't changed are reused automatically, so your 89MB `node_modules` is stored once, not once per checkpoint. All file types in your workspace are captured -- source code, binaries, images, config files -- nothing is silently excluded.

### Keep working, keep saving

```bash
# do more work with your agent...

bento save -m "added tests"
# Tagged: cp-2, latest

# and more...

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
Agent:      claudecode
Message:    added tests

Config:
  Task:      refactor auth module
  Harness:   claudecode
  Git:       main (a1b2c3d)
  Platform:  darwin/arm64
```

## Going Back in Time

Something went wrong at cp-3? Restore to cp-2:

```bash
bento open cp-2
```

Your workspace is now exactly as it was at cp-2 -- code, agent memory, and all. Your agent can pick up right where it left off.

### Restore to a separate directory

Want to look at an old checkpoint without touching your current workspace?

```bash
bento open cp-1 ~/old-checkpoint
```

### Skip the big stuff

If you only need the agent's memory and your code (not the 89MB of dependencies):

```bash
bento open cp-2 --skip-layers deps
```

## Trying Different Approaches

### Fork from a checkpoint

Your agent tried one approach but you want to try something different? Fork:

```bash
bento fork cp-1 -m "trying redis instead of postgres"
```

This restores your workspace to cp-1 so you can take a different path. Your checkpoint history branches:

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

  agent: changed
    from: sha256:abc1... (64KB)
    to:   sha256:def4... (68KB)
  deps: unchanged (digest sha256:4f4f...)
  project: changed
    from: sha256:111a... (128KB)
    to:   sha256:222b... (135KB)
```

## Organizing with Tags

Give checkpoints meaningful names:

```bash
bento tag cp-3 postgres-done
bento tag cp-5 redis-done
```

Now you can refer to them by name:

```bash
bento open postgres-done
bento diff postgres-done redis-done
```

## Cleaning Up

Over time, checkpoints accumulate. Clean up old ones:

```bash
# Keep only the last 5 checkpoints
bento gc --keep-last 5

# Keep tagged checkpoints no matter what
bento gc --keep-last 5 --keep-tagged
```

## Managing Secrets Safely

Bento never stores your secrets. It stores *references* that get resolved when you restore.

In your `bento.yaml`:

```yaml
secrets:
  DATABASE_URL:
    source: env           # reads from your DATABASE_URL env var
    var: DATABASE_URL
  API_KEY:
    source: file          # reads from a file on disk
    path: /run/secrets/api-key

env_files:
  ".env":
    template: ".env.example"
    secrets: ["DATABASE_URL", "API_KEY"]
```

When you `bento open`, your `.env` file gets populated from the template with real values from your local machine. The actual secrets never leave your system.

Check what's configured:

```bash
bento env show
```

## Running Scripts on Save/Restore

Hooks let you run commands at key moments:

```yaml
# in bento.yaml
hooks:
  pre_save: "make clean"              # tidy up before saving
  post_restore: "npm install"         # reinstall after restoring
  pre_push: "npm test"               # run tests before sharing
```

## Supported Agents

Bento auto-detects these agents when you run `bento init`:

| Agent | How it's detected |
|-------|-------------------|
| Claude Code | `.claude/` directory or `CLAUDE.md` |
| Codex | `.codex/` directory or `AGENTS.md` |
| Aider | `.aider.conf.yml` or `.aider.chat.history.md` |
| Cursor | `.cursor/` directory |
| Windsurf | `.windsurf/` directory |

Don't see your agent? You can define a custom harness in `bento.yaml`:

```yaml
harness_config:
  name: my-agent
  detect: ".my-agent/config.json"
  layers:
    - name: project
      patterns: ["**/*.py", "**/*.js", "*.md", "*.yaml"]
    - name: agent
      patterns: [".my-agent/**"]
    - name: deps
      patterns: [".venv/**", "node_modules/**"]
      frequency: rarely
```

## Multiple Agents

If your project uses multiple agents (e.g. Claude Code and Codex), bento detects all of them and creates separate agent layers:

```bash
bento init
```

```
Detected agent: claude-code+codex
```

```
Scanning workspace...
  agent-claude-code: 3 files, 2KB (changed)
  agent-codex:       4 files, 3KB (changed)
  deps:              0 files, 32B (empty)
  project:           12 files, 45KB (changed)
```

Each agent's state is tracked independently. To force a single agent:

```bash
bento init --harness claude-code
```

## Quick Reference

| What you want to do | Command |
|---------------------|---------|
| Start tracking a workspace | `bento init` |
| Save a checkpoint | `bento save -m "description"` |
| See all checkpoints | `bento list` |
| Restore a checkpoint | `bento open cp-3` |
| Restore to a different folder | `bento open cp-3 ~/other-dir` |
| Try a different approach | `bento fork cp-1 -m "new idea"` |
| Compare checkpoints | `bento diff cp-1 cp-3` |
| Name a checkpoint | `bento tag cp-3 my-milestone` |
| View checkpoint details | `bento inspect cp-3` |
| Clean up old checkpoints | `bento gc --keep-last 10` |
| View env/secret config | `bento env show` |
| Push to a registry | `bento push` |
