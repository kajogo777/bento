package harness

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
