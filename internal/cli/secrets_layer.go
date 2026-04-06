package cli

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// extractSecretsEnvelope finds the encrypted secrets layer in a manifest and
// returns the raw envelope JSON bytes. Returns nil, nil when the manifest has
// no secrets layer.
func extractSecretsEnvelope(store registry.Store, manifestBytes []byte) ([]byte, error) {
	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	// Find the layer annotated as encrypted secrets.
	var secretsDigest string
	for _, ld := range m.Layers {
		if ld.Annotations[manifest.AnnotationSecretsEncrypted] == "true" {
			secretsDigest = string(ld.Digest)
			break
		}
	}
	if secretsDigest == "" {
		return nil, nil // no secrets layer
	}

	// Fetch the layer blob (small tar.gz containing secrets.enc).
	blob, err := store.FetchBlob(secretsDigest)
	if err != nil {
		return nil, fmt.Errorf("fetching secrets layer blob: %w", err)
	}

	// Extract the secrets.enc file from the tar.gz.
	content, err := workspace.ExtractFileContentFromLayer(bytes.NewReader(blob), "secrets.enc", 10*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("extracting secrets.enc from layer: %w", err)
	}

	return content, nil
}
