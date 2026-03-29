package extension

import (
	"os"
	"path/filepath"
)

// Node detects Node.js projects and adds node_modules to the deps layer.
type Node struct{}

func (n Node) Name() string { return "node" }

func (n Node) Detect(workDir string) bool {
	_, err := os.Stat(filepath.Join(workDir, "package.json"))
	return err == nil
}

func (n Node) Contribute(_ string) Contribution {
	return Contribution{
		Layers: map[string][]string{
			"deps": {"node_modules/**"},
		},
	}
}

// Python detects Python projects and adds .venv and __pycache__ to deps.
type Python struct{}

func (p Python) Name() string { return "python" }

func (p Python) Detect(workDir string) bool {
	for _, marker := range []string{"pyproject.toml", "Pipfile", "setup.py"} {
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

// GoMod detects Go modules and adds vendor to deps.
type GoMod struct{}

func (g GoMod) Name() string { return "go-mod" }

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

// Rust detects Rust projects. The target/ directory goes into a build-cache extra layer
// because it's large and structurally different from deps like node_modules.
type Rust struct{}

func (r Rust) Name() string { return "rust" }

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
