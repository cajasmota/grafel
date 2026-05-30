package cpp

// type_alias.go — C/C++ type alias extractor.
//
// Covers:
//
//  1. typedef <existing> <alias>;
//     e.g. typedef unsigned long size_t;
//          typedef struct Foo_ Foo;
//
//  2. using <alias> = <type>;   (C++11)
//     e.g. using MyInt = int;
//          using StringVec = std::vector<std::string>;
//          template<typename T> using Ptr = std::shared_ptr<T>;
//
// Each matched alias emits one SCOPE.Schema/type_alias entity.
// The base tree-sitter cpp extractor does NOT emit typedef or alias_declaration
// entities, so this custom extractor fills that gap for all c-cpp records.
//
// Status: partial (regex/heuristic; no full AST type resolution).

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_type_alias", &typeAliasExtractor{})
}

type typeAliasExtractor struct{}

func (e *typeAliasExtractor) Language() string { return "custom_cpp_type_alias" }

var (
	// typedef <type> <alias>;
	// Simple form: alias is the last identifier before ';'
	// e.g. typedef unsigned long size_t;
	//      typedef struct Foo_ Foo;
	reTypedefSimple = regexp.MustCompile(
		`(?m)\btypedef\b(?:[^(;]|\([^)]*\))*\b([A-Za-z_]\w*)\s*;`,
	)

	// typedef <ret> (*<alias>)(<params>);  — function pointer typedef
	// e.g. typedef void (*Callback)(int);
	// capture: (1) alias name inside (*...)
	reTypedefFuncPtr = regexp.MustCompile(
		`(?m)\btypedef\b[^(]*\(\s*\*\s*([A-Za-z_]\w*)\s*\)`,
	)

	// using <alias> = <type>;         (possibly with template prefix)
	// template<...> using <alias> = ...;
	// capture: (1) alias name
	reUsingAlias = regexp.MustCompile(
		`(?m)(?:template\s*<[^>]*>\s*)?using\s+([A-Za-z_]\w*)\s*=\s*[^;]+;`,
	)
)

func (e *typeAliasExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.type_alias_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}
	// Works for both C and C++
	if file.Language != "cpp" && file.Language != "c" {
		return nil, nil
	}

	src := string(file.Content)
	hasTypedef := strings.Contains(src, "typedef")
	hasUsing := strings.Contains(src, "using ")
	if !hasTypedef && !hasUsing {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	if hasTypedef {
		// Function-pointer typedefs take priority — match first so we don't
		// double-count them via the simple pattern.
		for _, m := range reTypedefFuncPtr.FindAllStringSubmatchIndex(src, -1) {
			alias := strings.TrimSpace(src[m[2]:m[3]])
			if seen[alias] || alias == "" {
				continue
			}
			seen[alias] = true
			ent := makeEntity(alias, "SCOPE.Schema", "type_alias", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"alias_kind", "typedef",
				"provenance", "INFERRED_FROM_TYPEDEF",
			)
			entities = append(entities, ent)
		}

		// Simple typedef: last identifier before ';'
		for _, m := range reTypedefSimple.FindAllStringSubmatchIndex(src, -1) {
			alias := strings.TrimSpace(src[m[2]:m[3]])
			if seen[alias] || alias == "" {
				continue
			}
			seen[alias] = true
			ent := makeEntity(alias, "SCOPE.Schema", "type_alias", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"alias_kind", "typedef",
				"provenance", "INFERRED_FROM_TYPEDEF",
			)
			entities = append(entities, ent)
		}
	}

	if hasUsing {
		for _, m := range reUsingAlias.FindAllStringSubmatchIndex(src, -1) {
			alias := strings.TrimSpace(src[m[2]:m[3]])
			if seen[alias] || alias == "" {
				continue
			}
			// Skip plain `using namespace std;` forms (already handled by base extractor)
			// reUsingAlias requires `=` so namespace-style is excluded.
			seen[alias] = true
			ent := makeEntity(alias, "SCOPE.Schema", "type_alias", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent,
				"alias_kind", "using",
				"provenance", "INFERRED_FROM_USING_ALIAS",
			)
			entities = append(entities, ent)
		}
	}

	return entities, nil
}
