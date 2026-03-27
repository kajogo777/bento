package manifest

import (
	"testing"
)

func TestMarshalUnmarshalRoundtrip(t *testing.T) {
	cfg := &BentoConfigObj{
		SchemaVersion:    "1.0",
		Agent:            "claude",
		AgentVersion:     "3.5",
		Task:             "build-feature",
		SessionID:        "sess-abc123",
		ParentCheckpoint: "sha256:aabbcc",
		Checkpoint:       5,
		Created:          "2025-01-15T10:00:00Z",
		Status:           "completed",
		Harness:          "docker",
		GitSha:           "deadbeef",
		GitBranch:        "main",
		Message:          "checkpoint after refactor",
		EnvFiles: map[string]EnvFileRef{
			".env": {
				Template: "KEY={{secret}}",
				Secrets:  []string{"MY_SECRET"},
			},
		},
		Metrics: &Metrics{
			TokenUsage: 1500,
			Duration:   "3m20s",
			LayerCount: 4,
		},
		Environment: &Environment{
			OS:   "linux",
			Arch: "amd64",
		},
	}

	data, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig failed: %v", err)
	}

	got, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("UnmarshalConfig failed: %v", err)
	}

	// Verify all fields survived the roundtrip.
	if got.SchemaVersion != cfg.SchemaVersion {
		t.Errorf("SchemaVersion: got %q, want %q", got.SchemaVersion, cfg.SchemaVersion)
	}
	if got.Agent != cfg.Agent {
		t.Errorf("Agent: got %q, want %q", got.Agent, cfg.Agent)
	}
	if got.AgentVersion != cfg.AgentVersion {
		t.Errorf("AgentVersion: got %q, want %q", got.AgentVersion, cfg.AgentVersion)
	}
	if got.Task != cfg.Task {
		t.Errorf("Task: got %q, want %q", got.Task, cfg.Task)
	}
	if got.SessionID != cfg.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, cfg.SessionID)
	}
	if got.ParentCheckpoint != cfg.ParentCheckpoint {
		t.Errorf("ParentCheckpoint: got %q, want %q", got.ParentCheckpoint, cfg.ParentCheckpoint)
	}
	if got.Checkpoint != cfg.Checkpoint {
		t.Errorf("Checkpoint: got %d, want %d", got.Checkpoint, cfg.Checkpoint)
	}
	if got.Created != cfg.Created {
		t.Errorf("Created: got %q, want %q", got.Created, cfg.Created)
	}
	if got.Status != cfg.Status {
		t.Errorf("Status: got %q, want %q", got.Status, cfg.Status)
	}
	if got.Harness != cfg.Harness {
		t.Errorf("Harness: got %q, want %q", got.Harness, cfg.Harness)
	}
	if got.GitSha != cfg.GitSha {
		t.Errorf("GitSha: got %q, want %q", got.GitSha, cfg.GitSha)
	}
	if got.GitBranch != cfg.GitBranch {
		t.Errorf("GitBranch: got %q, want %q", got.GitBranch, cfg.GitBranch)
	}
	if got.Message != cfg.Message {
		t.Errorf("Message: got %q, want %q", got.Message, cfg.Message)
	}

	// EnvFiles
	if len(got.EnvFiles) != 1 {
		t.Fatalf("EnvFiles length: got %d, want 1", len(got.EnvFiles))
	}
	envRef, ok := got.EnvFiles[".env"]
	if !ok {
		t.Fatal("EnvFiles missing .env key")
	}
	if envRef.Template != "KEY={{secret}}" {
		t.Errorf("EnvFiles[.env].Template: got %q, want %q", envRef.Template, "KEY={{secret}}")
	}
	if len(envRef.Secrets) != 1 || envRef.Secrets[0] != "MY_SECRET" {
		t.Errorf("EnvFiles[.env].Secrets: got %v, want [MY_SECRET]", envRef.Secrets)
	}

	// Metrics
	if got.Metrics == nil {
		t.Fatal("Metrics is nil")
	}
	if got.Metrics.TokenUsage != 1500 {
		t.Errorf("Metrics.TokenUsage: got %d, want 1500", got.Metrics.TokenUsage)
	}
	if got.Metrics.Duration != "3m20s" {
		t.Errorf("Metrics.Duration: got %q, want %q", got.Metrics.Duration, "3m20s")
	}
	if got.Metrics.LayerCount != 4 {
		t.Errorf("Metrics.LayerCount: got %d, want 4", got.Metrics.LayerCount)
	}

	// Environment
	if got.Environment == nil {
		t.Fatal("Environment is nil")
	}
	if got.Environment.OS != "linux" {
		t.Errorf("Environment.OS: got %q, want %q", got.Environment.OS, "linux")
	}
	if got.Environment.Arch != "amd64" {
		t.Errorf("Environment.Arch: got %q, want %q", got.Environment.Arch, "amd64")
	}
}

func TestMarshalUnmarshalRoundtrip_OptionalFieldsEmpty(t *testing.T) {
	cfg := &BentoConfigObj{
		SchemaVersion: "1.0",
		Checkpoint:    0,
		Created:       "2025-01-15T10:00:00Z",
	}

	data, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig failed: %v", err)
	}

	got, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("UnmarshalConfig failed: %v", err)
	}

	if got.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion: got %q, want %q", got.SchemaVersion, "1.0")
	}
	if got.Checkpoint != 0 {
		t.Errorf("Checkpoint: got %d, want 0", got.Checkpoint)
	}
	if got.Created != "2025-01-15T10:00:00Z" {
		t.Errorf("Created: got %q, want %q", got.Created, "2025-01-15T10:00:00Z")
	}

	// Optional fields should be zero values.
	if got.Agent != "" {
		t.Errorf("Agent should be empty, got %q", got.Agent)
	}
	if got.AgentVersion != "" {
		t.Errorf("AgentVersion should be empty, got %q", got.AgentVersion)
	}
	if got.Task != "" {
		t.Errorf("Task should be empty, got %q", got.Task)
	}
	if got.SessionID != "" {
		t.Errorf("SessionID should be empty, got %q", got.SessionID)
	}
	if got.ParentCheckpoint != "" {
		t.Errorf("ParentCheckpoint should be empty, got %q", got.ParentCheckpoint)
	}
	if got.Status != "" {
		t.Errorf("Status should be empty, got %q", got.Status)
	}
	if got.Harness != "" {
		t.Errorf("Harness should be empty, got %q", got.Harness)
	}
	if got.GitSha != "" {
		t.Errorf("GitSha should be empty, got %q", got.GitSha)
	}
	if got.GitBranch != "" {
		t.Errorf("GitBranch should be empty, got %q", got.GitBranch)
	}
	if got.Message != "" {
		t.Errorf("Message should be empty, got %q", got.Message)
	}
	if got.EnvFiles != nil {
		t.Errorf("EnvFiles should be nil, got %v", got.EnvFiles)
	}
	if got.Metrics != nil {
		t.Errorf("Metrics should be nil, got %v", got.Metrics)
	}
	if got.Environment != nil {
		t.Errorf("Environment should be nil, got %v", got.Environment)
	}
}

func TestUnmarshalConfig_InvalidJSON(t *testing.T) {
	_, err := UnmarshalConfig([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
