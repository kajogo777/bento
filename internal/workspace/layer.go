package workspace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PackLayer creates a tar.gz archive from the given files. The file paths must
// be relative to workDir. Files are stored in the archive with forward-slash
// paths relative to the workspace root.
func PackLayer(workDir string, files []string) ([]byte, error) {
	var buf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	gw.OS = 0xFF // unknown OS - avoid platform-dependent header
	tw := tar.NewWriter(gw)

	for _, file := range files {
		normalized := NormalizePath(file)
		absPath := filepath.Join(workDir, filepath.FromSlash(normalized))

		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", normalized, err)
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil, fmt.Errorf("header %s: %w", normalized, err)
		}
		header.Name = normalized
		// Zero out timestamps so identical file content produces identical
		// archives regardless of when the file was last modified. This
		// ensures round-trip idempotency: save → restore → save yields
		// the same layer digests.
		header.ModTime = time.Time{}
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}

		if err := tw.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write header %s: %w", normalized, err)
		}

		f, err := os.Open(absPath)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", normalized, err)
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return nil, fmt.Errorf("copy %s: %w", normalized, err)
		}
		f.Close()
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ListLayerFilesWithHashes returns file paths and their content sizes from a tar.gz archive.
// The returned map keys are normalized file paths, values are file sizes (used as a cheap
// change indicator since identical files have identical sizes in the deterministic tar).
func ListLayerFilesWithSizes(data []byte) (map[string]int64, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	files := make(map[string]int64)
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		if header.Typeflag == tar.TypeReg {
			files[NormalizePath(header.Name)] = header.Size
		}
	}
	return files, nil
}

// ListLayerFiles returns the list of file paths contained in a tar.gz archive.
func ListLayerFiles(data []byte) ([]string, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	var files []string
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		if header.Typeflag == tar.TypeReg {
			files = append(files, NormalizePath(header.Name))
		}
	}
	return files, nil
}

// CleanStaleFiles removes files in targetDir that are not in the keepFiles set.
// It preserves .git/, bento.yaml, and .bentoignore. Empty directories are
// removed after file cleanup.
func CleanStaleFiles(targetDir string, keepFiles map[string]bool) error {
	// Collect files to remove
	var toRemove []string
	err := filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}

		rel, err := filepath.Rel(targetDir, path)
		if err != nil {
			return nil
		}
		normalized := NormalizePath(rel)

		// Skip root
		if normalized == "." {
			return nil
		}

		// Always preserve .git directory
		if normalized == ".git" || strings.HasPrefix(normalized, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Preserve bento config files
		if normalized == "bento.yaml" || normalized == ".bentoignore" {
			return nil
		}

		// Skip directories on this pass (clean empty ones later)
		if d.IsDir() {
			return nil
		}

		if !keepFiles[normalized] {
			toRemove = append(toRemove, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Remove stale files
	for _, path := range toRemove {
		os.Remove(path)
	}

	// Remove empty directories (walk bottom-up by sorting longest paths first)
	// Do multiple passes until no more empty dirs are found
	for {
		removed := false
		filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(targetDir, path)
			normalized := NormalizePath(rel)
			if normalized == "." || normalized == ".git" || strings.HasPrefix(normalized, ".git/") {
				return nil
			}
			entries, _ := os.ReadDir(path)
			if len(entries) == 0 {
				os.Remove(path)
				removed = true
			}
			return nil
		})
		if !removed {
			break
		}
	}

	return nil
}

// UnpackLayer extracts a tar.gz archive to targetDir. It handles cross-platform
// path conversion and rejects absolute paths or paths containing ".." for safety.
func UnpackLayer(data []byte, targetDir string) error {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		name := NormalizePath(header.Name)

		// Reject absolute paths.
		if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
			return fmt.Errorf("rejecting absolute path in archive: %s", name)
		}

		// Reject paths with "..".
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return fmt.Errorf("rejecting path with .. in archive: %s", name)
			}
		}

		target := filepath.Join(targetDir, filepath.FromSlash(name))

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, DefaultFileMode(name)); err != nil {
				return fmt.Errorf("mkdir %s: %w", name, err)
			}
		case tar.TypeReg:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", name, err)
			}
			mode := DefaultFileMode(name)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("create %s: %w", name, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write %s: %w", name, err)
			}
			f.Close()
		}
	}
	return nil
}
