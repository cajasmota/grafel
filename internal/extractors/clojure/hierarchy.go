// hierarchy.go — Clojure protocol/interface conformance: `defrecord`,
// `deftype`, `extend-type`, `extend-protocol`, `extend` and `(:gen-class)`
// (#6370).
//
// Before this, clojure emitted NO hierarchy edge by any of the three paths a
// language can get one, all three verified for clojure specifically rather
// than inherited from the issue's list:
//
//  1. this package contained no "EXTENDS"/"IMPLEMENTS" literal at all — its
//     edge vocabulary was CALLS / CONTAINS / IMPORTS only;
//  2. "clojure" is absent from `supportedLanguages` in
//     `internal/extractors/cross/hierarchy/extractor.go` (14 entries);
//  3. `internal/engine/rules/clojure/` exists but declares zero
//     `relationship:` rules — the pack is frameworks/orms/build-tools only.
//
// `deftypeRE` reached the declaration HEAD and stopped at the name, so "what
// implements this protocol" returned empty — indistinguishable from "nothing
// does".
//
// # The discriminator is TOKEN SHAPE AT PAREN-DEPTH 1, not position counting
//
// In `deftype`/`defrecord`/`extend-type`/`extend-protocol` the tail is an
// alternating sequence, at depth 1, of BARE SYMBOLS (the supertypes) and
// LISTS (the method implementations):
//
//	(defrecord Foo [a b]
//	  Bar                        ; depth-1 symbol  -> supertype
//	  (m [_] (println a) helper) ; depth-1 list    -> skipped whole
//	  java.lang.Runnable         ; depth-1 symbol  -> supertype
//	  (run [_] ...))
//
// The field vector needs no special reasoning and gets none: `[a b]` is simply
// one more depth-1 token that is neither a symbol nor a list, and the same is
// true of a map (`extend`'s method map) and of a keyword. So there is no
// "skip the first N tokens" rule to get wrong when a form omits or adds one —
// the shape decides, and it decides the same way for every one of these forms.
//
// The depth fence is the load-bearing one. `helper` and `println` above are
// symbols too; they are excluded because they sit at depth 2. Delete the depth
// test and every identifier in every method body becomes a supertype. `let`
// and `letfn` bindings are covered by the same fence and need no rule of their
// own — they are at depth >= 2 by construction.
//
// # `extend-protocol` is INVERTED, and nothing but the keyword says so
//
//	(extend-type    MyType  ProtoA ProtoB)  ; MyType    IMPLEMENTS ProtoA/ProtoB
//	(extend-protocol MyProto TypeA TypeB)   ; TypeA/TypeB IMPLEMENTS MyProto
//
// Both forms are "one head symbol then an alternating tail", so a single code
// path that reads the head as the subject emits every `extend-protocol` edge
// BACKWARDS while producing exactly the same edge COUNT. That is why the two
// keywords take different arms of the switch in scanClojureHierarchy, and why
// TestClojureHierarchy_ExtendProtocolIsInverted asserts owner->target pairs
// rather than counting rows: a row-count assertion passes with every edge
// reversed.
//
// # Edge kinds: a ladder, decided by the KEYWORD, never by the target's name
//
//   - defrecord / deftype / extend-type / extend-protocol / extend
//     -> IMPLEMENTS.
//     None of these forms has class inheritance: every depth-1 symbol in them
//     is a protocol, a Java/host interface, or Object/clojure.lang.* — all
//     conformance. The one residual ambiguity (is `Bar` a protocol or a Java
//     interface?) does not need answering, because BOTH answers are IMPLEMENTS.
//   - (:gen-class :extends X)    -> EXTENDS   (a real host superclass).
//   - (:gen-class :implements [A B]) -> IMPLEMENTS.
//
// Every arm is selected by the reader macro or keyword the scanner is standing
// in — a syntactic fact, decidable per file, needing no cross-file type
// resolution. Nothing here resembles `csLooksLikeInterfaceName`
// (internal/extractors/csharp/hierarchy.go), which guesses the kind from the
// target's spelling and is self-documented as wrong in both directions.
//
// # What is deliberately NOT handled
//
//   - `defprotocol` / `definterface` are NOT scanned, and the reason is not
//     "they have no supertypes" (true, but not the reason). Their tails admit
//     keyword/value OPTION pairs — `(defprotocol P :extend-via-metadata true
//     (m [this]))` — whose VALUE is a bare depth-1 symbol. Running the scan
//     over them would emit `P IMPLEMENTS true`. They are excluded at the call
//     site, by keyword, rather than filtered afterwards.
//     TestClojureHierarchy_DefprotocolAndDefinterfaceAreNotScanned pins it.
//   - `reify` and `proxy` produce ANONYMOUS instances. There is no named
//     entity to anchor the edge on, and an edge with no owning component is
//     the file-anchored shape internal/extractors/file_anchored_rels_guard_test.go
//     forbids. Out of scope.
//   - `defmulti` is not scanned either: `(defmulti area class)` has a bare
//     depth-1 symbol that is a DISPATCH FUNCTION, not a supertype.
//   - `java.util.Map$Entry` and other JVM nested classes ARE handled: `$` is
//     in the target charset. It was not in the first draft of this file, which
//     rejected such a target whole — a silent recall miss, not a fence.
//
// # Why the edges are emitted HERE and not by registering clojure in
// # cross/hierarchy
//
// That pass invents its own graph nodes: for every type it addEntity's a
// SCOPE.Component for the type AND another for each named parent.
// extractClojure already emits one SCOPE.Component per defrecord/deftype, so
// registering clojure there would mint a duplicate component per type with the
// edges anchored on the pass's own node rather than on the one the rest of the
// clojure graph uses. That is why #6335 emitted F#'s edges from the F#
// extractor, #6437 groovy's, #6804 nim's and #6810 pony's.
// TestClojureHierarchy_NoDuplicateComponents guards both halves.
//
// # The anchor, and the recall limit it buys
//
// A `defrecord`/`deftype` edge is anchored on the component the SAME form
// produced, matched by the byte offset of the form's opening paren — not by
// name, so two same-named forms in one file cannot cross-contaminate.
//
// An `extend-type`/`extend-protocol` edge names an IMPLEMENTER that the form
// itself does not declare. It is anchored on a SCOPE.Component this extractor
// already emits for that name IN THE SAME FILE, and when there is none the
// edge is DROPPED. That is a real recall limit — `(extend-type
// java.lang.String Proto)` yields nothing — and it is deliberate: the only
// alternatives are to synthesise a component for the implementer (which mints
// a duplicate node whenever the type is declared in another file, the exact
// defect this file avoids by not registering in cross/hierarchy) or to anchor
// the edge on the file (forbidden by the guard test).
// TestClojureHierarchy_ExtendTypeOnAForeignTypeIsDropped_KnownDivergence pins
// the limit so it is a recorded decision rather than a silent zero.
package clojure

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// clojureHierarchyHeadRE matches the head of every form whose tail is scanned:
// the opening paren, the keyword, and the symbol that follows it.
//
// The keyword list is the whole ladder-selection mechanism, so it is
// deliberately exhaustive rather than a prefix: `defprotocol`, `definterface`
// and `defmulti` are ABSENT for the reasons in the package header, and adding
// one is a behaviour change that the corresponding test will catch.
//
// `^\s*\(` — a line-start anchor that allows only WHITESPACE before the paren
// — is what excludes a form the reader never evaluates as a form: `'(defrecord
// ...)` and `#_(defrecord ...)` both put a non-whitespace character before the
// paren on that line, so neither is matched.
// TestClojureHierarchy_QuotedAndDiscardedFormsAreNotMatched grades it; without
// the anchor both produce edges.
//
// The captured symbol's charset is `deftypeRE`'s name charset plus `.`, `/`
// and `$`, i.e. the same set clojureTargetRE admits, and the parity is
// deliberate in both directions: this class captures a DECLARED name, so
// anything deftypeRE can name as an entity must be capturable here or the
// component exists with its edges anchored under a truncated name; and it also
// captures an `extend-*` head, which for `extend-protocol` is the edge's
// TARGET, so truncation there is a wrong answer rather than a missing one.
// `extend-type`'s subject is routinely a host or namespace-qualified name
// (`java.util.Date`, `other.ns/Rec`), which is why `.` and `/` are needed at
// all. TestClojureHierarchy_HeadCharsetMatchesDeclaredNames and
// TestClojureHierarchy_ExtendProtocolHeadKeepsItsQualifier grade it.
//
// `$` is in this class in BOTH positions, and both are graded by
// TestClojureHierarchy_HeadCharsetAdmitsDollarInBothPositions.
//
// An earlier revision of this comment called them equivalent, on the ground
// that the only heads a `$` could appear in are an `extend-type` subject
// (where a nested host class can never match a component this file declares,
// so the edge is dropped for want of an anchor either way) and an
// `extend-protocol` head. Both of those observations are true; the conclusion
// was not, because the list omitted the third head this regex captures — a
// `defrecord`/`deftype` DECLARED NAME, which becomes the `owner` of the
// self-edge check. `deftypeRE` stops at `$`, so for `(defrecord Foo$Bar ...)`
// the entity is `Foo` while this regex captures `Foo$Bar`; narrow this class
// and the owner no longer equals a `Foo$Bar` written in the tail, and a
// spurious self-edge appears. That is a behaviour difference, not an
// equivalence, and it was ALIVE against the whole suite until the test above
// was written.
var clojureHierarchyHeadRE = regexp.MustCompile(
	`(?m)^\s*\((defrecord|deftype|extend-type|extend-protocol|extend)\s+([\w\-\?!\*'+$]+(?:[\./][\w\-\?!\*'+$]+)*)`)

// clojureTargetRE validates one depth-1 token as a usable type name.
//
// The two anchors do DIFFERENT work and are graded separately, because with
// only one of them the other reads as untested:
//
//   - `^` rejects a token merely PREFIXED by punctuation — `:keyword`,
//     `^:private`, `#_discard`, `'quoted`, a number — WHOLE, instead of
//     letting the valid tail through.
//     TestClojureHierarchy_NonSymbolDepth1TokensAreRejectedWhole.
//   - `$` rejects a token that STARTS valid and ends invalid. Without it,
//     MatchString succeeds on any valid PREFIX while the caller still uses the
//     whole token as the ToID, so `other.ns/` — a namespace qualifier with its
//     name half missing — would become an edge target that can never resolve.
//     TestClojureHierarchy_TokenValidOnlyAtItsStartIsRejected.
//
// The regex has FOUR character positions, and they are separate guards that a
// single input rarely separates. Each is graded by its own row, because a row
// covering one says nothing about the others:
//
//		^[A-Za-z_]   [\w\-?!*'+$]*   (?:[./]   [\w\-?!*'+$]+ )*$
//		     P1            P2            P3           P4
//
//	  - P1, the LEADING class, deliberately excludes `$` while P2 and P4 admit
//	    it: `Map$Entry` is a type name, `$Bogus` is not. Widening P1 to admit
//	    `$` was ALIVE against the whole suite until
//	    TestClojureHierarchy_LeadingDollarIsRejectedPerToken was written — and
//	    that test puts the bad token BESIDE a good one, so it asserts the
//	    rejection is per TOKEN and not per form.
//	  - P2 and P4 admit `$` for JVM nested-class notation — `Map$Entry`,
//	    `IFn$LL` — which is ordinary host interop in a `deftype` body and in
//	    `(:gen-class :implements)`. Excluding it rejected the token WHOLE, so
//	    the symptom was a silent recall miss with no truncated name to notice,
//	    not a fence. The two positions need two rows: `java.util.Map$Entry`
//	    reaches `$` through P4 (it follows a `.`), and only an UNQUALIFIED
//	    `Map$Entry` reaches P2. Both exist.
//	  - P2 and P4 otherwise carry exactly `deftypeRE`'s name charset
//	    (`[\w\-?!*'+]`), and that parity is the point rather than a
//	    coincidence: any type this extractor can NAME as an entity must be
//	    nameable as a target, or a locally-declared protocol is silently
//	    unlinkable. TestClojureHierarchy_TargetCharsetMatchesDeclaredNames
//	    grades the parity with one deliberately ugly symbol.
//	  - P3 admits `.` and `/` (`java.lang.Runnable`, `other.ns/Proto`).
//
// P4's `+` (a separator must be followed by at least one name character) and
// the closing `$` anchor OVERLAP on `other.ns/`, which is the only input in
// the suite that either rejects, so each mutant dies on that one row.
//
// They are not, however, one guard, and an earlier revision of this comment
// said they were. The anchor alone also rejects a token that is valid only at
// its start with no separator involved at all — `Foo:bar` — which `+` has no
// opinion on. No row exercises that, so the two are currently graded only
// jointly, and `Foo:bar` is deliberately not added: `:` inside a symbol is
// reserved by the Clojure reader, so pinning its REJECTION would assert a
// decision this issue has not made, and pinning its acceptance would be a
// behaviour change outside it. Recorded so the gap is visible rather than
// dressed up as an equivalence.
var clojureTargetRE = regexp.MustCompile(`^[A-Za-z_][\w\-\?!\*'+$]*(?:[\./][\w\-\?!\*'+$]+)*$`)

// clojureGenClassExtendsRE and clojureGenClassImplementsRE read the two
// keyword-delimited slots of a `(:gen-class ...)` directive. They are applied
// ONLY to the text of that directive (see genClassEdges), never to the whole
// ns form: `:extends` is a legal keyword anywhere, and a file-wide search
// would read one out of `(:require [x :refer [:extends]])`.
var (
	clojureGenClassExtendsRE = regexp.MustCompile(
		`:extends\s+([\w\-\?!\*'+]+(?:[\./][\w\-\?!\*'+]+)*)`)
	clojureGenClassImplementsRE = regexp.MustCompile(`:implements\s+\[([^\]]*)\]`)
)

// clojureHierarchy is the whole per-file hierarchy result.
//
// byForm keys defrecord/deftype edges by the byte offset of their form's
// opening paren, which is what ties an edge to the component that the SAME
// form produced. byName keys extend-type/extend-protocol edges by the
// IMPLEMENTER's name, the only handle those forms give. nsEdges carries the
// (:gen-class) edges, which belong to the namespace component.
type clojureHierarchy struct {
	byForm  map[int][]types.RelationshipRecord
	byName  map[string][]types.RelationshipRecord
	nsEdges []types.RelationshipRecord
}

func (h *clojureHierarchy) forForm(open int) []types.RelationshipRecord {
	if h == nil {
		return nil
	}
	return h.byForm[open]
}

func (h *clojureHierarchy) forName(name string) []types.RelationshipRecord {
	if h == nil {
		return nil
	}
	return h.byName[name]
}

// scanClojureHierarchy reads `src` and returns every hierarchy edge it states.
//
// It scans a SANITISED copy of the source, not `src` itself: string literals
// and `;` comments are blanked by the pre-existing stripStringsAndComments,
// and `(comment ...)` forms by blankCommentForms. Both are length-preserving,
// so every offset in the result indexes back into the ORIGINAL `src`
// unchanged — which is what lets byForm keys be compared against offsets the
// entity loop computes from the raw source.
//
// Sanitising is where fences 2 and 3 live and they are not decorative: today
// `deftypeRE` runs on raw `src`, and its `^\s*\(` ALLOWS leading whitespace,
// so an indented `(defrecord X ...)` inside a `(comment ...)` block or inside
// a multi-line docstring is already matched and already produces an ENTITY.
// That entity over-fire is pre-existing and untouched here; what this scan
// guarantees is that no EDGE joins it. Delete either sanitiser and the
// corresponding fixture forbidden row fires.
func scanClojureHierarchy(src string) *clojureHierarchy {
	clean := blankCommentForms(stripStringsAndComments(src))
	h := &clojureHierarchy{
		byForm: map[int][]types.RelationshipRecord{},
		byName: map[string][]types.RelationshipRecord{},
	}

	for _, m := range clojureHierarchyHeadRE.FindAllStringSubmatchIndex(clean, -1) {
		kw := clean[m[2]:m[3]]
		head := clean[m[4]:m[5]]
		open := strings.LastIndexByte(clean[:m[4]], '(')
		if open < 0 {
			continue
		}
		line := strings.Count(clean[:open], "\n") + 1
		syms := depth1Symbols(clean, m[5])

		switch kw {
		case "defrecord", "deftype":
			// The supertypes are the depth-1 symbols; the field vector is
			// skipped by shape, not by counting.
			h.byForm[open] = appendImplements(nil, head, syms, line, "clojure_"+kw+"_body")
		case "extend-type", "extend":
			// Head is the SUBJECT, tail symbols are the protocols. `extend`
			// is the map form — `(extend AType AProto {:m (fn ...)})` — whose
			// method MAP is a depth-1 token that is neither symbol nor list,
			// so it is skipped by exactly the same shape rule that skips a
			// field vector, and it needs no arm of its own.
			h.byName[head] = appendImplements(h.byName[head], head, syms, line, "clojure_"+strings.ReplaceAll(kw, "-", "_"))
		case "extend-protocol":
			// INVERTED: head is the PROTOCOL, tail symbols are implementers.
			for _, s := range syms {
				h.byName[s] = appendImplements(h.byName[s], s, []string{head}, line, "clojure_extend_protocol")
			}
		}
	}

	// The (:gen-class) directive is read only inside the namespace form; see
	// genClassEdges for why a file-wide search fabricates edges.
	_, _, nsOpen := findNsForm(clean)
	h.nsEdges = genClassEdges(clean, nsOpen)
	return h
}

// appendImplements adds one IMPLEMENTS record per valid, non-self target,
// deduped WITHIN this call: `(defrecord Foo [] P (m [_] 1) P (n [_] 2))`
// states one fact and gets one edge.
//
// It deliberately does NOT dedup against records already in `out`. Such a
// pass was written and removed: `byForm` always calls with a nil `out`, and
// every `byName` list is deduped again by the `have` map at the call site in
// extractClojure before it reaches a record, so the extra pass could not
// change any output and was a claim no test could check. The cross-FORM case
// it appeared to cover is really the call site's, and
// TestClojureHierarchy_TargetRepeatedAcrossFormsYieldsOneEdge grades it there.
func appendImplements(out []types.RelationshipRecord, owner string, targets []string, line int, provenance string) []types.RelationshipRecord {
	seen := map[string]bool{}
	for _, t := range targets {
		if !clojureTargetRE.MatchString(t) || t == owner || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, newHierarchyEdge(t, "IMPLEMENTS", line, provenance))
	}
	return out
}

// newHierarchyEdge builds one embedded hierarchy relationship.
//
// FromID is deliberately left EMPTY. The assembly loop stamps the owning
// record's own entity id, the only value that anchors the edge on the
// COMPONENT. A non-empty non-hex FromID (e.g. the file path) would be
// rewritten by ReferencesEmbedded onto the FILE entity, merging every type in
// a multi-type file onto one node — the defect fixed in #6295 and #6298 and
// now guarded by internal/extractors/file_anchored_rels_guard_test.go.
//
// ToID is the symbol as WRITTEN, qualifier and all: unlike Pony, where the
// qualifier is a package alias that must be erased to bind, a Clojure target
// is either a namespace-qualified protocol or a fully-qualified Java class,
// and in both cases the written form is the resolvable one.
//
// types.Props is a key-SORTED slice whose Get binary-searches it (#6802), so
// the keys are set through Set rather than written as a literal.
func newHierarchyEdge(to, kind string, line int, provenance string) types.RelationshipRecord {
	var props types.Props
	props.Set("line", strconv.Itoa(line))
	props.Set("provenance", provenance)
	return types.RelationshipRecord{
		// FromID intentionally empty — see the doc comment above.
		ToID:       to,
		Kind:       kind,
		Properties: props,
	}
}

// genClassEdges reads `(ns foo (:gen-class :extends b.Base :implements [x.A]))`.
//
// This is the ONLY place in the language where a real superclass is named, and
// it is the only arm that emits EXTENDS. The two slots are keyword-delimited
// rather than positional, so they are read by keyword; the containing
// directive is located structurally (balanced parens from `(:gen-class`) so
// that the keywords are only ever read inside it.
//
// # Two guards on WHICH `(:gen-class` is read, and why both are needed
//
// `nsOpen` is the byte offset of the namespace form's opening paren, or -1
// when the file has none. The search is confined to that form, and this is not
// tidiness: `(:gen-class` is a KEYWORD, and in Clojure a keyword in head
// position is a function that looks itself up in a map. `(:gen-class m)` and
// `(:gen-class {:extends bogus.Fake})` are ordinary code that any programmer
// can write inside a defn, and a file-wide `strings.Index` reads the second
// one as a directive and FABRICATES `EXTENDS bogus.Fake`. Worse, it takes the
// FIRST hit and stops, so the real directive's edges are lost as well —
// a fabricated edge and a recall miss from one input.
// TestClojureHierarchy_GenClassKeywordOutsideTheNsFormIsNotADirective pins it.
//
// The whole-word check is a SECOND guard and is not redundant with the first:
// it rejects `(:gen-class-name ...)` written INSIDE the ns form, where the
// scoping cannot help. It was deleted once, on the reasoning that "Clojure has
// no directive beginning with :gen-class" — true, and beside the point, since
// the code never required a directive. The loop then continues rather than
// giving up, so a prefix keyword before the real one does not hide it.
// TestClojureHierarchy_GenClassPrefixKeywordInsideTheNsFormIsSkipped pins
// both halves.
func genClassEdges(clean string, nsOpen int) []types.RelationshipRecord {
	if nsOpen < 0 {
		return nil
	}
	nsEnd := matchParen(clean, nsOpen)
	i := -1
	for at := nsOpen; at < nsEnd; {
		k := strings.Index(clean[at:nsEnd], "(:gen-class")
		if k < 0 {
			break
		}
		at += k
		j := at + len("(:gen-class")
		if j >= nsEnd || isClojureBreak(clean[j]) {
			i = at
			break
		}
		at = j
	}
	if i < 0 {
		return nil
	}
	end := matchParen(clean, i)
	body := clean[i:end]
	line := strings.Count(clean[:i], "\n") + 1

	var out []types.RelationshipRecord
	if m := clojureGenClassExtendsRE.FindStringSubmatch(body); m != nil && clojureTargetRE.MatchString(m[1]) {
		out = append(out, newHierarchyEdge(m[1], "EXTENDS", line, "clojure_gen_class_extends"))
	}
	if m := clojureGenClassImplementsRE.FindStringSubmatch(body); m != nil {
		seen := map[string]bool{}
		for _, f := range strings.Fields(m[1]) {
			if !clojureTargetRE.MatchString(f) || seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, newHierarchyEdge(f, "IMPLEMENTS", line, "clojure_gen_class_implements"))
		}
	}
	return out
}

// depth1Symbols returns the bare symbol tokens at paren-depth 1 of the form
// whose interior the caller is already inside, scanning from byte offset
// `from` to the form's closing paren.
//
// THIS IS THE FENCE. Tokens are collected only while depth == 1; `(`, `[` and
// `{` all raise the depth, so a method implementation list, a field vector, a
// method map and every binding form nested in any of them are crossed whole
// and contribute nothing. Remove the `depth == 1` test and every identifier in
// every method body is reported as a supertype.
//
// Non-symbol depth-1 tokens are not special-cased here; they are collected as
// tokens and rejected by clojureTargetRE, which is anchored at both ends.
func depth1Symbols(clean string, from int) []string {
	var out []string
	depth := 1
	i := from
	for i < len(clean) {
		ch := clean[i]
		switch {
		case ch == '(' || ch == '[' || ch == '{':
			depth++
			i++
		case ch == ')' || ch == ']' || ch == '}':
			depth--
			if depth == 0 {
				return out
			}
			i++
		case isClojureSpace(ch):
			i++
		default:
			j := i
			for j < len(clean) && !isClojureBreak(clean[j]) {
				j++
			}
			if j == i {
				j++ // never stall on a byte isClojureBreak calls a break but no case above claimed
			}
			if depth == 1 {
				out = append(out, clean[i:j])
			}
			i = j
		}
	}
	return out
}

// blankCommentForms replaces every `(comment ...)` form with spaces, preserving
// length and newlines so that offsets and line numbers computed on the result
// still index the original source.
//
// `(comment ...)` is idiomatic Clojure — a rich-comment block of scratch forms
// at the bottom of a file — and its contents are NOT evaluated. Without this,
// an indented `(defrecord Scratch [] Proto ...)` inside one would produce a
// real IMPLEMENTS edge, because deftypeRE's `^\s*\(` allows leading whitespace.
//
// The name is matched WHOLE: the byte after "comment" must be a break, so
// `(commentary ...)` is left alone. Blanking the form's own parens as well as
// its interior keeps the surrounding depth count balanced.
func blankCommentForms(s string) string {
	b := []byte(s)
	const kw = "(comment"
	for i := 0; i+len(kw) <= len(b); i++ {
		if b[i] != '(' || string(b[i:i+len(kw)]) != kw {
			continue
		}
		if i+len(kw) < len(b) && !isClojureBreak(b[i+len(kw)]) {
			continue
		}
		end := matchParen(string(b), i)
		for k := i; k < end; k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
		i = end - 1
	}
	return string(b)
}

// matchParen returns the offset just past the paren matching the `(` at
// `open`, or len(s) when the form is unterminated. An unterminated form is
// treated as running to end of file, which is what the pre-existing
// findFormEnd does for entity end-lines.
func matchParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

func isClojureSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == ','
}

// isClojureBreak reports whether `c` ends a token. Note that `'` and `^` are
// NOT breaks: they are legal inside symbol names (`foo'`), and a LEADING one
// is rejected by clojureTargetRE's anchored start instead, which keeps
// `'Quoted` and `^:private` out as whole tokens rather than splitting them
// into a break plus a valid-looking name.
//
// `"` and `;` were in this set and are not any more. Both callers — the
// depth-1 tokeniser and blankCommentForms — read only the SANITISED source,
// and stripStringsAndComments blanks every string literal and every `;`
// comment to spaces before either runs, so neither character can reach here.
// Listing them was a claim no input could exercise, not a guard; removing them
// left the whole suite green, which is the evidence for that and not a
// substitute for it.
func isClojureBreak(c byte) bool {
	switch c {
	case '(', ')', '[', ']', '{', '}':
		return true
	}
	return isClojureSpace(c)
}
