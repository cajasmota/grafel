// Package treesitter provides a parser factory over the grafel-owned ts binding
// abstraction (internal/treesitter/ts). It supports the bundled grammars and
// enforces a 10% syntax error ratio gate before returning a ParseResult.
//
// Binding (B2 cutover, ADR 0023, #5418). Every language is parsed through the
// official tree-sitter/go-tree-sitter binding via its per-language grammar
// provider (internal/treesitter/ts/grammars/<lang>). The legacy smacker binding
// has been removed entirely; the migrated set and the resolver live in
// adapters.go. ParseResult.TSTree is the binding-agnostic tree every extractor
// consumes.
package treesitter

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/cajasmota/grafel/internal/indexstate"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Sentinel errors.
var (
	// ErrUnsupportedLanguage is returned when the requested language name has no
	// registered grammar in the factory.
	ErrUnsupportedLanguage = errors.New("treesitter: unsupported language")

	// ErrHighSyntaxErrorRate is returned when the parsed tree's error_ratio
	// exceeds the 10% fault-tolerance gate defined in [DECISION] A6.
	ErrHighSyntaxErrorRate = errors.New("treesitter: syntax error rate exceeds 10%")
)

// maxErrorRatio is the fault-tolerance threshold from [DECISION] A6.
// Files with error_ratio > maxErrorRatio are rejected as too malformed.
const maxErrorRatio = 0.10

// SupportedLanguages returns the sorted list of language names accepted by
// the factory. The slice is a copy — callers may not modify it.
func SupportedLanguages() []string {
	// Return a fixed ordered slice so tests can assert on length and membership
	// without relying on map iteration order.
	return []string{
		"bash",
		"c",
		// "shell" is an alias for bash and "protobuf" an alias for proto; aliases are
		// omitted from the sorted list to avoid duplication.
		// Callers querying SupportedLanguages() see "bash" / "proto"; the factory
		// also accepts "shell" and "protobuf".
		"cpp",
		"css",
		"csharp",
		"dockerfile",
		"elixir",
		"go",
		"groovy",
		"hcl",
		"html",
		"java",
		"javascript",
		"kotlin",
		"lua",
		"ocaml",
		"php",
		"proto",
		"python",
		"ruby",
		"rust",
		"scala",
		"sql",
		"swift",
		"terraform",
		"toml",
		"typescript",
		"yaml",
	}
}

// ParseResult holds the output of a single Parse call.
type ParseResult struct {
	// TSTree is the binding-agnostic parse tree. Extractors consume it via the
	// ts façade (ADR 0023, #5418). It is populated on success, but is nil
	// when Parse returns ErrHighSyntaxErrorRate (#5963) — the tree is closed
	// internally before the error is returned, since no caller uses it on
	// that path and leaving it live with no owner leaked C heap for the life
	// of the process.
	TSTree ts.Tree
	// Language is the normalised language name used for the parse.
	Language string
	// ErrorRatio is the fraction of ERROR nodes in the tree
	// (error_nodes / total_nodes). 0.0 means no syntax errors.
	ErrorRatio float64
	// NodeCount is the total number of nodes in the tree.
	NodeCount int
}

// ParserFactory creates tree-sitter parsers for supported languages.
//
// Concurrency. Parse is safe for concurrent use from any number of goroutines
// (#5954, closing the ADR-0023 §5 open follow-up). The historical global
// `parseMu` that serialised every process-wide parse is gone; it was a
// workaround for issue #481, whose root cause ADR-0023:73 identifies as a
// shared-grammar-state race in the *smacker* binding — a binding that has
// since been removed entirely (see the package godoc). Under the official
// binding the safety argument is:
//
//   - Every parse builds and owns its OWN *tsofficial.Parser
//     (parseOfficial -> adapter.NewParser -> ts_parser_new, Close'd on
//     return). tree-sitter's C contract is "one TSParser per thread", which
//     this satisfies by construction — no parser instance is ever shared.
//   - The only object shared across goroutines is the per-language
//     ts.Language handle in migratedLanguages (adapters.go). A TSLanguage is
//     an immutable, statically-allocated const struct; ts_parser_set_language
//     stores it via ts_language_copy, which for a non-wasm language is a
//     no-op returning the same pointer (upstream src/language.c). Nothing
//     ever writes through it, and tree-sitter documents TSLanguage as safe to
//     share read-only across parsers.
//   - No bundled grammar's external scanner keeps mutable file-scope state;
//     scanner state is allocated per parser by its create() hook. (The
//     TSLanguage ref-count path in ts_language_copy is wasm-only and
//     unreachable: TREE_SITTER_FEATURE_WASM is not in any cgo CFLAGS line.)
//   - Corroborating evidence that this was always safe: unserialised
//     concurrent parsing has been shipping regardless. parseMu only ever
//     covered ParserFactory.Parse, while internal/extractors/yaml/helm.go
//     (unconditionally, for every Helm template) and every extractor's
//     `if file.TSTree == nil { NewParser… }` fallback construct their own
//     parser and call Parse OUTSIDE the mutex, from the same worker pool. The
//     mutex was cargo, not a constraint.
//
// CONCURRENCY IS NOT UNBOUNDED. See parseOfficial: the #5630 parse gate
// (indexstate.AcquireParseSlot) is the real ceiling on simultaneous
// ts_parser_parse calls, and it must be, because GOMAXPROCS does not bound cgo.
//
// Determinism is NOT provided by serialisation and never was: parseMu wrapped
// only ts_parser_parse, while the node walk, the extractors, and the
// append-to-shared-slice all ran outside it. The actual #481 fix is the
// canonical sort applied to the worker-pool output before anything downstream
// consumes it (sortClassifiedFiles / sortEntityRecords in
// cmd/grafel/index.go), which is unaffected by this change.
type ParserFactory struct {
	tracer trace.Tracer
}

// NewParserFactory constructs a ParserFactory.
// If tracer is nil, the global OTel tracer provider is used.
func NewParserFactory(tracer trace.Tracer) *ParserFactory {
	if tracer == nil {
		tracer = otel.Tracer("treesitter")
	}
	return &ParserFactory{tracer: tracer}
}

// Parse parses source using the grammar for language and returns a ParseResult.
//
// Behaviour:
//   - Returns ErrUnsupportedLanguage if language is not in the registry.
//   - Returns ErrHighSyntaxErrorRate if error_ratio > 10% (file too malformed).
//     The returned ParseResult still carries Language/ErrorRatio/NodeCount,
//     but TSTree is nil — the tree is closed before returning (#5963).
//   - An empty source slice returns a zero-node result with no error.
//   - The OTel span "treesitter.parse" is always emitted with attributes:
//     language, file_size_bytes, error_ratio, node_count.
func (f *ParserFactory) Parse(ctx context.Context, source []byte, language string) (*ParseResult, error) {
	_, span := f.tracer.Start(ctx, "treesitter.parse")
	defer span.End()

	if _, ok := migratedLanguages[language]; !ok {
		span.SetAttributes(
			attribute.String("language", language),
			attribute.Int("file_size_bytes", len(source)),
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedLanguage, language)
	}

	// Fast-path: empty source.
	if len(source) == 0 {
		span.SetAttributes(
			attribute.String("language", language),
			attribute.Int("file_size_bytes", 0),
			attribute.Float64("error_ratio", 0.0),
			attribute.Int("node_count", 0),
		)
		return &ParseResult{
			Language:   language,
			ErrorRatio: 0.0,
			NodeCount:  0,
		}, nil
	}

	return f.parseOfficial(span, source, language)
}

// abiGuardOnce ensures the ABI guard runs at most once per language.
var abiGuardOnce sync.Map // language -> *sync.Once

// parseOfficial parses source on the official binding. It runs the ABI guard
// once per language before the first real parse, so an ABI-incompatible grammar
// fails loudly here instead of SIGSEGV'ing on RootNode.
func (f *ParserFactory) parseOfficial(span trace.Span, source []byte, language string) (*ParseResult, error) {
	onceI, _ := abiGuardOnce.LoadOrStore(language, &sync.Once{})
	var guardErr error
	onceI.(*sync.Once).Do(func() { guardErr = abiGuard(language) })
	if guardErr != nil {
		return nil, guardErr
	}

	lang, adapter, _ := tsLanguageFor(language)

	// #5630 — account + cap. Every real (non-empty) tree-sitter parse passes
	// through here, so this is the single chokepoint that makes "the daemon is
	// parsing" ALWAYS observable (indexstate.ParseInFlight / the busy signal,
	// surfaced by grafel_index_status) and ALWAYS bounded by the daemon-wide
	// in-process parse ceiling. It fixes the untracked-parse bug where the
	// reactive/incremental in-process reindex re-parsed source while
	// index_status reported idle and the #5602 cap (subprocess-only) could not
	// throttle it. AcquireParseSlot blocks until a slot is free (no-op when the
	// gate is unbounded — non-daemon callers); ReleaseParseSlot frees it and
	// clears the busy counter. The slot is held across the parse + node-walk so
	// the whole CPU-heavy span counts and is capped.
	indexstate.AcquireParseSlot()
	defer indexstate.ReleaseParseSlot()

	// Parser construction/teardown operate on a per-call, independent parser
	// instance (fresh ts_parser_new + SetLanguage; Close frees only that
	// parser, and the produced tree outlives it). This per-call ownership is
	// what makes the parse below safe to run concurrently — see the
	// ParserFactory godoc for the full thread-safety argument (#5954).
	p, err := adapter.NewParser(lang)
	if err != nil {
		return nil, fmt.Errorf("treesitter: parser init failed for language %s: %w", language, err)
	}
	defer p.Close()

	// #5954 — parses run concurrently; the process-wide parseMu is gone.
	//
	// WHAT BOUNDS THIS. Exactly one thing: the AcquireParseSlot gate above
	// (#5630). It is a counting semaphore taken on the Go side and held ACROSS
	// this cgo call, so at most ParseConcurrencyCap() goroutines are inside
	// ts_parser_parse at any instant.
	//
	// GOMAXPROCS DOES NOT BOUND THIS, and it is a mistake to reason as if it
	// did: a goroutine in a cgo call parks in _Gsyscall and the runtime hands
	// its P to another goroutine, so N concurrent cgo calls occupy N OS threads
	// whatever GOMAXPROCS says. grafel's extract CPU budget is implemented
	// purely as GOMAXPROCS=<n> in a child's env, so it caps Go-side parallelism
	// and nothing else. The gate is therefore load-bearing, not belt-and-braces
	// — it is what makes GRAFEL_EXTRACT_GOMAXPROCS mean anything for C parsing.
	//
	// The cap is installed by the daemon at startup and, for the non-daemon
	// index path, by ensureParseConcurrencyDefault in cmd/grafel/index.go,
	// which sets the background indexing core budget: 25% of machine capacity,
	// max(1, NumCPU/4). Foreground/interactive rebuilds are deliberately
	// exempt — they are user-initiated and awaited. When no cap is installed
	// (a bare library caller, most tests) parsing is unbounded here and the
	// caller owns the ceiling.
	//
	// Coverage: the gate is NOT only this factory. Every extractor-level parse
	// that constructs its own ts.Parser and so bypasses the factory —
	// internal/extractors/yaml/helm.go and the seven `file.TSTree == nil`
	// inline fallbacks — takes the same slot around its Parse call (#5954).
	// That matters because those paths bypassed parseMu too, which is why they
	// are also the proof that unserialised concurrent parsing already shipped.
	// The one remaining ungated parse is abiGuard's per-language probe: it runs
	// once per language per process, on a few dozen bytes, before this gate is
	// acquired, so it is bounded by construction.
	//
	// The per-parse watchdog in official.Parse (GRAFEL_PARSE_TIMEOUT) still
	// bounds a runaway parse (#5473) — it now costs one slot instead of
	// freezing ALL in-process parsing.
	tree, err := p.Parse(source)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("treesitter: parse failed for language %s: %w", language, err)
	}
	if tree == nil {
		return nil, fmt.Errorf("treesitter: parse produced nil tree for language %s", language)
	}

	// #6736 — tree-sitter-kotlin misparses a top-level declaration that carries
	// two or more annotations and is not the last construct in the file, WITHOUT
	// producing a single ERROR node. Repair it here, before the ratio and the
	// error-skipping wrapper below, so every downstream consumer (the Kotlin
	// extractor as well as the Spring route pass) sees the declaration. Kotlin
	// only, and a no-op — no re-parse, no wrapper — unless the misparse
	// signature is actually present. See kotlin_annot_repair.go.
	if language == "kotlin" {
		if repaired, ok := repairKotlinAnnotationMisparse(p, source, tree); ok {
			tree = repaired
		}
	}

	total, errNodes := countNodesTS(tree.RootNode())
	errorRatio := ratio(total, errNodes)
	setParseSpan(span, language, len(source), errorRatio, total)

	if errorRatio > maxErrorRatio {
		// #5963 — same leak class as #5954/#5962: every caller bails on
		// err != nil without ever taking ownership of (or closing) the tree,
		// so a tree returned alongside ErrHighSyntaxErrorRate would leak for
		// the life of the process (no finalizer on go-tree-sitter@v0.24.0,
		// ~19.7 bytes of C heap per source byte). Files that trip this ratio
		// skew toward the largest minified/generated/malformed inputs, so the
		// leak was biased toward the biggest trees. Close it here — the
		// single chokepoint every real parse funnels through — instead of
		// patching every call site: no caller inspects the tree on this
		// error path (verified by audit), so there is nothing to preserve.
		// TSTree is left nil (not just closed) so a caller that forgets to
		// check the error still gets a nil-deref instead of a use-after-free
		// on a dangling Close()'d handle.
		tree.Close()
		return &ParseResult{
			Language:   language,
			ErrorRatio: errorRatio,
			NodeCount:  total,
		}, fmt.Errorf("%w: language=%s error_ratio=%.4f", ErrHighSyntaxErrorRate, language, errorRatio)
	}

	// #6360 — the ratio above is a whole-file AVERAGE, so a file with one
	// localised typo passes the gate and would otherwise be handed to the
	// extractor in full, ERROR subtree included. Hide the ERROR nodes and
	// everything below them so the extractor sees a true subset of the file
	// rather than tree-sitter's error recovery. Only pay for the wrapper when
	// there is actually something to hide: a clean parse (errNodes == 0, the
	// overwhelming majority) is returned completely untouched.
	outTree := tree
	if errNodes > 0 {
		outTree = newErrorSkippingTree(tree)
	}

	return &ParseResult{
		TSTree:     outTree,
		Language:   language,
		ErrorRatio: errorRatio,
		NodeCount:  total,
	}, nil
}

func ratio(total, errNodes int) float64 {
	if total > 0 {
		return float64(errNodes) / float64(total)
	}
	return 0
}

func setParseSpan(span trace.Span, language string, size int, errorRatio float64, total int) {
	span.SetAttributes(
		attribute.String("language", language),
		attribute.Int("file_size_bytes", size),
		attribute.Float64("error_ratio", errorRatio),
		attribute.Int("node_count", total),
	)
}

// countNodesTS traverses the ts façade and returns the total node count and the
// number of ERROR nodes. Iterative to avoid stack overflow on deeply nested
// trees (e.g. large minified files).
func countNodesTS(root ts.Node) (total, errNodes int) {
	if root == nil {
		return 0, 0
	}
	stack := []ts.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		total++
		if n.IsError() {
			errNodes++
		}
		childCount := int(n.ChildCount())
		for i := 0; i < childCount; i++ {
			if c := n.Child(i); c != nil {
				stack = append(stack, c)
			}
		}
	}
	return total, errNodes
}
