// config_consumer.go — supplemental pass that emits DEPENDS_ON_CONFIG edges
// from JS/TS code that reads a configuration key to a shared config-key entity
// (issue #3641, epic #3625).
//
// Detected reader shapes (literal keys only — honest-partial):
//
//	process.env.API_URL              → config:API_URL
//	process.env['API_URL']           → config:API_URL
//	import.meta.env.VITE_X           → config:VITE_X      (Vite)
//	config.get('db.host')            → config:db.host     (node-config)
//
// Dynamic keys — process.env[varName], config.get(someVar) — are NOT emitted;
// only literal keys are recorded so the graph never fabricates a missing key.
//
// Each read produces a SCOPE.Config/config_key entity (shared, file-agnostic
// node) and a DEPENDS_ON_CONFIG edge from the enclosing function / component /
// method (or the file entity at module scope) to it, mirroring the Python
// config_consumer shape at config-KEY granularity so config:<key>'s inbound
// edges form the config-change blast radius.

package javascript

import (
	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// emitConfigConsumerEdges scans the AST for config-read shapes and appends
// config-key entities + DEPENDS_ON_CONFIG edges to x.entities. x.entities[0]
// MUST be the file entity. Safe with an empty tree.
func (x *extractor) emitConfigConsumerEdges(root ts.Node) {
	if root == nil || len(x.entities) == 0 {
		return
	}

	var reads []extreg.ConfigRead

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_declaration", "generator_function_declaration":
			name := x.nodeText(n.ChildByFieldName("name"))
			if body := n.ChildByFieldName("body"); body != nil {
				walk(body, name)
			}
			return
		case "method_definition":
			name := x.nodeText(n.ChildByFieldName("name"))
			if body := n.ChildByFieldName("body"); body != nil {
				walk(body, name)
			}
			return
		case "variable_declarator":
			// const Foo = () => {...}  /  const handler = function() {...}
			nameNode := n.ChildByFieldName("name")
			valNode := n.ChildByFieldName("value")
			if nameNode != nil && valNode != nil {
				vt := valNode.Type()
				if vt == "arrow_function" || vt == "function" || vt == "function_expression" {
					name := x.nodeText(nameNode)
					if body := valNode.ChildByFieldName("body"); body != nil {
						walk(body, name)
						return
					}
				}
			}
		case "member_expression":
			if key := jsMemberConfigKey(x, n); key != "" {
				reads = append(reads, extreg.ConfigRead{Key: key, FromName: enclosing, Pattern: "process_env"})
			}
		case "subscript_expression":
			if key := jsSubscriptConfigKey(x, n); key != "" {
				reads = append(reads, extreg.ConfigRead{Key: key, FromName: enclosing, Pattern: "process_env"})
			}
		case "call_expression":
			if key := jsConfigGetKey(x, n); key != "" {
				reads = append(reads, extreg.ConfigRead{Key: key, FromName: enclosing, Pattern: "node_config"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extreg.EmitConfigReads(&x.entities, x.language, reads)
}

// jsMemberConfigKey returns the config key for process.env.KEY or
// import.meta.env.KEY member expressions, or "" otherwise.
func jsMemberConfigKey(x *extractor, n ts.Node) string {
	obj := n.ChildByFieldName("object")
	prop := n.ChildByFieldName("property")
	if obj == nil || prop == nil || prop.Type() != "property_identifier" {
		return ""
	}
	objText := x.nodeText(obj)
	// process.env.KEY  or  import.meta.env.KEY
	if objText == "process.env" || objText == "import.meta.env" {
		return x.nodeText(prop)
	}
	return ""
}

// jsSubscriptConfigKey returns the config key for process.env['KEY'] /
// import.meta.env['KEY'] subscript expressions with a literal string index.
func jsSubscriptConfigKey(x *extractor, n ts.Node) string {
	obj := n.ChildByFieldName("object")
	idx := n.ChildByFieldName("index")
	if obj == nil || idx == nil {
		return ""
	}
	objText := x.nodeText(obj)
	if objText != "process.env" && objText != "import.meta.env" {
		return ""
	}
	if idx.Type() != "string" {
		return "" // dynamic index → honest-partial skip
	}
	return jsStripString(x.nodeText(idx))
}

// jsConfigGetKey returns the config key for a node-config `config.get('k')` /
// `config.has('k')` call with a literal string argument, or "".
func jsConfigGetKey(x *extractor, call ts.Node) string {
	fn := call.ChildByFieldName("function")
	args := call.ChildByFieldName("arguments")
	if fn == nil || args == nil || fn.Type() != "member_expression" {
		return ""
	}
	obj := fn.ChildByFieldName("object")
	prop := fn.ChildByFieldName("property")
	if obj == nil || prop == nil {
		return ""
	}
	if x.nodeText(obj) != "config" {
		return ""
	}
	method := x.nodeText(prop)
	if method != "get" && method != "has" {
		return ""
	}
	first := args.NamedChild(0)
	if first == nil || first.Type() != "string" {
		return "" // no literal first arg → dynamic, skip
	}
	return jsStripString(x.nodeText(first))
}

// jsStripString removes surrounding quotes from a JS string node's text.
func jsStripString(s string) string {
	if len(s) >= 2 {
		q := s[0]
		if (q == '"' || q == '\'' || q == '`') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}
