/* ============================================================
   graph-route-truncation-wiring.test.tsx — #7146 item 2.

   The banner component itself is pinned by graph-truncation-banner.test.tsx,
   which renders <GraphTruncationBanner> DIRECTLY. That leaves the hop that
   actually matters unpinned: routes/graph.tsx deriving `data` and handing it
   to the banner. Re-scoring #7129's mutants showed
   `<GraphTruncationBanner data={undefined} />` at that call site ALIVE with
   334/334 green — extracting the component made it testable and MOVED the
   untested hop instead of closing it.

   So this test renders the ROUTE (GraphScreen), not the banner.

   Mock boundary: only the two data-source hooks the route consumes,
   `@/hooks/use-graph-stream` and `useGraph` from `@/hooks/use-graph` (the
   latter via importOriginal so the module's other exports stay real). Both
   are strictly BELOW the hop under test. The route's own
   `data = streamFailed ? fallback.data : stream.state.payload` derivation,
   the `<GraphTruncationBanner data={data} />` prop pass-through, and the real
   banner component are all unmocked production code.

   The fixtures are plain `GraphPayload` values, NOT `normalizeGraphPayload`
   output. That is deliberate: under SSR no fetch runs, so the route never
   reaches `normalizeGraphPayload` (it lives in `useGraph`'s queryFn) and
   production stream payloads are built by `lib/graph-stream-reducer.ts`.
   Calling the normalizer here would only duplicate the banner test's
   coverage while reading like evidence about the route.

   Assertions are scoped to the banner's own subtree via its
   `data-testid="graph-truncation-banner"` anchor. A whole-document
   `toContain` is not enough: "0 / 12 nodes" also appears in the route's
   "Building graph… 0 / 12 nodes" progress affordance, so that sub-assertion
   would survive deleting the banner it is meant to check.

   The absence case carries a POSITIVE CONTROL. `not.toContain` has several
   independent ways to be a no-op — the route rendered nothing, it threw into
   an error boundary, the payload never arrived — and cannot tell them apart.
   A reviewer demonstrated this by making the route `return null` on a
   complete graph, which left an absence-only assertion green. So the absence
   case also asserts that the route rendered AND that the banner's own slot
   rendered.

   Location: `src/lib/`, not next to the route. `package.json`'s test script
   is literally `vitest run src/lib`, so a test placed anywhere else is never
   run by `npm test` — which is also why the banner's own test sits in
   `src/lib/` for a component under `src/components/graph/`. The runner's
   scope is the convention here, not directory mirroring.

   Axes VARIED across the four cases:
     - `nodeTruncated`      — true / false / false / true
     - `edgeTruncated`      — false / false / TRUE / false
     - `limits`             — present / absent / absent / present
     - delivery path        — stream (`phase: "done"`) for cases 1-3,
                              FALLBACK (`phase: "error"` → `fallback.data`)
                              for case 4
   The node/edge truncation flags are varied INDEPENDENTLY (case 3 has edges
   truncated and nodes NOT) so the two arms of the banner's guard are scored
   separately; two guards that only ever fire together grade neither.

   Axes held CONSTANT: `nodes: []`, `edges: []`, `communities: []`,
   `repos: []`, `totalNodeCount: 12`, `totalEdgeCount: 34`, the group id
   (`demo`, the route default), the route props (none), the router entry, and
   the query-client config. So the difference between "banner shows" and
   "banner absent" is the truncation metadata alone, not a different render
   path or data source.
   ============================================================ */
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { GraphPayload } from "@/data/types";
import GraphScreen from "@/routes/graph";

/** What the mocked hooks hand the route. Set by renderGraphRoute. */
const injected = {
  streamPhase: "done" as "done" | "error",
  streamPayload: undefined as GraphPayload | undefined,
  fallbackPayload: undefined as GraphPayload | undefined,
};

vi.mock("@/hooks/use-graph-stream", () => ({
  useGraphStream: () => ({
    state: {
      payload: injected.streamPayload,
      hasMeta: true,
      totalNodes: injected.streamPayload?.totalNodeCount ?? 0,
    },
    phase: injected.streamPhase,
    loadedNodes: injected.streamPayload?.nodes.length ?? 0,
    totalNodes: injected.streamPayload?.totalNodeCount ?? 0,
    error: null,
    errorDetail: null,
  }),
}));

vi.mock("@/hooks/use-graph", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/use-graph")>()),
  useGraph: () => ({
    data: injected.fallbackPayload,
    isLoading: false,
    isError: false,
  }),
}));

function graphPayload(overrides: Partial<GraphPayload> = {}): GraphPayload {
  return {
    nodes: [],
    edges: [],
    communities: [],
    repos: [],
    totalNodeCount: 12,
    totalEdgeCount: 34,
    nodeTruncated: false,
    edgeTruncated: false,
    ...overrides,
  };
}

/**
 * The slot the route renders the banner INTO. Present whenever the route's
 * top-level markup rendered at all, so it is the positive control that tells
 * "banner correctly absent" apart from "route produced nothing".
 */
const BANNER_SLOT = 'class="shrink-0 border-b border-border bg-bg px-4 py-2 space-y-2"';

type Delivery = "stream" | "fallback";

function renderGraphRoute(payload: GraphPayload, via: Delivery = "stream"): string {
  injected.streamPhase = via === "fallback" ? "error" : "done";
  injected.streamPayload = via === "fallback" ? graphPayload() : payload;
  injected.fallbackPayload = via === "fallback" ? payload : undefined;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: Infinity } },
  });
  return renderToStaticMarkup(
    <MemoryRouter initialEntries={["/graph/demo"]}>
      <QueryClientProvider client={client}>
        <GraphScreen />
      </QueryClientProvider>
    </MemoryRouter>,
  );
}

/** The banner's OWN subtree, or null when the banner rendered nothing. */
function bannerSubtree(html: string): string | null {
  const m = /<div data-testid="graph-truncation-banner"[^>]*>([\s\S]*?)<\/div>/.exec(html);
  return m ? m[1] : null;
}

/** Positive control: the route rendered, and the banner's slot with it. */
function expectRouteRendered(html: string): void {
  expect(html.length).toBeGreaterThan(0);
  expect(html).toContain("Module overview");
  expect(html).toContain(BANNER_SLOT);
}

describe("routes/graph.tsx → GraphTruncationBanner wiring", () => {
  beforeEach(() => {
    injected.streamPhase = "done";
    injected.streamPayload = undefined;
    injected.fallbackPayload = undefined;
  });

  it("renders the banner from the stream payload when NODES are truncated", () => {
    const html = renderGraphRoute(graphPayload({
      nodeTruncated: true,
      edgeTruncated: false,
      limits: { nodeCap: 500, edgeCap: 4_000 },
    }));

    expectRouteRendered(html);
    const banner = bannerSubtree(html);
    expect(banner).not.toBeNull();
    expect(banner).toContain("Bounded result:");
    expect(banner).toContain("0 / 12 nodes");
    expect(banner).toContain("0 / 34 edges");
    expect(banner).toContain("Caps: 500 nodes, 4,000 edges.");
  });

  it("renders the banner when EDGES are truncated and nodes are not", () => {
    const html = renderGraphRoute(graphPayload({
      nodeTruncated: false,
      edgeTruncated: true,
    }));

    expectRouteRendered(html);
    const banner = bannerSubtree(html);
    expect(banner).not.toBeNull();
    expect(banner).toContain("Bounded result:");
    expect(banner).toContain("0 / 34 edges");
  });

  it("renders NO banner when the route holds a complete graph", () => {
    const html = renderGraphRoute(graphPayload());

    // Positive control FIRST: without it this subtest passes when the route
    // renders nothing at all (measured ALIVE at 336/336 by review).
    expectRouteRendered(html);
    expect(bannerSubtree(html)).toBeNull();
    expect(html).not.toContain("Bounded result:");
  });

  it("renders the banner when the payload arrives via the FALLBACK fetch", () => {
    const html = renderGraphRoute(graphPayload({
      nodeTruncated: true,
      limits: { nodeCap: 500, edgeCap: 4_000 },
    }), "fallback");

    expectRouteRendered(html);
    const banner = bannerSubtree(html);
    expect(banner).not.toBeNull();
    expect(banner).toContain("Bounded result:");
    expect(banner).toContain("0 / 12 nodes");
    expect(banner).toContain("Caps: 500 nodes, 4,000 edges.");
  });
});
