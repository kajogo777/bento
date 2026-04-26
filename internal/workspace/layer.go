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
	"runtime"
	"strings"
	"time"

	pgzip "github.com/klauspost/pgzip"
)

// PackOptions configures a pack operation.
type PackOptions struct {
	// AllowMissingExternal warns instead of failing when an external file is
	// inaccessible.
	AllowMissingExternal bool

	// FileOverrides maps normalized workspace-relative paths to temp files
	// whose content should be packed instead of the real file on disk.
	// Used by secret scrubbing to pack placeholder-substituted content.
	FileOverrides map[string]string

	// Progress is invoked periodically during packing with the cumulative
	// number of uncompressed bytes processed. Called from the packing
	// goroutine; implementations should be fast and non-blocking (atomic
	// stores are typical).
	Progress func(uncompressedBytes int64)
}

// progressReportBytes is the minimum number of uncompressed bytes between
// successive Progress callback invocations. Small enough to feel responsive
// on a single large file, large enough not to dominate CPU.
const progressReportBytes = 4 << 20 // 4 MiB

// progressWriter wraps an io.Writer and invokes onProgress with the cumulative
// byte count, throttled to at most one call per progressReportBytes written.
type progressWriter struct {
	w          io.Writer
	total      int64
	sinceLast  int64
	onProgress func(int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if n > 0 {
		p.total += int64(n)
		p.sinceLast += int64(n)
		if p.sinceLast >= progressReportBytes && p.onProgress != nil {
			p.onProgress(p.total)
			p.sinceLast = 0
		}
	}
	return n, err
}

// newBoundedPgzipWriter wraps pgzip.NewWriterLevel with a concurrency cap.
//
// pgzip's default concurrency is (1 MiB block size) × (runtime.GOMAXPROCS)
// in-flight blocks per writer. On high-core machines this compounds badly
// with bento's outer errgroup (one pack goroutine per layer, also up to
// NumCPU), producing a peak working set of roughly numLayers × NumCPU MiB
// purely for compression buffers — on a 10-core laptop that's already
// ~100 MiB before the actual tar stream.
//
// We cap the internal block count to min(NumCPU, 4). Empirically gzip
// parallelism saturates around 4 concurrent blocks per stream; going
// higher trades memory for vanishingly small throughput gains. This keeps
// steady-state per-layer memory at ~4 MiB of block buffers, independent
// of the host's core count.
func newBoundedPgzipWriter(w io.Writer) (*pgzip.Writer, error) {
	gw, err := pgzip.NewWriterLevel(w, pgzip.DefaultCompression)
	if err != nil {
		return nil, err
	}
	blocks := runtime.NumCPU()
	if blocks > 4 {
		blocks = 4
	}
	if blocks < 1 {
		blocks = 1
	}
	// 1 MiB block size is pgzip's default and the sweet spot for gzip
	// parallelism — smaller blocks hurt compression ratio, larger blocks
	// increase per-block buffer memory without speed gains.
	if err := gw.SetConcurrency(1<<20, blocks); err != nil {
		_ = gw.Close()
		return nil, fmt.Errorf("gzip concurrency: %w", err)
	}
	gw.OS = 0xFF
	return gw, nil
}

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

// PackLayer creates a tar.gz archive from the given files and returns the
// result as a temp file. The file paths must be relative to workDir. Files
// are stored in the archive with forward-slash paths relative to the
// workspace root. This is a convenience wrapper around PackLayerToTemp for
// the common "workspace files only, no options" case.
func PackLayer(workDir string, files []string) (*PackResult, error) {
	return PackLayerToTemp(workDir, files, nil, PackOptions{})
}

// PackResult holds the result of PackLayerToTemp.
type PackResult struct {
	Path       string // absolute path to temp .tar.gz file
	Size       int64  // compressed size in bytes
	GzipDigest string // "sha256:<hex>" of compressed bytes (OCI descriptor digest)
	DiffID     string // "sha256:<hex>" of uncompressed tar bytes (OCI config diff_id)
}

// PackLayerToTemp creates a tar.gz archive combining workspace files and
// external files, writing the output to a temp file rather than loading the
// result into memory. It computes both the gzip digest (for OCI descriptor)
// and the uncompressed tar digest (for OCI config diff_id) in a single
// streaming pass.
//
// Compression uses klauspost/pgzip (parallel gzip) at default compression.
// The output is a standard gzip stream compatible with any gzip reader; the
// parallelization only affects the writer side. For a 3.6 GB highly
// compressible input this delivers ~28x speedup over compress/gzip.
//
// See PackOptions for fields.
func PackLayerToTemp(workDir string, files []string, extFiles []ExternalFile, opts PackOptions) (*PackResult, error) {
	overrides := opts.FileOverrides
	allowMissingExternal := opts.AllowMissingExternal

	tmpFile, err := os.CreateTemp("", "bento-layer-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	gzipHasher := sha256.New()
	mw := io.MultiWriter(tmpFile, gzipHasher)

	gw, err := newBoundedPgzipWriter(mw)
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("gzip writer: %w", err)
	}

	uncompHasher := sha256.New()

	// Build the tar destination: gzip + diff-id hasher, optionally wrapped
	// in a progressWriter that reports uncompressed byte totals. Reporting on
	// the uncompressed side is the natural measure of "work done" for a
	// layer and is independent of the achieved compression ratio.
	var tarDst io.Writer = io.MultiWriter(gw, uncompHasher)
	if opts.Progress != nil {
		tarDst = &progressWriter{w: tarDst, onProgress: opts.Progress}
	}
	tw := tar.NewWriter(tarDst)

	packErr := func() error {
		// Pack workspace files
		for _, file := range files {
			normalized := NormalizePath(file)
			absPath := filepath.Join(workDir, filepath.FromSlash(normalized))

			// Use Lstat to detect symlinks without following them.
			linfo, err := os.Lstat(absPath)
			if err != nil {
				return fmt.Errorf("stat %s: %w", normalized, err)
			}

			// Handle symlinks: store as tar symlink entries.
			if linfo.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(absPath)
				if err != nil {
					return fmt.Errorf("readlink %s: %w", normalized, err)
				}
				header := &tar.Header{
					Typeflag: tar.TypeSymlink,
					Name:     normalized,
					Linkname: linkTarget,
					ModTime:  time.Time{},
				}
				if err := tw.WriteHeader(header); err != nil {
					return fmt.Errorf("write symlink header %s: %w", normalized, err)
				}
				continue
			}

			info := linfo
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return fmt.Errorf("header %s: %w", normalized, err)
			}
			header.Name = normalized
			header.ModTime = time.Time{}
			header.AccessTime = time.Time{}
			header.ChangeTime = time.Time{}

			// Skip directories (they're created implicitly by file entries).
			if info.IsDir() {
				if err := tw.WriteHeader(header); err != nil {
					return fmt.Errorf("write header %s: %w", normalized, err)
				}
				continue
			}

			// Determine the actual file to read content from.
			// If there's a scrub override, use the override path and
			// update the header size to match.
			readPath := absPath
			if overrides != nil {
				if override, ok := overrides[normalized]; ok {
					readPath = override
					if oInfo, oErr := os.Stat(override); oErr == nil {
						header.Size = oInfo.Size()
					}
				}
			}

			if err := tw.WriteHeader(header); err != nil {
				return fmt.Errorf("write header %s: %w", normalized, err)
			}

			f, err := os.Open(readPath)
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
			// Use Lstat to detect symlinks.
			linfo, err := os.Lstat(ef.AbsPath)
			if err != nil {
				if allowMissingExternal {
					fmt.Printf("Warning: external file not found, skipping: %s\n", ef.AbsPath)
					continue
				}
				return fmt.Errorf("external file inaccessible: %s (use --allow-missing-external to skip)", ef.AbsPath)
			}

			// Handle symlinks.
			if linfo.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(ef.AbsPath)
				if err != nil {
					if allowMissingExternal {
						continue
					}
					return fmt.Errorf("readlink external %s: %w", ef.AbsPath, err)
				}
				header := &tar.Header{
					Typeflag: tar.TypeSymlink,
					Name:     ef.ArchivePath,
					Linkname: linkTarget,
					ModTime:  time.Time{},
				}
				if err := tw.WriteHeader(header); err != nil {
					return fmt.Errorf("writing symlink header for %s: %w", ef.ArchivePath, err)
				}
				continue
			}

			if linfo.IsDir() {
				if allowMissingExternal {
					fmt.Printf("Warning: external path is a directory, skipping: %s\n", ef.AbsPath)
					continue
				}
				return fmt.Errorf("external file is a directory: %s", ef.AbsPath)
			}

			hdr := &tar.Header{
				Name:    ef.ArchivePath,
				Size:    linfo.Size(),
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
//
// The optional resolvePath function expands portable placeholders back to
// machine-specific paths. Pass nil to skip placeholder expansion.
func UnpackLayerWithExternalFromReader(r io.Reader, targetDir string, resolvePath func(string) string) error {
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
			// Expand portable placeholders to machine-specific paths.
			if resolvePath != nil {
				stripped := strings.TrimPrefix(name, "__external__")
				resolved := resolvePath(stripped)
				name = "__external__" + resolved
			}
			targetPath := absFromArchivePath(name)
			if targetPath == "" || strings.Contains(targetPath, "..") {
				_, _ = io.Copy(io.Discard, tr)
				continue
			}
			switch header.Typeflag {
			case tar.TypeDir:
				_ = os.MkdirAll(targetPath, 0o755)
			case tar.TypeSymlink:
				_ = os.MkdirAll(filepath.Dir(targetPath), 0o755)
				_ = os.Remove(targetPath) // remove existing before creating symlink
				_ = os.Symlink(header.Linkname, targetPath)
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
		case tar.TypeSymlink:
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", name, err)
			}
			_ = os.Remove(target) // remove existing before creating symlink
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("symlink %s: %w", name, err)
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
func UnpackLayerWithExternal(data []byte, targetDir string, resolvePath func(string) string) error {
	return UnpackLayerWithExternalFromReader(bytes.NewReader(data), targetDir, resolvePath)
}

// UnpackLayer extracts a tar.gz archive to targetDir.
// External-prefixed files are skipped (use UnpackLayerWithExternal for those).
func UnpackLayer(data []byte, targetDir string) error {
	return UnpackLayerWithExternal(data, targetDir, nil)
}

// HashBytes computes the SHA256 of data and returns the hex digest.
// Use this for in-memory content (e.g., file content already loaded).
func HashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
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

// PackBytesToTempLayer creates a tar.gz archive containing a single file with
// the given name and content. Returns a PackResult with the temp file path and
// digests. Used by the OCI secrets backend to pack encrypted secrets as a layer.
func PackBytesToTempLayer(fileName string, content []byte) (*PackResult, error) {
	tmpFile, err := os.CreateTemp("", "bento-enc-layer-*.tar.gz")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	gzipHasher := sha256.New()
	mw := io.MultiWriter(tmpFile, gzipHasher)

	gw, err := newBoundedPgzipWriter(mw)
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("gzip writer: %w", err)
	}

	uncompHasher := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(gw, uncompHasher))

	header := &tar.Header{
		Name:    fileName,
		Size:    int64(len(content)),
		Mode:    0600,
		ModTime: time.Time{},
	}
	if err := tw.WriteHeader(header); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("write header: %w", err)
	}
	if _, err := tw.Write(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("write content: %w", err)
	}

	if err := tw.Close(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close gzip: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("close temp file: %w", err)
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
