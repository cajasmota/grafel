// translation_key.go — shared, cross-language helpers for the i18n /
// localization key-usage topology (child of epic #3628). It mirrors the
// external-service model in external_service.go and the template-render model
// in template_render.go.
//
// The capability answers one question a rewrite (or an i18n / localization
// audit) needs: "where is the 'errors.notFound' string used, and which keys
// are referenced at all?" — so a graph consumer can drive untranslated-key
// analysis (a key node with no backing catalog entry) and impact analysis
// (rename a key → every USES_TRANSLATION caller).
//
// To make every reference site of a key converge on ONE graph node, each
// language extractor emits:
//
//   - one SCOPE.TranslationKey / subtype="translation_key" entity per distinct
//     literal key, with a SYNTHETIC constant SourceFile (TranslationKeySourceFile)
//     so EntityRecord.ComputeID(SourceFile+Kind+Name) collapses the same key
//     across files AND languages/frameworks into a single node. An
//     `errors.notFound` referenced in Login.tsx and again in Signup.tsx
//     therefore share one "i18n:errors.notFound" node, and that node's inbound
//     USES_TRANSLATION set is the codebase's full footprint for that key.
//
//   - one USES_TRANSLATION edge (enclosing function / component → key node)
//     carried as a structural-ref ToID (TranslationKeyTargetID) that the
//     resolver binds via the byQualifiedName exact-match tier (the entity's
//     QualifiedName is set equal to that ToID).
//
// Precision-first / honest-partial: the REQUIRE-I18N-CONTEXT boundary. An edge
// is emitted only when the call is rooted at a recognised i18n function context
// (a t/$t/i18n.t/Trans i18next binding, a gettext family symbol, Rails I18n.t /
// relative t('.x'), Laravel __ / trans) AND the key is a STATIC string literal.
// A dynamic key (`t(keyVar)`, interpolated template) is skipped; a bare
// `_('x')` that is NOT the gettext underscore (e.g. lodash `_`) or an unrelated
// `t(...)` that is not an i18n binding emits NO node/edge — a fabricated key
// would poison untranslated-key analysis. The detection of these shapes lives
// in each language extractor; this file owns the node/edge construction so the
// convergence invariant is identical everywhere.

package extractor

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// TranslationKeySourceFile is the synthetic, constant SourceFile assigned to
// every translation-key entity so identical keys converge to a single graph
// node under EntityRecord.ComputeID (SourceFile+Kind+Name).
const TranslationKeySourceFile = "<translation-key>"

// translationKeyName is the "i18n:" namespace prefix for a key node's Name so
// it never collides with a same-named code symbol, e.g. "i18n:errors.notFound".
const translationKeyName = "i18n:"

// TranslationKeyName returns the canonical entity Name for a translation key.
func TranslationKeyName(key string) string {
	return translationKeyName + key
}

// TranslationKeyTargetID returns the structural-ref ToID for a USES_TRANSLATION
// edge pointing at a key entity. Shape:
//
//	scope:translationkey:<key>
//
// This value is ALSO stored as the key entity's QualifiedName, so the
// resolver's byQualifiedName exact-match tier (internal/resolve/refs.go) binds
// the edge to that entity without any new linker code. Constant across
// languages so a react-i18next `t('errors.notFound')` and a hypothetical
// gettext `_('errors.notFound')` resolve to the same node.
func TranslationKeyTargetID(key string) string {
	return "scope:translationkey:" + key
}

// TranslationKeyEntity builds the SCOPE.TranslationKey / translation_key entity
// for a single literal key. The entity is deliberately file-agnostic (synthetic
// SourceFile) so it is the shared localization convergence node, and its
// QualifiedName equals the edge ToID so USES_TRANSLATION edges resolve via
// byQualifiedName.
func TranslationKeyEntity(key, lang string) types.EntityRecord {
	e := types.EntityRecord{
		Name:          TranslationKeyName(key),
		QualifiedName: TranslationKeyTargetID(key),
		Kind:          string(types.EntityKindTranslationKey),
		Subtype:       "translation_key",
		Language:      lang,
		SourceFile:    TranslationKeySourceFile,
		StartLine:     1,
		EndLine:       1,
		Signature:     TranslationKeyName(key),
		Properties: map[string]string{
			"key": key,
		},
	}
	if rel := relativeKeyRoot(key); rel {
		e.Properties["relative"] = "true"
	}
	e.ID = e.ComputeID()
	return e
}

// relativeKeyRoot reports whether key is a Rails "relative" key (leading dot,
// e.g. `t('.title')`), which resolves against the current view/controller
// scope at runtime. We keep it as-written (the literal IS the key) but flag it
// so consumers know the resolution is scope-dependent.
func relativeKeyRoot(key string) bool {
	return strings.HasPrefix(key, ".")
}

// TranslationUse is one resolved i18n key reference detected by a language
// extractor: the literal key, the Name of the enclosing function / component,
// and the optional framework tag (e.g. "react-i18next", "vue-i18n", "gettext",
// "rails-i18n", "laravel") captured for the edge property.
type TranslationUse struct {
	Key      string // literal translation key (errors.notFound, messages.welcome)
	FromName string // enclosing function/component Name; "" => file entity
	Tag      string // i18n framework tag for the edge property
}

// EmitTranslationKeyEdges appends, to *entities, the translation-key entities
// and USES_TRANSLATION edges for the given detections.
//
// entities[0] MUST be the file entity (every language extractor appends it
// first). Edges whose FromName is "" — or whose FromName has no matching host
// entity — attach to the file entity (index 0) as a conservative fallback so
// the edge is never silently dropped. Identical keys converge to one key entity
// (deduped by key) and one edge per (FromName, key) tuple (the first tag seen
// for that tuple wins).
//
// Returns the number of USES_TRANSLATION edges emitted. Safe with nil/empty
// input. Detections whose Key is empty (or that look dynamic — caller's
// responsibility to pre-filter) are skipped — precision over recall.
func EmitTranslationKeyEdges(entities *[]types.EntityRecord, lang string, uses []TranslationUse) int {
	if entities == nil || len(*entities) == 0 || len(uses) == 0 {
		return 0
	}

	hostByName := map[string]int{}
	for i := range *entities {
		hostByName[(*entities)[i].Name] = i
	}

	seenEdge := map[string]bool{}
	seenKey := map[string]bool{}
	var newEntities []types.EntityRecord
	emitted := 0

	for _, u := range uses {
		key := strings.TrimSpace(u.Key)
		if key == "" {
			continue
		}

		hostIdx := 0 // file entity by default
		if u.FromName != "" {
			if idx, ok := hostByName[u.FromName]; ok {
				hostIdx = idx
			}
		}

		edgeKey := u.FromName + "\x00" + key
		if !seenEdge[edgeKey] {
			seenEdge[edgeKey] = true
			props := map[string]string{"key": key}
			if u.Tag != "" {
				props["framework"] = u.Tag
			}
			(*entities)[hostIdx].Relationships = append((*entities)[hostIdx].Relationships,
				types.RelationshipRecord{
					ToID:       TranslationKeyTargetID(key),
					Kind:       string(types.RelationshipKindUsesTranslation),
					Properties: props,
				})
			emitted++
		}

		if !seenKey[key] {
			seenKey[key] = true
			newEntities = append(newEntities, TranslationKeyEntity(key, lang))
		}
	}

	*entities = append(*entities, newEntities...)
	return emitted
}

// IsI18nImportSource reports whether a JS/TS import module / package name
// establishes an i18n CONTEXT for a file (react-i18next, i18next, vue-i18n, …).
// A file that imports one of these is allowed to resolve a bare i18n-function
// call (t/$t/useTranslation/useI18n) into a key reference; a file with NO such
// import does not (so a local helper named `t` or a lodash `_` never fabricates
// a key). Matched by leading scoped/path segment. Python gettext context is
// gated separately by the Python extractor's isI18nPythonSource.
func IsI18nImportSource(source string) bool {
	s := strings.ToLower(strings.TrimSpace(source))
	s = strings.Trim(s, "'\"`")
	s = strings.ReplaceAll(s, "\\", "/")
	if s == "" {
		return false
	}
	// Leading scoped or path segment.
	head := s
	if strings.HasPrefix(s, "@") {
		parts := strings.SplitN(s, "/", 3)
		if len(parts) >= 2 {
			head = parts[0] + "/" + parts[1]
		}
	} else if i := strings.IndexAny(s, "/."); i >= 0 {
		head = s[:i]
	}
	// JS/TS i18n packages only — Python gettext / django.utils.translation
	// context is gated by the Python extractor's own isI18nPythonSource, not
	// this leading-segment dictionary (which would misfire on a `django.*`
	// import that is unrelated to translation).
	switch head {
	case "react-i18next", "i18next", "next-i18next",
		"vue-i18n", "@nuxtjs/i18n", "i18n-js",
		"@lingui/react", "@lingui/macro":
		return true
	}
	return false
}

// IsStaticTranslationKey reports whether s is a usable static translation key:
// non-empty, and free of interpolation markers that signal a dynamic value
// (`${`, `#{`, `{{`, a bare `+` concatenation is handled by the caller seeing a
// non-literal node). This is the literal-payload guard; the CONTEXT guard
// (is this actually an i18n function?) is the language extractor's job.
func IsStaticTranslationKey(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	if strings.Contains(s, "${") || strings.Contains(s, "#{") || strings.Contains(s, "{{") {
		return false
	}
	return true
}
