# Bento CLI Skill

Bento packages AI agent workspace state into portable, layered OCI artifacts — everything git doesn't track: agent memory, dependencies, build caches, conversation history, session state. Checkpoints are standard OCI images pushable to any container registry.

## Commands

```bash
bento init                          # initialize workspace tracking
bento save -m "message"             # save a checkpoint
bento open <ref>                    # restore checkpoint (ref: cp-1, latest, tag, digest)
bento open undo                     # undo last open
bento list                          # list checkpoints
bento status                        # show head, changes, remote sync
bento diff                          # workspace vs latest
bento diff cp-2 cp-5                # between two checkpoints
bento tag <ref> <name>              # tag a checkpoint
bento inspect [ref]                 # metadata and layers (--files for file list)
bento push [remote]                 # push to OCI registry
bento pull                          # pull from remote
bento gc                            # garbage collection
bento env set|unset|show|export     # manage env vars and secret refs
bento watch                         # auto-checkpoint on file changes
bento add <file> --layer <name>     # add file to a layer
bento keys generate|list|public     # manage Curve25519 keypairs
bento secrets export                # export encrypted secret envelope
bento sessions [inspect]            # list/inspect agent sessions
bento explore                       # interactive checkpoint browser
```

Global flags: `--dir <path>` (workspace directory), `-v` (verbose), `--help`

## Key Concepts

**Checkpoints** — Snapshots stored as OCI images. Auto-numbered (`cp-1`, `cp-2`), content-addressed by digest, taggable. `latest` always points to most recent.

**Layers** — Files split into `deps` (node_modules, .venv — large, rarely changes), `agent` (memory, config — small, changes often), `project` (catch-all). Identical layers are deduplicated across saves.

**Extensions** — Auto-detect agent framework and language tooling. No config needed. Agents: Claude Code, Codex, OpenCode, OpenClaw, Cursor, Stakpak, Pi. Deps: Node, Python, Go, Rust, Ruby, Elixir, OCaml.

**Secrets** — Auto-scanned via gitleaks (200+ rules), replaced with placeholders in layers, encrypted locally, restored transparently on `bento open`. Zero config.

**Hooks** — In `bento.yaml`: `pre_save` (aborts on failure), `post_save`, `post_restore`.

## Workflows

```bash
# Checkpoint before risky work
bento save -m "stable state"
bento tag latest pre-experiment
# ... if it breaks:
bento open pre-experiment

# Parallel exploration
bento open latest ./approach-a
bento open latest ./approach-b
bento diff approach-a approach-b

# Hand off to another agent
bento save -m "done with auth" && bento push ghcr.io/team/project
# Other agent:
bento init --remote ghcr.io/team/project && bento pull && bento open latest

# Resume after interruption
bento status && bento sessions inspect && bento open latest

# Undo mistakes
bento diff                    # see what changed
bento open <last-good-ref>    # roll back
bento open undo               # or undo the last open
```

## Agent Tips

- Save frequently with descriptive messages — deduped layers make it cheap
- Run `bento status` at session start to understand current state
- Use `bento diff` before/after changes to verify modifications
- Tag milestones before big changes: `bento tag latest pre-refactor`
- `bento open` auto-backs up — `bento open undo` reverses it
- Use `--dir` to operate on other workspaces without cd-ing
