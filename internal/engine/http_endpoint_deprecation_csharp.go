// C# / ASP.NET Core deprecation + API-version signals for the language-agnostic
// endpoint enrichment passes in http_endpoint_deprecation.go (epic #3628).
//
// The flagship pass (http_endpoint_deprecation.go) already gives C# two things
// for free, because they are language-agnostic:
//
//   - api_version from an explicit `/api/v2/...` route segment (applyEndpointAPIVersion).
//   - deprecated=true from a `Sunset`/`Deprecation` response header written in
//     the action body, and from a leading `// DEPRECATED` banner comment.
//
// What C# additionally needs — and what this file adds — are the .NET-idiomatic
// markers the generic passes cannot see:
//
//   - Deprecation: the standard [Obsolete("…")] attribute, the ApiExplorer
//     [Deprecated] attribute, and the [ApiVersion(..., Deprecated = true)] flag
//     in the endpoint handler's attribute region. (All are handler-region
//     scoped — a controller-wide [ApiVersion(Deprecated=true)] is honest-partial
//     for deprecation so a class-wide flag can't leak across controllers in a
//     multi-class file; the version it names is still pinned, see below.)
//   - API version: the [ApiVersion("2.0")] attribute (Asp.Versioning /
//     Microsoft.AspNetCore.Mvc.Versioning), which pins the version even when the
//     conventional route (`api/[controller]`) carries no /vN segment.
//
// Property contract is IDENTICAL to the flagship (deprecated / deprecated_since /
// deprecated_replacement / deprecation_source / api_version). HONEST-PARTIAL:
// a non-obsolete action carries no `deprecated`; a versionless route with no
// [ApiVersion] attribute carries no `api_version`; a since/replacement that the
// [Obsolete] message does not name is left empty (never fabricated).
//
// Refs #3628.
package engine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// csObsoleteAttrRe matches an [Obsolete] attribute, with the optional message
// argument captured. Covers `[Obsolete]`, `[Obsolete("use /api/v2")]`,
// `[Obsolete("msg", true)]` and the fully-qualified `[System.Obsolete("…")]`.
// The `\b` after Obsolete keeps `[ObsoleteSomething]` from matching.
var csObsoleteAttrRe = regexp.MustCompile(`\[\s*(?:System\.)?Obsolete\b\s*(?:\(\s*(?:@?"([^"]{0,200})")?[^)]*\))?\s*\]`)

// csDeprecatedAttrRe matches the ApiExplorer [Deprecated] attribute
// (Microsoft.AspNetCore.OData / Mvc.ApiExplorer), optionally fully-qualified.
// `\b` prevents `[DeprecatedHelper]` from matching.
var csDeprecatedAttrRe = regexp.MustCompile(`\[\s*Deprecated\b\s*(?:\([^)]{0,200}\))?\s*\]`)

// csApiVersionDeprecatedRe matches an [ApiVersion("1.0", Deprecated = true)]
// flag — the Asp.Versioning way to mark a version as sunset.
var csApiVersionDeprecatedRe = regexp.MustCompile(`(?i)\[\s*ApiVersion\b[^\]]*\bDeprecated\s*=\s*true[^\]]*\]`)

// csharpDeprecationVerdict resolves a C#/.NET deprecation marker from the
// attribute/comment region that decorates the endpoint's handler. Returns
// (verdict, true) on the first marker found; (zero, false) when none apply.
func csharpDeprecationVerdict(region string) (deprecationVerdict, bool) {
	// [Obsolete("message")] — the canonical .NET deprecation marker. The
	// message commonly names the replacement ("use /api/v2/users") and may
	// carry a "since X" hint, both pulled by the shared message parser.
	if m := csObsoleteAttrRe.FindStringSubmatch(region); m != nil {
		v := deprecationVerdict{deprecated: true, source: "[Obsolete]"}
		if m[1] != "" {
			v.since, v.replacement = parseDeprecationMessage(m[1])
		}
		return v, true
	}
	// [ApiVersion(..., Deprecated = true)] — version-level sunset flag.
	if csApiVersionDeprecatedRe.MatchString(region) {
		return deprecationVerdict{deprecated: true, source: "[ApiVersion(Deprecated=true)]"}, true
	}
	// [Deprecated] — ApiExplorer deprecation attribute.
	if csDeprecatedAttrRe.MatchString(region) {
		return deprecationVerdict{deprecated: true, source: "[Deprecated]"}, true
	}
	return deprecationVerdict{}, false
}

// csApiVersionAttrRe captures the leading numeric component of an
// [ApiVersion("2.0")] / [ApiVersion("2")] attribute (the major version). The
// minor component ("2.0" → 2) is intentionally dropped to match the flagship
// `api_version` shape, which is the bare major version with no leading 'v'.
var csApiVersionAttrRe = regexp.MustCompile(`\[\s*ApiVersion\s*\(\s*@?"v?(\d+)(?:\.\d+)?"`)

// csharpAPIVersionFromAttribute resolves an api_version for a C# endpoint whose
// route path carries no explicit version segment, by reading an [ApiVersion]
// attribute. It prefers an action-level attribute in the handler's decorator
// region (anchored on the route declaration line) and falls back to a single
// controller-level attribute for the whole file. Honest-partial: when no
// [ApiVersion] attribute exists, or the file declares conflicting versions that
// cannot be attributed to this endpoint, no version is returned.
func csharpAPIVersionFromAttribute(content string, e *types.EntityRecord) (int, bool) {
	// 1. Action-level [ApiVersion] in the handler decorator region.
	anchor := e.StartLine
	if anchor <= 0 {
		anchor = routeDeclarationLine(content, e.Properties["path"], e.Properties["verb"])
	}
	if anchor > 0 {
		region, _ := handlerDecoratorRegion(content, anchor)
		if v, ok := csParseAPIVersion(region); ok {
			return v, true
		}
	}
	// 2. Controller-level: a single unambiguous [ApiVersion] in the file.
	if v, ok := csSoleFileAPIVersion(content); ok {
		return v, true
	}
	return 0, false
}

// csParseAPIVersion returns the major version named by the FIRST [ApiVersion]
// attribute in s (in range), and whether one was found.
func csParseAPIVersion(s string) (int, bool) {
	m := csApiVersionAttrRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	return csVersionInRange(m[1])
}

// csSoleFileAPIVersion returns the file's controller-level api_version only when
// every [ApiVersion] attribute in the file names the SAME major version — a
// file declaring both v1 and v2 is ambiguous per-endpoint, so it yields nothing
// (the action-level region check above already had its chance).
func csSoleFileAPIVersion(content string) (int, bool) {
	ms := csApiVersionAttrRe.FindAllStringSubmatch(content, -1)
	if len(ms) == 0 {
		return 0, false
	}
	first, ok := csVersionInRange(ms[0][1])
	if !ok {
		return 0, false
	}
	for _, m := range ms[1:] {
		v, ok := csVersionInRange(m[1])
		if !ok || v != first {
			return 0, false
		}
	}
	return first, true
}

// csVersionInRange parses a major-version string and bounds-checks it against
// the same [min,max] the path-derived version uses.
func csVersionInRange(s string) (int, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, false
	}
	if v < endpointAPIVersionMin || v > endpointAPIVersionMax {
		return 0, false
	}
	return v, true
}
