package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// BentoConfig represents the bento.yaml configuration file.
type BentoConfig struct {
	ID         string              `yaml:"id,omitempty"`
	Extensions []string            `yaml:"extensions,omitempty"`
	Task       string              `yaml:"task,omitempty"`
	Store      string              `yaml:"store,omitempty"`
	Remote     string              `yaml:"remote,omitempty"`
	Layers     []LayerConfig       `yaml:"layers,omitempty"`
	Ignore     []string            `yaml:"ignore,omitempty"`
	Env        map[string]EnvEntry `yaml:"env,omitempty"`
	Secrets        SecretsConfig        `yaml:"secrets,omitempty"`
	Recipients     []RecipientConfig    `yaml:"recipients,omitempty"`
	Hooks          HooksConfig          `yaml:"hooks,omitempty"`
	Retention      RetentionConfig      `yaml:"retention,omitempty"`
	Watch          WatchConfig          `yaml:"watch,omitempty"`
}

// LayerConfig defines a layer in bento.yaml.
// Patterns starting with ~/ or / are treated as external paths.
// CatchAll, when true, makes this layer capture all files not matched by other layers.
// Watch controls how this layer is monitored during `bento watch`:
// "realtime" (instant detection), "periodic" (check every ~30s), or "off" (not watched).
type LayerConfig struct {
	Name     string   `yaml:"name"`
	Patterns []string `yaml:"patterns"`
	CatchAll bool     `yaml:"catch_all,omitempty"`
	Watch    string   `yaml:"watch,omitempty"`
}

// EnvEntry represents a single environment variable in bento.yaml.
// It can be either a plain string value or a secret reference.
//
// Plain value in YAML:
//
//	env:
//	  NODE_ENV: development
//
// Secret reference in YAML:
//
//	env:
//	  DATABASE_URL:
//	    source: env
//	    var: DATABASE_URL
type EnvEntry struct {
	// Value holds a literal string value. Non-empty when IsRef is false.
	Value string
	// Source identifies the secret provider (env, file, exec, vault, etc.).
	// Non-empty when IsRef is true.
	Source string
	// Fields holds provider-specific fields (var, path, key, role, command).
	Fields map[string]string
	// IsRef is true when this entry is a secret reference, false for literals.
	IsRef bool
}

// NewLiteralEnv creates an EnvEntry for a plain string value.
func NewLiteralEnv(value string) EnvEntry {
	return EnvEntry{Value: value}
}

// NewSecretEnv creates an EnvEntry for a secret reference.
func NewSecretEnv(source string, fields map[string]string) EnvEntry {
	return EnvEntry{Source: source, Fields: fields, IsRef: true}
}

// UnmarshalYAML implements custom YAML unmarshaling for EnvEntry.
// Accepts either a scalar string or a mapping with source + fields.
func (e *EnvEntry) UnmarshalYAML(value *yaml.Node) error {
	// Scalar → literal value.
	if value.Kind == yaml.ScalarNode {
		e.Value = value.Value
		e.IsRef = false
		return nil
	}

	// Mapping → secret reference.
	if value.Kind == yaml.MappingNode {
		m := make(map[string]string)
		if err := value.Decode(&m); err != nil {
			return err
		}
		source, ok := m["source"]
		if !ok || source == "" {
			return fmt.Errorf("env entry has mapping form but no 'source' field")
		}
		e.Source = source
		delete(m, "source")
		e.Fields = m
		e.IsRef = true
		return nil
	}

	return fmt.Errorf("env entry must be a string or a mapping, got %v", value.Kind)
}

// MarshalYAML implements custom YAML marshaling for EnvEntry.
// Literals are emitted as scalars; references as mappings.
func (e EnvEntry) MarshalYAML() (interface{}, error) {
	if !e.IsRef {
		return e.Value, nil
	}
	m := make(map[string]string, len(e.Fields)+1)
	m["source"] = e.Source
	for k, v := range e.Fields {
		m[k] = v
	}
	return m, nil
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
	KeepLast   int             `yaml:"keep_last,omitempty"`
	KeepTagged bool            `yaml:"keep_tagged,omitempty"`
	Tiers      []RetentionTier `yaml:"tiers,omitempty"`
}

// RetentionTier defines a time-based retention tier for watch-mode auto-GC.
// Behavior depends on Resolution:
//   - nil (omitted in YAML): keep all checkpoints in this age range
//   - 0:                     keep none — delete all checkpoints in this age range
//   - >0 (e.g. 1h):         keep one checkpoint per interval (newest in each bucket)
type RetentionTier struct {
	MaxAge     time.Duration  `yaml:"max_age"`
	Resolution *time.Duration `yaml:"resolution,omitempty"`
}

// WatchConfig defines configuration for `bento watch` auto-checkpointing.
type WatchConfig struct {
	Debounce       int    `yaml:"debounce,omitempty"`         // seconds of quiet before saving; default 10
	Message        string `yaml:"message,omitempty"`          // checkpoint message; default "auto-save"
	SkipSecretScan bool   `yaml:"skip_secret_scan,omitempty"` // skip secret scanning on auto-saves
}

// Secret scan mode constants.
const (
	// SecretsModeScrub detects secrets and scrubs them from OCI layers,
	// storing real values locally + encrypted. This is the default.
	SecretsModeScrub = "scrub"

	// SecretsModeBlock detects secrets and aborts the save with an error.
	// Forces the user to remove secrets or add them to .gitleaksignore.
	SecretsModeBlock = "block"

	// SecretsModeOff disables secret scanning entirely.
	SecretsModeOff = "off"
)

// SecretsConfig controls how bento handles secrets detected by gitleaks.
type SecretsConfig struct {
	// Mode controls secret handling: "scrub" (default), "block", or "off".
	//
	//   scrub — auto-scrub secrets from OCI layers, store locally + encrypted
	//   block — abort save if secrets are detected (strict mode)
	//   off   — skip secret scanning entirely
	//
	// When empty, the first save that detects secrets prompts the user
	// to choose (interactive) or defaults to "scrub" (non-interactive).
	Mode string `yaml:"mode,omitempty"`
}

// RecipientConfig defines a recipient entry in bento.yaml for key wrapping.
type RecipientConfig struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
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
	".git/**",
	".env", ".env.local", ".env.*.local",
	"*.pem", "*.key", "*.p12", "token.json", "credentials",
	"auth.json", "oauth_tokens", "credentials.json",
	"*.sqlite", "*.db", "*.sqlite-shm", "*.sqlite-wal",
	".DS_Store", "Thumbs.db",
	"*.swp", "*.swo", "*~",
}

// GenerateWorkspaceID creates a new workspace identifier in the format ws-<random>.
func GenerateWorkspaceID() (string, error) {
	b := make([]byte, 8) // 16 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating workspace id: %w", err)
	}
	return "ws-" + hex.EncodeToString(b), nil
}

// StorePath returns the full path to this workspace's OCI store directory.
// It joins the configured store root with the workspace ID.
func (c *BentoConfig) StorePath() string {
	return filepath.Join(c.Store, c.ID)
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

			// Validate watch value.
			if l.Watch != "" {
				switch l.Watch {
				case "realtime", "periodic", "off":
					// valid
				default:
					return fmt.Errorf("layer %q has invalid watch value %q — must be \"realtime\", \"periodic\", or \"off\"", l.Name, l.Watch)
				}
			}
		}
	}

	// Validate env entries with secret references.
	for name, entry := range c.Env {
		if entry.IsRef && entry.Source == "" {
			return fmt.Errorf("env %q has a reference form but no source — set source: to the secret provider (e.g. env, file, exec)", name)
		}
	}

	// Validate secrets config.
	if c.Secrets.Mode != "" {
		switch c.Secrets.Mode {
		case SecretsModeScrub, SecretsModeBlock, SecretsModeOff:
			// valid
		default:
			return fmt.Errorf("secrets.mode %q is invalid — must be %q, %q, or %q", c.Secrets.Mode, SecretsModeScrub, SecretsModeBlock, SecretsModeOff)
		}
	}

	// Validate recipients config.
	if len(c.Recipients) > 0 {
		seenNames := make(map[string]bool)
		for i, r := range c.Recipients {
			if r.Name == "" {
				return fmt.Errorf("recipients[%d] has an empty name", i)
			}
			if seenNames[r.Name] {
				return fmt.Errorf("duplicate recipient name %q — each recipient must have a unique name", r.Name)
			}
			seenNames[r.Name] = true
			if r.Key == "" {
				return fmt.Errorf("recipients[%d] (%q) has an empty key", i, r.Name)
			}
			if !strings.HasPrefix(r.Key, "bento-pk-") {
				return fmt.Errorf("recipients[%d] (%q): key must start with \"bento-pk-\"", i, r.Name)
			}
			// Validate decoded key is exactly 32 bytes.
			keyData, decErr := base64DecodeRawURL(r.Key[len("bento-pk-"):])
			if decErr != nil {
				return fmt.Errorf("recipients[%d] (%q): invalid base64url in key: %w", i, r.Name, decErr)
			}
			if len(keyData) != 32 {
				return fmt.Errorf("recipients[%d] (%q): decoded key must be exactly 32 bytes, got %d", i, r.Name, len(keyData))
			}
		}
	}

	// Validate watch config.
	if c.Watch.Debounce != 0 && c.Watch.Debounce < 1 {
		return fmt.Errorf("watch.debounce must be >= 1 second (got %d)", c.Watch.Debounce)
	}

	// Validate retention config.
	if c.Retention.KeepLast < 0 {
		return fmt.Errorf("retention.keep_last must be >= 0 (got %d)", c.Retention.KeepLast)
	}
	if len(c.Retention.Tiers) > 0 {
		for i, tier := range c.Retention.Tiers {
			if tier.MaxAge <= 0 {
				return fmt.Errorf("retention.tiers[%d]: max_age must be a positive duration (e.g. \"1h\", \"24h\")", i)
			}
			if tier.Resolution != nil {
				r := *tier.Resolution
				if r < 0 {
					return fmt.Errorf("retention.tiers[%d]: resolution must be >= 0", i)
				}
				if r > 0 && r >= tier.MaxAge {
					return fmt.Errorf("retention.tiers[%d]: resolution (%v) must be smaller than max_age (%v)", i, r, tier.MaxAge)
				}
			}
			if i > 0 && tier.MaxAge <= c.Retention.Tiers[i-1].MaxAge {
				return fmt.Errorf("retention.tiers[%d]: max_age (%v) must be larger than previous tier (%v)", i, tier.MaxAge, c.Retention.Tiers[i-1].MaxAge)
			}
		}
	}

	// Validate hooks config.
	if c.Hooks.Timeout < 0 {
		return fmt.Errorf("hooks.timeout must be >= 0 (got %d)", c.Hooks.Timeout)
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

	// Migrate: assign a workspace ID if missing (pre-v0.4 workspaces).
	if cfg.ID == "" {
		if err := migrateWorkspaceID(dir, cfg); err != nil {
			return nil, fmt.Errorf("migrating workspace id: %w", err)
		}
	}

	return cfg, nil
}

// migrateWorkspaceID generates a new workspace ID for a pre-v0.4 workspace,
// renames the old basename-keyed store to the new ID, and persists the updated
// bento.yaml so the migration is a one-time operation.
func migrateWorkspaceID(dir string, cfg *BentoConfig) error {
	id, err := GenerateWorkspaceID()
	if err != nil {
		return err
	}
	cfg.ID = id

	// If an old store exists under the basename convention, rename it.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	oldStorePath := filepath.Join(cfg.Store, filepath.Base(absDir))
	newStorePath := cfg.StorePath()

	if info, statErr := os.Stat(oldStorePath); statErr == nil && info.IsDir() {
		// Only rename if the new path doesn't already exist.
		if _, statErr := os.Stat(newStorePath); os.IsNotExist(statErr) {
			if err := os.Rename(oldStorePath, newStorePath); err != nil {
				return fmt.Errorf("renaming store %s → %s: %w", oldStorePath, newStorePath, err)
			}
		}
	}

	// Persist the ID back to bento.yaml.
	if err := Save(dir, cfg); err != nil {
		return fmt.Errorf("saving migrated bento.yaml: %w", err)
	}

	return nil
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

// base64DecodeRawURL decodes a base64url-encoded string (no padding).
func base64DecodeRawURL(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
