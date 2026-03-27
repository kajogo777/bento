package secrets

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

// PopulateEnvFile writes an env file to outputPath. If templatePath is non-empty,
// it reads the template and replaces placeholder values for keys in the values map.
// If templatePath is empty, it generates the file directly from the values map.
func PopulateEnvFile(templatePath, outputPath string, values map[string]string) error {
	var output string

	if templatePath != "" {
		// Template mode: read template and substitute values
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
		output = strings.Join(lines, "\n") + "\n"
	} else {
		// Direct mode: generate from values map
		// Use the secrets list from the env file config to determine which
		// values to include. If no filtering is needed, write all values.
		var lines []string
		for k, v := range values {
			lines = append(lines, k+"="+v)
		}
		sort.Strings(lines)
		output = strings.Join(lines, "\n") + "\n"
	}

	if err := os.WriteFile(outputPath, []byte(output), 0600); err != nil {
		return fmt.Errorf("writing env file %s: %w", outputPath, err)
	}

	return nil
}
