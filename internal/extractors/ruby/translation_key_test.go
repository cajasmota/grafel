package ruby_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func rbTransEdge(recs []types.EntityRecord, fromName, key string) bool {
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

func rbTransNode(recs []types.EntityRecord, key string) (string, int) {
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

func rbAnyTransNode(recs []types.EntityRecord) bool {
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTranslationKey) {
			return true
		}
	}
	return false
}

// Rails: `I18n.t('users.title')` in `title` →
// USES_TRANSLATION(title -> i18n:users.title). Asserts node id + edge.
func TestRubyTrans_I18nT(t *testing.T) {
	src := `class UsersController
  def title
    I18n.t('users.title')
  end
end
`
	recs := extractRubyRecords(t, src)
	if !rbTransEdge(recs, "title", "users.title") {
		t.Fatalf("missing USES_TRANSLATION(title -> users.title)")
	}
	if id, n := rbTransNode(recs, "users.title"); id == "" || n != 1 {
		t.Errorf("expected exactly 1 users.title node, got id=%q n=%d", id, n)
	}
}

// I18n.translate alias.
func TestRubyTrans_I18nTranslate(t *testing.T) {
	src := `class A
  def show
    I18n.translate('a.b.c')
  end
end
`
	recs := extractRubyRecords(t, src)
	if !rbTransEdge(recs, "show", "a.b.c") {
		t.Fatalf("missing USES_TRANSLATION(show -> a.b.c)")
	}
}

// Relative key: `t('.title')` (leading dot is the unambiguous Rails i18n form).
func TestRubyTrans_RelativeKey(t *testing.T) {
	src := `class B
  def edit
    t('.title')
  end
end
`
	recs := extractRubyRecords(t, src)
	if !rbTransEdge(recs, "edit", ".title") {
		t.Fatalf("missing USES_TRANSLATION(edit -> .title)")
	}
	id, _ := rbTransNode(recs, ".title")
	if id == "" {
		t.Errorf("expected a .title node")
	}
}

// Negative: bare `t('plain')` with NO receiver and NO leading dot is ambiguous
// (could be a local method) → no node/edge.
func TestRubyTrans_BarePlain_Skipped(t *testing.T) {
	src := `class C
  def index
    t('plain')
  end
end
`
	recs := extractRubyRecords(t, src)
	if rbAnyTransNode(recs) {
		t.Fatalf("ambiguous bare t('plain') should NOT create a translation node")
	}
}

// Negative: dynamic key `I18n.t(key)` → no node/edge.
func TestRubyTrans_DynamicKey_Skipped(t *testing.T) {
	src := `class D
  def show(key)
    I18n.t(key)
  end
end
`
	recs := extractRubyRecords(t, src)
	if rbAnyTransNode(recs) {
		t.Fatalf("dynamic key I18n.t(key) should NOT create a translation node")
	}
}
