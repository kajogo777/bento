package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPopulateEnvFile_ReplacesValues(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, ".env.template")
	out := filepath.Join(dir, ".env")

	templateContent := "DB_HOST=placeholder\nDB_PORT=5432\nAPI_KEY=changeme\n"
	if err := os.WriteFile(tmpl, []byte(templateContent), 0o644); err != nil {
		t.Fatal(err)
	}

	values := map[string]string{
		"DB_HOST": "localhost",
		"API_KEY": "real-key-123",
	}

	if err := PopulateEnvFile(tmpl, out, values); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "DB_HOST=localhost") {
		t.Errorf("expected DB_HOST=localhost, got:\n%s", content)
	}
	if !strings.Contains(content, "DB_PORT=5432") {
		t.Errorf("DB_PORT should be preserved as 5432, got:\n%s", content)
	}
	if !strings.Contains(content, "API_KEY=real-key-123") {
		t.Errorf("expected API_KEY=real-key-123, got:\n%s", content)
	}
}

func TestPopulateEnvFile_MissingValues(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, ".env.template")
	out := filepath.Join(dir, ".env")

	templateContent := "SECRET=placeholder\nOTHER=keep\n"
	if err := os.WriteFile(tmpl, []byte(templateContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// No values provided for SECRET; it should keep placeholder.
	values := map[string]string{}

	if err := PopulateEnvFile(tmpl, out, values); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "SECRET=placeholder") {
		t.Errorf("expected placeholder to be preserved, got:\n%s", content)
	}
}

func TestPopulateEnvFile_CommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, ".env.template")
	out := filepath.Join(dir, ".env")

	templateContent := "# This is a comment\n\nKEY=old\n"
	if err := os.WriteFile(tmpl, []byte(templateContent), 0o644); err != nil {
		t.Fatal(err)
	}

	values := map[string]string{"KEY": "new"}
	if err := PopulateEnvFile(tmpl, out, values); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.Contains(content, "# This is a comment") {
		t.Error("comment should be preserved")
	}
	if !strings.Contains(content, "KEY=new") {
		t.Errorf("expected KEY=new, got:\n%s", content)
	}
}

func TestPopulateEnvFile_MissingTemplate(t *testing.T) {
	dir := t.TempDir()
	err := PopulateEnvFile(filepath.Join(dir, "nonexistent"), filepath.Join(dir, "out"), nil)
	if err == nil {
		t.Fatal("expected error for missing template")
	}
}
