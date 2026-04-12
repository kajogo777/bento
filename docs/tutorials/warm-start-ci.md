# Warm-Start CI

Create and push a warm checkpoint locally:

```bash
cd my-project
npm install
bento init
# Detected extensions: claude-code, node
# Created bento.yaml
# Store: ~/.bento/store (local)
# Created .bentoignore

bento save -m "CI base: all deps installed"
# Secret scan: 42 files clean
# Tagged: cp-1, latest

bento push ghcr.io/myorg/ci-base/my-project
# Pushing to ghcr.io/myorg/ci-base/my-project...
# Done.
```

In CI, open instead of `npm install`:

```yaml
# .github/workflows/agent-task.yml
jobs:
  run-agent:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install bento
        run: |
          curl -fsSL https://github.com/kajogo777/bento/releases/latest/download/bento-linux-amd64 -o /usr/local/bin/bento
          chmod +x /usr/local/bin/bento
      - name: Restore warm checkpoint
        run: bento open ghcr.io/myorg/ci-base/my-project:latest .
      - name: Run agent
        run: claude -p "Fix the issue: ${{ github.event.issue.body }}"
      - name: Save result for debugging
        if: always()
        run: |
          bento save -m "CI run: issue #${{ github.event.issue.number }}"
          bento push ghcr.io/myorg/ci-runs/my-project
```

Debug a failed CI run locally:

```bash
bento open ghcr.io/myorg/ci-runs/my-project:cp-15 ~/debug-ci
# Pulling from ghcr.io/myorg/ci-runs/my-project:cp-15...
# Generated bento.yaml from artifact metadata
# Restoring checkpoint cp-15 (sequence 15)...
# Remote: ghcr.io/myorg/ci-runs/my-project
# Restored to /Users/alice/debug-ci
#
#   To undo: bento open undo

cd ~/debug-ci
bento inspect cp-15
# Checkpoint: cp-15 (sequence 15)
# Digest:     sha256:abc123def45...
# Created:    2026-03-20 03:15:00
# Extensions: claude-code, node
# Message:    CI run: issue #42
#
# Layers:
#   [1/3] deps    — 1207 files, 89.0MB
#   [2/3] agent   — 10 files, 72.0KB
#   [3/3] project — 52 files, 168.0KB
#
# Total size: 89.2MB
```
