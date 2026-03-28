# Project Scaffold

This document describes the Go project structure, dependencies, build setup, CI configuration, and credential provider interface for the bento CLI.

> **Note:** The module layout below is a **planning document**. Some files listed (e.g. `attach.go`, `watch.go`, `sandbox.go`, `mcp.go`, `auth.go`, `secrets/manifest.go`) are **roadmap items** not yet implemented. See the actual `internal/` directory for the current file tree.

## Module Layout

```
github.com/kajogo777/bento/
├── cmd/
│   └── bento/
│       └── main.go                  # entrypoint
├── internal/
│   ├── cli/                         # cobra command definitions
│   │   ├── root.go                  # root command, global flags
│   │   ├── init.go                  # bento init
│   │   ├── save.go                  # bento save
│   │   ├── open.go                  # bento open (restore)
│   │   ├── list.go                  # bento list
│   │   ├── diff.go                  # bento diff
│   │   ├── fork.go                  # bento fork
│   │   ├── tag.go                   # bento tag
│   │   ├── inspect.go               # bento inspect
│   │   ├── attach.go                # bento attach
│   │   ├── push.go                  # bento push
│   │   ├── gc.go                    # bento gc
│   │   ├── env.go                   # bento env show/set
│   │   ├── watch.go                 # bento watch
│   │   ├── sandbox.go               # bento sandbox start/resume
│   │   └── mcp.go                   # bento mcp-server
│   ├── workspace/                   # filesystem operations
│   │   ├── scanner.go               # file tree walking, pattern matching
│   │   ├── scanner_test.go
│   │   ├── layer.go                 # layer packing (tar+gzip creation)
│   │   ├── layer_test.go
│   │   ├── ignore.go                # .bentoignore parsing
│   │   ├── ignore_test.go
│   │   ├── diff.go                  # checkpoint diffing
│   │   └── platform.go              # cross-platform path/permission handling
│   ├── registry/                    # OCI registry operations
│   │   ├── store.go                 # oras-go Target abstraction
│   │   ├── store_test.go
│   │   ├── local.go                 # OCI image layout store
│   │   ├── remote.go                # remote registry store
│   │   ├── resolve.go               # ref parsing (oci://, file://, registry)
│   │   └── auth.go                  # Docker credential helper integration
│   ├── manifest/                    # OCI manifest and config
│   │   ├── config.go                # SessionConfig struct, serialization
│   │   ├── config_test.go
│   │   ├── annotations.go           # annotation keys and builders
│   │   ├── manifest.go              # manifest construction
│   │   └── dag.go                   # checkpoint DAG traversal
│   ├── secrets/                     # secret reference management
│   │   ├── manifest.go              # secrets manifest parsing
│   │   ├── hydrate.go               # secret resolution orchestrator
│   │   ├── hydrate_test.go
│   │   ├── scan.go                  # pre-push secret scanning
│   │   ├── scan_test.go
│   │   ├── envfile.go               # .env template population
│   │   └── providers/               # credential provider implementations
│   │       ├── provider.go          # Provider interface
│   │       ├── env.go               # resolve from environment variables
│   │       ├── vault.go             # HashiCorp Vault
│   │       ├── onepassword.go       # 1Password CLI
│   │       ├── awssts.go            # AWS STS assume-role
│   │       ├── gcloud.go            # Google Cloud Secret Manager
│   │       ├── azure.go             # Azure Key Vault
│   │       ├── file.go              # read from local file
│   │       ├── exec.go              # run arbitrary command
│   │       └── registry.go          # provider registry and lookup
│   ├── harness/                     # agent framework adapters
│   │   ├── harness.go               # Harness interface, LayerDef, ChangeFrequency
│   │   ├── detect.go                # auto-detection logic (try each harness)
│   │   ├── yaml.go                  # YAML-defined harness loader
│   │   ├── yaml_test.go
│   │   ├── claudecode.go            # Claude Code harness
│   │   ├── openclaw.go              # OpenClaw harness
│   │   ├── opencode.go              # OpenCode harness
│   │   ├── cursor.go                # Cursor harness
│   │   ├── codex.go                 # Codex harness
│   │   ├── githubcopilot.go         # GitHub Copilot harness
│   │   ├── windsurf.go              # Windsurf harness
│   │   └── fallback.go              # default harness when no agent detected
│   ├── hooks/                       # lifecycle hook execution
│   │   ├── runner.go                # shell execution, timeout, platform handling
│   │   └── runner_test.go
│   ├── mcp/                         # MCP server implementation
│   │   ├── server.go                # stdio MCP server
│   │   ├── tools.go                 # tool definitions (save, list, restore, etc.)
│   │   └── server_test.go
│   ├── config/                      # bento.yaml parsing
│   │   ├── config.go                # BentoConfig struct
│   │   ├── config_test.go
│   │   └── defaults.go              # platform-specific defaults
│   └── policy/                      # retention and GC
│       ├── gc.go                    # garbage collection logic
│       └── gc_test.go
├── bento.yaml.example               # example config
├── .bentoignore.example             # example ignore file
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── .goreleaser.yaml
├── .github/
│   └── workflows/
│       ├── ci.yaml
│       ├── release.yaml
│       └── test-harnesses.yaml
├── LICENSE                          # Apache 2.0
├── README.md
├── SPEC.md
├── CONTRIBUTING.md
└── docs/
    ├── harness-dev.md
    └── error-handling.md
```

## Key Dependencies

```go
// go.mod
module github.com/kajogo777/bento

go 1.23

require (
    // OCI registry operations (core dependency)
    oras.land/oras-go/v2 v2.6.0

    // OCI image spec types
    github.com/opencontainers/image-spec v1.1.1

    // CLI framework
    github.com/spf13/cobra v1.8.0

    // YAML config parsing
    gopkg.in/yaml.v3 v3.0.1

    // File watching (for bento watch)
    github.com/fsnotify/fsnotify v1.7.0

    // Glob pattern matching
    github.com/gobwas/glob v0.2.3

    // Terminal output
    github.com/charmbracelet/lipgloss v0.9.1
    github.com/charmbracelet/bubbles v0.18.0

    // Docker credential helpers (for registry auth)
    github.com/docker/cli v25.0.0

    // MCP server (stdio JSON-RPC)
    github.com/sourcegraph/jsonrpc2 v0.2.0

    // Secret scanning
    github.com/trufflesecurity/trufflehog/v3 v3.0.0  // or embed patterns directly

    // Testing
    github.com/stretchr/testify v1.9.0
)
```

## Credential Provider Interface

The credential provider system resolves secret references during restore. Each provider knows how to talk to one secret backend.

### Interface

```go
// Provider resolves a secret value from an external source.
type Provider interface {
    // Name returns the provider identifier (matches "source" field in secrets manifest).
    Name() string

    // Resolve fetches the secret value. Returns the plaintext value or an error.
    // The SecretRef contains all fields from the secrets manifest entry.
    Resolve(ctx context.Context, ref SecretRef) (string, error)

    // Available returns true if this provider can be used on the current system.
    // For example, the vault provider checks if VAULT_ADDR is set.
    Available() bool
}

// SecretRef represents a single entry in the secrets manifest.
type SecretRef struct {
    Source string                 // provider name: "vault", "env", "1password", etc.
    Fields map[string]string     // all key-value fields from the manifest entry
}
```

### Built-in Providers

#### env -- Environment Variables

The simplest provider. Reads a secret from a local environment variable.

```yaml
secrets:
  GITHUB_TOKEN:
    source: env
    var: GITHUB_TOKEN
```

```go
func (p *EnvProvider) Resolve(ctx context.Context, ref SecretRef) (string, error) {
    varName := ref.Fields["var"]
    value := os.Getenv(varName)
    if value == "" {
        return "", fmt.Errorf("environment variable %s is not set", varName)
    }
    return value, nil
}
```

#### file -- Local File

Reads a secret from a file on disk. Useful for mounted secrets in Kubernetes.

```yaml
secrets:
  TLS_CERT:
    source: file
    path: /run/secrets/tls-cert
```

```go
func (p *FileProvider) Resolve(ctx context.Context, ref SecretRef) (string, error) {
    data, err := os.ReadFile(ref.Fields["path"])
    if err != nil {
        return "", fmt.Errorf("reading secret file %s: %w", ref.Fields["path"], err)
    }
    return strings.TrimSpace(string(data)), nil
}
```

#### vault -- HashiCorp Vault

```yaml
secrets:
  DATABASE_URL:
    source: vault
    path: secret/data/myapp/db
    key: url
```

```go
func (p *VaultProvider) Available() bool {
    return os.Getenv("VAULT_ADDR") != ""
}

func (p *VaultProvider) Resolve(ctx context.Context, ref SecretRef) (string, error) {
    client, err := vault.NewClient(vault.DefaultConfig())
    if err != nil {
        return "", err
    }
    secret, err := client.KVv2("secret").Get(ctx, ref.Fields["path"])
    if err != nil {
        return "", err
    }
    value, ok := secret.Data[ref.Fields["key"]].(string)
    if !ok {
        return "", fmt.Errorf("key %s not found at path %s", ref.Fields["key"], ref.Fields["path"])
    }
    return value, nil
}
```

#### onepassword -- 1Password CLI

```yaml
secrets:
  API_KEY:
    source: 1password
    vault: Engineering
    item: api-credentials
    field: api_key
```

```go
func (p *OnePasswordProvider) Available() bool {
    _, err := exec.LookPath("op")
    return err == nil
}

func (p *OnePasswordProvider) Resolve(ctx context.Context, ref SecretRef) (string, error) {
    args := []string{"item", "get", ref.Fields["item"],
        "--vault", ref.Fields["vault"],
        "--fields", ref.Fields["field"],
        "--format", "json",
    }
    out, err := exec.CommandContext(ctx, "op", args...).Output()
    if err != nil {
        return "", fmt.Errorf("1password: %w", err)
    }
    var result struct{ Value string `json:"value"` }
    json.Unmarshal(out, &result)
    return result.Value, nil
}
```

#### awssts -- AWS STS Assume Role

Returns temporary credentials for an IAM role.

```yaml
secrets:
  AWS_ACCESS_KEY_ID:
    source: aws-sts
    role: arn:aws:iam::123456789:role/agent-role
    field: access_key_id
```

#### gcloud -- Google Cloud Secret Manager

```yaml
secrets:
  DB_PASSWORD:
    source: gcloud
    project: my-project
    secret: db-password
    version: latest
```

#### azure -- Azure Key Vault

```yaml
secrets:
  STORAGE_KEY:
    source: azure
    vault: my-vault
    secret: storage-key
```

#### exec -- Arbitrary Command

Escape hatch for custom secret backends. Runs a command and uses stdout as the secret value.

```yaml
secrets:
  CUSTOM_SECRET:
    source: exec
    command: "./scripts/get-secret.sh my-secret"
```

```go
func (p *ExecProvider) Resolve(ctx context.Context, ref SecretRef) (string, error) {
    out, err := exec.CommandContext(ctx, "sh", "-c", ref.Fields["command"]).Output()
    if err != nil {
        return "", fmt.Errorf("exec provider: %w", err)
    }
    return strings.TrimSpace(string(out)), nil
}
```

### Provider Registry

Providers register themselves at init time:

```go
var providers = map[string]Provider{
    "env":        &EnvProvider{},
    "file":       &FileProvider{},
    "vault":      &VaultProvider{},
    "1password":  &OnePasswordProvider{},
    "aws-sts":    &AWSStsProvider{},
    "gcloud":     &GCloudProvider{},
    "azure":      &AzureProvider{},
    "exec":       &ExecProvider{},
}

func Resolve(ctx context.Context, ref SecretRef) (string, error) {
    provider, ok := providers[ref.Source]
    if !ok {
        return "", fmt.Errorf("unknown secret provider: %s", ref.Source)
    }
    if !provider.Available() {
        return "", fmt.Errorf("secret provider %s is not available (check prerequisites)", ref.Source)
    }
    return provider.Resolve(ctx, ref)
}
```

### Adding Custom Providers

Users can add providers via Go plugins or (more practically) use the `exec` provider as an escape hatch for any secret backend not built in.

Future: a plugin system where providers are standalone binaries discovered by name convention (`bento-provider-<name>`) in PATH, following the credential helper pattern used by Docker and git.

## Build and Release

### Makefile

```makefile
BINARY := bento
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build test lint release clean

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/bento

test:
	go test ./... -race -count=1

test-integration:
	go test ./... -race -count=1 -tags=integration

lint:
	golangci-lint run

release:
	goreleaser release --clean

clean:
	rm -rf bin/ dist/
```

### Dockerfile

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /bento ./cmd/bento

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git
COPY --from=builder /bento /usr/local/bin/bento
ENTRYPOINT ["bento"]
```

### GoReleaser Config

```yaml
# .goreleaser.yaml
version: 2
builds:
  - main: ./cmd/bento
    binary: bento
    env:
      - CGO_ENABLED=0
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{.Version}}

archives:
  - format: tar.gz
    name_template: "bento_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        format: zip

brews:
  - repository:
      owner: kajogo777
      name: homebrew-tap
    homepage: https://bento.dev
    description: "Portable agent workspaces. Pack, ship, resume."

scoops:
  - repository:
      owner: kajogo777
      name: scoop-bucket
    homepage: https://bento.dev
    description: "Portable agent workspaces. Pack, ship, resume."

winget:
  - publisher: kajogo777
    short_description: "Portable agent workspaces"
    package_identifier: kajogo777/bento

dockers:
  - image_templates:
      - "ghcr.io/kajogo777/bento:{{ .Version }}"
      - "ghcr.io/kajogo777/bento:latest"
    dockerfile: Dockerfile

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
```

### CI Workflows

#### ci.yaml -- runs on every PR

```yaml
name: CI
on:
  pull_request:
  push:
    branches: [main]

jobs:
  test:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
        go: ["1.23"]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}
      - run: go test ./... -race -count=1
      - run: go vet ./...

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - uses: golangci/golangci-lint-action@v4

  build:
    strategy:
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - run: go build ./cmd/bento
```

#### release.yaml -- runs on tag push

```yaml
name: Release
on:
  push:
    tags: ["v*"]

permissions:
  contents: write
  packages: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

#### test-harnesses.yaml -- integration tests with real agent workspaces

```yaml
name: Harness Integration Tests
on:
  pull_request:
    paths:
      - "internal/harness/**"
      - "docs/harness-dev.md"

jobs:
  test-harnesses:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - name: Setup test workspaces
        run: |
          # Create mock agent workspace structures for each harness
          mkdir -p test-workspaces/claude-code/.claude
          echo "# Test project" > test-workspaces/claude-code/CLAUDE.md
          touch test-workspaces/claude-code/.claude/settings.json

          mkdir -p test-workspaces/codex/.codex
          echo "# Test project" > test-workspaces/codex/AGENTS.md
          touch test-workspaces/codex/.codex/config.toml

          mkdir -p test-workspaces/aider
          touch test-workspaces/aider/.aider.conf.yml
          touch test-workspaces/aider/.aider.chat.history.md

      - name: Run harness tests
        run: go test ./internal/harness/... -tags=integration -v

      - name: Test save/restore cycle
        run: |
          go build -o bin/bento ./cmd/bento
          for ws in test-workspaces/*/; do
            echo "Testing workspace: $ws"
            bin/bento init --dir "$ws"
            bin/bento save --dir "$ws" -m "test checkpoint"
            bin/bento inspect --dir "$ws" latest
          done
```

## Implementation Order

Recommended order for building bento:

1. **Config parsing** (`internal/config/`) -- load `bento.yaml`, merge defaults
2. **Workspace scanner** (`internal/workspace/scanner.go`) -- glob matching, file walking
3. **Layer packing** (`internal/workspace/layer.go`) -- tar+gzip creation from matched files
4. **Local store** (`internal/registry/local.go`) -- OCI image layout read/write
5. **Manifest construction** (`internal/manifest/`) -- build OCI manifests with annotations
6. **Save command** (`internal/cli/save.go`) -- wire scanner + packer + store
7. **Open command** (`internal/cli/open.go`) -- pull manifest, extract layers
8. **Harness detection** (`internal/harness/detect.go`) -- try each harness, pick the first match
9. **Init command** (`internal/cli/init.go`) -- detect harness, generate `bento.yaml`
10. **List/inspect/tag** -- read-only operations on the store
11. **Harnesses** (in priority order):
    - Claude Code
    - OpenClaw
    - OpenCode
    - Cursor
    - Codex
    - GitHub Copilot
    - Windsurf
12. **Secret scanning** (`internal/secrets/scan.go`) -- regex-based pre-push check
13. **Secret hydration** (`internal/secrets/hydrate.go`) -- provider resolution
14. **Remote store** (`internal/registry/remote.go`) -- oras-go remote registry
15. **Push command** -- copy from local to remote
16. **Diff command** -- compare two checkpoints
17. **Fork command** -- save + restore with parent reference
18. **Hooks** (`internal/hooks/`) -- shell execution at lifecycle points
19. **Watch command** -- fsnotify + debounced save
20. **MCP server** (`internal/mcp/`) -- stdio JSON-RPC
21. **GC** (`internal/policy/`) -- retention-based cleanup
