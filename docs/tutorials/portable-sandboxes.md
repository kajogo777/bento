# Portable Sandboxes

Save and push from any sandbox (E2B, Fly.io, Docker, bare metal):

```bash
bento save -m "checkpoint before migration"
# Secret scan: 38 files clean
# Tagged: cp-2, latest

bento push ghcr.io/myorg/workspaces/my-project
# Pushing to ghcr.io/myorg/workspaces/my-project...
# Done.
```

Open on any other environment:

```bash
bento open ghcr.io/myorg/workspaces/my-project:latest ~/my-project
# Pulling from ghcr.io/myorg/workspaces/my-project:latest...
# Generated bento.yaml from artifact metadata
# Restoring checkpoint latest (sequence 2)...
# Restored to /home/user/my-project
#
#   To undo: bento open undo
```

Bento artifacts are standard OCI images, so they also work directly in Dockerfiles:

```dockerfile
FROM python:3.12
COPY --from=ghcr.io/myorg/workspaces/my-project / /workspace/
WORKDIR /workspace
CMD ["python", "main.py"]
```
