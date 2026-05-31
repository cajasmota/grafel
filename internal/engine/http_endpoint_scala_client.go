// Scala consumer-side HTTP client synthesis — sttp (#3554, epic #3505).
//
// sttp (https://sttp.softwaremill.com) is the dominant Scala HTTP client. A
// request is described as a value built from `basicRequest` / `quickRequest`
// (or `emptyRequest`) with a verb combinator carrying the target URL, where the
// URL is a `uri"..."` string-interpolator literal:
//
//	val r = basicRequest.get(uri"https://api.example.com/v1/users")
//	basicRequest
//	  .body(payload)
//	  .post(uri"https://api.example.com/v1/users")
//	quickRequest.get(uri"http://catalog:8080/products/$id")
//	basicRequest.response(asJson[User]).put(uri"https://api.example.com/v1/users/$id")
//
// Before this pass any Scala service that called a downstream API via sttp
// produced no http_endpoint_call entity and no cross-repo FETCHES edge — the
// Scala side of the cross-link graph was producer-only (tapir/http4s/akka).
//
// This emits one synthetic http_endpoint_call per detected sttp verb call site
// (via the shared emitClient from applyHTTPEndpointSynthesis), so the existing
// cross-repo linker pairs them with producer-side definitions by canonical path
// Name. The `uri"..."` host is stripped (the producer serves "/v1/users"
// without the host); `$id` / `${...}` interpolations become `{id}` placeholders.
// The enclosing `def <name>` is attributed as the calling reference.
//
// Verbs: get, post, put, patch, delete, head, options. The combinator may
// appear before OR after intermediate builder combinators (.body / .response /
// .header / .auth …); we match the verb+uri pair directly so ordering on the
// chain is irrelevant.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/archigraph/internal/engine/httproutes"
)

// scSttpVerbURIRe matches an sttp verb combinator whose argument is a
// `uri"..."` interpolator literal: `.get(uri"https://host/path")`,
// `.post(uri"...")`. Group 1 = verb, group 2 = the URL body inside the uri
// interpolator (interpolation markers `$x` / `${...}` are left in place and
// canonicalised to `{x}` downstream).
var scSttpVerbURIRe = regexp.MustCompile(
	`\.(get|post|put|patch|delete|head|options)\s*\(\s*uri"([^"\n\r]*)"`,
)

// scEnclosingDefRe captures Scala method declarations for caller attribution:
// `def foo(...)`, `override def foo(...)`, `private def foo[...](...)`.
var scEnclosingDefRe = regexp.MustCompile(
	`(?m)^[ \t]*(?:(?:private|protected|final|override|implicit|lazy|\s)+\s+)?def\s+([A-Za-z_]\w*)\s*[\[(]`,
)

// scSttpInterpRe rewrites a Scala interpolation marker (`$id` or `${expr}`) in a
// `uri"..."` body into a `{id}` / `{param}` placeholder so the canonical path is
// stable across call sites.
var scSttpInterpRe = regexp.MustCompile(`\$\{[^}]*\}|\$[A-Za-z_]\w*`)

// scClientEmitFn is the runtime-aware emitter type for the Scala client.
type scClientEmitFn func(method, canonicalPath, framework, refKind, refName string, runtimeDynamic bool)

// synthesizeScalaClientWithRuntime is the package-level entry point referenced
// from applyHTTPEndpointSynthesis. Emits one outbound http_endpoint_call per
// sttp verb call site + a FETCHES edge from the enclosing def.
func synthesizeScalaClientWithRuntime(content string, emit scClientEmitFn) {
	if !strings.Contains(content, "basicRequest") &&
		!strings.Contains(content, "quickRequest") &&
		!strings.Contains(content, "emptyRequest") &&
		!strings.Contains(content, "uri\"") {
		return
	}

	funcs := indexScalaDefs(content)

	for _, m := range scSttpVerbURIRe.FindAllStringSubmatchIndex(content, -1) {
		verb := strings.ToUpper(content[m[2]:m[3]])
		raw := content[m[4]:m[5]]
		// runtimeDynamic when the uri carried a Scala interpolation marker.
		runtimeDynamic := strings.Contains(raw, "$")
		raw = canonicalizeSttpInterpolation(raw)
		path, ok := normalizeRawClientPath(raw)
		if !ok {
			continue
		}
		caller := enclosingScalaDefAt(funcs, m[0])
		canonical := httproutes.Canonicalize(httproutes.FrameworkSpring, path)
		emit(verb, canonical, "sttp", "Function", caller, runtimeDynamic)
	}
}

// canonicalizeSttpInterpolation rewrites Scala `$x` / `${expr}` interpolation
// markers inside a uri-literal body to `{param}` placeholders, and trims a
// trailing `/` segment introduced by a removed interpolation when needed.
func canonicalizeSttpInterpolation(raw string) string {
	return scSttpInterpRe.ReplaceAllStringFunc(raw, func(tok string) string {
		// `$id` -> {id}; `${user.id}` -> {param} (expression form has no clean name).
		if strings.HasPrefix(tok, "${") {
			return "{param}"
		}
		name := strings.TrimPrefix(tok, "$")
		return "{" + name + "}"
	})
}

// indexScalaDefs builds a sorted (offset, name) list for every Scala method
// declaration in the file, for enclosing-def attribution.
func indexScalaDefs(content string) []jsFuncSpan {
	var out []jsFuncSpan
	for _, m := range scEnclosingDefRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		out = append(out, jsFuncSpan{offset: m[0], name: content[m[2]:m[3]]})
	}
	return out
}

// enclosingScalaDefAt returns the nearest preceding def name for a call site.
func enclosingScalaDefAt(funcs []jsFuncSpan, pos int) string {
	return enclosingJSFuncAt(funcs, pos)
}
