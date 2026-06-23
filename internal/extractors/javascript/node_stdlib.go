// Package javascript — Node.js stdlib receiver classification (Refs #44).
//
// Background
// ----------
// The receiver-typed CALLS path in receiver.go (issue #421) only fires when
// the receiver's static type resolves to a relative import (project-internal
// file). Node.js stdlib calls are the dominant residual bug-extractor cohort
// in TS/JS scripts (e.g. starter-workflows at ~12.42%):
//
//	import * as path from "path";
//	import fs from "node:fs";
//
//	function f() { return path.join(a, b); }      // bare CALLS -> "join"
//	function g() { return fs.readFileSync(p); }   // bare CALLS -> "readFileSync"
//
// Both calls land in the bug-extractor because (a) the receiver-typed path
// requires a relative import and (b) the bare leaf names ("join",
// "readFileSync", ...) are deliberately omitted from `jsBareNames` to avoid
// collisions with user method names in other ecosystems.
//
// Fix
// ---
// When the callee is a member_expression `<recv>.<method>` and `<recv>` is a
// bare identifier whose IMPORTS binding maps to a known Node.js stdlib
// module spec (`"path"`, `"fs"`, `"node:path"`, ...), emit a cross-language
// "external" structural-ref stub keyed on the canonical `node:<module>`
// name. The synth pass (`internal/external/synth.go`'s `:external:` branch)
// canonicalises that to the `node:<module>` allowlist key and routes the
// edge to a single `ext:node:<module>` placeholder — matching the per-
// module collapse used by Go stdlib interface dispatch (#364) and the
// cross-language external-import branch.
//
// Same shape covers the bare-named-import case
// (`import { join } from "path"`); the call appears as a bare identifier
// `join` and the import binding still maps `join` → `"path"`. That branch
// is exercised by classifyBareNodeStdlibCall.
//
// Conservative bias
// -----------------
// The allowlist below contains ONLY canonical Node.js stdlib module specs.
// User-named imports from third-party packages (`import { join } from
// "lodash"`) do not match (`"lodash"` isn't in the set), so the existing
// bare-name / receiver-type collisions are unchanged. Mis-classification
// risk is bounded by the import spec; we never inspect method names.
package javascript

import (
	"strings"

	"github.com/cajasmota/grafel/internal/treesitter/ts"

	extreg "github.com/cajasmota/grafel/internal/extractor"
)

// nodeStdlibModules is the set of import specifiers recognised as Node.js
// stdlib modules. Both the bare form (`"path"`, `"fs"`) and the
// `node:`-prefixed form (`"node:path"`, `"node:fs"`) are accepted; both
// canonicalise to the same `node:<module>` placeholder key (which exists
// in `knownExternalPackages` for the `node:`-prefixed form and as the bare
// module name for the unprefixed form — synth's case-folded allowlist
// lookup accepts either).
//
// Membership tracks the Node.js documented stdlib set as of 22.x LTS,
// minus already-handled cases via the existing `knownExternalPackages`
// path (no removals — duplication is fine, the lookup is a set check).
var nodeStdlibModules = map[string]struct{}{
	// Core I/O and process.
	"fs":             {},
	"fs/promises":    {},
	"path":           {},
	"path/posix":     {},
	"path/win32":     {},
	"os":             {},
	"process":        {},
	"child_process":  {},
	"cluster":        {},
	"worker_threads": {},

	// Network.
	"http":         {},
	"https":        {},
	"http2":        {},
	"net":          {},
	"tls":          {},
	"dgram":        {},
	"dns":          {},
	"dns/promises": {},

	// Streams / events / buffers.
	"stream":           {},
	"stream/promises":  {},
	"stream/web":       {},
	"stream/consumers": {},
	"events":           {},
	"buffer":           {},
	"string_decoder":   {},

	// Utilities.
	"util":            {},
	"util/types":      {},
	"querystring":     {},
	"url":             {},
	"crypto":          {},
	"zlib":            {},
	"assert":          {},
	"assert/strict":   {},
	"perf_hooks":      {},
	"timers":          {},
	"timers/promises": {},

	// Runtime/diagnostics.
	"v8":                {},
	"vm":                {},
	"module":            {},
	"readline":          {},
	"readline/promises": {},
	"repl":              {},
	"tty":               {},
	"inspector":         {},
	"trace_events":      {},
	"console":           {},
	"punycode":          {},
	// `async_hooks`, `diagnostics_channel` deliberately omitted: not on
	// the synth allowlist as bare names; can be added later via synth's
	// knownExternalPackages if they become hot in real corpora.

	// node: prefixed forms — same modules, explicit prefix.
	"node:fs":                {},
	"node:fs/promises":       {},
	"node:path":              {},
	"node:path/posix":        {},
	"node:path/win32":        {},
	"node:os":                {},
	"node:process":           {},
	"node:child_process":     {},
	"node:cluster":           {},
	"node:worker_threads":    {},
	"node:http":              {},
	"node:https":             {},
	"node:http2":             {},
	"node:net":               {},
	"node:tls":               {},
	"node:dgram":             {},
	"node:dns":               {},
	"node:dns/promises":      {},
	"node:stream":            {},
	"node:stream/promises":   {},
	"node:stream/web":        {},
	"node:stream/consumers":  {},
	"node:events":            {},
	"node:buffer":            {},
	"node:string_decoder":    {},
	"node:util":              {},
	"node:util/types":        {},
	"node:querystring":       {},
	"node:url":               {},
	"node:crypto":            {},
	"node:zlib":              {},
	"node:assert":            {},
	"node:assert/strict":     {},
	"node:perf_hooks":        {},
	"node:timers":            {},
	"node:timers/promises":   {},
	"node:v8":                {},
	"node:vm":                {},
	"node:module":            {},
	"node:readline":          {},
	"node:readline/promises": {},
	"node:repl":              {},
	"node:tty":               {},
	"node:inspector":         {},
	"node:trace_events":      {},
	"node:console":           {},
	"node:punycode":          {},
}

// nodeStdlibCanonical returns the canonical `node:<module>` key for a raw
// Node.js stdlib import specifier, or "" if the spec is not in the
// allowlist. Both the bare form ("path") and prefixed form ("node:path")
// canonicalise to the same `node:<module>` shape so the downstream synth
// allowlist sees a single key per module.
//
// Sub-path forms ("fs/promises", "node:stream/web") preserve the full
// suffix in the canonical key — `node:fs/promises` and `node:fs` are
// distinct placeholders. The synth allowlist uses a case-folded prefix-
// or-equal match that accepts both shapes.
func nodeStdlibCanonical(spec string) string {
	if spec == "" {
		return ""
	}
	if _, ok := nodeStdlibModules[spec]; !ok {
		return ""
	}
	// Collapse sub-path forms (`fs/promises`, `stream/web`) to the root
	// module (`node:fs`, `node:stream`). The synth allowlist keys on the
	// root and the `:external:` branch rejects anything containing a
	// slash, so sub-paths must be stripped here.
	canonical := spec
	if !strings.HasPrefix(canonical, "node:") {
		canonical = "node:" + canonical
	}
	if i := strings.IndexByte(canonical, '/'); i > 0 {
		canonical = canonical[:i]
	}
	return canonical
}

// receiverNodeStdlibTarget resolves a member_expression call
// (`<recv>.<method>`) to a cross-language `:external:node:<module>`
// structural-ref stub when the receiver is a bare identifier bound to a
// Node.js stdlib import. Returns "" on any miss; the caller falls back to
// the bare method name (or whatever other resolution paths it tries).
//
// Supported receiver shapes (matches receiver.go's receiverIdent
// conservatism):
//
//   - bare identifier `<recv>`        — typed via importByLocal[<recv>]
//
// `this.<field>` is NOT supported here: Node.js stdlib imports go into
// the file scope, not into class fields. The receiver-binder in
// receiver.go covers the typed-class-field shape independently.
func (x *extractor) receiverNodeStdlibTarget(memberExpr ts.Node, method string) string {
	if memberExpr == nil || method == "" {
		return ""
	}
	obj := memberExpr.ChildByFieldName("object")
	if obj == nil {
		return ""
	}
	// Bare identifier only. `this.<field>` shapes never bind to a
	// top-level import.
	if obj.Type() != "identifier" {
		return ""
	}
	recvName := x.nodeText(obj)
	if recvName == "" {
		return ""
	}
	binding, ok := x.importByLocal[recvName]
	if !ok || binding == nil {
		return ""
	}
	canonical := nodeStdlibCanonical(binding.importPath)
	if canonical == "" {
		return ""
	}
	return nodeStdlibExternalRef(x.language, canonical, method)
}

// classifyBareNodeStdlibCall handles the bare-named-import shape:
// `import { join } from "path"` followed by `join(...)`. The call has no
// member_expression so receiverNodeStdlibTarget does not apply, but the
// import binding still maps `join` → `"path"` and the leaf identifier is
// in scope. Returns the cross-language external ref stub on a hit, "" on
// a miss.
func (x *extractor) classifyBareNodeStdlibCall(name string) string {
	if name == "" {
		return ""
	}
	binding, ok := x.importByLocal[name]
	if !ok || binding == nil {
		return ""
	}
	canonical := nodeStdlibCanonical(binding.importPath)
	if canonical == "" {
		return ""
	}
	return nodeStdlibExternalRef(x.language, canonical, name)
}

// nodeStdlibExternalRef builds the cross-language `:external:` structural-
// ref stub that synth.go's classifyExternal recognises and canonicalises
// to the trailing segment after `:external:`. The leading prefix mirrors
// the operation-method shape so the stub passes the resolver's
// 6-segment check without confusing the per-language extractor inverse.
//
// Shape: `scope:operation:method:<lang>:external:<canonical>` where
// `<canonical>` is the `node:<module>` key. The trailing method name is
// dropped because the synth path collapses every stub to a single
// `ext:node:<module>` placeholder per module (matching the Go stdlib
// dispatch in #364 and the per-package import collapse elsewhere).
func nodeStdlibExternalRef(lang, canonical, _method string) string {
	if lang == "" || canonical == "" {
		return ""
	}
	return "scope:operation:method:" + lang + ":external:" + canonical
}

// Compile-time hint that the extreg import is intentional even if a future
// refactor inlines the structural-ref builder.
var _ = extreg.BuildOperationStructuralRef
