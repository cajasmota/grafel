// Producer-side + consumer-side detectors for the generic event-identity
// pass (event_type_edges.go, GAP-005).
package engine

import (
	"fmt"
	"regexp"
	"strings"
)

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

// isEventTypeValueUsable rejects a captured value that cannot be a stable wire
// contract: one carrying a `%` format verb (a `fmt.Sprintf` template such as
// `order.%s.placed`), which never verbatim-joins a consumer (review
// MUST-FIX #2).
func isEventTypeValueUsable(value string) bool {
	return !strings.Contains(value, "%")
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
	src string,
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
			fmt.Sprintf("SCOPE.Function:%s", caller),
			value,
			"PUBLISHES_TO",
			map[string]string{"lang": "go", "key": key, "detection": detection},
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
	src string,
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
			fmt.Sprintf("SCOPE.Function:%s", caller),
			value,
			"PUBLISHES_TO",
			map[string]string{"lang": "javascript", "key": key, "detection": detection},
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
