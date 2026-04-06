# Parallel Exploration

Save a starting point and tag it:

```bash
bento save -m "before auth refactor"
# Secret scan: 42 files clean
# Tagged: cp-1, latest

bento tag cp-1 before-refactor
# Tagged cp-1 as before-refactor
```

Fork into parallel workspaces:

```bash
bento open before-refactor ~/auth-jwt
# Restoring checkpoint before-refactor (sequence 1)...
# Restored to /Users/alice/auth-jwt
#
#   To undo: bento open undo

bento open before-refactor ~/auth-sessions
# Restoring checkpoint before-refactor (sequence 1)...
# Restored to /Users/alice/auth-sessions
#
#   To undo: bento open undo
```

Run agents in each directory:

```bash
cd ~/auth-jwt
claude "Refactor auth to use JWT tokens"
bento save -m "JWT approach"
# Secret scan: 45 files clean
# Tagged: cp-2, latest

cd ~/auth-sessions
claude "Refactor auth to use server-side sessions"
bento save -m "sessions approach"
# Secret scan: 44 files clean
# Tagged: cp-3, latest
```

List checkpoints and compare:

```bash
bento list
# TAG                        CREATED              DIGEST               MESSAGE
# cp-1, before-refactor      2026-03-20 10:00     sha256:abc123def45... before auth refactor
# cp-2                        2026-03-20 11:30     sha256:def456789ab... JWT approach
# cp-3                        2026-03-20 11:45     sha256:789012abc34... sessions approach

bento diff before-refactor cp-2
# Comparing before-refactor → cp-2
#
#   project: 4 changes (2 added, 2 modified)
#     + src/middleware/jwt.ts  (+89 lines)
#     + tests/jwt.spec.ts  (+45 lines)
#     ~ src/middleware/auth.ts  (+42/-18 lines)
#     ~ package.json  (+2/-0 lines)
#   deps: 1 changes (1 modified)
#     ~ package-lock.json  (+120/-0 lines)

bento diff before-refactor cp-3
# Comparing before-refactor → cp-3
#
#   project: 3 changes (1 added, 2 modified)
#     + src/middleware/session-store.ts  (+67 lines)
#     ~ src/middleware/auth.ts  (+55/-18 lines)
#     ~ package.json  (+1/-0 lines)
#   deps: 1 changes (1 modified)
#     ~ package-lock.json  (+85/-0 lines)
```

Keep the winner:

```bash
cd ~/my-project
bento open cp-2
# Restoring checkpoint cp-2 (sequence 2)...
# Restored to /Users/alice/my-project
#
#   To undo: bento open undo
```
