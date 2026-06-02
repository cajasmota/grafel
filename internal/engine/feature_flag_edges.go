// Feature-flag gating topology (issue #3628 area #17).
//
// Feature flags are a sibling concept to config reads (#3641/#1885,
// DEPENDS_ON_CONFIG): a runtime check that decides whether a code path is
// taken. Where config reads answer "what consumes config key K", feature
// flags answer "what code is gated by flag F" — flag blast-radius.
//
// This pass scans a file for flag-management-SDK check call sites, attributes
// each to its enclosing function (reusing the cross-language
// indexEnclosingFunctions / enclosingFuncAt helpers from orm_queries.go), and
// emits:
//
//   - one synthetic SCOPE.FeatureFlag entity per distinct flag key
//     (synthetic ID `feature:<flag-key>`, Subtype = the detecting SDK), and
//   - one GATED_BY edge per (enclosing function, flag) pair, pointing at the
//     flag entity.
//
// The flag entity ID is the key string, so two repos (or two files) that
// check the same flag converge on one node — the same cross-repo identity
// strategy used by MessageTopic / ServerlessFunction.
//
// Supported SDKs (cross-language):
//
//   - LaunchDarkly : client.variation("key", user, false) /
//     ldclient.bool_variation("key", ...) /
//     *Variation / variationDetail family
//   - Unleash      : unleash.isEnabled("key") / isFeatureEnabled("key")
//   - OpenFeature  : client.getBooleanValue("key", false) /
//     getStringValue / getNumberValue / getObjectValue
//   - Flipper      : Flipper.enabled?(:key) / :key.to_sym (Ruby)
//   - Flagsmith    : flagsmith.has_feature("key") / is_feature_enabled("key")
//
// Honest-partial: only LITERAL string/symbol flag keys are emitted. A dynamic
// key (`variation(k, ...)`, `isEnabled(flagName)`) yields NO edge and NO
// fabricated flag entity — the same disposition config-read uses for dynamic
// settings.
//
// Append-only — never modifies existing entities or edges, so this pass
// cannot regress the surrounding pipeline on files that contain no flag
// checks.
package engine

import (
	"regexp"
	"strconv"

	"github.com/cajasmota/archigraph/internal/types"
)

// featureFlagEntityKind / featureFlagEdgeKind are aliased through the typed
// enum so kinds.go stays canonical (producer_kinds_test.go guardrail).
var (
	featureFlagEntityKind = string(types.EntityKindFeatureFlag)
	featureFlagEdgeKind   = string(types.RelationshipKindGatedBy)
)

// featureFlagPatternType tags every emitted entity/edge so downstream
// consumers can filter the pass output, matching the pattern_type convention
// used by the ORM and config passes.
const featureFlagPatternType = "feature_flag_gating"

// flagHit is one detected flag-check call site.
type flagHit struct {
	key    string // literal flag key (symbol leading ':' already stripped)
	sdk    string // detecting SDK: launchdarkly | unleash | openfeature | flipper | flagsmith
	method string // the SDK call/method observed
	caller string // enclosing-function name ("" → file scope)
	line   int    // 1-indexed source line
}

// flagSDKMatcher pairs a compiled regex with the SDK/method it identifies.
// The regex MUST expose the literal flag key in capture group 1; a key that
// is not a literal string/symbol is filtered out before the regex even
// matches (the patterns only accept quoted strings or `:symbol` tokens), so
// dynamic keys never produce a hit.
type flagSDKMatcher struct {
	re     *regexp.Regexp
	sdk    string
	method string
}

// flagSDKMatchers is the cross-language matcher table. Patterns deliberately
// only accept a quoted literal (or Ruby `:symbol`) as the key argument so a
// dynamic key (bare identifier / expression) is structurally rejected.
//
// Key-literal sub-patterns:
//
//	"([^"\\]+)"  — double-quoted (JS/Java/Python/Go/C#)
//	'([^'\\]+)'  — single-quoted (JS/Ruby/Python)
//	:([A-Za-z_]\w*[?!]?) — Ruby symbol
var flagSDKMatchers = []flagSDKMatcher{
	// LaunchDarkly. Server + client SDKs across languages expose a
	// `variation` family: variation / boolVariation / bool_variation /
	// stringVariation / intVariation / jsonVariation / variationDetail.
	// The receiver is an LD client; we don't require a specific receiver
	// name (it varies: client, ldclient, ld, etc.) but DO require the
	// variation method name, which is LD-specific enough to avoid noise.
	{
		re: regexp.MustCompile(
			`(?i)\b(?:bool_|string_|int_|number_|json_|double_)?variation(?:_detail|detail)?\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "launchdarkly",
		method: "variation",
	},

	// Unleash. isEnabled("flag") / isFeatureEnabled("flag") /
	// is_enabled("flag"). Also the Python decorator-ish `@app.feature("flag")`
	// is handled by the generic feature() matcher below.
	{
		re: regexp.MustCompile(
			`(?i)\bis(?:_)?(?:feature)?(?:_)?enabled\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "unleash",
		method: "isEnabled",
	},

	// OpenFeature. getBooleanValue / getStringValue / getIntegerValue /
	// getNumberValue / getDoubleValue / getObjectValue (+ *Details). The
	// Python SDK uses snake_case: get_boolean_value, get_string_value, …
	{
		re: regexp.MustCompile(
			`(?i)\bget_?(?:boolean|string|integer|number|double|object)_?value(?:_?details?)?\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "openfeature",
		method: "getBooleanValue",
	},

	// Flipper (Ruby). Flipper.enabled?(:flag) / Flipper.enabled?(:flag, user)
	// — symbol or string key. The `?` is part of the Ruby predicate method
	// name, not optionality.
	{
		re: regexp.MustCompile(
			`\bFlipper\.enabled\?\s*\(\s*` +
				`(?::([A-Za-z_]\w*[?!]?)|"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "flipper",
		method: "Flipper.enabled?",
	},

	// Flagsmith. has_feature("flag") / is_feature_enabled("flag") /
	// hasFeature("flag") / isFeatureEnabled("flag"). The is_feature_enabled
	// shape overlaps Unleash's regex; first-match-wins ordering puts Unleash
	// first, so Flagsmith here is the has_feature shape (distinct).
	{
		re: regexp.MustCompile(
			`(?i)\bhas_?feature\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "flagsmith",
		method: "has_feature",
	},
}

// applyFeatureFlagEdges scans the file for flag-check call sites and appends
// a SCOPE.FeatureFlag entity (deduped per key) plus a GATED_BY edge per
// (enclosing function, flag) pair. No-op for files with no recognised flag
// SDK call. Runs append-only.
func applyFeatureFlagEdges(args DetectorPassArgs) DetectorPassResult {
	lang := args.Lang
	path := args.Path
	content := args.Content
	entities := args.Entities
	relationships := args.Relationships

	if len(content) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	src := string(content)

	hits := scanFeatureFlags(lang, src)
	if len(hits) == 0 {
		return DetectorPassResult{Entities: entities, Relationships: relationships}
	}

	// Dedup the flag entities by key (cross-SDK: a key is one node). The
	// first SDK that detected the key wins the Subtype, mirroring how a
	// single topic node is shared by multiple producers.
	emittedFlag := map[string]bool{}
	// Dedup edges by (caller, key) so multiple checks of the same flag in
	// one function produce a single GATED_BY edge.
	emittedEdge := map[string]bool{}

	for _, h := range hits {
		flagID := buildFeatureFlagID(h.key)

		if !emittedFlag[h.key] {
			emittedFlag[h.key] = true
			entities = append(entities, types.EntityRecord{
				ID:         flagID,
				Name:       h.key,
				Kind:       featureFlagEntityKind,
				Subtype:    h.sdk,
				SourceFile: path,
				StartLine:  h.line,
				EndLine:    h.line,
				Language:   lang,
				Properties: map[string]string{
					"flag":         h.key,
					"sdk":          h.sdk,
					"pattern_type": featureFlagPatternType,
				},
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.8,
			})
		}

		edgeKey := h.caller + "\x00" + h.key
		if emittedEdge[edgeKey] {
			continue
		}
		emittedEdge[edgeKey] = true

		relationships = append(relationships, types.RelationshipRecord{
			FromID: buildFeatureFlagCallerID(h.caller, path),
			ToID:   flagID,
			Kind:   featureFlagEdgeKind,
			Properties: map[string]string{
				"flag":         h.key,
				"sdk":          h.sdk,
				"method":       h.method,
				"line":         strconv.Itoa(h.line),
				"pattern_type": featureFlagPatternType,
			},
		})
	}

	return DetectorPassResult{Entities: entities, Relationships: relationships}
}

// scanFeatureFlags runs every SDK matcher over the source and returns one
// flagHit per literal flag-check call site, attributed to its enclosing
// function. First-matcher-wins per call offset prevents an is_feature_enabled
// shape from being double-counted by both the Unleash and Flagsmith regexes.
func scanFeatureFlags(lang, src string) []flagHit {
	funcs := indexEnclosingFunctions(lang, src)

	var hits []flagHit
	// Track byte offsets already claimed by a matcher so overlapping
	// patterns don't emit two hits for one call site.
	claimed := map[int]bool{}

	for _, m := range flagSDKMatchers {
		for _, loc := range m.re.FindAllStringSubmatchIndex(src, -1) {
			start := loc[0]
			if claimed[start] {
				continue
			}
			key := firstFlagCapture(src, loc)
			if key == "" {
				continue // dynamic / unparseable key — honest-partial skip
			}
			claimed[start] = true
			hits = append(hits, flagHit{
				key:    key,
				sdk:    m.sdk,
				method: m.method,
				caller: enclosingFuncAt(funcs, start),
				line:   lineAtOffset(src, start),
			})
		}
	}
	return hits
}

// firstFlagCapture returns the first non-empty capture group in a match,
// which (for every matcher) is the literal flag key. Returns "" when no
// group captured (defensive — should not happen given the patterns).
func firstFlagCapture(src string, loc []int) string {
	for i := 2; i+1 < len(loc); i += 2 {
		if loc[i] >= 0 {
			k := src[loc[i]:loc[i+1]]
			if k != "" {
				return k
			}
		}
	}
	return ""
}

// buildFeatureFlagID returns the synthetic cross-repo entity ID for a flag
// key: `feature:<flag-key>`. Matching keys converge on one node.
func buildFeatureFlagID(key string) string {
	return "feature:" + key
}

// buildFeatureFlagCallerID returns the FromID for a GATED_BY edge. Mirrors
// buildCallerID (orm_queries.go): `Function:<name>` when the enclosing
// function is known, else a file-anchored placeholder so the edge still
// expresses "something in this file is gated by flag F".
func buildFeatureFlagCallerID(caller, path string) string {
	if caller == "" {
		return "File:" + path
	}
	return "Function:" + caller
}
