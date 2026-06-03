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
//   - FeatureManagement (.NET) : _featureManager.IsEnabledAsync("key") /
//     IsEnabled("key") / the [FeatureGate("key")] attribute on an MVC
//     controller/action (Microsoft.FeatureManagement)
//   - Unleash      : unleash.isEnabled("key") / isFeatureEnabled("key")
//   - OpenFeature  : client.getBooleanValue("key", false) /
//     getStringValue / getNumberValue / getObjectValue
//   - Flipper      : Flipper.enabled?(:key) / :key.to_sym (Ruby)
//   - Flagsmith    : flagsmith.has_feature("key") / is_feature_enabled("key")
//   - FF4j (Java)  : ff4j.check("key")
//   - Split.io     : client.getTreatment("split-name") /
//     getTreatmentWithConfig / getTreatments (the treatment family is
//     Split-specific)
//   - Unleash-React: useFlag("key") / useFlagsStatus — the @unleash/proxy-client
//     React hook
//   - GrowthBook    : gb.isOn("key") / gb.isOff("key") /
//     gb.getFeatureValue("key", default) (JS/TS-first; receiver-gated on
//     gb/growthbook)
//   - ConfigCat     : configCatClient.getValue("key", default) /
//     getValueAsync / getValueDetails (receiver-gated on configCat*)
//   - Generic/custom : getFlag("key") / feature_enabled("key") /
//     featureEnabled("key") / isFeatureEnabled("key") — FF-specific custom
//     wrappers, distinct enough from arbitrary code to attribute
//
// Honest-partial: only LITERAL string/symbol flag keys are emitted. A dynamic
// key (`variation(k, ...)`, `isEnabled(flagName)`, `getTreatment(name)`) yields
// NO edge and NO fabricated flag entity — the same disposition config-read uses
// for dynamic settings. Bare subscript access (`flags['x']`) is deliberately
// NOT matched: a `flags[...]` map index is far too common in ordinary code to
// attribute to a feature flag without import context, so it is skipped rather
// than risk false positives.
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
	sdk    string // detecting SDK: launchdarkly | featuremanagement | unleash | unleash-react | openfeature | flipper | flagsmith | ff4j | split | growthbook | configcat | custom
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
	// Both snake_case (python/ruby: bool_variation) and camelCase (Java/JS:
	// boolVariation) typed prefixes are accepted — the camelCase form has no
	// word boundary between the prefix and `Variation`, so the prefix is part
	// of the same matched token rather than relying on `\bvariation`. The
	// receiver is an LD client; we don't require a specific receiver name (it
	// varies: client, ldclient, ldClient, ld, etc.) but DO require the
	// variation method name, which is LD-specific enough to avoid noise.
	{
		re: regexp.MustCompile(
			`(?i)\b(?:bool|string|int|number|json|double|migration)?_?variation(?:_?detail)?\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "launchdarkly",
		method: "variation",
	},

	// Microsoft.FeatureManagement (.NET). The canonical runtime check is
	// `_featureManager.IsEnabledAsync("flag")` (Task-returning, hence the
	// `Async` suffix) — also the synchronous `IsEnabled("flag")` on a
	// FeatureManager. The Unleash matcher below requires `enabled\s*\(`, which
	// the `Async` suffix breaks, so this dedicated matcher must run FIRST to
	// attribute the .NET FeatureManagement idiom (and to claim the `Async`
	// form Unleash structurally cannot). A `featureManager`/`_featureManager`
	// receiver is required so the `IsEnabled[Async]` method name — generic on
	// its own — is only attributed when it is the FeatureManagement API,
	// matching the receiver-gated discipline used for FF4j.
	{
		re: regexp.MustCompile(
			`(?i)\b_?featureManager\s*\.\s*IsEnabled(?:Async)?\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "featuremanagement",
		method: "IsEnabledAsync",
	},

	// Microsoft.FeatureManagement attribute gate (.NET). The
	// `[FeatureGate("admin-panel")]` attribute decorates an MVC controller or
	// action so the whole endpoint is feature-gated declaratively (no call
	// site). The `FeatureGate` attribute name is FeatureManagement-specific, so
	// the literal first argument is a reliable flag key. The enclosing-function
	// attribution still applies (the attribute precedes the method it gates);
	// when it decorates a class it attributes to file scope.
	{
		re: regexp.MustCompile(
			`(?i)\bFeatureGate\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "featuremanagement",
		method: "FeatureGate",
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

	// Split.io. client.getTreatment("split-name") /
	// getTreatmentWithConfig("split-name") / getTreatments(...) (plural takes
	// a list, not a single literal — excluded). The `getTreatment` family is
	// Split-specific, so the method name alone is a reliable signal without a
	// receiver constraint. Split keys are conventionally called "splits"; we
	// store the literal as the flag key so a split shares the converged node
	// identity with any other provider checking the same key string.
	{
		re: regexp.MustCompile(
			`(?i)\bgetTreatment(?:WithConfig)?\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "split",
		method: "getTreatment",
	},

	// Unleash React proxy client hook: useFlag("beta-ui") /
	// useFlag('beta-ui'). useFlagsStatus()/useVariant("x") are related; only
	// the single-literal-key shape is attributed. Distinct from the server
	// isEnabled matcher above (different call name) so both can fire in a
	// codebase mixing client+server SDKs.
	{
		re: regexp.MustCompile(
			`(?i)\buseFlag\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "unleash-react",
		method: "useFlag",
	},

	// FF4j (Java). ff4j.check("flag") — the bare `check` method name is too
	// generic to attribute on its own, so an `ff4j` receiver is required (the
	// canonical FF4j instance/bean name). This keeps the matcher Java-specific
	// and avoids false-positiving on arbitrary `.check("...")` calls.
	{
		re: regexp.MustCompile(
			`(?i)\bff4j\s*\.\s*check\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "ff4j",
		method: "ff4j.check",
	},

	// GrowthBook (JS/TS-first SDK). gb.isOn("flag") / gb.isOff("flag") /
	// gb.getFeatureValue("flag", default). isOn/isOff/getFeatureValue are
	// generic enough that a `gb`/`growthbook` receiver is required (the
	// canonical GrowthBook instance name returned by `new GrowthBook(...)`),
	// mirroring the receiver-gated discipline used for FF4j and
	// FeatureManagement. This keeps the matcher GrowthBook-specific and avoids
	// false-positiving on arbitrary `.isOn(...)` calls.
	{
		re: regexp.MustCompile(
			`(?i)\bgrowthbook\s*\.\s*(?:isOn|isOff|getFeatureValue)\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "growthbook",
		method: "growthbook.isOn",
	},
	{
		re: regexp.MustCompile(
			`(?i)\bgb\s*\.\s*(?:isOn|isOff|getFeatureValue)\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "growthbook",
		method: "gb.isOn",
	},

	// ConfigCat (JS/TS + multi-lang SDK). configCatClient.getValue("flag",
	// default, user) / getValueAsync("flag", default) / getValueDetails.
	// `getValue` is generic on its own, so a ConfigCat-flavoured receiver
	// (`configCat`/`configCatClient`) is required — same receiver-gated
	// discipline as FF4j/FeatureManagement/GrowthBook — so an arbitrary
	// `.getValue("...")` is not misattributed.
	{
		re: regexp.MustCompile(
			`(?i)\bconfigCat\w*\s*\.\s*getValue(?:Async|Details)?\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "configcat",
		method: "configCat.getValue",
	},

	// Generic / custom feature-flag wrappers. getFlag("key") /
	// feature_enabled("key") / featureEnabled("key"). These method names are
	// FF-specific enough to attribute. isFeatureEnabled / isEnabled are
	// already handled by the Unleash matcher above (first-match-wins), so this
	// matcher targets the getFlag + feature_enabled shapes it does not cover.
	{
		re: regexp.MustCompile(
			`(?i)\b(?:getFlag|feature_?enabled)\s*\(\s*` +
				`(?:"([^"\\]+)"|'([^'\\]+)')`,
		),
		sdk:    "custom",
		method: "getFlag",
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
