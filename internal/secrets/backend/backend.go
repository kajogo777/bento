// Package backend defines the SecretBackend interface and a registry of
// built-in implementations. Backends store and retrieve scrubbed secret
// values so that bento can remove secrets from OCI artifacts and restore
// them transparently on open.
//
// The design mirrors the extension system: a Go interface, a registry of
// built-in implementations, and config-driven selection by name.
package backend

import "context"

// SecretBackend stores and retrieves scrubbed secret values.
//
// Each backend is responsible for:
//   - Storing a map of placeholder ID → secret value (Put)
//   - Retrieving that map by checkpoint key (Get)
//   - Cleaning up when checkpoints are garbage collected (Delete)
//   - Reporting whether it can operate in the current environment (Available)
//   - Providing human-readable restore instructions (Hint)
//
// The key passed to Put/Get/Delete has the format "<workspaceID>/<tag>"
// (e.g., "ws-abc123/cp-5"). Backends must treat this as an opaque string
// and use it as-is for storage/retrieval.
//
// The secrets map passed to Put uses placeholder IDs as keys (e.g.,
// "a1b2c3d4e5f6") and the real secret values as values. Backends must
// store and return this map exactly — no key transformation.
type SecretBackend interface {
	// Name returns the backend identifier used in bento.yaml
	// (e.g., "local", "oci", "vault").
	// Must be lowercase, alphanumeric + hyphens, unique across all backends.
	Name() string

	// Put stores secrets for a checkpoint.
	//
	// Parameters:
	//   ctx     — context for cancellation/timeout
	//   key     — checkpoint identifier: "<workspaceID>/<tag>"
	//   secrets — map of placeholder ID → real secret value
	//
	// Returns:
	//   meta — backend-specific metadata from the put operation
	//          (e.g., OCI backend returns {"ciphertext": "...", "rawKey": "..."}).
	//          Passed to Hint() for generating restore instructions.
	//          May be nil if no metadata is needed.
	//   err  — nil on success
	//
	// Implementations must be idempotent: calling Put twice with the same
	// key should overwrite, not error.
	Put(ctx context.Context, key string, secrets map[string]string) (meta map[string]string, err error)

	// Get retrieves secrets for a checkpoint.
	//
	// Parameters:
	//   ctx  — context for cancellation/timeout
	//   key  — checkpoint identifier: "<workspaceID>/<tag>"
	//   opts — backend-specific options for retrieval
	//          (e.g., OCI backend reads {"ciphertext": "...", "rawKey": "..."}).
	//          May be nil.
	//
	// Returns:
	//   secrets — map of placeholder ID → real secret value
	//   err     — nil on success; return a descriptive error if the key
	//             doesn't exist or credentials are missing
	Get(ctx context.Context, key string, opts map[string]string) (map[string]string, error)

	// Delete removes secrets for a checkpoint. Called by `bento gc`.
	//
	// Must be idempotent: deleting a non-existent key should not error.
	Delete(ctx context.Context, key string) error

	// Available reports whether this backend can operate in the current
	// environment. Check for required CLI tools, credentials, network
	// access, etc.
	//
	// Called before Put/Get to provide early, actionable error messages
	// instead of cryptic failures mid-operation.
	Available() bool

	// Hint returns human-readable restore instructions.
	//
	// Parameters:
	//   key  — checkpoint identifier: "<workspaceID>/<tag>"
	//   meta — metadata returned by Put() for this checkpoint
	//
	// Returns two strings:
	//   display — shown in terminal after save; MAY contain sensitive
	//             material (e.g., one-time encryption key for OCI backend)
	//   persist — stored in OCI manifest restoreHint field; MUST NOT
	//             contain any secret material; shown on failed hydration
	//
	// Both strings should be actionable — tell the user exactly what
	// command to run or what to configure.
	Hint(key string, meta map[string]string) (display string, persist string)
}

// Configurable is an optional interface backends can implement to receive
// provider-specific configuration. The opts map contains backend-specific
// fields passed during initialization.
//
// Configure is called after FindBackend() and before any Put/Get/Delete
// calls. Return an error for invalid or missing required config.
type Configurable interface {
	Configure(opts map[string]string) error
}
