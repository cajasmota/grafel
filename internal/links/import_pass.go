package links

import (
	"sort"
	"strings"
)

// isBareNameExt reports whether id is a bare-name external placeholder
// of the form "ext:<name>" with no module qualifier (no second ":" after
// the prefix). Retained for historical context (issue #509) and unit-
// test coverage; the import pass now uses isBuiltinExt below, which
// consults the entity's subtype rather than parsing the ID string.
//
// Background: #509 used this string-shape predicate as a precision
// filter, assuming "no second colon" === "bare built-in placeholder".
// Issue #566 disproved that assumption: real npm packages such as
// `ext:axios`, `ext:react`, `ext:@tanstack/react-query` also have a
// single colon, and were being silently dropped — emitting zero
// cross-repo import-method links across the client-fixture group even
// though all three repos genuinely share those packages.
//
// The correct discriminator is the entity's `subtype`:
//   - subtype="package" — real npm / PyPI / Maven module → eligible
//   - subtype="function" or other — bare-name built-in placeholder
//     (e.g. `[].filter()`, `array.split(...)`) → skip (preserves #509)
//
// See isBuiltinExt for the predicate the linker actually uses.
func isBareNameExt(id string) bool {
	const prefix = "ext:"
	if !strings.HasPrefix(id, prefix) {
		return false
	}
	rest := id[len(prefix):]
	if rest == "" {
		return true // pathological "ext:" — also bare/empty.
	}
	return !strings.Contains(rest, ":")
}

// isBuiltinExt reports whether id is an "ext:" external placeholder that
// should be skipped by the cross-repo import linker because it represents
// a bare-name built-in (e.g. `ext:filter`) rather than a real shared
// package (e.g. `ext:axios`, `ext:react:useState`).
//
// Decision matrix (id has "ext:" prefix):
//
//	subtype="package"            → admit (real npm/PyPI/Maven module)
//	id has a second ":"          → admit (qualified `ext:<module>:<name>`)
//	otherwise                    → skip (bare-name built-in placeholder)
//
// Background: #509 used the second-colon test alone as a precision
// filter. Issue #566 disproved that assumption — the external synthesiser
// emits real packages as `ext:<package>` (single colon, no second `:`)
// with subtype="package", so the synthesised npm packages such as
// `ext:axios`, `ext:react`, `ext:@tanstack/react-query` were being
// silently dropped. The subtype check restores them while still rejecting
// the dynamic-dispatch bare-name `ext:filter` / `ext:split` placeholders
// that motivated #509.
//
// Hand-rolled test fixtures that mint `ext:<name>` IDs without populating
// subtype continue to be filtered by the second-colon fallback — the
// existing #509 fixtures (`ext:filter` / `ext:react:useState`) keep
// behaviour. Real graphs always populate subtype via external-synth, so
// the fallback only matters in tests.
func isBuiltinExt(id string, subtypes map[string]string) bool {
	const prefix = "ext:"
	if !strings.HasPrefix(id, prefix) {
		return false
	}
	if subtypes[id] == "package" {
		return false // real shared package — admit.
	}
	rest := id[len(prefix):]
	if rest == "" {
		return true // pathological "ext:" — skip.
	}
	if strings.Contains(rest, ":") {
		return false // qualified `ext:<module>:<name>` — admit.
	}
	return true // bare-name built-in placeholder — skip.
}

// httpEndpointKindLink is the legacy entity Kind used by the synthetic
// http_endpoint emission pass (#534 Phase 1). After #1217 the synthesis
// pass emits http_endpoint_definition (producer) and http_endpoint_call
// (consumer). This constant is kept for the constant references below;
// use isHTTPEndpointLink() for kind-matching logic.
const httpEndpointKindLink = "http_endpoint"

// isHTTPEndpointLink reports whether kind is any of the three HTTP endpoint
// entity kinds (legacy + the two new split kinds from #1217). Used throughout
// the links package to match entities regardless of which extractor version
// produced them.
func isHTTPEndpointLink(kind string) bool {
	switch kind {
	case "http_endpoint", "http_endpoint_definition", "http_endpoint_call":
		return true
	}
	return false
}

// runImportPass implements P1: structural cross-repo imports/calls edges.
//
// Idempotent overwrite: every link previously emitted with method=import is
// replaced; entries from other passes survive untouched. Confidence comes
// from ScoreImport (structural — top of the band).
//
// Pair iteration: O(E) over edges, with explicit self-pair skipping and
// per-(source,target,method) dedupe so a graph that mentions the same
// edge twice (e.g. two extractor passes touching the same call site)
// emits exactly one link.
//
// Also handles #534 Phase 2: synthetic `http_endpoint` entities whose
// Name is a canonical `http:<METHOD>:<path>` string are matched across
// repos by Name (kind+name identity). When the same endpoint name shows
// up in two repos — typically because one repo is the backend that
// SERVES the route and the other is the frontend that CONSUMES it via
// a typed-client extractor (landing in #533) — emit a cross-repo
// import-method link. The frontend side isn't extracted yet, so this is
// a no-op on today's corpora but the linker change is required so the
// edges appear automatically when #533 ships.
func runImportPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "import"}

	// Build entity-id → repo map across the whole group. O(N) where N is
	// total entities; replaces what would otherwise be an O(repos × edges)
	// lookup if we re-scanned every repo per edge.
	entRepo := map[string]string{}
	// Per-repo entity-id → subtype map. Subtype MUST be looked up against
	// the repo the edge originates from, not against a merged group-wide
	// map. Issue #566 verification surfaced the failure mode:
	// `ext:log` is subtype="package" in a Go repo (where `log` is the
	// stdlib `log` package) but subtype="function" in a JavaScript repo
	// (where `log` is the bare `console.log` method). A merged map with
	// first-write-wins picked whichever repo loaded first and emitted
	// false-positive cross-repo links into the JS repo's bare-name
	// placeholders. Per-repo lookup keeps each side honest: the edge from
	// the JS repo's `local → ext:log` consults the JS repo's subtype
	// (function) and the bare-name filter rejects it correctly.
	subtypeByRepo := map[string]map[string]string{}

	// Issue #1507 — spurious cross-repo imports via shared ext:* placeholders.
	//
	// `ext:*` entity IDs are NOT repo-scoped: every repo that references the
	// same external package emits an entity with the SAME literal ID string.
	// The first-write-wins assignment of entRepo[extID] = firstRepo produces
	// an ARBITRARY repo attribution. A subsequent repo whose edge references
	// the same `ext:*` ID finds a DIFFERENT repo in entRepo, making the edge
	// look like a cross-repo import when it is an intra-repo reference.
	//
	// Concrete failure mode: if orders, analytics, order-saga, and workers all
	// import `py_shared`, each emits an `ext:py_shared` entity. entRepo assigns
	// ext:py_shared to whichever repo loaded first (say, analytics). Then every
	// orders/order-saga/workers edge `local_fn → ext:py_shared` resolves as
	// fromRepo=X, toRepo=analytics → spurious orders→analytics,
	// order-saga→analytics, workers→analytics cross-repo links are emitted.
	//
	// Fix (two complementary guards):
	//
	//  Guard A — repo-name collision: if the base name of the ext:* ID
	//   (everything after "ext:") exactly matches (case-folded, hyphen/
	//   underscore normalised) a slug in the indexed group, the external
	//   placeholder is a proxy for that repo's code. Remove it from entRepo
	//   entirely so the fromRepo/toRepo guard below treats it as unresolved.
	//   Example: ext:py_shared with group containing "py-shared" → removed.
	//   This ensures imports of a grouped library don't produce cross-repo
	//   links between the consuming services (orders, analytics, etc.) instead
	//   of to the library itself.
	//
	//  Guard B — multi-origin collision: if the same ext:* ID appears in
	//   MORE THAN ONE distinct repo, its entRepo attribution is arbitrary
	//   (first-write-wins) and the resulting directional link (e.g.
	//   orders::create_order → analytics::ext:opentelemetry) is misleading:
	//   it looks like orders imports from analytics, when both repos simply
	//   share the same external monitoring lib. Remove any such ext:* ID from
	//   entRepo. This supersedes the original #566 "two repos share
	//   ext:axios" behaviour: #566 links were informative but created false
	//   service-level import edges that confuse the topology view. Issue
	//   #1507 establishes that cross-repo import edges must represent actual
	//   code-level dependencies, not shared external-package usage.

	// Build the normalised repo-slug set for Guard A.
	repoSlugs := make(map[string]struct{}, len(graphs))
	for _, g := range graphs {
		repoSlugs[normalizeSlug(g.Repo)] = struct{}{}
	}

	// extRepos[id] tracks the distinct repos that contain each ext:* entity.
	extRepos := map[string]map[string]struct{}{}

	for _, g := range graphs {
		for _, e := range g.Entities {
			// First write wins on repo: structural IDs are stable per
			// (repo, kind, name, file) so collision across repos is
			// already disambiguated by the per-repo seed. The exception
			// is ext:* IDs — handled by the guards below.
			if _, ok := entRepo[e.ID]; !ok {
				entRepo[e.ID] = g.Repo
			}
			if strings.HasPrefix(e.ID, "ext:") {
				if extRepos[e.ID] == nil {
					extRepos[e.ID] = map[string]struct{}{}
				}
				extRepos[e.ID][g.Repo] = struct{}{}
			}
			st, ok := subtypeByRepo[g.Repo]
			if !ok {
				st = map[string]string{}
				subtypeByRepo[g.Repo] = st
			}
			if existing := st[e.ID]; existing == "" && e.Subtype != "" {
				st[e.ID] = e.Subtype
			}
		}
	}

	// Apply Guard A and Guard B: remove ext:* IDs whose entRepo attribution
	// would produce spurious cross-repo links.
	for id, repos := range extRepos {
		// Guard A: ext:* name matches a repo slug in this group.
		baseName := id[len("ext:"):]
		// Strip a qualifier suffix (ext:module:name → base is "module").
		if colon := strings.IndexByte(baseName, ':'); colon > 0 {
			baseName = baseName[:colon]
		}
		if _, isRepo := repoSlugs[normalizeSlug(baseName)]; isRepo {
			delete(entRepo, id)
			continue
		}
		// Guard B: same ext:* ID in more than one repo — arbitrary attribution
		// and misleading directional link (issue #1507).
		if len(repos) > 1 {
			delete(entRepo, id)
		}
	}

	now := discoveredAt()
	var fresh []Link
	emitted := map[string]bool{} // dedupe by link id
	for _, g := range graphs {
		// Subtype lookup must be against the originating repo (g.Repo)
		// for both endpoints: an ext:* id's classification (package vs
		// bare-name built-in) is repo-local and language-dependent.
		localSubtypes := subtypeByRepo[g.Repo]
		for _, edge := range g.Edges {
			rel := normalizedRelation(edge.Kind)
			if rel != RelationImports && rel != RelationCalls {
				continue
			}
			fromRepo := entRepo[edge.FromID]
			toRepo := entRepo[edge.ToID]
			if fromRepo == "" || toRepo == "" {
				continue
			}
			if fromRepo == toRepo {
				// Self-pair: not a cross-repo edge.
				continue
			}
			// Issue #509 / #566: skip "ext:" placeholders whose
			// originating-repo subtype is NOT "package" (e.g. JS
			// `ext:log` subtype=function — a bare console.log call).
			// Real packages (`ext:axios` subtype=package) pass through.
			if isBuiltinExt(edge.FromID, localSubtypes) || isBuiltinExt(edge.ToID, localSubtypes) {
				continue
			}
			source := entityKey(fromRepo, edge.FromID)
			target := entityKey(toRepo, edge.ToID)
			id := MakeID(source, target, MethodImport)
			if emitted[id] {
				continue
			}
			emitted[id] = true
			fresh = append(fresh, Link{
				ID:           id,
				Source:       source,
				Target:       target,
				Relation:     rel,
				Method:       MethodImport,
				Confidence:   ScoreImport(),
				Channel:      nil,
				Identifier:   nil,
				DiscoveredAt: now,
			})
		}
	}

	// #534 Phase 2 — cross-repo http_endpoint matching by Name. The
	// synthetic emission gives every endpoint a deterministic
	// `http:<METHOD>:<path>` Name; if two repos emit the same Name we
	// know they reference the same logical HTTP route.
	//
	// Match by Name (the canonical http:VERB:PATH string), not by
	// stamped ID — EntityID hashes in the repo tag and source file, so
	// the on-disk IDs for the same endpoint in two repos are distinct.
	//
	// Index: name → repo → stampedID. First-occurrence wins per repo
	// because the per-file synth pass already deduped by canonical ID
	// and the buildDocument step deduped by (kind, name, sourceFile);
	// the only remaining source of multiplicity here is two source
	// files in the SAME repo emitting the same route, in which case
	// either entity-id works as the cross-repo endpoint.
	type httpEntry struct {
		stampedID string
	}
	httpByName := map[string]map[string]httpEntry{}
	for _, g := range graphs {
		for _, e := range g.Entities {
			if !isHTTPEndpointLink(e.Kind) {
				continue
			}
			if e.Name == "" {
				continue
			}
			byRepo, ok := httpByName[e.Name]
			if !ok {
				byRepo = map[string]httpEntry{}
				httpByName[e.Name] = byRepo
			}
			if _, exists := byRepo[g.Repo]; !exists {
				byRepo[g.Repo] = httpEntry{stampedID: e.ID}
			}
		}
	}
	// Sort names for deterministic emission order.
	httpNames := make([]string, 0, len(httpByName))
	for n := range httpByName {
		httpNames = append(httpNames, n)
	}
	sort.Strings(httpNames)
	for _, name := range httpNames {
		byRepo := httpByName[name]
		if len(byRepo) < 2 {
			continue
		}
		repos := make([]string, 0, len(byRepo))
		for r := range byRepo {
			repos = append(repos, r)
		}
		sort.Strings(repos)
		for i := 0; i < len(repos); i++ {
			for j := i + 1; j < len(repos); j++ {
				ra, rb := repos[i], repos[j]
				source := entityKey(ra, byRepo[ra].stampedID)
				target := entityKey(rb, byRepo[rb].stampedID)
				id := MakeID(source, target, MethodImport)
				if emitted[id] {
					continue
				}
				emitted[id] = true
				fresh = append(fresh, Link{
					ID:           id,
					Source:       source,
					Target:       target,
					Relation:     RelationImports,
					Method:       MethodImport,
					Confidence:   ScoreImport(),
					Channel:      nil,
					Identifier:   nil,
					DiscoveredAt: now,
				})
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodImport), fresh, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Skipped = skipped
	return res, nil
}

// normalizeSlug folds a repo slug or ext:* base name to a canonical form
// for comparison. Lowercases and replaces hyphens with underscores so that
// "py-shared" and "py_shared" are treated as the same slug.
// Used by runImportPass Guard A (issue #1507).
func normalizeSlug(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

// normalizedRelation maps a graph relationship Kind to one of the
// canonical relation values used in links.json. Accepts upper- or
// lowercase forms (extractors emit either).
func normalizedRelation(kind string) string {
	switch kind {
	case "imports", "IMPORTS", "import", "IMPORT":
		return RelationImports
	case "calls", "CALLS", "call", "CALL":
		return RelationCalls
	}
	return ""
}
