package workspace

import (
	"os"
	"testing"
)

// packToBytes is a test-only helper that packs a layer and returns the
// compressed bytes directly, auto-removing the temp file.
//
// Production code should call PackLayerToTemp and work off the temp file
// path; materializing the full archive into memory is only convenient in
// tests where the archives are tiny.
func packToBytes(t *testing.T, workDir string, files []string, extFiles []ExternalFile) []byte {
	t.Helper()
	res, err := PackLayerToTemp(workDir, files, extFiles, PackOptions{})
	if err != nil {
		t.Fatalf("PackLayerToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(res.Path) })

	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("reading packed layer: %v", err)
	}
	return data
}
