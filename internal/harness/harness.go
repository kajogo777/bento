package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kajogo777/bento/internal/manifest"
	"github.com/kajogo777/bento/internal/secrets"
)

// WatchMethod constants for per-layer watch behavior.
const (
	WatchRealtime = "realtime" // fsnotify — instant detection
	WatchPeriodic = "periodic" // polling — periodic fingerprint check
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

// SessionConfig holds metadata extracted from the workspace.
type SessionConfig struct {
	Agent        string `json:"agent"`
	AgentVersion string `json:"agentVersion,omitempty"`
	Status       string `json:"status"`
	GitSha       string `json:"gitSha,omitempty"`
	GitBranch    string `json:"gitBranch,omitempty"`
}

// Harness maps an agent framework's file layout to bento's layer taxonomy.
type Harness interface {
	Name() string
	Detect(workDir string) bool
	Layers(workDir string) []LayerDef
	SessionConfig(workDir string) (*SessionConfig, error)
	Ignore() []string
	SecretPatterns() []string
	DefaultHooks() map[string]string
}

// execGit runs a git command in the given directory and returns trimmed output.
func execGit(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// BaseSessionConfig returns a SessionConfig populated with git metadata.
func BaseSessionConfig(agentName, workDir string) *SessionConfig {
	cfg := &SessionConfig{Agent: agentName, Status: "paused"}
	if out, err := execGit(workDir, "rev-parse", "HEAD"); err == nil {
		cfg.GitSha = out
	}
	if out, err := execGit(workDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		cfg.GitBranch = out
	}
	return cfg
}

// --- Common layer builders ---

func AgentLayer(patterns []string) LayerDef {
	return LayerDef{
		Name:        "agent",
		Patterns:    patterns,
		MediaType:   manifest.MediaTypeAgent,
		WatchMethod: WatchPeriodic,
	}
}

func DepsLayer(patterns []string) LayerDef {
	return LayerDef{
		Name:        "deps",
		Patterns:    patterns,
		MediaType:   manifest.MediaTypeDeps,
		WatchMethod: WatchPeriodic,
	}
}

func ProjectLayer(patterns []string) LayerDef {
	return LayerDef{
		Name:        "project",
		Patterns:    patterns,
		MediaType:   manifest.MediaTypeProject,
		CatchAll:    true,
		WatchMethod: WatchRealtime,
	}
}

// CommonDepsPatterns are dependency directory patterns shared across harnesses.
var CommonDepsPatterns = []string{
	"node_modules/**",
	".venv/**",
	"vendor/**",
}

// CommonSourcePatterns are source file glob patterns shared across harnesses.
var CommonSourcePatterns = []string{
	"**/*.go", "**/*.py", "**/*.js", "**/*.ts", "**/*.jsx", "**/*.tsx",
	"**/*.rs", "**/*.java", "**/*.c", "**/*.cpp", "**/*.h",
	"**/*.html", "**/*.css", "**/*.scss",
	"**/*.sql", "**/*.sh", "**/*.bash",
	"**/*.json", "**/*.yaml", "**/*.yml", "**/*.toml", "**/*.xml",
	"**/*.md", "**/*.txt", "**/*.csv",
	"Makefile", "Dockerfile", "docker-compose*.yaml",
	"go.mod", "go.sum",
	"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
	"pyproject.toml", "requirements*.txt", "Pipfile", "Pipfile.lock",
	"Cargo.toml", "Cargo.lock",
	".gitignore", ".gitattributes",
	".env.example", ".env.template",
	".mcp.json",
	"**", // catch-all
}

// CommonIgnorePatterns are file patterns excluded from all layers.
var CommonIgnorePatterns = []string{
	".env", ".env.local", ".env.*.local",
	"*.pem", "*.key", "*.p12", "token.json", "credentials",
	".DS_Store", "Thumbs.db",
	"*.swp", "*.swo", "*~",
	".git/**", "__pycache__/**", "*.pyc",
	"dist/**", "build/**",
}

// CommonCredentialFiles are filenames excluded to prevent credential leakage.
var CommonCredentialFiles = []string{
	"auth.json", "oauth_tokens", "credentials.json",
	"*.sqlite", "*.db", "*.sqlite-shm", "*.sqlite-wal",
}

// CommonSecretPatterns are the default secret detection patterns.
var CommonSecretPatterns = secrets.DefaultPatterns

// ExpandHome expands ~ prefix to the user's home directory.
func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// IsExternalPattern returns true if the pattern refers to a path outside the workspace.
func IsExternalPattern(pattern string) bool {
	return strings.HasPrefix(pattern, "~/") || strings.HasPrefix(pattern, "/")
}
