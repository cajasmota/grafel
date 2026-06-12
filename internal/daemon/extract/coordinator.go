package extract

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/engine"
	bazelextract "github.com/cajasmota/archigraph/internal/extractors/bazel"
	configextract "github.com/cajasmota/archigraph/internal/extractors/config"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"
)

// CoordinatorConfig governs subprocess fan-out. Defaults are tuned to
// stay under the 600MB Phase-F target on a typical laptop:
//
//	Concurrency = NumCPU / 2 capped at 4   → 4 × 150MB = 600MB worst-case
//	BatchSize   = 80 files                  → ~100MB peak per subprocess
//
// All fields zero-valued fall back to defaults.
type CoordinatorConfig struct {
	// BinaryPath is the absolute path of the archigraph binary the
	// daemon should fork-exec to. When empty the coordinator uses
	// os.Executable(), which on Linux/macOS resolves to the same binary
	// the daemon process is running from.
	BinaryPath string

	// Concurrency caps the number of concurrent extract subprocesses.
	// Zero means runtime.NumCPU()/2 capped at 4.
	Concurrency int

	// BatchSize is the number of files per subprocess. Zero means 80.
	BatchSize int

	// SkipPasses is forwarded to every subprocess via --skip-pass.
	SkipPasses []string

	// TmpDir is where batch files (one per subprocess) are written.
	// Zero means os.TempDir(). The coordinator cleans up after exit.
	TmpDir string

	// Stderr receives subprocess stderr (logs, progress). When nil the
	// coordinator discards it.
	Stderr io.Writer
}

func (c CoordinatorConfig) concurrency() int {
	if c.Concurrency > 0 {
		return c.Concurrency
	}
	// Emergency runtime override (no redeploy needed once this build ships):
	// ARCHIGRAPH_EXTRACT_CONCURRENCY caps the number of concurrent extract
	// subprocesses. Used to throttle the daemon on shared/contended machines
	// where a high-churn repo (merge-every-few-minutes) would otherwise drive
	// continuous full-reindex fan-out (#3648).
	if n := envPositiveInt("ARCHIGRAPH_EXTRACT_CONCURRENCY"); n > 0 {
		return n
	}
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	if n > 4 {
		n = 4
	}
	return n
}

// extractGOMAXPROCS returns the per-subprocess GOMAXPROCS cap. Each extract
// subprocess is a full archigraph process; without this it inherits the
// daemon's GOMAXPROCS (= host core count) and the Go runtime spins one OS
// thread per core. With concurrency() subprocesses running in parallel the
// effective CPU draw becomes concurrency × hostCores, which is the observed
// runaway (#3648: ~7 of 12 cores during back-to-back reindexes).
//
// Bounding each child to a small GOMAXPROCS keeps total extract CPU at roughly
// concurrency × cap cores, leaving headroom for the consuming repo's CI.
//
//	ARCHIGRAPH_EXTRACT_GOMAXPROCS overrides the per-child value.
//	Default: 2 (so 4 children × 2 = ~8 worker threads worst-case, vs the
//	         previous unbounded 4 × hostCores).
func extractGOMAXPROCS() int {
	if n := envPositiveInt("ARCHIGRAPH_EXTRACT_GOMAXPROCS"); n > 0 {
		return n
	}
	return 2
}

// envPositiveInt reads a strictly-positive integer from the named env var.
// Returns 0 when unset, empty, non-numeric, or <= 0.
func envPositiveInt(name string) int {
	raw := os.Getenv(name)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func (c CoordinatorConfig) batchSize() int {
	if c.BatchSize > 0 {
		return c.BatchSize
	}
	return 80
}

// Result is the aggregated output of an extraction round across every
// subprocess. The daemon hands these slices to the buildDocument pass
// and downstream stages (resolution, classification, algorithms).
type Result struct {
	Entities      []types.EntityRecord
	Relationships []types.RelationshipRecord

	// Aggregated stats — sum across every subprocess's BatchStats.
	Files          int
	Processed      int
	Extracted      int
	Skipped        int
	Failed         int
	Pass1Rels      int
	Pass2Rels      int
	Pass25Rels     int
	Pass3Rels      int
	ByLang         map[string]int
	ByCrossExt     map[string]int
	PeakRSSBytes   uint64
	SubprocessRSS  []uint64
	Subprocesses   int
	NonFatalErrors []string

	// Pass1Plumbed counters (issue #2447): aggregated from every subprocess.
	// See BatchStats for full semantics, including heterogeneous-repo
	// expectations (issue #2464): FalseCount > 0 is normal on multi-language
	// repos. Use TrueCount / (TrueCount + FalseCount) as the health ratio.
	Pass1PlumbedTrueCount  int
	Pass1PlumbedFalseCount int
}

// Coordinate is the daemon-side entrypoint that replaces the in-process
// Pass 1/2.5/3 loop. It:
//
//  1. Walks the repo (caller-supplied file list).
//  2. Classifies each file to determine the per-language bucket.
//  3. Partitions files into batches of cfg.BatchSize.
//  4. Spawns up to cfg.Concurrency subprocesses, each running the
//     `archigraph extract` subcommand against one batch.
//  5. Streams JSONL envelopes from every subprocess's stdout and folds
//     them into a single Result.
//
// Memory contract: the coordinator's RSS is bounded by the final record
// set (entities + relationships) plus a small per-subprocess buffer for
// JSON decoding. It never holds AST trees or per-file source bytes.
func Coordinate(ctx context.Context, repoRoot string, files []string, cfg CoordinatorConfig) (*Result, error) {
	if repoRoot == "" {
		return nil, errors.New("coordinator: repoRoot is required")
	}
	bin := cfg.BinaryPath
	if bin == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve archigraph binary: %w", err)
		}
		bin = exe
	}

	// Pre-classify so we can partition by language. Classification is
	// cheap (filename + size); it does not parse the file.
	buckets, err := bucketByLanguage(ctx, repoRoot, files)
	if err != nil {
		return nil, err
	}

	tmpDir := cfg.TmpDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	batchDir, err := os.MkdirTemp(tmpDir, "archigraph-extract-")
	if err != nil {
		return nil, fmt.Errorf("create batch dir: %w", err)
	}
	defer os.RemoveAll(batchDir)

	batches, err := writeBatches(batchDir, buckets, cfg.batchSize())
	if err != nil {
		return nil, err
	}

	// Pre-pass (#1292): build the repo-wide DRF register-name registry by
	// scanning ALL Python files before any extraction batch spawns. This
	// ensures every subprocess receives the complete set of router.register()
	// basenames regardless of which batch those files land in.
	//
	// Without this, each subprocess only scanned its own partial batch, so
	// register() calls in batch N were invisible to batch M — resulting in
	// ghost bare-prefix Route entities (e.g. /alternate-addresses, /aoc)
	// surviving suppression and appearing as phantom http_endpoints.
	drfNamesPath, drfWriteErr := writeDRFNamesFile(batchDir, repoRoot, buckets["python"])
	if drfWriteErr != nil {
		// Non-fatal: fall back to per-batch scan (which is correct for single-batch
		// repos and avoids a hard failure if the pre-pass itself has an I/O error).
		drfNamesPath = ""
	}

	// Pre-pass (#2505): build the repo-wide ORM field-name index by scanning
	// ALL Python files before any extraction batch spawns. This ensures every
	// subprocess receives the complete set of Django model field names
	// regardless of which batch those model files land in.
	//
	// Without this, each subprocess only scanned its own partial batch, so
	// model fields defined in batch N were invisible to batch M — causing
	// applyORMFieldEdges in batch M to emit no READS_FIELD edges for cross-
	// batch ORM references (e.g. views.py in batch M querying User.cognito_id
	// where User is defined in models.py in batch N).
	ormFieldsPath, ormWriteErr := writeORMFieldsFile(batchDir, repoRoot, buckets["python"])
	if ormWriteErr != nil {
		// Non-fatal: fall back to per-batch scan (original #2448 behaviour,
		// correct for single-batch repos and avoids a hard failure on I/O error).
		ormFieldsPath = ""
	}

	skip := strings.Join(cfg.SkipPasses, ",")
	stderr := cfg.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	res := &Result{
		ByLang:     map[string]int{},
		ByCrossExt: map[string]int{},
	}
	var mu sync.Mutex

	sem := make(chan struct{}, cfg.concurrency())
	// Per-subprocess GOMAXPROCS cap (#3648). Computed once; applied to every
	// child's environment so the Go runtime in each extract subprocess does not
	// scale its thread pool to the full host core count.
	childGOMAXPROCS := strconv.Itoa(extractGOMAXPROCS())
	childEnv := append(os.Environ(), "GOMAXPROCS="+childGOMAXPROCS)
	var wg sync.WaitGroup
	var firstErr error
	var firstErrOnce sync.Once
	setErr := func(e error) { firstErrOnce.Do(func() { firstErr = e }) }

	for _, b := range batches {
		select {
		case <-ctx.Done():
			setErr(ctx.Err())
		default:
		}
		if firstErr != nil {
			break
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(b batchSpec) {
			defer wg.Done()
			defer func() { <-sem }()

			args := []string{
				"extract",
				"--repo", repoRoot,
				"--batch", b.path,
				"--batch-id", b.id,
			}
			if b.language != "" {
				args = append(args, "--lang", b.language)
			}
			if drfNamesPath != "" {
				args = append(args, "--drf-names", drfNamesPath)
			}
			if ormFieldsPath != "" {
				args = append(args, "--orm-fields", ormFieldsPath)
			}
			if skip != "" {
				args = append(args, "--skip-pass", skip)
			}

			cmd := exec.CommandContext(ctx, bin, args...)
			// Bound the child's Go runtime to extractGOMAXPROCS() cores so
			// concurrent extract subprocesses can't collectively saturate the
			// host (#3648). GOMAXPROCS is appended last so it wins over any
			// inherited value.
			cmd.Env = childEnv
			cmd.Stderr = stderr
			stdout, perr := cmd.StdoutPipe()
			if perr != nil {
				setErr(fmt.Errorf("stdout pipe (%s): %w", b.id, perr))
				return
			}
			if serr := cmd.Start(); serr != nil {
				setErr(fmt.Errorf("start subprocess (%s): %w", b.id, serr))
				return
			}

			ents, rels, stats, errs := decodeStream(stdout)

			if werr := cmd.Wait(); werr != nil {
				// A non-zero exit with usable output is a soft failure —
				// surface it as a non-fatal error but keep going.
				mu.Lock()
				res.NonFatalErrors = append(res.NonFatalErrors,
					fmt.Sprintf("subprocess %s exit: %v", b.id, werr))
				mu.Unlock()
			}

			mu.Lock()
			res.Entities = append(res.Entities, ents...)
			res.Relationships = append(res.Relationships, rels...)
			res.NonFatalErrors = append(res.NonFatalErrors, errs...)
			res.Subprocesses++
			if stats != nil {
				res.Files += stats.Files
				res.Processed += stats.Processed
				res.Extracted += stats.Extracted
				res.Skipped += stats.Skipped
				res.Failed += stats.Failed
				res.Pass1Rels += stats.Pass1Rels
				res.Pass2Rels += stats.Pass2Rels
				res.Pass25Rels += stats.Pass25Rels
				res.Pass3Rels += stats.Pass3Rels
				for k, v := range stats.ByLang {
					res.ByLang[k] += v
				}
				for k, v := range stats.ByCrossExt {
					res.ByCrossExt[k] += v
				}
				if stats.RSSBytes > res.PeakRSSBytes {
					res.PeakRSSBytes = stats.RSSBytes
				}
				res.SubprocessRSS = append(res.SubprocessRSS, stats.RSSBytes)
				res.Pass1PlumbedTrueCount += stats.Pass1PlumbedTrueCount
				res.Pass1PlumbedFalseCount += stats.Pass1PlumbedFalseCount
			}
			mu.Unlock()
		}(b)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	// #1885 — Config entity discovery pass. Runs in-process against the
	// full file list (pre-classification) so project-level config files
	// that no subprocess saw (Dockerfile, Makefile, .env, etc.) become
	// first-class SCOPE.Config entities. Failure is non-fatal — config
	// signal is supplemental.
	if configEnts, configRels, derr := configextract.Discover(ctx, repoRoot, files); derr == nil {
		res.Entities = append(res.Entities, configEnts...)
		res.Relationships = append(res.Relationships, configRels...)
	} else {
		res.NonFatalErrors = append(res.NonFatalErrors,
			fmt.Sprintf("config_discover: %v", derr))
	}

	// #2183 — Bazel BUILD-graph fusion (M6). Parses BUILD/BUILD.bazel files
	// and emits BAZEL_DEPENDS_ON edges + target entities. Failure is
	// non-fatal — build-graph signal is supplemental.
	if bazelEnts, bazelRels, derr := bazelextract.Discover(ctx, repoRoot, files); derr == nil {
		res.Entities = append(res.Entities, bazelEnts...)
		res.Relationships = append(res.Relationships, bazelRels...)

		// Resolver overlay: cross-reference BAZEL_DEPENDS_ON against CALLS/IMPORTS.
		overlayResult := resolve.RunBazelOverlay(res.Entities, res.Relationships)
		res.Relationships = append(res.Relationships, overlayResult.AnnotatedRels...)
	} else {
		res.NonFatalErrors = append(res.NonFatalErrors,
			fmt.Sprintf("bazel_discover: %v", derr))
	}

	// Issue #481 — deterministic ordering. Subprocesses complete in
	// scheduler-dependent order; sort canonically so downstream passes
	// (BuildIndex first-writer-wins, dedup) see a stable slice and
	// graph.json is byte-identical across runs.
	sortEntityRecords(res.Entities)
	sortRelationshipRecords(res.Relationships)

	return res, nil
}

// batchSpec is the coordinator's internal handle for one subprocess.
type batchSpec struct {
	id       string
	path     string
	language string
}

// bucketByLanguage classifies every file and returns a map from
// classifier language tag to repo-relative paths. Files the classifier
// marks Skip (or with empty language) are dropped here so subprocesses
// never see them.
func bucketByLanguage(ctx context.Context, repoRoot string, files []string) (map[string][]string, error) {
	cls, err := classifier.New("", nil)
	if err != nil {
		return nil, fmt.Errorf("init classifier: %w", err)
	}
	out := map[string][]string{}
	for _, rel := range files {
		abs := filepath.Join(repoRoot, rel)
		var size int64 = -1
		if st, err := os.Stat(abs); err == nil {
			size = st.Size()
		}
		cr := cls.ClassifyWithSize(ctx, rel, size)
		if cr.Skip || cr.Language == "" {
			continue
		}
		out[cr.Language] = append(out[cr.Language], rel)
	}
	return out, nil
}

// writeBatches partitions each language bucket into batches of size
// batchSize and writes one batch file per partition. Returns the list
// of batches that need to be processed.
//
// Languages with very small file counts share batches with their
// language pinned, so the subprocess only loads grammars it needs.
// Mixed-language fallback is intentionally avoided — the cleanest way
// to bound subprocess memory is to keep each subprocess single-language.
func writeBatches(dir string, buckets map[string][]string, batchSize int) ([]batchSpec, error) {
	// Sort languages for deterministic batch ordering.
	langs := make([]string, 0, len(buckets))
	for l := range buckets {
		langs = append(langs, l)
	}
	sort.Strings(langs)

	var batches []batchSpec
	for _, lang := range langs {
		files := buckets[lang]
		sort.Strings(files)
		for i := 0; i < len(files); i += batchSize {
			end := i + batchSize
			if end > len(files) {
				end = len(files)
			}
			id := fmt.Sprintf("%s-%04d", lang, i/batchSize)
			path := filepath.Join(dir, id+".txt")
			f, err := os.Create(path)
			if err != nil {
				return nil, fmt.Errorf("create batch file: %w", err)
			}
			w := bufio.NewWriter(f)
			for _, rel := range files[i:end] {
				fmt.Fprintln(w, rel)
			}
			if err := w.Flush(); err != nil {
				f.Close()
				return nil, err
			}
			if err := f.Close(); err != nil {
				return nil, err
			}
			batches = append(batches, batchSpec{id: id, path: path, language: lang})
		}
	}
	return batches, nil
}

// writeDRFNamesFile scans all Python files in pyFiles for router.register()
// basenames and writes one basename per line to a temp file in dir. Returns
// the absolute path of the written file, or ("", nil) when no names were
// found (so subprocesses stay in fallback mode and don't need the flag).
//
// This is the coordinator side of the #1292 multi-batch DRF ghost fix. By
// scanning ALL Python files at once (before any subprocess spawns), we produce
// a complete, repo-wide set of DRF register basenames. Each subprocess then
// loads this file via --drf-names instead of scanning only its own partial
// batch — eliminating the cross-batch visibility gap that caused 124 ghost
// bare-prefix paths to survive suppression.
func writeDRFNamesFile(dir, repoRoot string, pyFiles []string) (string, error) {
	if len(pyFiles) == 0 {
		return "", nil
	}

	// Scan all Python files into the engine's global registry temporarily.
	engine.ClearDRFRegisterNames()
	for _, rel := range pyFiles {
		abs := filepath.Join(repoRoot, rel)
		content, err := os.ReadFile(abs)
		if err != nil {
			continue // skip unreadable files; don't fail the whole pre-pass
		}
		engine.ScanDRFRegisterNames(content)
	}

	names := engine.CollectDRFRegisterNames()
	// Always clear after collecting — we don't want this coordinator-side global
	// to bleed into any in-process extraction that might follow (tests, etc.).
	engine.ClearDRFRegisterNames()

	if len(names) == 0 {
		return "", nil // no register() calls found; subprocesses use fallback
	}

	f, err := os.CreateTemp(dir, "drf-names-*.txt")
	if err != nil {
		return "", fmt.Errorf("create drf-names file: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, n := range names {
		if _, err := fmt.Fprintln(w, n); err != nil {
			return "", fmt.Errorf("write drf-names file: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return "", fmt.Errorf("flush drf-names file: %w", err)
	}
	return f.Name(), nil
}

// writeORMFieldsFile scans all Python files in pyFiles for Django model field
// declarations and writes one "<Model>.<field>" name per line to a temp file
// in dir. Returns the absolute path of the written file, or ("", nil) when no
// field names were found (so subprocesses stay in fallback mode).
//
// This is the coordinator side of the #2505 multi-batch ORM cross-file fix.
// By scanning ALL Python files at once (before any subprocess spawns), we
// produce a complete, repo-wide set of Django model field names. Each
// subprocess then loads this file via --orm-fields instead of scanning only
// its own partial batch — eliminating the cross-batch visibility gap that
// caused applyORMFieldEdges to miss READS_FIELD edges for models defined in
// other batches (e.g. User.cognito_id defined in models.py, queried in
// views.py, where the two files land in different subprocess batches).
//
// The temp file lives in batchDir and is cleaned up by the caller's
// `defer os.RemoveAll(batchDir)` — no separate cleanup is required.
func writeORMFieldsFile(dir, repoRoot string, pyFiles []string) (string, error) {
	if len(pyFiles) == 0 {
		return "", nil
	}

	// Collect all "<Model>.<field>" names across every Python file using
	// the shared regex-based BuildFieldIndex. No AST / tree-sitter needed.
	seen := map[string]bool{}
	for _, rel := range pyFiles {
		abs := filepath.Join(repoRoot, rel)
		content, err := os.ReadFile(abs)
		if err != nil {
			continue // skip unreadable files; don't fail the whole pre-pass
		}
		idx := engine.BuildFieldIndex(string(content))
		for name := range idx {
			seen[name] = true
		}
	}

	if len(seen) == 0 {
		return "", nil // no Django model fields found; subprocesses use fallback
	}

	// Sort for determinism so that the file is byte-identical across runs.
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	f, err := os.CreateTemp(dir, "orm-fields-*.txt")
	if err != nil {
		return "", fmt.Errorf("create orm-fields file: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, n := range names {
		if _, err := fmt.Fprintln(w, n); err != nil {
			return "", fmt.Errorf("write orm-fields file: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return "", fmt.Errorf("flush orm-fields file: %w", err)
	}
	return f.Name(), nil
}

// decodeStream consumes the subprocess's stdout (one JSONL envelope per
// line) and splits it into entity records, standalone relationships,
// the trailing BatchStats, and any KindError messages. A malformed line
// terminates the stream — callers treat this as a soft failure (the
// subprocess output we did parse is still usable).
func decodeStream(r io.Reader) ([]types.EntityRecord, []types.RelationshipRecord, *BatchStats, []string) {
	var (
		ents  []types.EntityRecord
		rels  []types.RelationshipRecord
		stats *BatchStats
		errs  []string
	)
	dec := json.NewDecoder(bufio.NewReaderSize(r, 256*1024))
	for {
		var env Envelope
		if err := dec.Decode(&env); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			errs = append(errs, fmt.Sprintf("decode envelope: %v", err))
			break
		}
		switch env.Type {
		case KindEntity:
			if env.Entity != nil {
				ents = append(ents, *env.Entity)
			}
		case KindRelationship:
			if env.Rel != nil {
				rels = append(rels, *env.Rel)
			}
		case KindStats:
			stats = env.Stats
		case KindError:
			if env.Err != "" {
				errs = append(errs, env.Err)
			}
		}
	}
	return ents, rels, stats, errs
}

// sortEntityRecords / sortRelationshipRecords mirror the helpers in
// cmd/archigraph/index.go so the merged record set is byte-identical to
// the in-process pipeline's. Kept in this package (rather than imported)
// so the coordinator does not introduce an internal/daemon → cmd cycle.
func sortEntityRecords(s []types.EntityRecord) {
	sort.SliceStable(s, func(a, b int) bool {
		ra, rb := &s[a], &s[b]
		if ra.SourceFile != rb.SourceFile {
			return ra.SourceFile < rb.SourceFile
		}
		if ra.Kind != rb.Kind {
			return ra.Kind < rb.Kind
		}
		if ra.QualifiedName != rb.QualifiedName {
			return ra.QualifiedName < rb.QualifiedName
		}
		if ra.Name != rb.Name {
			return ra.Name < rb.Name
		}
		if ra.StartLine != rb.StartLine {
			return ra.StartLine < rb.StartLine
		}
		return ra.ID < rb.ID
	})
}

func sortRelationshipRecords(s []types.RelationshipRecord) {
	sort.SliceStable(s, func(a, b int) bool {
		ra, rb := &s[a], &s[b]
		if ra.FromID != rb.FromID {
			return ra.FromID < rb.FromID
		}
		if ra.ToID != rb.ToID {
			return ra.ToID < rb.ToID
		}
		return ra.Kind < rb.Kind
	})
}
