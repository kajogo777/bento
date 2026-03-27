# Harness Development Guide

This guide explains how to build a bento harness for an AI coding agent. A harness tells bento where an agent keeps its files, how to decompose them into layers, and what to ignore.

You can define a harness in two ways: YAML in `bento.yaml` (no code, covers most cases) or Go by implementing the `Harness` interface (for complex detection or dynamic config extraction).

## The Layer Model

Every harness must assign files to at least the three core layers:

| Layer | Purpose | What goes here |
|-------|---------|---------------|
| **project** | Files you'd commit to git | Source code, tests, build definitions, lock files, READMEs |
| **agent** | Files the agent created for itself | Memory, session history, plans, skills, custom commands, rules |
| **deps** | Installed/derived artifacts expensive to recreate | node_modules, .venv, build caches, compiled output |

Harnesses can define additional layers when the defaults aren't granular enough. Common additions: build-cache (Rust target/, .next/cache/), data (SQLite files), runtime (pinned agent CLI binaries).

Files matching no layer pattern are excluded. This is intentional.

## Built-in Harness Reference

### Claude Code

Detection: `.claude/` directory or `CLAUDE.md` in workspace root.

#### File layout

```
workspace/
├── CLAUDE.md                    # -> agent layer
├── .claude/
│   ├── settings.json            # -> agent layer
│   ├── commands/                # -> agent layer (custom slash commands)
│   │   └── review.md
│   ├── plans/                   # -> agent layer
│   │   └── auth-refactor.md
│   └── todos/                   # -> agent layer
│       └── current.json
├── .mcp.json                    # -> project layer (MCP server config)
├── src/                         # -> project layer
├── tests/                       # -> project layer
├── package.json                 # -> project layer
├── node_modules/                # -> deps layer
└── .env.example                 # -> project layer (template)
```

#### Layer definition

```go
func (h *ClaudeCodeHarness) Layers() []LayerDef {
    return []LayerDef{
        {
            Name: "project",
            Patterns: []string{
                "**/*.go", "**/*.py", "**/*.js", "**/*.ts", "**/*.jsx", "**/*.tsx",
                "**/*.rs", "**/*.java", "**/*.c", "**/*.cpp", "**/*.h",
                "**/*.html", "**/*.css", "**/*.scss",
                "**/*.sql", "**/*.sh", "**/*.bash",
                "**/*.json", "**/*.yaml", "**/*.yml", "**/*.toml", "**/*.xml",
                "**/*.md", "**/*.txt", "**/*.csv",
                "Makefile", "Dockerfile", "docker-compose*.yaml",
                "go.mod", "go.sum",
                "package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
                "pyproject.toml", "requirements*.txt", "Pipfile", "Pipfile.lock",
                "Cargo.toml", "Cargo.lock",
                ".gitignore", ".gitattributes",
                ".env.example", ".env.template",
                ".mcp.json",
            },
            MediaType: "application/vnd.bento.layer.project.v1.tar+gzip",
            Frequency: ChangesOften,
        },
        {
            Name: "agent",
            Patterns: []string{
                "CLAUDE.md",
                ".claude/**",
            },
            MediaType: "application/vnd.bento.layer.agent.v1.tar+gzip",
            Frequency: ChangesOften,
        },
        {
            Name: "deps",
            Patterns: []string{
                "node_modules/**",
                ".venv/**",
                "vendor/**",
                ".tool-versions",
            },
            MediaType: "application/vnd.bento.layer.deps.v1.tar+gzip",
            Frequency: ChangesRarely,
        },
    }
}
```

#### Ignore patterns

```go
func (h *ClaudeCodeHarness) Ignore() []string {
    return []string{
        ".env", ".env.local", ".env.*.local",
        ".claude/credentials", ".claude/oauth_tokens",
        "*.pem", "*.key", "*.p12",
        "token.json", "credentials",
        ".DS_Store", "Thumbs.db",
        "*.swp", "*.swo", "*~",
        ".git/**",
        "__pycache__/**", "*.pyc",
        "dist/**", "build/**",
    }
}
```

#### Session config extraction

```go
func (h *ClaudeCodeHarness) SessionConfig(workDir string) (*SessionConfig, error) {
    config := &SessionConfig{Agent: "claude-code", Status: "paused"}

    if version, err := exec.Command("claude", "--version").Output(); err == nil {
        config.AgentVersion = strings.TrimSpace(string(version))
    }
    if sha, err := exec.Command("git", "-C", workDir, "rev-parse", "HEAD").Output(); err == nil {
        config.GitSha = strings.TrimSpace(string(sha))
    }
    if branch, err := exec.Command("git", "-C", workDir, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
        config.GitBranch = strings.TrimSpace(string(branch))
    }
    return config, nil
}
```

User-level state (`~/.claude/`, `~/.claude.json`) is NOT captured. That belongs to the user, not the workspace.

### Codex

Detection: `.codex/` directory or `AGENTS.md` in workspace root.

#### File layout

```
workspace/
├── AGENTS.md                    # -> agent layer
├── .codex/
│   ├── config.toml              # -> agent layer
│   ├── setup.sh                 # -> agent layer (worktree setup script)
│   ├── setup.ps1                # -> agent layer (Windows setup)
│   ├── skills/                  # -> agent layer
│   │   └── deploy.md
│   ├── rules/                   # -> agent layer
│   │   └── allow-npm.toml
│   └── actions/                 # -> agent layer
│       └── test.toml
├── src/                         # -> project layer
├── package.json                 # -> project layer
└── node_modules/                # -> deps layer
```

#### Layer definition

```go
func (h *CodexHarness) Layers() []LayerDef {
    return []LayerDef{
        {
            Name:      "project",
            Patterns:  []string{/* same source patterns as Claude Code */},
            MediaType: "application/vnd.bento.layer.project.v1.tar+gzip",
            Frequency: ChangesOften,
        },
        {
            Name:      "agent",
            Patterns:  []string{"AGENTS.md", ".codex/**"},
            MediaType: "application/vnd.bento.layer.agent.v1.tar+gzip",
            Frequency: ChangesOften,
        },
        {
            Name:      "deps",
            Patterns:  []string{"node_modules/**", ".venv/**", "vendor/**"},
            MediaType: "application/vnd.bento.layer.deps.v1.tar+gzip",
            Frequency: ChangesRarely,
        },
    }
}

func (h *CodexHarness) DefaultHooks() map[string]string {
    return map[string]string{
        "post_restore": "test -f .codex/setup.sh && sh .codex/setup.sh || true",
    }
}
```

User-level state (`~/.codex/`) is NOT captured.

### Aider

Detection: `.aider.conf.yml` or `.aider.chat.history.md` exists.

#### File layout

```
workspace/
├── .aider.conf.yml              # -> agent layer
├── .aider.chat.history.md       # -> agent layer (conversation log)
├── .aider.input.history          # -> agent layer (readline history)
├── .aider.tags.cache.v3/        # -> agent layer (repo map cache)
├── src/                         # -> project layer
├── requirements.txt             # -> project layer
└── .venv/                       # -> deps layer
```

#### Layer definition

```go
func (h *AiderHarness) Layers() []LayerDef {
    return []LayerDef{
        {
            Name:      "project",
            Patterns:  []string{/* standard source patterns */},
            MediaType: "application/vnd.bento.layer.project.v1.tar+gzip",
            Frequency: ChangesOften,
        },
        {
            Name:      "agent",
            Patterns:  []string{".aider*", ".aider.tags.cache.v3/**"},
            MediaType: "application/vnd.bento.layer.agent.v1.tar+gzip",
            Frequency: ChangesOften,
        },
        {
            Name:      "deps",
            Patterns:  []string{".venv/**", "node_modules/**"},
            MediaType: "application/vnd.bento.layer.deps.v1.tar+gzip",
            Frequency: ChangesRarely,
        },
    }
}
```

### Cursor

Detection: `.cursor/` directory exists.

Agent layer captures `.cursor/rules/**`, `.cursor/mcp.json`, and legacy `.cursorrules`. Chat history lives in VS Code's internal SQLite database outside the workspace -- bento does not capture IDE-internal state.

### Windsurf

Detection: `.windsurf/` directory exists.

Agent layer captures `.windsurf/rules/`. Cascade memories are stored in `~/.codeium/windsurf/memories/` (user-level, not captured).

## Writing a Harness for a New Agent

For agents not listed above (OpenClaw, Goose, Gemini CLI, or proprietary tools):

**1. Map the file layout.** Run the agent on a sample project. Catalog every file it creates outside your source code. Check the workspace root for dotfiles, subdirectories the agent creates, and the user home directory for global state.

**2. Classify each file:**
- Project state (would you commit it?) -> project layer
- Agent state (memory, history, skills?) -> agent layer
- Derived/installed (expensive to recreate?) -> deps layer
- Sensitive (secrets, credentials?) -> ignore list
- Disposable (logs, temp, cache?) -> ignore list

**3. Start with YAML:**

```yaml
harness:
  name: openclaw
  detect: ".openclaw/"
  layers:
    - name: project
      patterns: ["**/*.py", "**/*.js", "**/*.ts", "*.md", "*.yaml", "*.json"]
    - name: agent
      patterns: [".openclaw/**"]
    - name: deps
      patterns: [".venv/**", "node_modules/**"]
      frequency: rarely
  ignore:
    - ".openclaw/credentials"
    - ".openclaw/cache/http/**"
    - "*.log"
  hooks:
    post_restore: "pip install -e . 2>/dev/null || true"
```

**4. Test the cycle.** `bento save` -> `bento inspect` (check layer sizes) -> `bento open` into empty dir -> verify workspace works -> `bento save` again -> verify unchanged layers have identical digests.

## Common Pitfalls

**Overly broad patterns.** `**/*` captures everything. Be specific about extensions and directories.

**Missing lock files.** Lock files (`package-lock.json`, `Cargo.lock`) belong in project, not deps. They're small, change with code, and are needed to reproduce deps.

**Capturing user-level state.** Files in `~/` belong to the user. Don't include `~/.claude/`, `~/.codex/`, or `~/.cursor/`.

**Forgetting .gitignore alignment.** Most gitignored files should also be bentoignored. Exception: deps (node_modules, .venv) -- git ignores them, bento captures them.

**Empty layers.** If a layer has zero matching files, include it as an empty tar archive. Keeps manifest structure consistent.

## Secret Patterns

Every harness should define patterns for common credential formats:

```go
func (h *MyHarness) SecretPatterns() []string {
    return []string{
        `(?i)AKIA[0-9A-Z]{16}`,              // AWS access key
        `(?i)sk-[a-zA-Z0-9]{20,}`,           // OpenAI/Anthropic API key
        `ghp_[a-zA-Z0-9]{36}`,               // GitHub PAT
        `glpat-[a-zA-Z0-9\-]{20,}`,          // GitLab PAT
        `-----BEGIN (RSA |EC )?PRIVATE KEY`,  // Private keys
        `(?i)(password|passwd|pwd)\s*[:=]`,   // Password assignments
    }
}
```

These are checked during pre-push scan. False positives are warned, not blocked -- the user decides.
