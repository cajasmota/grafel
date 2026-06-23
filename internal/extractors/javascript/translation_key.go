// translation_key.go — supplemental pass that emits USES_TRANSLATION edges
// from JS/TS functions / components to a shared SCOPE.TranslationKey node
// (localization capability, child of epic #3628). It lets the graph answer
// "where is the 'errors.notFound' string used?" and supports untranslated-key
// analysis. Covers react-i18next / i18next and vue-i18n.
//
// Detected shapes (REQUIRE-I18N-CONTEXT — honest-partial, precision-first):
//
//	import { useTranslation } from "react-i18next";
//	const { t } = useTranslation(); t("errors.notFound")        → errors.notFound
//	import i18n from "i18next"; i18n.t("x")                      → x
//	i18next.t("x")                                               → x
//	<Trans i18nKey="x">…</Trans>                                 → x
//	vue-i18n: $t("x") / this.$t("x") / t("x") from useI18n()     → x
//
// The i18n CONTEXT gate: a file must either import a recognised i18n module
// (react-i18next / i18next / vue-i18n / …) OR the call must be on an
// unambiguous i18n receiver (`i18n.t` / `i18next.t` / `$t` / `this.$t`). A bare
// `t("x")` is honored ONLY when an i18n import is present in the file — so a
// local helper named `t` or a lodash `_` never fabricates a key.
//
// Intentionally DROPPED: a dynamic key (`t(keyVar)`, `t(`a${x}`)`); a bare
// `t("x")` with NO i18n import in the file; any call whose receiver is not a
// recognised i18n binding.
//
// Node/edge construction (convergence on one node per key via a synthetic
// SourceFile) lives in extractor.EmitTranslationKeyEdges.

package javascript

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// emitTranslationKeyEdges scans the AST for i18n key references rooted at a
// recognised i18n context and appends translation-key entities +
// USES_TRANSLATION edges to x.entities. x.entities[0] MUST be the file entity.
func (x *extractor) emitTranslationKeyEdges(root ts.Node) {
	if root == nil || len(x.entities) == 0 {
		return
	}
	hasI18nImport := x.fileHasI18nImport()

	var uses []extreg.TranslationUse

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_declaration", "generator_function_declaration":
			enclosing = x.nodeText(n.ChildByFieldName("name"))
		case "method_definition":
			enclosing = x.nodeText(n.ChildByFieldName("name"))
		case "variable_declarator":
			// const Login = () => {...} / const Login = function() {...}
			if nn := n.ChildByFieldName("name"); nn != nil {
				if v := n.ChildByFieldName("value"); v != nil {
					switch v.Type() {
					case "arrow_function", "function", "function_expression":
						enclosing = x.nodeText(nn)
					}
				}
			}
		case "call_expression":
			if key, tag, ok := x.jsTranslationCall(n, hasI18nImport); ok {
				uses = append(uses, extreg.TranslationUse{Key: key, FromName: enclosing, Tag: tag})
			}
		case "jsx_self_closing_element", "jsx_opening_element":
			if key, ok := x.jsxTransKey(n); ok {
				uses = append(uses, extreg.TranslationUse{Key: key, FromName: enclosing, Tag: "react-i18next"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extreg.EmitTranslationKeyEdges(&x.entities, x.language, uses)
}

// fileHasI18nImport reports whether the file imports a recognised i18n module
// (react-i18next / i18next / vue-i18n / …). Establishes the CONTEXT that makes
// a bare `t("x")` trustworthy.
func (x *extractor) fileHasI18nImport() bool {
	for _, b := range x.importByLocal {
		if b != nil && extreg.IsI18nImportSource(b.importPath) {
			return true
		}
	}
	return false
}

// jsTranslationCall resolves a call_expression to an i18n key when the call is
// a recognised translation invocation AND its first argument is a static
// string literal. Returns (key, framework-tag, true) or ("","",false).
func (x *extractor) jsTranslationCall(call ts.Node, hasI18nImport bool) (string, string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return "", "", false
	}

	tag := ""
	switch fn.Type() {
	case "identifier":
		name := x.nodeText(fn)
		switch name {
		case "t":
			// Bare t("x") — trustworthy only with an i18n import in the file.
			if !hasI18nImport {
				return "", "", false
			}
			tag = "react-i18next"
		case "$t":
			tag = "vue-i18n"
		default:
			return "", "", false
		}
	case "member_expression":
		root, tail := x.i18nMemberRootTail(fn)
		switch {
		case tail == "t" && (root == "i18n" || root == "i18next"):
			tag = "i18next"
		case tail == "t" && root == "this":
			// this.t(...) — only with an i18n import (vue Options API rarely).
			if !hasI18nImport {
				return "", "", false
			}
			tag = "react-i18next"
		case tail == "$t" && root == "this":
			tag = "vue-i18n"
		default:
			return "", "", false
		}
	default:
		return "", "", false
	}

	key, ok := x.jsFirstStringArg(call)
	if !ok {
		return "", "", false
	}
	return key, tag, true
}

// jsFirstStringArg returns the first argument of a call when it is a plain
// static string literal (single/double quote, or a backtick template with NO
// substitutions). Returns ok=false for a dynamic key (identifier, interpolated
// template, member access, concatenation) — the honest-partial boundary.
func (x *extractor) jsFirstStringArg(call ts.Node) (string, bool) {
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
			lit := stringLiteralValue(x.nodeText(a))
			if extreg.IsStaticTranslationKey(lit) {
				return lit, true
			}
			return "", false
		case "template_string", "template_literal":
			// Static only when there is no `${...}` substitution child.
			if jsTemplateHasSubstitution(a) {
				return "", false
			}
			lit := stringLiteralValue(x.nodeText(a))
			if extreg.IsStaticTranslationKey(lit) {
				return lit, true
			}
			return "", false
		default:
			// First positional arg is dynamic (variable / member / concat).
			return "", false
		}
	}
	return "", false
}

// jsTemplateHasSubstitution reports whether a template_string node contains a
// `${...}` interpolation (template_substitution child).
func jsTemplateHasSubstitution(n ts.Node) bool {
	for i := 0; i < int(n.ChildCount()); i++ {
		if c := n.Child(i); c != nil && c.Type() == "template_substitution" {
			return true
		}
	}
	return false
}

// jsxTransKey extracts the static `i18nKey="..."` attribute value from a
// `<Trans i18nKey="x">` element. Returns ("", false) when the element is not a
// Trans element, has no i18nKey, or the attribute value is dynamic ({expr}).
func (x *extractor) jsxTransKey(el ts.Node) (string, bool) {
	nameNode := el.ChildByFieldName("name")
	if nameNode == nil {
		return "", false
	}
	if x.nodeText(nameNode) != "Trans" {
		return "", false
	}
	for i := 0; i < int(el.ChildCount()); i++ {
		attr := el.Child(i)
		if attr == nil || attr.Type() != "jsx_attribute" {
			continue
		}
		// jsx_attribute children: property_identifier "=" (string | jsx_expression)
		if attr.NamedChildCount() < 1 {
			continue
		}
		an := attr.NamedChild(0)
		if an == nil || x.nodeText(an) != "i18nKey" {
			continue
		}
		// Find the value node (string => static; jsx_expression => dynamic).
		for j := 0; j < int(attr.NamedChildCount()); j++ {
			v := attr.NamedChild(j)
			if v == nil {
				continue
			}
			if v.Type() == "string" {
				lit := stringLiteralValue(x.nodeText(v))
				if extreg.IsStaticTranslationKey(lit) {
					return lit, true
				}
				return "", false
			}
		}
		return "", false // i18nKey present but value dynamic
	}
	return "", false
}

// i18nMemberRootTail returns the ROOT and LAST property of a member_expression.
// For `i18n.t` it returns ("i18n", "t"); for `this.$t` it returns ("this",
// "$t"). Returns ("", "") when the receiver is neither a plain identifier nor
// `this`. (Distinct from external_service.go's jsMemberRootAndTail, which
// flattens a deep dotted tail and rejects a `this` root.)
func (x *extractor) i18nMemberRootTail(member ts.Node) (string, string) {
	prop := member.ChildByFieldName("property")
	obj := member.ChildByFieldName("object")
	if prop == nil || obj == nil {
		return "", ""
	}
	tail := strings.TrimSpace(x.nodeText(prop))
	switch obj.Type() {
	case "identifier":
		return strings.TrimSpace(x.nodeText(obj)), tail
	case "this":
		return "this", tail
	}
	return "", tail
}
