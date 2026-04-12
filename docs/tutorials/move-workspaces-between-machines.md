# Move Workspaces Between Machines

On your laptop, save and push. The first push remembers the remote:

```bash
bento save -m "moving to cloud VM"
# Secret scan: 42 files clean
# Tagged: cp-3, latest

bento push ghcr.io/myorg/workspaces/my-project
# Remote: ghcr.io/myorg/workspaces/my-project
# Pushing to ghcr.io/myorg/workspaces/my-project...
# Done.
```

On the cloud VM, open from the registry. The remote is remembered here too:

```bash
bento open ghcr.io/myorg/workspaces/my-project:latest ~/my-project
# Pulling from ghcr.io/myorg/workspaces/my-project:latest...
# Generated bento.yaml from artifact metadata
# Restoring checkpoint latest (sequence 3)...
# Remote: ghcr.io/myorg/workspaces/my-project
# Restored to /home/bob/my-project
#
#   To undo: bento open undo

cd ~/my-project
```

Work on the VM, then push back. No URL needed — the remote is already configured:

```bash
bento save -m "heavy build done"
# Secret scan: 42 files clean
# Tagged: cp-4, latest

bento push
# Pushing to ghcr.io/myorg/workspaces/my-project...
# Done.
```

Back on your laptop, check status, pull, and open:

```bash
bento status
# Head:      cp-3 (saved 2 hours ago)
# Remote:    ghcr.io/myorg/workspaces/my-project
#   Sync:    1 checkpoint behind (remote has cp-4, head is cp-3)
#            run `bento pull` to sync

bento pull
# Pulling from ghcr.io/myorg/workspaces/my-project...
#   pulled cp-4
#   pulled latest
# Done.

bento open latest
# Restoring checkpoint latest (sequence 4)...
# Restored to /Users/alice/my-project
#
#   To undo: bento open undo
```

From here on, the round-trip is just:

```bash
# Either machine:
bento save -m "more work"
bento push

# Other machine:
bento pull
bento open latest
```

No URLs to remember. `push`, `pull`, and `status` all use the configured remote.

Bento uses Docker credential helpers for registry auth. If `docker push` works, bento works.
