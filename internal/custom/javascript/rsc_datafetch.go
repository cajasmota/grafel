package javascript

import (
	"regexp"
	"strings"

	"github.com/cajasmota/grafel/internal/types"
)

// rsc_datafetch.go — React Server Component data-fetch edges (#5488, epic #5479).
//
// An App-Router React Server Component (an async `page.tsx`/`layout.tsx` or other
// server component with NO `'use client'` directive) loads its data on the server
// during render. It does so by `await`-ing data-access calls: a local/imported
// data function (`getUsers()`, `fetchPost(id)`, a `*.server.ts` model fn) or a
// direct `await fetch(url)`. Those awaited data-access sites are the server-side
// data-flow of the route — but they were not surfaced as edges from the component.
//
// This pass lifts that flow to a first-class edge from the server-component entity
// to the data source it awaits:
//
//   - `await getUsers()` / `await db.user.findMany()` → CALLS edge component→callee,
//     tagged `rsc_data_fetch=true` so the RSC→data flow is queryable. The resolver
//     binds the callee name to the real function/model entity (esp. `*.server.ts`).
//   - `await fetch(url)`                              → a data-fetch site
//     (SCOPE.Operation subtype="data_fetch") + a READS_FROM edge, tagged
//     `rsc_data_fetch=true`.
//
// Gating: only runs for a *server* component — an App-Router `page`/`layout`/server
// component file with no module-level `'use client'`. Client components (which use
// the same call syntax inside event handlers / effects) are NOT tagged, so client
// event handlers are never mislabelled as server data-fetches.

var (
	// reRSCAwaitCall matches an awaited call to a bare identifier or a member
	// chain — `await getUsers(`, `await db.user.findMany(`, `await api.posts.list(`.
	// Group 1 is the full callee expression (identifier or dotted member ref). The
	// `fetch` builtin is handled separately (reRSCAwaitFetch) and filtered out here.
	reRSCAwaitCall = regexp.MustCompile(`\bawait\s+([A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*)\s*\(`)

	// reRSCAwaitFetch matches a direct `await fetch('url'|"url"|`url`|expr)` — the
	// Web fetch data load. Group 1 (when present) is the literal URL.
	reRSCAwaitFetch = regexp.MustCompile(`\bawait\s+fetch\s*\(\s*(?:['"` + "`" + `]([^'"` + "`" + `]*)['"` + "`" + `])?`)
)

// rscDataFetchOwner emits the server-side data-fetch edges for a server-component
// owner. owner is the EntityRecord representing the (implicit) Server Component;
// src is the full module source. It returns the data-fetch site entities to add
// plus the edges to hang off the owner. callers gate this on the file being a
// server component (no `'use client'`, App-Router page/layout/server component).
//
// Both CALLS (function/model fetch) and READS_FROM (`await fetch`) edges carry
// `rsc_data_fetch=true` so a consumer can ask "what does this server component
// load during render".
func rscDataFetchEdges(owner *types.EntityRecord, src, filePath, language string) (ents []types.EntityRecord, rels []types.RelationshipRecord) {
	seenCall := map[string]bool{}

	// Direct `await fetch(url)` → data_fetch site + READS_FROM edge.
	seenFetch := map[string]bool{}
	for _, m := range reRSCAwaitFetch.FindAllStringSubmatchIndex(src, -1) {
		url := ""
		if m[2] >= 0 {
			url = src[m[2]:m[3]]
		}
		key := "fetch|" + url
		if seenFetch[key] {
			continue
		}
		seenFetch[key] = true
		name := "fetch"
		if url != "" {
			name = "fetch " + url
		}
		site := makeEntity(name, "SCOPE.Operation", "data_fetch", filePath, language, lineOf(src, m[0]))
		setProps(&site, "framework", "nextjs", "fetch_kind", "web_fetch",
			"url", url, "rsc_data_fetch", "true", "rendering", "server",
			"provenance", "INFERRED_FROM_RSC_FETCH")
		ents = append(ents, site)
		rels = append(rels, types.RelationshipRecord{
			FromID: owner.Name,
			ToID:   site.ID,
			Kind:   string(types.RelationshipKindReadsFrom),
			Properties: map[string]string{
				"framework":      "nextjs",
				"rsc_data_fetch": "true",
				"fetch_kind":     "web_fetch",
				"url":            url,
				"provenance":     "INFERRED_FROM_RSC_FETCH",
			},
		})
	}

	// Awaited data-access calls → CALLS edge component→callee.
	for _, m := range reRSCAwaitCall.FindAllStringSubmatchIndex(src, -1) {
		callee := src[m[2]:m[3]]
		// `await fetch(...)` is modelled above as a READS_FROM data-fetch site.
		if callee == "fetch" {
			continue
		}
		if seenCall[callee] {
			continue
		}
		seenCall[callee] = true
		// The resolver binds the callee name to the real function / model entity
		// (a local helper, an imported `getX`/`fetchX`, or a `*.server.ts` model
		// fn). Use the leaf name as the bind target; keep the full chain for context.
		leaf := callee
		if i := strings.LastIndexByte(callee, '.'); i >= 0 {
			leaf = callee[i+1:]
		}
		rels = append(rels, types.RelationshipRecord{
			FromID: owner.Name,
			ToID:   leaf,
			Kind:   string(types.RelationshipKindCalls),
			Properties: map[string]string{
				"framework":      "nextjs",
				"rsc_data_fetch": "true",
				"callee":         callee,
				"rendering":      "server",
				"provenance":     "INFERRED_FROM_RSC_DATA_FETCH",
			},
		})
	}
	return ents, rels
}
