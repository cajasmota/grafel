package extractor

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestIsI18nImportSource(t *testing.T) {
	cases := map[string]bool{
		"react-i18next":            true,
		"i18next":                  true,
		"next-i18next":             true,
		"vue-i18n":                 true,
		"@nuxtjs/i18n":             true,
		"@lingui/react":            true,
		"gettext":                  false, // python gettext gated by python extractor, not this JS dict
		"django.utils.translation": false, // module form handled by python extractor, not this dict
		"express":                  false,
		"lodash":                   false,
		"./local/util":             false,
		"":                         false,
	}
	for in, want := range cases {
		if got := IsI18nImportSource(in); got != want {
			t.Errorf("IsI18nImportSource(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIsStaticTranslationKey(t *testing.T) {
	cases := map[string]bool{
		"errors.notFound": true,
		".relative":       true,
		"Welcome":         true,
		"":                false,
		"  ":              false,
		"a.${x}":          false, // JS interpolation
		"a.#{x}":          false, // Ruby interpolation
		"a.{{x}}":         false, // mustache/handlebars interpolation
	}
	for in, want := range cases {
		if got := IsStaticTranslationKey(in); got != want {
			t.Errorf("IsStaticTranslationKey(%q) = %v, want %v", in, got, want)
		}
	}
}

// Convergence at the construction layer: the same key emitted from two callers
// produces ONE entity (deduped) and one edge per (caller, key) tuple — and the
// key node id is stable regardless of caller / language.
func TestEmitTranslationKeyEdges_Convergence(t *testing.T) {
	file := types.EntityRecord{Name: "file", Kind: "SCOPE.Component", Subtype: "file"}
	a := types.EntityRecord{Name: "A", Kind: string(types.EntityKindFunction)}
	b := types.EntityRecord{Name: "B", Kind: string(types.EntityKindFunction)}
	ents := []types.EntityRecord{file, a, b}

	n := EmitTranslationKeyEdges(&ents, "jsts", []TranslationUse{
		{Key: "errors.notFound", FromName: "A", Tag: "react-i18next"},
		{Key: "errors.notFound", FromName: "B", Tag: "react-i18next"},
		{Key: "errors.notFound", FromName: "A", Tag: "react-i18next"}, // dup edge
	})
	if n != 2 {
		t.Fatalf("expected 2 edges (A,B), got %d", n)
	}

	// Exactly one key node.
	keyNodes := 0
	var keyID string
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindTranslationKey) &&
			ents[i].Name == TranslationKeyName("errors.notFound") {
			keyNodes++
			keyID = ents[i].ID
		}
	}
	if keyNodes != 1 {
		t.Fatalf("expected exactly 1 translation-key node, got %d", keyNodes)
	}
	if keyID == "" {
		t.Fatalf("key node has empty ID")
	}

	// Each caller has exactly one USES_TRANSLATION edge to the key target.
	want := TranslationKeyTargetID("errors.notFound")
	for _, name := range []string{"A", "B"} {
		count := 0
		for i := range ents {
			if ents[i].Name != name {
				continue
			}
			for _, r := range ents[i].Relationships {
				if r.Kind == string(types.RelationshipKindUsesTranslation) && r.ToID == want {
					count++
				}
			}
		}
		if count != 1 {
			t.Errorf("caller %s: expected 1 USES_TRANSLATION edge, got %d", name, count)
		}
	}

	// Cross-language stability: a python-emitted key converges to the SAME ID.
	pyEnt := TranslationKeyEntity("errors.notFound", "python")
	if pyEnt.ID != keyID {
		t.Errorf("cross-language key ID mismatch: python=%q jsts=%q", pyEnt.ID, keyID)
	}
}
