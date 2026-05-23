package react_props

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
)

// Issue #1964 — react_component entities emitted by this regex-based
// extractor MUST carry non-zero start_line / end_line so the docgen
// source_window helper (internal/docgen/llm_bundle.go) can produce a
// useful excerpt. Before the fix every component had StartLine=EndLine=0
// and the bundle source_window was entirely missing for JSX components.
func TestReactComponent_LineRangePopulated_Function(t *testing.T) {
	src := "" +
		"// header\n" +                            // line 1
		"import React from 'react';\n" +           // line 2
		"\n" +                                     // line 3
		"export function FixtureComponentA(props) {\n" + // line 4
		"  return <div>{props.label}</div>;\n" +   // line 5
		"}\n" +                                    // line 6
		""

	e := &Extractor{}
	out, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "fixture/component_a.jsx",
		Content:  []byte(src),
		Language: "javascript",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	var got *struct{ start, end int }
	for i := range out {
		if out[i].Subtype == "react_component" && out[i].Name == "FixtureComponentA" {
			got = &struct{ start, end int }{out[i].StartLine, out[i].EndLine}
			break
		}
	}
	if got == nil {
		t.Fatalf("no react_component entity emitted; out=%+v", out)
	}
	if got.start != 4 {
		t.Errorf("start_line: got %d, want 4", got.start)
	}
	if got.end != 6 {
		t.Errorf("end_line: got %d, want 6", got.end)
	}
}

// Same coverage for the arrow-function declaration shape (the React
// component idiom used by the W1R4 ContractProposals reproducer).
func TestReactComponent_LineRangePopulated_Arrow(t *testing.T) {
	src := "" +
		"import React from 'react';\n" +                    // line 1
		"\n" +                                              // line 2
		"export const FixtureComponentB = (props) => {\n" + // line 3
		"  const x = 1;\n" +                                // line 4
		"  return (\n" +                                    // line 5
		"    <span>{props.label}</span>\n" +                // line 6
		"  );\n" +                                          // line 7
		"};\n" +                                            // line 8
		""

	e := &Extractor{}
	out, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     "fixture/component_b.jsx",
		Content:  []byte(src),
		Language: "javascript",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	var got *struct{ start, end int }
	for i := range out {
		if out[i].Subtype == "react_component" && out[i].Name == "FixtureComponentB" {
			got = &struct{ start, end int }{out[i].StartLine, out[i].EndLine}
			break
		}
	}
	if got == nil {
		t.Fatalf("no react_component entity emitted; out=%+v", out)
	}
	if got.start != 3 {
		t.Errorf("start_line: got %d, want 3", got.start)
	}
	if got.end < 7 {
		// Closing brace may land on line 8; accept either.
		t.Errorf("end_line: got %d, want >= 7", got.end)
	}
}
