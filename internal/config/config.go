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
// CatchAll, when true, makes this layer capture all files not matched by other layers.
type LayerConfig struct {
	Name     string   `yaml:"name"`
	Patterns []string `yaml:"patterns"`
	CatchAll bool     `yaml:"catch_all,omitempty"`
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

// Validate checks the configuration for errors that would cause silent data
// loss or corrupt checkpoints. Returns the first hard error found.
func (c *BentoConfig) Validate() error {
	// Validate layer definitions when custom layers are configured.
	if len(c.Layers) > 0 {
		seen := make(map[string]bool)
		catchAllCount := 0

		for i, l := range c.Layers {
			if l.Name == "" {
				return fmt.Errorf("layer %d has an empty name", i)
			}
			if seen[l.Name] {
				return fmt.Errorf("duplicate layer name %q — each layer must have a unique name", l.Name)
			}
			seen[l.Name] = true

			isCatchAll := l.CatchAll || l.Name == "project"
			if isCatchAll {
				catchAllCount++
			}
			if catchAllCount > 1 {
				return fmt.Errorf("layer %q: only one catch_all layer is allowed", l.Name)
			}

			if len(l.Patterns) == 0 && !isCatchAll {
				return fmt.Errorf("layer %q has no patterns and is not a catch_all — it will always be empty; add patterns or set catch_all: true", l.Name)
			}
		}
	}

	// Validate secrets regardless of whether layers are configured.
	for name, s := range c.Secrets {
		if s.Source == "" {
			return fmt.Errorf("secret %q has no source — set source: to the secret provider (e.g. aws_ssm, env)", name)
		}
	}

	return nil
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

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("bento.yaml: %w", err)
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
