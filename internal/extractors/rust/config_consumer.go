// config_consumer.go — supplemental pass that emits DEPENDS_ON_CONFIG edges
// from Rust functions / methods that read a configuration key to a shared
// config-key entity (issue #5020, follow-up from #4965; epic #3641/#3625).
//
// Detected reader shapes (literal keys only — honest-partial):
//
//	env::var("KEY")                 → config:KEY            (env_var)
//	std::env::var("KEY")            → config:KEY            (env_var)
//	env::var_os("KEY")              → config:KEY            (env_var)
//	dotenvy::var("KEY")             → config:KEY            (dotenvy)
//	Env::prefixed("APP_")           → config:APP_          (figment)
//
// `dotenvy::dotenv()` only loads the .env file; the actual key reads still go
// through `env::var("KEY")` (or `dotenvy::var("KEY")`), so the env_var /
// dotenvy shapes above are what carry the literal keys.
//
// Dynamic keys — env::var(name), env::var(format!(...)) — are NOT emitted; we
// only record string-literal keys so the graph never fabricates a key that
// doesn't exist in the config. Keyless crate APIs (`envy::from_env::<T>()`,
// `config::Config::builder()`) deserialise a whole struct / merge sources with
// no single literal key and are deferred (see PR body).
//
// Each detected read produces:
//   - a SCOPE.Config / config_key entity (shared, file-agnostic node) via
//     extractor.EmitConfigReads, and
//   - a DEPENDS_ON_CONFIG edge from the enclosing function/method to it,
//
// mirroring the Go/Java/PHP/Python config_consumer shape at config-KEY
// granularity (one node per key) so the inbound edge set of config:<key> is the
// config-change blast radius.

package rust

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// emitConfigConsumerEdges scans every function / method body for config-read
// shapes and appends config-key entities + DEPENDS_ON_CONFIG edges to records.
//
// records[0] MUST be the file entity. Mutates *records in place. Safe with
// nil / empty input.
func emitConfigConsumerEdges(root *sitter.Node, src []byte, records *[]types.EntityRecord) {
	if root == nil || records == nil || len(*records) == 0 {
		return
	}
	// Fast guard: the file must mention an env/config read idiom.
	s := string(src)
	if !strings.Contains(s, "env::var") && !strings.Contains(s, "var_os") &&
		!strings.Contains(s, "dotenvy::var") && !strings.Contains(s, "Env::prefixed") {
		return
	}

	var reads []extractor.ConfigRead

	var walk func(n *sitter.Node, enclosing string)
	walk = func(n *sitter.Node, enclosing string) {
		if n == nil {
			return
		}
		if n.Type() == "function_item" {
			leaf := childFieldText(n, "name", src)
			owner := rustImplOwnerName(n, src)
			name := leaf
			if owner != "" && leaf != "" {
				name = owner + "." + leaf
			}
			if body := n.ChildByFieldName("body"); body != nil {
				for i := 0; i < int(body.ChildCount()); i++ {
					walk(body.Child(i), name)
				}
			}
			return
		}
		if n.Type() == "call_expression" {
			if key, pat := rustConfigKeyFromCall(n, src); key != "" {
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

	extractor.EmitConfigReads(records, "rust", reads)
}

// rustImplOwnerName returns the type name of the enclosing `impl Foo { ... }`
// block for a function_item, or "" when the function is free-standing. This
// makes a method's FromName "Foo.method", matching the receiver-qualified names
// the main walk emits for SCOPE.Operation methods so the edge attaches to the
// right host.
func rustImplOwnerName(fn *sitter.Node, src []byte) string {
	for p := fn.Parent(); p != nil; p = p.Parent() {
		if p.Type() == "impl_item" {
			if t := p.ChildByFieldName("type"); t != nil {
				name := strings.TrimSpace(string(src[t.StartByte():t.EndByte()]))
				if idx := strings.IndexAny(name, "<"); idx >= 0 {
					name = strings.TrimSpace(name[:idx])
				}
				return name
			}
			return ""
		}
	}
	return ""
}

// rustConfigKeyFromCall returns the literal config key + detector label when the
// call_expression matches a supported config-read shape, or ("","") otherwise.
//
// Supported:
//
//	env::var("KEY") / std::env::var("KEY") / env::var_os("KEY")  → "env_var"
//	dotenvy::var("KEY")                                          → "dotenvy"
//	Env::prefixed("PREFIX")                                      → "figment"
func rustConfigKeyFromCall(call *sitter.Node, src []byte) (string, string) {
	fn := call.ChildByFieldName("function")
	if fn == nil && call.ChildCount() > 0 {
		fn = call.Child(0)
	}
	if fn == nil {
		return "", ""
	}

	pattern := ""
	switch fn.Type() {
	case "scoped_identifier":
		// env::var, std::env::var, env::var_os, dotenvy::var
		raw := strings.TrimSpace(string(src[fn.StartByte():fn.EndByte()]))
		switch raw {
		case "env::var", "std::env::var", "core::env::var",
			"env::var_os", "std::env::var_os":
			pattern = "env_var"
		case "dotenvy::var", "dotenv::var":
			pattern = "dotenvy"
		case "Env::prefixed", "figment::providers::Env::prefixed":
			pattern = "figment"
		}
	case "field_expression":
		// Receiver-form figment provider: `Env::prefixed` parses as a scoped
		// identifier, so field_expression is not expected here; left for
		// future receiver-bound shapes.
	}
	if pattern == "" {
		return "", ""
	}

	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() == 0 {
		return "", ""
	}
	first := args.NamedChild(0)
	if first == nil {
		return "", ""
	}
	key := rustLiteralText(first, src)
	if key == "" {
		return "", ""
	}
	return key, pattern
}
