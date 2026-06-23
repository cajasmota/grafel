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
//	cfg.get_string("db.host")       → config:db.host       (config_crate)
//	cfg.get_int("db.port")          → config:db.port       (config_crate)
//	cfg.get::<T>("db.host")         → config:db.host       (config_crate)
//
// The `config` crate (#5079) surfaces literal keys only at the typed getter
// call sites on a built Config — `get_string` / `get_int` / `get_bool` /
// `get_float` / the turbofish `get::<T>` — so those receiver-bound getters are
// recognised at config-KEY granularity like the env shapes. The keyless builder
// step itself (`Config::builder().add_source(...).build()`) carries no literal
// key and is intentionally NOT emitted.
//
// `dotenvy::dotenv()` only loads the .env file; the actual key reads still go
// through `env::var("KEY")` (or `dotenvy::var("KEY")`), so the env_var /
// dotenvy shapes above are what carry the literal keys.
//
// Dynamic keys — env::var(name), env::var(format!(...)) — are NOT emitted; we
// only record string-literal keys so the graph never fabricates a key that
// doesn't exist in the config. The truly keyless crate APIs — `envy::
// from_env::<T>()` (whole-struct env deserialisation) and `Figment::new().
// merge(...).extract::<T>()` (typed struct extract) — carry no single literal
// key (they would require walking the target struct's fields + serde renames)
// and remain deferred (#5079 follow-up). The `config` crate's literal keys DO
// surface at its typed getter call sites (get_string / get::<T>(...)), which
// this pass now records (#5079).
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
	// Fast guard: the file must mention an env/config read idiom.
	s := string(src)
	if !strings.Contains(s, "env::var") && !strings.Contains(s, "var_os") &&
		!strings.Contains(s, "dotenvy::var") && !strings.Contains(s, "Env::prefixed") &&
		!strings.Contains(s, ".get") {
		return
	}

	var reads []extractor.ConfigRead

	var walk func(n ts.Node, enclosing string)
	walk = func(n ts.Node, enclosing string) {
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
func rustImplOwnerName(fn ts.Node, src []byte) string {
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
func rustConfigKeyFromCall(call ts.Node, src []byte) (string, string) {
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
		// Receiver-bound config-crate getter: cfg.get_string("k") /
		// .get_int / .get_bool / .get_float. `Env::prefixed` parses as a
		// scoped_identifier (handled above), not a field_expression. The bare
		// `.get("k")` (HashMap/BTreeMap etc.) is intentionally excluded — only
		// the config-crate-specific typed getter names qualify.
		if rustIsConfigGetter(fieldName(fn, src)) {
			pattern = "config_crate"
		}
	case "generic_function":
		// Turbofish getter: cfg.get::<MyType>("k"). The inner function is a
		// field_expression whose field is the getter method name. The
		// turbofish `get::<T>` on a config Config is config-crate idiomatic, so
		// it qualifies in addition to the typed get_* names.
		if inner := fn.ChildByFieldName("function"); inner != nil &&
			inner.Type() == "field_expression" {
			if m := fieldName(inner, src); m == "get" || rustIsConfigGetter(m) {
				pattern = "config_crate"
			}
		}
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

// fieldName returns the method/field name of a field_expression's `field`
// child, or "" when absent.
func fieldName(fieldExpr ts.Node, src []byte) string {
	if fieldExpr == nil {
		return ""
	}
	if name := fieldExpr.ChildByFieldName("field"); name != nil {
		return strings.TrimSpace(string(src[name.StartByte():name.EndByte()]))
	}
	return ""
}

// rustIsConfigGetter reports whether method is one of the `config` crate's
// typed key getters that take a literal key as their first argument:
// get_string / get_int / get_bool / get_float / get_table / get_array. These
// getter names are config-crate specific enough — combined with a literal
// string first argument — to record honestly. The bare `get` is excluded here
// (it collides with HashMap::get); only the turbofish `get::<T>` form is
// admitted, at its call site, as config-crate idiomatic.
func rustIsConfigGetter(method string) bool {
	switch method {
	case "get_string", "get_int", "get_bool",
		"get_float", "get_table", "get_array":
		return true
	}
	return false
}
