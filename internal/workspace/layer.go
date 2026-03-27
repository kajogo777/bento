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
)

// PackLayer creates a tar.gz archive from the given files. The file paths must
// be relative to workDir. Files are stored in the archive with forward-slash
// paths relative to the workspace root.
func PackLayer(workDir string, files []string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
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
