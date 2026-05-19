package audit

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func TestClassifyOrphan_ImportPlaceholder(t *testing.T) {
	e := &graph.Entity{Kind: "SCOPE.Component", Subtype: "import", Name: "x"}
	if got := ClassifyOrphan(e); got != CauseImportPlaceholder {
		t.Errorf("want %s, got %s", CauseImportPlaceholder, got)
	}
}

func TestClassifyOrphan_ConstNoReferences(t *testing.T) {
	cases := []*graph.Entity{
		{Kind: "Operation", Subtype: "const_call", Name: "foo"},
		{Kind: "Component", Subtype: "const_destructure", Name: "bar"},
		{Kind: "Operation", Subtype: "const", Name: "baz"},
	}
	for _, e := range cases {
		if got := ClassifyOrphan(e); got != CauseConstNoReferences {
			t.Errorf("%+v: want %s, got %s", e, CauseConstNoReferences, got)
		}
	}
}

func TestClassifyOrphan_FrameworkSynthetic(t *testing.T) {
	cases := []*graph.Entity{
		{Kind: "manifest:nextjs", Name: "/api/foo"},
		{Kind: "hierarchy:django", Name: "App"},
		{Kind: "http_endpoint", Name: "GET /users"},
		{Kind: "dbmap:postgres", Name: "users"},
	}
	for _, e := range cases {
		if got := ClassifyOrphan(e); got != CauseFrameworkSynth {
			t.Errorf("%+v: want %s, got %s", e, CauseFrameworkSynth, got)
		}
	}
}

func TestClassifyOrphan_RealConstructBug(t *testing.T) {
	cases := []*graph.Entity{
		{Kind: "Class", Name: "PaymentService"},
		{Kind: "Function", Name: "processPayment"},
		{Kind: "Method", Name: "handle"},
		{Kind: "Interface", Name: "Handler"},
	}
	for _, e := range cases {
		if got := ClassifyOrphan(e); got != CauseRealConstructBug {
			t.Errorf("%+v: want %s, got %s", e, CauseRealConstructBug, got)
		}
	}
}

func TestClassifyOrphan_CrossFileExport(t *testing.T) {
	// PascalCase non-construct kind => export candidate.
	e := &graph.Entity{Kind: "Variable", Subtype: "let", Name: "DefaultTheme"}
	if got := ClassifyOrphan(e); got != CauseCrossFileExport {
		t.Errorf("want %s, got %s", CauseCrossFileExport, got)
	}
	// camelCase too.
	e2 := &graph.Entity{Kind: "Variable", Subtype: "let", Name: "useTheme"}
	if got := ClassifyOrphan(e2); got != CauseCrossFileExport {
		t.Errorf("want %s, got %s", CauseCrossFileExport, got)
	}
}

func TestClassifyOrphan_Misc(t *testing.T) {
	e := &graph.Entity{Kind: "Variable", Subtype: "tmp", Name: "x"}
	if got := ClassifyOrphan(e); got != CauseMisc {
		t.Errorf("want %s, got %s", CauseMisc, got)
	}
}

func TestClassifyImportToID(t *testing.T) {
	cases := map[string]ImportFormat{
		"0123456789abcdef":      ImportFormatHex,
		"ext:react:useState":    ImportFormatExtQualified,
		"ext:antd":              ImportFormatExtBare,
		"./constants":           ImportFormatPathString,
		"../../util":            ImportFormatPathString,
		"/abs/path":             ImportFormatPathString,
		"raw_module_name":       ImportFormatOther,
		"PascalNoPrefix":        ImportFormatOther,
		"0123456789ABCDEF":      ImportFormatOther, // uppercase hex isn't our canonical id
		"short":                 ImportFormatOther,
	}
	for in, want := range cases {
		if got := classifyImportToID(in); got != want {
			t.Errorf("%q: want %s, got %s", in, want, got)
		}
	}
}

func TestIsExportishName(t *testing.T) {
	if !isExportishName("UserCard") {
		t.Error("UserCard should be exportish")
	}
	if !isExportishName("useTheme") {
		t.Error("useTheme should be exportish")
	}
	if isExportishName("MAX_RETRIES") {
		t.Error("MAX_RETRIES is pure upper, not exportish")
	}
	if isExportishName("foo") {
		t.Error("pure lowercase is not exportish")
	}
	if isExportishName("") {
		t.Error("empty is not exportish")
	}
	if isExportishName("_internal") {
		t.Error("underscore-prefixed is not exportish")
	}
}
