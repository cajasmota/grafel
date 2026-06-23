// config_consumer.go — supplemental pass that emits DEPENDS_ON_CONFIG edges
// from .NET / C# code that reads a configuration key to a shared config-key
// entity (issue #3641, epic #3625).
//
// Detected reader shapes (literal keys only — honest-partial):
//
//	Configuration["ConnectionStrings:Default"]   → config:ConnectionStrings:Default
//	_configuration["App:Url"]                     → config:App:Url   (element access)
//	Configuration.GetValue<int>("App:Port")       → config:App:Port  (GetValue)
//	Configuration.GetConnectionString("Default")  → config:Default   (named conn)
//	Environment.GetEnvironmentVariable("PATH")    → config:PATH      (env var)
//
// Dynamic keys — Configuration[key], GetValue<T>(name) — are NOT emitted; only
// literal string keys are recorded so the graph never fabricates a missing key.
//
// Each read produces a SCOPE.Config / config_key entity (shared, file-agnostic
// node) and a DEPENDS_ON_CONFIG edge from the enclosing method (or the file
// entity at type/file scope) to it, mirroring the Go/Java/JS config_consumer
// shape at config-KEY granularity so config:<key>'s inbound edges are the
// config-change blast radius.

package csharp

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitConfigConsumerEdges scans every method / constructor body (and type/file
// scope) for config-read shapes and appends config-key entities +
// DEPENDS_ON_CONFIG edges to *entities.
//
// (*entities)[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitConfigConsumerEdges(root ts.Node, src []byte, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var reads []extractor.ConfigRead

	var walk func(n ts.Node, enclosingType, enclosing string)
	walk = func(n ts.Node, enclosingType, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "class_declaration", "struct_declaration", "record_declaration", "interface_declaration":
			cls := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), cls, enclosing)
			}
			return
		case "method_declaration", "constructor_declaration":
			leaf := childFieldText(n, "name", src)
			name := leaf
			if enclosingType != "" && leaf != "" {
				name = enclosingType + "." + leaf
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), enclosingType, name)
			}
			return
		case "invocation_expression":
			if key, pat := csharpInvocationConfigKey(n, src); key != "" {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: enclosing, Pattern: pat})
			}
		case "element_access_expression":
			if key := csharpElementAccessConfigKey(n, src); key != "" {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: enclosing, Pattern: "configuration_indexer"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosingType, enclosing)
		}
	}
	walk(root, "", "")

	extractor.EmitConfigReads(entities, "csharp", reads)
}

// csharpInvocationConfigKey returns the literal config key + detector label for
// a supported method invocation, or ("","") otherwise.
//
//	<cfg>.GetValue<T>("k") / <cfg>.GetValue("k")       → "get_value"
//	<cfg>.GetConnectionString("name")                  → "get_connection_string"
//	<cfg>.GetSection("k") / <cfg>.GetRequiredSection   → "get_section"
//	Environment.GetEnvironmentVariable("X")            → "env_var"
func csharpInvocationConfigKey(call ts.Node, src []byte) (string, string) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "member_access_expression" {
		return "", ""
	}
	nameNode := fn.ChildByFieldName("name")
	if nameNode == nil {
		return "", ""
	}
	// `GetValue<int>(...)` parses the name as a generic_name; the method
	// identifier is its leading identifier child.
	method := nodeText(nameNode, src)
	if nameNode.Type() == "generic_name" {
		for i := 0; i < int(nameNode.ChildCount()); i++ {
			ch := nameNode.Child(i)
			if ch != nil && ch.Type() == "identifier" {
				method = nodeText(ch, src)
				break
			}
		}
	}
	obj := fn.ChildByFieldName("expression")
	recv := ""
	if obj != nil {
		recv = strings.TrimSpace(nodeText(obj, src))
	}

	pattern := ""
	switch method {
	case "GetValue", "GetSection", "GetRequiredSection", "GetConnectionString":
		// Gate on a receiver that looks like an IConfiguration handle.
		if csharpLooksLikeConfig(recv) {
			switch method {
			case "GetConnectionString":
				pattern = "get_connection_string"
			case "GetValue":
				pattern = "get_value"
			default:
				pattern = "get_section"
			}
		}
	case "GetEnvironmentVariable":
		if recv == "Environment" || strings.HasSuffix(recv, ".Environment") {
			pattern = "env_var"
		}
	}
	if pattern == "" {
		return "", ""
	}

	key := csharpFirstStringArg(call.ChildByFieldName("arguments"), src)
	if key == "" {
		return "", ""
	}
	return key, pattern
}

// csharpElementAccessConfigKey returns the config key for a
// Configuration["KEY"] / _config["KEY"] element-access expression with a
// literal string index, or "".
func csharpElementAccessConfigKey(n ts.Node, src []byte) string {
	exprNode := n.ChildByFieldName("expression")
	if exprNode == nil {
		// expression is the first child when the field is unnamed.
		if n.NamedChildCount() > 0 {
			exprNode = n.NamedChild(0)
		}
	}
	if exprNode == nil {
		return ""
	}
	recv := strings.TrimSpace(nodeText(exprNode, src))
	if !csharpLooksLikeConfig(recv) {
		return ""
	}
	// The subscript lives in the bracketed_argument_list child.
	var bracket ts.Node
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if n.NamedChild(i).Type() == "bracketed_argument_list" {
			bracket = n.NamedChild(i)
			break
		}
	}
	if bracket == nil {
		return ""
	}
	return csharpFirstStringArg(bracket, src)
}

// csharpLooksLikeConfig reports whether recv looks like an IConfiguration
// handle. We accept the canonical names plus common field/property idioms
// (Configuration, _configuration, _config, config, configuration) so the
// detection stays precise without cross-file type resolution.
func csharpLooksLikeConfig(recv string) bool {
	if recv == "" || strings.ContainsAny(recv, "().[]{}") {
		// Strip a leading "this." so `this._config` still matches.
		trimmed := strings.TrimPrefix(recv, "this.")
		if trimmed == recv || strings.ContainsAny(trimmed, "().[]{}") {
			return false
		}
		recv = trimmed
	}
	switch recv {
	case "Configuration", "configuration", "_configuration",
		"_config", "config", "Config", "Settings", "_settings":
		return true
	}
	return strings.HasSuffix(recv, "Configuration") || strings.HasSuffix(recv, "configuration")
}

// csharpFirstStringArg returns the literal content of the first string argument
// in an argument_list / bracketed_argument_list node, or "" when the first
// argument is missing or non-literal (dynamic) — honest-partial.
func csharpFirstStringArg(args ts.Node, src []byte) string {
	if args == nil {
		return ""
	}
	for i := 0; i < int(args.NamedChildCount()); i++ {
		arg := args.NamedChild(i)
		if arg.Type() != "argument" {
			continue
		}
		// Unwrap the argument to its inner expression.
		inner := arg
		if arg.NamedChildCount() == 1 {
			inner = arg.NamedChild(0)
		}
		if s := csharpStringLiteral(inner, src); s != "" {
			return s
		}
		// First positional argument is non-literal → dynamic, skip.
		return ""
	}
	return ""
}

// csharpStringLiteral extracts the literal content of a C# string node,
// dropping the surrounding quotes. Handles regular, verbatim (@"..."), and
// interpolated strings — returns "" for an interpolated string carrying an
// `interpolation` part (dynamic), honest-partial.
func csharpStringLiteral(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	switch node.Type() {
	case "string_literal", "verbatim_string_literal", "raw_string_literal":
		raw := strings.TrimSpace(nodeText(node, src))
		return csharpUnquote(raw)
	case "interpolated_string_expression":
		// Any interpolation child => dynamic key, skip.
		for i := 0; i < int(node.NamedChildCount()); i++ {
			if node.NamedChild(i).Type() == "interpolation" {
				return ""
			}
		}
		return csharpUnquote(strings.TrimSpace(nodeText(node, src)))
	}
	return ""
}

// csharpUnquote strips a leading verbatim/interpolation marker ('@' / '$') and
// the surrounding double-quotes from a C# string literal's raw text.
func csharpUnquote(raw string) string {
	raw = strings.TrimLeft(raw, "@$")
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return raw[1 : len(raw)-1]
	}
	return raw
}
