// Package extractors provides language and framework extractor registry.
// Language extractors register themselves via init() functions in subpackages
// and are dispatched by language name.
//
// Registration is delegated to the internal/extractor base package so that
// sub-packages (e.g., internal/extractors/golang) can call extractor.Register()
// without creating an import cycle.
package extractors

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/generated"
	"github.com/cajasmota/grafel/internal/types"
)

// ErrNoExtractorForLanguage is returned when no extractor is registered for the requested language.
var ErrNoExtractorForLanguage = errors.New("no extractor registered for language")

// FileInput is the input contract for all extractors.
// Re-exported from internal/extractor for callers that only import this package.
type FileInput = extractor.FileInput

// Extractor is the interface all language and framework extractors implement.
// Re-exported from internal/extractor for callers that only import this package.
type Extractor = extractor.Extractor

// tracer is set once via SetTracer before the registry is used in production.
var (
	globalTracer trace.Tracer
)

// SetTracer configures the OTel tracer used for extractor dispatch spans.
// Must be called before the first Extract call. Safe for concurrent use.
func SetTracer(t trace.Tracer) {
	globalTracer = t
}

func getTracer() trace.Tracer {
	return globalTracer
}

// Register adds an extractor to the global registry.
// Delegates to internal/extractor.Register so sub-packages can call either.
func Register(language string, e Extractor) {
	extractor.Register(language, e)
}

// Get retrieves the extractor registered for the given language.
// Returns false if no extractor is registered for that language.
func Get(language string) (Extractor, bool) {
	return extractor.Get(language)
}

// Extract dispatches to the registered extractor for the given language,
// wrapping execution in an OTel span and panic recovery.
//
// Returns ErrNoExtractorForLanguage if no extractor is registered.
// Returns a wrapped error if the extractor panics.
func Extract(ctx context.Context, file FileInput) (entities []types.EntityRecord, retErr error) {
	start := time.Now()

	t := getTracer()
	var span trace.Span
	if t != nil {
		ctx, span = t.Start(ctx, "extractor.dispatch",
			trace.WithAttributes(
				attribute.String("language", file.Language),
				attribute.String("file", file.Path),
			),
		)
		defer func() {
			durationMs := time.Since(start).Milliseconds()
			span.SetAttributes(
				attribute.Int64("duration_ms", durationMs),
				attribute.Int("entity_count", len(entities)),
			)
			if retErr != nil {
				span.RecordError(retErr)
				span.SetStatus(codes.Error, retErr.Error())
			}
			span.End()
		}()
	}

	ext, ok := Get(file.Language)
	if !ok {
		retErr = fmt.Errorf("%w: %s", ErrNoExtractorForLanguage, file.Language)
		return nil, retErr
	}

	entities, retErr = safeExtract(ctx, ext, file)
	return entities, retErr
}

// safeExtract calls ext.Extract, recovers from panics, and stamps the
// generated-source flag on whatever comes back (#6329).
//
// THE STAMP BELONGS HERE, not in FileInput. safeExtract is the one chokepoint
// every production extraction path funnels through — cmd/grafel/index.go via
// extractPass1, internal/daemon/extract/subproc.go via Extract, and
// internal/extractors/incremental.go which calls safeExtract directly (see the
// #6151 comment at its call site for why). Threading a "this file is
// generated" hint through FileInput instead would make it 60 independent
// opt-ins across 22 tree-sitter and 38 line-scanner extractors, with a silent
// failure mode: an extractor that ignored the hint would emit unstamped
// entities and nothing downstream could tell. Here no extractor is consulted,
// so none can forget.
//
// Detection can only ever ADD the flag. Nothing on this path drops a file,
// drops an entity, or narrows extraction — PR1 changes what the graph SAYS
// about a file, never what it CONTAINS. Skipping bodies (ProjectDeclarations)
// is deliberately a separate change, because that one can lose data.
func safeExtract(ctx context.Context, ext Extractor, file FileInput) (entities []types.EntityRecord, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("extractor panicked: %v", r)
		}
	}()
	entities, retErr = ext.Extract(ctx, file)
	return stampGenerated(file, entities), retErr
}

// stampGenerated marks every entity from a machine-generated file.
//
// Hand-written files — the overwhelming majority — return untouched, and in
// particular an entity that carried no Properties map does not gain an empty
// one. Several downstream checks are written as `len(e.Properties) == 0`, so
// allocating here would change what they see on every file in the repository.
func stampGenerated(file FileInput, entities []types.EntityRecord) []types.EntityRecord {
	if len(entities) == 0 {
		return entities
	}
	d := generated.Detect(file.Path, file.Content)
	if !d.Generated {
		return entities
	}
	for i := range entities {
		if entities[i].Properties == nil {
			entities[i].Properties = make(map[string]string, 2)
		}
		entities[i].Properties[types.EntityGeneratedProperty] = "true"
		entities[i].Properties[types.EntityGeneratedByProperty] = d.Rule
	}
	return entities
}

// List returns a sorted slice of all registered language names.
func List() []string {
	langs := extractor.List()
	sort.Strings(langs)
	return langs
}
