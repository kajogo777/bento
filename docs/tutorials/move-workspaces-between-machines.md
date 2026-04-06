# Move Workspaces Between Machines

On your laptop, save and push:

```bash
bento save -m "moving to cloud VM"
# Secret scan: 42 files clean
# Tagged: cp-3, latest

bento push ghcr.io/myorg/workspaces/my-project
# Pushing to ghcr.io/myorg/workspaces/my-project...
# Done.
```

On the cloud VM, open from the registry:

```bash
bento open ghcr.io/myorg/workspaces/my-project:latest ~/my-project
# Pulling from ghcr.io/myorg/workspaces/my-project:latest...
# Generated bento.yaml from artifact metadata
# Restoring checkpoint latest (sequence 3)...
# Restored to /home/bob/my-project
#
#   To undo: bento open undo

cd ~/my-project
```

Work on the VM, then push back:

```bash
bento save -m "heavy build done"
# Secret scan: 42 files clean
# Tagged: cp-4, latest

bento push
# Pushing to ghcr.io/myorg/workspaces/my-project...
# Done.
```

Back on your laptop:

```bash
bento open ghcr.io/myorg/workspaces/my-project:latest .
# Pulling from ghcr.io/myorg/workspaces/my-project:latest...
# Restoring checkpoint latest (sequence 4)...
# Restored to /Users/alice/my-project
#
#   To undo: bento open undo
```

Bento uses Docker credential helpers for registry auth. If `docker push` works, bento works.
