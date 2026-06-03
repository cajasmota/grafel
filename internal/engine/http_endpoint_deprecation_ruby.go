// Ruby (Rails / Sinatra) deprecation + API-version signals for the
// language-agnostic endpoint enrichment passes in http_endpoint_deprecation.go
// (epic #3628).
//
// The flagship pass (http_endpoint_deprecation.go) already gives Ruby two things
// for free, because they are language-agnostic:
//
//   - api_version from an explicit `/api/v1/...` / `/v2/...` route segment
//     (applyEndpointAPIVersion). Rails composes this from `namespace :api do;
//     namespace :v1 do` and `scope '/api/v2'`, and Sinatra from the literal
//     verb-block path — both surface as the canonical `path` property the
//     path-derived version reads. So Rails-namespace-versioned and Sinatra
//     versioned endpoints already carry api_version with no Ruby-specific code.
//   - deprecated=true from a `Sunset`/`Deprecation` response header written in a
//     Sinatra handler block body (the cross-language RFC-8594 signal), and from a
//     leading `# DEPRECATED` banner comment.
//
// What Ruby additionally needs — and what this file adds — is the Ruby-idiomatic
// deprecation marker the generic passes do not recognise: a YARD `# @deprecated`
// tag or a `# Deprecated:` doc comment immediately above a Sinatra verb block
// (the handler-decorator region the flagship anchors on the block's StartLine).
//
// HONEST-PARTIAL — what is intentionally NOT credited here:
//
//   - Rails controller-action deprecation. A Rails endpoint is synthesised from
//     config/routes.rb, but its `# @deprecated` action comment lives in a
//     SEPARATE app/controllers/<name>_controller.rb file that this per-file pass
//     never sees during routes.rb synthesis. Crediting it would require a
//     cross-file lookup that the enrichment pass deliberately does not do, so a
//     Rails action comment yields no `deprecated` (never fabricated). The
//     path-derived `api_version` still lights up for namespace/scope-versioned
//     Rails routes.
//   - Grape `version 'v2'` / `desc 'x', deprecated: true`. Grape endpoints are
//     extracted as SCOPE.Operation `endpoint` entities by the custom Grape
//     extractor (internal/custom/ruby/grape_deep.go), NOT as the
//     http_endpoint_definition producers this pass stamps, so they are out of
//     reach until Grape is promoted to the synthesised endpoint shape.
//
// Property contract is IDENTICAL to the flagship (deprecated / deprecated_since /
// deprecated_replacement / deprecation_source / api_version).
//
// Refs #3628.
package engine

import "regexp"

// rubyYardDeprecatedRe matches a YARD `# @deprecated` doc tag with its optional
// trailing message (up to end of line). This is the idiomatic Ruby deprecation
// marker — YARD renders it as a deprecation banner and rubocop's
// `InternalAffairs/DeprecateClass`-style cops key off it. The `\b` keeps
// `@deprecatedness` from matching.
var rubyYardDeprecatedRe = regexp.MustCompile(`#[^\n]*@deprecated\b([^\n]{0,200})`)

// rubyDeprecatedCommentRe matches a `# Deprecated: <message>` doc comment — the
// plain-prose Ruby convention (mirrors Go's `// Deprecated:` godoc). The colon
// is required so it does not collide with prose merely mentioning "deprecated".
var rubyDeprecatedCommentRe = regexp.MustCompile(`(?i)#\s*Deprecated:\s*([^\n]{0,200})`)

// rubyDeprecationVerdict resolves a Ruby deprecation marker from the comment
// region that decorates a Sinatra verb block (the contiguous run of `#` comment
// lines immediately above the `get '/x' do` line, captured by
// handlerDecoratorRegion). Returns (verdict, true) on the first marker found;
// (zero, false) when none apply so the cross-language Sunset-header / generic
// `# DEPRECATED` fallbacks still get their chance.
//
// Honest-partial: a since/replacement the marker does not name is left empty
// (never fabricated); both are pulled from the free-text message by the shared
// parser (`use /api/v2/x instead` → replacement, `since 2.0` → since).
func rubyDeprecationVerdict(region string) (deprecationVerdict, bool) {
	// YARD `# @deprecated <msg>` — the canonical Ruby tag.
	if m := rubyYardDeprecatedRe.FindStringSubmatch(region); m != nil {
		v := deprecationVerdict{deprecated: true, source: "# @deprecated"}
		v.since, v.replacement = parseDeprecationMessage(m[1])
		return v, true
	}
	// `# Deprecated: <msg>` — the plain-prose doc-comment convention.
	if m := rubyDeprecatedCommentRe.FindStringSubmatch(region); m != nil {
		v := deprecationVerdict{deprecated: true, source: "# Deprecated:"}
		v.since, v.replacement = parseDeprecationMessage(m[1])
		return v, true
	}
	return deprecationVerdict{}, false
}
