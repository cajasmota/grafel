package javascript_test

import (
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func jsTransEdge(recs []types.EntityRecord, fromName, key string) bool {
	want := extreg.TranslationKeyTargetID(key)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == string(types.RelationshipKindUsesTranslation) && r.ToID == want {
				return true
			}
		}
	}
	return false
}

func jsTransNode(recs []types.EntityRecord, key string) (string, int) {
	want := extreg.TranslationKeyName(key)
	id, count := "", 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTranslationKey) && recs[i].Name == want {
			id = recs[i].ID
			count++
		}
	}
	return id, count
}

func jsAnyTransNode(recs []types.EntityRecord) bool {
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTranslationKey) {
			return true
		}
	}
	return false
}

// react-i18next: `const { t } = useTranslation(); t('errors.notFound')` →
// USES_TRANSLATION(Login -> i18n:errors.notFound). Asserts node id + edge.
func TestJSTrans_ReactI18next(t *testing.T) {
	src := []byte(`import { useTranslation } from "react-i18next";

function Login() {
  const { t } = useTranslation();
  return t("errors.notFound");
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsTransEdge(recs, "Login", "errors.notFound") {
		t.Fatalf("missing USES_TRANSLATION(Login -> errors.notFound)")
	}
	if id, n := jsTransNode(recs, "errors.notFound"); id == "" || n != 1 {
		t.Errorf("expected exactly 1 errors.notFound node, got id=%q n=%d", id, n)
	}
}

// i18next: `i18n.t('x')` — explicit receiver is the context (no import needed).
func TestJSTrans_I18nReceiver(t *testing.T) {
	src := []byte(`function greet() {
  return i18n.t("home.greeting");
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsTransEdge(recs, "greet", "home.greeting") {
		t.Fatalf("missing USES_TRANSLATION(greet -> home.greeting)")
	}
}

// <Trans i18nKey="x"> JSX attribute.
func TestJSTrans_TransComponent(t *testing.T) {
	src := []byte(`import { Trans } from "react-i18next";

function Banner() {
  return <Trans i18nKey="banner.title">Hello</Trans>;
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsTransEdge(recs, "Banner", "banner.title") {
		t.Fatalf("missing USES_TRANSLATION(Banner -> banner.title)")
	}
}

// vue-i18n: `$t('x')`.
func TestJSTrans_VueI18n(t *testing.T) {
	src := []byte(`function render() {
  return $t("nav.home");
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if !jsTransEdge(recs, "render", "nav.home") {
		t.Fatalf("missing USES_TRANSLATION(render -> nav.home)")
	}
}

// Convergence: the SAME key referenced in two components collapses to ONE node,
// with an edge from each caller.
func TestJSTrans_Convergence(t *testing.T) {
	src := []byte(`import { useTranslation } from "react-i18next";

function A() {
  const { t } = useTranslation();
  return t("shared.label");
}
function B() {
  const { t } = useTranslation();
  return t("shared.label");
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if id, n := jsTransNode(recs, "shared.label"); id == "" || n != 1 {
		t.Fatalf("expected exactly 1 shared.label node, got id=%q n=%d", id, n)
	}
	if !jsTransEdge(recs, "A", "shared.label") {
		t.Errorf("missing USES_TRANSLATION(A -> shared.label)")
	}
	if !jsTransEdge(recs, "B", "shared.label") {
		t.Errorf("missing USES_TRANSLATION(B -> shared.label)")
	}
}

// Negative: bare `t('x')` with NO i18n import in the file → no node/edge (could
// be a local helper named t).
func TestJSTrans_BareTNoImport_Skipped(t *testing.T) {
	src := []byte(`function doThing() {
  return t("not.i18n");
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if jsAnyTransNode(recs) {
		t.Fatalf("bare t() with no i18n import should NOT create a translation node")
	}
}

// Negative: dynamic key `t(keyVar)` → no node/edge.
func TestJSTrans_DynamicKey_Skipped(t *testing.T) {
	src := []byte(`import { useTranslation } from "react-i18next";

function Dyn(keyVar) {
  const { t } = useTranslation();
  return t(keyVar);
}
`)
	recs := extract(t, src, "javascript", parseJS(t, src))
	if jsAnyTransNode(recs) {
		t.Fatalf("dynamic key t(keyVar) should NOT create a translation node")
	}
}

// Negative: interpolated template `t(`a.${x}`)` → no node/edge.
func TestJSTrans_InterpolatedKey_Skipped(t *testing.T) {
	src := []byte("import { useTranslation } from \"react-i18next\";\n\nfunction Dyn(x) {\n  const { t } = useTranslation();\n  return t(`a.${x}`);\n}\n")
	recs := extract(t, src, "javascript", parseJS(t, src))
	if jsAnyTransNode(recs) {
		t.Fatalf("interpolated key should NOT create a translation node")
	}
}
