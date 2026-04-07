package extension

import (
	"os"
	"path/filepath"
)

// AgentsMD is a cross-agent extension that ensures AGENTS.md always lands
// in the agent layer, regardless of which agent is active.
type AgentsMD struct{}

func (a AgentsMD) Name() string                                       { return "agents-md" }
func (a AgentsMD) NormalizePath(_ string) func(path string) string     { return nil }
func (a AgentsMD) ResolvePath(_ string) func(path string) string       { return nil }

func (a AgentsMD) Detect(workDir string) bool {
	_, err := os.Stat(filepath.Join(workDir, "AGENTS.md"))
	return err == nil
}

func (a AgentsMD) Contribute(_ string) Contribution {
	return Contribution{
		Layers: map[string][]string{
			"agent": {"AGENTS.md"},
		},
	}
}
