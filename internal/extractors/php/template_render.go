// template_render.go — supplemental pass that emits RENDERS edges from Laravel
// controller actions / routes to a shared SCOPE.Template node (epic #3628). It
// lets the graph answer "what Blade view does this action render?" (outbound
// RENDERS) and "who renders welcome?" (inbound RENDERS).
//
// Detected shapes (static only — honest-partial, precision-first):
//
//	return view('welcome');            → RENDERS welcome           (Laravel helper)
//	view('users.list', $data);         → RENDERS users/list        (dot → slash)
//	return View::make('dashboard');    → RENDERS dashboard         (View facade)
//
// Intentionally DROPPED (would mislead view-layer analysis):
//
//	view($name)                        (variable / dynamic view name)
//	view("users.$id")                  (interpolated string → dynamic)
//
// Laravel uses dot-notation for Blade view paths (`users.list` == the file
// resources/views/users/list.blade.php). NormalizeTemplateName converts the
// dots to slashes so a `view('users.list')` and an explicit 'users/list'
// converge to one node. All node/edge construction lives in
// extractor.EmitTemplateEdges.

package php

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitTemplateRenderEdges scans every function / method body (and file scope)
// for Laravel view() / View::make() render shapes and appends template entities
// + RENDERS edges to *entities.
//
// (*entities)[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitTemplateRenderEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var edges []extractor.TemplateEdge

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
			if name, pat := phpViewCallTemplate(n, src); name != "" {
				edges = append(edges, extractor.TemplateEdge{Name: name, FromName: enclosing, Pattern: pat})
			}
		case "scoped_call_expression":
			if name, pat := phpViewMakeTemplate(n, src); name != "" {
				edges = append(edges, extractor.TemplateEdge{Name: name, FromName: enclosing, Pattern: pat})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingClass, enclosing)
		}
	}
	walk(root, "", "")

	extractor.EmitTemplateEdges(entities, "php", edges)
}

// phpViewCallTemplate returns the literal Blade view name + detector label when
// the call is the Laravel `view('name')` global helper with a literal first
// string argument, or ("","") otherwise (including dynamic names → drop).
func phpViewCallTemplate(call ts.Node, src []byte) (string, string) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "name" {
		return "", ""
	}
	if strings.TrimSpace(string(src[fn.StartByte():fn.EndByte()])) != "view" {
		return "", ""
	}
	name := phpFirstStringArg(call.ChildByFieldName("arguments"), src)
	if name == "" {
		return "", ""
	}
	return name, "laravel_view"
}

// phpViewMakeTemplate returns the literal Blade view name + detector label when
// the call is the Laravel `View::make('name')` facade with a literal first
// string argument, or ("","") otherwise.
func phpViewMakeTemplate(call ts.Node, src []byte) (string, string) {
	nameNode := call.ChildByFieldName("name")
	if nameNode == nil || nameNode.Type() != "name" {
		return "", ""
	}
	if strings.TrimSpace(string(src[nameNode.StartByte():nameNode.EndByte()])) != "make" {
		return "", ""
	}
	scope := call.ChildByFieldName("scope")
	if scope == nil {
		return "", ""
	}
	raw := strings.TrimSpace(string(src[scope.StartByte():scope.EndByte()]))
	leaf := raw
	if i := strings.LastIndex(raw, "\\"); i >= 0 {
		leaf = raw[i+1:]
	}
	if leaf != "View" {
		return "", ""
	}
	name := phpFirstStringArg(call.ChildByFieldName("arguments"), src)
	if name == "" {
		return "", ""
	}
	return name, "view_make"
}
