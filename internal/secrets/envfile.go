package secrets

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// PopulateEnvFile reads a template env file and replaces placeholder values for
// keys that exist in the values map. Each line of the form KEY=placeholder is
// rewritten to KEY=resolvedValue if KEY is present in values. The result is
// written to outputPath.
func PopulateEnvFile(templatePath, outputPath string, values map[string]string) error {
	f, err := os.Open(templatePath)
	if err != nil {
		return fmt.Errorf("opening template %s: %w", templatePath, err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			if val, ok := values[key]; ok {
				line = key + "=" + val
			}
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading template %s: %w", templatePath, err)
	}

	output := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(outputPath, []byte(output), 0644); err != nil {
		return fmt.Errorf("writing env file %s: %w", outputPath, err)
	}

	return nil
}
