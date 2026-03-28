package workspace

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DisplayPath converts an archive entry name to a human-readable path.
// External files stored as "__external__/~/..." are shown as "~/.../..."
// and external absolute paths as "/...".
func DisplayPath(archiveName string) string {
	if strings.HasPrefix(archiveName, "__external__") {
		p := archiveName[len("__external__"):]
		// /~/ prefix → ~/
		if strings.HasPrefix(p, "/~/") {
			return "~/" + p[3:]
		}
		return p
	}
	return archiveName
}

// PackLayer creates a tar.gz archive from the given files. The file paths must
// be relative to workDir. Files are stored in the archive with forward-slash
// paths relative to the workspace root.
func PackLayer(workDir string, files []string) ([]byte, error) {
	return PackLayerWithExternal(workDir, files, nil, false)
}

// PackResult holds the result of PackLayerWithExternalToTemp.
type PackResult struct {
	Path       string // absolute path to temp .tar.gz file
	Size       int64  // compressed size in bytes
	GzipDigest string // "sha256:<hex>" of compressed bytes (OCI descriptor digest)
	DiffID     string // "sha256:<hex>" of uncompressed tar bytes (OCI config diff_id)
}

// PackLayerWithExternalToTemp creates a tar.gz archive combining workspace
// files and external files, writing the output to a temp file rather than
// loading the result into memory. It computes both the gzip digest (for OCI
// descriptor) and the uncompressed tar digest (for OCI config diff_id) in a
// single streaming pass.
func PackLayerWithExternalToTemp(workDir string, files []string, extFiles []ExternalFile, allowMissingExternal bool) (*PackResult, error) {
	tmpFile, err := os.CreateTemp("", "bento-layer-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	gzipHasher := sha256.New()
	mw := io.MultiWriter(tmpFile, gzipHasher)

	gw, err := gzip.NewWriterLevel(mw, gzip.DefaultCompression)
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("gzip writer: %w", err)
	}
	gw.OS = 0xFF

	uncompHasher := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(gw, uncompHasher))

	packErr := func() error {
		// Pack workspace files
		for _, file := range files {
			normalized := NormalizePath(file)
			absPath := filepath.Join(workDir, filepath.FromSlash(normalized))

			info, err := os.Stat(absPath)
			if err != nil {
				return fmt.Errorf("stat %s: %w", normalized, err)
			}

			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return fmt.Errorf("header %s: %w", normalized, err)
			}
			header.Name = normalized
			header.ModTime = time.Time{}
			header.AccessTime = time.Time{}
			header.ChangeTime = time.Time{}

			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("write header %s: %w", normalized, err)
			}

			f, err := os.Open(absPath)
			if err != nil {
				return fmt.Errorf("open %s: %w", normalized, err)
			}
			if _, err := io.Copy(tw, f); err != nil {
				_ = f.Close()
				return fmt.Errorf("copy %s: %w", normalized, err)
			}
			_ = f.Close()
		}

		// Pack external files (stream directly, no full-file reads into memory)
		for _, ef := range extFiles {
			info, err := os.Stat(ef.AbsPath)
			if err != nil || info.IsDir() {
				if allowMissingExternal {
					fmt.Printf("Warning: external file not found, skipping: %s\n", ef.AbsPath)
					continue
				}
				return fmt.Errorf("external file inaccessible: %s (use --allow-missing-external to skip)", ef.AbsPath)
			}

			hdr := &tar.Header{
				Name:    ef.ArchivePath,
				Size:    info.Size(),
				Mode:    int64(DefaultFileMode(ef.ArchivePath)),
				ModTime: time.Time{},
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return fmt.Errorf("writing header for %s: %w", ef.ArchivePath, err)
			}

			f, err := os.Open(ef.AbsPath)
			if err != nil {
				if allowMissingExternal {
					fmt.Printf("Warning: cannot read external file, skipping: %s\n", ef.AbsPath)
					continue
				}
				return fmt.Errorf("reading external file %s: %w", ef.AbsPath, err)
			}
			if _, err := io.Copy(tw, f); err != nil {
				_ = f.Close()
				return fmt.Errorf("copying external file %s: %w", ef.ArchivePath, err)
			}
			_ = f.Close()
		}

		if err := tw.Close(); err != nil {
			return err
		}
		return gw.Close()
	}()

	if packErr != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, packErr
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("closing temp file: %w", err)
	}

	info, err := os.Stat(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("stat temp file: %w", err)
	}

	return &PackResult{
		Path:       tmpPath,
		Size:       info.Size(),
		GzipDigest: "sha256:" + hex.EncodeToString(gzipHasher.Sum(nil)),
		DiffID:     "sha256:" + hex.EncodeToString(uncompHasher.Sum(nil)),
	}, nil
}

// PackLayerWithExternal creates a tar.gz archive combining workspace files and
// external files. Workspace files are relative to workDir; external files use
// their ArchivePath. If allowMissingExternal is false, inaccessible external
// files are returned as errors instead of silently skipped.
func PackLayerWithExternal(workDir string, files []string, extFiles []ExternalFile, allowMissingExternal bool) ([]byte, error) {
	packed, err := PackLayerWithExternalToTemp(workDir, files, extFiles, allowMissingExternal)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.Remove(packed.Path) }()

	data, err := os.ReadFile(packed.Path)
	if err != nil {
		return nil, fmt.Errorf("reading temp file: %w", err)
	}
	return data, nil
}

// LineHashSet maps the SHA256 hash of each line to the count of occurrences.
type LineHashSet map[[32]byte]int

// HashLinesFromReader streams r line by line, hashes each line with
// sha256.Sum256, and returns a LineHashSet with occurrence counts.
func HashLinesFromReader(r io.Reader) (LineHashSet, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	set := make(LineHashSet)
	for scanner.Scan() {
		h := sha256.Sum256(scanner.Bytes())
		set[h]++
	}
	return set, scanner.Err()
}

// HashLinesFromFile opens path and calls HashLinesFromReader.
func HashLinesFromFile(path string) (LineHashSet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return HashLinesFromReader(f)
}

// ExtractLineHashesFromLayer does a single streaming pass through a tar.gz
// archive, returning LineHashSets for all files whose display-path key is in
// want. Entries larger than maxLineBytes are skipped.
func ExtractLineHashesFromLayer(r io.Reader, want map[string]bool, maxLineBytes int64) (map[string]LineHashSet, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	result := make(map[string]LineHashSet, len(want))
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		key := DisplayPath(NormalizePath(header.Name))
		if !want[key] {
			_, _ = io.Copy(io.Discard, tr)
			continue
		}
		if header.Size > maxLineBytes {
			_, _ = io.Copy(io.Discard, tr)
			continue
		}
		set, err := HashLinesFromReader(tr)
		if err != nil {
			return nil, fmt.Errorf("hashing lines in %s: %w", key, err)
		}
		result[key] = set
	}
	return result, nil
}

// ExtractFileContentFromLayer streams a tar.gz archive from r and returns the
// content of the file matching filePath (compared via DisplayPath). Files larger
// than maxBytes are skipped. Returns os.ErrNotExist if the file is not found.
func ExtractFileContentFromLayer(r io.Reader, filePath string, maxBytes int64) ([]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		key := DisplayPath(NormalizePath(header.Name))
		if key != filePath {
			_, _ = io.Copy(io.Discard, tr)
			continue
		}
		if header.Size > maxBytes {
			return nil, fmt.Errorf("file %s exceeds size limit (%d bytes)", filePath, maxBytes)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filePath, err)
		}
		return data, nil
	}
	return nil, os.ErrNotExist
}

// ListLayerFilesWithHashesFromReader returns file paths and their content SHA256
// hashes by reading from r. This detects modifications even when file size is
// unchanged. r must contain a valid gzip-compressed tar archive.
func ListLayerFilesWithHashesFromReader(r io.Reader) (map[string]string, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	files := make(map[string]string)
	tr := tar.NewReader(gr)
	h := sha256.New()
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar next: %w", err)
		}
		if header.Typeflag == tar.TypeReg {
			h.Reset()
			if _, err := io.Copy(h, tr); err != nil {
				return nil, fmt.Errorf("hashing %s: %w", header.Name, err)
			}
			files[DisplayPath(NormalizePath(header.Name))] = fmt.Sprintf("%x", h.Sum(nil))
		}
	}
	return files, nil
}

// ListLayerFilesWithHashes returns file paths and their content SHA256 hashes
// from a tar.gz archive in memory. For large layers prefer
// ListLayerFilesWithHashesFromReader with an os.File.
func ListLayerFilesWithHashes(data []byte) (map[string]string, error) {
	return ListLayerFilesWithHashesFromReader(bytes.NewReader(data))
}

// ListLayerFilesFromReader returns the list of file paths in a tar.gz archive
// read from r.
func ListLayerFilesFromReader(r io.Reader) ([]string, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

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
			files = append(files, DisplayPath(NormalizePath(header.Name)))
		}
	}
	return files, nil
}

// ListLayerFiles returns the list of file paths contained in a tar.gz archive.
// For large layers prefer ListLayerFilesFromReader with an os.File.
func ListLayerFiles(data []byte) ([]string, error) {
	return ListLayerFilesFromReader(bytes.NewReader(data))
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
		_ = os.Remove(path)
	}

	// Remove empty directories (walk bottom-up by sorting longest paths first)
	// Do multiple passes until no more empty dirs are found
	for {
		removed := false
		_ = filepath.WalkDir(targetDir, func(path string, d os.DirEntry, err error) error {
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
				_ = os.Remove(path)
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

// UnpackLayerWithExternalFromReader extracts a tar.gz archive read from r to
// targetDir. Files with the "__external__" sentinel are routed to their
// original absolute locations; the target path is encoded in the entry name
// itself (see portablePath / absFromArchivePath in scanner.go).
// All file content is streamed directly to disk without loading full files
// into memory.
func UnpackLayerWithExternalFromReader(r io.Reader, targetDir string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

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

		// Route __external__ files back to their original absolute locations.
		if strings.HasPrefix(name, "__external__") {
			targetPath := absFromArchivePath(name)
			if targetPath == "" || strings.Contains(targetPath, "..") {
				_, _ = io.Copy(io.Discard, tr)
				continue
			}
			switch header.Typeflag {
			case tar.TypeDir:
				_ = os.MkdirAll(targetPath, 0o755)
			case tar.TypeReg:
				_ = os.MkdirAll(filepath.Dir(targetPath), 0o755)
				mode := os.FileMode(header.Mode)
				if mode == 0 {
					mode = DefaultFileMode(name)
				}
				f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
				if err != nil {
					return fmt.Errorf("create %s: %w", targetPath, err)
				}
				if _, err := io.Copy(f, tr); err != nil {
					_ = f.Close()
					return fmt.Errorf("write %s: %w", targetPath, err)
				}
				_ = f.Close()
			}
			continue
		}

		// Regular workspace files
		if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
			return fmt.Errorf("rejecting absolute path in archive: %s", name)
		}
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
				_ = f.Close()
				return fmt.Errorf("write %s: %w", name, err)
			}
			_ = f.Close()
		}
	}
	return nil
}

// UnpackLayerWithExternal extracts a tar.gz archive to targetDir.
// For large layers prefer UnpackLayerWithExternalFromReader with an os.File.
func UnpackLayerWithExternal(data []byte, targetDir string) error {
	return UnpackLayerWithExternalFromReader(bytes.NewReader(data), targetDir)
}

// UnpackLayer extracts a tar.gz archive to targetDir.
// External-prefixed files are skipped (use UnpackLayerWithExternal for those).
func UnpackLayer(data []byte, targetDir string) error {
	return UnpackLayerWithExternal(data, targetDir)
}

// HashFileStreaming computes the SHA256 of a file by streaming it, without
// loading the entire file into memory.
func HashFileStreaming(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
