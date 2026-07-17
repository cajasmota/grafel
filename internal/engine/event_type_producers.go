// Producer-side + consumer-side detectors for the generic event-identity
// pass (event_type_edges.go, GAP-005).
package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
)

// ---------------------------------------------------------------------------
// Shared: producer-source structural-ref (RC-A precision fix)
// ---------------------------------------------------------------------------

// eventTypeProducerSentinelCallers are the enclosing-function-not-found
// fallback values findEnclosingGoFunctionName / findEnclosingNodeFunctionName
// return when no real function/method could be located (see those helpers in
// serverless_edges.go). Neither is a real symbol name, so a structural-ref
// built from it can never resolve via byLocation — treat it the same as an
// empty caller and drop the edge outright (RC-A fix part 2, drop-on-unresolved
// safety net) rather than emit a doomed stub.
var eventTypeProducerSentinelCallers = map[string]bool{
	"package": true, // Go: no enclosing func decl found in the lookback window
	"module":  true, // JS/TS: no enclosing func/arrow found in the lookback window
}

// producerSourceRef builds the location-qualified structural-ref FromID for a
// PUBLISHES_TO producer edge (RC-A precision fix). Every producer detector in
// this file previously stamped the FromID as a bare-leaf-name symbolic stub —
// `fmt.Sprintf("SCOPE.Function:%s", caller)` — which fails resolution for
// three independent reasons (see the RC-A root-cause writeup at the top of
// this file's package doc / the task description):
//
//  1. The stub claims kind "SCOPE.Function", but the tree-sitter extractors
//     emit real Go/JS/Java functions and methods as SCOPE.Operation, so the
//     precise kind+name resolver tier never hits.
//  2. The stub carries no source-file context (it's a standalone
//     Relationship, and the SCOPE.EventType ToID node is minted with
//     SourceFile: "" too), so the resolver's same-file/same-package locality
//     tie-break (rewriteOneWithCaller) never engages.
//  3. A repeated leaf name across files/packages (e.g. two different
//     "Handler" functions) collides in the bare-name index, so even when the
//     kind is fixed the lookup is ambiguous.
//
// This helper instead emits the canonical Format-A structural-ref
// `scope:operation:method:<lang>:<file>:<name>` — the SAME convention
// internal/extractor.BuildOperationStructuralRef centralizes for class→method
// CONTAINS edges and the Go/JS/TS bare-call CALLS path (Refs #44) — which the
// central resolver (internal/resolve.Index.lookupStructural) binds
// deterministically via byLocation[file][name] (same-file, the dominant case)
// with a same-package fallback for Go (byPackageOperation).
//
// Returns "" — signalling the caller should not emit an edge at all — when
// path or caller is empty, or caller is a known non-resolving sentinel
// ("package"/"module"). This is the belt half of the RC-A fix: a caller name
// that could never structurally resolve is dropped at the source rather than
// handed to the resolver as a doomed stub. The suspenders half (dropping a
// stub that DOES look like a real name but still fails to resolve against the
// graph) is enforced downstream by resolve.DropUnresolvedPublishesTo, which
// runs after the central resolver and removes any surviving PUBLISHES_TO edge
// whose FromID never became a real (16-char hex) entity id.
func producerSourceRef(lang, path, caller string) string {
	if path == "" || caller == "" || eventTypeProducerSentinelCallers[caller] {
		return ""
	}
	return extractor.BuildOperationStructuralRef(lang, path, caller)
}

// ---------------------------------------------------------------------------
// Shared: allowlisted event-type key extraction
// ---------------------------------------------------------------------------

// eventTypeAllowlistKeyRe matches an allowlisted event-type key bound to a
// STRING LITERAL, in either bare-identifier (Go struct field / JS object
// literal, e.g. `EventType: "OrderPlaced"`) or quoted-key (JSON-style, e.g.
// `"eventType":"OrderPlaced"`) form. The key alternation is matched
// case-insensitively (Go struct fields are conventionally capitalized,
// JSON/JS keys are conventionally camelCase) but anchored on \b so it never
// matches mid-identifier (e.g. "ContentType", "ResourceType").
//
// Allowlist: eventType, eventName, detailType, detail-type — all
// UNAMBIGUOUS event-envelope keys. Bare `type` was DELIBERATELY dropped
// (GAP-005 review FIX 2): because the publish gate is generic
// (`.send`/`.emit`/`.publish`/`.produce`), a bare `type` key over-minted on
// non-event payloads — `logger.emit({type:"error"})`,
// `httpClient.send({type:"json"})`, `styleSheet.emit({type:"css"})` all
// carry a `type` string that is NOT an event contract. The four remaining
// keys have no such collision.
//
// Between the `:` and the opening quote, ONE optional wrapper call is
// tolerated (`\w[\w.]*\(\s*`) — the EventBridge Go SDK shape `DetailType:
// aws.String("OrderPlaced")` wraps the literal in a single SDK helper call
// (GAP-015 RC3). The wrapper pattern is a GENERIC dotted-call shape
// (`\w[\w.]*\(`) matched on syntax alone — it is NOT anchored to the
// `aws.String` identifier specifically, so it also matches other
// single-argument string-wrapper helpers (`ptr.String(...)`,
// `types.String(...)`, etc.) without any corpus-specific package/function
// name. v1: deliberately not made greedy/recursive — no nested calls — to
// keep the precision boundary tight; a second wrapper layer simply doesn't
// match, by design. Cross-file/imported consts are out of scope for v1 (see
// buildGoStringBindingTable).
//
// Group 1 = the matched key text, group 2 = the OPTIONAL wrapper-call name
// (empty when the value is a bare literal), group 3 = the string value.
var eventTypeAllowlistKeyRe = regexp.MustCompile(
	`["']?\b((?i:eventType|detailType|detail-type|eventName))\b["']?\s*:\s*(?:(\w[\w.]*)\(\s*)?["'` + "`" + `]([^"'` + "`" + `\n\r]+)["'` + "`" + `]`,
)

// eventTypeFormatterFuncs is the set of wrapper-call base names (the segment
// after the last `.`) that TRANSFORM their string argument at runtime, so the
// captured literal is NOT the wire value that would verbatim-join a consumer
// (review MUST-FIX #2). Covers the fmt Sprint-family and the common
// strings-package case-/whitespace-mutators. A DetailType wrapped in one of
// these is skipped rather than minted as a garbage/never-joining node. Not
// exhaustive by design — the `%`-format-verb guard in isEventTypeValueUsable
// catches the dominant fmt.Sprintf template case independently of this set.
var eventTypeFormatterFuncs = map[string]bool{
	"Sprintf": true, "Sprint": true, "Sprintln": true, "Errorf": true,
	"ToUpper": true, "ToLower": true, "Title": true, "ToTitle": true,
	"TrimSpace": true, "Trim": true, "TrimPrefix": true, "TrimSuffix": true,
	"ReplaceAll": true, "Replace": true, "Join": true, "Repeat": true,
	"Format": true, "Fields": true, "Split": true,
}

// isEventTypeWrapperFormatter reports whether a captured wrapper-call name is
// a runtime string transformer (see eventTypeFormatterFuncs). The base name
// is the last dotted segment (`strings.ToUpper` -> `ToUpper`).
func isEventTypeWrapperFormatter(wrapper string) bool {
	if wrapper == "" {
		return false
	}
	base := wrapper
	if i := strings.LastIndex(wrapper, "."); i >= 0 {
		base = wrapper[i+1:]
	}
	return eventTypeFormatterFuncs[base]
}

// eventTypeReservedValues denylists values that pattern-match the allowlisted
// {eventName: "X"} shape (findAllowlistedEventType's own precision gate) but
// are NEVER a domain event-type contract (defect RC-B-1). "eventName" is a
// legitimate domain-event-envelope key (order-service `EventName:
// "OrderPlaced"`), but it is ALSO the literal field name DynamoDB Streams
// stamps on every stream record to carry the record's CRUD operation type —
// `event.Records[i].EventName` is always exactly one of INSERT / MODIFY /
// REMOVE. A DynamoDB-Streams Lambda handler that also happens to
// republish/forward events elsewhere in the SAME function scope (the
// realistic real-world shape — this pass's function-scope recall widening
// doesn't require co-location with the actual publish call) previously mined
// a bogus `event:type:INSERT` node from the record's plumbing field. These
// three values are DynamoDB Streams' fixed vocabulary, never a domain event
// name, so they are rejected case-insensitively regardless of which
// allowlisted key produced them.
var eventTypeReservedValues = map[string]bool{
	"INSERT": true,
	"MODIFY": true,
	"REMOVE": true,
}

// isEventTypeValueUsable rejects a captured value that cannot be a stable wire
// contract: one carrying a `%` format verb (a `fmt.Sprintf` template such as
// `order.%s.placed`), which never verbatim-joins a consumer (review
// MUST-FIX #2), or one matching the DynamoDB-Streams record-operation-type
// denylist (defect RC-B-1, eventTypeReservedValues).
func isEventTypeValueUsable(value string) bool {
	if strings.Contains(value, "%") {
		return false
	}
	if eventTypeReservedValues[strings.ToUpper(value)] {
		return false
	}
	return true
}

// eventTypeAllowlistKeyIdentRe mirrors eventTypeAllowlistKeyRe but matches a
// BARE IDENTIFIER value instead of a string literal — the shape `DetailType:
// aws.String(orderDetailType)` (GAP-015 RC4), where the wrapper call's sole
// argument is a same-file const/var identifier rather than a literal. Like
// eventTypeAllowlistKeyRe, the wrapper is matched generically on syntax
// (`\w[\w.]*\(`), not on a specific package/function name. The wrapper call
// is REQUIRED here (unlike the literal form) because a bare identifier
// directly bound to the key with no call at all is almost never an
// event-type contract in practice, and requiring the wrapper keeps this
// narrowly scoped to the single-argument string-wrapper-helper idiom (v1
// narrowing — a bare `DetailType: orderDetailType` with no wrapper is out of
// scope). The identifier itself is resolved via buildGoStringBindingTable,
// which is SAME-FILE ONLY (v1 narrowing — no cross-file/import resolution).
// Group 1 = the matched key text, group 2 = the wrapper-call name, group 3 =
// the identifier name.
var eventTypeAllowlistKeyIdentRe = regexp.MustCompile(
	`["']?\b((?i:eventType|detailType|detail-type|eventName))\b["']?\s*:\s*(\w[\w.]*)\(\s*([A-Za-z_]\w*)\s*\)`,
)

// findAllowlistedEventType scans arg (the text of a call's argument list, or
// any bounded literal window) for the first allowlisted key/string-literal
// pair whose value is a usable wire contract — a formatter/template wrapper
// (review MUST-FIX #2) is skipped and scanning continues to the next match.
// Returns ("", "", false) when none is found.
func findAllowlistedEventType(arg string) (key, value string, ok bool) {
	for _, m := range eventTypeAllowlistKeyRe.FindAllStringSubmatch(arg, -1) {
		wrapper, val := m[2], m[3]
		if !isEventTypeValueUsable(val) || isEventTypeWrapperFormatter(wrapper) {
			continue
		}
		return m[1], val, true
	}
	return "", "", false
}

// ---------------------------------------------------------------------------
// Producer — Go
// ---------------------------------------------------------------------------

// goPublishSiteRe matches common Go publish-call method names across
// AWS SDK (SNS/SQS/Kinesis/EventBridge), Sarama/Kafka (SendMessage),
// confluent-kafka-go (Produce), and generic wrapper conventions
// (Publish/Send). This is a GENERIC gate (unlike the AWS-only
// effect_sinks_aws_go.go sniffers) because GAP-005 targets any channel, not
// just AWS. PutEvents(WithContext)? (GAP-015 RC2) covers the EventBridge Go
// SDK v2 (`client.PutEvents(...)`) and v1 (`client.PutEventsWithContext(...)`)
// publish call — matched on the SDK METHOD NAME only, not on the receiver's
// package/type, so it fires for any `*.PutEvents(...)` call regardless of
// which package the client comes from. v1 narrowing: PutEvents/
// PutEventsWithContext are the only EventBridge sink names covered; other
// less-common EventBridge send paths (e.g. a hand-rolled HTTP client calling
// the API directly) are out of scope.
var goPublishSiteRe = regexp.MustCompile(
	`\.(?:Publish(?:WithContext)?|PublishMessage|SendMessage(?:Batch)?(?:WithContext)?|PutRecords?(?:WithContext)?|PutEvents(?:WithContext)?|Produce|Send)\s*\(`,
)

// goConstStringBindingRe matches a single `const X = "..."` declaration (with
// or without an explicit type name, e.g. `const orderDetailType string =
// "OrderShipped"`) — the NON-grouped form, which carries the literal `const`
// keyword on the declaration line. Grouped `const ( X = "..." )` block members
// are bare `X = "..."` (no per-line `const`) and are handled separately by
// goGroupedBlockBindings (re-review MUST-FIX #3). Group 1 = identifier,
// group 2 = literal value.
var goConstStringBindingRe = regexp.MustCompile(
	`(?m)^\s*const\s+(\w+)\s*(?:\w+\s+)?=\s*"([^"\n\r]+)"`,
)

// goVarStringBindingRe matches a single `var X = "..."` declaration (with or
// without an explicit type name). Grouped `var ( X = "..." )` members are
// handled by goGroupedBlockBindings (re-review MUST-FIX #3). Group 1 =
// identifier, group 2 = literal value.
// NOTE: this regex is not scope-aware — an in-function `var x = "..."` also
// matches — but the publish-site shadow guard (goIdentifierShadowedInFunc)
// rejects any identifier that is a param/local of the enclosing function, so
// only genuinely PACKAGE-level bindings survive to resolve a publish site.
var goVarStringBindingRe = regexp.MustCompile(
	`(?m)^\s*var\s+(\w+)\s*(?:\w+\s+)?=\s*"([^"\n\r]+)"`,
)

// goGroupedBlockOpenRe matches the opener of a grouped `const (` or `var (`
// declaration block. Group 1 = the keyword (const/var).
var goGroupedBlockOpenRe = regexp.MustCompile(`(?m)^\s*(const|var)\s*\(\s*$`)

// goGroupedMemberBindingRe matches a bare `X = "..."` member line inside a
// grouped const/var block (with or without an explicit type name, e.g.
// `orderDetailType string = "OrderShipped"`). Group 1 = identifier, group 2 =
// literal value.
var goGroupedMemberBindingRe = regexp.MustCompile(
	`(?m)^\s*(\w+)\s*(?:\w+\s+)?=\s*"([^"\n\r]+)"`,
)

// goGroupedBlockBindings extracts `X = "..."` string members from grouped
// `const ( ... )` / `var ( ... )` blocks (re-review MUST-FIX #3 — grouped
// const is very common in real Go). It walks from each block opener to its
// closing `)` and matches bare member lines that the single-declaration
// goConstStringBindingRe/goVarStringBindingRe (which require the leading
// keyword) cannot see. Emits (name, value) pairs via add.
func goGroupedBlockBindings(src string, add func(name, value string)) {
	for _, loc := range goGroupedBlockOpenRe.FindAllStringIndex(src, -1) {
		// Block body runs from just after the `(` line to the next line that is
		// a closing `)` at the start (ignoring leading whitespace).
		rest := src[loc[1]:]
		end := len(rest)
		if idx := regexp.MustCompile(`(?m)^\s*\)`).FindStringIndex(rest); idx != nil {
			end = idx[0]
		}
		body := rest[:end]
		for _, m := range goGroupedMemberBindingRe.FindAllStringSubmatch(body, -1) {
			add(m[1], m[2])
		}
	}
}

// buildGoStringBindingTable scans src for `const X = "..."` and `var X =
// "..."` string bindings (GAP-015 RC4: same-file identifier resolution for
// `DetailType: aws.String(orderDetailType)`-shaped producer call-sites) and
// returns a name->literal table. Only UNAMBIGUOUS bindings are kept — an
// identifier bound to two different literal values anywhere in the file is
// dropped entirely rather than guessed at.
//
// Both single-line (`const X = "..."`, `var X = "..."`) and GROUPED block
// members (`const ( X = "..." )`, `var ( X = "..." )`, via
// goGroupedBlockBindings) are matched — re-review MUST-FIX #3, since grouped
// const is very common in real Go and was the dominant RC4 real-world miss.
//
// SAFETY (review MUST-FIX #1): this table intentionally does NOT include
// function-local `:=`/`=` bindings. Those are scope-local and a file-global
// table cannot tell which function a binding belongs to, so including them
// let a `detail := "X"` in one function wrongly resolve a same-named
// PARAMETER at a publish site in another function. Resolution is therefore
// restricted to package-level-style const/var declarations, AND every
// publish-site resolution is additionally gated by goIdentifierShadowedInFunc
// (which rejects the identifier if it is a param — including a CLOSURE param
// — or a `:=`/`var` local of the ENCLOSING lexical function/func-literal).
// Together these guarantee a resolved identifier really is the package-level
// binding — not a shadowing local/param — so the previous "never a false
// positive" reasoning now actually holds. NOTE: a grouped-block member that
// happens to sit inside a function body (rare) is not distinguished from a
// package-level one here, but the shadow guard still rejects it if it is a
// param/local at the publish site; the residual risk is only a same-named
// unrelated in-function grouped const, which is vanishingly rare.
//
// v1 narrowing (deliberately out of scope, follow-up candidates):
//   - SAME-FILE ONLY. A const/var defined in another file of the same
//     package (very common in real Go — constants often live in a shared
//     `const.go`) is NOT resolved. This is likely the dominant real-world
//     miss for this RC.
//   - NO cross-package/imported identifiers (e.g. `events.OrderPlacedType`).
//   - Function-local `:=`/`=` bindings are NOT resolved at all (even for a
//     publish site in the SAME function), a deliberate precision-over-recall
//     tradeoff for the file-global-table design — see SAFETY above.
//   - Entries built via a map literal, a helper return value, or appended
//     into a slice in a loop are not covered — only the IDENTIFIER form.
func buildGoStringBindingTable(src string) map[string]string {
	bindings := map[string]string{}
	ambiguous := map[string]bool{}
	add := func(name, value string) {
		if ambiguous[name] {
			return
		}
		if existing, seen := bindings[name]; seen {
			if existing != value {
				delete(bindings, name)
				ambiguous[name] = true
			}
			return
		}
		bindings[name] = value
	}
	for _, re := range []*regexp.Regexp{goConstStringBindingRe, goVarStringBindingRe} {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			add(m[1], m[2])
		}
	}
	goGroupedBlockBindings(src, add)
	return bindings
}

// goShortVarDeclReFor builds a matcher for a `:=` short-var declaration of a
// specific identifier — `x :=`, `x, y :=`, or `y, x :=` (a comma list of
// simple identifiers ending in `:=`). Anchored on \b and restricted to a
// comma/ident run so an expression like `if x > 0 { ... }` cannot false-match.
func goShortVarDeclReFor(ident string) *regexp.Regexp {
	q := regexp.QuoteMeta(ident)
	return regexp.MustCompile(`\b` + q + `\b\s*(?:,\s*\w+\s*)*:=` +
		`|(?:\b\w+\s*,\s*)+` + q + `\b\s*(?:,\s*\w+\s*)*:=`)
}

// goVarDeclReFor matches a `var ident` local declaration of a specific
// identifier.
func goVarDeclReFor(ident string) *regexp.Regexp {
	return regexp.MustCompile(`\bvar\s+` + regexp.QuoteMeta(ident) + `\b`)
}

// goAnyFuncParamOpenRe matches the parameter-list `(` of ANY Go function
// header in funcScope — both a top-level `func name(` / `func (recv) name(`
// declaration AND an anonymous func-literal / closure `func(` (re-review
// MUST-FIX #1: closure params were previously invisible). FindAllStringIndex
// end-1 is the `(` index the param list starts at.
var goAnyFuncParamOpenRe = regexp.MustCompile(`\bfunc\s*(?:\(\s*\w+\s+\*?\w+\s*\)\s*)?(?:\w+\s*)?\(`)

// goIdentifierShadowedInFunc reports whether ident is a parameter of, or a
// `:=`/`var` local declared in, ANY function or func-literal that lexically
// encloses the publish site, where funcScope is the text from the enclosing
// top-level func decl up to the site (review MUST-FIX #1; closure params
// added in re-review). When true the identifier is NOT a package-level
// binding at this call site, so it must not resolve against the file-global
// table. Conservative: a false "shadowed" only costs recall (skip an edge),
// never a wrong edge — so scanning EVERY func(...) param list in funcScope
// (not just lexically-enclosing ones) is a safe over-approximation.
func goIdentifierShadowedInFunc(funcScope, ident string) bool {
	if goShortVarDeclReFor(ident).MatchString(funcScope) {
		return true
	}
	if goVarDeclReFor(ident).MatchString(funcScope) {
		return true
	}
	// Parameter of ANY function/closure header in funcScope — including
	// func-literal params, which a single top-level-decl probe would miss.
	identRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(ident) + `\b`)
	for _, loc := range goAnyFuncParamOpenRe.FindAllStringIndex(funcScope, -1) {
		paramOpen := loc[1] - 1 // regex ends at the param-list '('.
		params := extractBalancedParensEngine(funcScope, paramOpen)
		if identRe.MatchString(params) {
			return true
		}
	}
	return false
}

// resolveAllowlistedEventType tries the direct string-literal match first
// (findAllowlistedEventType); when that fails, it falls back to the
// identifier form (GAP-015 RC4) — a single wrapper-call whose sole argument
// is a bare identifier resolvable via resolveIdent. resolveIdent encapsulates
// the file-global binding lookup PLUS the enclosing-function shadow guard, so
// a param/local shadowing the identifier yields no resolution. A formatter/
// transformer wrapper is rejected before resolution, and the RESOLVED const
// value itself is gated through isEventTypeValueUsable so a const bound to a
// `%`-format template (e.g. `const tmpl = "order.%s.placed"`) does not leak
// via the identifier path either (review MUST-FIX #2 + re-review). viaConst
// reports which path matched, so callers can tag the emitted edge.
func resolveAllowlistedEventType(
	text string,
	resolveIdent func(name string) (string, bool),
) (key, value string, viaConst, ok bool) {
	if key, value, ok = findAllowlistedEventType(text); ok {
		return key, value, false, true
	}
	if m := eventTypeAllowlistKeyIdentRe.FindStringSubmatch(text); m != nil {
		wrapper, ident := m[2], m[3]
		if isEventTypeWrapperFormatter(wrapper) {
			return "", "", false, false
		}
		if v, found := resolveIdent(ident); found && isEventTypeValueUsable(v) {
			return m[1], v, true, true
		}
	}
	return "", "", false, false
}

// enclosingGoFuncBodyStart returns the byte offset of the nearest preceding
// Go function/method declaration before offset, bounded by the same 4000-byte
// lookback window findEnclosingGoFunctionName uses. Used to widen the
// producer-recall search (see applyEventTypeProducerGo) to "earlier in this
// function" without leaking into a PRIOR function's body.
func enclosingGoFuncBodyStart(src string, offset int) int {
	lookback := offset - 4000
	if lookback < 0 {
		lookback = 0
	}
	window := src[lookback:offset]
	matches := goFunctionDeclRe.FindAllStringIndex(window, -1)
	if len(matches) == 0 {
		return lookback
	}
	last := matches[len(matches)-1]
	return lookback + last[0]
}

// applyEventTypeProducerGo scans Go source for publish call-sites and, at
// each one, extracts an allowlisted key/string-literal pair.
//
// Precision boundary #1 (co-location, the common case): the key/value pair
// appears inside the call's own argument list — mirrors the windowed
// extraction event_bus_edges.go uses.
//
// Precision boundary #2 (function-scope recall, GAP-005 root-cause C):
// real producers frequently build the event/envelope struct SEPARATELY from
// the publish call — `evt := OrderEvent{EventType: "OrderPlaced"}` a few
// lines above `client.PutRecord(ctx, &kinesis.PutRecordInput{Data: body})` —
// so the co-location gate alone finds nothing on realistic code. When the
// argument list has no match, widen the search to the rest of the enclosing
// function's body (from the nearest preceding `func` decl up to the call
// site). This still requires a real publish sink in the SAME function, so it
// cannot mint from an arbitrary unrelated struct elsewhere in the file.
//
// KNOWN LIMITATION (review FOLLOW-UP #4): the function-scope widening is
// co-location-agnostic WITHIN a function — it cannot tell whether the
// DetailType/eventType it finds earlier in the function actually belongs to
// the payload handed to THIS publish call. A function that builds an
// unrelated `{DetailType: "OrderPlaced"}` struct and then, separately, calls
// PutEvents on a DIFFERENT payload will still attribute OrderPlaced to that
// PutEvents. This is inherent to the pre-existing function-scope-struct-field
// path (GAP-005) and is newly reachable for EventBridge via the RC2 PutEvents
// sink; tightening it to per-payload co-location is a follow-up.
func applyEventTypeProducerGo(
	path, src string,
	emitEdge func(fromID, verbatim, kind string, props map[string]string),
) {
	bindings := buildGoStringBindingTable(src)
	for _, m := range goPublishSiteRe.FindAllStringIndex(src, -1) {
		// Enclosing-function text from its decl up to the publish site — used
		// both for the shadow guard (review MUST-FIX #1) and the function-scope
		// recall widening below.
		bodyStart := enclosingGoFuncBodyStart(src, m[0])
		funcScope := src[bodyStart:m[0]]
		resolveIdent := func(name string) (string, bool) {
			if goIdentifierShadowedInFunc(funcScope, name) {
				return "", false
			}
			v, found := bindings[name]
			return v, found
		}

		openParen := m[1] - 1 // regex ends in `\(`, so m[1]-1 is the '(' index.
		arg := extractBalancedParensEngine(src, openParen)
		key, value, viaConst, ok := resolveAllowlistedEventType(arg, resolveIdent)
		detection := "publish-site-literal"
		if viaConst {
			detection = "eventbridge-detailtype-const"
		}
		if !ok {
			key, value, viaConst, ok = resolveAllowlistedEventType(funcScope, resolveIdent)
			detection = "function-scope-struct-field"
			if viaConst {
				detection = "eventbridge-detailtype-const"
			}
		}
		if !ok {
			continue
		}
		caller := findEnclosingGoFunctionName(src, m[0])
		emitEdge(
			producerSourceRef("go", path, caller),
			value,
			"PUBLISHES_TO",
			map[string]string{"lang": "go", "key": key, "detection": detection},
		)
	}
}

// ---------------------------------------------------------------------------
// Producer — Go — event-store write (GAP-015 RC1)
// ---------------------------------------------------------------------------
//
// The dominant Go event-producer family does NOT publish to a broker at all.
// Instead it builds a domain event via a constructor whose event-type is a
// POSITIONAL string-literal argument (not a `key: "value"` field), stashes it
// in a struct, and persists that struct via an event-store write — a
// semantic method like WriteEvent/PublishEvent/SaveEvent/AppendEvent whose
// backing is commonly a DynamoDB table with Streams fan-out. Neither the
// existing goPublishSiteRe (no broker call here) nor the allowlisted
// key/literal extraction (the event name isn't in `key: "value"` form) sees
// this shape, so today no producer edge exists for it.

// goEventStoreWriteSiteRe matches a call to a SEMANTIC event-store write
// function — the verb+Event shape (WriteEvent/PublishEvent/SaveEvent/
// StoreEvent/AppendEvent and any `...Event(s)...` variant sharing one of
// those verb prefixes, e.g. WriteEventBatch, WriteEvents,
// PublishEventToStore). Matched on the METHOD/FUNCTION NAME only (\b-anchored,
// so it fires for both a bare package-level call `WriteEvent(...)` and a
// method call `store.WriteEvent(...)` — the leading `.` is not required, it
// just naturally satisfies the \b boundary when present).
//
// `Event` is matched as a TOKEN, not a substring prefix. Go's regexp is RE2
// (no lookahead), so the token boundary is expressed by the RE2-compatible
// suffix group `(?:s|[A-Z0-9_]\w*)?` that must be immediately followed by
// `\s*\(`: what follows `Event` is either nothing (call `(`), a pluralizing
// `s` (`WriteEvents(`), or an uppercase/digit/underscore word start
// (`WriteEventBatch(`) — but NEVER a lowercase-letter continuation. This is
// what stops `*Eventually(` (Event + "ually" — a database consistency-mode
// helper, review surface 1) from matching: `SaveEventually`/`StoreEventually`/
// `WriteEventually`/`PublishEventually`/`AppendEventually`/
// `StoreEventuallyConsistent` all have a LOWERCASE `u` after `Event`, so the
// suffix group cannot bridge to the trailing `(` and the match fails.
//
// Verb set (review): `Record`/`Emit` were DROPPED — they overwhelmingly
// denote analytics/telemetry emission (`analytics.RecordEvent(...)`,
// `tracer.EmitEvent(...)`), NOT a domain event-store write contract.
// Precision-first: event-store writes are overwhelmingly
// Write/Publish/Save/Store/Append, so dropping the two telemetry-collision
// verbs removes a confirmed end-to-end false-positive class at negligible
// recall cost.
//
// Deliberately NOT matched: raw DynamoDB `PutItem`/`PutItemWithContext`. That
// primitive is used for every DynamoDB write in a codebase (order tables,
// user tables, config tables, ...) with zero relationship to event
// publishing — gating on it would treat nearly any DynamoDB write beside an
// event constructor as a producer edge, a massive false-positive source.
// Only the SEMANTIC verb+Event call name is trusted as a sink.
//
// v1 residual (name-only matching, documented): `WriteEventLog(` (audit-log
// writer) and a mock `SaveEventCalled(` still match — they are
// indistinguishable by NAME from a legit `WriteEventBatch(` without dataflow.
// Accepted as low-frequency residual; see the RC1 report.
var goEventStoreWriteSiteRe = regexp.MustCompile(
	`\b(?:Write|Publish|Save|Store|Append)\w*Event(?:s|[A-Z0-9_]\w*)?\s*\(`,
)

// goEventStoreConstructorCallRe matches an event-constructor call — the
// New/Make/Build+Event shape (`NewOrderEvent(`, `MakeBillingEvent(`,
// `BuildOrderEvent(`, `NewOrderPlacedEvent(`) — on the CONSTRUCTOR FUNCTION
// NAME only, mirroring the sink regex's syntax-only matching (no
// corpus-specific package/function name). Uses the same RE2 `Event`-token
// boundary as the sink regex so `NewEventually(`-style helpers are not
// mistaken for event constructors.
var goEventStoreConstructorCallRe = regexp.MustCompile(
	`\b(?:New|Make|Build)\w*Event(?:s|[A-Z0-9_]\w*)?\s*\(`,
)

// goQuotedStringRe matches a double-quoted string literal in a constructor
// call's argument-list text. Go string literals in source are conventionally
// double-quoted (backtick raw strings for a positional event-name argument
// are not modeled in v1, matching the scope of this detector).
var goQuotedStringRe = regexp.MustCompile(`"([^"\n\r]+)"`)

// goIDLikeValueRe matches a value that looks like an identifier/UUID token
// rather than an event-type contract name: an all-lowercase-alnum run,
// optionally kebab/snake-segmented (`order-123`, `a1b2`,
// `550e8400-e29b-41d4-a716-446655440000`). Combined with a digit-presence
// check in isGoEventTypeNameShape, this rejects the event-sourcing idiom
// where the event TYPE is in the constructor NAME and the first literal is an
// aggregate/ID (review surface 6b). Dot-separated names (`order.placed`) are
// deliberately NOT matched here (dot is not a segment separator in this
// regex), so a legit lowercase dotted event name is NOT rejected.
var goIDLikeValueRe = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

// isGoEventTypeNameShape reports whether value is plausibly an event-type
// contract token rather than an aggregate/ID. It rejects a value that is
// ID-shaped (goIDLikeValueRe) AND carries a digit — the lowercase-kebab/
// snake-with-digits and UUID-ish shapes. A PascalCase/dotted name
// (`OrderPlaced`, `order.placed`) has an uppercase letter or a dot and so is
// NOT matched by goIDLikeValueRe, and is kept. Precision-first: a rare
// all-lowercase-with-digit legit event name (e.g. `orderv2placed`) is a
// documented residual recall loss, not a false positive.
func isGoEventTypeNameShape(value string) bool {
	if goIDLikeValueRe.MatchString(value) && strings.ContainsAny(value, "0123456789") {
		return false
	}
	return true
}

// nearestGoEventConstructorLiteral scans scope (function-scope text ending
// right before the write-sink call) for event-constructor calls and returns
// the event-name string literal of the LEXICALLY NEAREST one — i.e. the last
// constructor match in scope, closest to the sink.
//
// Ambiguity handling (precision over recall):
//   - Multiple constructors before one sink: only the nearest is considered;
//     attributing EVERY constructor literal in scope to the one sink would
//     risk a wrong edge for an earlier, unrelated construction.
//   - MULTIPLE string literals inside the nearest constructor's args (review
//     surface 6a/6c): the first-literal heuristic cannot tell an event-name
//     argument from a source/topic/region argument or a nested-wrapper
//     literal (`NewOrderEvent(ctx, "orders-svc", "OrderSettled")`,
//     `NewOrderEvent(id, aws.String("region-us"), "OrderPlaced")`), so the
//     whole binding is DROPPED. Only a SINGLE, unambiguous string literal is
//     trusted.
//   - The single literal is additionally gated through isEventTypeValueUsable
//     (formatter/`%`-template — review MUST-FIX #2 parity) and
//     isGoEventTypeNameShape (ID/UUID shape — review surface 6b).
func nearestGoEventConstructorLiteral(scope string) (value string, ok bool) {
	matches := goEventStoreConstructorCallRe.FindAllStringIndex(scope, -1)
	if len(matches) == 0 {
		return "", false
	}
	nearest := matches[len(matches)-1]
	openParen := nearest[1] - 1 // regex ends in `\(`, so len-1 is the '(' index.
	arg := extractBalancedParensEngine(scope, openParen)
	lits := goQuotedStringRe.FindAllStringSubmatch(arg, -1)
	// Precision: only a SINGLE unambiguous string literal is trusted. Zero
	// literals (event name is a var/const — not resolved in v1) or more than
	// one literal (ambiguous positional arg) both drop.
	if len(lits) != 1 {
		return "", false
	}
	v := lits[0][1]
	if !isEventTypeValueUsable(v) || !isGoEventTypeNameShape(v) {
		return "", false
	}
	return v, true
}

// applyEventTypeProducerGoEventStore scans Go source for event-store write
// call-sites (goEventStoreWriteSiteRe) and, for each one, looks BACKWARD
// within the enclosing function's body for the nearest preceding
// event-constructor call carrying a positional string-literal event name
// (goEventStoreConstructorCallRe + nearestGoEventConstructorLiteral). Both
// the write sink AND the constructor literal must be present in the SAME
// enclosing-function scope — mirrors applyEventTypeProducerGo's
// enclosing-function-scope mechanism (enclosingGoFuncBodyStart). Emits
// nothing when either is absent, or the recovered literal fails the
// usability guard (formatter/`%`-template).
//
// v1 limitation: the constructor call must textually PRECEDE the write sink
// within the function (the realistic "build then persist" order) — a
// constructor call built inline as an argument to the sink call itself, or
// one that lexically follows the sink, is not matched. See RC1 report for
// the full v1-limitations list.
func applyEventTypeProducerGoEventStore(
	path, src string,
	emitEdge func(fromID, verbatim, kind string, props map[string]string),
) {
	for _, m := range goEventStoreWriteSiteRe.FindAllStringIndex(src, -1) {
		bodyStart := enclosingGoFuncBodyStart(src, m[0])
		funcScope := src[bodyStart:m[0]]
		value, ok := nearestGoEventConstructorLiteral(funcScope)
		if !ok {
			continue
		}
		caller := findEnclosingGoFunctionName(src, m[0])
		emitEdge(
			producerSourceRef("go", path, caller),
			value,
			"PUBLISHES_TO",
			map[string]string{"lang": "go", "detection": "event-store-constructor-arg"},
		)
	}
}

// ---------------------------------------------------------------------------
// Producer — JS/TS
// ---------------------------------------------------------------------------

// jstsPublishSiteRe matches common JS/TS publish call-sites: generic
// `.publish(`/`.send(`/`.sendMessage(`/`.produce(`/`.emit(` method calls
// (covers AWS SDK v2 style, ioredis/kafkajs, EventEmitter) plus the AWS SDK
// v3 `new XCommand(` construction shape (SNS/SQS/Kinesis).
var jstsPublishSiteRe = regexp.MustCompile(
	`\.(?:publish|send|sendMessage|produce|emit)\s*\(` +
		`|new\s+\w*(?:PublishCommand|SendMessageCommand|PutRecordCommand|PutRecordsCommand)\s*\(`,
)

// enclosingNodeFuncBodyStart mirrors enclosingGoFuncBodyStart for JS/TS,
// bounded by the same 4000-byte lookback window findEnclosingNodeFunctionName
// uses.
func enclosingNodeFuncBodyStart(src string, offset int) int {
	lookback := offset - 4000
	if lookback < 0 {
		lookback = 0
	}
	window := src[lookback:offset]
	matches := nodeFunctionNameForOffsetRe.FindAllStringIndex(window, -1)
	if len(matches) == 0 {
		return lookback
	}
	last := matches[len(matches)-1]
	return lookback + last[0]
}

// applyEventTypeProducerJSTS mirrors applyEventTypeProducerGo for JS/TS,
// including the function-scope recall widening (GAP-005 root-cause C) for
// the `const evt = {eventType:"X"}; ...; client.send(evt)` shape.
func applyEventTypeProducerJSTS(
	lang, path, src string,
	emitEdge func(fromID, verbatim, kind string, props map[string]string),
) {
	for _, m := range jstsPublishSiteRe.FindAllStringIndex(src, -1) {
		openParen := m[1] - 1
		arg := extractBalancedParensEngine(src, openParen)
		key, value, ok := findAllowlistedEventType(arg)
		detection := "publish-site-literal"
		if !ok {
			bodyStart := enclosingNodeFuncBodyStart(src, m[0])
			key, value, ok = findAllowlistedEventType(src[bodyStart:m[0]])
			detection = "function-scope-struct-field"
		}
		if !ok {
			continue
		}
		caller := findEnclosingNodeFunctionName(src, m[0])
		emitEdge(
			producerSourceRef(lang, path, caller),
			value,
			"PUBLISHES_TO",
			map[string]string{"lang": "javascript", "key": key, "detection": detection},
		)
	}
}

// ---------------------------------------------------------------------------
// Producer — Java (GAP-015 RC5)
// ---------------------------------------------------------------------------

// javaPutEventsSiteRe matches the EventBridge publish sink keyed on the AWS
// SDK method idiom (NOT on any corpus class/variable name): `.putEvents(`.
//
// v1 deliberately keys ONLY on EventBridge `putEvents`. The generic
// `.publish(` sink was DROPPED: it matches Reactor/RxJava `.publish()`
// operators, `Optional`/stream chains, and arbitrary custom buses, and —
// because SNS `.publish(` carries no `detailType` (it uses subject / message
// attributes) — a detailType-gated `.publish(` only ever fired for
// EventBridge-shaped code or false positives. SNS + other publish idioms
// (Kinesis `.putRecord(s)`, SQS `.sendMessage`, Kafka `.send`) are deferred
// to the follow-up (see the v1 limitations section in the report).
var javaPutEventsSiteRe = regexp.MustCompile(`\.putEvents\s*\(`)

// javaDetailTypeRe matches the event-name binding keyed on the AWS SDK
// idiom: the v2 fluent-builder `.detailType("X")` or the v1/setter form
// `.setDetailType("X")`. Java string literals are always double-quoted, so
// (unlike the Go/JS-TS allowlist regex) this doesn't need to handle
// single/backtick quoting.
//
// v1: STRING-LITERAL argument only. A variable-bound or constant-bound
// detail type (`.detailType(EVENT_NAME)`, `.detailType(evt.type())`) is a
// follow-up — resolving it needs a symbol table, out of scope for v1.
var javaDetailTypeRe = regexp.MustCompile(`\.(?:detailType|setDetailType)\s*\(\s*"([^"\n\r]+)"\s*\)`)

// findJavaDetailType scans arg (the balanced-paren argument text of a
// putEvents call) for the first `.detailType(...)`/`.setDetailType(...)`
// string-literal binding. Returns ("", false) when none is found.
//
// LOW-1 (v1): a single putEvents carrying two entries with distinct
// detailType("A")/detailType("B") captures ONLY the first — findJavaDetailType
// returns the first regex match. Multi-detailType fan-out per putEvents is a
// follow-up.
func findJavaDetailType(arg string) (value string, ok bool) {
	m := javaDetailTypeRe.FindStringSubmatch(arg)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// balancedParenArgBounds returns the [openParen+1, close) byte range of the
// first balanced `(`…`)` starting at openParen (which must point at the `(`).
// Returns ok=false when the parens never balance before EOF.
//
// Unlike extractBalancedParensEngine, this takes the depth count over a
// caller-supplied (comment/string-MASKED) copy so that an unbalanced `(`
// inside a string literal in the putEvents argument — e.g. a `:(` emoticon or
// a URL in the free-text EventBridge `detail` JSON — does not desync the depth
// counter and swallow the whole file (MEDIUM-1). The caller slices the value
// from the ORIGINAL source using the returned offsets (offsets are identical
// because the mask preserves length).
func balancedParenArgBounds(masked string, openParen int) (start, end int, ok bool) {
	depth := 0
	for i := openParen; i < len(masked); i++ {
		switch masked[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return openParen + 1, i, true
			}
		}
	}
	return 0, 0, false
}

// maskJavaCommentsAndStrings returns a copy of src with every `//` line
// comment, `/* */` block comment, and `"..."` string-literal SPAN replaced by
// spaces of equal length (newlines preserved). Byte offsets are therefore
// identical to the original, so a regex match position in the masked copy
// maps 1:1 back to src. This is what makes a `putEvents(` inside a comment or
// a log-line string NOT count as a real sink (review finding #2). char
// literals (`'x'`) are stepped over so an apostrophe-in-a-string edge does
// not desync the scanner. Java text blocks (`"""..."""`) are not specially
// handled — v1 masks them as a sequence of ordinary string tokens, which is
// safe (over-masking, never under-masking).
func maskJavaCommentsAndStrings(src string) string {
	b := []byte(src)
	out := make([]byte, len(b))
	copy(out, b)
	blank := func(i int) {
		if b[i] != '\n' && b[i] != '\r' {
			out[i] = ' '
		}
	}
	const (
		normal = iota
		lineComment
		blockComment
		str
		charLit
	)
	state := normal
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch state {
		case normal:
			switch {
			case c == '/' && i+1 < len(b) && b[i+1] == '/':
				state = lineComment
				blank(i)
			case c == '/' && i+1 < len(b) && b[i+1] == '*':
				state = blockComment
				blank(i)
			case c == '"':
				state = str
				blank(i)
			case c == '\'':
				state = charLit
				blank(i)
			}
		case lineComment:
			blank(i)
			if c == '\n' {
				state = normal
			}
		case blockComment:
			blank(i)
			if c == '*' && i+1 < len(b) && b[i+1] == '/' {
				blank(i + 1)
				i++
				state = normal
			}
		case str:
			blank(i)
			if c == '\\' && i+1 < len(b) {
				blank(i + 1)
				i++
			} else if c == '"' {
				state = normal
			}
		case charLit:
			blank(i)
			if c == '\\' && i+1 < len(b) {
				blank(i + 1)
				i++
			} else if c == '\'' {
				state = normal
			}
		}
	}
	return string(out)
}

// applyEventTypeProducerJava detects a Java EventBridge producer keyed on the
// AWS SDK idiom: an EventBridge `.putEvents(...)` sink whose builder-chain
// argument CO-LOCATES a `.detailType("X")`/`.setDetailType("X")` string
// literal —
// `client.putEvents(PutEventsRequest.builder().entries(PutEventsRequestEntry.
// builder().detailType("OrderPlaced").build()).build())`. The detailType is
// bound to the SAME call's balanced-paren argument, so unlike a loose
// whole-method-scope co-existence check it cannot associate an unrelated
// `.detailType(...)` with an unrelated publish (review finding #1).
//
// Sinks inside comments or string literals are excluded: putEvents positions
// are located in a comment/string-MASKED copy of the source (review finding
// #2), then the argument is extracted from the ORIGINAL source at the same
// offset (offsets are preserved by the mask).
//
// v1 narrowing (breadth deferred to the follow-up issue):
//   - EventBridge `putEvents` sink ONLY — generic `.publish(`, SNS, SQS,
//     Kinesis, Kafka sinks are not modeled.
//   - detailType must be CO-LOCATED in the putEvents call argument — the
//     v1/setter form built in a separate statement (`entry.setDetailType(
//     "X"); client.putEvents(req)`) and any cross-method/cross-file binding
//     is deferred (would need call-graph / dataflow resolution).
//   - STRING-LITERAL detailType only (variable/constant-bound deferred).
func applyEventTypeProducerJava(
	path, src string,
	emitEdge func(fromID, verbatim, kind string, props map[string]string),
) {
	methods := indexJavaEnclosingMethods(src)
	masked := maskJavaCommentsAndStrings(src)
	for _, m := range javaPutEventsSiteRe.FindAllStringIndex(masked, -1) {
		openParen := m[1] - 1 // regex ends in `\(`, so m[1]-1 is the '(' index.
		// Balance the parens over the MASKED copy so an unbalanced `(` inside
		// a string literal in the argument (e.g. a `:(` emoticon in the
		// EventBridge `detail` JSON) can't swallow the file (MEDIUM-1); then
		// slice the detailType value from the ORIGINAL source at the same
		// offsets (mask preserves length) so the masked-out literal is visible.
		start, end, ok := balancedParenArgBounds(masked, openParen)
		if !ok {
			continue
		}
		value, ok := findJavaDetailType(src[start:end])
		if !ok {
			continue
		}
		caller := enclosingJavaMethodAt(methods, m[0])
		if caller == "" {
			// No enclosing method (e.g. a static field initializer): the
			// fromID would be the bare `SCOPE.Function:` prefix, which
			// emitEdge's fromID=="" guard does not catch. Reject (review
			// finding #3).
			//
			// LOW-2 (v1): a static-field-initializer putEvents placed AFTER a
			// method attributes to that nearest-preceding method (shared
			// enclosingJavaMethodAt limitation) rather than being rejected;
			// only a putEvents with NO preceding method in the file is caught
			// here. Precise class-vs-method scoping is a follow-up.
			continue
		}
		emitEdge(
			producerSourceRef("java", path, caller),
			value,
			"PUBLISHES_TO",
			map[string]string{"lang": "java", "key": "detailType", "detection": "publish-site-literal"},
		)
	}
}

// ---------------------------------------------------------------------------
// Consumer — event-source-mapping FilterCriteria (GAP-003 fold-in)
// ---------------------------------------------------------------------------

// eventTypeArrayKeyRe matches an (optionally quoted, case-insensitive)
// `eventType`/`detailType`/`detail-type` key immediately followed by a
// flow-style array opener — the shape FilterCriteria.Pattern's `data.
// eventType: [...]` (or bare `eventType`/`detail-type`) takes in both native
// HCL (`eventType = [...]`) and flow-style YAML/JSON (`eventType: [...]`).
var eventTypeArrayKeyRe = regexp.MustCompile(
	`["']?\b(?i:eventType|detailType|detail-type)\b["']?\s*[:=]\s*\[`,
)

// quotedStringRe extracts a single quoted string value (single or double
// quotes) — used to pull the individual event-type values out of the
// bracketed array extractEventTypeArrayValues locates.
var quotedStringRe = regexp.MustCompile(`["']([^"'\n\r]+)["']`)

// extractEventTypeArrayValues finds every `eventType`/`detailType`/
// `detail-type` array in src (there may be more than one FilterCriteria
// filter block) and returns the de-duplicated union of quoted string values,
// in first-seen order.
func extractEventTypeArrayValues(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range eventTypeArrayKeyRe.FindAllStringIndex(src, -1) {
		bracketPos := m[1] - 1 // regex ends in `\[`.
		inner := extractBalancedBracket(src, bracketPos)
		for _, sm := range quotedStringRe.FindAllStringSubmatch(inner, -1) {
			v := sm[1]
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// hclEventSourceMappingRe matches `resource "aws_lambda_event_source_mapping"
// "name"` blocks.
var hclEventSourceMappingRe = regexp.MustCompile(`resource\s+"aws_lambda_event_source_mapping"\s+"(\w+)"`)

// hclESMFunctionNameArnRe extracts `function_name = aws_lambda_function.<name>.arn`.
var hclESMFunctionNameArnRe = regexp.MustCompile(`function_name\s*=\s*aws_lambda_function\.(\w+)\.arn`)

// hclESMFunctionNameLiteralRe extracts `function_name = "<name>"`.
var hclESMFunctionNameLiteralRe = regexp.MustCompile(`function_name\s*=\s*"([^"]+)"`)

// applyEventTypeConsumerHCL parses Terraform `aws_lambda_event_source_mapping`
// resources for a FilterCriteria pattern enumerating event-type values,
// mirroring/generalizing applyEventBridgeHCL's rule-block extraction
// (event_bus_edges.go:220-264) to the ESM `data.eventType` shape.
func applyEventTypeConsumerHCL(
	src string,
	emitEdge func(fromID, verbatim, kind string, props map[string]string),
) {
	if !strings.Contains(src, "aws_lambda_event_source_mapping") {
		return
	}
	for _, m := range hclEventSourceMappingRe.FindAllStringSubmatchIndex(src, -1) {
		resName := src[m[2]:m[3]]

		blockStart := m[1]
		bracePos := strings.Index(src[blockStart:], "{")
		if bracePos < 0 {
			continue
		}
		bracePos += blockStart
		body := extractBalancedBraces(src, bracePos)

		lambdaName := ""
		if fm := hclESMFunctionNameArnRe.FindStringSubmatch(body); fm != nil {
			lambdaName = fm[1]
		} else if fm := hclESMFunctionNameLiteralRe.FindStringSubmatch(body); fm != nil {
			lambdaName = fm[1]
		}
		if lambdaName == "" {
			continue
		}

		values := extractEventTypeArrayValues(body)
		if len(values) == 0 {
			continue
		}

		lambdaID := lambdaFunctionID(lambdaName)
		fromID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaID)
		for _, v := range values {
			emitEdge(fromID, v, "SUBSCRIBES_TO",
				map[string]string{"iac": "terraform", "resource": resName, "detection": "event-source-mapping-filter-criteria"})
		}
	}
}

// serverlessESMFunctionNameRe finds the nearest preceding `<2-space-indent>
// <name>:` YAML mapping key under a `functions:` stanza — a best-effort
// heuristic to attribute a filterPatterns block (which is nested several
// levels under that function) back to its owning function name without a
// full YAML parse.
var serverlessESMFunctionNameRe = regexp.MustCompile(`(?m)^  (\w+):\s*$`)

// applyEventTypeConsumerServerlessYML parses serverless.yml `stream.
// filterPatterns` stanzas (flow-style `eventType: [...]` arrays) for
// event-type values, attributing them to the nearest enclosing function
// name found by serverlessESMFunctionNameRe. Mirrors
// applyEventBridgeServerlessYML's text-only path.
func applyEventTypeConsumerServerlessYML(
	src string,
	emitEdge func(fromID, verbatim, kind string, props map[string]string),
) {
	if !strings.Contains(src, "filterPatterns") && !strings.Contains(src, "filter_criteria") {
		return
	}
	for _, m := range eventTypeArrayKeyRe.FindAllStringIndex(src, -1) {
		bracketPos := m[1] - 1
		inner := extractBalancedBracket(src, bracketPos)
		var values []string
		seen := map[string]bool{}
		for _, sm := range quotedStringRe.FindAllStringSubmatch(inner, -1) {
			v := sm[1]
			if v != "" && !seen[v] {
				seen[v] = true
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			continue
		}

		// Attribute to the nearest preceding function name.
		fnName := ""
		for _, fm := range serverlessESMFunctionNameRe.FindAllStringSubmatchIndex(src, -1) {
			if fm[0] > m[0] {
				break
			}
			fnName = src[fm[2]:fm[3]]
		}
		if fnName == "" {
			continue
		}

		lambdaID := lambdaFunctionID(fnName)
		fromID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaID)
		for _, v := range values {
			emitEdge(fromID, v, "SUBSCRIBES_TO",
				map[string]string{"iac": "serverless.yml", "function_name": fnName, "detection": "event-source-mapping-filter-criteria"})
		}
	}
}

// ---------------------------------------------------------------------------
// Consumer — SAM / CloudFormation template FilterCriteria (GAP-005 review
// FIX 1 — the dominant IaC form the HCL + serverless.yml paths missed)
// ---------------------------------------------------------------------------

// cfnTemplateGateRe recognizes a CloudFormation / SAM template. Any of:
// the `AWSTemplateFormatVersion` header, the SAM `Transform: AWS::Serverless`
// macro, or a resource `Type:` naming a SAM function / raw Lambda ESM. A
// serverless-framework serverless.yml (handled separately above) carries
// none of these tokens, so it never double-mints here.
var cfnTemplateGateRe = regexp.MustCompile(
	`AWSTemplateFormatVersion` +
		`|Transform\s*:\s*['"]?AWS::Serverless` +
		`|Type\s*:\s*['"]?AWS::Serverless::Function` +
		`|Type\s*:\s*['"]?AWS::Lambda::EventSourceMapping`,
)

// cfnResourcesRe finds the top-level `Resources:` mapping key (column 0).
var cfnResourcesRe = regexp.MustCompile(`(?m)^Resources:\s*$`)

// cfnTopLevelKeyRe finds a column-0 mapping key — the boundary that ends the
// Resources block (the next top-level section, e.g. `Outputs:`).
var cfnTopLevelKeyRe = regexp.MustCompile(`(?m)^\S`)

// cfnLogicalIDLineRe matches an indented CFN logical-id header line
// (`  MyFn:` on its own line). Group 1 = leading whitespace, group 2 = id.
var cfnLogicalIDLineRe = regexp.MustCompile(`(?m)^(\s+)(\w+):\s*$`)

// cfnResourceTypeRe extracts a resource's `Type: AWS::...` value.
var cfnResourceTypeRe = regexp.MustCompile(`Type\s*:\s*['"]?(AWS::[A-Za-z0-9:]+)`)

// cfnFunctionNameRefRe extracts the standalone-ESM target function:
// `FunctionName: !Ref MyFn`, `FunctionName: !GetAtt MyFn.Arn`, or a literal
// `FunctionName: "my-fn"`. Group 1 = the intrinsic-ref logical id (Ref /
// GetAtt), group 2 = a literal name.
var cfnFunctionNameRefRe = regexp.MustCompile(
	`FunctionName\s*:\s*(?:!Ref\s+(\w+)|!GetAtt\s+(\w+)\.[A-Za-z]+|['"]?([\w-]+)['"]?)`,
)

// cfnPatternValueRe extracts a FilterCriteria `Pattern:` value in either of
// the two real-world shapes: a single-line single-quoted JSON string (group
// 1 — the compact form, e.g. `- Pattern: '{ "eventType": ["X"] }'`), or a
// multi-line DOUBLE-quoted YAML string with backslash-`\"`-escaped inner
// quotes (group 2 — the shape SAM/hand-authored templates actually emit to
// keep a long JSON pattern human-readable across lines, e.g.
// `- Pattern: "{ \"eventType\": [\n      \"X\"\n    ] }"`). The synthetic
// unit tests that shipped with GAP-005 only covered the single-quoted form,
// so the double-quoted/escaped/multi-line shape silently matched nothing on
// a real corpus (root cause of the zero-SCOPE.EventType-nodes bug). Group 2
// uses `(?:[^"\\]|\\.)*` so an escaped `\"` never terminates the match early.
var cfnPatternValueRe = regexp.MustCompile(`-\s*Pattern\s*:\s*(?:'([^']*)'|"((?:[^"\\]|\\.)*)")`)

// cfnPatternLineFoldRe matches a newline plus any following indentation
// inside a Pattern value — the YAML double-quoted-scalar line-continuation
// whitespace that separates `\"eventType\": [` from the next `\"X\",` line.
// Folding it away (rather than to a space) keeps extractEventTypeArrayValues'
// no-newline-in-quotes character class working over the flattened string.
var cfnPatternLineFoldRe = regexp.MustCompile(`\r?\n\s*`)

// normalizeCFNPatternValue turns a raw Pattern value (as captured by
// cfnPatternValueRe, before or after unescaping) into a single-line JSON-ish
// string safe for extractEventTypeArrayValues: fold line continuations away,
// then unescape `\"` -> `"` so the embedded JSON's own quoting survives the
// outer YAML double-quoted-string escaping.
func normalizeCFNPatternValue(s string) string {
	s = cfnPatternLineFoldRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, `\"`, `"`)
	return s
}

// collectCFNPatternValues finds every FilterCriteria `Pattern:` value in
// block (there may be more than one Filters entry) and returns their
// normalized bodies joined by a space, ready for extractEventTypeArrayValues.
// Returns "" when block has no Pattern field.
func collectCFNPatternValues(block string) string {
	matches := cfnPatternValueRe.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		return ""
	}
	var parts []string
	for _, m := range matches {
		raw := m[1]
		if m[2] != "" {
			raw = m[2]
		}
		if raw == "" {
			continue
		}
		parts = append(parts, normalizeCFNPatternValue(raw))
	}
	return strings.Join(parts, " ")
}

// applyEventTypeConsumerCFN parses SAM / CloudFormation templates for
// event-source-mapping FilterCriteria.Pattern `data.eventType` arrays and
// mints SUBSCRIBES_TO edges. Two shapes:
//
//  1. Inline `Events:` on an `AWS::Serverless::Function` — the consumer is
//     the enclosing function's logical id.
//  2. Standalone `AWS::Lambda::EventSourceMapping` — the consumer is the
//     `FunctionName: !Ref <id>` (or !GetAtt / literal) target.
//
// The `Pattern` value is a JSON string; extractEventTypeArrayValues finds the
// `eventType`/`detailType`/`detail-type` array inside it verbatim (the JSON
// string's `"eventType": [...]` matches eventTypeArrayKeyRe directly). Only a
// real `FilterCriteria` block is scanned — nothing looser.
func applyEventTypeConsumerCFN(
	src string,
	emitEdge func(fromID, verbatim, kind string, props map[string]string),
) {
	if !cfnTemplateGateRe.MatchString(src) {
		return
	}
	// Isolate the Resources: mapping — logical-id blocks live only there.
	loc := cfnResourcesRe.FindStringIndex(src)
	if loc == nil {
		return
	}
	body := src[loc[1]:]
	if end := cfnTopLevelKeyRe.FindStringIndex(body); end != nil {
		body = body[:end[0]]
	}

	// Detect the logical-id indent from the first header line under Resources.
	first := cfnLogicalIDLineRe.FindStringSubmatch(body)
	if first == nil {
		return
	}
	childIndent := first[1]

	// Split body into per-logical-id blocks at that indent.
	headerRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(childIndent) + `(\w+):\s*$`)
	heads := headerRe.FindAllStringSubmatchIndex(body, -1)
	for i, h := range heads {
		logicalID := body[h[2]:h[3]]
		blockStart := h[1]
		blockEnd := len(body)
		if i+1 < len(heads) {
			blockEnd = heads[i+1][0]
		}
		block := body[blockStart:blockEnd]

		// Precision gate: only real FilterCriteria blocks.
		if !strings.Contains(block, "FilterCriteria") {
			continue
		}
		tm := cfnResourceTypeRe.FindStringSubmatch(block)
		if tm == nil {
			continue
		}
		resType := tm[1]

		patternText := collectCFNPatternValues(block)
		if patternText == "" {
			continue
		}
		values := extractEventTypeArrayValues(patternText)
		if len(values) == 0 {
			continue
		}

		// Resolve the consumer function name per shape.
		fnName := ""
		props := map[string]string{"iac": "cloudformation", "detection": "event-source-mapping-filter-criteria"}
		switch resType {
		case "AWS::Serverless::Function":
			// Shape 1 — inline Events on the SAM function; the function IS the
			// enclosing logical id.
			fnName = logicalID
			props["sam_function"] = logicalID
		case "AWS::Lambda::EventSourceMapping":
			// Shape 2 — standalone ESM; target is FunctionName: !Ref <id>.
			if fm := cfnFunctionNameRefRe.FindStringSubmatch(block); fm != nil {
				switch {
				case fm[1] != "":
					fnName = fm[1]
				case fm[2] != "":
					fnName = fm[2]
				default:
					fnName = fm[3]
				}
			}
			props["esm_resource"] = logicalID
		default:
			continue
		}
		if fnName == "" {
			continue
		}

		fromID := fmt.Sprintf("%s:%s", serverlessFunctionKind, lambdaFunctionID(fnName))
		for _, v := range values {
			emitEdge(fromID, v, "SUBSCRIBES_TO", props)
		}
	}
}
