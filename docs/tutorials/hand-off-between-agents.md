# Hand Off Between Agents

Save your Claude Code workspace:

```bash
bento save -m "backend API complete, ready for frontend"
# Secret scan: 42 files clean
# Tagged: cp-5, latest
```

Open in a new directory for a different agent:

```bash
bento open cp-5 ~/my-project-cursor
# Restoring checkpoint cp-5 (sequence 5)...
# Restored to /Users/alice/my-project-cursor
#
#   To undo: bento open undo

cd ~/my-project-cursor
# Open this directory in Cursor, Codex, or any other agent
```

The new agent gets source code, installed dependencies, and the previous agent's memory files.

For handoffs between machines, push to a registry:

```bash
bento save -m "backend done"
# Secret scan: 42 files clean
# Tagged: cp-5, latest

bento push ghcr.io/myorg/workspaces/my-project
# Pushing to ghcr.io/myorg/workspaces/my-project...
# Done.
```

Teammate opens with their preferred agent:

```bash
bento open ghcr.io/myorg/workspaces/my-project:latest ~/my-project
# Pulling from ghcr.io/myorg/workspaces/my-project:latest...
# Generated bento.yaml from artifact metadata
# Restoring checkpoint latest (sequence 5)...
# Remote: ghcr.io/myorg/workspaces/my-project
# Restored to /home/bob/my-project
#
#   To undo: bento open undo
```

Use AGENTS.md for cross-agent context since it's supported by Claude Code, Cursor, Codex, Gemini CLI, Windsurf, and others.
