# Secret Scrubbing

## Problem

Config files like `.mcp.json`, `config.yaml`, and others often contain secrets
(API keys, tokens, passwords) inline. Bento's gitleaks scanner detects these
during save. Previously, this aborted the save — forcing users to either
suppress the warning (secrets in the OCI artifact in plain text) or skip
scanning entirely.

## Solution

Bento automatically scrubs secrets from files during `bento save` and restores
them during `bento open`. No configuration required.

- **Save** detects secrets via gitleaks, replaces them with unique placeholders,
  packs the scrubbed content into OCI layers, and stores the real values locally
  (encrypted).
- **Open** restores scrubbed files and hydrates them with real values — seamlessly
  on the same machine, or with a secret key when opening on another machine.

## Design Principles

1. **Zero config** — scrubbing works out of the box, no setup needed
2. **Transparent** — scrubbing and hydration happen automatically
3. **WYSIWYG** — files on disk always have real values; scrubbing only affects packed layers
4. **No secrets in OCI** — artifacts contain only opaque placeholders (unless the user explicitly opts in with `push --include-secrets`, which includes them encrypted)
5. **One key per checkpoint** — a single data encryption key protects all secrets for a checkpoint

## Architecture

```
bento save:
  1. gitleaks scans files, finds secrets
  2. Load parent checkpoint's scrub state (content hashes + encrypted secrets)
  3. For each file with secrets:
     a. Compute content hash (SHA256 of original file)
     b. If unchanged from parent: reuse previous placeholder IDs (stable digests)
     c. If changed or new: generate fresh random placeholder IDs
  4. Replace secret values with placeholders in-memory
  5. Pack the scrubbed content into OCI layers (real files on disk untouched)
  6. Encrypt secrets into an envelope (NaCl secretbox + Curve25519 key wrapping)
  7. Pack encrypted envelope as an OCI layer ("secrets" layer with annotations)
  8. Store scrub records + content hashes in OCI manifest metadata
  9. Save checkpoint (workspace layers + secrets layer in one manifest)

bento open:
  1. Restore scrubbed files from OCI layers
  2. Read scrub records from OCI manifest
  3. Extract encrypted secrets from OCI secrets layer
  4. Decrypt envelope with local keypair (or --private-key)
  5. Replace placeholders with real values
  6. Verify hydrated content hash matches stored ContentHash
  7. Files on disk have real secrets — ready to work

bento push:
  Without --include-secrets: secrets layer is stripped before pushing
  With --include-secrets: secrets layer kept; re-wrapped for recipients
    if --sender/--recipient specified
```

## Placeholder Format

```
__BENTO_SCRUBBED[<12 hex chars>]__
```

- 12 random hex = 48 bits of entropy (~281 trillion possibilities)
- Double underscores + brackets prevent collision with natural file content
- Regex: `__BENTO_SCRUBBED\[[0-9a-f]{12}\]__`
- Generated at save time; validated to not already exist in the file content
- Each finding gets a unique placeholder, even within the same file
- New placeholders generated per save (each checkpoint is independent)

## Encryption

All secret encryption uses NaCl secretbox (XSalsa20-Poly1305):

- 32-byte random key generated per checkpoint
- 24-byte random nonce prepended to ciphertext
- Authenticated encryption (tamper detection)
- Key displayed as `bento-sk-<base64url>` for easy copy-paste

The same encrypted blob (ciphertext) is used everywhere:
- Stored locally at `~/.bento/secrets/<ws>/<tag>.enc.json`
- Packed into OCI layer when `push --include-secrets`
- Written to stdout by `secrets export`
- Read from file by `open --secrets-file`

Two functions handle all encryption/decryption:

```go
// Encrypt: used by save
backend.EncryptSecrets(secrets) → (ciphertext, secretKey, err)

// Decrypt: used by open, import, hydrate
backend.DecryptSecrets(ciphertext, secretKey) → (secrets, err)
```

## OCI Manifest Metadata

Stored in `BentoConfigObj` (the existing OCI config label):

```go
type ScrubFileRecord struct {
    Path         string              `json:"path"`
    Replacements []ScrubReplacement  `json:"replacements"`
}

type ScrubReplacement struct {
    Placeholder string `json:"placeholder"` // "__BENTO_SCRUBBED[a1b2c3d4e5f6]__"
    RuleID      string `json:"ruleID"`      // "openai-api-key" (diagnostics only)
}
```

Added to `BentoConfigObj`:

```go
ScrubRecords []ScrubFileRecord `json:"scrubRecords,omitempty"`
RestoreHint  string            `json:"restoreHint,omitempty"`
```

Each `ScrubFileRecord` includes a `ContentHash` (SHA256 of the original pre-scrub
file content) used for change detection and hydration integrity verification.

No secret values, no hashes of secrets, no cryptographic material in the OCI manifest config.

## Secrets Storage

Encrypted secrets are stored as an OCI layer in the checkpoint manifest — the
single source of truth. No separate local files are created.

The secrets layer is a tar.gz containing a single `secrets.enc` file with a
`MultiRecipientEnvelope` JSON (NaCl secretbox ciphertext + Curve25519-wrapped
DEK). It is identified by the `dev.bento.secrets.encrypted=true` annotation
on the layer descriptor.

On `bento push`, the secrets layer is stripped by default. With `--include-secrets`,
it is kept (and optionally re-wrapped for specified recipients).

## User Flows

### Save (always the same)

```
$ bento save -m "my work"
Scrubbed 1 secret(s):
  .mcp.json          aws-access-token
Scanning workspace...
  deps:      0 files, 32B (empty)
  agent:     0 files, 32B (empty)
  project:   2 files, 210B (changed)
Secret scan: clean
Tagged: cp-1, latest

Hint: To share secrets with the checkpoint:
   Via registry:  bento push --include-secrets
   Via file:      bento secrets export cp-1 > bundle.enc
```

The secret key is shown when you push or export — not at save time.

### Push

```
$ bento push --include-secrets
Included encrypted secrets.
Recipient: bento open <ref> <dir> --secret-key __BENTO_SCRUBBED[2fbcfa66210e]__
Pushing to ghcr.io/org/project...
Done.
```

### Export

```
$ bento secrets export cp-1 > bundle.enc
Secret key: __BENTO_SCRUBBED[2fbcfa66210e]__
Recipient: bento open <ref> <dir> --secret-key __BENTO_SCRUBBED[2fbcfa66210e]__ --secrets-file bundle.enc
```

The key and recipient command are printed to stderr. The ciphertext goes to
stdout (into the file).

### Open (same machine)

```
$ bento open cp-1 ./restore
Restoring checkpoint cp-1 (sequence 1)...
  .mcp.json          aws-access-token         OK
Hydrated 1 secret(s)
Restored to ./restore
```

No key needed. Local store has the plaintext.

### Open (different machine, with key + OCI layer)

```
$ bento open ghcr.io/org/project:cp-1 ./workspace --secret-key __BENTO_SCRUBBED[2fbcfa66210e]__
Restoring checkpoint cp-1 (sequence 1)...
  .mcp.json          aws-access-token         OK
Hydrated 1 secret(s)
Restored to ./workspace
```

### Open (different machine, with key + secrets file)

```
$ bento open ghcr.io/org/project:cp-1 ./workspace --secret-key __BENTO_SCRUBBED[2fbcfa66210e]__ --secrets-file bundle.enc
Restoring checkpoint cp-1 (sequence 1)...
  .mcp.json          aws-access-token         OK
Hydrated 1 secret(s)
Restored to ./workspace
```

### Open (no secrets available — fails)

```
$ bento open ghcr.io/org/project:cp-1 ./workspace
Restoring checkpoint cp-1 (sequence 1)...
  .mcp.json          aws-access-token         FAILED

Error: 1 secret(s) could not be hydrated.

To restore secrets, re-run with the secret key:
  bento open ghcr.io/org/project:cp-1 ./workspace --secret-key <KEY>

If you have a secrets file from the sender:
  bento open ghcr.io/org/project:cp-1 ./workspace --secret-key <KEY> --secrets-file bundle.enc

Ask the sender for the key (shown when they ran bento push or bento secrets export).
Error: secrets not available — provide --secret-key to hydrate
```

Open returns a non-zero exit code. The user must provide the key and re-run.

## CLI Commands

### Modified

| Command | Change |
|---------|--------|
| `bento save` | Scrubs secrets, stores locally + encrypts, shows sharing hints |
| `bento open` | Hydrates from local/OCI layer/secrets file; fails with hints if unavailable |
| `bento push` | `--include-secrets` packs encrypted envelope + shows key |
| `bento gc` | Cleans up local secret files for pruned checkpoints |

### New flags

| Flag | Command | Purpose |
|------|---------|---------|
| `--secret-key` | `open` | Decryption key for encrypted secrets |
| `--secrets-file` | `open` | Path to encrypted bundle from sender |
| `--include-secrets` | `push` | Include encrypted envelope in OCI artifact |

### New subcommands

| Command | Purpose |
|---------|---------|
| `bento secrets export <ref>` | Output encrypted envelope to stdout |

## Implementation

### Files

```
internal/secrets/
  scrub.go              # ScrubFile(), HydrateFile(), PlaceholderRe()
  scrub_test.go
  backend/
    backend.go          # SecretBackend interface
    registry.go         # FindBackend(), DefaultBackend()
    local.go            # LocalBackend (plaintext JSON files)
    local_test.go
    oci.go              # OCIBackend (NaCl encryption), EncryptSecrets(), DecryptSecrets()
    oci_test.go
internal/cli/
  secrets.go            # export subcommand
  save_core.go          # scrub flow after gitleaks scan
  open.go               # hydrate flow (local → OCI layer → secrets file)
  push.go               # --include-secrets flag
  gc.go                 # backend cleanup on prune
internal/config/
  config.go             # (no secrets config — zero config design)
internal/manifest/
  config.go             # ScrubRecords, RestoreHint
  annotations.go        # AnnotationSecretsEncrypted
internal/workspace/
  layer.go              # fileOverrides parameter, PackBytesToTempLayer()
e2e/
  scrub_test.go         # 18 E2E tests
specs/
  secret-scrubbing.md   # this file
examples/
  secrets-oci.yaml      # example config
```

### Save flow (save_core.go)

```
1. Open store, load parent scrub state (content hashes + encrypted secrets from OCI layer)
2. gitleaks scans all workspace files
3. If secrets found:
   a. Group findings by file
   b. For each file:
      - Compute SHA256 content hash
      - If hash matches parent: reuse parent's placeholder IDs (prevPlaceholders)
      - ScrubFile(content, findings, prevPlaceholders) → scrubbed + replacements
   c. Write scrubbed content to temp files
   d. Register temp files as overrides for layer packing
   e. Collect placeholder→value map
4. Pack workspace layers (packer reads from overrides instead of real files)
5. Skip check: if all layer digests match parent → skip save (stable placeholders make this work)
6. Encrypt: EncryptSecrets(secrets) → ciphertext + DEK
7. Wrap: BuildMultiRecipientEnvelope(DEK, self) → envelope
8. Pack envelope as OCI layer: PackBytesToTempLayer("secrets.enc", envJSON)
9. Append secrets layer to layerInfos (with annotations)
10. Embed scrub records + content hashes in OCI manifest config
11. BuildManifest + SaveCheckpoint (workspace layers + secrets layer)
```

### Open flow (open.go)

```
1. Restore scrubbed files from OCI layers
2. Read scrub records from manifest config
3. Extract secrets from OCI secrets layer:
   extractSecretsEnvelope(store, manifest) → envelope bytes
4. Decrypt: TryUnwrapEnvelope(envelope, privateKey) → placeholder→value map
5. For each scrubbed file:
   a. HydrateFile(content, secrets)
   b. Verify SHA256(hydrated) == ContentHash (warn on mismatch)
6. If secrets layer missing: show hint with recovery commands
```

### Push flow (push.go)

```
Without --include-secrets:
  1. Strip secrets layer from manifest (RemoveSecretsLayer)
  2. Push workspace-only manifest to remote

With --include-secrets:
  1. If no re-wrapping needed: push manifest as-is (self-wrapped secrets layer)
  2. If --sender/--recipient specified:
     a. Extract envelope from OCI layer
     b. RewrapEnvelope for new sender/recipients
     c. Remove old secrets layer, inject re-wrapped layer
     d. Push manifest with re-wrapped secrets layer
```

## Security Properties

| Property | Guarantee |
|----------|-----------|
| No secrets in workspace layers | Scrubbed before packing; only placeholders |
| Encrypted secrets layer | NaCl secretbox (XSalsa20-Poly1305) + Curve25519 key wrapping |
| No crypto material in config | No hashes of secrets, no salts, no keys in manifest config |
| Forward secrecy | One-time 32-byte DEK per checkpoint |
| Secrets not pushed by default | Push strips secrets layer unless --include-secrets |
| WYSIWYG on disk | Real files never modified during save; hydrated immediately on open |
| Graceful degradation | If secrets unavailable: files restore with placeholders + actionable hints |
| Hydration integrity | ContentHash verified after hydration to detect corruption |
| Stable placeholder IDs | Reused from parent checkpoint; no information leakage |

## GC Integration

Encrypted secrets are stored as OCI layer blobs in the store. When `bento gc`
prunes checkpoints, the orphaned blobs (including secrets layers) are cleaned
up automatically by blob GC. No separate cleanup is needed.
