package manifest

import "encoding/json"

// BentoConfigObj is the OCI config object stored as a blob in the registry.
type BentoConfigObj struct {
	SchemaVersion    string                `json:"schemaVersion"`
	Agent            string                `json:"agent,omitempty"`
	AgentVersion     string                `json:"agentVersion,omitempty"`
	Task             string                `json:"task,omitempty"`
	SessionID        string                `json:"sessionId,omitempty"`
	ParentCheckpoint string                `json:"parentCheckpoint,omitempty"`
	Checkpoint       int                   `json:"checkpoint"`
	Created          string                `json:"created"`
	Status           string                `json:"status,omitempty"`
	Harness          string                `json:"harness,omitempty"`
	GitSha           string                `json:"gitSha,omitempty"`
	GitBranch        string                `json:"gitBranch,omitempty"`
	Message          string                `json:"message,omitempty"`
	EnvFiles         map[string]EnvFileRef `json:"envFiles,omitempty"`
	Metrics          *Metrics              `json:"metrics,omitempty"`
	Environment      *Environment          `json:"environment,omitempty"`
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

// UnmarshalConfig deserializes JSON into a BentoConfigObj.
func UnmarshalConfig(data []byte) (*BentoConfigObj, error) {
	var cfg BentoConfigObj
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
