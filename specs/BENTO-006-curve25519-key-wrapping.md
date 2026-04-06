# Curve25519 Key Wrapping for Secret Sharing

**Version:** 0.2.0
**Status:** Proposal
**Authors:** George Fahmy
**Repository:** github.com/kajogo777/bento
**Related:** `secret-scrubbing.md`, `SPEC.md` §6

## Abstract

This proposal replaces bento's symmetric key sharing (`bento-sk-...`) with
Curve25519 keypair-based envelope encryption. A default keypair is auto-
generated on first save. The checkpoint's data encryption key (DEK) is always
wrapped to the sender (and optionally to additional recipients). Secrets are
recovered using the recipient's private key — no symmetric key strings to
manage, share, or lose.

## Motivation

The current symmetric key flow has fundamental UX problems:

1. **Key management burden** — every push produces a `bento-sk-...` string
   that must be communicated out-of-band and stored somewhere
2. **No access control** — anyone with the string can decrypt
3. **Easy to lose** — the string is shown once at push time; if you don't
   save it, it's gone (unless you have the local store)
4. **Team friction** — every push requires re-sharing the key with every
   teammate through a separate channel

Curve25519 key wrapping eliminates all of these. A keypair is generated once,
the private key lives on your machine, and the public key is short enough to
commit to a repo or put in a Slack bio.

## Design Principles

1. **Zero config** — default keypair auto-generated on first save; no setup
   step required
2. **Keypair is the only path** — no symmetric key strings, no `--secret-key`
   flag, no `bento-dk-` prefixes
3. **WireGuard-style keys** — Curve25519 public keys are 32 bytes = 43 chars
   in base64url, short enough to copy-paste anywhere
4. **NaCl family** — uses `crypto/box` (Curve25519 + XSalsa20 + Poly1305)
   from the same NaCl family as the existing secretbox encryption; zero new
   dependencies
5. **Multi-recipient** — the DEK is wrapped once per recipient; adding a
   recipient is O(1) and doesn't touch the ciphertext or layers
6. **Sender is always a recipient** — the sender's own public key is always
   included in the wrapped keys list, ensuring the sender can always recover
   their own secrets from the registry

## Key Format

### Key Prefixes

- `bento-pk-` — public key (safe to share, commit, publish)
- `bento-sk-` — secret/private key (never leaves the machine)

**Key encoding:** raw base64url (no padding). A Curve25519 key is exactly
32 bytes → 43 base64url chars. Total with prefix: 52 chars.

**Comparison with WireGuard:** WireGuard uses standard base64 (with padding)
for its Curve25519 keys, yielding 44-char strings. Bento uses base64url (no
padding) for URL/CLI safety, yielding 43-char strings. Same curve, same key
size, same copy-paste convenience.

### Key Storage

```
~/.bento/keys/
  default.json          # user's default keypair (auto-generated)
  <name>.json           # named keypairs (e.g., "work", "personal")
```

Each file:

```json
{
  "name": "default",
  "publicKey": "bento-pk-<base64url>",
  "privateKey": "bento-sk-<base64url>",
  "created": "2026-04-01T12:00:00Z"
}
```

File permissions: `0600`. Directory permissions: `0700`.

**Platform-specific key paths** (mirrors the existing secrets directory
convention from `internal/secrets/backend/local.go`):

| Platform | Keys directory |
|----------|---------------|
| macOS | `~/.bento/keys/` |
| Linux | `~/.local/share/bento/keys/` (or `$XDG_DATA_HOME/bento/keys/`) |
| Windows | `%LOCALAPPDATA%\bento\keys\` |

### Auto-Generation

On first `bento save`, if no keypair exists in the keys directory, bento
automatically generates a default keypair and saves it:

```
$ bento save -m "first checkpoint"
Generated default keypair:
  Public key: bento-pk-<base64url, 43 chars>
  Saved to:   ~/.bento/keys/default.json

Share your public key with teammates so they can add you as a recipient.

Scrubbed 1 secret(s):
  .mcp.json          aws-access-token
...
```

This is a one-time event. Subsequent saves use the existing keypair silently.

### Known Recipients

```
~/.bento/keys/recipients/
  <name>.pub             # plain text file containing one bento-pk-... string
```

**`.pub` file format:** a single line containing the `bento-pk-...` string.
Leading/trailing whitespace is trimmed. Lines starting with `#` are ignored
(comments). Blank lines are ignored. Only the first non-comment, non-blank
line is used. Example:

```
# Alice's bento public key (added 2026-04-01)
bento-pk-<base64url, 43 chars>
```

Or in `bento.yaml`:

```yaml
recipients:
  - name: alice
    key: bento-pk-<base64url>
  - name: bob
    key: bento-pk-<base64url>
```

## Cryptographic Design

### Wrapping Scheme

The DEK wrapping uses NaCl `crypto_box` (authenticated boxes):

```
Wrap:
  1. Generate random 24-byte nonce
  2. Compute shared secret: X25519(sender_private, recipient_public)
  3. Derive symmetric key: HSalsa20(shared_secret, zero_nonce)
  4. Encrypt DEK: XSalsa20-Poly1305(derived_key, nonce, DEK)
  5. Output: nonce || ciphertext  (24 + 48 = 72 bytes)
  6. Store sender's public key in envelope metadata

Unwrap:
  1. Read sender's public key from envelope metadata
  2. Extract nonce (first 24 bytes)
  3. Compute shared secret: X25519(recipient_private, sender_public)
  4. Derive symmetric key: HSalsa20(shared_secret, zero_nonce)
  5. Decrypt DEK: XSalsa20-Poly1305(derived_key, nonce, ciphertext)
```

This is exactly `golang.org/x/crypto/nacl/box.Seal` /
`box.Open` — a single function call in each direction.

**Why `crypto_box` (authenticated) instead of `crypto_box_seal` (anonymous)?**
The sender's public key is cryptographically bound to the wrapped DEK, which
gives recipients three things for free:

1. **Provenance** — the envelope proves the DEK was wrapped by the holder of
   the sender's private key. Recipients can verify who sealed the checkpoint
   without any separate signing infrastructure.
2. **Workspace lineage** — the sender's public key in the envelope metadata
   acts as an identity marker, enabling checkpoint history tracing across
   machines (e.g., "Alice created cp-1, Bob forked cp-2 from it").
3. **Trust decisions** — recipients can maintain a list of trusted sender keys
   and reject checkpoints from unknown senders before attempting decryption.

The sender already has a keypair (auto-generated), so authenticated boxes add
no extra setup cost. The sender's public key is stored in the envelope — it's
a public key, safe to include in plaintext.

### Wrapped Key Format

Each wrapped DEK is 72 bytes (24-byte nonce + 48-byte authenticated ciphertext).
Base64url-encoded: 96 chars.

### Envelope Format

```json
{
  "v": 1,
  "sender": "bento-pk-<sender's public key>",
  "ciphertext": "<base64url(nonce || secretbox.Seal(secrets))>",
  "wrappedKeys": [
    {
      "recipient": "bento-pk-<base64url>",
      "wrappedDEK": "<base64url(72 bytes)>"
    },
    {
      "recipient": "bento-pk-<base64url>",
      "wrappedDEK": "<base64url(72 bytes)>"
    }
  ]
}
```

The `sender` field is the sender's Curve25519 public key. Recipients need it
to call `box.Open`. It also serves as a provenance identifier — recipients can
verify which keypair sealed the checkpoint.

The `ciphertext` field contains the secrets encrypted with the DEK via NaCl
secretbox (unchanged from current implementation). The `wrappedKeys` array
contains the DEK wrapped to each recipient.

The sender is always present in `wrappedKeys` — this is what allows recovery
from the registry without any local state.

## Save Flow

```
1. Load parent scrub state (content hashes + secrets from parent OCI layer)
2. gitleaks scan → findings
3. ScrubFile(content, findings, prevPlaceholders) → scrubbed content + replacements
   (reuses parent placeholder IDs for unchanged files)
4. Pack scrubbed workspace layers
5. Skip check: if all layer digests match parent → skip save
6. Load or auto-generate keypair:
   a. Try LoadDefaultKeypair()
   b. If ErrNoKeypair: GenerateKeypair(), SaveKeypair("default"), print public key
7. EncryptSecrets(secrets) → ciphertext + rawDEK
8. BuildMultiRecipientEnvelope(ciphertext, rawDEK, senderPub, senderPriv, [self])
   → envelope JSON
9. PackBytesToTempLayer("secrets.enc", envJSON) → secrets OCI layer
10. Append secrets layer to manifest layers (with annotations)
11. Build OCI config + manifest (includes secrets layer)
12. SaveCheckpoint to local OCI layout
```

**No symmetric key is stored or displayed.** The envelope always contains
wrapped keys. Recovery is always via private key.

## Open Flow

```
1. Try local plaintext store → if found, done
2. Try key wrapping:
   a. Load user's private key from ~/.bento/keys/
   b. Read sender's public key from envelope
   c. Scan wrappedKeys for matching recipient public key
   d. If found: box.Open(wrappedDEK, nonce, senderPubKey, recipientPrivKey) → DEK
   e. DecryptSecrets(ciphertext, DEK) → secrets → done
3. Fail with hint:
   "No matching private key found. Either:
    - Copy your private key from the original machine (~/.bento/keys/)
    - Ask the sender to re-push with your public key as a recipient"
```

## Push Flow

```
$ bento push --include-secrets
Secrets sealed for 1 recipient(s):
  (self)  bento-pk-dG9tIG...
Sealed by: bento-pk-dG9tIG... (default)
Pushing to ghcr.io/org/project...
Done.

To open on another machine, copy your private key:
  ~/.bento/keys/default.json
```

With explicit recipients:

```
$ bento push --include-secrets --recipient alice --recipient bob
Secrets sealed for 3 recipient(s):
  (self)  bento-pk-dG9tIG...
  alice   bento-pk-c2FsbH...
  bob     bento-pk-Ym9iIH...
Sealed by: bento-pk-dG9tIG... (default)
Pushing to ghcr.io/org/project...
Done.

Recipients can open with:
  bento open ghcr.io/org/project:cp-3 ./workspace
```

## CLI Commands

### `bento keys`

```bash
# Generate a new keypair (optional — auto-generated on first save)
bento keys generate [--name <name>]

# List keypairs and known recipients
bento keys list

# Show public key (for sharing)
bento keys public [--name <name>]

# Import a recipient's public key
bento keys add-recipient <name> <bento-pk-...>

# Remove a recipient
bento keys remove-recipient <name>
```

### Modified Commands

```bash
# Push with secrets (always uses key wrapping)
bento push --include-secrets
bento push --include-secrets --recipient bento-pk-...
bento push --include-secrets --recipient alice --recipient bob

# Open auto-discovers private key
bento open ghcr.io/org/project:cp-1 ./workspace
```

### Flags

| Flag | Command | Purpose |
|------|---------|---------|
| `--recipient` | `push`, `save` | Add recipient public key or name |
| `--name` | `keys generate` | Name for the keypair |
| `--private-key` | `open` | Explicit path to private key file |

### Removed (vs current implementation)

| Removed | Reason |
|---------|--------|
| `--secret-key` flag | No symmetric keys; use private key instead |
| `BENTO_SECRET_KEY` env var | No symmetric keys |
| `bento-dk-` prefix | No symmetric keys emitted |
| `bento-sk-` as symmetric key prefix | `bento-sk-` is now exclusively the private key prefix |
| `.enc.json` local files | Replaced by wrapped envelope |
| `bento secrets export` (symmetric) | Replaced by `bento keys` workflow |

### `bento keys list` Output

```
$ bento keys list
Keypairs:
  default    bento-pk-<base64url, 43 chars>    (created 2026-04-01)
  work       bento-pk-<base64url, 43 chars>    (created 2026-03-15)

Recipients:
  alice      bento-pk-<base64url, 43 chars>    (from bento.yaml)
  bob        bento-pk-<base64url, 43 chars>    (from ~/.bento/keys/recipients/bob.pub)
```

If no keypairs exist:

```
$ bento keys list
No keypairs found. One will be auto-generated on first save,
or generate now with:
  bento keys generate
```

## Config Changes

### `bento.yaml`

```yaml
# Optional: default recipients for --include-secrets
recipients:
  - name: alice
    key: bento-pk-<alice's public key>
  - name: bob
    key: bento-pk-<bob's public key>
```

### Validation

- `recipients[].key` MUST start with `bento-pk-`
- Decoded key MUST be exactly 32 bytes
- `recipients[].name` MUST be unique within the list
- Duplicate public keys across different names: warning (not error)

## OCI Manifest Changes

### Annotations

New annotation on the secrets layer descriptor:

| Key | Description |
|-----|-------------|
| `dev.bento.secrets.key-wrapping` | `"curve25519"` — always set for new checkpoints |
| `dev.bento.secrets.sender` | Sender's public key (`bento-pk-...`) for provenance |

### Envelope Storage

The envelope is stored as the encrypted secrets layer in the OCI artifact
(when pushed with `--include-secrets`). The format is always the
`MultiRecipientEnvelope` JSON described above.

## Implementation

### Keypair Generation

Keypairs are generated using `golang.org/x/crypto/nacl/box.GenerateKey`, which
internally uses `crypto/rand` and `curve25519.ScalarBaseMult`:

```go
package keys

import (
    "crypto/rand"
    "encoding/base64"
    "fmt"
    "strings"
    "golang.org/x/crypto/nacl/box"
)

const (
    PrefixPublicKey  = "bento-pk-"
    PrefixPrivateKey = "bento-sk-"
)

// GenerateKeypair creates a new Curve25519 keypair using crypto/rand.
func GenerateKeypair() (publicKey, privateKey [32]byte, err error) {
    pub, priv, err := box.GenerateKey(rand.Reader)
    if err != nil {
        return [32]byte{}, [32]byte{}, fmt.Errorf("generating keypair: %w", err)
    }
    return *pub, *priv, nil
}

// FormatPublicKey encodes a public key as "bento-pk-<base64url>".
func FormatPublicKey(key [32]byte) string {
    return PrefixPublicKey + base64.RawURLEncoding.EncodeToString(key[:])
}

// FormatPrivateKey encodes a private key as "bento-sk-<base64url>".
func FormatPrivateKey(key [32]byte) string {
    return PrefixPrivateKey + base64.RawURLEncoding.EncodeToString(key[:])
}

// ParsePublicKey decodes a "bento-pk-..." string to 32 bytes.
func ParsePublicKey(s string) ([32]byte, error) {
    return parseKey(s, PrefixPublicKey, "public key")
}

// ParsePrivateKey decodes a "bento-sk-..." string to 32 bytes.
func ParsePrivateKey(s string) ([32]byte, error) {
    return parseKey(s, PrefixPrivateKey, "private key")
}

func parseKey(s, prefix, label string) ([32]byte, error) {
    if !strings.HasPrefix(s, prefix) {
        return [32]byte{}, fmt.Errorf("invalid %s: must start with %q", label, prefix)
    }
    b, err := base64.RawURLEncoding.DecodeString(s[len(prefix):])
    if err != nil {
        return [32]byte{}, fmt.Errorf("invalid %s: %w", label, err)
    }
    if len(b) != 32 {
        return [32]byte{}, fmt.Errorf("invalid %s: expected 32 bytes, got %d", label, len(b))
    }
    var key [32]byte
    copy(key[:], b)
    return key, nil
}
```

### Key Loading and Auto-Generation

```go
// LoadOrCreateKeypair loads the default keypair, or generates one if none exists.
// This is the primary entry point used by save/push flows.
//
// Returns (publicKey, privateKey, created, error) where created=true if a new
// keypair was generated.
func LoadOrCreateKeypair() (pub, priv [32]byte, created bool, err error) {
    pub, priv, err = LoadDefaultKeypair()
    if err == nil {
        return pub, priv, false, nil
    }
    if !errors.Is(err, ErrNoKeypair) {
        return [32]byte{}, [32]byte{}, false, err
    }
    // Auto-generate default keypair.
    pub, priv, err = GenerateKeypair()
    if err != nil {
        return [32]byte{}, [32]byte{}, false, err
    }
    if err := SaveKeypair("default", pub, priv); err != nil {
        return [32]byte{}, [32]byte{}, false, err
    }
    return pub, priv, true, nil
}

// LoadDefaultKeypair loads the user's default keypair from the platform-
// specific keys directory. Returns (publicKey, privateKey, error).
//
// Search order:
//   1. <keys_dir>/default.json
//   2. If no default, iterate named keypairs alphabetically, use first found.
//   3. If no keypairs exist, return ErrNoKeypair.
func LoadDefaultKeypair() ([32]byte, [32]byte, error)

// SaveKeypair writes a keypair to the keys directory.
// Creates the directory (0700) and file (0600) if they don't exist.
func SaveKeypair(name string, pub, priv [32]byte) error

// ErrNoKeypair is returned when no keypair is found on disk.
var ErrNoKeypair = errors.New("no keypair found")
```

### Recipient Resolution

```go
// ResolveRecipients resolves a list of recipient specifiers to public keys.
//
// Each specifier is resolved in order:
//   1. If it starts with "bento-pk-": parse as a literal public key
//   2. If it matches a name in bento.yaml recipients: use that key
//   3. If it matches a file in <keys_dir>/recipients/<name>.pub: read that file
//   4. Otherwise: return an error with actionable message
//
// The sender's own public key is always prepended (implicit self-recipient)
// and the list is deduplicated by public key bytes.
func ResolveRecipients(
    specifiers []string,
    configRecipients []ConfigRecipient,
    senderPub [32]byte,
) ([][32]byte, error)
```

**Resolution order for `--recipient alice`:**
1. Check `bento.yaml` `recipients` list for `name: alice`
2. Check `<keys_dir>/recipients/alice.pub`
3. If not found: error with `"unknown recipient \"alice\" — add with: bento keys add-recipient alice <bento-pk-...>"`

**Resolution order for `--recipient bento-pk-...`:**
1. Parse directly as a public key
2. If parse fails: error with `"invalid public key — must start with bento-pk-"`

### DEK Extraction for Wrapping

The current `EncryptSecrets()` returns the DEK as a formatted string. The
wrapping flow needs the raw 32-byte DEK. Refactor to return raw bytes:

```go
// EncryptSecrets encrypts secrets and returns the ciphertext and raw 32-byte
// DEK (for key wrapping). No formatted key string is produced.
func EncryptSecrets(secrets map[string]string) (ciphertext string, rawDEK [32]byte, err error)
```

The `rawDEK` is passed to `WrapDEK()` for each recipient.

### WrapDEK / UnwrapDEK

```go
package backend

import "golang.org/x/crypto/nacl/box"

// WrapDEK wraps a 32-byte DEK to a recipient's Curve25519 public key
// using NaCl authenticated boxes (crypto_box).
//
// Returns 72 bytes: 24-byte nonce || 48-byte box.Seal output.
func WrapDEK(dek [32]byte, recipientPub [32]byte, senderPriv [32]byte) ([]byte, error) {
    var nonce [24]byte
    if _, err := rand.Read(nonce[:]); err != nil {
        return nil, fmt.Errorf("generating nonce: %w", err)
    }
    out := box.Seal(nonce[:], dek[:], &nonce, &recipientPub, &senderPriv)
    return out, nil // 24 + 32 + box.Overhead(16) = 72 bytes
}

// UnwrapDEK unwraps a DEK using the recipient's private key and the
// sender's public key (from the envelope's "sender" field).
//
// Input: 72 bytes (24-byte nonce || 48-byte ciphertext).
func UnwrapDEK(wrapped []byte, senderPub, recipientPriv [32]byte) ([32]byte, error) {
    if len(wrapped) != 72 {
        return [32]byte{}, fmt.Errorf("invalid wrapped DEK: expected 72 bytes, got %d", len(wrapped))
    }
    var nonce [24]byte
    copy(nonce[:], wrapped[:24])
    plaintext, ok := box.Open(nil, wrapped[24:], &nonce, &senderPub, &recipientPriv)
    if !ok {
        return [32]byte{}, fmt.Errorf("DEK unwrap failed — wrong key or corrupted data")
    }
    if len(plaintext) != 32 {
        return [32]byte{}, fmt.Errorf("invalid DEK: expected 32 bytes, got %d", len(plaintext))
    }
    var dek [32]byte
    copy(dek[:], plaintext)
    return dek, nil
}

// MultiRecipientEnvelope is the on-disk/in-OCI format for wrapped secrets.
type MultiRecipientEnvelope struct {
    Version     int              `json:"v"`
    Sender      string           `json:"sender"`    // "bento-pk-..."
    Ciphertext  string           `json:"ciphertext"`
    WrappedKeys []WrappedKeyEntry `json:"wrappedKeys"`
}

type WrappedKeyEntry struct {
    Recipient  string `json:"recipient"`   // "bento-pk-..."
    WrappedDEK string `json:"wrappedDEK"`  // base64url(72 bytes)
}
```

### CLI: `bento keys` Subcommand

Register `keys` as a subcommand of the root command in `NewRootCmd()`
(`internal/cli/root.go`), alongside `save`, `open`, `push`, etc.

```go
// internal/cli/keys.go

func newKeysCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "keys",
        Short: "Manage Curve25519 keypairs for secret sharing",
    }
    cmd.AddCommand(
        newKeysGenerateCmd(),
        newKeysListCmd(),
        newKeysPublicCmd(),
        newKeysAddRecipientCmd(),
        newKeysRemoveRecipientCmd(),
    )
    return cmd
}
```

### Files

```
internal/keys/
  keys.go             # GenerateKeypair, Format*, Parse*, LoadOrCreate*, Save*
  keys_test.go        # Round-trip, invalid input, auto-generation, platform paths
  recipients.go       # ResolveRecipients, LoadRecipientFile, AddRecipient, RemoveRecipient
  recipients_test.go  # Resolution order, dedup, error cases

internal/secrets/backend/
  oci.go              # WrapDEK(), UnwrapDEK(), MultiRecipientEnvelope
                      # Refactor EncryptSecrets() to return rawDEK
                      # Remove FormatDataKey, ParseDataKey, symmetric key logic
  oci_test.go         # Wrapping round-trip, multi-recipient, envelope serialization

internal/cli/
  root.go             # Register newKeysCmd()
  keys.go             # bento keys generate|list|public|add-recipient|remove-recipient
  save_core.go        # LoadOrCreateKeypair, EncryptSecrets, wrap DEK, build envelope
  open.go             # Load private key, scan wrappedKeys, unwrap DEK
                      # Remove --secret-key flag and BENTO_SECRET_KEY env var
  push.go             # Wire in --recipient flag, display sealed-by info
                      # Remove symmetric key display

internal/config/
  config.go           # Add Recipients []ConfigRecipient field + validation
```

### What to Remove from Current Codebase

| File | Remove |
|------|--------|
| `internal/secrets/backend/oci.go` | `bento-sk-` prefix constant (as symmetric key), `FormatDataKey()`, `ParseDataKey()` |
| `internal/cli/open.go` | `--secret-key` flag, `BENTO_SECRET_KEY` env var, symmetric key decryption path |
| `internal/cli/push.go` | Symmetric key display (`"Recipient runs: bento open --secret-key ..."`) |
| `internal/cli/secrets.go` | `bento secrets export` symmetric envelope export (replace with key-based workflow) |
| `internal/manifest/annotations.go` | `AnnotationSecretsEncrypted` (replaced by `dev.bento.secrets.key-wrapping`) |
| Local storage | `.enc.json` files no longer written; plaintext `.json` still used for same-machine opens |

## Security Properties

| Property | Guarantee |
|----------|-----------|
| Forward secrecy per checkpoint | Each checkpoint gets a fresh DEK; wrapping doesn't change this |
| Recipient isolation | Each recipient's wrapped DEK is independent; compromising one doesn't affect others |
| No private keys in OCI | Only public keys appear in the envelope; private keys never leave `~/.bento/keys/` |
| Authenticated encryption | NaCl `crypto_box` provides authentication + confidentiality; sender identity is cryptographically bound |
| Sender provenance | Recipient can verify the wrapped DEK was produced by the holder of the sender's private key |
| Sender self-access | Sender is always a recipient; can always recover from registry via private key |
| Key size | 32-byte Curve25519 keys = 128-bit security level |
| No symmetric keys to leak | No `bento-dk-` strings in terminal history, Slack messages, or env vars |

### Threat Model

- **Registry compromise:** attacker gets ciphertext + wrapped DEKs + sender's
  public key. Cannot decrypt without a recipient's private key. The sender's
  public key is not sensitive — it only enables verification, not decryption.

- **Recipient compromise:** attacker gets one recipient's private key. Can
  decrypt only checkpoints wrapped to that recipient. Other recipients and
  other checkpoints (with different DEKs) are unaffected.

- **Sender impersonation:** an attacker who doesn't have the sender's private
  key cannot produce wrapped DEKs that pass `box.Open` with the sender's
  public key. Recipients can detect forged envelopes by checking the sender
  field against a trusted list.

- **Private key loss:** if the sender loses their private key and the local
  plaintext store, secrets are unrecoverable. Mitigation: the private key is
  a file (`~/.bento/keys/default.json`, 0600 permissions) — back it up like
  you would an SSH key.

- **Adding recipients after push:** requires re-wrapping the DEK (not
  re-encrypting the ciphertext). The sender must have the DEK (from local
  plaintext store or by unwrapping with their own private key) to wrap it
  for a new recipient. This is a `bento secrets rewrap` operation.

## User Flows

### First Save (Auto-Setup)

```
$ bento save -m "initial work"
Generated default keypair:
  Public key: bento-pk-<base64url, 43 chars>
  Saved to:   ~/.bento/keys/default.json

Scrubbed 1 secret(s):
  .mcp.json          aws-access-token
Scanning workspace...
  project:   2 files, 210B (changed)
Tagged: cp-1, latest
```

No setup step. No key to copy. Just works.

### Push (Solo)

```
$ bento push --include-secrets
Secrets sealed for 1 recipient(s):
  (self)  bento-pk-dG9tIG...
Pushing to ghcr.io/org/project...
Done.
```

### Open on Another Machine

```
# Copy private key to new machine first:
scp ~/.bento/keys/default.json newmachine:~/.bento/keys/

# Then open:
$ bento open ghcr.io/org/project:cp-1 ./workspace
Restoring checkpoint cp-1 (sequence 1)...
  Decrypting secrets with key "default"...
  .mcp.json          aws-access-token         OK
Hydrated 1 secret(s)
Restored to ./workspace
```

### Adding a Teammate

```
$ bento keys add-recipient alice bento-pk-<alice's public key>
Added recipient "alice"

$ bento push --include-secrets --recipient alice
Secrets sealed for 2 recipient(s):
  (self)   bento-pk-dG9tIG...
  alice    bento-pk-c2FsbH...
Pushing to ghcr.io/org/project...
Done.
```

### Open (No Matching Key)

```
$ bento open ghcr.io/org/project:cp-3 ./workspace
Restoring checkpoint cp-3 (sequence 3)...
  No matching private key found.

To restore secrets:
  1. Copy your private key from the original machine:
     scp original:~/.bento/keys/default.json ~/.bento/keys/
  2. Or ask the sender to re-push with your public key:
     bento keys public   # show your public key
     # sender runs: bento push --include-secrets --recipient <your-key>
```

## Testing

### Unit Tests

- `keys_test.go`: generate, format, parse round-trip; invalid key detection;
  auto-generation; platform-specific paths
- `oci_test.go`: WrapDEK/UnwrapDEK round-trip; multi-recipient wrap; wrong key
  fails; envelope serialization

### E2E Tests

1. **Auto-generation:**
   fresh workspace (no keys) → save → verify keypair created → inspect
   envelope has wrappedKeys with self

2. **Full cycle with key wrapping:**
   save → push --include-secrets → open on "different machine" (different
   keys dir, same private key) → verify secrets hydrated

3. **Multi-recipient:**
   wrap to 3 recipients → each can open independently → non-recipient cannot

4. **Self-recovery from registry:**
   save → push --include-secrets → delete local plaintext store → open from
   registry → verify secrets hydrated via private key unwrap

5. **No matching key:**
   open with wrong private key → clear error message with instructions

6. **Rewrap:**
   save → push → add recipient → rewrap → new recipient can open

## Future Extensions

- **`bento secrets rewrap`** — add/remove recipients without re-encrypting
  the ciphertext (requires DEK access via local store or private key unwrap)
- **Key rotation** — generate new keypair, re-wrap all active checkpoints
- **Team key management** — shared recipients list in a team config repo
- **Hardware key support** — PKCS#11 / YubiKey for private key storage
  (Curve25519 is supported by modern security keys)
- **`bento keys export` / `bento keys import`** — backup and restore keypairs
