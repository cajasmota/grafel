package python_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func transEdge(recs []types.EntityRecord, fromName, key string) bool {
	want := extractor.TranslationKeyTargetID(key)
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

func transNode(recs []types.EntityRecord, key string) (string, int) {
	want := extractor.TranslationKeyName(key)
	id, count := "", 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTranslationKey) && recs[i].Name == want {
			id = recs[i].ID
			count++
		}
	}
	return id, count
}

func anyTransNode(recs []types.EntityRecord) bool {
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTranslationKey) {
			return true
		}
	}
	return false
}

// Django/gettext: `_('Welcome')` (gettext aliased to _) in `home` →
// USES_TRANSLATION(home -> i18n:Welcome). Asserts node id + edge.
func TestPyTrans_DjangoGettextUnderscore(t *testing.T) {
	src := `from django.utils.translation import gettext as _

def home(request):
    return _('Welcome')
`
	recs := extractPy(t, src, "views.py")
	if !transEdge(recs, "home", "Welcome") {
		t.Fatalf("missing USES_TRANSLATION(home -> Welcome)")
	}
	if id, n := transNode(recs, "Welcome"); id == "" || n != 1 {
		t.Errorf("expected exactly 1 Welcome node, got id=%q n=%d", id, n)
	}
}

// gettext_lazy('Sign in').
func TestPyTrans_GettextLazy(t *testing.T) {
	src := `from django.utils.translation import gettext_lazy

LABEL = gettext_lazy('Sign in')
`
	recs := extractPy(t, src, "forms.py")
	// Module-scope key attaches to the file entity; assert the node exists.
	if id, n := transNode(recs, "Sign in"); id == "" || n != 1 {
		t.Fatalf("expected exactly 1 'Sign in' node, got id=%q n=%d", id, n)
	}
}

// `import gettext; gettext.gettext('Hello')`.
func TestPyTrans_GettextModuleAttr(t *testing.T) {
	src := `import gettext

def greet():
    return gettext.gettext('Hello')
`
	recs := extractPy(t, src, "g.py")
	if !transEdge(recs, "greet", "Hello") {
		t.Fatalf("missing USES_TRANSLATION(greet -> Hello)")
	}
}

// Convergence: same key in two functions → one node, two edges.
func TestPyTrans_Convergence(t *testing.T) {
	src := `from django.utils.translation import gettext as _

def a():
    return _('shared')

def b():
    return _('shared')
`
	recs := extractPy(t, src, "v.py")
	if id, n := transNode(recs, "shared"); id == "" || n != 1 {
		t.Fatalf("expected exactly 1 'shared' node, got id=%q n=%d", id, n)
	}
	if !transEdge(recs, "a", "shared") || !transEdge(recs, "b", "shared") {
		t.Errorf("expected USES_TRANSLATION from both a and b")
	}
}

// Negative: a non-i18n `_('x')` — `_` is NOT imported from a gettext source
// (e.g. a local placeholder / lodash-style underscore) → no node/edge.
func TestPyTrans_NonI18nUnderscore_Skipped(t *testing.T) {
	src := `def handler():
    _ = compute()
    return _('not.i18n')
`
	recs := extractPy(t, src, "h.py")
	if anyTransNode(recs) {
		t.Fatalf("bare _('x') with no gettext import should NOT create a translation node")
	}
}

// Negative: dynamic key `_(msg)` → no node/edge.
func TestPyTrans_DynamicKey_Skipped(t *testing.T) {
	src := `from django.utils.translation import gettext as _

def dyn(msg):
    return _(msg)
`
	recs := extractPy(t, src, "d.py")
	if anyTransNode(recs) {
		t.Fatalf("dynamic key _(msg) should NOT create a translation node")
	}
}
