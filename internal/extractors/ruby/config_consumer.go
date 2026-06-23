// config_consumer.go — supplemental pass that emits DEPENDS_ON_CONFIG edges
// from Ruby methods that read a configuration key to a shared config-key
// entity (issue #3641, epic #3625).
//
// Detected reader shapes (literal keys only — honest-partial):
//
//	ENV['DATABASE_URL']              → config:DATABASE_URL   (element reference)
//	ENV["DATABASE_URL"]              → config:DATABASE_URL
//	ENV.fetch('REDIS_URL')           → config:REDIS_URL      (fetch, w/ default)
//	ENV.fetch('REDIS_URL', 'x')      → config:REDIS_URL
//
// Dynamic keys — ENV[var], ENV.fetch(var) — are NOT emitted; only literal
// string keys are recorded so the graph never fabricates a missing key.
//
// Each read produces a SCOPE.Config / config_key entity (shared, file-agnostic
// node) and a DEPENDS_ON_CONFIG edge from the enclosing method (or the file
// entity at top level) to it, mirroring the Go/Java/JS config_consumer shape at
// config-KEY granularity so config:<key>'s inbound edges are the config-change
// blast radius.

package ruby

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// emitConfigConsumerEdges scans every method / singleton_method body (and the
// top level) for config-read shapes and appends config-key entities +
// DEPENDS_ON_CONFIG edges to *entities.
//
// (*entities)[0] MUST be the file entity. Mutates *entities in place. Safe with
// nil / empty input.
func emitConfigConsumerEdges(root ts.Node, src []byte, entities *[]types.EntityRecord) {
	if root == nil || entities == nil || len(*entities) == 0 {
		return
	}

	var reads []extractor.ConfigRead

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
		if n == nil {
			return
		}
		switch n.Type() {
		case "method", "singleton_method":
			// Method bodies attach reads to the bare method Name (matching the
			// entity Name emitted by buildMethod).
			name := childFieldText(n, "name", src)
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i), name)
			}
			return
		case "element_reference":
			// ENV['KEY'] / ENV["KEY"].
			if key := rubyEnvElementKey(n, src); key != "" {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: enclosing, Pattern: "env"})
			}
		case "call":
			// ENV.fetch('KEY' [, default]).
			if key := rubyEnvFetchKey(n, src); key != "" {
				reads = append(reads, extractor.ConfigRead{Key: key, FromName: enclosing, Pattern: "env_fetch"})
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i), enclosing)
		}
	}
	walk(root, "")

	extractor.EmitConfigReads(entities, "ruby", reads)
}

// rubyEnvElementKey returns the config key for an ENV['KEY'] element reference
// with a literal string index, or "" otherwise.
//
// tree-sitter-ruby shape: (element_reference object: (constant) (string ...)).
func rubyEnvElementKey(n ts.Node, src []byte) string {
	obj := n.ChildByFieldName("object")
	if obj == nil || strings.TrimSpace(rubyNodeText(obj, src)) != "ENV" {
		return ""
	}
	// The index is the first string argument child after the object.
	for i := 0; i < int(n.NamedChildCount()); i++ {
		ch := n.NamedChild(i)
		if ch == obj {
			continue
		}
		if ch.Type() == "string" {
			return rubyStringContent(ch, src)
		}
		// Non-literal index (variable / expression) → dynamic, skip.
		return ""
	}
	return ""
}

// rubyEnvFetchKey returns the config key for an ENV.fetch('KEY' [, default])
// call with a literal first string argument, or "" otherwise.
func rubyEnvFetchKey(n ts.Node, src []byte) string {
	recv := n.ChildByFieldName("receiver")
	method := n.ChildByFieldName("method")
	if recv == nil || method == nil {
		return ""
	}
	if strings.TrimSpace(rubyNodeText(recv, src)) != "ENV" {
		return ""
	}
	if rubyNodeText(method, src) != "fetch" {
		return ""
	}
	args := n.ChildByFieldName("arguments")
	if args == nil {
		return ""
	}
	first := args.NamedChild(0)
	if first == nil || first.Type() != "string" {
		return "" // no literal first arg → dynamic, skip
	}
	return rubyStringContent(first, src)
}

// rubyStringContent extracts the literal content of a tree-sitter-ruby string
// node, dropping the surrounding quotes. Returns "" for an interpolated string
// (one carrying an `interpolation` child) — honest-partial, no fabrication.
func rubyStringContent(strNode ts.Node, src []byte) string {
	for i := 0; i < int(strNode.NamedChildCount()); i++ {
		if strNode.NamedChild(i).Type() == "interpolation" {
			return "" // dynamic content → skip
		}
	}
	raw := rubyNodeText(strNode, src)
	raw = strings.TrimSpace(raw)
	if len(raw) >= 2 {
		q := raw[0]
		if (q == '"' || q == '\'') && raw[len(raw)-1] == q {
			return raw[1 : len(raw)-1]
		}
	}
	return raw
}

// rubyNodeText returns the raw source text spanned by node.
func rubyNodeText(node ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	return string(src[node.StartByte():node.EndByte()])
}
