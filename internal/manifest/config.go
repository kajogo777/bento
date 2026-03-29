package manifest

import (
	"encoding/json"
	"fmt"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// BentoConfigObj holds bento-specific metadata for a checkpoint.
type BentoConfigObj struct {
	SchemaVersion    string                `json:"schemaVersion"`
	WorkspaceID      string                `json:"workspaceId,omitempty"`
	Extensions       []string              `json:"extensions,omitempty"`
	Task             string                `json:"task,omitempty"`
	ParentCheckpoint string                `json:"parentCheckpoint,omitempty"`
	Checkpoint       int                   `json:"checkpoint"`
	Created          string                `json:"created"`
	Status           string                `json:"status,omitempty"`
	GitSha           string                `json:"gitSha,omitempty"`
	GitBranch        string                `json:"gitBranch,omitempty"`
	Message          string                `json:"message,omitempty"`
	// Env holds environment variables — each entry is either a plain string value
	// or a secret reference (object with source + provider fields).
	// Secret references store only pointers, never actual secret values.
	Env map[string]ManifestEnvEntry `json:"env,omitempty"`
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

// ManifestEnvEntry represents an env var in the OCI manifest config.
// It can be either a plain string or a secret reference object.
// In JSON: a string value serializes as a JSON string, a secret reference
// serializes as an object with "source" and provider-specific fields.
type ManifestEnvEntry struct {
	// Value holds a literal string. Non-empty when IsRef is false.
	Value string
	// Source identifies the secret provider. Non-empty when IsRef is true.
	Source string `json:"source,omitempty"`
	// Path is the vault/file path.
	Path string `json:"path,omitempty"`
	// Key is the field within the secret.
	Key string `json:"key,omitempty"`
	// Var is the source env var name (source=env).
	Var string `json:"var,omitempty"`
	// Role is the IAM role ARN (source=aws-sts).
	Role string `json:"role,omitempty"`
	// Command is the shell command (source=exec).
	Command string `json:"command,omitempty"`
	// IsRef distinguishes literals from references.
	IsRef bool `json:"-"`
}

// MarshalJSON emits a plain JSON string for literals or an object for refs.
func (e ManifestEnvEntry) MarshalJSON() ([]byte, error) {
	if !e.IsRef {
		return json.Marshal(e.Value)
	}
	// Build a clean map with only non-empty fields.
	m := map[string]string{"source": e.Source}
	if e.Path != "" {
		m["path"] = e.Path
	}
	if e.Key != "" {
		m["key"] = e.Key
	}
	if e.Var != "" {
		m["var"] = e.Var
	}
	if e.Role != "" {
		m["role"] = e.Role
	}
	if e.Command != "" {
		m["command"] = e.Command
	}
	return json.Marshal(m)
}

// UnmarshalJSON accepts either a JSON string (literal) or object (secret ref).
func (e *ManifestEnvEntry) UnmarshalJSON(data []byte) error {
	// Try string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		e.Value = s
		e.IsRef = false
		return nil
	}
	// Try object.
	type refFields struct {
		Source  string `json:"source"`
		Path    string `json:"path"`
		Key     string `json:"key"`
		Var     string `json:"var"`
		Role    string `json:"role"`
		Command string `json:"command"`
	}
	var rf refFields
	if err := json.Unmarshal(data, &rf); err != nil {
		return err
	}
	if rf.Source == "" {
		return fmt.Errorf("env entry object missing required 'source' field")
	}
	e.Source = rf.Source
	e.Path = rf.Path
	e.Key = rf.Key
	e.Var = rf.Var
	e.Role = rf.Role
	e.Command = rf.Command
	e.IsRef = true
	return nil
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
