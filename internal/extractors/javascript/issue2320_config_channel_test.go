// Package javascript_test — issue #2320 Config-channel tests for the JS/TS extractor.
//
// Each toggle is tested with three sub-cases:
//  1. Config-only set  → behavior follows Config (env var cleared)
//  2. Env-var-only set → behavior follows env (Config nil — backward compat)
//  3. Both set         → Config wins (env var contradicts Config)
//
// A fourth case checks the default (nil Config + unset env → documented default).
package javascript_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tsjavascript "github.com/smacker/go-tree-sitter/javascript"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
)

// parseJSForConfig is a local parse helper so this file is self-contained.
func parseJSForConfig(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tsjavascript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parseJSForConfig: %v", err)
	}
	return tree
}

// extractWithCfg runs the JS extractor with an explicit config and returns entities.
func extractWithCfg(t *testing.T, src []byte, cfg *extreg.ExtractorConfig) []entitySubtype {
	t.Helper()
	tree := parseJSForConfig(t, src)
	e := javascript.New()
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     "test.js",
		Content:  src,
		Language: "javascript",
		TSTree:   tree,
		Config:   cfg,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	out := make([]entitySubtype, len(ents))
	for i, en := range ents {
		out[i] = entitySubtype{name: en.Name, subtype: en.Subtype}
	}
	return out
}

type entitySubtype struct {
	name    string
	subtype string
}

func hasSubtype(ents []entitySubtype, name, subtype string) bool {
	for _, e := range ents {
		if e.name == name && e.subtype == subtype {
			return true
		}
	}
	return false
}

// destructureSrc has one plain-hook destructure and one mutation-hook destructure.
const destructureSrc2320 = `
const { data, isLoading } = useFooQuery();
const { mutate: createFoo } = useCreateFoo();
`

// ---------------------------------------------------------------------------
// JSEmitDestructureDetail — Config-only on
// ---------------------------------------------------------------------------

func TestDestructureConfig_ConfigOnly_On(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_DESTRUCTURE_DETAIL", "") // env off
	on := true
	cfg := &extreg.ExtractorConfig{JSEmitDestructureDetail: &on}

	ents := extractWithCfg(t, []byte(destructureSrc2320), cfg)

	// Config says on → const_destructure subtypes must appear.
	if !hasSubtype(ents, "data", "const_destructure") && !hasSubtype(ents, "isLoading", "const_destructure") {
		t.Error("Config-only on: expected const_destructure subtype for plain-hook binding; not found")
	}
}

// ---------------------------------------------------------------------------
// JSEmitDestructureDetail — Config-only off
// ---------------------------------------------------------------------------

func TestDestructureConfig_ConfigOnly_Off(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_DESTRUCTURE_DETAIL", "") // env also off
	off := false
	cfg := &extreg.ExtractorConfig{JSEmitDestructureDetail: &off}

	ents := extractWithCfg(t, []byte(destructureSrc2320), cfg)

	for _, e := range ents {
		if e.subtype == "const_destructure" || e.subtype == "const_destructure_call" {
			t.Errorf("Config-only off: unexpected subtype %q on entity %q", e.subtype, e.name)
		}
	}
}

// ---------------------------------------------------------------------------
// JSEmitDestructureDetail — env-only (backward compat; Config nil)
// ---------------------------------------------------------------------------

func TestDestructureConfig_EnvOnly(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_DESTRUCTURE_DETAIL", "1") // env on
	// nil Config → pure env-var path
	ents := extractWithCfg(t, []byte(destructureSrc2320), nil)

	found := false
	for _, e := range ents {
		if e.subtype == "const_destructure" || e.subtype == "const_destructure_call" {
			found = true
			break
		}
	}
	if !found {
		t.Error("env-only: expected const_destructure* subtype with GRAFEL_EMIT_DESTRUCTURE_DETAIL=1 and nil Config; none found")
	}
}

// ---------------------------------------------------------------------------
// JSEmitDestructureDetail — Config wins over env
// ---------------------------------------------------------------------------

func TestDestructureConfig_ConfigWins(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_DESTRUCTURE_DETAIL", "1") // env says on
	off := false
	cfg := &extreg.ExtractorConfig{JSEmitDestructureDetail: &off} // Config says off

	ents := extractWithCfg(t, []byte(destructureSrc2320), cfg)

	for _, e := range ents {
		if e.subtype == "const_destructure" || e.subtype == "const_destructure_call" {
			t.Errorf("Config-wins: Config=off should suppress destructure detail even when env=1; got subtype %q on %q", e.subtype, e.name)
		}
	}
}

// ---------------------------------------------------------------------------
// JSEmitDestructureDetail — nil Config + unset env → default off
// ---------------------------------------------------------------------------

func TestDestructureConfig_NilConfig_EnvUnset(t *testing.T) {
	t.Setenv("GRAFEL_EMIT_DESTRUCTURE_DETAIL", "")
	ents := extractWithCfg(t, []byte(destructureSrc2320), nil)

	for _, e := range ents {
		if e.subtype == "const_destructure" || e.subtype == "const_destructure_call" {
			t.Errorf("nil Config + unset env: default should be off; got subtype %q on %q", e.subtype, e.name)
		}
	}
}
