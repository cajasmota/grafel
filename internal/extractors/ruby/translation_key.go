// translation_key.go — supplemental pass that emits USES_TRANSLATION edges
// from Ruby methods to a shared SCOPE.TranslationKey node (localization
// capability, child of epic #3628). It lets the graph answer "where is the
// 'users.title' string used?" and supports untranslated-key analysis. Covers
// Rails I18n.
//
// Detected shapes (REQUIRE-I18N-CONTEXT — honest-partial, precision-first):
//
//	I18n.t('users.title')          → users.title    (explicit I18n receiver)
//	I18n.translate('a.b')          → a.b
//	t('.title')                    → .title         (Rails relative key — the
//	                                 leading dot makes it unambiguously i18n)
//
// The i18n CONTEXT gate:
//   - `I18n.t` / `I18n.translate` — the explicit receiver IS the context.
//   - bare `t('x')` — honored ONLY when the key is a Rails relative key
//     (leading `.`), an unambiguous i18n shape. A bare `t('plain')` with no
//     receiver and no leading dot is ambiguous (could be a local method) and
//     is DROPPED — precision over recall.
//
// Intentionally DROPPED: a dynamic key (`t(key_var)`, interpolated); a bare
// `t('plain')` (ambiguous); symbol args.
//
// Node/edge construction (convergence on one node per key) lives in
// extractor.EmitTranslationKeyEdges.

package ruby

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitTranslationKeyEdges scans every method body for Rails I18n key references
// and appends translation-key entities + USES_TRANSLATION edges to *entities.
//
// (*entities)[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitTranslationKeyEdges(root ts.Node, src []byte, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var uses []extractor.TranslationUse

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "method", "singleton_method":
			name := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), name)
			}
			return
		case "call":
			if key, ok := rubyTranslationCall(n, src); ok {
				uses = append(uses, extractor.TranslationUse{Key: key, FromName: enclosing, Tag: "rails-i18n"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extractor.EmitTranslationKeyEdges(entities, "ruby", uses)
}

// rubyTranslationCall returns the literal key for a recognised Rails I18n call
// with a static first string argument, or ("", false).
//
//	I18n.t('x') / I18n.translate('x')  → explicit-receiver i18n
//	t('.relative')                     → relative-key i18n (leading dot)
func rubyTranslationCall(call ts.Node, src []byte) (string, bool) {
	method := call.ChildByFieldName("method")
	if method == nil {
		return "", false
	}
	mname := strings.TrimSpace(rubyNodeText(method, src))

	recv := call.ChildByFieldName("receiver")
	requireRelative := false
	switch {
	case recv != nil:
		// Explicit receiver — must be I18n with t/translate.
		if strings.TrimSpace(rubyNodeText(recv, src)) != "I18n" {
			return "", false
		}
		if mname != "t" && mname != "translate" {
			return "", false
		}
	default:
		// Receiver-less — only `t` / `translate`, and only the relative form.
		if mname != "t" && mname != "translate" {
			return "", false
		}
		requireRelative = true
	}

	key, ok := rubyFirstStringArg(call, src)
	if !ok {
		return "", false
	}
	if requireRelative && !strings.HasPrefix(key, ".") {
		return "", false // bare t('plain') is ambiguous — drop
	}
	return key, true
}

// rubyFirstStringArg returns the first positional argument when it is a static
// string literal, or ("", false) for a dynamic / symbol / interpolated arg.
func rubyFirstStringArg(call ts.Node, src []byte) (string, bool) {
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return "", false
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		a := args.NamedChild(i)
		if a == nil {
			continue
		}
		switch a.Type() {
		case "string":
			s := rubyStringContent(a, src)
			if extractor.IsStaticTranslationKey(s) {
				return s, true
			}
			return "", false
		case "pair":
			// keyword arg (scope:, default:) — not the positional key.
			continue
		default:
			// First positional is a symbol / variable / interpolation — dynamic.
			return "", false
		}
	}
	return "", false
}
