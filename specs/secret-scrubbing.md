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
  2. For each finding: replace secret value with unique placeholder in-memory
  3. Pack the scrubbed content into OCI layers (real files on disk untouched)
  4. Store plaintext secrets locally (~/.bento/secrets/)
  5. Encrypt secrets into an envelope (NaCl secretbox, one-time key per checkpoint)
  6. Store encrypted envelope locally (for push/export to use later)
  7. Store scrub records in OCI manifest metadata
  8. Display sharing hints (key is shown later, at push/export time)

bento open (same machine):
  1. Restore scrubbed files from OCI layers
  2. Read scrub records from OCI manifest
  3. Pull plaintext secrets from local store
  4. Replace placeholders with real values
  5. Files on disk have real secrets — ready to work

bento open (different machine):
  1. Restore scrubbed files from OCI layers
  2. Try local store → not found
  3. Try OCI encrypted layer (if pushed with --include-secrets) → decrypt with key
  4. Try --secrets-file flag → decrypt with key
  5. If all fail: show hint with exact commands to run
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

No secret values, no hashes, no cryptographic material in the OCI manifest.

## Local Storage

Secrets are stored in two files per checkpoint:

```
~/.bento/secrets/<workspaceID>/
  <tag>.json       # plaintext: {"placeholder": "real-value", ...}  (0600)
  <tag>.enc.json   # encrypted envelope: {"ciphertext": "...", "secretKey": "..."}  (0600)
```

The plaintext file enables seamless same-machine opens (no key needed).
The encrypted envelope is used by `push --include-secrets` and `secrets export`.

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
1. gitleaks scans all workspace files
2. If secrets found:
   a. Group findings by file
   b. For each file: ScrubFile(content, findings) → scrubbed + replacements
   c. Write scrubbed content to temp files
   d. Register temp files as overrides for layer packing
   e. Collect placeholder→value map
3. Pack layers (packer reads from overrides instead of real files)
4. Store plaintext locally: localBe.Put(key, secrets)
5. Encrypt: EncryptSecrets(secrets) → ciphertext + secretKey
6. Store envelope locally: localBe.Put(key+".enc", {ciphertext, secretKey})
7. Embed scrub records + restore hint in OCI manifest
8. Display key + sharing hints
```

### Open flow (open.go)

```
1. Restore scrubbed files from OCI layers
2. Read scrub records from manifest
3. Try to get secrets:
   a. Local backend (same machine) → if found, done
   b. OCI encrypted layer + --secret-key → if found, done
   c. --secrets-file + --secret-key → if found, done
4. For each scrubbed file: HydrateFile(content, secrets)
5. If secrets not found: show hint with exact commands
```

## Security Properties

| Property | Guarantee |
|----------|-----------|
| No secrets in OCI layers | Scrubbed before packing; only placeholders |
| No crypto material in manifest | No hashes, no salts, no keys |
| Forward secrecy | One-time key per checkpoint; old checkpoints safe if current key leaks |
| WYSIWYG on disk | Real files never modified during save; hydrated immediately on open |
| Graceful degradation | If secrets unavailable: files restore with placeholders + actionable hints |
| Local storage security | File permissions 0600; directory 0700 |
| Encrypted envelope | NaCl secretbox (XSalsa20-Poly1305); authenticated encryption |
| Export is encrypted | Exported bundle is ciphertext, not readable without key |

## GC Integration

When `bento gc` prunes checkpoints, it also deletes the corresponding local
secret files (`<tag>.json` and `<tag>.enc.json`) for each pruned checkpoint.
