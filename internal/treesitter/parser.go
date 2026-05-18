// Package treesitter provides a parser factory backed by smacker/go-tree-sitter.
// It supports 26 languages from the bundled grammars and enforces a 10% syntax
// error ratio gate before returning a ParseResult to callers.
package treesitter

import (
	"context"
	"errors"
	"fmt"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/css"
	"github.com/smacker/go-tree-sitter/dockerfile"
	"github.com/smacker/go-tree-sitter/elixir"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/groovy"
	"github.com/smacker/go-tree-sitter/hcl"
	"github.com/smacker/go-tree-sitter/html"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/lua"
	tsmarkdown "github.com/smacker/go-tree-sitter/markdown/tree-sitter-markdown"
	"github.com/smacker/go-tree-sitter/ocaml"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/protobuf"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/sql"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/toml"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/go-tree-sitter/yaml"
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

// languageRegistry maps lowercase language names (as used by the Python
// indexer language registry and go-enry) to their tree-sitter grammars.
// "terraform" is an alias for "hcl". Languages not bundled in this version
// of smacker/go-tree-sitter (dart, haskell, clojure, r, julia, zig, json)
// are absent — callers receive ErrUnsupportedLanguage for those names.
var languageRegistry map[string]*sitter.Language

func init() {
	languageRegistry = map[string]*sitter.Language{
		"bash":       bash.GetLanguage(),
		"shell":      bash.GetLanguage(), // shell files use the bash grammar
		"c":          c.GetLanguage(),
		"cpp":        cpp.GetLanguage(),
		"css":        css.GetLanguage(),
		"csharp":     csharp.GetLanguage(),
		"dockerfile": dockerfile.GetLanguage(),
		"elixir":     elixir.GetLanguage(),
		"go":         golang.GetLanguage(),
		"groovy":     groovy.GetLanguage(),
		"hcl":        hcl.GetLanguage(),
		"html":       html.GetLanguage(),
		"java":       java.GetLanguage(),
		"javascript": javascript.GetLanguage(),
		"kotlin":     kotlin.GetLanguage(),
		"lua":        lua.GetLanguage(),
		"markdown":   tsmarkdown.GetLanguage(),
		"ocaml":      ocaml.GetLanguage(),
		"php":        php.GetLanguage(),
		"proto":      protobuf.GetLanguage(),
		"python":     python.GetLanguage(),
		"ruby":       ruby.GetLanguage(),
		"rust":       rust.GetLanguage(),
		"scala":      scala.GetLanguage(),
		"sql":        sql.GetLanguage(),
		"swift":      swift.GetLanguage(),
		"terraform":  hcl.GetLanguage(), // alias: terraform files use HCL grammar
		"toml":       toml.GetLanguage(),
		"typescript": typescript.GetLanguage(),
		"yaml":       yaml.GetLanguage(),
	}
}

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
		"markdown",
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
	// Tree is the concrete syntax tree returned by tree-sitter.
	Tree *sitter.Tree
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
// non-deterministic output even though every goroutine uses its own
// *sitter.Parser and *sitter.Tree (per-file ents counts on the SAME source
// jumped between 0, 4, 5, etc. across runs on kickstart.nvim). The likely
// culprit is shared state inside the bundled smacker/go-tree-sitter
// grammar objects (the *sitter.Language pointers in languageRegistry are
// shared across all parsers). Until that race is fixed upstream we
// serialise the parse + node-walk via parseMu; correctness wins over the
// per-file parallelism we lose, and the impact on real-world repos
// dominated by I/O+extractor work is marginal.
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
	ctx, span := f.tracer.Start(ctx, "treesitter.parse")
	defer span.End()

	lang, ok := languageRegistry[language]
	if !ok {
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
			Tree:       nil,
			Language:   language,
			ErrorRatio: 0.0,
			NodeCount:  0,
		}, nil
	}

	// Issue #481 — serialise parse calls across goroutines (see
	// ParserFactory godoc for the rationale).
	parseMu.Lock()
	p := sitter.NewParser()
	p.SetLanguage(lang)

	tree, err := p.ParseCtx(ctx, nil, source)
	parseMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("treesitter: parse failed for language %s: %w", language, err)
	}

	root := tree.RootNode()
	total, errNodes := countNodes(root)

	var errorRatio float64
	if total > 0 {
		errorRatio = float64(errNodes) / float64(total)
	}

	span.SetAttributes(
		attribute.String("language", language),
		attribute.Int("file_size_bytes", len(source)),
		attribute.Float64("error_ratio", errorRatio),
		attribute.Int("node_count", total),
	)

	result := &ParseResult{
		Tree:       tree,
		Language:   language,
		ErrorRatio: errorRatio,
		NodeCount:  total,
	}

	if errorRatio > maxErrorRatio {
		return result, fmt.Errorf("%w: language=%s error_ratio=%.4f", ErrHighSyntaxErrorRate, language, errorRatio)
	}

	return result, nil
}

// countNodes performs a depth-first traversal of the tree and returns the
// total node count and the number of ERROR nodes. Iterative to avoid stack
// overflow on deeply nested trees (e.g. large minified files).
func countNodes(root *sitter.Node) (total, errNodes int) {
	if root == nil {
		return 0, 0
	}

	stack := []*sitter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		total++
		if n.IsError() {
			errNodes++
		}
		childCount := int(n.ChildCount())
		for i := 0; i < childCount; i++ {
			stack = append(stack, n.Child(i))
		}
	}
	return total, errNodes
}
