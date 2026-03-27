package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

// PushToRemote copies a checkpoint from a local store to a remote OCI registry.
// remoteRef is the full registry reference (e.g. "ghcr.io/myorg/workspaces/myproject").
// tags are the tags to push (e.g. ["cp-3", "latest"]).
func PushToRemote(ctx context.Context, localStore Store, remoteRef string, tags []string) error {
	// Load all tagged checkpoints from local store
	entries, err := localStore.ListCheckpoints()
	if err != nil {
		return fmt.Errorf("listing local checkpoints: %w", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("no checkpoints to push")
	}

	// Set up remote repository
	repo, err := newRemoteRepo(ctx, remoteRef)
	if err != nil {
		return err
	}

	// Build an in-memory store with the blobs to push
	memStore := memory.New()

	// Track which digests we've already pushed
	pushed := make(map[string]bool)

	for _, entry := range entries {
		// Filter to requested tags, or push all if no filter
		if len(tags) > 0 && !containsTag(tags, entry.Tag) {
			continue
		}

		manifestBytes, configBytes, layers, err := localStore.LoadCheckpoint(entry.Tag)
		if err != nil {
			return fmt.Errorf("loading checkpoint %s: %w", entry.Tag, err)
		}

		// Push config blob
		configDigest := digest.FromBytes(configBytes)
		if !pushed[configDigest.String()] {
			configDesc := ocispec.Descriptor{
				MediaType: "application/vnd.bento.config.v1+json",
				Digest:    configDigest,
				Size:      int64(len(configBytes)),
			}
			if err := memStore.Push(ctx, configDesc, bytes.NewReader(configBytes)); err != nil {
				return fmt.Errorf("staging config: %w", err)
			}
			pushed[configDigest.String()] = true
		}

		// Push layer blobs
		for _, ld := range layers {
			layerDigest := digest.FromBytes(ld.Data)
			if pushed[layerDigest.String()] {
				continue
			}
			layerDesc := ocispec.Descriptor{
				MediaType: ld.MediaType,
				Digest:    layerDigest,
				Size:      int64(len(ld.Data)),
			}
			if err := memStore.Push(ctx, layerDesc, bytes.NewReader(ld.Data)); err != nil {
				return fmt.Errorf("staging layer %s: %w", ld.Digest, err)
			}
			pushed[layerDigest.String()] = true
		}

		// Push manifest
		manifestDigest := digest.FromBytes(manifestBytes)
		manifestDesc := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    manifestDigest,
			Size:      int64(len(manifestBytes)),
		}
		if !pushed[manifestDigest.String()] {
			if err := memStore.Push(ctx, manifestDesc, bytes.NewReader(manifestBytes)); err != nil {
				return fmt.Errorf("staging manifest: %w", err)
			}
			pushed[manifestDigest.String()] = true
		}

		// Tag in memory store
		if err := memStore.Tag(ctx, manifestDesc, entry.Tag); err != nil {
			return fmt.Errorf("tagging %s in memory: %w", entry.Tag, err)
		}

		// Copy to remote with tag
		_, err = oras.Copy(ctx, memStore, entry.Tag, repo, entry.Tag, oras.DefaultCopyOptions)
		if err != nil {
			return fmt.Errorf("pushing %s: %w", entry.Tag, err)
		}

		fmt.Printf("  pushed %s (%s)\n", entry.Tag, truncDigest(manifestDigest.String()))
	}

	return nil
}

// PullFromRemote copies a checkpoint from a remote OCI registry to the local store.
// remoteRef is "ghcr.io/myorg/workspaces/myproject:tag" or just a tag if remote is configured.
func PullFromRemote(ctx context.Context, localStore *LocalStore, remoteRef, tag string) error {
	repo, err := newRemoteRepo(ctx, remoteRef)
	if err != nil {
		return err
	}

	// Pull manifest
	_, manifestReader, err := oras.Fetch(ctx, repo, tag, oras.DefaultFetchOptions)
	if err != nil {
		return fmt.Errorf("fetching manifest %s:%s: %w", remoteRef, tag, err)
	}
	manifestBytes, err := io.ReadAll(manifestReader)
	manifestReader.Close()
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}

	// Parse manifest
	var m ocispec.Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	// Pull config blob
	configReader, err := repo.Blobs().Fetch(ctx, m.Config)
	if err != nil {
		return fmt.Errorf("fetching config blob: %w", err)
	}
	configBytes, err := io.ReadAll(configReader)
	configReader.Close()
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}

	// Pull layer blobs
	var layers []LayerData
	for _, ld := range m.Layers {
		layerReader, err := repo.Blobs().Fetch(ctx, ld)
		if err != nil {
			return fmt.Errorf("fetching layer %s: %w", ld.Digest, err)
		}
		layerBytes, err := io.ReadAll(layerReader)
		layerReader.Close()
		if err != nil {
			return fmt.Errorf("reading layer: %w", err)
		}

		layers = append(layers, LayerData{
			MediaType: ld.MediaType,
			Data:      layerBytes,
			Digest:    ld.Digest.String(),
		})
	}

	// Save to local store
	_, err = localStore.SaveCheckpoint(tag, manifestBytes, configBytes, layers)
	if err != nil {
		return fmt.Errorf("caching locally: %w", err)
	}

	return nil
}

// newRemoteRepo creates an oras remote.Repository with Docker credential auth.
func newRemoteRepo(ctx context.Context, ref string) (*remote.Repository, error) {
	repo, err := remote.NewRepository(ref)
	if err != nil {
		return nil, fmt.Errorf("invalid remote reference %q: %w", ref, err)
	}

	// Use plaintext HTTP for local/development registries
	host := repo.Reference.Host()
	if strings.HasPrefix(host, "localhost") ||
		strings.HasPrefix(host, "127.0.0.1") ||
		strings.HasPrefix(host, "host.docker.internal") ||
		strings.HasPrefix(host, "0.0.0.0") {
		repo.PlainHTTP = true
	}

	// Also support BENTO_PLAINHTTP=1 for other non-TLS registries
	if os.Getenv("BENTO_PLAINHTTP") == "1" {
		repo.PlainHTTP = true
	}

	// Use Docker credential store for auth
	credStore, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err == nil {
		repo.Client = &auth.Client{
			Credential: credentials.Credential(credStore),
		}
	}

	return repo, nil
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

func truncDigest(d string) string {
	if len(d) > 19 {
		return d[:19] + "..."
	}
	return d
}
