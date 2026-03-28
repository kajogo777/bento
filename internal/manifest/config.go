package manifest

import (
	"encoding/json"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// BentoConfigObj holds bento-specific metadata for a checkpoint.
type BentoConfigObj struct {
	SchemaVersion    string                `json:"schemaVersion"`
	WorkspaceID      string                `json:"workspaceId,omitempty"`
	Agent            string                `json:"agent,omitempty"`
	AgentVersion     string                `json:"agentVersion,omitempty"`
	Task             string                `json:"task,omitempty"`
	ParentCheckpoint string                `json:"parentCheckpoint,omitempty"`
	Checkpoint       int                   `json:"checkpoint"`
	Created          string                `json:"created"`
	Status           string                `json:"status,omitempty"`
	Harness          string                `json:"harness,omitempty"`
	GitSha           string                `json:"gitSha,omitempty"`
	GitBranch        string                `json:"gitBranch,omitempty"`
	Message          string                `json:"message,omitempty"`
	// Env holds plain key-value environment variables to set on restore.
	// Values are stored verbatim; do NOT put secrets here.
	Env map[string]string `json:"env,omitempty"`
	// Secrets maps variable names to references for secrets to resolve on restore.
	// Only references (provider + path) are stored, never secret values.
	Secrets  map[string]SecretRef  `json:"secrets,omitempty"`
	EnvFiles map[string]EnvFileRef `json:"envFiles,omitempty"`
	Metrics  *Metrics              `json:"metrics,omitempty"`
	Environment *Environment       `json:"environment,omitempty"`

	// Portable workspace configuration embedded for "open anywhere" support.
	// These fields allow `bento open` to regenerate a working bento.yaml
	// in a fresh directory without requiring `bento init`.
	// See specs/portable-config.md for the full specification.
	Remote    string          `json:"remote,omitempty"`
	Layers    []LayerDef      `json:"layers,omitempty"`
	Hooks     *HooksDef       `json:"hooks,omitempty"`
	Ignore    []string        `json:"ignore,omitempty"`
	Retention *RetentionDef   `json:"retention,omitempty"`
}

// LayerDef describes a custom layer definition for embedding in the OCI config.
type LayerDef struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns,omitempty"`
	CatchAll bool     `json:"catchAll,omitempty"`
}

// HooksDef describes lifecycle hooks for embedding in the OCI config.
type HooksDef struct {
	PreSave     string `json:"preSave,omitempty"`
	PostSave    string `json:"postSave,omitempty"`
	PostRestore string `json:"postRestore,omitempty"`
	PrePush     string `json:"prePush,omitempty"`
	PostPush    string `json:"postPush,omitempty"`
	PostFork    string `json:"postFork,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
}

// RetentionDef describes GC retention policy for embedding in the OCI config.
type RetentionDef struct {
	KeepLast   int  `json:"keepLast,omitempty"`
	KeepTagged bool `json:"keepTagged,omitempty"`
}

// SecretRef describes how to resolve a secret at restore time.
// Only the reference is stored in the manifest, never the secret value.
type SecretRef struct {
	Source string `json:"source"`           // vault, env, aws-sts, 1password, gcloud, azure, file, exec
	Path   string `json:"path,omitempty"`   // vault path, file path, or 1password item
	Key    string `json:"key,omitempty"`    // field within the secret
	Var    string `json:"var,omitempty"`    // source env var name (source=env)
	Role   string `json:"role,omitempty"`   // IAM role ARN (source=aws-sts)
	Command string `json:"command,omitempty"` // shell command (source=exec)
}

// EnvFileRef describes a templated env file and the secrets it references.
type EnvFileRef struct {
	Template string   `json:"template"`
	Secrets  []string `json:"secrets"`
}

// Metrics holds runtime metrics for a checkpoint.
type Metrics struct {
	TokenUsage int    `json:"tokenUsage"`
	Duration   string `json:"duration"`
	LayerCount int    `json:"layerCount"`
}

// Environment records the OS and architecture where the checkpoint was created.
type Environment struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// MarshalConfig serializes a BentoConfigObj to JSON.
func MarshalConfig(cfg *BentoConfigObj) ([]byte, error) {
	return json.Marshal(cfg)
}

// UnmarshalConfig extracts BentoConfigObj from OCI image config bytes.
// The bento metadata is stored in config.Labels["dev.bento.config"] as JSON.
// Falls back to direct unmarshal for backward compatibility with older formats.
func UnmarshalConfig(data []byte) (*BentoConfigObj, error) {
	// Try OCI image config format first (current format)
	var imageConfig ocispec.Image
	if err := json.Unmarshal(data, &imageConfig); err == nil {
		if bentoJSON, ok := imageConfig.Config.Labels["dev.bento.config"]; ok {
			var cfg BentoConfigObj
			if err := json.Unmarshal([]byte(bentoJSON), &cfg); err != nil {
				return nil, err
			}
			return &cfg, nil
		}
	}

	// Fall back to direct bento config format (legacy)
	var cfg BentoConfigObj
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
