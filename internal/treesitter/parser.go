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
		// "shell" is an alias for bash; omitted from sorted list to avoid duplication.
		// Callers querying SupportedLanguages() see "bash"; the factory accepts "shell".
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
	// TSTree is the binding-agnostic parse tree, always populated. Extractors
	// consume it via the ts façade (ADR 0023, #5418).
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
// Issue #481 — empirically, concurrent Parse() calls produced
// non-deterministic output. Until that is re-validated against the official
// binding (ADR 0023 §5) we serialise the parse + node-walk via parseMu;
// correctness wins over the per-file parallelism we lose, and the impact on
// real-world repos dominated by I/O+extractor work is marginal.
type ParserFactory struct {
	tracer trace.Tracer
}

// parseMu serialises tree-sitter parse calls across goroutines. See the
// ParserFactory godoc for the rationale.
var parseMu sync.Mutex

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
	// parser, and the produced tree outlives it). Issue #481's
	// non-determinism is specific to the parse itself, so we keep those out of
	// the critical section and serialise ONLY the parse below.
	p, err := adapter.NewParser(lang)
	if err != nil {
		return nil, fmt.Errorf("treesitter: parser init failed for language %s: %w", language, err)
	}
	defer p.Close()

	// Issue #481 — serialise the parse across goroutines (see ParserFactory
	// godoc for the rationale). The per-parse watchdog in official.Parse
	// (GRAFEL_PARSE_TIMEOUT) bounds how long this critical section can be held:
	// a runaway parse is halted at the deadline and returns an error instead of
	// hanging, so a single pathological file can no longer pin parseMu — and
	// thus ALL in-process parsing — indefinitely (was: ~26-min freeze, #5473).
	parseMu.Lock()
	tree, err := p.Parse(source)
	parseMu.Unlock()
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("treesitter: parse failed for language %s: %w", language, err)
	}
	if tree == nil {
		return nil, fmt.Errorf("treesitter: parse produced nil tree for language %s", language)
	}

	total, errNodes := countNodesTS(tree.RootNode())
	errorRatio := ratio(total, errNodes)
	setParseSpan(span, language, len(source), errorRatio, total)

	result := &ParseResult{
		TSTree:     tree,
		Language:   language,
		ErrorRatio: errorRatio,
		NodeCount:  total,
	}
	if errorRatio > maxErrorRatio {
		return result, fmt.Errorf("%w: language=%s error_ratio=%.4f", ErrHighSyntaxErrorRate, language, errorRatio)
	}
	return result, nil
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
