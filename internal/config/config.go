package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// BentoConfig represents the bento.yaml configuration file.
type BentoConfig struct {
	Store    string            `yaml:"store"`
	Remote   string            `yaml:"remote,omitempty"`
	Sync     string            `yaml:"sync,omitempty"`
	Harness  string            `yaml:"harness,omitempty"`
	Task     string            `yaml:"task,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
	Secrets  map[string]Secret `yaml:"secrets,omitempty"`
	EnvFiles map[string]EnvFile `yaml:"env_files,omitempty"`
	Ignore   []string          `yaml:"ignore,omitempty"`
	Hooks    HooksConfig       `yaml:"hooks,omitempty"`
	Retention RetentionConfig  `yaml:"retention,omitempty"`

	// HarnessInline allows defining a custom harness inline.
	HarnessInline *InlineHarness `yaml:"harness_config,omitempty"`
}

// Secret represents a secret reference in bento.yaml.
type Secret struct {
	Source string            `yaml:"source"`
	Fields map[string]string `yaml:",inline"`
}

// EnvFile represents an env file template mapping.
type EnvFile struct {
	Template string   `yaml:"template"`
	Secrets  []string `yaml:"secrets"`
}

// HooksConfig defines lifecycle hooks.
type HooksConfig struct {
	PreSave     string `yaml:"pre_save,omitempty"`
	PostSave    string `yaml:"post_save,omitempty"`
	PostRestore string `yaml:"post_restore,omitempty"`
	PrePush     string `yaml:"pre_push,omitempty"`
	PostPush    string `yaml:"post_push,omitempty"`
	PostFork    string `yaml:"post_fork,omitempty"`
	Timeout     int    `yaml:"timeout,omitempty"` // seconds, default 300
}

// RetentionConfig defines garbage collection policy.
type RetentionConfig struct {
	KeepLast   int  `yaml:"keep_last,omitempty"`
	KeepTagged bool `yaml:"keep_tagged,omitempty"`
}

// InlineHarness defines a custom harness in bento.yaml.
type InlineHarness struct {
	Name           string              `yaml:"name"`
	Detect         string              `yaml:"detect"`
	Layers         []InlineLayerDef    `yaml:"layers"`
	Ignore         []string            `yaml:"ignore,omitempty"`
	SecretPatterns []string            `yaml:"secret_patterns,omitempty"`
	Hooks          map[string]string   `yaml:"hooks,omitempty"`
}

// InlineLayerDef defines a layer in a YAML harness definition.
type InlineLayerDef struct {
	Name      string   `yaml:"name"`
	Patterns  []string `yaml:"patterns"`
	MediaType string   `yaml:"media_type,omitempty"`
	Frequency string   `yaml:"frequency,omitempty"`
}

// DefaultStorePath returns the platform-appropriate default store location.
func DefaultStorePath() string {
	switch runtime.GOOS {
	case "linux":
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "bento", "store")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "bento", "store")
	case "windows":
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "bento", "store")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "AppData", "Local", "bento", "store")
	default: // darwin
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".bento", "store")
	}
}

// Load reads and parses a bento.yaml file from the given directory.
func Load(dir string) (*BentoConfig, error) {
	path := filepath.Join(dir, "bento.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading bento.yaml: %w", err)
	}

	cfg := &BentoConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing bento.yaml: %w", err)
	}

	if cfg.Store == "" {
		cfg.Store = DefaultStorePath()
	} else {
		cfg.Store = expandPath(cfg.Store)
	}

	if cfg.Sync == "" {
		cfg.Sync = "manual"
	}

	return cfg, nil
}

// Save writes a BentoConfig to bento.yaml in the given directory.
func Save(dir string, cfg *BentoConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling bento.yaml: %w", err)
	}
	path := filepath.Join(dir, "bento.yaml")
	return os.WriteFile(path, data, 0644)
}

// expandPath expands ~ and environment variables in a path.
func expandPath(p string) string {
	if len(p) > 0 && p[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	return os.ExpandEnv(p)
}
