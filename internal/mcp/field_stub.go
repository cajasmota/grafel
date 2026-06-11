// Field-level partial-stub signal for stub_detector (#4669, epic #4493).
//
// stub_detector is endpoint-level: it contrasts the v3 endpoint's side-effect
// profile against its oracle counterpart, so an endpoint with SOME db_read
// reads as "implemented" even when individual response FIELDS are hardcoded
// (GET /clients/get_extras cat1/cat5 #763; checklists part_id:null #831). This
// file adds the complementary field-level signal: for a v3 endpoint's
// handler(s), classify each constructed response-object field as derived vs
// literal-bound and surface the UNCONDITIONALLY literal-bound DATA fields as
// `partial_stub_fields`. It COMPLEMENTS the endpoint verdict — a fully
// "implemented" endpoint can still carry partial-stub fields.
//
// The literal-vs-derived classification is the per-language substrate analyzer
// (internal/substrate/field_literals*.go), fed the same single-function source
// window the effects/branches facets walk. Python (DRF) + JS/TS (NestJS) ship;
// other languages register their own analyzer (honest-partial otherwise).
package mcp

import (
	"path/filepath"

	"github.com/cajasmota/archigraph/internal/graph"
	"github.com/cajasmota/archigraph/internal/stubdetector"
	"github.com/cajasmota/archigraph/internal/substrate"
)

// partialStubFieldsForEndpoint computes the partial-stub fields for a v3
// endpoint definition by analysing the source of every handler that implements
// it. It unions each handler's per-object field facets and runs the
// language-general PartialStubFields roll-up (a field flagged only when EVERY
// occurrence across all handlers is literal-bound, and not an envelope flag).
//
// supported reports whether ANY analysed handler's language has a registered
// field analyzer — so the tool can honest-partial (a v3 stack with no analyzer
// is "unknown", never "no literal fields"). fields is nil/empty when supported
// but nothing is unconditionally literal.
func partialStubFieldsForEndpoint(
	r *LoadedRepo,
	def *graph.Entity,
	hres *stubHandlerResolution,
	byID map[string]*graph.Entity,
) (fields []stubdetector.PartialStubField, supported bool) {
	roots := hres.resolveStubHandlers(def)
	var all []substrate.FieldFacet
	for _, id := range roots {
		h := byID[id]
		if h == nil {
			continue
		}
		lang := substrate.LanguageForPath(h.SourceFile)
		analyzer := substrate.FieldLiteralAnalyzerFor(lang)
		if analyzer == nil {
			continue
		}
		supported = true
		src := readHandlerSource(r, h)
		if src == "" {
			continue
		}
		all = append(all, analyzer(src, handlerStartLine(h))...)
	}
	if !supported {
		return nil, false
	}
	flagged := substrate.PartialStubFields(all)
	out := make([]stubdetector.PartialStubField, 0, len(flagged))
	for _, f := range flagged {
		out = append(out, stubdetector.PartialStubField{
			Field:        f.Field,
			LiteralValue: f.LiteralValue,
			Line:         f.Line,
		})
	}
	return out, true
}

// readHandlerSource reads a handler entity's source window from disk, mirroring
// attachBranchesFacet's resolution (abs path under repo, degenerate-span pad).
func readHandlerSource(r *LoadedRepo, h *graph.Entity) string {
	start, end := branchSourceSpan(h)
	if start <= 0 {
		return ""
	}
	abs := h.SourceFile
	if !filepath.IsAbs(abs) && r != nil && r.Path != "" {
		abs = filepath.Join(r.Path, h.SourceFile)
	}
	src, err := readRawSourceWindow(abs, start, end)
	if err != nil {
		return ""
	}
	return src
}

func handlerStartLine(h *graph.Entity) int {
	if h.StartLine > 0 {
		return h.StartLine
	}
	return 1
}

// partialStubFieldsToJSON renders the flagged fields into the public JSON shape
// surfaced on each stub_detector endpoint record.
func partialStubFieldsToJSON(fields []stubdetector.PartialStubField) []map[string]any {
	out := make([]map[string]any, 0, len(fields))
	for _, f := range fields {
		m := map[string]any{
			"field":         f.Field,
			"literal_value": f.LiteralValue,
			"line":          f.Line,
		}
		out = append(out, m)
	}
	return out
}
