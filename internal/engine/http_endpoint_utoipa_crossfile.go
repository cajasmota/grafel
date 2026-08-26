// http_endpoint_utoipa_crossfile.go — additive CROSS-FILE registration markers
// for utoipa_axum `routes!(handler)` (#6668, final arm of #6560/@arthurgeron).
//
// The gap
// -------
// synthesizeUtoipaAxumRoutes (http_endpoint_utoipa_axum.go) resolves a
// `routes!(h)` argument against an attribute map built from THIS FILE ONLY. In
// the canonical utoipa_axum layout the handler and its `#[utoipa::path]` live in
// one module and the router that registers them in another:
//
//	// src/items.rs   #[utoipa::path(get, path = "/items")] pub async fn list_items()
//	// src/router.rs  use crate::items::list_items;
//	//                OpenApiRouter::new().routes(routes!(list_items))
//
// `attrs[list_items]` misses in router.rs, so that pass emits nothing and the
// registration is invisible to every consumer of the endpoint family.
//
// Why this is a MARKER and not a mint
// -----------------------------------
// The decision was taken on #6560 and is recorded there: file-scoped mint,
// cross-file work moved to resolve/link time. Two alternatives were rejected
// with reasons, and both reasons are structural rather than cautious:
//
//   - A GROUP-WIDE attribute map is not implementable in this layer at all. The
//     emit-level dedupe it would have to cooperate with is `seen`, declared at
//     http_endpoint_synthesis.go:443 INSIDE a per-file detector pass and
//     reconstructed for every file. There is no place in that pass to hold
//     group state.
//   - An OUT-OF-BAND minter (the Django nested-urlconf shape) is the only true
//     cross-file producer in the tree, and its cost is the argument against
//     copying it: a pass wired in from cmd/grafel/index.go, a pass-wide `seen`
//     PLUS a per-file `scanned` guard added by #6417 to repair an identity bug
//     the pass itself caused, a per-file mount dedupe that deliberately does not
//     consult the pass-wide map, and two post-hoc subtraction passes. Landing
//     that while #6530 (one synthetic ID, two claimants) is still open would
//     trade a missing endpoint for a corrupted identity.
//
// So this follows the FastAPI mount-point precedent instead
// (fastapiMountPointSynthetics, http_endpoint_synthesis.go), which faced the
// identical choice and refused to go cross-file for a reason written in-tree:
// "the router is constructed in a different file in the canonical FastAPI
// layout, and a cross-file read would not survive incremental indexing or the
// daemon fast path." Like that pass, this one emits an ADDITIVE record carrying
// a JOIN KEY resolved from THIS FILE'S OWN IMPORTS, and stops there. Nothing is
// folded, no path is invented, no existing entity changes shape — so the result
// is byte-identical under a full index, `--incremental`, and the daemon fast
// path.
//
// THE STATED BOUND. This does NOT produce a canonical http_endpoint_definition
// for the cross-file handler. It is a correct partial chosen over an incorrect
// whole, and the marker is a PROMISE whose consumer is filed separately rather
// than implied — this repo's precedent is that such promises sit unredeemed
// (#6414, the FastAPI prefix-fold consumer of #6385's join key, is still open).
// Until that consumer lands, the marker's value is that the registration is
// visible and joinable at all, where before it was absent.
//
// What is emitted, and when NOTHING is
// ------------------------------------
// One SCOPE.Route / Subtype="utoipa_handler_registration" record per handler,
// per file, carrying `handler_module` + `handler_name` (the join key) and
// `mount_prefix` when a same-file `.nest(...)` supplies one. The kind is
// deliberately NOT in the http_endpoint family: an endpoint record with no path
// would be migrated to an http_endpoint_definition by the #1217 legacy-kind
// migration in ResolveHTTPEndpointHandlers and become a pathless phantom in the
// canonical family — exactly the invention the #6150 rule forbids. SCOPE.Route
// also appears in no resolverKindEquivalents list, so the marker can never be
// mistaken for a HANDLER by another endpoint's source_handler lookup.
//
// Emission is refused, silently and completely, whenever the registration is
// not unambiguously cross-file and unambiguously joinable:
//
//   - the handler HAS a same-file `#[utoipa::path]` contract — Arm A/B1 already
//     mints a real definition for it, and a marker beside that definition would
//     be a second claimant for one handler (#6530's hazard, in miniature);
//   - the handler is registered by another producer reading this same file — a
//     `.route(...)` (axumRouteRe) or a Rocket verb attribute (rocketRouteAttrRe),
//     via the same utoipaRegisteredElsewhere key the mint uses;
//   - the handler name is not bound by a `use` declaration in this file, or is
//     bound only through a glob (`use crate::items::*;`). The join key is then
//     unknown, and a marker with a guessed module is worse than no marker;
//   - the handler name is CONTESTED: more than one `use` declaration in this
//     file names it, and this pass models neither scopes nor `cfg` evaluation
//     so it cannot tell which is live. This holds whether or not the competing
//     declarations were themselves publishable — a competitor rejected for its
//     own reasons (a MULTI-segment `super::`/`self::` path, a malformed path)
//     still contests the name (#6675). Two shapes do NOT contest: one that
//     names no local at all (a glob), and a SINGLE-segment relative path
//     (`mod tests { use super::create_item; }`), which re-exports the parent
//     scope's own binding rather than rivalling it;
//   - the same handler is named twice, in one macro or across several. The
//     marker is emitted once, deduped on its ID by emitAdditiveSynthetic in
//     applyHTTPEndpointSynthesis — the same one-line contract the FastAPI mount
//     synthetic uses. A SECOND, function-local `emitted` guard was written and
//     then DELETED: it was dead code, verified by mutation. Its removal changes
//     no observable result, and TestUtoipaCrossFile_MarkerNotEmittedTwice_6668
//     pins the surviving behaviour rather than the deleted helper.
//
// Deliberately still unhandled, and NOT worked around here:
//
//   - a path-qualified argument, `routes!(crate::items::list)`. utoipaRoutesMacroRe
//     admits bare identifiers only and fails the WHOLE macro on a qualified one;
//     widening it is a change to the mint's recognition rule, not to this marker,
//     and is out of scope for this arm.
//   - CROSS-FILE `.nest(...)`. `mount_prefix` is stamped only from a nest visible
//     in this file's own bytes; a router nested from another file yields no
//     prefix rather than a guessed one.
//   - the PATHS-AGREE duplicate: one handler whose utoipa mint in file A and
//     whose `.route(...)` in file B canonicalise to the SAME synthetic ID. That
//     is one ID with two claimants and it is #6530, still open. This file does
//     not touch it — neither file in that layout emits a marker (A has the
//     attribute, B has no `routes!`), so the behaviour is unchanged, and it is
//     pinned as such rather than left implied.
//
// House behaviour, inherited and not new: a `use` declaration or a `routes!`
// invocation inside a BLOCK comment or a string literal is still read as real
// code, exactly as synthesizeAxumRoutes reads a commented-out `.route(`. A
// LINE-commented `use` is not, because the declaration scan is anchored at the
// start of a line.
package engine

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// utoipaCrossFileSubtype is the Subtype stamped on every marker, and the field
// a future consumer should key on together with the Kind.
const utoipaCrossFileSubtype = "utoipa_handler_registration"

// utoipaCrossFilePatternType mirrors the `pattern_type` convention the other
// additive synthetics use (`url_mount_point`), so a consumer can select these
// records by property without depending on the Kind string.
const utoipaCrossFilePatternType = "utoipa_handler_registration"

// rustUseBinding is one name brought into this file's scope by a `use`
// declaration: the module path it came from and the name it has THERE (which
// differs from the local name under `as`).
type rustUseBinding struct {
	module string
	name   string
}

// rustUseDeclRe matches a whole `use ...;` declaration, anchored at the start of
// a line so a `//`-commented declaration is never read. The body is parsed by
// hand rather than by regex because the brace-group form nests.
var rustUseDeclRe = regexp.MustCompile(`(?m)^[ \t]*(?:pub(?:\s*\([^)]*\))?[ \t]+)?use[ \t]+([^;]+);`)

// rustUsePathRe validates a module path: `crate::items`, `super::api::v1`,
// `self::handlers`. Whitespace and `*` are rejected, so a glob import or a
// malformed fragment yields no binding at all.
var rustUsePathRe = regexp.MustCompile(`^[A-Za-z_]\w*(?:::[A-Za-z_]\w*)*$`)

// rustUseIdentRe validates a leaf name.
//
// Widening it to admit `*` is EQUIVALENT under the CURRENT mint recognition
// rule and only that rule: a `*` binding is unreachable because
// utoipaRoutesMacroRe admits only `[A-Za-z_]\w*` as a macro argument. If that
// rule is ever widened to path-qualified arguments (`routes!(crate::items::x)`,
// listed as future work on #6669), the equivalence LAPSES and this validation
// becomes load-bearing — so widen the two together or not at all.
var rustUseIdentRe = regexp.MustCompile(`^[A-Za-z_]\w*$`)

// rustUseLeafAsRe splits a `<path> as <alias>` leaf. The left side is a PATH,
// not just an identifier: `use crate::{items::create_item as mk};` is legal and
// an identifier-only left side silently resolved it to nothing, because the
// `::` split below then ran on the text `create_item as mk`.
var rustUseLeafAsRe = regexp.MustCompile(`^([A-Za-z_][\w:]*)[ \t\r\n]+as[ \t\r\n]+([A-Za-z_]\w*)$`)

// rustPoisonLocal marks a local name CONTESTED (#6675): any binding already
// published for it is withdrawn and no later declaration may republish it. It
// is called from BOTH the collision path (two publishable bindings for one
// name) and every rejection path where a local name is derivable — the two are
// the same fact, "some declaration names this local and this pass cannot tell
// which one is live", and only the second used to be silent.
func rustPoisonLocal(out map[string]rustUseBinding, dropped map[string]bool, local string) {
	delete(out, local)
	dropped[local] = true
}

// rustAddUseLeaf records one leaf of a `use` declaration under the LOCAL name
// the rest of the file will spell, while the binding keeps the name the
// DECLARING module knows it by — those differ under `as`, and the join key must
// be the declaring module's own pair or it cannot match anything there.
//
// ORDER MATTERS. The alias is split off FIRST and its left side is allowed to be
// a path, because `use crate::{items::create_item as mk};` is both legal and
// real. Splitting on `::` first left the text `create_item as mk` as the item
// name, which failed identifier validation and resolved the whole leaf to
// nothing — restrictive, therefore invisible.
//
// COLLISION POLICY: DROP, not first-wins. See rustUseBindings.
//
// `self`, `*`, and any leaf that survives neither shape are dropped: none of
// them names a resolvable single item.
func rustAddUseLeaf(out map[string]rustUseBinding, dropped map[string]bool, module, leaf string) {
	name, local := leaf, leaf
	aliased := false
	if m := rustUseLeafAsRe.FindStringSubmatch(leaf); m != nil {
		name, local, aliased = m[1], m[2], true
	}
	// A leaf may itself be a PATH — `use crate::{items::create_item,
	// admin::purge};` is the most common rustfmt grouping, and rejecting it
	// would leave the dominant real-world spelling silently unresolved. The
	// leading segments belong to the module, the last to the item.
	if cut := strings.LastIndex(name, "::"); cut >= 0 {
		module, name = module+"::"+name[:cut], name[cut+2:]
		if !aliased {
			local = name
		}
	}
	// ─── THE CONTESTED-NAME BOUNDARY (#6675) ───────────────────────────────
	//
	// Everything below this line REJECTS a binding. The question asked at each
	// rejection is NOT "can this binding be published?" but "does this
	// declaration NAME a local?" — because a survivor published beside an
	// unresolvable competitor is published as UNAMBIGUOUS, and since #6669
	// resolves on (handler_module, handler_name) that MIS-JOINS rather than
	// merely missing. So recording a name as contested happens EARLIER than
	// rejecting its binding: every rejection below poisons `local`.
	//
	// The boundary is ruled here rather than left to the reader. A local name
	// is derivable exactly when `local` is an identifier other than `self`:
	//
	//   - GLOB (`use crate::items::*;`) — `local` is `*`. NO local name exists,
	//     so nothing is contested and NOTHING may be poisoned. Poisoning on a
	//     glob would silently drop a legitimate marker for a name the glob
	//     never mentioned — invisible, because there is no error, just a
	//     missing enrichment. Do not invent a name here.
	//   - BARE `self` (`use crate::items::{self};`) — the name Rust binds is
	//     the MODULE's last segment (`items`), which this leaf does not carry;
	//     deriving it would be a second guess. Left alone deliberately. `self
	//     as alias` DOES carry one (`alias`) and is contested below.
	//
	// The one further rejection that does not poison is the NESTED GROUP,
	// refused whole in rustUseBindings before any leaf is seen — its leaves are
	// not reliably parseable, so no local name can be derived safely. That is
	// ruled at the rejection site there, not here.
	if !rustUseIdentRe.MatchString(local) || local == "self" {
		return
	}
	// rustUsePathRe is the ONLY thing standing between the concatenation above
	// and a published garbage join key: `use crate::{::create_item};` would
	// otherwise yield module `crate::`, and `{1bad::create_item}` would yield
	// `crate::1bad`. Pinned by TestUtoipaCrossFile_MalformedUsePathRefused_6668.
	// The path is unusable, but the leaf still names `local` — contested.
	if module == "" || !rustUsePathRe.MatchString(module) {
		rustPoisonLocal(out, dropped, local)
		return
	}
	// The item name is unusable while `local` is a real name this file spells.
	if !rustUseIdentRe.MatchString(name) {
		rustPoisonLocal(out, dropped, local)
		return
	}
	// `use crate::items::{self as it};` binds the MODULE under `it`, not an
	// item — unpublishable, but `it` is named and therefore contested.
	if name == "self" {
		rustPoisonLocal(out, dropped, local)
		return
	}
	// A `self::`- or `super::`-rooted module is resolvable only against the
	// declaring file's own position in the module tree, which this per-file pass
	// does not know. Stamping it verbatim would publish a join key that cannot
	// match anything by identity — a guess wearing the shape of an answer, which
	// is what #6150 forbids. `crate::…` and an absolute crate path are both
	// resolvable and are kept. The leaf names `local`, so it is a candidate for
	// contesting — with ONE exception, ruled below.
	if root := module; root == "self" || root == "super" ||
		strings.HasPrefix(root, "self::") || strings.HasPrefix(root, "super::") {
		// SEGMENT COUNT DECIDES, and it is a semantic rule rather than a
		// convenience carve-out. A relative path with EXACTLY ONE segment after
		// the root — `use super::create_item;`, i.e. module == "super" — names
		// whatever `create_item` ALREADY IS in the parent scope. That is a
		// re-export of the binding under consideration, not a rival to it, so
		// it cannot be a competitor and must not contest anything. This is the
		// archetypal unit-test prelude:
		//
		//	use crate::real::create_item;
		//	mod tests { use super::create_item; }
		//
		// present in essentially every Rust file that has tests. Poisoning
		// there would drop a legitimate marker silently — no error, just a
		// missing enrichment — and would also make the pass disagree with
		// itself between two spellings of one idiom, since `use super::*;`
		// names no local and never contested. `self::create_item` is the same
		// shape rooted at the current module.
		//
		// A MULTI-segment relative path — `use super::stub::create_item;` —
		// reaches into a SIBLING module and genuinely can name a different
		// item, which is #6675's first witness. It contests.
		if module == "self" || module == "super" {
			return
		}
		rustPoisonLocal(out, dropped, local)
		return
	}
	if dropped[local] {
		return
	}
	if prev, exists := out[local]; exists {
		if prev.module == module && prev.name == name {
			// The same binding declared twice names one item; not ambiguous.
			return
		}
		// AMBIGUOUS: two declarations bind one local name to DIFFERENT items.
		// Neither can be published, so the name is poisoned for the rest of the
		// file — a later third declaration must not resurrect it either.
		rustPoisonLocal(out, dropped, local)
		return
	}
	out[local] = rustUseBinding{module: module, name: name}
}

// rustUseBindings maps LOCAL name → declaring (module, name) for every `use`
// declaration in this file that names a single importable item.
//
// Both the plain form (`use crate::items::list_items;`) and the single-level
// brace group (`use crate::items::{list_items, create_item};`) are read. A
// A NESTED group (`use crate::{items::{create_item, purge}, admin};`) is
// rejected WHOLE, and that guard is load-bearing rather than defensive. The
// argument that leaf validation subsumes it is FALSE, and the counter-example is
// pinned by TestUtoipaCrossFile_NestedUseGroupFabricatesNothing_6668: a scan
// that cuts the group at the first `}` splits out a bare `purge`, which passes
// leaf validation and is then attributed to the OUTER base — a join key of
// `crate::purge` for a handler that lives at `crate::items::purge`. Since #6669
// resolves on (handler_module, handler_name), an invented module MIS-JOINS
// rather than merely missing, which is the worse failure and exactly what #6150
// forbids. So: the group runs to the LAST `}`, and any `{`/`}` inside it kills
// the declaration.
//
// (An earlier revision deleted this guard on the strength of the subsumption
// argument, after a mutant that removed it survived. The mutant survived because
// the fixture labelled `nested-group` contained no nested braces. Deleting an
// unkillable guard is right only when the subsumption is DEMONSTRATED.)
//
// A glob (`use crate::items::*;`) is rejected on the leaf instead: `*` is not an
// identifier, so it names no item and no join key can be derived from it.
//
// COLLISION POLICY (review round 2). When two declarations bind ONE local name
// to DIFFERENT items, BOTH are dropped and the name is poisoned for the rest of
// the file. This is not first-wins, and the difference is deliberate.
//
// The previous revision had NO collision handling at all and justified that with
// the claim that two `use` declarations binding one local name is E0252 and does
// not compile. THAT CLAIM IS FALSE: E0252 is per-SCOPE, so both of these compile
// and both reached the map —
//
//	use crate::items::create_item;
//	mod tests { use crate::mocks::create_item; }   // different scope
//
//	#[cfg(feature = "x")]      use crate::real::create_item;
//	#[cfg(not(feature = "x"))] use crate::stub::create_item;
//
// — and because the map was LAST-WINS, a `mod tests` block (which by convention
// sits at the bottom of a file) always beat the top-level declaration. The
// published join key was then `crate::mocks`, which MIS-JOINS at #6669 rather
// than missing: the failure this arm calls worse than a phantom.
//
// First-wins would return the common cases to `crate::items`, but it is still a
// guess: it encodes "the first declaration in byte order is the one the macro
// meant", which is true by convention and not by the language. This pass does
// not model scopes or `cfg` evaluation, so it cannot know which binding is in
// effect at the registration site — and #6150's rule for exactly that situation
// is to leave the endpoint UNENRICHED rather than guess. Dropping matches what
// every other unresolvable shape here already does (glob, nested group,
// `self::`/`super::`).
//
// THE REACH OF THAT POLICY (#6675). Poisoning is triggered by NAMING a local,
// not by successfully binding one. Until #6675 the poison check sat AFTER the
// module-validity, `self::`/`super::` and glob/nested-group rejections, so a
// competing declaration of one of THOSE shapes returned before reaching the map
// and never poisoned:
//
//	#[cfg(a)] use crate::real::create_item;
//	#[cfg(b)] use super::stub::create_item;   // rejected upstream, did not poison
//
// which published `crate::real` — a guess at which cfg arm is live, and a
// MIS-JOIN at #6669 rather than a miss. Now the recording of a local name
// happens BEFORE the rejection of its binding, so both declarations above
// contest `create_item` and neither is published. The boundary of that rule —
// which rejections name a local and which genuinely name none (glob, bare
// `self`, the whole-group nested rejection) — is ruled at the contested-name
// boundary in rustAddUseLeaf and at the nested-group guard below.
//
// A group leaf that is itself a PATH — `use crate::{items::create_item,
// admin::purge};`, the most common rustfmt grouping — IS resolved: the leading
// segments join the base to form the module and the last names the item. An
// earlier revision rejected this shape, which left the dominant real-world
// spelling silently unresolved.
//
// A `self::`- or `super::`-rooted module is REFUSED. Both are resolvable only
// against the declaring file's own position in the module tree, which a per-file
// pass does not know, so the join key would be a guess in the shape of an
// answer. `crate::…` and an absolute crate path (`items::list`, naming another
// crate) are resolvable as identities and are kept.
func rustUseBindings(content string) map[string]rustUseBinding {
	out := map[string]rustUseBinding{}
	// Local names that two declarations bound to DIFFERENT items. Poisoned for
	// the whole file so a third declaration cannot resurrect one.
	dropped := map[string]bool{}
	for _, m := range rustUseDeclRe.FindAllStringSubmatch(content, -1) {
		spec := strings.TrimSpace(m[1])
		if spec == "" {
			continue
		}
		if open := strings.IndexByte(spec, '{'); open >= 0 {
			end := strings.IndexByte(spec, '}')
			if end < open {
				continue
			}
			// A NESTED group makes the whole declaration unreadable to a comma
			// split, and readable-looking in the worst way: see the header, and
			// TestUtoipaCrossFile_NestedUseGroupFabricatesNothing_6668. Rejecting
			// on the body's braces is the ONE guard for that — scanning to the
			// last '}' instead would also work, but two guards for one case
			// leaves each individually unkillable, so this file keeps exactly
			// one and pins it.
			//
			// #6675 — THIS REJECTION DOES NOT POISON, and unlike the glob and
			// bare-`self` carve-outs that is a KNOWN-OPEN HOLE rather than a
			// rule. Tracked as #6688; the fixture that records it is
			// `nested-group-competitor-follows`, which asserts `want 1` and
			// must be flipped to 0 when #6688 closes.
			//
			// The reason it is open, stated without overstating it: deriving a
			// BINDING from a nested group is unsafe, and that is this guard's
			// whole point — a naive cut at the first `}` attributes a bare
			// `purge` to the OUTER base and fabricates `crate::purge`. But
			// poisoning needs only the set of names MENTIONED, which is an
			// identifier scan, and `create_item` does occur literally in
			// `crate::{items::{create_item, purge}, admin}`. So this is a
			// deferred derivation, not an impossible one; the hazard to price
			// first is over-poisoning a module SEGMENT (`items`, `admin`),
			// which would drop a legitimate marker silently.
			if strings.ContainsAny(spec[open+1:end], "{") {
				continue
			}
			base := strings.TrimSpace(spec[:open])
			base = strings.TrimSpace(strings.TrimSuffix(base, "::"))
			for _, leaf := range strings.Split(spec[open+1:end], ",") {
				rustAddUseLeaf(out, dropped, base, strings.TrimSpace(leaf))
			}
			continue
		}
		idx := strings.LastIndex(spec, "::")
		if idx < 0 {
			continue
		}
		rustAddUseLeaf(out, dropped, strings.TrimSpace(spec[:idx]), strings.TrimSpace(spec[idx+2:]))
	}
	return out
}

// rustLineOfOffset returns the 1-based line number of a byte offset.
func rustLineOfOffset(content string, off int) int {
	if off < 0 {
		off = 0
	}
	if off > len(content) {
		off = len(content)
	}
	return 1 + strings.Count(content[:off], "\n")
}

// utoipaCrossFileRegistrationID keys a marker by the registration FILE and the
// declaring (module, name) it joins to. Including the file keeps two routers
// that register the same handler from collapsing onto one record — they are two
// registrations and a later consumer may need both mount points.
func utoipaCrossFileRegistrationID(relPath string, b rustUseBinding) string {
	return "utoipa:registration:" + relPath + ":" + b.module + "::" + b.name
}

// utoipaCrossFileRegistrations returns the additive markers for this file. See
// the file header for the emission rules and for the four cases that emit
// nothing.
func utoipaCrossFileRegistrations(content, relPath string) []types.EntityRecord {
	if !utoipaHasRoutesMacro(content) {
		return nil
	}
	imports := rustUseBindings(content)
	if len(imports) == 0 {
		return nil
	}
	// Same-file contracts are what the MINT consumes; a handler present here is
	// not a cross-file case at all and must not also get a marker.
	attrs := utoipaHandlerRoutes(content)
	registeredElsewhere := utoipaRegisteredElsewhere(content)
	nests := rustNestEntries(content)

	var out []types.EntityRecord
	for _, m := range utoipaRoutesMacroRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 || m[2] < 0 {
			continue
		}
		// Resolved once per MACRO, at the macro's own offset, exactly as the
		// mint does — every handler in `routes!(a, b)` shares one mount point.
		prefix := rustNestPrefixFor(content, nests, m[0])
		for _, handler := range utoipaMacroHandlerArgs(content[m[2]:m[3]]) {
			if _, sameFile := attrs[handler]; sameFile {
				continue
			}
			if registeredElsewhere[handler] {
				continue
			}
			b, bound := imports[handler]
			if !bound {
				continue
			}
			props := map[string]string{
				"framework":      "utoipa_axum",
				"pattern_type":   utoipaCrossFilePatternType,
				"handler_module": b.module,
				"handler_name":   b.name,
			}
			if prefix != "" {
				props["mount_prefix"] = prefix
			}
			out = append(out, types.EntityRecord{
				ID:                 utoipaCrossFileRegistrationID(relPath, b),
				Name:               utoipaCrossFileRegistrationID(relPath, b),
				QualifiedName:      utoipaCrossFileRegistrationID(relPath, b),
				Kind:               string(types.EntityKindRoute),
				Subtype:            utoipaCrossFileSubtype,
				SourceFile:         relPath,
				StartLine:          rustLineOfOffset(content, m[0]),
				Language:           "rust",
				EnrichmentRequired: false,
				EnrichmentStatus:   types.StatusPending,
				QualityScore:       0.5,
				Properties:         props,
			})
		}
	}
	return out
}
