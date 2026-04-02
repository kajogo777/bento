# Curve25519 Key Wrapping for Secret Sharing

**Version:** 0.1.0
**Status:** Proposal
**Authors:** George Fahmy
**Repository:** github.com/kajogo777/bento
**Related:** `secret-scrubbing.md`, `SPEC.md` §6

## Abstract

This proposal adds asymmetric key wrapping to bento's existing secret scrubbing
system using Curve25519 keypairs. The checkpoint's data encryption key (DEK) is
wrapped to one or more recipient public keys, enabling secret sharing without
out-of-band key exchange. The scrub-and-hydrate architecture is unchanged —
this proposal only replaces *how the DEK is delivered* to recipients.

## Motivation

Today, sharing secrets across machines requires the sender to communicate a
`bento-dk-...` symmetric key string out-of-band (Slack, email, etc.). This has
several friction points:

1. **Manual key exchange** — the sender must copy the key and send it to each
   recipient through a separate channel
2. **No access control** — anyone with the key can decrypt; there's no way to
   encrypt *to* a specific person
3. **No revocation** — once shared, the key can't be un-shared
4. **Team workflows** — in a team, every push requires re-sharing the key with
   every teammate

Curve25519 key wrapping solves all four: the sender encrypts the DEK to each
recipient's public key. Recipients decrypt with their private key. No key
strings to copy-paste. Public keys are short enough to put in a git config or
team manifest.

## Design Principles

1. **Additive, not replacing** — symmetric `--secret-key` flow remains fully
   supported; key wrapping is an additional sharing mechanism
2. **WireGuard-style keys** — Curve25519 public keys are 32 bytes = 44 chars
   in base64, short enough to copy-paste, put in Slack bios, or commit to a
   repo
3. **NaCl family** — uses `crypto/box` (Curve25519 + XSalsa20 + Poly1305)
   from the same NaCl family as the existing secretbox encryption; zero new
   dependencies
4. **Multi-recipient** — the DEK is wrapped once per recipient; adding a
   recipient is O(1) and doesn't touch the ciphertext or layers
5. **Sender is always a recipient** — the sender's own public key is
   implicitly added to the wrapped keys list, ensuring the sender never
   loses access to their own secrets (even after local store cleanup or
   machine migration)
6. **Zero config for local** — same-machine opens still use the local plaintext
   store; key wrapping only matters for cross-machine sharing

## Key Format

### Keypair Generation

```
$ bento keys generate
Private key: bento-sk-<base64(32 bytes)>   (44 chars after prefix)
Public key:  bento-pk-<base64(32 bytes)>    (44 chars after prefix)

Keys saved to ~/.bento/keys/default.json
```

**Key encoding:** raw base64url (no padding), matching the existing `bento-dk-`
convention. A Curve25519 public key is exactly 32 bytes → 43 base64url chars.

**Key prefixes:**
- `bento-pk-` — public key (safe to share, commit, publish)
- `bento-sk-` — secret/private key (never leaves the machine)
- `bento-dk-` — data encryption key (symmetric, for backward-compat sharing)

**Prefix migration:** the existing symmetric key format `bento-sk-` is renamed
to `bento-dk-`. Implementations MUST accept both prefixes on input (parsing)
for backward compatibility. New keys MUST be emitted with `bento-dk-`. The
`--secret-key` CLI flag accepts either prefix.

**Comparison with WireGuard:** WireGuard uses standard base64 (with padding) for
its Curve25519 keys, yielding 44-char strings. Bento uses base64url (no padding)
for URL/CLI safety, yielding 43-char strings. Same curve, same key size, same
copy-paste convenience.

### Key Storage

```
~/.bento/keys/
  default.json          # user's default keypair
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
gives recipients two things for free:

1. **Provenance** — the envelope proves the DEK was wrapped by the holder of
   the sender's private key. Recipients can verify who sealed the checkpoint
   without any separate signing infrastructure.
2. **Workspace lineage** — the sender's public key in the envelope metadata
   acts as an identity marker, enabling checkpoint history tracing across
   machines (e.g., "Alice created cp-1, Bob forked cp-2 from it").
3. **Trust decisions** — recipients can maintain a list of trusted sender keys
   and reject checkpoints from unknown senders before attempting decryption.

The sender already has a keypair (from `bento keys generate`), so authenticated
boxes add no extra setup cost. The sender's public key is stored in the envelope
— it's a public key, safe to include in plaintext.

### Wrapped Key Format

Each wrapped DEK is 72 bytes (24-byte nonce + 48-byte authenticated ciphertext).
Base64url-encoded: 96 chars.

### Multi-Recipient Envelope

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

The `ciphertext` field is identical to today's format — the secrets encrypted
with the DEK via NaCl secretbox. The `wrappedKeys` array replaces the need to
share `bento-dk-...` out-of-band.

## Save Flow Changes

```
Current save flow (unchanged):
  1. gitleaks scan → findings
  2. ScrubFile() → scrubbed content + replacements
  3. Pack scrubbed layers
  4. Store plaintext locally
  5. EncryptSecrets(secrets) → ciphertext + DEK
  6. Store encrypted envelope locally

New addition (after step 5):
  7. If sender has a keypair (any keypair in ~/.bento/keys/):
     a. Collect recipient list: explicit recipients (bento.yaml + --recipient)
        + sender's own public key (ALWAYS implicit)
     b. Deduplicate by public key (in case sender is also listed explicitly)
     c. For each recipient public key:
        - box.Seal(DEK, nonce, recipientPubKey, senderPrivKey) → wrappedDEK
     d. Build multi-recipient envelope:
        {sender, ciphertext, wrappedKeys: [{recipient, wrappedDEK}, ...]}
     e. Store envelope locally (replaces .enc.json)
  8. If sender has NO keypair AND recipients are configured:
     - Error: "recipients configured but no keypair found"
  9. If sender has NO keypair AND no recipients configured:
     - Symmetric-only flow (today's behavior, bento-dk-... key)
  10. On push --include-secrets:
      - Pack the envelope (wrapped or symmetric) into OCI layer
      - If wrapped: no --secret-key displayed (recipients use private keys)
      - If symmetric: display bento-dk-... key as before
```

**Key rule: having a keypair is sufficient to trigger wrapping.** The sender
doesn't need explicit recipients — wrapping to self alone ensures the sender
can always recover secrets via their private key, even after local store
cleanup, machine migration, or `bento gc`. This eliminates the failure mode
where a user pushes with `--include-secrets`, loses the local store, and has
no way to decrypt their own checkpoint.

## Open Flow Changes

```
Current open flow (unchanged):
  1. Try local plaintext store → if found, done
  2. Try --secret-key flag → decrypt ciphertext → done
  3. Try --secrets-file + --secret-key → done
  4. Fail with hint

New addition (between steps 1 and 2):
  1.5. Try key wrapping:
       a. Load user's private key from ~/.bento/keys/
       b. Read sender's public key from envelope
       c. Scan wrappedKeys for matching recipient public key
       d. If found: box.Open(wrappedDEK, nonce, senderPubKey, recipientPrivKey) → DEK
       e. DecryptSecrets(ciphertext, DEK) → secrets → done
```

The `--secret-key` flow remains as a fallback. Users without keypairs configured
can still use the symmetric key approach.

## CLI Changes

### New Commands

```bash
# Generate a new keypair
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
# Push with key wrapping (recipients from bento.yaml or flags)
bento push --include-secrets
bento push --include-secrets --recipient bento-pk-...
bento push --include-secrets --recipient alice --recipient bob

# Open auto-discovers private key
bento open ghcr.io/org/project:cp-1 ./workspace
# Falls back to --secret-key if no matching private key found
```

### New Flags

| Flag | Command | Purpose |
|------|---------|---------|
| `--recipient` | `push`, `save` | Add recipient public key or name |
| `--name` | `keys generate` | Name for the keypair |
| `--private-key` | `open` | Explicit path to private key file |

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
| `dev.bento.secrets.key-wrapping` | `"curve25519"` when wrapped keys are present |
| `dev.bento.secrets.sender` | Sender's public key (`bento-pk-...`) for provenance |

### Envelope Storage

The multi-recipient envelope replaces the current `{ciphertext, secretKey}`
format in the encrypted secrets layer. The format is backward-compatible:

- **Old format** (no `wrappedKeys`): requires `--secret-key` flag
- **New format** (has `wrappedKeys`): auto-decrypts with matching private key,
  falls back to `--secret-key`

Detection: if the parsed envelope JSON has a `wrappedKeys` field, use key
wrapping; otherwise, fall back to symmetric key.

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
    "golang.org/x/crypto/nacl/box"
)

// GenerateKeypair creates a new Curve25519 keypair using crypto/rand.
// Returns the public and private keys as 32-byte arrays.
func GenerateKeypair() (publicKey, privateKey [32]byte, err error) {
    pub, priv, err := box.GenerateKey(rand.Reader)
    if err != nil {
        return [32]byte{}, [32]byte{}, fmt.Errorf("generating keypair: %w", err)
    }
    return *pub, *priv, nil
}

const (
    PrefixPublicKey  = "bento-pk-"
    PrefixPrivateKey = "bento-sk-"
    PrefixDataKey    = "bento-dk-"
    // Legacy prefix — accepted on input, never emitted.
    LegacyPrefixSymmetricKey = "bento-sk-"
)

// FormatPublicKey encodes a public key as "bento-pk-<base64url>".
func FormatPublicKey(key [32]byte) string {
    return PrefixPublicKey + base64.RawURLEncoding.EncodeToString(key[:])
}

// FormatPrivateKey encodes a private key as "bento-sk-<base64url>".
func FormatPrivateKey(key [32]byte) string {
    return PrefixPrivateKey + base64.RawURLEncoding.EncodeToString(key[:])
}

// FormatDataKey encodes a symmetric DEK as "bento-dk-<base64url>".
func FormatDataKey(key [32]byte) string {
    return PrefixDataKey + base64.RawURLEncoding.EncodeToString(key[:])
}

// ParsePublicKey decodes a "bento-pk-..." string to 32 bytes.
// Returns an error if the prefix is wrong or the key is not 32 bytes.
func ParsePublicKey(s string) ([32]byte, error) {
    return parseKey(s, PrefixPublicKey, "public key")
}

// ParsePrivateKey decodes a "bento-sk-..." string to 32 bytes.
// Returns an error if the prefix is wrong or the key is not 32 bytes.
func ParsePrivateKey(s string) ([32]byte, error) {
    return parseKey(s, PrefixPrivateKey, "private key")
}

// ParseDataKey decodes a "bento-dk-..." or legacy "bento-sk-..." string
// to 32 bytes. Accepts both prefixes for backward compatibility.
func ParseDataKey(s string) ([32]byte, error) {
    if strings.HasPrefix(s, PrefixDataKey) {
        return parseKey(s, PrefixDataKey, "data key")
    }
    // Accept legacy bento-sk- prefix for backward compat with old envelopes.
    if strings.HasPrefix(s, LegacyPrefixSymmetricKey) {
        return parseKey(s, LegacyPrefixSymmetricKey, "data key (legacy)")
    }
    return [32]byte{}, fmt.Errorf("invalid data key: must start with %q or %q",
        PrefixDataKey, LegacyPrefixSymmetricKey)
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

### Key Loading and Storage

```go
// LoadDefaultKeypair loads the user's default keypair from the platform-
// specific keys directory. Returns (publicKey, privateKey, error).
//
// Search order:
//   1. ~/.bento/keys/default.json (macOS)
//      ~/.local/share/bento/keys/default.json (Linux)
//      %LOCALAPPDATA%\bento\keys\default.json (Windows)
//   2. If no default, iterate named keypairs alphabetically, use first found.
//   3. If no keypairs exist, return ErrNoKeypair.
func LoadDefaultKeypair() ([32]byte, [32]byte, error)

// SaveKeypair writes a keypair to the keys directory.
// Creates the directory (0700) and file (0600) if they don't exist.
func SaveKeypair(name string, pub, priv [32]byte) error

// ErrNoKeypair is returned when no keypair is found on disk.
var ErrNoKeypair = errors.New("no keypair found — run 'bento keys generate' first")
```

**Platform-specific key paths** (mirrors the existing secrets directory
convention from `internal/secrets/backend/local.go`):

| Platform | Keys directory |
|----------|---------------|
| macOS | `~/.bento/keys/` |
| Linux | `~/.local/share/bento/keys/` (or `$XDG_DATA_HOME/bento/keys/`) |
| Windows | `%LOCALAPPDATA%\bento\keys\` |

### Recipient Resolution

When `--recipient` flags or `bento.yaml` recipients are provided, the
implementation resolves each entry to a public key:

```go
// ResolveRecipients resolves a list of recipient specifiers to public keys.
//
// Each specifier is resolved in order:
//   1. If it starts with "bento-pk-": parse as a literal public key
//   2. If it matches a name in bento.yaml recipients: use that key
//   3. If it matches a file in ~/.bento/keys/recipients/<name>.pub: read that file
//   4. Otherwise: return an error with actionable message
//
// The sender's own public key is always appended (implicit self-recipient)
// and the list is deduplicated by public key bytes.
func ResolveRecipients(
    specifiers []string,
    configRecipients []ConfigRecipient,
    senderPub [32]byte,
) ([][32]byte, error)
```

**Resolution order for `--recipient alice`:**
1. Check `bento.yaml` `recipients` list for `name: alice`
2. Check `~/.bento/keys/recipients/alice.pub`
3. If not found: error with `"unknown recipient \"alice\" — add with: bento keys add-recipient alice <bento-pk-...>"`

**Resolution order for `--recipient bento-pk-...`:**
1. Parse directly as a public key
2. If parse fails: error with `"invalid public key — must start with bento-pk-"`

### Error Handling: Keypair Presence on Save

The wrapping trigger is the **presence of a keypair**, not the presence of
explicit recipients. The decision matrix:

| Has keypair? | Recipients configured? | Behavior |
|---|---|---|
| No | No | Symmetric only (today's flow, `bento-dk-...`) |
| No | Yes | **Error:** "recipients configured but no keypair found" |
| Yes | No | Wrap DEK to **self only** (sender is implicit recipient) |
| Yes | Yes | Wrap DEK to all recipients + self |

When recipients are configured but the sender has no keypair:

```
$ bento save -m "my work"
Error: recipients configured but no keypair found.

Generate a keypair first:
  bento keys generate

Then retry:
  bento save -m "my work"
```

The save MUST fail — not silently skip wrapping, not auto-generate. The user
must explicitly generate a keypair because it's a long-lived identity. The
error message tells them exactly what to do.

When the sender has a keypair but no explicit recipients, the save proceeds
normally and wraps the DEK to the sender only. This ensures the sender can
always recover their own secrets via their private key.

### DEK Extraction for Wrapping

The current `EncryptSecrets()` returns the DEK as a formatted string
(`bento-dk-...`). The wrapping flow needs the raw 32-byte DEK. Two approaches:

**Option A (recommended):** refactor `EncryptSecrets` to also return raw bytes:

```go
// EncryptSecrets encrypts secrets and returns the ciphertext, formatted key
// string, and raw 32-byte DEK (for key wrapping).
func EncryptSecrets(secrets map[string]string) (ciphertext, formattedKey string, rawDEK [32]byte, err error)
```

**Option B:** parse the formatted key back to bytes using `ParseDataKey()`.
This works but is a round-trip through string encoding for no reason.

The implementation SHOULD use Option A. The `rawDEK` is passed to `WrapDEK()`
for each recipient. The `formattedKey` is still stored in the local `.enc.json`
for backward compatibility with the `--secret-key` flow.

### WrapDEK / UnwrapDEK

```go
package backend

import "golang.org/x/crypto/nacl/box"

// WrapDEK wraps a 32-byte DEK to a recipient's Curve25519 public key
// using NaCl authenticated boxes (crypto_box).
//
// The sender's keypair is required for authenticated encryption.
// Returns 72 bytes: 24-byte nonce || 48-byte box.Seal output.
func WrapDEK(dek [32]byte, recipientPub [32]byte, senderPub, senderPriv [32]byte) ([]byte, error) {
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
// Returns the 32-byte DEK or an error if decryption fails.
func UnwrapDEK(wrapped []byte, senderPub, recipientPub, recipientPriv [32]byte) ([32]byte, error) {
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
    Sender      string           `json:"sender"`              // "bento-pk-..." (sender's public key)
    Ciphertext  string           `json:"ciphertext"`
    WrappedKeys []WrappedKeyEntry `json:"wrappedKeys,omitempty"`
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
No keypairs found. Generate one with:
  bento keys generate
```

### Files

```
internal/keys/
  keys.go             # GenerateKeypair, Format*, Parse*, Load*, Save*
  keys_test.go        # Round-trip, invalid input, platform paths
  recipients.go       # ResolveRecipients, LoadRecipientFile, AddRecipient, RemoveRecipient
  recipients_test.go  # Resolution order, dedup, error cases

internal/secrets/backend/
  oci.go              # Add WrapDEK(), UnwrapDEK(), MultiRecipientEnvelope
                      # Modify EncryptSecrets() to return rawDEK
                      # Modify Put()/Get() to handle both old and new envelope formats
                      # Update prefix: emit bento-dk-, accept bento-sk- on parse
  oci_test.go         # Wrapping round-trip, multi-recipient, backward compat

internal/cli/
  root.go             # Register newKeysCmd()
  keys.go             # bento keys generate|list|public|add-recipient|remove-recipient
  save_core.go        # After EncryptSecrets: resolve recipients, wrap DEK, build envelope
  open.go             # Before --secret-key fallback: try key wrapping auto-discovery
  push.go             # Wire in --recipient flag, display sealed-by info

internal/config/
  config.go           # Add Recipients []ConfigRecipient field + validation
```

## Migration & Backward Compatibility

| Scenario | Behavior |
|----------|----------|
| Old bento opens new envelope (with `wrappedKeys`) | Ignores `wrappedKeys`, prompts for `--secret-key` as before |
| New bento opens old envelope (no `wrappedKeys`) | Falls back to `--secret-key` as before |
| No recipients configured | Behaves exactly like today (symmetric key only) |
| Mix of wrapped + symmetric | Both work; `--secret-key` always accepted as override |

No breaking changes. No migration needed. The feature is purely additive.

## Security Properties

| Property | Guarantee |
|----------|-----------|
| Forward secrecy per checkpoint | Each checkpoint gets a fresh DEK; wrapping doesn't change this |
| Recipient isolation | Each recipient's wrapped DEK is independent; compromising one doesn't affect others |
| No private keys in OCI | Only public keys appear in the envelope; private keys never leave `~/.bento/keys/` |
| Authenticated encryption | NaCl `crypto_box` provides authentication + confidentiality; sender identity is cryptographically bound |
| Sender provenance | Recipient can verify the wrapped DEK was produced by the holder of the sender's private key |
| Sender self-access | Sender is always an implicit recipient; cannot accidentally lock themselves out |
| Key size | 32-byte Curve25519 keys = 128-bit security level |

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

- **Adding recipients after push:** requires re-wrapping the DEK (not
  re-encrypting the ciphertext). The sender must have the DEK (from local
  store) and their private key to wrap it for a new recipient. This is a
  `bento secrets rewrap` operation.

## User Flows

### First-Time Setup

```
$ bento keys generate
Generated keypair "default":
  Public key:  bento-pk-<base64url, 43 chars>
  Private key: saved to ~/.bento/keys/default.json

Share your public key with teammates:
  bento-pk-<base64url, 43 chars>
```

### Adding a Teammate

```
$ bento keys add-recipient alice bento-pk-<alice's public key>
Added recipient "alice"

# Or in bento.yaml:
recipients:
  - name: alice
    key: bento-pk-<alice's public key>
```

### Push with Key Wrapping

```
$ bento push --include-secrets
Wrapped secrets for 2 recipient(s):
  alice  bento-pk-c2FsbH...
  bob    bento-pk-Ym9iIH...
Sealed by: bento-pk-dG9tIG... (default)
Pushing to ghcr.io/org/project...
Done.

Recipients can open with:
  bento open ghcr.io/org/project:cp-3 ./workspace
  (auto-decrypts if their private key is in ~/.bento/keys/)
```

### Open with Auto-Discovery

```
$ bento open ghcr.io/org/project:cp-3 ./workspace
Restoring checkpoint cp-3 (sequence 3)...
  Decrypting secrets with key "default"...
  .mcp.json          aws-access-token         OK
Hydrated 1 secret(s)
Restored to ./workspace
```

### Fallback to Symmetric Key

```
$ bento open ghcr.io/org/project:cp-3 ./workspace
Restoring checkpoint cp-3 (sequence 3)...
  No matching private key found for wrapped recipients.

To restore secrets, either:
  1. Generate a keypair and ask the sender to re-push with your public key:
     bento keys generate
  2. Use the symmetric secret key:
     bento open ghcr.io/org/project:cp-3 ./workspace --secret-key <KEY>
```

## Testing

### Unit Tests

- `keys_test.go`: generate, format, parse round-trip; invalid key detection
- `oci_test.go`: WrapDEK/UnwrapDEK round-trip; multi-recipient wrap; wrong key
  fails; envelope serialization; backward compat with old format

### E2E Tests

1. **Full cycle with key wrapping:**
   generate keypair → save → push --include-secrets --recipient → open on
   "different machine" (different keys dir) → verify secrets hydrated

2. **Multi-recipient:**
   wrap to 3 recipients → each can open independently → non-recipient cannot

3. **Backward compatibility:**
   new bento opens old-format envelope with --secret-key → works

4. **Fallback:**
   open with no matching key → falls back to --secret-key prompt

5. **Rewrap:**
   save → push → add recipient → rewrap → new recipient can open

## Future Extensions

- **`bento secrets rewrap`** — add/remove recipients without re-encrypting
  the ciphertext (requires local DEK access)
- **Key rotation** — generate new keypair, re-wrap all active checkpoints
- **Team key management** — shared recipients list in a team config repo
- **Hardware key support** — PKCS#11 / YubiKey for private key storage
  (Curve25519 is supported by modern security keys)
