package harness

import "github.com/bentoci/bento/internal/manifest"

// ChangeFrequency indicates how often a layer changes.
type ChangeFrequency string

const (
	ChangesOften  ChangeFrequency = "often"
	ChangesRarely ChangeFrequency = "rarely"
)

// LayerDef defines a layer for file assignment.
type LayerDef struct {
	Name      string
	Patterns  []string
	MediaType string
	Frequency ChangeFrequency
	CatchAll  bool // if true, unmatched files fall into this layer
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
	// Name returns the harness identifier.
	Name() string

	// Detect returns true if this harness is active in the workspace.
	Detect(workDir string) bool

	// Layers returns the layer definitions for this harness.
	Layers() []LayerDef

	// SessionConfig extracts session metadata from the workspace.
	SessionConfig(workDir string) (*SessionConfig, error)

	// Ignore returns additional exclude patterns.
	Ignore() []string

	// SecretPatterns returns regex patterns for secret detection.
	SecretPatterns() []string

	// DefaultHooks returns suggested hooks for this agent framework.
	DefaultHooks() map[string]string
}

// --- Common layer builders ---
// These construct standard LayerDefs so harnesses don't repeat media types,
// patterns, and frequency values.

// AgentLayer returns a standard agent layer definition.
func AgentLayer(patterns []string) LayerDef {
	return LayerDef{
		Name:      "agent",
		Patterns:  patterns,
		MediaType: manifest.MediaTypeAgent,
		Frequency: ChangesOften,
	}
}

// DepsLayer returns a standard deps layer definition.
func DepsLayer(patterns []string) LayerDef {
	return LayerDef{
		Name:      "deps",
		Patterns:  patterns,
		MediaType: manifest.MediaTypeDeps,
		Frequency: ChangesRarely,
	}
}

// ProjectLayer returns a standard project catch-all layer definition.
func ProjectLayer(patterns []string) LayerDef {
	return LayerDef{
		Name:      "project",
		Patterns:  patterns,
		MediaType: manifest.MediaTypeProject,
		Frequency: ChangesOften,
		CatchAll:  true,
	}
}

// CommonDepsPatterns returns dependency directory patterns shared across harnesses.
var CommonDepsPatterns = []string{
	"node_modules/**",
	".venv/**",
	"vendor/**",
}

// CommonSourcePatterns returns source file glob patterns shared across harnesses.
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
}

// CommonIgnorePatterns returns file patterns that should be excluded from all layers.
var CommonIgnorePatterns = []string{
	".env", ".env.local", ".env.*.local",
	"*.pem", "*.key", "*.p12", "token.json", "credentials",
	".DS_Store", "Thumbs.db",
	"*.swp", "*.swo", "*~",
	".git/**", "__pycache__/**", "*.pyc",
	"dist/**", "build/**",
}

// CommonSecretPatterns returns regex patterns for detecting secrets in file content.
var CommonSecretPatterns = []string{
	`(?i)AKIA[0-9A-Z]{16}`,
	`(?i)sk-[a-zA-Z0-9]{20,}`,
	`ghp_[a-zA-Z0-9]{36}`,
	`glpat-[a-zA-Z0-9\-]{20,}`,
	`-----BEGIN (RSA |EC )?PRIVATE KEY`,
	`(?i)(password|passwd|pwd)\s*[:=]`,
}
