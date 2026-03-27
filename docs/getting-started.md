# Getting Started with Bento

This tutorial walks you through using bento to checkpoint, restore, and manage your AI agent workspace.

## Installation

Build from source (requires Go 1.23+):

```bash
git clone https://github.com/bentoci/bento.git
cd bento
make build
# Binary is at bin/bento -- add it to your PATH
export PATH="$PWD/bin:$PATH"
```

Verify the installation:

```bash
bento --version
```

## Tutorial: Your First Checkpoint

### 1. Set up a sample project

```bash
mkdir my-project && cd my-project
git init

# Create some source files
echo 'package main

import "fmt"

func main() {
    fmt.Println("hello")
}' > main.go

# Simulate an agent workspace (Claude Code in this example)
mkdir -p .claude
echo '{"model": "claude-sonnet"}' > .claude/settings.json
echo "# My Project\nAlways use descriptive variable names." > CLAUDE.md
```

### 2. Initialize bento

```bash
bento init --task "build hello world app"
```

Bento detects your agent framework automatically and creates two files:

- **bento.yaml** -- configuration for layers, store location, hooks, and secrets
- **.bentoignore** -- patterns for files to exclude (similar to .gitignore)

### 3. Save your first checkpoint

```bash
bento save -m "initial project setup"
```

Output:

```
Scanning workspace...
  agent:     2 files, 185B (changed)
  project:   3 files, 312B (changed)
  deps:      0 files, 32B (empty)
Secret scan: clean
Tagged: cp-1, latest
```

Bento scanned your workspace, assigned files to layers (agent, project, deps), ran a secret scan, and saved an immutable checkpoint tagged `cp-1`.

### 4. List checkpoints

```bash
bento list
```

```
TAG                  CREATED              DIGEST                 MESSAGE
cp-1, latest         2026-03-26T10:00:00Z sha256:abc123...       initial project setup
```

### 5. Inspect a checkpoint

```bash
bento inspect cp-1
```

```
Checkpoint: cp-1 (sequence 1)
Digest:     sha256:abc123...
Created:    2026-03-26T10:00:00Z
Agent:      claudecode
Message:    initial project setup

Config:
  Task:      build hello world app
  Harness:   claudecode
  Git:       main (a1b2c3d)
  Platform:  darwin/arm64
```

## Restoring a Checkpoint

Make some changes and save a second checkpoint:

```bash
echo 'func add(a, b int) int { return a + b }' >> main.go
bento save -m "added add function"
```

Now restore to the first checkpoint:

```bash
bento open cp-1
```

Check that `main.go` is back to its original state:

```bash
cat main.go
# Only the original "hello" code -- the add function is gone
```

Restore the latest state:

```bash
bento open latest
```

### Restoring to a different directory

```bash
mkdir /tmp/restored
bento open cp-1 /tmp/restored
# cp-1's files are now in /tmp/restored/
```

### Selective layer restore

Restore only the agent layer (skip the large deps layer):

```bash
bento open cp-1 --layers agent
# or skip specific layers:
bento open cp-1 --skip-layers deps
```

## Branching and Comparing

### Fork to try a different approach

```bash
# You're at cp-2. Fork from cp-1 to try an alternative:
bento fork cp-1 -m "trying a different algorithm"

# The workspace is now back to cp-1's state.
# Make your changes and save:
echo 'func multiply(a, b int) int { return a * b }' >> main.go
bento save -m "alternative: multiply instead of add"
```

Your checkpoint history now looks like:

```
cp-1 → cp-2 (add function)
  ↘
   cp-3 (multiply function)
```

### Compare two checkpoints

```bash
bento diff cp-2 cp-3
```

```
Comparing cp-2 → cp-3

  Layer 0: unchanged (digest sha256:6b79...)
  Layer 1: changed
    from: sha256:d06d... (312 bytes)
    to:   sha256:9527... (318 bytes)
  Layer 2: unchanged (digest sha256:4f4f...)
```

## Tagging and Cleanup

### Custom tags

```bash
bento tag cp-2 stable
bento tag cp-3 experimental

bento list
# Shows: cp-1, cp-2/stable, cp-3/experimental/latest
```

### Garbage collection

```bash
# Keep only the last 5 checkpoints
bento gc --keep-last 5

# Keep all tagged checkpoints regardless of age
bento gc --keep-last 3 --keep-tagged
```

## Configuration

### bento.yaml

After `bento init`, your `bento.yaml` controls everything:

```yaml
# Where checkpoints are stored locally
store: ~/.bento/store

# Optional remote registry for sharing
remote: ghcr.io/myorg/workspaces

# Sync mode: "manual" (push explicitly) or "on-save"
sync: manual

# Which agent harness to use ("auto" for detection)
harness: auto

# Task description
task: "build hello world app"

# Plain environment variables (safe to store)
env:
  NODE_ENV: development
  LOG_LEVEL: debug

# Secret references (never stored, resolved at restore time)
secrets:
  DATABASE_URL:
    source: env
    var: DATABASE_URL
  API_KEY:
    source: file
    path: /run/secrets/api-key

# Env file template mapping
env_files:
  ".env":
    template: ".env.example"
    secrets: ["DATABASE_URL", "API_KEY"]

# Patterns to exclude from all layers
ignore:
  - "*.log"
  - "tmp/"

# Lifecycle hooks
hooks:
  pre_save: "make clean-temp"
  post_restore: "make setup"
  pre_push: "npm test"

# Retention policy for garbage collection
retention:
  keep_last: 10
  keep_tagged: true
```

### Hooks

Hooks run shell commands at lifecycle points:

| Hook | When it runs | Failure behavior |
|------|-------------|-----------------|
| `pre_save` | Before saving a checkpoint | Aborts the save |
| `post_save` | After saving a checkpoint | Warns but continues |
| `post_restore` | After restoring a checkpoint | Warns but continues |
| `pre_push` | Before pushing to a registry | Aborts the push |
| `post_fork` | After forking a checkpoint | Warns but continues |

### Secrets

Bento never stores secrets. It stores references that are resolved at restore time:

```yaml
secrets:
  # Read from an environment variable
  GITHUB_TOKEN:
    source: env
    var: GITHUB_TOKEN

  # Read from a file on disk
  TLS_CERT:
    source: file
    path: /run/secrets/tls-cert

  # Run a command to get the value
  CUSTOM_SECRET:
    source: exec
    command: "./scripts/get-secret.sh my-secret"
```

### Environment variables

```bash
# View current env vars and secret refs
bento env show

# Set an env var
bento env set NODE_ENV production
```

## Custom Harnesses

If your agent isn't auto-detected, define a harness in `bento.yaml`:

```yaml
harness_config:
  name: my-agent
  detect: ".my-agent/config.json"
  layers:
    - name: project
      patterns:
        - "**/*.py"
        - "**/*.js"
        - "*.md"
        - "*.yaml"
        - "*.json"
    - name: agent
      patterns:
        - ".my-agent/**"
    - name: deps
      patterns:
        - ".venv/**"
        - "node_modules/**"
      frequency: rarely
  ignore:
    - ".my-agent/cache/**"
    - "*.log"
  hooks:
    post_restore: "pip install -e . 2>/dev/null || true"
```

Key concepts:

- **patterns** -- glob patterns that assign files to layers (`**` matches any directory depth)
- **frequency** -- `often` (default) or `rarely` (for deduplication hints)
- **detect** -- file or directory whose presence activates this harness
- Files matching no layer are excluded automatically

## Supported Agents

Bento auto-detects these agent frameworks:

| Agent | Detection | Agent layer captures |
|-------|-----------|---------------------|
| Claude Code | `.claude/` or `CLAUDE.md` | `.claude/**`, `CLAUDE.md` |
| Codex | `.codex/` or `AGENTS.md` | `.codex/**`, `AGENTS.md` |
| Aider | `.aider.conf.yml` or `.aider.chat.history.md` | `.aider*`, `.aider.tags.cache.v3/**` |
| Cursor | `.cursor/` | `.cursor/rules/**`, `.cursor/mcp.json`, `.cursorrules` |
| Windsurf | `.windsurf/` | `.windsurf/**` |

## Next Steps

- Read the [full specification](../specs/SPEC.md) for details on the OCI artifact format
- Read the [harness development guide](../specs/harness-dev.md) to add support for a new agent
- Read the [error handling guide](../specs/error-handling.md) for edge case behavior
