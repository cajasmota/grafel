package javascript

import (
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// metafw_server.go — shared server / hydration / data-loader / static-generation
// recognition for the React-based meta-frameworks (Next.js, Remix, Gatsby) so
// they all classify the four meta-framework "Server / Data Flow / Build" cells
// with the same fidelity (issue #2858).
//
// The capabilities modelled here (per tools/coverage/capability-dictionary.yaml,
// meta_framework groups) are:
//
//   - server_components      — server-vs-client component split. The canonical
//                              markers are the RSC directives `'use server'` /
//                              `'use client'`, the App-Router server default,
//                              and `*.server.{ts,tsx,js,jsx}` server-only modules.
//   - hydration_boundaries   — `'use client'` directives and framework islands;
//                              the boundary at which server-rendered markup
//                              becomes interactive on the client.
//   - data_loaders           — framework data-loading functions
//                              (getServerSideProps / getStaticProps /
//                              getStaticPaths / generateStaticParams / Remix
//                              loader / SvelteKit load / Nuxt useAsyncData …).
//   - static_generation      — SSG / prerender markers
//                              (getStaticProps / getStaticPaths /
//                              generateStaticParams / `export const prerender`
//                              / `export const dynamic = 'force-static'`).
//
// All emitters feed the per-extractor dedup-aware adder so the host framework
// tag + provenance is attached. No new entity/edge Kinds are introduced — these
// are SCOPE.Operation / SCOPE.Pattern entities the resolver already understands.

var (
	// reUseClientDirective matches the React Server Components `'use client'`
	// directive (single/double-quoted) at any position. Its mere presence makes
	// the module a Client Component / hydration boundary.
	reUseClientDirective = regexp.MustCompile(`['"]use client['"]`)

	// reUseServerDirective matches the `'use server'` directive marking a module
	// or function body as a Server Action / server-only boundary.
	reUseServerDirective = regexp.MustCompile(`['"]use server['"]`)
)

// emitRSCBoundary inspects src for the `'use client'` / `'use server'`
// directives and emits the matching server_components + hydration_boundaries
// markers. framework tags the host (nextjs/remix/gatsby). It returns whether a
// `'use client'` directive was found so the caller can decide the default
// (directive-free App-Router modules are Server Components by default).
//
// Emitted entities:
//
//	'use client'  → SCOPE.Pattern subtype="client_boundary"  (hydration_boundaries)
//	'use server'  → SCOPE.Pattern subtype="server_boundary"  (server_components)
func emitRSCBoundary(src, filePath, language, framework string, add reactComponentSink) (hasUseClient bool) {
	if m := reUseClientDirective.FindStringIndex(src); m != nil {
		ent := makeEntity("use client", "SCOPE.Pattern", "client_boundary", filePath, language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "directive", "use client",
			"hydration", "client", "provenance", "INFERRED_FROM_RSC_USE_CLIENT")
		add(ent)
		hasUseClient = true
	}
	if m := reUseServerDirective.FindStringIndex(src); m != nil {
		ent := makeEntity("use server", "SCOPE.Pattern", "server_boundary", filePath, language, lineOf(src, m[0]))
		setProps(&ent, "framework", framework, "directive", "use server",
			"rendering", "server", "provenance", "INFERRED_FROM_RSC_USE_SERVER")
		add(ent)
	}
	return hasUseClient
}

// emitServerOnlyModule emits a server_components marker for a `*.server.*`
// module — the file-name convention several meta-frameworks use to force a
// module to be server-only (never bundled to the client). isServerModule is the
// caller's path-based predicate; name is a stable identifier for the marker.
func emitServerOnlyModule(name, filePath, language, framework string, add reactComponentSink) {
	ent := makeEntity(name, "SCOPE.Pattern", "server_boundary", filePath, language, 1)
	setProps(&ent, "framework", framework, "module_scope", "server",
		"rendering", "server", "provenance", "INFERRED_FROM_SERVER_MODULE_SUFFIX")
	add(ent)
}

// metafwServerComponentEntity builds the implicit-Server-Component marker for an
// App-Router-style module that has NO `'use client'` directive (issue #2858,
// server_components). The RSC model defaults every component to a Server
// Component unless it opts into the client with `'use client'`.
func metafwServerComponentEntity(name, filePath, language, framework string) types.EntityRecord {
	ent := makeEntity(name, "SCOPE.Pattern", "server_component", filePath, language, 1)
	setProps(&ent, "framework", framework, "rendering", "server",
		"component_kind", "server", "provenance", "INFERRED_FROM_RSC_DEFAULT")
	return ent
}
