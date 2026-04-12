# Resume Where You Left Off

```bash
cd my-project
bento init
# Detected extensions: claude-code, node
# Created bento.yaml
# Store: ~/.bento/store (local)
# Created .bentoignore
```

Work with your agent normally. Save when you're at a good stopping point:

```bash
bento save -m "auth refactor in progress, tests passing"
# Secret scan: 42 files clean
# Tagged: cp-1, latest
```

Days or weeks later, check where you left off:

```bash
bento status
# Workspace: /Users/alice/my-project
# Extensions: claude-code, node
#
# Head:      cp-1 (saved 3 days ago)
# Message:   auth refactor in progress, tests passing
# Local:     1 checkpoint(s)
#
# Remote:    (none)
#
# Changes:   clean
```

See your checkpoint history:

```bash
bento list
# TAG    CREATED              DIGEST               MESSAGE
# cp-1   2026-03-20 10:30     sha256:abc123def45... auth refactor in progress, tests passing
```

Resume from where you left off:

```bash
bento open cp-1
# Restoring checkpoint cp-1 (sequence 1)...
# Restored to /Users/alice/my-project
#
#   To undo: bento open undo
```

Or use watch mode for automatic checkpointing:

```bash
bento watch
# Workspace: /Users/alice/my-project
# Debounce:  10s
# Layers:
#   project: realtime
#   deps:    periodic [node_modules]
#   agent:   periodic [.claude]
# Watching for changes... (Ctrl-C to stop)
# OK cp-2 (checkpoint 14:32:05)
# OK cp-3 (checkpoint 14:35:12)
```
