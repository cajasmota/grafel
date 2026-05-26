package css_test

import (
	"context"
	"os"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors/css"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Less extractor tests
// ---------------------------------------------------------------------------

func TestExtractLess_Variables(t *testing.T) {
	src := `
@primary-color: #3498db;
@secondary-color: #2ecc71;
@font-size-base: 16px;
@spacing-sm: 8px;
@border-radius: 4px;
`
	file := extractor.FileInput{
		Path:     "styles/theme.less",
		Content:  []byte(src),
		Language: "css",
	}

	var ents []types.EntityRecord
	css.ExtractLess(context.Background(), file, &ents)

	varNames := make(map[string]bool)
	for _, e := range ents {
		if e.Subtype == "variable" {
			varNames[e.Name] = true
		}
	}

	for _, want := range []string{"@primary-color", "@secondary-color", "@font-size-base", "@spacing-sm", "@border-radius"} {
		if !varNames[want] {
			t.Errorf("expected Less variable %q to be extracted", want)
		}
	}

	if len(ents) < 5 {
		t.Errorf("expected at least 5 variable entities, got %d", len(ents))
	}
}

func TestExtractLess_Mixins(t *testing.T) {
	src := `
.button-style(@bg: @primary-color; @color: #fff) {
  background-color: @bg;
  color: @color;
}

.clearfix() {
  &::after { content: ""; display: table; clear: both; }
}
`
	file := extractor.FileInput{
		Path:     "styles/mixins.less",
		Content:  []byte(src),
		Language: "css",
	}

	var ents []types.EntityRecord
	css.ExtractLess(context.Background(), file, &ents)

	mixinNames := make(map[string]bool)
	for _, e := range ents {
		if e.Subtype == "mixin" {
			mixinNames[e.Name] = true
		}
	}

	for _, want := range []string{"button-style", "clearfix"} {
		if !mixinNames[want] {
			t.Errorf("expected Less mixin %q to be extracted", want)
		}
	}
}

func TestExtractLess_AllEntityCounts(t *testing.T) {
	src := `
@primary-color: #3498db;
@secondary-color: #2ecc71;
@accent-color: #e74c3c;
@font-size-base: 16px;
@spacing-sm: 8px;

.button-style(@bg: @primary-color) {
  background: @bg;
}

.clearfix() {
  &::after { content: ""; }
}
`
	file := extractor.FileInput{
		Path:     "styles/all.less",
		Content:  []byte(src),
		Language: "css",
	}

	var ents []types.EntityRecord
	varCount, mixinCount, _ := css.ExtractLess(context.Background(), file, &ents)

	if varCount < 5 {
		t.Errorf("expected >= 5 variables, got %d", varCount)
	}
	if mixinCount < 2 {
		t.Errorf("expected >= 2 mixins, got %d", mixinCount)
	}
	total := varCount + mixinCount
	if total < 7 {
		t.Errorf("expected >= 7 total entities, got %d", total)
	}
}

func TestExtractLess_EntityFields(t *testing.T) {
	src := "@primary-color: #3498db;\n"
	file := extractor.FileInput{
		Path:     "theme.less",
		Content:  []byte(src),
		Language: "css",
	}

	var ents []types.EntityRecord
	css.ExtractLess(context.Background(), file, &ents)

	if len(ents) == 0 {
		t.Fatal("expected at least 1 entity")
	}
	e := ents[0]
	if e.Kind != "SCOPE.Component" {
		t.Errorf("Kind=%q want SCOPE.Component", e.Kind)
	}
	if e.Language != "less" {
		t.Errorf("Language=%q want less", e.Language)
	}
	if e.SourceFile != "theme.less" {
		t.Errorf("SourceFile=%q want theme.less", e.SourceFile)
	}
	if e.Name != "@primary-color" {
		t.Errorf("Name=%q want @primary-color", e.Name)
	}
	if e.Metadata == nil {
		t.Fatal("Metadata must not be nil")
	}
	if e.Metadata["kind"] != "variable" {
		t.Errorf("Metadata[kind]=%v want variable", e.Metadata["kind"])
	}
	if e.Metadata["value"] != "#3498db" {
		t.Errorf("Metadata[value]=%v want #3498db", e.Metadata["value"])
	}
}

func TestExtractLess_MixinParams(t *testing.T) {
	src := ".flex-center(@direction: row; @wrap: nowrap) {\n  display: flex;\n}\n"
	file := extractor.FileInput{
		Path:    "mixins.less",
		Content: []byte(src),
	}

	var ents []types.EntityRecord
	css.ExtractLess(context.Background(), file, &ents)

	if len(ents) == 0 {
		t.Fatal("expected at least 1 entity")
	}
	e := ents[0]
	if e.Subtype != "mixin" {
		t.Errorf("Subtype=%q want mixin", e.Subtype)
	}
	params, ok := e.Metadata["params"].([]string)
	if !ok {
		t.Fatalf("Metadata[params] is not []string: %T", e.Metadata["params"])
	}
	if len(params) != 2 {
		t.Errorf("expected 2 params, got %d: %v", len(params), params)
	}
}

func TestExtractLess_SkipsComments(t *testing.T) {
	src := "// @commented-var: #000;\n/* @also-commented: #fff; */\n@real-var: blue;\n"
	file := extractor.FileInput{
		Path:    "comments.less",
		Content: []byte(src),
	}

	var ents []types.EntityRecord
	varCount, _, _ := css.ExtractLess(context.Background(), file, &ents)

	if varCount != 1 {
		t.Errorf("expected 1 variable (skipping commented), got %d", varCount)
	}
	if len(ents) > 0 && ents[0].Name != "@real-var" {
		t.Errorf("expected @real-var, got %q", ents[0].Name)
	}
}

func TestExtractLess_SkipsAtRuleKeywords(t *testing.T) {
	// @media, @import, @charset, @keyframes should NOT be extracted as variables
	src := "@import \"base.less\";\n@primary-color: #3498db;\n"
	file := extractor.FileInput{
		Path:    "imports.less",
		Content: []byte(src),
	}

	var ents []types.EntityRecord
	varCount, _, _ := css.ExtractLess(context.Background(), file, &ents)

	// Only @primary-color should be extracted as a variable, not @import.
	// @import is now emitted as a separate "import" entity (issue #383),
	// so we check varCount and find the variable entity by Subtype.
	if varCount != 1 {
		t.Errorf("expected 1 variable (skipping @import), got %d", varCount)
	}
	var got string
	for _, e := range ents {
		if e.Subtype == "variable" {
			got = e.Name
			break
		}
	}
	if got != "@primary-color" {
		t.Errorf("expected @primary-color variable, got %q", got)
	}
}

func TestExtractLess_EmptyContent(t *testing.T) {
	file := extractor.FileInput{
		Path:    "empty.less",
		Content: []byte{},
	}

	var ents []types.EntityRecord
	varCount, mixinCount, _ := css.ExtractLess(context.Background(), file, &ents)

	if varCount != 0 || mixinCount != 0 {
		t.Errorf("expected all counts=0 for empty content, got %d/%d", varCount, mixinCount)
	}
}

func TestExtractLess_StartLine(t *testing.T) {
	src := "// comment line 1\n@primary-color: red;\n@secondary-color: blue;\n"
	file := extractor.FileInput{
		Path:    "lines.less",
		Content: []byte(src),
	}

	var ents []types.EntityRecord
	css.ExtractLess(context.Background(), file, &ents)

	if len(ents) < 2 {
		t.Fatalf("expected 2 entities, got %d", len(ents))
	}
	if ents[0].StartLine != 2 {
		t.Errorf("first variable start_line=%d want 2", ents[0].StartLine)
	}
	if ents[1].StartLine != 3 {
		t.Errorf("second variable start_line=%d want 3", ents[1].StartLine)
	}
}

func TestExtractLess_NoParamsMixin(t *testing.T) {
	src := ".clearfix() {\n  &::after { content: \"\"; display: table; clear: both; }\n}\n"
	file := extractor.FileInput{
		Path:    "clearfix.less",
		Content: []byte(src),
	}

	var ents []types.EntityRecord
	_, mixinCount, _ := css.ExtractLess(context.Background(), file, &ents)

	if mixinCount != 1 {
		t.Errorf("expected 1 mixin, got %d", mixinCount)
	}
	params, _ := ents[0].Metadata["params"].([]string)
	if len(params) != 0 {
		t.Errorf("expected empty params for no-arg mixin, got %v", params)
	}
}

func TestExtractLess_FixtureFile(t *testing.T) {
	content, err := os.ReadFile("../../../testdata/fixtures/sources/css/less/variables.less")
	if err != nil {
		t.Skipf("fixture not found: %v", err)
	}
	file := extractor.FileInput{
		Path:    "testdata/fixtures/sources/css/less/variables.less",
		Content: content,
	}

	var ents []types.EntityRecord
	varCount, mixinCount, _ := css.ExtractLess(context.Background(), file, &ents)

	if varCount < 5 {
		t.Errorf("expected >= 5 variables from fixture, got %d", varCount)
	}
	if mixinCount < 2 {
		t.Errorf("expected >= 2 mixins from fixture, got %d", mixinCount)
	}
	total := varCount + mixinCount
	if total < 7 {
		t.Errorf("expected >= 7 total entities from fixture, got %d", total)
	}
	// All entities must be SCOPE.Component
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" {
			t.Errorf("entity %q has Kind=%q want SCOPE.Component", e.Name, e.Kind)
		}
	}
}

func TestExtractLess_DispatchedViaExtractor(t *testing.T) {
	src := "@primary-color: #3498db;\n@secondary-color: blue;\n.btn(@bg: red) { }\n"
	ext, ok := extractor.Get("css")
	if !ok {
		t.Fatal("css extractor not registered")
	}

	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "theme.less",
		Content:  []byte(src),
		Language: "css",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) < 3 {
		t.Errorf("expected >= 3 entities from .less dispatch, got %d", len(ents))
	}
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" {
			t.Errorf("entity %q: Kind=%q want SCOPE.Component", e.Name, e.Kind)
		}
	}
}
