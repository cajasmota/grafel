// config_consumer.go — supplemental pass that emits DEPENDS_ON_CONFIG edges
// from PHP / Laravel code that reads a configuration key to a shared config-key
// entity (issue #3641, epic #3625).
//
// Detected reader shapes (literal keys only — honest-partial):
//
//	getenv('DATABASE_URL')           → config:DATABASE_URL   (env_getenv)
//	$_ENV['DATABASE_URL']            → config:DATABASE_URL   (env_superglobal)
//	env('APP_KEY')                   → config:APP_KEY        (laravel_env)
//	config('app.timezone')           → config:app.timezone  (laravel_config)
//
// Dynamic keys — getenv($var), env($name), config($key), $_ENV[$var] — are NOT
// emitted; only literal string keys are recorded so the graph never fabricates
// a missing key.
//
// Each read produces a SCOPE.Config / config_key entity (shared, file-agnostic
// node) and a DEPENDS_ON_CONFIG edge from the enclosing function / method (or
// the file entity at top level) to it, mirroring the Go/Java/JS config_consumer
// shape at config-KEY granularity so config:<key>'s inbound edges are the
// config-change blast radius.

package php

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitConfigConsumerEdges scans every function / method body (and file scope)
// for config-read shapes and appends config-key entities + DEPENDS_ON_CONFIG
// edges to *entities.
//
// (*entities)[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitConfigConsumerEdges(root ts.Node, file extractor.FileInput, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}
	src := file.Content

	var reads []extractor.ConfigRead

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
			if key, pat := phpFunctionCallConfigKey(n, src); key != "" {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: enclosing, Pattern: pat})
			}
		case "subscript_expression":
			if key := phpSuperglobalEnvKey(n, src); key != "" {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: enclosing, Pattern: "env_superglobal"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingClass, enclosing)
		}
	}
	walk(root, "", "")

	extractor.EmitConfigReads(entities, "php", reads)
}

// phpFunctionCallConfigKey returns the literal config key + detector label when
// the call is one of the supported reader functions with a literal first
// string argument, or ("","") otherwise.
//
//	getenv('X')          → "env_getenv"
//	env('X')             → "laravel_env"     (Laravel global helper)
//	config('app.x')      → "laravel_config"  (Laravel global helper)
func phpFunctionCallConfigKey(call ts.Node, src []byte) (string, string) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "name" {
		return "", ""
	}
	pattern := ""
	switch strings.TrimSpace(string(src[fn.StartByte():fn.EndByte()])) {
	case "getenv":
		pattern = "env_getenv"
	case "env":
		pattern = "laravel_env"
	case "config":
		pattern = "laravel_config"
	default:
		return "", ""
	}
	key := phpFirstStringArg(call.ChildByFieldName("arguments"), src)
	if key == "" {
		return "", ""
	}
	return key, pattern
}

// phpSuperglobalEnvKey returns the config key for a $_ENV['X'] (or
// $_SERVER['X']) subscript expression with a literal string index, or "".
//
// tree-sitter-php shape: (subscript_expression (variable_name) (string ...)).
func phpSuperglobalEnvKey(n ts.Node, src []byte) string {
	// The subscripted object is the first child; the index is field "index" in
	// most grammar revisions, else the last named child.
	var obj ts.Node
	for i := 0; i < int(n.NamedChildCount()); i++ {
		obj = n.NamedChild(i)
		break
	}
	if obj == nil || obj.Type() != "variable_name" {
		return ""
	}
	recv := strings.TrimSpace(string(src[obj.StartByte():obj.EndByte()]))
	if recv != "$_ENV" && recv != "$_SERVER" {
		return ""
	}
	idx := n.ChildByFieldName("index")
	if idx == nil {
		// Fallback: the index is the second named child.
		if n.NamedChildCount() >= 2 {
			idx = n.NamedChild(1)
		}
	}
	if idx == nil {
		return ""
	}
	return phpStringLiteral(idx, src)
}

// phpFirstStringArg returns the literal content of the first string argument in
// an `arguments` node, or "" when the first argument is missing or non-literal
// (dynamic) — honest-partial.
func phpFirstStringArg(args ts.Node, src []byte) string {
	if args == nil || args.NamedChildCount() == 0 {
		return ""
	}
	first := args.NamedChild(0)
	if first == nil {
		return ""
	}
	// The argument node may wrap the literal (grammar revisions differ); unwrap
	// a single-child `argument` node to its inner expression.
	if first.Type() == "argument" && first.NamedChildCount() == 1 {
		first = first.NamedChild(0)
	}
	return phpStringLiteral(first, src)
}

// phpStringLiteral extracts the literal content of a PHP string node, dropping
// the surrounding quotes. Returns "" for an interpolated (encapsed) string that
// contains a variable / expression — honest-partial, no fabrication.
func phpStringLiteral(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case "string", "encapsed_string":
		// Reject interpolation: any non-text child means a dynamic segment.
		for i := 0; i < int(node.NamedChildCount()); i++ {
			ct := node.NamedChild(i).Type()
			if ct != "string_content" && ct != "string_value" && ct != "escape_sequence" {
				return ""
			}
		}
		raw := strings.TrimSpace(string(src[node.StartByte():node.EndByte()]))
		if len(raw) >= 2 {
			q := raw[0]
			if (q == '"' || q == '\'') && raw[len(raw)-1] == q {
				return raw[1 : len(raw)-1]
			}
		}
		return raw
	}
	return ""
}
