# Share Workspaces with Teammates

Save and push your workspace:

```bash
bento save -m "auth bug: token refresh fails after 1 hour"
# Secret scan: 42 files clean
# Tagged: cp-7, latest

bento push ghcr.io/myorg/workspaces/my-project
# Secrets layer stripped (use --include-secrets to share)
# Pushing to ghcr.io/myorg/workspaces/my-project...
# Done.
```

Your teammate opens it:

```bash
bento open ghcr.io/myorg/workspaces/my-project:cp-7 ~/debug-auth
# Pulling from ghcr.io/myorg/workspaces/my-project:cp-7...
# Generated bento.yaml from artifact metadata
# Restoring checkpoint cp-7 (sequence 7)...
# Remote: ghcr.io/myorg/workspaces/my-project
# Restored to /home/bob/debug-auth
#
#   To undo: bento open undo
```

To share secrets too, set up keypairs first:

```bash
bento keys generate --name alice
# Generated keypair "alice":
#   Public key:  bento-pk-abc123...
#   Private key: saved to /Users/alice/.bento/keys/alice.json
#
# Share your public key with teammates:
#   bento-pk-abc123...

bento recipients add bob bento-pk-def456...
# Added recipient "bob" to bento.yaml

bento push --include-secrets --sender alice
# Re-wrapped secrets for 2 recipient(s)
# Sealed by: bento-pk-abc123...
# Pushing to ghcr.io/myorg/workspaces/my-project...
# Done.
#
# Recipients can open with:
#   bento open ghcr.io/myorg/workspaces/my-project:cp-7 ./workspace
#   (auto-decrypts if their private key is in ~/.bento/keys/)
```
