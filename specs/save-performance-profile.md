# Save Performance Profile

**Date:** 2026-04-05
**Workspace:** Bento project (146 source files, 573 external agent files, 30MB session data)
**Platform:** darwin/arm64 (Apple M3 Max, 16 cores)
**Binary:** compiled from main @ d94cbb3

## Profile Results

Measured with an instrumented save (secret scanning enabled, no skip flags):

| Phase | Time | % of total | Details |
|-------|------|-----------|---------|
| config.Load | 0.3ms | <1% | Parse bento.yaml |
| ResolveAndMerge | 0.5ms | <1% | Detect extensions, merge patterns |
| workspace.Scan | 27ms | 2% | Walk workspace + external dirs, assign files to layers |
| secret.Scan | 95ms | 8% | Gitleaks scan of 204 workspace files (39 hits). Uses file-hash cache — cold scan is ~5× slower |
| **ParseSessions** | **152ms** | **12%** | Parse 10 Claude Code session JSONL files (30MB, 11K lines). Full JSON parse per line |
| **PackLayers** | **965ms** | **78%** | tar+gzip 777 files across 3 layers (deps: 0, agent: 630, project: 146). Parallel with NumCPU goroutines |
| MarshalConfig | 0.1ms | <1% | JSON-encode BentoConfigObj |
| **TOTAL** | **1.24s** | **100%** | |

## Bottleneck Analysis

### PackLayers (78%)

The dominant cost. 630 agent files (external, under `~/.claude/`) account for most of it — these include session JSONL files, memory, rules, skills, and global config. The packing is already parallelized across NumCPU.

**Potential optimizations:**
- Layer-level change detection before packing (skip unchanged layers entirely — already implemented via digest comparison, but packing still happens to compute the digest)
- Incremental layer packing (content-addressed file index, only repack changed files)
- Exclude session JSONL from the agent layer tar when session metadata is embedded in config (trade-off: lose raw session data in checkpoint)

### ParseSessions (12%)

Parses every line of every session JSONL file with `json.Unmarshal` to count user/assistant messages. Naive string matching (`bytes.Contains`) was tried but produces incorrect counts because `"type":"user"` matches inside nested content (tool results containing conversation text).

**Potential optimizations:**
- **Append-only cache** (prototyped, not merged): session JSONL files are append-only, so `{path, fileSize} → metadata` is a valid cache key. On repeat saves, only parse bytes appended since last scan. First save: ~150ms, subsequent saves: <5ms. Especially impactful for `bento watch` which saves repeatedly.
- **Partial JSON decode**: parse only the `"type"` field at the start of each line (custom scanner that reads until the first field, avoids allocating the full `ccRecord`). Would reduce per-line cost ~3-5×.
- **Pre-computed count in metadata**: if Claude Code ever adds a message count to its session metadata files, we could read that instead of counting.

### secret.Scan (8%)

Already cached by file content hash. Cold scan (no cache) is ~500ms. The cache makes repeat scans fast.

## Session Data Profile

| Metric | Value |
|--------|-------|
| Session files | 10 |
| Total lines | 11,427 |
| Total size | 30MB |
| Largest session | 3,623 messages, ~15MB |
| Average session | ~640 messages |

Session data grows monotonically (append-only JSONL). For long-lived workspaces, this will become the dominant I/O cost of save — a workspace with 100 sessions at 100MB+ would push ParseSessions past PackLayers.

## Recommendations

1. **Implement append-only session cache** — highest ROI for `bento watch` workflows. Cache invalidation is simple (file size as key). Estimated effort: small.
2. **Investigate layer-level skip** — if a layer's file set hasn't changed (same file list + mtimes), skip packing entirely. Currently bento packs to compute the digest, then compares. A pre-pack fingerprint could avoid this.
3. **Profile PackLayers internals** — is the bottleneck in tar creation, gzip compression, or file I/O? If gzip, consider zstd (faster, better ratio). If I/O, consider io_uring or mmap.

## Reproducing

To reproduce this profile, create `cmd/profile/main.go` with instrumented timing around each phase of `ExecuteSave` and run `go run ./cmd/profile` from the workspace root. The profile binary was not committed — it's a throwaway diagnostic tool.
