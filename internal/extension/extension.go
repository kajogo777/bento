package extension

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/manifest"
)

// WatchMethod constants for per-layer watch behavior.
const (
	WatchRealtime = "realtime" // fsnotify, instant detection
	WatchPeriodic = "periodic" // polling, periodic fingerprint check
	WatchOff      = "off"      // not watched (still included in saves)
)

// ValidWatchMethods is the set of accepted watch values.
var ValidWatchMethods = map[string]bool{
	WatchRealtime: true,
	WatchPeriodic: true,
	WatchOff:      true,
}

// LayerDef defines a layer for file assignment.
type LayerDef struct {
	Name        string
	Patterns    []string // workspace-relative globs; ~/... or /... = external paths
	MediaType   string
	CatchAll    bool   // if true, unmatched files fall into this layer
	WatchMethod string // "realtime", "periodic", or "off"; defaults to "realtime"
}

// Extension is a composable unit that contributes patterns to bento's layer model.
// Each extension has a single concern: an agent, a language/framework, or a tool.
type Extension interface {
	// Name returns the extension identifier (e.g., "claude-code", "node", "python").
	Name() string

	// Detect returns true if this extension is relevant to the workspace.
	Detect(workDir string) bool

	// Contribute returns the patterns and config this extension adds.
	Contribute(workDir string) Contribution

	// NormalizePath returns a function that replaces workspace-derived
	// components in portable paths with stable placeholders. Called at save time.
	// Return nil if no normalization is needed.
	NormalizePath(workDir string) func(path string) string

	// ResolvePath returns a function that expands placeholders back to
	// workspace-derived components for the target workDir. Called at restore time.
	// Must not check filesystem existence. Return nil if not needed.
	ResolvePath(workDir string) func(path string) string
}

// Contribution holds what an extension adds to the build.
type Contribution struct {
	// Layers maps layer name → patterns to add (e.g., "agent" → [".claude/**"]).
	Layers map[string][]string

	// ExtraLayers defines entirely new layers (e.g., "build-cache" for Rust target/).
	ExtraLayers []LayerDef

	// Ignore patterns to exclude from all layers.
	Ignore []string

	// Hooks default lifecycle hooks (user config overrides these).
	Hooks map[string]string
}

// MergeResult holds the output of merging all extension contributions.
type MergeResult struct {
	Layers []LayerDef
	Ignore []string
	Hooks  map[string]string
}

// Merge combines contributions from all active extensions into final layer definitions.
// Core layers (deps, agent, project) always exist. Extensions add patterns to them.
// Extra layers from extensions are appended after the core three.
func Merge(contributions []Contribution) MergeResult {
	// layerMap collects deduplicated patterns per layer name.
	type layerState struct {
		patterns []string
		seen     map[string]bool
	}
	layerMap := make(map[string]*layerState)
	var layerOrder []string // tracks first-seen order

	ensureLayer := func(name string) *layerState {
		if ls, ok := layerMap[name]; ok {
			return ls
		}
		ls := &layerState{seen: make(map[string]bool)}
		layerMap[name] = ls
		layerOrder = append(layerOrder, name)
		return ls
	}

	// Seed the three core layers so they always exist and appear first.
	ensureLayer("deps")
	ensureLayer("agent")

	ignoreSet := make(map[string]bool)
	var ignore []string
	hooks := make(map[string]string)

	for _, c := range contributions {
		for layerName, patterns := range c.Layers {
			ls := ensureLayer(layerName)
			for _, p := range patterns {
				if !ls.seen[p] {
					ls.seen[p] = true
					ls.patterns = append(ls.patterns, p)
				}
			}
		}

		for _, el := range c.ExtraLayers {
			ls := ensureLayer(el.Name)
			for _, p := range el.Patterns {
				if !ls.seen[p] {
					ls.seen[p] = true
					ls.patterns = append(ls.patterns, p)
				}
			}
		}

		for _, p := range c.Ignore {
			if !ignoreSet[p] {
				ignoreSet[p] = true
				ignore = append(ignore, p)
			}
		}

		for k, v := range c.Hooks {
			if _, exists := hooks[k]; !exists {
				hooks[k] = v
			}
		}
	}

	// Build layer definitions in order.
	var layers []LayerDef
	for _, name := range layerOrder {
		ls := layerMap[name]
		layers = append(layers, LayerDef{
			Name:        name,
			Patterns:    ls.patterns,
			MediaType:   manifest.LayerMediaType,
			WatchMethod: defaultWatchForLayer(name),
		})
	}

	// Project is always last, always catch-all.
	layers = append(layers, LayerDef{
		Name:        "project",
		Patterns:    CommonSourcePatterns,
		MediaType:   manifest.LayerMediaType,
		CatchAll:    true,
		WatchMethod: WatchRealtime,
	})

	return MergeResult{
		Layers: layers,
		Ignore: ignore,
		Hooks:  hooks,
	}
}

// defaultWatchForLayer returns the default watch method for a layer based on its name.
func defaultWatchForLayer(layerName string) string {
	switch layerName {
	case "deps", "agent":
		return WatchPeriodic
	default:
		return WatchRealtime
	}
}

// CommonSourcePatterns are source file glob patterns used by the project catch-all layer.
var CommonSourcePatterns = []string{
	"**/*.go", "**/*.py", "**/*.js", "**/*.ts", "**/*.jsx", "**/*.tsx",
	"**/*.rs", "**/*.java", "**/*.c", "**/*.cpp", "**/*.h",
	"**/*.rb", "**/*.erb", "**/*.rake",       // Ruby
	"**/*.ex", "**/*.exs", "**/*.eex", "**/*.heex", // Elixir
	"**/*.ml", "**/*.mli", "**/*.mll", "**/*.mly",  // OCaml
	"**/*.html", "**/*.css", "**/*.scss",
	"**/*.sql", "**/*.sh", "**/*.bash",
	"**/*.json", "**/*.yaml", "**/*.yml", "**/*.toml", "**/*.xml",
	"**/*.md", "**/*.txt", "**/*.csv",
	"Makefile", "Dockerfile", "docker-compose*.yaml",
	"go.mod", "go.sum",
	"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
	"bun.lockb", "bun.lock", "bunfig.toml",
	"deno.json", "deno.jsonc", "deno.lock",
	"pyproject.toml", "requirements*.txt", "Pipfile", "Pipfile.lock", "uv.lock",
	"Cargo.toml", "Cargo.lock",
	"Gemfile", "Gemfile.lock", "Rakefile",
	"mix.exs", "mix.lock",
	"dune-project", "dune-workspace", "*.opam",
	".gitignore", ".gitattributes",
	".env.example", ".env.template",
	".mcp.json",
	"**", // catch-all
}

// CommonSecretPatterns are the default secret detection patterns.
var CommonSecretPatterns = []string{
	`(?i)AKIA[0-9A-Z]{16}`,
	`(?i)sk-[a-zA-Z0-9]{20,}`,
	`ghp_[a-zA-Z0-9]{36}`,
	`glpat-[a-zA-Z0-9\-]{20,}`,
	`-----BEGIN (RSA |EC )?PRIVATE KEY`,
	`(?i)(password|passwd|pwd)\s*[:=]\s*[^\s${\}][^\s]*`,
}

// ExpandHome expands ~ prefix to the user's home directory.
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// PrefixReplacer returns a function that replaces a path prefix with a
// different prefix. If the path doesn't match, it's returned unchanged.
// Used by extensions to build NormalizePath/ResolvePath functions.
func PrefixReplacer(from, to string) func(string) string {
	return func(path string) string {
		if strings.HasPrefix(path, from+"/") {
			return to + path[len(from):]
		}
		if path == from {
			return to
		}
		return path
	}
}

// PortablePath converts an absolute path to a portable form for use in archive
// entry names. Home-directory paths are converted to /~/ so they restore
// correctly on machines with different usernames. The result always uses
// forward slashes so archive keys are consistent across platforms.
func PortablePath(absPath string) string {
	normalized := strings.ReplaceAll(absPath, string(filepath.Separator), "/")
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		normalizedHome := strings.ReplaceAll(home, string(filepath.Separator), "/")
		if strings.HasPrefix(normalized, normalizedHome+"/") {
			return "/~/" + normalized[len(normalizedHome)+1:]
		}
	}
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return normalized
}

// IsExternalPattern returns true if the pattern refers to a path outside the workspace.
func IsExternalPattern(pattern string) bool {
	return strings.HasPrefix(pattern, "~/") || strings.HasPrefix(pattern, "/") || filepath.IsAbs(pattern)
}
