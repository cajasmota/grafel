// translation_key.go — supplemental pass that emits USES_TRANSLATION edges
// from Python functions / methods to a shared SCOPE.TranslationKey node
// (localization capability, child of epic #3628). It lets the graph answer
// "where is the 'Welcome' string used?" and supports untranslated-key
// analysis. Covers Django / gettext.
//
// Detected shapes (REQUIRE-I18N-CONTEXT — honest-partial, precision-first):
//
//	from django.utils.translation import gettext as _
//	_("Welcome")                                              → Welcome
//	from django.utils.translation import gettext_lazy
//	gettext_lazy("Sign in")                                   → Sign in
//	from gettext import gettext, ngettext
//	gettext("Hello")                                          → Hello
//
// The i18n CONTEXT gate is the IMPORT: the called name (`_`, `gettext`,
// `gettext_lazy`, `ngettext`, `pgettext`, `ugettext`, …) must be bound by a
// `from django.utils.translation import …` / `from gettext import …` (alias
// honored). A bare `_("x")` whose `_` was NOT imported from a gettext source
// emits NO edge — so a throwaway `_` placeholder variable never fabricates a
// key.
//
// Intentionally DROPPED: a dynamic key (`_(msg_var)`); a `_("x")` whose `_` is
// not a recognised gettext import; module-scope keys attach to the file entity.
//
// Node/edge construction (convergence on one node per key via a synthetic
// SourceFile) lives in extractor.EmitTranslationKeyEdges.

package python

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// gettextFunctions are the recognised gettext-family callable names. The import
// source still has to be a recognised i18n module (gettext context gate).
var gettextFunctions = map[string]bool{
	"_": true, "gettext": true, "gettext_lazy": true,
	"ngettext": true, "ngettext_lazy": true,
	"pgettext": true, "pgettext_lazy": true,
	"npgettext": true, "ugettext": true, "ugettext_lazy": true,
}

// i18nPythonSources are the import modules that establish a gettext context.
func isI18nPythonSource(module string) bool {
	m := strings.TrimSpace(module)
	return m == "gettext" ||
		m == "django.utils.translation" ||
		strings.HasSuffix(m, ".utils.translation") ||
		m == "flask_babel" || m == "flask_babelex" ||
		m == "babel" || m == "gettext.gettext"
}

// emitTranslationKeyEdges scans every function / method body for gettext-family
// key references rooted at a recognised i18n import and appends translation-key
// entities + USES_TRANSLATION edges.
//
// entities[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input. Keys at module scope attach to the file entity.
func emitTranslationKeyEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	imports := buildPythonImportMap(root, file)
	if len(imports) == 0 {
		return // no imports → no gettext context can exist
	}
	src := file.Content

	// i18nLocal maps a local callable name to true when it was imported from a
	// recognised i18n module AND is a gettext-family function.
	i18nLocal := map[string]bool{}
	for local, b := range imports {
		if b.plainModule {
			continue // `import gettext` → calls are `gettext.gettext(...)`, handled below
		}
		if isI18nPythonSource(b.sourceModule) && gettextFunctions[b.importedName] {
			i18nLocal[local] = true
		}
	}
	// `import gettext` style — calls look like `gettext.gettext("x")`.
	gettextModuleAliases := map[string]bool{}
	for local, b := range imports {
		if b.plainModule && (b.sourceModule == "gettext" || b.sourceModule == "django.utils.translation") {
			gettextModuleAliases[local] = true
		}
	}

	if len(i18nLocal) == 0 && len(gettextModuleAliases) == 0 {
		return
	}

	var uses []extractor.TranslationUse

	var stack []string
	current := func() string {
		if len(stack) == 0 {
			return ""
		}
		return stack[len(stack)-1]
	}

	var walk func(n ts.Node, parentClass string)
	walk = func(n ts.Node, parentClass string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_definition":
			cls := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				cls = nodeText(nn, src)
			}
			childCls := cls
			if parentClass != "" && cls != "" {
				childCls = parentClass + "." + cls
			}
			stack = append(stack, childCls)
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), childCls)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "function_definition":
			leaf := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				leaf = nodeText(nn, src)
			}
			emitted := leaf
			if parentClass != "" && leaf != "" {
				emitted = parentClass + "." + leaf
			}
			stack = append(stack, emitted)
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), parentClass)
				}
			}
			stack = stack[:len(stack)-1]
			return
		case "decorated_definition":
			if inner := n.ChildByFieldName("definition"); inner != nil {
				walk(inner, parentClass)
			}
			return
		case "call":
			if key, ok := pyTranslationCall(n, src, i18nLocal, gettextModuleAliases); ok {
				uses = append(uses, extractor.TranslationUse{Key: key, FromName: current(), Tag: "gettext"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), parentClass)
		}
	}
	walk(root, "")

	extractor.EmitTranslationKeyEdges(entities, "python", uses)
}

// pyTranslationCall inspects a `call` node and returns the literal key when it
// is a recognised gettext-family call with a static first string argument.
//
// Two recognition paths:
//  1. bare `_("x")` / `gettext("x")` where the callee name is in i18nLocal.
//  2. `gettext.gettext("x")` where the root module is a gettext module alias
//     and the tail is a gettext-family function.
//
// Returns ok=false for an unrecognised callee or a dynamic first argument.
func pyTranslationCall(call ts.Node, src []byte, i18nLocal, gettextModuleAliases map[string]bool) (string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return "", false
	}
	switch fn.Type() {
	case "identifier":
		name := strings.TrimSpace(nodeText(fn, src))
		if !i18nLocal[name] {
			return "", false
		}
	case "attribute":
		root, tail := pyAttributeRootAndTail(fn, src)
		if root == "" || !gettextModuleAliases[root] || !gettextFunctions[tail] {
			return "", false
		}
	default:
		return "", false
	}
	return pyTransFirstStringArg(call, src)
}

// pyTransFirstStringArg returns the first positional argument of a call when it
// is a static string literal. Returns ok=false for a dynamic first argument
// (variable, f-string, concatenation) — the honest-partial boundary. Uses
// pyStringLiteralValue so an f-string with an interpolation yields "" (drop).
func pyTransFirstStringArg(call ts.Node, src []byte) (string, bool) {
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
			lit := pyStringLiteralValue(a, src)
			if extractor.IsStaticTranslationKey(lit) {
				return lit, true
			}
			return "", false
		case "keyword_argument", "comment":
			// e.g. message="x" — not the positional key; keep scanning.
			continue
		default:
			// First positional is dynamic (identifier, f-string, binary op).
			return "", false
		}
	}
	return "", false
}
