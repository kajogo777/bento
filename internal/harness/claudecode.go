package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeCode detects and configures the Claude Code agent framework.
type ClaudeCode struct{}

func (c ClaudeCode) Name() string { return "claude-code" }

func (c ClaudeCode) Detect(workDir string) bool {
	if info, err := os.Stat(filepath.Join(workDir, ".claude")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(workDir, "CLAUDE.md")); err == nil {
		return true
	}
	return false
}

func (c ClaudeCode) Layers() []LayerDef {
	return []LayerDef{
		{
			Name: "project",
			Patterns: []string{
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
			},
			MediaType: "application/vnd.bento.layer.project.v1.tar+gzip",
			Frequency: ChangesOften,
		},
		{
			Name:      "agent",
			Patterns:  []string{"CLAUDE.md", ".claude/**"},
			MediaType: "application/vnd.bento.layer.agent.v1.tar+gzip",
			Frequency: ChangesOften,
		},
		{
			Name:      "deps",
			Patterns:  []string{"node_modules/**", ".venv/**", "vendor/**", ".tool-versions"},
			MediaType: "application/vnd.bento.layer.deps.v1.tar+gzip",
			Frequency: ChangesRarely,
		},
	}
}

func (c ClaudeCode) SessionConfig(workDir string) (*SessionConfig, error) {
	cfg := &SessionConfig{
		Agent:  c.Name(),
		Status: "paused",
	}

	if out, err := exec.Command("claude", "--version").Output(); err == nil {
		cfg.AgentVersion = strings.TrimSpace(string(out))
	}

	if out, err := execGit(workDir, "rev-parse", "HEAD"); err == nil {
		cfg.GitSha = out
	}

	if out, err := execGit(workDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		cfg.GitBranch = out
	}

	return cfg, nil
}

func (c ClaudeCode) Ignore() []string {
	return commonIgnorePatterns()
}

func (c ClaudeCode) SecretPatterns() []string {
	return commonSecretPatterns()
}

func (c ClaudeCode) DefaultHooks() map[string]string {
	return nil
}

// execGit runs a git command in the given directory and returns trimmed output.
func execGit(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// commonIgnorePatterns returns the default ignore patterns shared across harnesses.
func commonIgnorePatterns() []string {
	return []string{
		".env", ".env.local", ".env.*.local",
		".claude/credentials", ".claude/oauth_tokens",
		"*.pem", "*.key", "*.p12", "token.json", "credentials",
		".DS_Store", "Thumbs.db",
		"*.swp", "*.swo", "*~",
		".git/**", "__pycache__/**", "*.pyc",
		"dist/**", "build/**",
	}
}

// commonSecretPatterns returns the default secret-detection regex patterns.
func commonSecretPatterns() []string {
	return []string{
		`(?i)AKIA[0-9A-Z]{16}`,
		`(?i)sk-[a-zA-Z0-9]{20,}`,
		`ghp_[a-zA-Z0-9]{36}`,
		`glpat-[a-zA-Z0-9\-]{20,}`,
		`-----BEGIN (RSA |EC )?PRIVATE KEY`,
		`(?i)(password|passwd|pwd)\s*[:=]`,
	}
}

// commonSourcePatterns returns the default source file glob patterns.
func commonSourcePatterns() []string {
	return []string{
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
}
