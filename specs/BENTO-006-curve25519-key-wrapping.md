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
`bento-sk-...` symmetric key string out-of-band (Slack, email, etc.). This has
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
5. **Zero config for local** — same-machine opens still use the local plaintext
   store; key wrapping only matters for cross-machine sharing

## Key Format

### Keypair Generation

```
$ bento keys generate
Private key: bento-priv-<base64(32 bytes)>   (44 chars after prefix)
Public key:  bento-pub-<base64(32 bytes)>    (44 chars after prefix)

Keys saved to ~/.bento/keys/default.json
```

**Key encoding:** raw base64url (no padding), matching the existing `bento-sk-`
convention. A Curve25519 public key is exactly 32 bytes → 43 base64url chars.

**Key prefixes:**
- `bento-pub-` — public key (safe to share, commit, publish)
- `bento-priv-` — private key (never leaves the machine)

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
  "publicKey": "bento-pub-<base64url>",
  "privateKey": "bento-priv-<base64url>",
  "created": "2026-04-01T12:00:00Z"
}
```

File permissions: `0600`. Directory permissions: `0700`.

### Known Recipients

```
~/.bento/keys/recipients/
  <name>.pub             # plain text, one public key per file
```

Or in `bento.yaml`:

```yaml
recipients:
  - name: alice
    key: bento-pub-<base64url>
  - name: bob
    key: bento-pub-<base64url>
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
  "sender": "bento-pub-<sender's public key>",
  "ciphertext": "<base64url(nonce || secretbox.Seal(secrets))>",
  "wrappedKeys": [
    {
      "recipient": "bento-pub-<base64url>",
      "wrappedDEK": "<base64url(72 bytes)>"
    },
    {
      "recipient": "bento-pub-<base64url>",
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
share `bento-sk-...` out-of-band.

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
  7. If recipients configured (bento.yaml or --recipient flags):
     a. For each recipient public key:
        - box.Seal(DEK, nonce, recipientPubKey, senderPrivKey) → wrappedDEK
     b. Build multi-recipient envelope:
        {sender, ciphertext, wrappedKeys: [{recipient, wrappedDEK}, ...]}
     c. Store envelope locally (replaces .enc.json)
  8. On push --include-secrets:
     - Pack the multi-recipient envelope into OCI layer
     - No --secret-key displayed (recipients use their private keys)
```

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
bento keys add-recipient <name> <bento-pub-...>

# Remove a recipient
bento keys remove-recipient <name>
```

### Modified Commands

```bash
# Push with key wrapping (recipients from bento.yaml or flags)
bento push --include-secrets
bento push --include-secrets --recipient bento-pub-...
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
    key: bento-pub-dG9tIGlzIGEgZ29vZCBjYXQgYW5kIGhlIGxpa2Vz
  - name: bob
    key: bento-pub-c2FsbHkgaXMgYSBnb29kIGRvZyBhbmQgc2hlIGxp
```

### Validation

- `recipients[].key` MUST start with `bento-pub-`
- Decoded key MUST be exactly 32 bytes
- `recipients[].name` MUST be unique within the list
- Duplicate public keys across different names: warning (not error)

## OCI Manifest Changes

### Annotations

New annotation on the secrets layer descriptor:

| Key | Description |
|-----|-------------|
| `dev.bento.secrets.key-wrapping` | `"curve25519"` when wrapped keys are present |
| `dev.bento.secrets.sender` | Sender's public key (`bento-pub-...`) for provenance |

### Envelope Storage

The multi-recipient envelope replaces the current `{ciphertext, secretKey}`
format in the encrypted secrets layer. The format is backward-compatible:

- **Old format** (no `wrappedKeys`): requires `--secret-key` flag
- **New format** (has `wrappedKeys`): auto-decrypts with matching private key,
  falls back to `--secret-key`

Detection: if the parsed envelope JSON has a `wrappedKeys` field, use key
wrapping; otherwise, fall back to symmetric key.

## Implementation

### Files

```
internal/secrets/backend/
  oci.go              # Add WrapDEK(), UnwrapDEK(), multi-recipient envelope
  oci_test.go         # Test wrapping/unwrapping, multi-recipient, round-trip

internal/keys/
  keys.go             # Keypair generation, loading, storage
  keys_test.go
  recipients.go       # Recipient management (add, remove, list, resolve)
  recipients_test.go

internal/cli/
  keys.go             # bento keys generate|list|public|add-recipient|remove-recipient
  save_core.go        # Wire in recipient resolution + DEK wrapping
  open.go             # Wire in private key discovery + DEK unwrapping
  push.go             # Wire in --recipient flag

internal/config/
  config.go           # Add Recipients field + validation
```

### Core Functions

```go
package keys

// GenerateKeypair creates a new Curve25519 keypair.
func GenerateKeypair() (publicKey, privateKey [32]byte, err error)

// FormatPublicKey encodes a public key as "bento-pub-<base64url>".
func FormatPublicKey(key [32]byte) string

// FormatPrivateKey encodes a private key as "bento-priv-<base64url>".
func FormatPrivateKey(key [32]byte) string

// ParsePublicKey decodes a "bento-pub-..." string to 32 bytes.
func ParsePublicKey(s string) ([32]byte, error)

// ParsePrivateKey decodes a "bento-priv-..." string to 32 bytes.
func ParsePrivateKey(s string) ([32]byte, error)

// LoadPrivateKey loads the user's private key from ~/.bento/keys/.
// Tries "default" first, then iterates named keys.
func LoadPrivateKey() ([32]byte, [32]byte, error) // returns (pub, priv, err)
```

```go
package backend

// WrapDEK wraps a 32-byte DEK to a recipient's Curve25519 public key
// using NaCl authenticated boxes. The sender's public key is included
// in the envelope for provenance verification.
func WrapDEK(dek [32]byte, recipientPub [32]byte, senderPub, senderPriv [32]byte) ([]byte, error)

// UnwrapDEK unwraps a DEK using the recipient's private key and the
// sender's public key (from the envelope).
func UnwrapDEK(wrapped []byte, senderPub, recipientPub, recipientPriv [32]byte) ([32]byte, error)

// MultiRecipientEnvelope is the on-disk/in-OCI format for wrapped secrets.
type MultiRecipientEnvelope struct {
    Version     int              `json:"v"`
    Sender      string           `json:"sender"`              // "bento-pub-..." (sender's public key)
    Ciphertext  string           `json:"ciphertext"`
    WrappedKeys []WrappedKeyEntry `json:"wrappedKeys,omitempty"`
}

type WrappedKeyEntry struct {
    Recipient  string `json:"recipient"`   // "bento-pub-..."
    WrappedDEK string `json:"wrappedDEK"`  // base64url(72 bytes)
}
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
  Public key:  bento-pub-dG9tIGlzIGEgZ29vZCBjYXQgYW5kIGhlIGxpa2Vz
  Private key: saved to ~/.bento/keys/default.json

Share your public key with teammates:
  bento-pub-dG9tIGlzIGEgZ29vZCBjYXQgYW5kIGhlIGxpa2Vz
```

### Adding a Teammate

```
$ bento keys add-recipient alice bento-pub-c2FsbHkgaXMgYSBnb29kIGRvZyBhbmQgc2hlIGxp
Added recipient "alice"

# Or in bento.yaml:
recipients:
  - name: alice
    key: bento-pub-c2FsbHkgaXMgYSBnb29kIGRvZyBhbmQgc2hlIGxp
```

### Push with Key Wrapping

```
$ bento push --include-secrets
Wrapped secrets for 2 recipient(s):
  alice  bento-pub-c2FsbH...
  bob    bento-pub-Ym9iIH...
Sealed by: bento-pub-dG9tIG... (default)
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
