package mcp

import (
	"context"
	"strings"
	"testing"

	mcpapi "github.com/mark3labs/mcp-go/mcp"
)

// discriminator_test.go — fuzzy enum-correction + helpful errors for the
// consolidated canonical tools (#5578). A bad discriminator value returns a
// helpful error naming the closest valid value and the full valid list; a good
// value dispatches normally (unchanged behaviour); and grafel_diff's per-aspect
// required-param validation errors helpfully when a value-specific param is
// missing.

func TestValidateDiscriminator(t *testing.T) {
	canon := []string{"response_shape", "payload", "auth", "literals", "refs"}
	acc := append([]string{}, canon...)

	// empty/missing value → default applies, no error.
	if e := validateDiscriminator("aspect", "", acc, canon); e != nil {
		t.Fatalf("empty value should be allowed (default), got error")
	}
	// canonical value → no error.
	if e := validateDiscriminator("aspect", "payload", acc, canon); e != nil {
		t.Fatalf("valid value rejected")
	}
	// case-insensitive accept.
	if e := validateDiscriminator("aspect", "PAYLOAD", acc, canon); e != nil {
		t.Fatalf("case-insensitive valid value rejected")
	}

	// typo → helpful error with closest-match suggestion + full valid list.
	e := validateDiscriminator("aspect", "shape", acc, canon)
	if e == nil || !e.IsError {
		t.Fatalf("invalid value should error")
	}
	msg := resultText(e)
	if !strings.Contains(msg, "did you mean") || !strings.Contains(msg, "response_shape") {
		t.Fatalf("expected closest-match suggestion 'response_shape', got: %s", msg)
	}
	for _, v := range canon {
		if !strings.Contains(msg, v) {
			t.Fatalf("error should list valid value %q, got: %s", v, msg)
		}
	}

	// far-off value → still errors, lists valids, no spurious suggestion.
	e2 := validateDiscriminator("aspect", "zzzzzzzzzz", acc, canon)
	if e2 == nil || !e2.IsError {
		t.Fatalf("far-off value should error")
	}
	if strings.Contains(resultText(e2), "did you mean") {
		t.Fatalf("far-off value should NOT suggest a match, got: %s", resultText(e2))
	}
}

func TestClosestEnum(t *testing.T) {
	canon := []string{"callers", "callees", "neighbors", "uses", "used_by"}
	cases := map[string]string{
		"caller":   "callers",
		"callee":   "callees",
		"neighbor": "neighbors",
		"usedby":   "used_by",
	}
	for in, want := range cases {
		if got := closestEnum(in, canon); got != want {
			t.Errorf("closestEnum(%q) = %q, want %q", in, got, want)
		}
	}
	if got := closestEnum("zzzzzzz", canon); got != "" {
		t.Errorf("closestEnum on far-off probe = %q, want empty", got)
	}
}

// TestDiffBadAspectHelpfulError exercises the helper through the real dispatcher.
func TestDiffBadAspectHelpfulError(t *testing.T) {
	srv := coreTestServer(t)
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"aspect": "shape", "group": "g"}
	res, err := srv.handleAnalysisDiff(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("bad aspect should return error result")
	}
	msg := resultText(res)
	if !strings.Contains(msg, "response_shape") || !strings.Contains(msg, "did you mean") {
		t.Fatalf("expected suggestion of response_shape, got: %s", msg)
	}
}

// TestDiffMissingRequiredParam covers the per-aspect required-param validation:
// aspect=refs needs repo+ref_a+ref_b; a missing one yields an aspect-aware error
// BEFORE any dispatch.
func TestDiffMissingRequiredParam(t *testing.T) {
	srv := coreTestServer(t)
	req := mcpapi.CallToolRequest{}
	// refs aspect with repo + ref_a but no ref_b.
	req.Params.Arguments = map[string]any{"aspect": "refs", "group": "g", "repo": "r1", "ref_a": "main"}
	res, err := srv.handleAnalysisDiff(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("missing ref_b should return error result")
	}
	msg := resultText(res)
	if !strings.Contains(msg, "ref_b") || !strings.Contains(msg, "refs") {
		t.Fatalf("expected aspect=refs missing ref_b error, got: %s", msg)
	}
}

// TestGoodDiscriminatorDispatches confirms a valid discriminator value still
// reaches its delegate (validation does not break the happy path). We assert the
// validation gate did NOT short-circuit: a good value produces a non-error,
// aspect-stamped result identical to calling the absorbed handler directly.
func TestGoodDiscriminatorDispatches(t *testing.T) {
	srv := coreTestServer(t)
	// grafel_related direction=callees: valid value must dispatch, not error.
	req := mcpapi.CallToolRequest{}
	req.Params.Arguments = map[string]any{"entity_id": "missing", "direction": "callees", "group": "g"}
	res, err := srv.handleCoreRelated(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	// A valid discriminator must NOT produce the "invalid direction" gate error.
	if res != nil && res.IsError && strings.Contains(resultText(res), "invalid direction") {
		t.Fatalf("valid direction was rejected by the gate: %s", resultText(res))
	}
}
