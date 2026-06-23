// translation_key.go — supplemental pass that emits USES_TRANSLATION edges
// from PHP functions / methods to a shared SCOPE.TranslationKey node
// (localization capability, child of epic #3628). It lets the graph answer
// "where is the 'messages.welcome' string used?" and supports untranslated-key
// analysis. Covers Laravel.
//
// Detected shapes (REQUIRE-I18N-CONTEXT — honest-partial, precision-first):
//
//	__('messages.welcome')         → messages.welcome   (Laravel translate helper)
//	trans('users.title')           → users.title        (Laravel trans helper)
//	trans_choice('apples', $n)     → apples             (pluralization helper)
//
// The i18n CONTEXT gate is the helper NAME: `__`, `trans`, and `trans_choice`
// are Laravel's global translation helpers — recognised directly (they are not
// general-purpose PHP functions). A static literal first argument is required.
//
// Intentionally DROPPED: a dynamic key (`__($key)`, interpolated `"users.$id"`);
// the Blade `@lang('x')` directive (it lives in *.blade.php template text, not
// the PHP AST — honest-skip here).
//
// Node/edge construction (convergence on one node per key) lives in
// extractor.EmitTranslationKeyEdges.

package php

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// laravelTransHelpers are the recognised Laravel translation helper names.
var laravelTransHelpers = map[string]bool{
	"__": true, "trans": true, "trans_choice": true,
}

// emitTranslationKeyEdges scans every function / method body (and file scope)
// for Laravel translation-helper calls and appends translation-key entities +
// USES_TRANSLATION edges to *entities.
//
// (*entities)[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitTranslationKeyEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var uses []extractor.TranslationUse

	var walk func(n ts.Node, enclosingClass, enclosing string)
	walk = func(n ts.Node, enclosingClass, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_declaration", "interface_declaration", "trait_declaration":
			cls := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), cls, enclosing)
			}
			return
		case "method_declaration":
			leaf := childFieldText(n, "name", src)
			name := leaf
			if enclosingClass != "" && leaf != "" {
				name = enclosingClass + "." + leaf
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), enclosingClass, name)
			}
			return
		case "function_definition":
			leaf := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), enclosingClass, leaf)
			}
			return
		case "function_call_expression":
			if key, ok := phpTranslationCall(n, src); ok {
				uses = append(uses, extractor.TranslationUse{Key: key, FromName: enclosing, Tag: "laravel"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingClass, enclosing)
		}
	}
	walk(root, "", "")

	extractor.EmitTranslationKeyEdges(entities, "php", uses)
}

// phpTranslationCall returns the literal key when the call is a recognised
// Laravel translation helper (`__`, `trans`, `trans_choice`) with a static
// first string argument, or ("", false).
func phpTranslationCall(call ts.Node, src []byte) (string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "name" {
		return "", false
	}
	name := strings.TrimSpace(string(src[fn.StartByte():fn.EndByte()]))
	if !laravelTransHelpers[name] {
		return "", false
	}
	key := phpFirstStringArg(call.ChildByFieldName("arguments"), src)
	if !extractor.IsStaticTranslationKey(key) {
		return "", false
	}
	return key, true
}
