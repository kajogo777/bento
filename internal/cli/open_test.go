package cli

import (
	"encoding/json"
	"testing"

	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestExcludeSecretsLayers(t *testing.T) {
	// Build a manifest with 3 layers: deps, agent, secrets
	m := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			{
				Annotations: map[string]string{
					manifest.AnnotationTitle: "deps",
				},
			},
			{
				Annotations: map[string]string{
					manifest.AnnotationTitle: "agent",
				},
			},
			{
				Annotations: map[string]string{
					manifest.AnnotationSecretsEncrypted:  "true",
					manifest.AnnotationSecretsKeyWrapping: "true",
				},
			},
		},
	}
	manifestBytes, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	layers := []registry.LayerData{
		{Digest: "sha256:deps"},
		{Digest: "sha256:agent"},
		{Digest: "sha256:secrets"},
	}

	result := excludeSecretsLayers(layers, manifestBytes)

	if len(result) != 2 {
		t.Fatalf("expected 2 layers after filtering, got %d", len(result))
	}
	if result[0].Digest != "sha256:deps" {
		t.Errorf("expected first layer to be deps, got %s", result[0].Digest)
	}
	if result[1].Digest != "sha256:agent" {
		t.Errorf("expected second layer to be agent, got %s", result[1].Digest)
	}
}

func TestExcludeSecretsLayers_NoSecrets(t *testing.T) {
	m := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			{Annotations: map[string]string{manifest.AnnotationTitle: "deps"}},
			{Annotations: map[string]string{manifest.AnnotationTitle: "agent"}},
		},
	}
	manifestBytes, _ := json.Marshal(m)

	layers := []registry.LayerData{
		{Digest: "sha256:deps"},
		{Digest: "sha256:agent"},
	}

	result := excludeSecretsLayers(layers, manifestBytes)

	if len(result) != 2 {
		t.Fatalf("expected 2 layers (unchanged), got %d", len(result))
	}
}

func TestExcludeSecretsLayers_BadManifest(t *testing.T) {
	layers := []registry.LayerData{
		{Digest: "sha256:deps"},
	}

	// Invalid manifest JSON — should return layers unchanged
	result := excludeSecretsLayers(layers, []byte("not json"))

	if len(result) != 1 {
		t.Fatalf("expected 1 layer (unchanged on bad manifest), got %d", len(result))
	}
}
