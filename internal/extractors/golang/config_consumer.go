// config_consumer.go — supplemental pass that emits DEPENDS_ON_CONFIG edges
// from Go functions / methods that read a configuration key to a shared
// config-key entity (issue #3641, epic #3625).
//
// Detected reader shapes (literal keys only — honest-partial):
//
//	os.Getenv("KEY")
//	os.LookupEnv("KEY")
//	viper.GetString("key")          (and GetBool/GetInt/.../Get)
//	v.GetString("key")              (any receiver named after a viper.Viper)
//
// Dynamic keys — os.Getenv(varName), viper.GetString(buildKey()) — are NOT
// emitted; we only record string-literal keys so the graph never fabricates a
// key that doesn't exist in the config.
//
// Each detected read produces:
//   - a SCOPE.Config / config_key entity (shared, file-agnostic node) via
//     extractor.EmitConfigReads, and
//   - a DEPENDS_ON_CONFIG edge from the enclosing function/method to it.
//
// Mirrors the Python config_consumer DEPENDS_ON_CONFIG shape for cross-language
// consistency, but at config-KEY granularity (one node per key) so the inbound
// edge set of config:<key> is the config-change blast radius.

package golang

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitConfigConsumerEdges scans every function / method body for config-read
// shapes and appends config-key entities + DEPENDS_ON_CONFIG edges to records.
//
// records[0] MUST be the file entity. Mutates *records in place. Safe with
// nil / empty input.
func emitConfigConsumerEdges(root ts.Node, src []byte, records *[]types.EntityRecord) {
	if root == nil || records == nil || len(*records) == 0 {
		return
	}

	var reads []extractor.ConfigRead

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "function_declaration":
			name := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				name = nodeText(nn, src)
			}
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), name)
				}
			}
			return
		case "method_declaration":
			leaf := ""
			if nn := n.ChildByFieldName("name"); nn != nil {
				leaf = nodeText(nn, src)
			}
			recv := receiverTypeName(n.ChildByFieldName("receiver"), src)
			name := leaf
			if recv != "" {
				name = recv + "." + leaf
			}
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), name)
				}
			}
			return
		case "call_expression":
			if key, pat := goConfigKeyFromCall(n, src); key != "" {
				reads = append(reads, extractor.ConfigRead{
					Key:      key,
					FromName: enclosing,
					Pattern:  pat,
				})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extractor.EmitConfigReads(records, "go", reads)
}

// viperGetMethods are the viper.Viper getter methods that take a config key as
// their first string-literal argument.
var viperGetMethods = map[string]bool{
	"Get":            true,
	"GetString":      true,
	"GetBool":        true,
	"GetInt":         true,
	"GetInt32":       true,
	"GetInt64":       true,
	"GetUint":        true,
	"GetUint16":      true,
	"GetUint32":      true,
	"GetUint64":      true,
	"GetFloat64":     true,
	"GetDuration":    true,
	"GetTime":        true,
	"GetStringSlice": true,
	"GetIntSlice":    true,
	"GetStringMap":   true,
	"GetSizeInBytes": true,
}

// goConfigKeyFromCall returns the literal config key + detector label when the
// call_expression matches a supported config-read shape, or ("","") otherwise.
//
// Supported:
//
//	os.Getenv("KEY") / os.LookupEnv("KEY")  → label "os_getenv"
//	<recv>.GetString("key") (viper-family)   → label "viper"
func goConfigKeyFromCall(call ts.Node, src []byte) (string, string) {
	fn := call.ChildByFieldName("function")
	args := call.ChildByFieldName("arguments")
	if fn == nil || args == nil || fn.Type() != "selector_expression" {
		return "", ""
	}
	operand := fn.ChildByFieldName("operand")
	field := fn.ChildByFieldName("field")
	if operand == nil || field == nil {
		return "", ""
	}
	recv := strings.TrimSpace(nodeText(operand, src))
	method := nodeText(field, src)

	pattern := ""
	switch {
	case recv == "os" && (method == "Getenv" || method == "LookupEnv"):
		pattern = "os_getenv"
	case viperGetMethods[method] && recv != "":
		// viper.GetString(...) or any v.GetString(...) where v is a *viper.Viper.
		// We can't statically prove the receiver type per-file without flow
		// analysis, so we gate on the literal package name "viper" OR a single
		// short receiver identifier (the common `v`, `cfg`, `conf`) to stay
		// precise. The literal-string-arg requirement below filters the rest.
		if recv == "viper" || isLikelyViperReceiver(recv) {
			pattern = "viper"
		}
	}
	if pattern == "" {
		return "", ""
	}

	// First argument MUST be a string literal — dynamic keys are skipped.
	if args.NamedChildCount() == 0 {
		return "", ""
	}
	first := args.NamedChild(0)
	if first == nil || first.Type() != "interpreted_string_literal" {
		return "", ""
	}
	key := goStripStringLiteral(nodeText(first, src))
	if key == "" {
		return "", ""
	}
	return key, pattern
}

// isLikelyViperReceiver returns true for short, lowercase single-identifier
// receivers commonly bound to a *viper.Viper (v, cfg, conf, config, vp). This
// keeps the viper getter detection precise without cross-file type resolution.
func isLikelyViperReceiver(recv string) bool {
	if strings.ContainsAny(recv, ".[](){}") {
		return false
	}
	switch recv {
	case "v", "vp", "cfg", "conf", "config", "viper":
		return true
	}
	return false
}

// goStripStringLiteral removes the surrounding double-quotes / backticks from a
// Go string literal node's text.
func goStripStringLiteral(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
