package workspace

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`src\main.go`, "src/main.go"},
		{`a\b\c\d.txt`, "a/b/c/d.txt"},
		{"already/forward", "already/forward"},
		{"noslash", "noslash"},
		{"", ""},
	}

	for _, tt := range tests {
		got := NormalizePath(tt.input)
		if got != tt.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNativePathSep(t *testing.T) {
	input := "src/main.go"
	got := NativePathSep(input)
	want := filepath.FromSlash(input)
	if got != want {
		t.Errorf("NativePathSep(%q) = %q, want %q", input, got, want)
	}

	if runtime.GOOS == "windows" {
		if got != `src\main.go` {
			t.Errorf("on Windows, NativePathSep(%q) should use backslashes, got %q", input, got)
		}
	} else {
		if got != "src/main.go" {
			t.Errorf("on Unix, NativePathSep(%q) should keep forward slashes, got %q", input, got)
		}
	}
}

func TestIsExecutable(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"script.sh", true},
		{"script.bash", true},
		{"script.py", true},
		{"script.rb", true},
		{"script.pl", true},
		{"main.go", false},
		{"app.js", false},
		{"README.md", false},
		{"Makefile", false},
		{"run.SH", true}, // case insensitive via ToLower
	}

	for _, tt := range tests {
		got := IsExecutable(tt.name)
		if got != tt.want {
			t.Errorf("IsExecutable(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDefaultFileMode(t *testing.T) {
	tests := []struct {
		name string
		want uint32
	}{
		{"main.go", 0644},
		{"README.md", 0644},
		{"script.sh", 0755},
		{"deploy.py", 0755},
		{"somedir/", 0755},
	}

	for _, tt := range tests {
		got := DefaultFileMode(tt.name)
		if uint32(got) != tt.want {
			t.Errorf("DefaultFileMode(%q) = %04o, want %04o", tt.name, got, tt.want)
		}
	}
}
