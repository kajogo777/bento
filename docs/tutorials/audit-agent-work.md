# Audit Agent Work

List your checkpoint history:

```bash
bento list
# TAG    CREATED              DIGEST               MESSAGE
# cp-3   2026-03-20 10:00     sha256:789012abc34... started auth refactor
# cp-4   2026-03-20 11:30     sha256:abc345def67... auth refactor complete
# cp-5   2026-03-20 14:00     sha256:def678abc90... added rate limiting
# cp-7   2026-03-20 17:00     sha256:345678def12... final cleanup
```

Inspect a specific checkpoint:

```bash
bento inspect cp-7
# Checkpoint: cp-7 (sequence 7)
# Digest:     sha256:345678def12...
# Parent:     sha256:234567abc01...
# Created:    2026-03-20 17:00:00
# Extensions: claude-code, node
# Message:    final cleanup
#
# Layers:
#   [1/3] deps    — 1207 files, 89.0MB
#   [2/3] agent   — 10 files, 72.0KB
#   [3/3] project — 48 files, 156.0KB
#
# Total size: 89.2MB

bento inspect cp-7 --files
# (same as above, plus full file listing per layer)
```

Diff between two checkpoints across all layers:

```bash
bento diff cp-3 cp-7
# Comparing cp-3 → cp-7
#
#   deps: 2 changes (2 modified)
#     ~ package-lock.json  (+42/-8 lines)
#     ~ package.json  (+2/-0 lines)
#   agent: 2 changes (2 modified)
#     ~ .claude/plans/auth-refactor.md  (+15/-3 lines)
#     ~ CLAUDE.md  (+8/-2 lines)
#   project: 4 changes (2 added, 2 modified)
#     + src/middleware/rate-limit.ts  (+67 lines)
#     + tests/rate-limit.spec.ts  (+45 lines)
#     ~ src/middleware/auth.ts  (+120/-45 lines)
#     ~ tests/auth.spec.ts  (+88/-12 lines)
```

Unified diff of a specific file:

```bash
bento diff cp-3 cp-7 --file src/middleware/auth.ts
# --- a/src/middleware/auth.ts (cp-3)
# +++ b/src/middleware/auth.ts (cp-7)
# @@ -12,8 +12,15 @@
#  ...
```

What's changed since the last save:

```bash
bento diff
# Comparing workspace → cp-7
#
#   project: 1 changes (1 modified)
#     ~ README.md  (+3/-0 lines)
```

View agent sessions:

```bash
bento sessions cp-7
# AGENT          SESSION                     MSGS  UPDATED              TITLE
# claude-code    abc123def456fab789fab012...  42    2026-03-20 17:00:00  auth refactor

bento sessions inspect abc123def456fab789fab012... --text
# Agent: claude-code
# Session: abc123def456fab789fab012...
#
# --- User 2026-03-20 10:00:00 ---
# Refactor the auth middleware to use JWT tokens
#
# --- Assistant (claude-sonnet-4-6) 2026-03-20 10:00:05 ---
# I'll start by reviewing the current auth implementation...
```
