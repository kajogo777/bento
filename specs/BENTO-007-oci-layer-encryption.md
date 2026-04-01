# OCI Layer Encryption

**Version:** 0.1.0
**Status:** Proposal
**Authors:** George Fahmy
**Repository:** github.com/kajogo777/bento
**Related:** `secret-scrubbing.md`, `BENTO-006-curve25519-key-wrapping.md`, `SPEC.md` §3.3

## Abstract

This proposal adds optional full-layer encryption to bento checkpoints using
the OCI container image encryption specification (as implemented by
`containers/ocicrypt`). When enabled, entire layer blobs are encrypted before
storage, making all layer content — not just secrets — confidential. This is
a complementary feature to secret scrubbing, targeting use cases where the
workspace content itself is sensitive.

## Motivation

Secret scrubbing (current) protects detected secrets while keeping layers
inspectable. This is the right default for most users. However, some use cases
require confidentiality of the entire workspace:

1. **Proprietary code** — a workspace containing trade secrets, unreleased
   product code, or licensed third-party code that must not be visible in a
   shared registry
2. **Regulated data** — workspaces with PII, PHI, or financial data embedded
   in config files, test fixtures, or agent memory that may not be caught by
   gitleaks pattern matching
3. **Confidential computing** — environments where the registry is untrusted
   and all content must be encrypted at rest
4. **Export control / DRM** — restricting which machines can unpack a
   checkpoint, tied to key distribution

**This proposal does NOT replace secret scrubbing.** Scrub mode remains the
default and recommended approach. Layer encryption is an opt-in addition for
users who need full confidentiality at the cost of inspectability.

## Design Principles

1. **Opt-in only** — layer encryption is never automatic; users must
   explicitly enable it
2. **Scrub mode is orthogonal** — secret scrubbing runs regardless of layer
   encryption; the two features compose independently
3. **Standard media types** — encrypted layers use the `+encrypted` suffix
   from the OCI encryption spec (`application/vnd.oci.image.layer.v1.tar+gzip+encrypted`)
4. **Selective encryption** — users can choose which layers to encrypt (e.g.,
   encrypt `project` but leave `deps` unencrypted for cache reuse)
5. **Key wrapping reuse** — uses the same Curve25519 key wrapping from
   BENTO-006 for recipient management, avoiding a second key management system

## Relationship to Existing Features

```
┌─────────────────────────────────────────────────────────┐
│                    Bento Checkpoint                      │
│                                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │    deps      │  │    agent    │  │     project     │ │
│  │  (layer 1)   │  │  (layer 2)  │  │    (layer 3)    │ │
│  └──────┬───────┘  └──────┬──────┘  └───────┬─────────┘ │
│         │                 │                  │           │
│    ┌────▼─────────────────▼──────────────────▼────┐     │
│    │          Secret Scrubbing (always)            │     │
│    │  Replaces detected secrets with placeholders  │     │
│    └────┬─────────────────┬──────────────────┬────┘     │
│         │                 │                  │           │
│    ┌────▼─────────────────▼──────────────────▼────┐     │
│    │       Layer Encryption (opt-in, this spec)    │     │
│    │  Encrypts entire layer blobs with AES-256-CTR │     │
│    └──────────────────────────────────────────────┘     │
│                                                         │
│  Secret scrubbing protects secrets.                     │
│  Layer encryption protects everything else.             │
│  They compose: scrub first, then encrypt.               │
└─────────────────────────────────────────────────────────┘
```

### Feature Matrix

| Scenario | Scrub mode | Layer encryption | Result |
|----------|-----------|-----------------|--------|
| Default (most users) | ✓ | ✗ | Secrets scrubbed, layers inspectable |
| Full confidentiality | ✓ | ✓ | Secrets scrubbed AND layers encrypted |
| Encryption only (not recommended) | ✗ | ✓ | Layers encrypted but secrets in plaintext inside encrypted blob |

**Recommendation:** always keep scrub mode enabled. Layer encryption is defense-
in-depth, not a replacement.

## Specification Status Warning

> **⚠ The OCI image encryption specification is NOT yet merged into the
> official OCI image-spec.** It has been an open pull request
> ([opencontainers/image-spec#775](https://github.com/opencontainers/image-spec/pull/775))
> since 2019. The `containers/ocicrypt` library implements the proposed spec
> and is used in production by containerd/imgcrypt, CRI-O, and skopeo, but
> the media types and annotation keys are not part of the ratified standard.
>
> **Implications for bento:**
> - Encrypted layers use non-standard media types that some registries or
>   tools may not handle correctly
> - The spec could change before ratification (though it has been stable
>   since 2021)
> - Tools that don't understand `+encrypted` media types will fail to
>   process encrypted layers
>
> This proposal accepts these trade-offs for users who explicitly opt in.
> The default (scrub mode only) uses fully standard OCI media types.

## Cryptographic Design

### Layer Encryption Scheme

Following the ocicrypt specification:

```
Encrypt:
  1. Generate random 256-bit DEK (data encryption key) per layer
  2. Generate random 128-bit IV (initialization vector)
  3. Encrypt layer blob: AES-256-CTR(DEK, IV, plaintext_layer)
  4. Compute HMAC-SHA256 over ciphertext for integrity
  5. Wrap DEK for each recipient (see Key Wrapping below)
  6. Store wrapped keys in layer descriptor annotations
  7. Change media type: append "+encrypted" suffix

Decrypt:
  1. Read wrapped keys from layer descriptor annotations
  2. Unwrap DEK using recipient's private key
  3. Verify HMAC-SHA256
  4. Decrypt layer blob: AES-256-CTR(DEK, IV, ciphertext)
  5. Verify layer digest matches diff_id in config
```

### Key Wrapping

Two modes, selectable by the user:

#### Mode 1: Curve25519 (recommended, requires BENTO-006)

Reuses the Curve25519 keypair infrastructure from BENTO-006. The per-layer DEK
is wrapped using NaCl `crypto_box_seal`, identical to how the secret envelope
DEK is wrapped.

```
Annotation key: dev.bento.enc.keys.curve25519
Value: base64(JSON array of {recipient, wrappedDEK} objects)
```

**Advantages:** consistent key management with secret sharing; short keys;
no external dependencies beyond `golang.org/x/crypto/nacl`.

#### Mode 2: ocicrypt-native (for ecosystem compatibility)

Uses the `containers/ocicrypt` library directly, supporting its full range of
key wrapping protocols:

- **jwe** — JSON Web Encryption (RSA-OAEP, ECDH-ES)
- **pkcs7** — PKCS#7 / CMS (RSA, certificates)
- **pgp** — OpenPGP (RSA, Curve25519 via age-compatible keys)
- **pkcs11** — Hardware security modules
- **keyprovider** — External binary or gRPC service

```
Annotation keys (per ocicrypt spec):
  org.opencontainers.image.enc.keys.jwe
  org.opencontainers.image.enc.keys.pkcs7
  org.opencontainers.image.enc.keys.pgp
  org.opencontainers.image.enc.pubopts
  org.opencontainers.image.enc.keys.provider.<name>
```

**Advantages:** interoperable with containerd/imgcrypt, skopeo, CRI-O;
supports enterprise key management (HSMs, KMS).

**Disadvantages:** heavy dependency tree (`containers/ocicrypt` pulls in GPG,
PKCS11, and more); complex configuration; overkill for most bento users.

### Recommended Default

Mode 1 (Curve25519) is the recommended default for bento. Mode 2 is available
for users who need ecosystem interop or enterprise key management. The
implementation SHOULD support both but MAY ship Mode 1 first and add Mode 2
later.

## Media Types

### Encrypted Layer Media Types

| Original | Encrypted |
|----------|-----------|
| `application/vnd.oci.image.layer.v1.tar+gzip` | `application/vnd.oci.image.layer.v1.tar+gzip+encrypted` |
| `application/vnd.oci.image.layer.v1.tar+zstd` | `application/vnd.oci.image.layer.v1.tar+zstd+encrypted` |

### Detection

A layer is encrypted if and only if its media type ends with `+encrypted`.
Implementations MUST check the media type suffix, not annotations, to determine
whether decryption is needed.

## OCI Manifest Changes

### Encrypted Layer Descriptor

```json
{
  "mediaType": "application/vnd.oci.image.layer.v1.tar+gzip+encrypted",
  "digest": "sha256:encrypted_digest...",
  "size": 93324000,
  "annotations": {
    "org.opencontainers.image.title": "project",
    "dev.bento.layer.file-count": "42",
    "dev.bento.enc.keys.curve25519": "<base64(wrapped keys JSON)>",
    "org.opencontainers.image.enc.pubopts": "<base64(cipher options JSON)>"
  }
}
```

### Cipher Options

Stored in `org.opencontainers.image.enc.pubopts` annotation:

```json
{
  "cipher": "AES_256_CTR_HMAC_SHA256",
  "hmac": "<base64(HMAC)>",
  "cipheroptions": {
    "nonce": "<base64(IV)>",
    "symkeylen": 32
  }
}
```

### Manifest Annotations

| Key | Description |
|-----|-------------|
| `dev.bento.encryption` | `"curve25519"` or `"ocicrypt"` — which mode is active |
| `dev.bento.encryption.layers` | Comma-separated list of encrypted layer names (e.g., `"agent,project"`) |

## Configuration

### `bento.yaml`

```yaml
# Layer encryption (opt-in)
encryption:
  # Enable layer encryption
  enabled: true

  # Which layers to encrypt (default: all)
  # Use "all" or list specific layer names
  layers:
    - agent
    - project
  # Leaving "deps" unencrypted preserves layer dedup across checkpoints

  # Key wrapping mode: "curve25519" (default) or "ocicrypt"
  mode: curve25519

  # Recipients (can also use bento.yaml recipients from BENTO-006)
  # If omitted, uses the recipients list from the top-level config
```

### Validation

- `encryption.enabled` MUST be a boolean
- `encryption.layers` MUST be `"all"` or a list of valid layer names
- `encryption.mode` MUST be `"curve25519"` or `"ocicrypt"`
- If `mode` is `"curve25519"`, at least one recipient MUST be configured
  (in `bento.yaml` or via `--recipient` flag)
- If `mode` is `"ocicrypt"`, the `containers/ocicrypt` library MUST be
  available (build tag or runtime check)

## CLI Changes

### Modified Commands

```bash
# Save with layer encryption
bento save --encrypt-layers
bento save --encrypt-layers=agent,project

# Push encrypted layers
bento push --include-secrets --encrypt-layers

# Open auto-decrypts if private key available
bento open ghcr.io/org/project:cp-1 ./workspace

# Inspect shows encryption status
bento inspect cp-1
```

### New Flags

| Flag | Command | Purpose |
|------|---------|---------|
| `--encrypt-layers` | `save`, `push` | Enable layer encryption; optionally specify which layers |
| `--decrypt-key` | `open` | Explicit private key for layer decryption (overrides auto-discovery) |

### Inspect Output

```
$ bento inspect cp-3
Checkpoint: cp-3 (sequence 3)
Created:    2026-04-01T12:00:00Z
Message:    "added auth module"
Extensions: claude-code, node
Secrets:    1 scrubbed (aws-access-token)
Encryption: curve25519 (agent, project layers)

Layers:
  deps       93.3 MB  1204 files  sha256:333...
  agent      64.0 KB     8 files  sha256:222...  🔒 encrypted
  project   128.0 KB    42 files  sha256:111...  🔒 encrypted
```

## Save Flow

```
1. Load bento.yaml
2. Resolve extensions → layer definitions
3. Scan workspace → assign files to layers
4. Secret scan + scrub (unchanged — always runs)
5. Pack layers (tar+gzip) using scrubbed overrides
6. IF encryption.enabled:
   a. Resolve recipients (bento.yaml + --recipient flags)
   b. For each layer in encryption.layers:
      i.   Generate random 256-bit DEK
      ii.  Generate random 128-bit IV
      iii. Encrypt layer blob: AES-256-CTR(DEK, IV, layer_bytes)
      iv.  Compute HMAC-SHA256(DEK, ciphertext)
      v.   For each recipient: wrap DEK (crypto_box_seal or ocicrypt)
      vi.  Replace layer blob with encrypted blob
      vii. Update media type: append "+encrypted"
      viii.Store wrapped keys + cipher opts in layer annotations
   c. Unencrypted layers: left as-is (standard media type)
7. Build OCI config + manifest
8. Store to local OCI layout
```

## Open Flow

```
1. Read manifest
2. For each layer:
   a. IF media type ends with "+encrypted":
      i.   Load user's private key
      ii.  Find matching wrapped key in annotations
      iii. Unwrap DEK
      iv.  Verify HMAC
      v.   Decrypt layer blob
      vi.  Verify decrypted digest matches diff_id
   b. ELSE: extract as normal
3. Secret hydration (unchanged — runs after layer extraction)
```

## Implementation

### Files

```
internal/encryption/
  encrypt.go          # EncryptLayer(), DecryptLayer()
  encrypt_test.go
  curve25519.go       # Curve25519 key wrapping for layer DEKs
  curve25519_test.go
  ocicrypt.go         # ocicrypt adapter (build-tagged, optional)
  ocicrypt_test.go

internal/cli/
  save_core.go        # Wire in layer encryption after packing
  open.go             # Wire in layer decryption before extraction
  inspect.go          # Show encryption status

internal/config/
  config.go           # Add Encryption config stanza + validation

internal/manifest/
  annotations.go      # Add encryption annotation constants
  manifest.go         # Handle +encrypted media type suffix
```

### Build Tags

The ocicrypt adapter (Mode 2) is behind a build tag to avoid pulling in the
heavy dependency tree for users who don't need it:

```go
//go:build ocicrypt

package encryption

import "github.com/containers/ocicrypt"
// ...
```

Default builds include only Curve25519 (Mode 1). Users who need ocicrypt
compatibility build with `go build -tags ocicrypt`.

### Core Functions

```go
package encryption

// EncryptLayer encrypts a layer blob and returns the encrypted blob,
// updated descriptor (with +encrypted media type and annotations),
// and the plaintext digest (for diff_id verification on decrypt).
func EncryptLayer(
    layerReader io.Reader,
    desc ocispec.Descriptor,
    recipients []keys.PublicKey,
    mode string, // "curve25519" or "ocicrypt"
) (io.Reader, ocispec.Descriptor, error)

// DecryptLayer decrypts an encrypted layer blob using the provided
// private key. Returns the decrypted layer reader and the verified
// plaintext digest.
func DecryptLayer(
    encLayerReader io.Reader,
    desc ocispec.Descriptor,
    privateKey keys.PrivateKey,
    mode string,
) (io.Reader, digest.Digest, error)

// IsEncrypted checks if a layer descriptor has an encrypted media type.
func IsEncrypted(desc ocispec.Descriptor) bool
```

## Trade-offs & Limitations

### What You Lose

| Capability | Without encryption | With encryption |
|------------|-------------------|-----------------|
| `docker pull` + `COPY --from` | ✓ works natively | ✗ fails (opaque blobs) |
| `crane` / `skopeo` inspection | ✓ full content | ✗ encrypted blobs |
| `bento diff` on encrypted layers | ✓ works | ⚠ requires decryption first |
| Layer dedup across checkpoints | ✓ identical layers share digests | ✗ different DEK per save = different digest |
| `cosign` verification | ✓ works | ✓ works (signs manifest, not layer content) |
| Registry compatibility | ✓ universal | ⚠ most registries accept arbitrary media types, but some may reject `+encrypted` |

### What You Gain

| Property | Benefit |
|----------|---------|
| Full content confidentiality | All workspace content hidden, not just detected secrets |
| Defense against pattern gaps | Secrets not caught by gitleaks are still protected |
| Regulatory compliance | Meets encryption-at-rest requirements for sensitive data |
| Access control | Only recipients with matching keys can read content |
| Selective encryption | Encrypt sensitive layers, leave deps unencrypted for cache reuse |

### Layer Dedup Impact

Encrypted layers get a fresh random DEK and IV on every save, so even identical
content produces different ciphertext and different digests. This breaks layer
dedup for encrypted layers.

**Mitigation:** selective encryption. Leave `deps` (the largest, most stable
layer) unencrypted. Only encrypt `agent` and `project` (smaller, change often
anyway). This preserves dedup for the layer that benefits most from it.

**Future mitigation:** deterministic encryption with a key derived from the
layer content hash + a master key. This would preserve dedup but requires
careful cryptographic design to avoid chosen-plaintext attacks. Out of scope
for this proposal.

## Migration & Backward Compatibility

| Scenario | Behavior |
|----------|----------|
| Old bento encounters `+encrypted` layer | Fails with clear error: "This checkpoint has encrypted layers. Upgrade bento to open it." |
| New bento opens unencrypted checkpoint | Works as before |
| Mixed encrypted/unencrypted layers | Supported by design (selective encryption) |
| Encryption disabled after being enabled | New checkpoints are unencrypted; old encrypted checkpoints still openable |

### Version Gate

Encrypted checkpoints MUST set `dev.bento.format.version` to at least the
version that introduces this feature. Older bento versions that don't understand
`+encrypted` media types will fail with a version mismatch error rather than
silently producing corrupt output.

## Security Properties

| Property | Guarantee |
|----------|-----------|
| Confidentiality | AES-256-CTR encrypts all layer content |
| Integrity | HMAC-SHA256 detects tampering of encrypted blobs |
| Authenticity | NaCl sealed boxes authenticate the key wrapping |
| Forward secrecy | Fresh DEK per layer per checkpoint |
| Selective disclosure | Unencrypted layers remain inspectable |
| Composability | Scrub mode + layer encryption = defense in depth |

## Testing

### Unit Tests

- `encrypt_test.go`: encrypt/decrypt round-trip; wrong key fails; HMAC
  tampering detected; media type suffix handling
- `curve25519_test.go`: DEK wrap/unwrap; multi-recipient; interop with
  BENTO-006 keys

### E2E Tests

1. **Full cycle:**
   save --encrypt-layers → inspect (shows 🔒) → open → verify file content

2. **Selective encryption:**
   encrypt only `project` → verify `deps` layer is readable without key →
   verify `project` requires key

3. **Multi-recipient:**
   encrypt to 2 recipients → each can open → non-recipient cannot

4. **Backward compat:**
   old-format checkpoint (no encryption) → new bento opens normally

5. **Mixed checkpoint:**
   some layers encrypted, some not → open works for authorized user

6. **Scrub + encrypt composition:**
   file with secret → scrubbed in layer → layer encrypted → open with key →
   secret hydrated correctly

7. **Wrong key:**
   open with wrong private key → clear error message

8. **Tampered layer:**
   modify encrypted blob → HMAC verification fails → clear error

## Comparison with BENTO-006

| Aspect | BENTO-006 (Key Wrapping) | BENTO-007 (Layer Encryption) |
|--------|-------------------------|------------------------------|
| **Protects** | Secret values only | Entire layer content |
| **Layers remain inspectable** | ✓ Yes | ✗ No (encrypted layers) |
| **Layer dedup** | ✓ Preserved | ✗ Broken for encrypted layers |
| **Dependency cost** | Zero (NaCl only) | Low (Mode 1) or High (Mode 2) |
| **Default** | Recommended for all users | Opt-in for sensitive workspaces |
| **Prerequisite** | None | BENTO-006 (for Curve25519 mode) |
| **OCI spec status** | N/A (custom envelope) | Unmerged PR (since 2019) |
| **Registry compat** | ✓ Universal | ⚠ Most work, some may reject |

**Recommendation:** implement BENTO-006 first. It solves the most common pain
point (sharing secrets without out-of-band key exchange) with zero trade-offs.
BENTO-007 is additive for users who need full confidentiality and are willing
to accept the inspectability and dedup trade-offs.

## Implementation Order

1. **BENTO-006** — Curve25519 key wrapping for secret envelopes
2. **BENTO-007 Mode 1** — Layer encryption with Curve25519 key wrapping
3. **BENTO-007 Mode 2** — ocicrypt adapter (if demand exists)

This ordering ensures the key management infrastructure exists before layer
encryption needs it, and avoids pulling in heavy dependencies until proven
necessary.
