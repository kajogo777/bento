package watcher

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/kajogo777/bento/internal/extension"
	"github.com/kajogo777/bento/internal/workspace"
)

// Config holds all parameters for the watcher.
type Config struct {
	WorkDir          string
	DebounceDuration time.Duration         // quiet period before saving; default 10s
	PollInterval     time.Duration         // for periodic-watch layers; default 30s
	Layers           []extension.LayerDef    // layers with WatchMethod set
	IgnorePatterns   []string
	SaveFunc         func() error          // called when debounce fires
}

// Watcher monitors the workspace for changes using a hybrid strategy:
// realtime (fsnotify) for project-like layers, periodic (polling) for
// deps/agent-like layers, and off for layers that should not trigger saves.
type Watcher struct {
	cfg            Config
	fsWatcher      *fsnotify.Watcher
	ignore         *workspace.IgnoreMatcher
	periodicDirs   []string            // absolute paths polled periodically
	offDirs        []string            // absolute paths with watch: off (not monitored)
	periodicHashes map[string]uint64   // last known fingerprint per dir
}

// New creates a Watcher. Call Run() to start the event loop.
func New(cfg Config) (*Watcher, error) {
	if cfg.DebounceDuration == 0 {
		cfg.DebounceDuration = 10 * time.Second
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 30 * time.Second
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}

	w := &Watcher{
		cfg:            cfg,
		fsWatcher:      fsw,
		ignore:         workspace.NewIgnoreMatcher(cfg.IgnorePatterns),
		periodicHashes: make(map[string]uint64),
	}

	// Collect periodic dirs from poll-method layers and add realtime dirs.
	for _, layer := range cfg.Layers {
		switch layer.WatchMethod {
		case extension.WatchPeriodic:
			dirs := periodicDirsFromLayer(cfg.WorkDir, layer)
			w.periodicDirs = append(w.periodicDirs, dirs...)
		case extension.WatchRealtime:
			// fsnotify dirs are added by the recursive walker below
		case extension.WatchOff:
			// not watched at all — collect dirs to skip in fsnotify
			dirs := periodicDirsFromLayer(cfg.WorkDir, layer)
			w.offDirs = append(w.offDirs, dirs...)
		}
	}

	// Add realtime-watch directories to fsnotify.
	if err := w.addDirRecursive(cfg.WorkDir); err != nil {
		_ = fsw.Close()
		return nil, fmt.Errorf("walking workspace for fsnotify: %w", err)
	}

	// Initialize periodic hashes (first snapshot — changes are detected on subsequent ticks).
	for _, dir := range w.periodicDirs {
		h, err := layerFingerprint(dir)
		if err == nil {
			w.periodicHashes[dir] = h
		}
	}

	return w, nil
}

// Run starts the event loop. It blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	defer w.fsWatcher.Close() //nolint:errcheck

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	defer debounce.Stop()

	pollTicker := time.NewTicker(w.cfg.PollInterval)
	defer pollTicker.Stop()

	pending := false

	for {
		select {
		case <-ctx.Done():
			return nil

		// --- realtime events (fsnotify, for realtime-watch layers) ---
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return nil
			}
			if !isRelevantEvent(event) {
				continue
			}
			rel := w.relPath(event.Name)
			if w.ignore.Match(rel) {
				continue
			}
			// New directory? Add to fsnotify (if not periodic or off).
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if !w.isPeriodicDir(event.Name) && !w.isOffDir(event.Name) {
						_ = w.addDirRecursive(event.Name)
					}
				}
			}
			// Skip events from off-watch directories.
			if w.isOffDir(event.Name) {
				continue
			}
			pending = true
			debounce.Reset(w.cfg.DebounceDuration)

		// --- periodic check (for periodic-watch layers) ---
		case <-pollTicker.C:
			// Re-check for newly created periodic/off dirs.
			w.refreshPeriodicDirs()
			w.refreshOffDirs()

			changed := false
			for _, dir := range w.periodicDirs {
				h, err := layerFingerprint(dir)
				if err != nil {
					continue
				}
				if prev, ok := w.periodicHashes[dir]; !ok || prev != h {
					w.periodicHashes[dir] = h
					if ok {
						changed = true
					}
				}
			}
			if changed {
				pending = true
				debounce.Reset(w.cfg.DebounceDuration)
			}

		// --- debounce fires → save ---
		case <-debounce.C:
			if pending {
				pending = false
				if err := w.cfg.SaveFunc(); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: auto-save failed: %v\n", err) //nolint:errcheck
				}
				// If events arrived during save, schedule another save.
				if pending {
					debounce.Reset(w.cfg.DebounceDuration)
				}
			}

		// --- fsnotify errors ---
		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return nil
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				pending = true
				debounce.Reset(w.cfg.DebounceDuration)
			}
			fmt.Fprintf(os.Stderr, "Warning: watcher error: %v\n", err) //nolint:errcheck
		}
	}
}

// addDirRecursive walks root and adds all non-ignored, non-periodic directories
// to the fsnotify watcher.
func (w *Watcher) addDirRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(w.cfg.WorkDir, path)
		if relErr != nil {
			return nil
		}
		rel = workspace.NormalizePath(rel)
		if rel == "." {
			// Always watch the root.
			return w.fsWatcher.Add(path)
		}
		if w.ignore.Match(rel) || w.ignore.Match(rel+"/") {
			return filepath.SkipDir
		}
		if w.isPeriodicDir(path) {
			return filepath.SkipDir
		}
		if w.isOffDir(path) {
			return filepath.SkipDir
		}
		return w.fsWatcher.Add(path)
	})
}

// isPeriodicDir returns true if absPath is one of the periodic-watch directories
// or is inside one.
func (w *Watcher) isPeriodicDir(absPath string) bool {
	for _, pd := range w.periodicDirs {
		if absPath == pd || strings.HasPrefix(absPath, pd+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// isOffDir returns true if absPath is one of the watch-off directories
// or is inside one.
func (w *Watcher) isOffDir(absPath string) bool {
	for _, od := range w.offDirs {
		if absPath == od || strings.HasPrefix(absPath, od+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// refreshPeriodicDirs re-scans layer patterns for newly created periodic dirs.
// This catches `npm init` creating `node_modules/` after the watcher started.
func (w *Watcher) refreshPeriodicDirs() {
	var updated []string
	for _, layer := range w.cfg.Layers {
		if layer.WatchMethod == extension.WatchPeriodic {
			dirs := periodicDirsFromLayer(w.cfg.WorkDir, layer)
			updated = append(updated, dirs...)
		}
	}
	w.periodicDirs = updated
}

// refreshOffDirs re-scans layer patterns for newly created watch-off dirs.
// This catches build output directories created after the watcher started.
func (w *Watcher) refreshOffDirs() {
	var updated []string
	for _, layer := range w.cfg.Layers {
		if layer.WatchMethod == extension.WatchOff {
			dirs := periodicDirsFromLayer(w.cfg.WorkDir, layer)
			updated = append(updated, dirs...)
		}
	}
	w.offDirs = updated
}

// relPath returns the workspace-relative path for an absolute path.
func (w *Watcher) relPath(absPath string) string {
	rel, err := filepath.Rel(w.cfg.WorkDir, absPath)
	if err != nil {
		return absPath
	}
	return workspace.NormalizePath(rel)
}

// isRelevantEvent filters out Chmod-only events which are noisy on macOS
// (Spotlight, antivirus, etc.) and don't indicate actual content changes.
func isRelevantEvent(e fsnotify.Event) bool {
	return e.Has(fsnotify.Create) || e.Has(fsnotify.Write) ||
		e.Has(fsnotify.Remove) || e.Has(fsnotify.Rename)
}

// periodicDirsFromLayer extracts root directories from a layer's patterns
// for periodic polling. "node_modules/**" → "<workDir>/node_modules", etc.
// External patterns (~/...) are resolved to absolute paths.
func periodicDirsFromLayer(workDir string, layer extension.LayerDef) []string {
	var dirs []string
	for _, p := range layer.Patterns {
		if extension.IsExternalPattern(p) {
			resolved := strings.TrimSuffix(extension.ExpandHome(p), "/")
			if info, err := os.Stat(resolved); err == nil && info.IsDir() {
				dirs = append(dirs, resolved)
			}
			continue
		}
		dir := strings.TrimSuffix(p, "/**")
		if dir == p || strings.Contains(dir, "*") {
			continue
		}
		abs := filepath.Join(workDir, dir)
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			dirs = append(dirs, abs)
		}
	}
	return dirs
}

// layerFingerprint computes a lightweight fingerprint of a directory by
// stat'ing only the top-level entries (not recursing). This catches bulk
// operations like npm install, pip install, go mod download.
func layerFingerprint(dir string) (uint64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	h := fnv.New64a()
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "%s\t%d\t%d\n", e.Name(), info.ModTime().UnixNano(), info.Size()) //nolint:errcheck
	}
	return h.Sum64(), nil
}
