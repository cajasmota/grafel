package references

import (
	"context"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// Wrap returns an extractor.Extractor that first runs base (the
// language extractor) and then runs a ReferenceExtractor on the same
// FileInput, concatenating the two output slices. Errors from the
// reference phase are logged but never cause the combined extractor to
// fail — the declaration output is always preserved so a malformed
// reference pass can never block indexing (matches the "return partial
// results" contract enforced by the dispatch layer in
// internal/extractors/registry.go).
//
// Wrap is the recommended integration point for the pipeline: it lets
// a single line in the registry or pipeline wiring opt a language in
// to reference extraction without any edits inside the language
// extractor package itself.
//
//	extractor.Register("go", references.Wrap(&golang.GoExtractor{}, refExt))
//
// The above line would be the responsibility of the pipeline owner
// (MX-1044) — this package does not register anything itself.
func Wrap(base extractor.Extractor, refs *ReferenceExtractor) extractor.Extractor {
	if refs == nil {
		refs = NewReferenceExtractor()
	}
	return &wrappedExtractor{base: base, refs: refs}
}

type wrappedExtractor struct {
	base extractor.Extractor
	refs *ReferenceExtractor
}

// Language returns the underlying language name so the wrapped
// extractor is transparent to the registry dispatch layer.
func (w *wrappedExtractor) Language() string {
	if w.base == nil {
		return ""
	}
	return w.base.Language()
}

// Extract runs the base extractor and the reference extractor and
// returns the concatenated slice. The reference extractor's error is
// dropped — callers that need the error should call ReferenceExtractor
// directly instead of through Wrap.
func (w *wrappedExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	var decls []types.EntityRecord
	var err error
	if w.base != nil {
		decls, err = w.base.Extract(ctx, file)
	}
	// Reference phase runs regardless of the base extractor's error
	// state so we never lose reference output to a transient failure
	// upstream. Errors from the reference phase are logged inside
	// ReferenceExtractor.Extract (via the OTel span) and dropped here.
	refs, _ := w.refs.Extract(ctx, file)
	if len(refs) > 0 {
		decls = append(decls, refs...)
	}
	return decls, err
}
