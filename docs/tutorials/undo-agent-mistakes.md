# Undo Agent Mistakes

Save before a risky operation:

```bash
bento save -m "before db refactor"
# Secret scan: 42 files clean
# Tagged: cp-4, latest
```

The agent makes a mess. Roll back:

```bash
bento open cp-4
# Restoring checkpoint cp-4 (sequence 4)...
# Restored to /Users/alice/my-project
#
#   To undo: bento open undo
```

Changed your mind? Undo the rollback:

```bash
bento open undo
# Restoring checkpoint pre-open (sequence 5)...
# Restored to /Users/alice/my-project
#
#   To undo: bento open undo
```

Restore only specific layers:

```bash
bento open cp-4 --layers deps
# Restoring checkpoint cp-4 (sequence 4)...
# Restored to /Users/alice/my-project
#
#   To undo: bento open undo
```

For continuous protection, use watch mode:

```bash
bento watch --debounce 5
# Workspace: /Users/alice/my-project
# Debounce:  5s
# Layers:
#   project: realtime
#   deps:    periodic [node_modules]
#   agent:   periodic [.claude]
# Watching for changes... (Ctrl-C to stop)
# OK cp-5 (checkpoint 14:32:05)
#   gc: pruned 2 old checkpoint(s)
```
