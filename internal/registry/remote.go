package registry

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
)

// PushToRemote copies checkpoints from a local store to a remote OCI registry.
// If tags is non-empty, only those tags are pushed. Otherwise all tags are pushed.
func PushToRemote(ctx context.Context, localStore Store, remoteRef string, tags []string) error {
	local, ok := localStore.(*LocalStore)
	if !ok {
		return fmt.Errorf("push requires a local OCI store")
	}

	entries, err := localStore.ListCheckpoints()
	if err != nil {
		return fmt.Errorf("listing local checkpoints: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("no checkpoints to push")
	}

	repo, err := newRemoteRepo(remoteRef)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if len(tags) > 0 && !containsStr(tags, entry.Tag) {
			continue
		}

		_, err := oras.Copy(ctx, local.OCI(), entry.Tag, repo, entry.Tag, oras.DefaultCopyOptions)
		if err != nil {
			return fmt.Errorf("pushing %s: %w", entry.Tag, err)
		}

		digest := entry.Digest
		if len(digest) > 19 {
			digest = digest[:19] + "..."
		}
		fmt.Printf("  pushed %s (%s)\n", entry.Tag, digest)
	}

	return nil
}

// PullFromRemote copies a checkpoint from a remote OCI registry to the local store.
func PullFromRemote(ctx context.Context, localStore *LocalStore, remoteRef, tag string) error {
	repo, err := newRemoteRepo(remoteRef)
	if err != nil {
		return err
	}

	_, err = oras.Copy(ctx, repo, tag, localStore.OCI(), tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("pulling %s:%s: %w", remoteRef, tag, err)
	}

	return nil
}

// PullAllFromRemote copies all tagged checkpoints from a remote registry to the local store.
// Returns the list of tags that were pulled.
func PullAllFromRemote(ctx context.Context, localStore *LocalStore, remoteRef string) ([]string, error) {
	tags, err := ListRemoteTags(ctx, remoteRef)
	if err != nil {
		return nil, err
	}

	var pulled []string
	for _, tag := range tags {
		if pullErr := PullFromRemote(ctx, localStore, remoteRef, tag); pullErr != nil {
			return pulled, fmt.Errorf("pulling %s: %w", tag, pullErr)
		}
		pulled = append(pulled, tag)
	}
	return pulled, nil
}

// ListRemoteTags returns all tags from a remote OCI registry repository.
func ListRemoteTags(ctx context.Context, remoteRef string) ([]string, error) {
	repo, err := newRemoteRepo(remoteRef)
	if err != nil {
		return nil, err
	}

	var allTags []string
	err = repo.Tags(ctx, "", func(tags []string) error {
		allTags = append(allTags, tags...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing tags from %s: %w", remoteRef, err)
	}
	return allTags, nil
}

// newRemoteRepo creates an oras remote.Repository with Docker credential auth.
func newRemoteRepo(ref string) (*remote.Repository, error) {
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
	if os.Getenv("BENTO_PLAINHTTP") == "1" {
		repo.PlainHTTP = true
	}

	// Use a custom HTTP transport with connection-level timeouts.
	// Note: no ReadTimeout or WriteTimeout here — layer blobs can be gigabytes
	// and we must not interrupt a legitimate in-progress transfer.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSClientConfig:       &tls.Config{},
		ForceAttemptHTTP2:     true,
	}
	httpClient := &http.Client{Transport: transport}

	// Use Docker credential store for auth
	credStore, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err == nil {
		repo.Client = &auth.Client{
			Client:     httpClient,
			Credential: credentials.Credential(credStore),
		}
	} else {
		repo.Client = &auth.Client{Client: httpClient}
	}

	return repo, nil
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
