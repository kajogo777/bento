package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/kajogo777/bento/internal/config"
	"github.com/kajogo777/bento/internal/extension"
	"github.com/kajogo777/bento/internal/hooks"
	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/registry"
	"github.com/kajogo777/bento/internal/secrets"
	"github.com/kajogo777/bento/internal/workspace"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"golang.org/x/sync/errgroup"
)

// SaveOptions configures a save operation. Used by both `bento save` and `bento watch`.
type SaveOptions struct {
	Dir                  string // absolute path to workspace root
	Message              string // checkpoint message
	Tag                  string // custom tag (empty = auto cp-N)
	SkipSecretScan       bool
	AllowMissingExternal bool
	Quiet                bool // suppress per-layer output (used by watch mode)
}

// SaveResult holds the outcome of a save operation.
type SaveResult struct {
	Tag     string // the tag assigned to this checkpoint (e.g. "cp-3")
	Digest  string // manifest digest
	Seq     int    // checkpoint sequence number
	Skipped bool   // true if save was elided because nothing changed
	Reason  string // human-readable reason when Skipped is true
}

// ExecuteSave performs a full save operation: scan, pack, build manifest, store.
// It acquires a file lock to prevent concurrent saves from corrupting the store.
// If all layer digests match the parent checkpoint, the save is skipped and
// SaveResult.Skipped is set to true.
func ExecuteSave(opts SaveOptions) (*SaveResult, error) {
	cfg, err := config.Load(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("loading bento.yaml: %w", err)
	}

	// Resolve extensions
	resolved := resolveExtensions(opts.Dir, cfg)
	layers := resolved.Layers

	// Run pre_save hook
	hookCmd := cfg.Hooks.PreSave
	if hookCmd == "" && resolved.Hooks != nil {
		hookCmd = resolved.Hooks["pre_save"]
	}
	if hookCmd != "" {
		runner := hooks.NewRunner(opts.Dir, cfg.Hooks.Timeout)
		if err := runner.Run("pre_save", hookCmd); err != nil {
			return nil, fmt.Errorf("pre_save hook failed: %w", err)
		}
	}

	// Collect ignore patterns
	ignorePatterns := append(config.DefaultIgnorePatterns, resolved.Ignore...)
	ignorePatterns = append(ignorePatterns, cfg.Ignore...)
	if bentoIgnore, err := workspace.LoadBentoIgnore(opts.Dir); err == nil {
		ignorePatterns = append(ignorePatterns, bentoIgnore...)
	}

	// Scan workspace
	scanner := workspace.NewScanner(opts.Dir, layers, ignorePatterns)
	scanResults, err := scanner.Scan()
	if err != nil {
		return nil, fmt.Errorf("scanning workspace: %w", err)
	}

	// Secret scan
	if !opts.SkipSecretScan {
		secretScanner, err := secrets.NewSecretScanner(nil)
		if err != nil {
			return nil, fmt.Errorf("initializing secret scanner: %w", err)
		}

		// Load .gitleaksignore if present.
		ignorePath := filepath.Join(opts.Dir, ".gitleaksignore")
		if _, statErr := os.Stat(ignorePath); statErr == nil {
			if loadErr := secretScanner.LoadGitleaksIgnore(ignorePath); loadErr != nil {
				return nil, fmt.Errorf("loading .gitleaksignore: %w", loadErr)
			}
		}

		// Enable scan cache inside the store directory.
		secretScanner.SetCachePath(filepath.Join(cfg.StorePath(), "secret-scan-cache.json"))

		var allFiles []string
		for _, sr := range scanResults {
			for _, f := range sr.WorkspaceFiles {
				allFiles = append(allFiles, filepath.Join(opts.Dir, f))
			}
		}
		if !opts.Quiet {
			secretScanner.SetProgressFunc(func(scanned, total int) {
				fmt.Printf("\rSecret scan: %d/%d files...", scanned, total)
			})
		}
		scanHits, err := secretScanner.ScanFiles(allFiles)
		if !opts.Quiet {
			if err == nil && len(scanHits) == 0 {
				fmt.Printf("\rSecret scan: %d files clean    \n", len(allFiles))
			} else {
				fmt.Println() // newline after progress
			}
		}
		if err != nil {
			return nil, fmt.Errorf("secret scan error: %w", err)
		}
		if len(scanHits) > 0 {
			fmt.Printf("Secret scan found %d potential secret(s):\n\n", len(scanHits))
			for _, r := range scanHits {
				fmt.Printf("  %s  (%s)\n", r.Fingerprint, r.Pattern)
			}
			fmt.Println("\nTo suppress false positives, copy the fingerprints above into .gitleaksignore (one per line).")
			return nil, fmt.Errorf("aborting save due to potential secrets. Use --skip-secret-scan to bypass")
		}
	}

	// Collect active extension names for manifest metadata
	activeExtensions := extension.ActiveExtensionNames(opts.Dir, cfg.Extensions)

	// Open store
	store, err := registry.NewStore(cfg.StorePath())
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}

	// Acquire file lock for concurrent save safety
	lockPath := filepath.Join(cfg.StorePath(), ".save-lock")
	unlock, err := acquireFileLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("acquiring save lock: %w", err)
	}
	defer unlock()

	// Determine checkpoint sequence by finding the highest existing cp-N tag
	// and incrementing. Using unique digest count is wrong because GC can
	// reduce the count, causing sequence numbers to go backwards and collide
	// with existing tags.
	existing, _ := store.ListCheckpoints()
	seq := 1
	for _, e := range existing {
		if strings.HasPrefix(e.Tag, "cp-") {
			if n, err := strconv.Atoi(e.Tag[3:]); err == nil && n >= seq {
				seq = n + 1
			}
		}
	}

	// Find parent digest
	parentDigest := ""
	if len(existing) > 0 {
		if d, err := store.ResolveTag("latest"); err == nil {
			parentDigest = d
		}
	}

	// Build map of previous layer digests for change detection.
	prevLayerDigests := make(map[string]string)
	if parentDigest != "" {
		if prevManifestBytes, _, loadErr := store.LoadManifest(parentDigest); loadErr == nil {
			var prevManifest ocispec.Manifest
			if jsonErr := json.Unmarshal(prevManifestBytes, &prevManifest); jsonErr == nil {
				for _, ld := range prevManifest.Layers {
					if name, ok := ld.Annotations["org.opencontainers.image.title"]; ok {
						prevLayerDigests[name] = string(ld.Digest)
					}
				}
			}
		}
	}

	// Pack layers concurrently, preserving original order.
	if !opts.Quiet {
		fmt.Println("Scanning workspace...")
	}

	type packResult struct {
		packed     *workspace.PackResult
		mediaType  string
		totalFiles int
		extCount   int
		status     string
		name       string
	}

	results := make([]packResult, len(layers))

	g := new(errgroup.Group)
	g.SetLimit(runtime.NumCPU())

	for i, ld := range layers {
		i, ld := i, ld
		g.Go(func() error {
			sr := scanResults[ld.Name]
			wsFiles := sr.WorkspaceFiles
			extFiles := sr.ExternalFiles

			packed, err := workspace.PackLayerWithExternalToTemp(opts.Dir, wsFiles, extFiles, opts.AllowMissingExternal)
			if err != nil {
				return fmt.Errorf("packing layer %s: %w", ld.Name, err)
			}

			mediaType := ld.MediaType
			if mediaType == "" {
				mediaType = manifest.LayerMediaType
			}

			totalFiles := len(wsFiles) + len(extFiles)
			status := "changed"
			if totalFiles == 0 {
				status = "empty"
			} else if parentDigest != "" {
				if prevDigest, ok := prevLayerDigests[ld.Name]; ok && prevDigest == packed.GzipDigest {
					status = "unchanged, reusing"
				}
			}

			results[i] = packResult{
				packed:     packed,
				mediaType:  mediaType,
				totalFiles: totalFiles,
				extCount:   len(extFiles),
				status:     status,
				name:       ld.Name,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		for _, r := range results {
			if r.packed != nil && r.packed.Path != "" {
				_ = os.Remove(r.packed.Path)
			}
		}
		return nil, err
	}

	// Check skip-if-unchanged: if all layers match the parent, skip the save.
	if parentDigest != "" {
		allUnchanged := true
		for _, r := range results {
			if r.status != "unchanged, reusing" && r.status != "empty" {
				allUnchanged = false
				break
			}
		}
		if allUnchanged {
			// Clean up temp files
			for _, r := range results {
				if r.packed != nil && r.packed.Path != "" {
					_ = os.Remove(r.packed.Path)
				}
			}
			return &SaveResult{Skipped: true, Reason: "no changes detected"}, nil
		}
	}

	// Print results and build layerInfos in original order.
	var layerInfos []manifest.LayerInfo
	for _, r := range results {
		if !opts.Quiet {
			extInfo := ""
			if r.extCount > 0 {
				extInfo = fmt.Sprintf(" (+%d external)", r.extCount)
			}
			sizeStr := formatSize(int(r.packed.Size))
			fmt.Printf("  %-10s %d files%s, %s (%s)\n", r.name+":", r.totalFiles, extInfo, sizeStr, r.status)
		}

		layerInfos = append(layerInfos, manifest.LayerInfo{
			Name:       r.name,
			MediaType:  r.mediaType,
			Path:       r.packed.Path,
			Size:       r.packed.Size,
			GzipDigest: r.packed.GzipDigest,
			DiffID:     r.packed.DiffID,
			FileCount:  r.totalFiles,
		})
	}

	// Defer cleanup of temp layer files
	defer func() {
		for _, li := range layerInfos {
			if li.Path != "" {
				_ = os.Remove(li.Path)
			}
		}
	}()

	// Build config object
	cfgObj := &manifest.BentoConfigObj{
		SchemaVersion:    "1.0.0",
		WorkspaceID:      cfg.ID,
		Extensions:       activeExtensions,
		Checkpoint:       seq,
		Created:          time.Now().UTC().Format(time.RFC3339),
		Status:           "paused",
		ParentCheckpoint: parentDigest,
		Task:             cfg.Task,
		Environment: &manifest.Environment{
			OS:   runtime.GOOS,
			Arch: runtime.GOARCH,
		},
	}
	cfgObj.Repos = discoverRepos(opts.Dir)
	cfgObj.Message = opts.Message

	// Embed env vars and secret references
	if len(cfg.Env) > 0 {
		cfgObj.Env = make(map[string]manifest.ManifestEnvEntry, len(cfg.Env))
		for name, entry := range cfg.Env {
			if entry.IsRef {
				cfgObj.Env[name] = manifest.ManifestEnvEntry{
					Source:  entry.Source,
					Path:    entry.Fields["path"],
					Key:     entry.Fields["key"],
					Var:     entry.Fields["var"],
					Role:    entry.Fields["role"],
					Command: entry.Fields["command"],
					IsRef:   true,
				}
			} else {
				cfgObj.Env[name] = manifest.ManifestEnvEntry{Value: entry.Value}
			}
		}
	}

	// Embed portable workspace config
	if cfg.Remote != "" {
		cfgObj.Remote = cfg.Remote
	}
	if len(cfg.Layers) > 0 {
		cfgObj.Layers = make([]manifest.LayerDef, len(cfg.Layers))
		for i, l := range cfg.Layers {
			cfgObj.Layers[i] = manifest.LayerDef{
				Name:     l.Name,
				Patterns: l.Patterns,
				CatchAll: l.CatchAll,
			}
		}
	}

	// Hooks
	cfgHooks := cfg.Hooks
	if cfgHooks.PreSave != "" || cfgHooks.PostSave != "" || cfgHooks.PostRestore != "" || cfgHooks.PrePush != "" || cfgHooks.PostPush != "" || cfgHooks.PostFork != "" || cfgHooks.Timeout != 0 {
		cfgObj.Hooks = &manifest.HooksDef{
			PreSave:     cfgHooks.PreSave,
			PostSave:    cfgHooks.PostSave,
			PostRestore: cfgHooks.PostRestore,
			PrePush:     cfgHooks.PrePush,
			PostPush:    cfgHooks.PostPush,
			PostFork:    cfgHooks.PostFork,
			Timeout:     cfgHooks.Timeout,
		}
	}

	// Ignore patterns
	allIgnore := append([]string{}, cfg.Ignore...)
	if bentoIgnorePatterns, err := workspace.LoadBentoIgnore(opts.Dir); err == nil {
		allIgnore = append(allIgnore, bentoIgnorePatterns...)
	}
	if len(allIgnore) > 0 {
		cfgObj.Ignore = allIgnore
	}

	// Retention policy
	if cfg.Retention.KeepLast != 0 || cfg.Retention.KeepTagged {
		cfgObj.Retention = &manifest.RetentionDef{
			KeepLast:   cfg.Retention.KeepLast,
			KeepTagged: cfg.Retention.KeepTagged,
		}
	}

	manifestBytes, configBytes, err := manifest.BuildManifest(cfgObj, layerInfos)
	if err != nil {
		return nil, fmt.Errorf("building manifest: %w", err)
	}

	var storeLayerData []registry.LayerData
	for _, li := range layerInfos {
		storeLayerData = append(storeLayerData, registry.LayerData{
			MediaType: li.MediaType,
			Path:      li.Path,
			Digest:    li.GzipDigest,
			Size:      li.Size,
		})
	}

	tag := fmt.Sprintf("cp-%d", seq)
	if opts.Tag != "" {
		tag = opts.Tag
	}
	manifestDigest, err := store.SaveCheckpoint(tag, manifestBytes, configBytes, storeLayerData)
	if err != nil {
		return nil, fmt.Errorf("saving checkpoint: %w", err)
	}

	if err := store.Tag(manifestDigest, "latest"); err != nil {
		return nil, fmt.Errorf("tagging latest: %w", err)
	}

	// Run post_save hook
	postHookCmd := cfg.Hooks.PostSave
	if postHookCmd == "" && resolved.Hooks != nil {
		postHookCmd = resolved.Hooks["post_save"]
	}
	if postHookCmd != "" {
		runner := hooks.NewRunner(opts.Dir, cfg.Hooks.Timeout)
		if err := runner.Run("post_save", postHookCmd); err != nil {
			fmt.Printf("Warning: post_save hook failed: %v\n", err)
		}
	}

	return &SaveResult{
		Tag:    tag,
		Digest: manifestDigest,
		Seq:    seq,
	}, nil
}

// gitOutput runs a git command and returns trimmed stdout.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// discoverRepos walks the workspace for git repositories and returns their state.
// It finds the root repo (if any) and submodules/nested repos.
func discoverRepos(workDir string) []manifest.RepoInfo {
	var repos []manifest.RepoInfo

	// Check root first
	if info := repoInfoAt(workDir, "."); info != nil {
		repos = append(repos, *info)
	}

	// Walk for nested .git dirs (submodules, multi-repo)
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Name() == ".git" && path != filepath.Join(workDir, ".git") {
			// Found a nested repo — the repo root is the parent dir
			repoDir := filepath.Dir(path)
			rel, relErr := filepath.Rel(workDir, repoDir)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if info := repoInfoAt(repoDir, rel); info != nil {
				repos = append(repos, *info)
			}
			// Don't descend into nested .git
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})

	return repos
}

// repoInfoAt collects git metadata for a repo at the given directory.
func repoInfoAt(dir, relPath string) *manifest.RepoInfo {
	// Verify it's actually a git repo
	if _, err := gitOutput(dir, "rev-parse", "--git-dir"); err != nil {
		return nil
	}
	info := &manifest.RepoInfo{Path: relPath}
	if sha, err := gitOutput(dir, "rev-parse", "HEAD"); err == nil {
		info.Sha = sha
	}
	if branch, err := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = branch
	}
	if remote, err := gitOutput(dir, "remote", "get-url", "origin"); err == nil {
		info.Remote = remote
	}
	return info
}
