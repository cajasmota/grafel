package extract

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/cajasmota/archigraph/internal/classifier"
	"github.com/cajasmota/archigraph/internal/engine"
	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors"
	"github.com/cajasmota/archigraph/internal/extractors/cross"
	"github.com/cajasmota/archigraph/internal/treesitter"
)

// SubprocessOptions configures a single short-lived extractor run.
// Construction is done by the daemon-side coordinator; the subprocess
// itself parses these out of CLI flags.
type SubprocessOptions struct {
	// RepoRoot is the absolute path of the repo being indexed. Paths
	// in BatchPath are relative to this root.
	RepoRoot string

	// Language pre-filters files to a single classifier language tag.
	// Empty means "any language" — useful for small repos where the
	// batch comfortably fits in one subprocess regardless of mix.
	Language string

	// BatchPath is the absolute path of a file containing one repo-
	// relative path per line. The subprocess streams this file rather
	// than receiving paths via argv to dodge OS argv-length limits on
	// large batches.
	BatchPath string

	// BatchID is an opaque label propagated into BatchStats.
	BatchID string

	// Output is the writer the subprocess emits JSONL to. In production
	// this is os.Stdout; tests inject a buffer.
	Output io.Writer

	// SkipPasses mirrors the indexer's pass-skip set. Per-file passes
	// (extract, framework, cross-lang) honour their entry; non-per-file
	// passes (graph-algo, build-document, enrichment) never run inside
	// the subprocess regardless of this set — the daemon owns those.
	SkipPasses map[string]bool
}

// Run is the subprocess-side entrypoint. It is invoked from
// `archigraph extract` (see cmd/archigraph/extract.go) and runs the
// per-file passes against the supplied batch, streaming JSONL records
// to opts.Output. Run returns nil on success even if some files within
// the batch fail to extract — per-file failures are surfaced as
// KindError envelopes so the coordinator can keep going.
//
// Memory profile (per spec target): ~80-150MB RSS for a 50-100 file
// batch (tree-sitter parse state + emitted records). All resources
// release on subprocess exit.
func Run(ctx context.Context, opts SubprocessOptions) error {
	if opts.RepoRoot == "" {
		return errors.New("extract: --repo is required")
	}
	if opts.BatchPath == "" {
		return errors.New("extract: --batch is required")
	}
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	files, err := readBatch(opts.BatchPath)
	if err != nil {
		return fmt.Errorf("read batch %s: %w", opts.BatchPath, err)
	}

	cls, err := classifier.New("", nil)
	if err != nil {
		return fmt.Errorf("init classifier: %w", err)
	}
	parser := treesitter.NewParserFactory(nil)

	rules, err := engine.LoadAllRules()
	if err != nil {
		return fmt.Errorf("load engine rules: %w", err)
	}
	detector := engine.New(rules)

	// We use buffered writes with a mutex because the cross-extractor
	// loop is single-threaded per file but we want a single coherent
	// stream of envelopes regardless of any future parallelism.
	bw := bufio.NewWriterSize(opts.Output, 64*1024)
	enc := json.NewEncoder(bw)
	var writeMu sync.Mutex
	emit := func(env Envelope) {
		writeMu.Lock()
		_ = enc.Encode(env)
		writeMu.Unlock()
	}

	stats := BatchStats{
		BatchID:    opts.BatchID,
		Files:      len(files),
		ByLang:     map[string]int{},
		ByCrossExt: map[string]int{},
	}

	crossExtractors := cross.AllExtractors()
	runExtract := !opts.SkipPasses["extract"]
	runFramework := !opts.SkipPasses["framework"]
	runCross := !opts.SkipPasses["cross-lang"]

	for _, rel := range files {
		abs := filepath.Join(opts.RepoRoot, rel)
		st, statErr := os.Stat(abs)
		var size int64 = -1
		if statErr == nil {
			size = st.Size()
		}
		cr := cls.ClassifyWithSize(ctx, rel, size)
		if cr.Skip || cr.Language == "" {
			stats.Skipped++
			continue
		}
		if opts.Language != "" && cr.Language != opts.Language {
			// Skip silently — coordinator partitioned by language and
			// gave us this file by mistake (or classifier disagreed
			// with the coordinator's filename-based bucket).
			stats.Skipped++
			continue
		}

		content, readErr := os.ReadFile(abs)
		if readErr != nil {
			stats.Failed++
			emit(Envelope{Type: KindError, Err: fmt.Sprintf("read %s: %v", rel, readErr)})
			continue
		}

		file := extractor.FileInput{
			Path:     rel,
			Content:  content,
			Language: cr.Language,
			RepoRoot: opts.RepoRoot,
		}

		// PLT #537 — route .tsx/.jsx through the tsx grammar (mirrors
		// the in-process Pass 1 path so the subprocess produces the
		// exact same entities as in-process for those files).
		parseLang := cr.Language
		if parseLang == "typescript" || parseLang == "javascript" {
			low := strings.ToLower(rel)
			if strings.HasSuffix(low, ".tsx") || strings.HasSuffix(low, ".jsx") {
				parseLang = "tsx"
			}
		}
		if pr, perr := parser.Parse(ctx, content, parseLang); perr == nil && pr != nil {
			file.Tree = pr.Tree
		}

		// Pass 1 — per-language extraction.
		if runExtract {
			ents, exErr := extractors.Extract(ctx, file)
			if exErr != nil {
				if errors.Is(exErr, extractors.ErrNoExtractorForLanguage) {
					stats.Skipped++
				} else {
					stats.Failed++
					emit(Envelope{Type: KindError, Err: fmt.Sprintf("extract %s: %v", rel, exErr)})
				}
			} else {
				stats.Processed++
				stats.Extracted++
				rels := 0
				for k := range ents {
					rels += len(ents[k].Relationships)
					e := ents[k]
					emit(Envelope{Type: KindEntity, Entity: &e})
				}
				stats.Pass1Rels += rels
				stats.ByLang[cr.Language] += rels
			}
		}

		// Pass 2.5 — YAML framework rules.
		if runFramework {
			if res, derr := detector.Detect(ctx, file); derr == nil && res != nil {
				for k := range res.Entities {
					e := res.Entities[k]
					emit(Envelope{Type: KindEntity, Entity: &e})
					stats.Pass25Rels += len(e.Relationships)
				}
				for k := range res.Relationships {
					r := res.Relationships[k]
					emit(Envelope{Type: KindRelationship, Rel: &r})
				}
				stats.Pass25Rels += len(res.Relationships)
			}
		}

		// Pass 3 — cross-language extractors.
		if runCross {
			for _, ce := range crossExtractors {
				out, cerr := ce.Extractor.Extract(ctx, file)
				if cerr != nil || len(out) == 0 {
					continue
				}
				rels := 0
				for k := range out {
					rels += len(out[k].Relationships)
					e := out[k]
					emit(Envelope{Type: KindEntity, Entity: &e})
				}
				stats.Pass3Rels += rels
				stats.ByCrossExt[ce.Name] += rels
			}
		}

		// Release the parse tree before moving to the next file —
		// tree-sitter trees are CGo-allocated and runtime.GC cannot
		// reclaim them. This is what keeps batch RSS bounded.
		if file.Tree != nil {
			file.Tree.Close()
			file.Tree = nil
		}
		// Drop the content slice promptly too. The next loop iteration
		// will overwrite the per-iteration locals but content holds the
		// raw file bytes which can be MB on large source files.
		content = nil
		_ = content
	}

	// Self-report RSS so the coordinator can log per-subprocess peak.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	stats.RSSBytes = ms.Sys
	emit(Envelope{Type: KindStats, Stats: &stats})

	return bw.Flush()
}

// readBatch reads a newline-delimited file of repo-relative paths.
// Blank lines and lines beginning with '#' are skipped so the batch
// files are inspectable by hand when debugging.
func readBatch(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var out []string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, sc.Err()
}
