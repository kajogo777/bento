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
	Agent     string             `yaml:"agent,omitempty"`
	Task      string             `yaml:"task,omitempty"`
	Store     string             `yaml:"store,omitempty"`
	Remote    string             `yaml:"remote,omitempty"`
	Layers    []LayerConfig      `yaml:"layers,omitempty"`
	Ignore    []string           `yaml:"ignore,omitempty"`
	Env       map[string]string  `yaml:"env,omitempty"`
	Secrets   map[string]Secret  `yaml:"secrets,omitempty"`
	EnvFiles  map[string]EnvFile `yaml:"env_files,omitempty"`
	Hooks     HooksConfig        `yaml:"hooks,omitempty"`
	Retention RetentionConfig    `yaml:"retention,omitempty"`
}

// LayerConfig defines a layer in bento.yaml.
// Patterns starting with ~/ or / are treated as external paths.
type LayerConfig struct {
	Name     string   `yaml:"name"`
	Patterns []string `yaml:"patterns"`
}

// Secret represents a secret reference in bento.yaml.
type Secret struct {
	Source string            `yaml:"source"`
	Fields map[string]string `yaml:",inline"`
}

// EnvFile represents an env file template mapping.
type EnvFile struct {
	Template string   `yaml:"template,omitempty"`
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

// DefaultIgnorePatterns are always excluded from checkpoints.
var DefaultIgnorePatterns = []string{
	"bento.yaml",
	".bentoignore",
	"bin/**",
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
