# Workspace Templates

Create a template workspace:

```bash
mkdir ~/template-python-api && cd ~/template-python-api
poetry init && poetry add fastapi uvicorn sqlalchemy && poetry install
# Set up CLAUDE.md, AGENTS.md, directory structure...

bento init --task "python API template"
# Detected extensions: python
# Created bento.yaml
# Store: ~/.bento/store (local)
# Created .bentoignore

bento save -m "python API template: fastapi + sqlalchemy + poetry"
# Secret scan: 28 files clean
# Tagged: cp-1, latest

bento push ghcr.io/myorg/templates/python-api
# Pushing to ghcr.io/myorg/templates/python-api...
# Done.
```

Start a new project from the template:

```bash
bento open ghcr.io/myorg/templates/python-api:latest ~/my-new-api
# Pulling from ghcr.io/myorg/templates/python-api:latest...
# Generated bento.yaml from artifact metadata
# Restoring checkpoint latest (sequence 1)...
# Restored to /Users/alice/my-new-api
#
#   To undo: bento open undo

cd ~/my-new-api
# Poetry virtualenv with fastapi, sqlalchemy installed
# CLAUDE.md with project conventions
# AGENTS.md for cross-agent context
# Directory structure scaffolded
```

When conventions change, push an updated template:

```bash
cd ~/template-python-api
# Update deps, CLAUDE.md, etc.
bento save -m "updated to fastapi 0.115, added alembic"
# Secret scan: 30 files clean
# Tagged: cp-2, latest

bento push ghcr.io/myorg/templates/python-api
# Pushing to ghcr.io/myorg/templates/python-api...
# Done.
```
