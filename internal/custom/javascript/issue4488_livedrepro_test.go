package javascript_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

// Issue #4488 LIVE-REPRO — response DTO shape resolution + void labeling.
//
// Byte-copies of REAL core-backend-v3 files are committed under
// testdata/issue4488:
//
//   - inspector.controller.ts — a NestJS controller mixing
//     `Promise<InspectorResponse>` (plain DTO), `Promise<PaginatedInspectorResponse>`
//     (a results-list DTO) and `Promise<void>` (the DELETE/204 handler).
//   - group.controller.ts     — has a real envelope return
//     `Promise<PagedResponse<GroupResponse>>` that must unwrap to GroupResponse.
//
// PRE-FIX: the NestJS extractor set `response_type` only when nestUnwrapType
// returned a bare DTO. Envelope wrappers (PagedResponse<T>) kept the envelope
// name (or resolved to an envelope class with no DTO fields), so the Paths
// Response row counted a response but rendered "(none)". A `Promise<void>`
// handler set NO `response_type` at all, yet the verb still produced a counted
// Response shape — the misleading "Response (1) (none)".
//
// POST-FIX: nestResolveResponseType strips Promise/Observable, descends through
// envelopes (PagedResponse/ApiResponse/{ data: T }), flags arrays, and reports
// genuine void. The extractor stamps `response_type` (unwrapped DTO),
// `response_is_array`, and `response_void` so the dashboard renders the real
// DTO shape or an explicit "204 No Content" — never a silent "(none)".

func nestExtract4488(t *testing.T, base string) []types.EntityRecord {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "issue4488", base))
	if err != nil {
		t.Fatalf("read %s: %v", base, err)
	}
	e, ok := extreg.Get("custom_js_nestjs")
	if !ok {
		t.Fatal("custom_js_nestjs not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: "src/" + base, Language: "typescript", Content: b})
	if err != nil {
		t.Fatalf("nest extract %s: %v", base, err)
	}
	return ents
}

// endpointByReturns finds the http-endpoint entity whose RETURNS edge targets
// Class:<dto>; falls back to matching on the response_type property.
func endpointByResponseType(ents []types.EntityRecord, dto string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Properties["response_type"] == dto {
			return &ents[i]
		}
	}
	return nil
}

func TestIssue4488_ResponseShapeResolution(t *testing.T) {
	ents := nestExtract4488(t, "inspector.controller.ts")

	// 1) Plain `Promise<InspectorResponse>` resolves to the DTO.
	if ep := endpointByResponseType(ents, "InspectorResponse"); ep == nil {
		t.Fatalf("PRE-FIX repro: no endpoint stamped response_type=InspectorResponse\n%s",
			dumpResponseProps(ents))
	} else {
		if ep.Properties["response_void"] == "true" {
			t.Errorf("InspectorResponse endpoint wrongly marked void")
		}
		if ep.Properties["response_is_array"] == "true" {
			t.Errorf("InspectorResponse endpoint wrongly marked array")
		}
	}

	// 2) `Promise<PaginatedInspectorResponse>` resolves (a results-carrying DTO,
	//    not an envelope in our set — kept as the DTO so its fields render).
	if ep := endpointByResponseType(ents, "PaginatedInspectorResponse"); ep == nil {
		t.Errorf("PaginatedInspectorResponse not resolved\n%s", dumpResponseProps(ents))
	}

	// 3) `Promise<void>` (the @Delete 204 handler) is labeled void, NOT a
	//    "(none)" typed response.
	var voidSeen bool
	for i := range ents {
		if ents[i].Properties["response_void"] == "true" {
			voidSeen = true
			if ents[i].Properties["response_type"] != "" {
				t.Errorf("void endpoint also stamped response_type=%q",
					ents[i].Properties["response_type"])
			}
		}
	}
	if !voidSeen {
		t.Fatalf("PRE-FIX repro: Promise<void> DELETE handler not labeled void\n%s",
			dumpResponseProps(ents))
	}
}

func TestIssue4488_EnvelopeUnwrap(t *testing.T) {
	ents := nestExtract4488(t, "group.controller.ts")

	// `Promise<PagedResponse<GroupResponse>>` must unwrap PAST the envelope to
	// the payload DTO GroupResponse (the envelope itself has no DTO fields →
	// would render "(none)").
	if ep := endpointByResponseType(ents, "GroupResponse"); ep == nil {
		t.Fatalf("PRE-FIX repro: PagedResponse<GroupResponse> did not unwrap to GroupResponse\n%s",
			dumpResponseProps(ents))
	}
	// Ensure the raw envelope name never leaks as a response_type.
	if ep := endpointByResponseType(ents, "PagedResponse"); ep != nil {
		t.Errorf("envelope PagedResponse leaked as response_type (should unwrap to payload)")
	}
}

func dumpResponseProps(ents []types.EntityRecord) string {
	out := "response props:\n"
	for i := range ents {
		rt := ents[i].Properties["response_type"]
		rv := ents[i].Properties["response_void"]
		ra := ents[i].Properties["response_is_array"]
		if rt != "" || rv != "" || ra != "" {
			out += "  " + ents[i].Name + " response_type=" + rt +
				" void=" + rv + " array=" + ra + "\n"
		}
	}
	return out
}
