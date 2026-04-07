package extension

import (
	"os"
	"path/filepath"
)

// ---------------------------------------------------------------------------
// Node / JavaScript / TypeScript ecosystem
// ---------------------------------------------------------------------------

// Node detects JS/TS projects across all major runtimes and package managers
// (npm, yarn, pnpm, bun, deno) and adds node_modules to the deps layer.
type Node struct{}

func (n Node) Name() string                                     { return "node" }
func (n Node) NormalizePath(_ string) func(path string) string   { return nil }
func (n Node) ResolvePath(_ string) func(path string) string     { return nil }

func (n Node) Detect(workDir string) bool {
	// npm / yarn / pnpm / bun all use package.json.
	markers := []string{
		"package.json",
		"bun.lockb",    // bun (binary lockfile, legacy)
		"bun.lock",     // bun (text lockfile, v1.2+)
		"bunfig.toml",  // bun config
		"deno.json",    // deno
		"deno.jsonc",   // deno (jsonc variant)
		"deno.lock",    // deno lockfile
	}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(workDir, m)); err == nil {
			return true
		}
	}
	return false
}

func (n Node) Contribute(workDir string) Contribution {
	// node_modules is used by npm, yarn, pnpm, and bun.
	// Deno can also create node_modules/ with --node-modules-dir.
	patterns := []string{"node_modules/**"}

	// Deno vendor directory (hermetic builds with vendor: true in deno.json).
	// Only add if it exists so we don't collide with Go's vendor/.
	vendorDir := filepath.Join(workDir, "vendor")
	if info, err := os.Stat(vendorDir); err == nil && info.IsDir() {
		// Only claim vendor/ if this looks like a deno project, not a Go project.
		if hasDeno(workDir) && !hasFile(workDir, "go.mod") {
			patterns = append(patterns, "vendor/**")
		}
	}

	return Contribution{
		Layers: map[string][]string{
			"deps": patterns,
		},
	}
}

// hasDeno returns true if deno markers exist in the workspace.
func hasDeno(workDir string) bool {
	for _, m := range []string{"deno.json", "deno.jsonc", "deno.lock"} {
		if _, err := os.Stat(filepath.Join(workDir, m)); err == nil {
			return true
		}
	}
	return false
}

// hasFile returns true if the named file exists in workDir.
func hasFile(workDir, name string) bool {
	_, err := os.Stat(filepath.Join(workDir, name))
	return err == nil
}

// ---------------------------------------------------------------------------
// Python ecosystem
// ---------------------------------------------------------------------------

// Python detects Python projects (pip, uv, pipenv, poetry, setuptools) and
// adds .venv to the deps layer.
type Python struct{}

func (p Python) Name() string                                     { return "python" }
func (p Python) NormalizePath(_ string) func(path string) string   { return nil }
func (p Python) ResolvePath(_ string) func(path string) string     { return nil }

func (p Python) Detect(workDir string) bool {
	for _, marker := range []string{
		"pyproject.toml",
		"Pipfile",
		"setup.py",
		"uv.lock",          // uv package manager
		".python-version",  // pyenv / uv python version pin
	} {
		if _, err := os.Stat(filepath.Join(workDir, marker)); err == nil {
			return true
		}
	}
	// Check for requirements*.txt
	matches, _ := filepath.Glob(filepath.Join(workDir, "requirements*.txt"))
	if len(matches) > 0 {
		return true
	}
	// Check for .venv directory
	if info, err := os.Stat(filepath.Join(workDir, ".venv")); err == nil && info.IsDir() {
		return true
	}
	return false
}

func (p Python) Contribute(_ string) Contribution {
	return Contribution{
		Layers: map[string][]string{
			"deps": {".venv/**"},
		},
		Ignore: []string{"__pycache__/**", "*.pyc"},
	}
}

// ---------------------------------------------------------------------------
// Go ecosystem
// ---------------------------------------------------------------------------

// GoMod detects Go modules and adds vendor to deps.
type GoMod struct{}

func (g GoMod) Name() string                                     { return "go-mod" }
func (g GoMod) NormalizePath(_ string) func(path string) string   { return nil }
func (g GoMod) ResolvePath(_ string) func(path string) string     { return nil }

func (g GoMod) Detect(workDir string) bool {
	_, err := os.Stat(filepath.Join(workDir, "go.mod"))
	return err == nil
}

func (g GoMod) Contribute(_ string) Contribution {
	return Contribution{
		Layers: map[string][]string{
			"deps": {"vendor/**"},
		},
	}
}

// ---------------------------------------------------------------------------
// Rust ecosystem
// ---------------------------------------------------------------------------

// Rust detects Rust projects. The target/ directory goes into a build-cache
// extra layer because it is large and structurally different from deps like
// node_modules.
type Rust struct{}

func (r Rust) Name() string                                     { return "rust" }
func (r Rust) NormalizePath(_ string) func(path string) string   { return nil }
func (r Rust) ResolvePath(_ string) func(path string) string     { return nil }

func (r Rust) Detect(workDir string) bool {
	_, err := os.Stat(filepath.Join(workDir, "Cargo.toml"))
	return err == nil
}

func (r Rust) Contribute(_ string) Contribution {
	return Contribution{
		ExtraLayers: []LayerDef{
			{
				Name:        "build-cache",
				Patterns:    []string{"target/**"},
				MediaType:   "application/vnd.oci.image.layer.v1.tar+gzip",
				WatchMethod: WatchOff,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Ruby ecosystem
// ---------------------------------------------------------------------------

// Ruby detects Ruby/Rails projects and adds vendored gems and bundle config
// to the deps layer.
type Ruby struct{}

func (r Ruby) Name() string                                     { return "ruby" }
func (r Ruby) NormalizePath(_ string) func(path string) string   { return nil }
func (r Ruby) ResolvePath(_ string) func(path string) string     { return nil }

func (r Ruby) Detect(workDir string) bool {
	for _, marker := range []string{"Gemfile", "Gemfile.lock", "Rakefile"} {
		if _, err := os.Stat(filepath.Join(workDir, marker)); err == nil {
			return true
		}
	}
	// Check for *.gemspec files
	matches, _ := filepath.Glob(filepath.Join(workDir, "*.gemspec"))
	return len(matches) > 0
}

func (r Ruby) Contribute(_ string) Contribution {
	return Contribution{
		Layers: map[string][]string{
			"deps": {
				"vendor/bundle/**", // bundler --path vendor/bundle
				".bundle/**",       // bundler config and local state
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Elixir ecosystem
// ---------------------------------------------------------------------------

// Elixir detects Elixir/Phoenix projects. deps/ holds Hex packages and
// _build/ holds compiled BEAM files (placed in a build-cache layer).
type Elixir struct{}

func (e Elixir) Name() string                                     { return "elixir" }
func (e Elixir) NormalizePath(_ string) func(path string) string   { return nil }
func (e Elixir) ResolvePath(_ string) func(path string) string     { return nil }

func (e Elixir) Detect(workDir string) bool {
	_, err := os.Stat(filepath.Join(workDir, "mix.exs"))
	return err == nil
}

func (e Elixir) Contribute(_ string) Contribution {
	return Contribution{
		Layers: map[string][]string{
			"deps": {"deps/**"},
		},
		ExtraLayers: []LayerDef{
			{
				Name:        "build-cache",
				Patterns:    []string{"_build/**"},
				MediaType:   "application/vnd.oci.image.layer.v1.tar+gzip",
				WatchMethod: WatchOff,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// OCaml ecosystem
// ---------------------------------------------------------------------------

// OCaml detects OCaml projects using dune, opam, or esy. _opam/ and _esy/
// hold local package switches; _build/ holds dune build output (placed in a
// build-cache layer).
type OCaml struct{}

func (o OCaml) Name() string                                     { return "ocaml" }
func (o OCaml) NormalizePath(_ string) func(path string) string   { return nil }
func (o OCaml) ResolvePath(_ string) func(path string) string     { return nil }

func (o OCaml) Detect(workDir string) bool {
	for _, marker := range []string{"dune-project", "dune-workspace"} {
		if _, err := os.Stat(filepath.Join(workDir, marker)); err == nil {
			return true
		}
	}
	// Check for *.opam files
	matches, _ := filepath.Glob(filepath.Join(workDir, "*.opam"))
	return len(matches) > 0
}

func (o OCaml) Contribute(_ string) Contribution {
	return Contribution{
		Layers: map[string][]string{
			"deps": {
				"_opam/**", // local opam switch (opam switch create .)
				"_esy/**",  // esy package manager alternative
			},
		},
		ExtraLayers: []LayerDef{
			{
				Name:        "build-cache",
				Patterns:    []string{"_build/**"},
				MediaType:   "application/vnd.oci.image.layer.v1.tar+gzip",
				WatchMethod: WatchOff,
			},
		},
	}
}
