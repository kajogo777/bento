# Push-time Encryption & Sender/Recipient Selection

## Problem

The current save flow encrypts secrets and builds a multi-recipient envelope at
save time, then push just uploads the pre-built envelope as-is. This has three
problems:

1. **Secrets stored in plaintext on disk.** Save writes both a plaintext
   `<tag>.json` and an encrypted `<tag>.enc.json` to `~/.bento/secrets/`. This
   hidden directory accumulates plaintext secrets across every workspace and
   checkpoint — toxic waste the user doesn't know exists.

2. **No way to add recipients at push time.** The `--recipient` flag on push
   was declared but never wired up (now removed as dead code). If a teammate
   joins after save, the only option is to re-save.

3. **No way to choose which sender key to use.** Save always uses
   `LoadOrCreateKeypair()`, which picks `default.json` (or first alphabetically).
   Users with multiple identities (personal/work) can't select one.

## Design

### Save: encrypt locally, no plaintext on disk

Save detects and scrubs secrets from OCI layers (unchanged), then encrypts the
placeholder→value map with a one-time DEK wrapped to the default keypair.
Only the encrypted envelope is stored on disk — no plaintext file.

```
~/.bento/secrets/<ws>/
  <tag>.enc.json   ← encrypted envelope (DEK wrapped to default keypair)
  <tag>.json       ← REMOVED (was plaintext)
```

Same-machine `bento open` loads the local envelope and unwraps the DEK using
the default keypair. This is fast (one NaCl box.Open) and seamless — the user
doesn't need to provide a key.

### Push: re-wrap with chosen sender/recipients

Push reads the local envelope, unwraps the DEK with the default keypair, then
re-wraps it for the chosen sender and recipient list. The ciphertext (encrypted
secrets) stays the same — only the key wrapping changes.

### Why this works

- The default keypair is always available on the save machine (auto-generated).
- The sender is always an implicit recipient, so unwrapping always succeeds
  on the same machine.
- Re-wrapping is cheap: one `box.Open` + N `box.Seal` calls on a 32-byte DEK.
- The ciphertext never changes — same secrets, same DEK, just different wrapping.

### Design principles

1. **No plaintext secrets on disk** — only encrypted envelopes in
   `~/.bento/secrets/`. The only plaintext is in the user's source files
   (which bento doesn't touch).

2. **Push is the sharing boundary** — identity and recipient decisions happen
   at push time. Save wraps to self for local use; push re-wraps for sharing.

3. **bento.yaml is the source of truth, CLI overrides** — `sender` and
   `recipients` are configured in bento.yaml. CLI flags (`--sender`,
   `--recipient`) override for one-off runs.

4. **Reuse existing code** — `EncryptSecrets()`, `BuildMultiRecipientEnvelope()`,
   and `ResolveRecipients()` already exist. Push adds `RewrapEnvelope()`.

## Dead Code Removed

- **`push.go`: `flagRecipients`** — declared and bound to `--recipient` flag
  but never read in the handler. Already removed.

- **`keys/recipients.go` step 3: `.pub` file lookup.** `resolveOne()` tries
  `keysDir/recipients/<name>.pub` as a fallback, but no CLI command writes to
  that directory. The `bento recipients` subcommands only manage `bento.yaml`.
  Remove step 3 from `resolveOne()` and the unused functions:
  `LoadRecipientFile`, `AddRecipient`/`AddRecipientTo`,
  `RemoveRecipient`/`RemoveRecipientFrom`, `ListRecipients`, and
  `RecipientInfo`. Only referenced in their own tests.

## CLI Changes

### `bento push`

```
bento push --include-secrets [--sender <name>] [--recipient <spec>]... [remote]
```

| Flag | Description |
|------|-------------|
| `--sender <name>` | Keypair name to use as sender. Overrides `sender` in bento.yaml. Must exist in `~/.bento/keys/<name>.json`. |
| `--recipient <spec>` | Additional recipient(s) beyond bento.yaml. Repeatable. Spec is a `bento-pk-...` literal or a name from bento.yaml `recipients`. |

**Behavior:**
- Reads `sender` and `recipients` from bento.yaml as defaults.
- `--sender` overrides the config sender. `--recipient` adds to (not replaces)
  the config recipients.
- The sender is always an implicit recipient (self-decryption).
- If no `--sender` and no `sender` in bento.yaml: `LoadOrCreateKeypair()`.

### `bento secrets export`

```
bento secrets export <ref> [--sender <name>] [--recipient <spec>]...
```

Same re-wrap logic as push. Exports the envelope to stdout.

### `bento save` — changes

- Remove `--recipient` flag (recipients are a push/sharing concern).
- Remove plaintext `localBe.Put(backendKey, scrubSecrets)`.
- Keep encryption: `EncryptSecrets()` → `BuildMultiRecipientEnvelope()` with
  default keypair (self as only recipient) → store `.enc.json`.

## bento.yaml Schema

```yaml
# Existing field — unchanged
recipients:
  - name: alice
    key: bento-pk-...
  - name: bob
    key: bento-pk-...

# New field
sender: work  # keypair name from ~/.bento/keys/work.json
```

The `sender` field is optional. When absent, `LoadOrCreateKeypair()` picks the
default keypair at push time.

### Config validation

- `sender`: if set, must be a non-empty string. Validated at push time (keypair
  must exist).
- `recipients`: existing validation unchanged (name uniqueness, valid key format).

## Save Flow (after changes)

```
1. gitleaks scans files, finds secrets
2. For each finding: replace secret value with placeholder in-memory
3. Pack scrubbed content into OCI layers (real files on disk untouched)
4. EncryptSecrets(placeholder→value map) → ciphertext + DEK
5. LoadOrCreateKeypair() → default keypair
6. BuildMultiRecipientEnvelope(ciphertext, DEK, defaultPub, defaultPriv, [defaultPub])
7. Store envelope as <tag>.enc.json (only file — no plaintext)
8. Store scrub records in OCI manifest metadata
```

Compared to current save: step 4-6 are unchanged, the plaintext write is
removed, and `--recipient` no longer adds extra recipients at save time.

## Push Flow (--include-secrets)

```
1. Load local envelope from ~/.bento/secrets/<ws>/<tag>.enc.json
2. Unwrap DEK using default keypair (always a recipient in the save-time envelope)
3. Determine push sender keypair:
   a. --sender flag → LoadKeypair(name)
   b. bento.yaml sender → LoadKeypair(name)
   c. Neither → LoadOrCreateKeypair()
4. Resolve recipients:
   a. bento.yaml recipients (by name → key lookup)
   b. + --recipient flag specs (literal keys or config names)
   c. + push sender as implicit self-recipient
   d. Deduplicate by public key bytes
5. Re-wrap: BuildMultiRecipientEnvelope(same ciphertext, DEK, pushSenderPub, pushSenderPriv, recipients)
6. Pack envelope as OCI secrets layer, inject into checkpoint
7. Push to registry
```

The ciphertext stays the same. Only the wrapping changes.

### Why re-wrap instead of re-encrypt?

- **Same ciphertext, different wrapping.** The DEK protects the secrets. The
  wrapping protects the DEK. Changing who can unwrap the DEK doesn't require
  re-encrypting the secrets.
- **One unwrap + N wraps vs re-scan + re-encrypt.** Re-wrapping is a few
  NaCl operations on 32 bytes. Re-encrypting would mean collecting secrets
  from source files (fragile — files may have changed) and running
  EncryptSecrets again.
- **Deterministic.** Same ciphertext across pushes with different recipients.
  Only the wrappedKeys array changes.

## Open Flow (after changes)

### Same machine

```
1. Restore scrubbed files from OCI layers
2. Read scrub records from manifest
3. Load local envelope (<tag>.enc.json)
4. Unwrap DEK with default keypair → decrypt → placeholder→value map
5. HydrateFile() for each scrubbed file
```

This replaces the current "Try 1" (plaintext read) with an envelope unwrap.
Seamless — the default keypair is always present on the save machine.

### Different machine (secrets in OCI via --include-secrets)

```
1. Restore scrubbed files from OCI layers
2. Read scrub records from manifest
3. Find encrypted secrets layer in OCI
4. Try to unwrap with local keypair (auto-discovery)
5. If --private-key flag: try that keypair
6. Decrypt → placeholder→value map
7. HydrateFile() for each scrubbed file
```

Unchanged from current behavior.

### Different machine (no secrets in OCI)

```
1. Restore scrubbed files with placeholders
2. Show hint: "push with --include-secrets" or "use --allow-missing-secrets"
```

## Implementation Plan

### New functions

```go
// internal/secrets/backend/oci.go

// RewrapEnvelope unwraps the DEK from an existing envelope using
// unwrapPub/unwrapPriv, then re-wraps it for newRecipients under
// newSenderPub/newSenderPriv. The ciphertext is unchanged.
func RewrapEnvelope(
    existingJSON []byte,
    unwrapPub, unwrapPriv [32]byte,
    newSenderPub, newSenderPriv [32]byte,
    newRecipients [][32]byte,
) (*MultiRecipientEnvelope, error)
```

### Changes by file

| File | Change |
|------|--------|
| `internal/cli/save_core.go` | Remove plaintext store. Remove `--recipient` threading. Keep encryption with default keypair only (self as sole recipient). |
| `internal/cli/save.go` | Remove `--recipient` flag. |
| `internal/cli/push.go` | Add `--sender`, `--recipient` flags. Load local envelope → unwrap DEK → re-wrap with push sender/recipients → pack layer. |
| `internal/cli/secrets.go` | Add `--sender`, `--recipient` flags to export. Same re-wrap logic as push. |
| `internal/cli/open.go` | Replace plaintext "Try 1" with local envelope unwrap. Keep existing OCI layer unwrap path. |
| `internal/cli/gc.go` | Remove `<tag>.json` cleanup. Keep `<tag>.enc.json` cleanup. |
| `internal/config/config.go` | Add `Sender string` field to `BentoConfig`. |
| `internal/secrets/backend/oci.go` | Add `RewrapEnvelope()`. |
| `internal/keys/recipients.go` | Remove `.pub` file functions and step 3 from `resolveOne()`. |
| `internal/keys/recipients_test.go` | Remove tests for `.pub` file functions. |

### E2E tests

Add to `e2e/scrub_test.go`:

1. **No plaintext on disk:** save → verify no `<tag>.json` in secrets dir, only `<tag>.enc.json`.
2. **Same-machine open:** save → open → verify secrets hydrated correctly.
3. **Push with --include-secrets:** save → push `--include-secrets` → pull on "other machine" → open → OK.
4. **Push with --recipient:** save → push `--include-secrets --recipient bento-pk-...` → open with recipient's key → OK.
5. **Push with --sender:** `keys generate --name work` → save → push `--include-secrets --sender work` → inspect → verify sender is `work`.
6. **Config sender + recipients:** bento.yaml `sender: work` + `recipients: [alice]` → push `--include-secrets --recipient bob` → open as alice → OK; open as bob → OK.
7. **Export with sender/recipient:** save → `secrets export --sender work --recipient bento-pk-...` → decrypt → OK.

## UX Examples

### Adding a teammate at push time

```
$ bento save -m "feature done"
Scrubbed 2 secret(s)
Tagged: cp-5, latest

$ bento push --include-secrets --recipient bento-pk-def...
Re-wrapped secrets for 2 recipient(s)
Sealed by: bento-pk-abc...
Pushing to ghcr.io/org/project...
Done.

Recipients can open with:
  bento open ghcr.io/org/project:cp-5 ./workspace
```

### Using a work identity

```
$ bento keys list
  default      bento-pk-abc...    (created 2026-03-15)
  work         bento-pk-xyz...    (created 2026-03-20)

$ bento push --include-secrets --sender work
Re-wrapped secrets for 1 recipient(s)
Sealed by: bento-pk-xyz...
Pushing to ghcr.io/org/project...
Done.
```

### Configuring in bento.yaml (team default)

```yaml
sender: work
recipients:
  - name: alice
    key: bento-pk-aaa...
  - name: bob
    key: bento-pk-bbb...
```

```
$ bento push --include-secrets
Re-wrapped secrets for 3 recipient(s)  (alice, bob, self)
Sealed by: bento-pk-xyz...   (keypair: work)
Pushing to ghcr.io/org/project...
Done.
```

### One-off override

```
$ bento push --include-secrets --recipient bento-pk-ccc...
Re-wrapped secrets for 4 recipient(s)  (alice, bob, self, + 1 from CLI)
Sealed by: bento-pk-xyz...   (keypair: work)
Done.
```

## Security Considerations

1. **No plaintext secrets on disk.** Save stores only the encrypted envelope.
   The plaintext `<tag>.json` is eliminated. Secrets on disk require the
   default keypair to decrypt.

2. **Re-wrapping preserves the same ciphertext.** Push doesn't re-encrypt —
   it unwraps the DEK and re-wraps it for new recipients. Same DEK, same
   ciphertext, different wrapping.

3. **Sender key is authenticated.** The push sender's private key is used in
   `box.Seal`, so recipients can verify the DEK came from the claimed sender.

4. **Old pushed envelopes remain valid.** Re-pushing with different recipients
   doesn't invalidate previous pushes.

5. **Default keypair is the local trust anchor.** The private key at
   `~/.bento/keys/default.json` (0600) protects local secret access. This is
   better than plaintext but not perfect — future work could integrate with
   OS keyrings or hardware tokens.

## Migration

- Save no longer writes `<tag>.json`. Old plaintext files are orphaned.
  `bento gc` should clean them up (delete all `<tag>.json` files from
  `~/.bento/secrets/`).
- Old checkpoints that only have `<tag>.json` (no `.enc.json`): `bento open`
  will fail to find the envelope and show the standard "secrets not available"
  hint. The user re-saves to generate an encrypted envelope.
- `--recipient` flag removed from `bento save`. Users should use
  `bento.yaml recipients` + `bento push --include-secrets` instead.

## Non-goals

- **Encrypting local OCI layers.** Local store has scrubbed layers (placeholders
  only). Real values are in encrypted envelopes and user source files.
- **Revoking a recipient.** NaCl doesn't support revocation. If someone had
  access to a pushed envelope, they keep it.
- **OS keyring integration.** The default keypair is file-based (0600). Keyring
  integration would improve local security but is out of scope.
