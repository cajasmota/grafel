/* ============================================================
   graph-route-truncation-wiring.test.tsx — #7146 item 2.

   The banner component itself is pinned by graph-truncation-banner.test.tsx,
   which renders <GraphTruncationBanner> DIRECTLY. That leaves the hop that
   actually matters unpinned: routes/graph.tsx deriving `data` from the graph
   stream and handing it to the banner. Re-scoring #7129's mutants showed
   `<GraphTruncationBanner data={undefined} />` at that call site ALIVE with
   334/334 green — extracting the component made it testable and MOVED the
   untested hop instead of closing it.

   So this test renders the ROUTE (GraphScreen), not the banner.

   Mock boundary: only `@/hooks/use-graph-stream` is replaced, i.e. strictly
   BELOW the hop under test. The route's own `data = streamFailed ?
   fallback.data : stream.state.payload` derivation and the
   `<GraphTruncationBanner data={data} />` prop pass-through are the real
   production code, and the banner is the real component. Nothing between the
   injected payload and the rendered markup is stubbed.

   Location: `src/lib/`, not next to the route. `package.json`'s test script is
   literally `vitest run src/lib`, so a test placed anywhere else is never run
   by `npm test` — which is also why the banner's own test sits in `src/lib/`
   for a component under `src/components/graph/`. The runner's scope is the
   convention here, not directory mirroring.

   Axes VARIED between the two cases: the truncation flags on the wire payload
   (node_truncated true/absent) and the presence of `limits`.
   Axes held CONSTANT: the payload reaches the route through the SAME injected
   stream hook, the same empty nodes/edges arrays, the same total counts
   (12 / 34), the same route props (none), the same group id, and the same
   `phase: "done"` stream phase. So the only thing that differs between "banner
   shows" and "banner absent" is the truncation metadata itself — the negative
   case is not an artefact of a different render path.
   ============================================================ */
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { GraphPayload, GraphPayloadWire } from "@/data/types";
import { normalizeGraphPayload } from "@/hooks/use-graph";
import GraphScreen from "@/routes/graph";

const streamPayload = { current: undefined as GraphPayload | undefined };

vi.mock("@/hooks/use-graph-stream", () => ({
  useGraphStream: () => ({
    state: {
      payload: streamPayload.current,
      hasMeta: true,
      totalNodes: streamPayload.current?.totalNodeCount ?? 0,
    },
    phase: "done",
    loadedNodes: streamPayload.current?.nodes.length ?? 0,
    totalNodes: streamPayload.current?.totalNodeCount ?? 0,
    error: null,
    errorDetail: null,
  }),
}));

function graphPayloadWire(overrides: Partial<GraphPayloadWire> = {}): GraphPayloadWire {
  return {
    nodes: [],
    edges: [],
    communities: [],
    repos: [],
    total_node_count: 12,
    total_edge_count: 34,
    ...overrides,
  };
}

function renderGraphRoute(payload: GraphPayload): string {
  streamPayload.current = payload;
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

describe("routes/graph.tsx → GraphTruncationBanner wiring", () => {
  beforeEach(() => {
    streamPayload.current = undefined;
  });

  it("renders the truncation banner from the payload the route holds", () => {
    const html = renderGraphRoute(normalizeGraphPayload(graphPayloadWire({
      node_truncated: true,
      edge_truncated: false,
      limits: { node_cap: 500, edge_cap: 4_000 },
    })));

    expect(html).toContain("Bounded result:");
    expect(html).toContain("0 / 12 nodes");
    expect(html).toContain("0 / 34 edges");
    expect(html).toContain("Caps: 500 nodes, 4,000 edges.");
  });

  it("renders no banner when the route holds a complete graph", () => {
    const html = renderGraphRoute(normalizeGraphPayload(graphPayloadWire()));

    expect(html).not.toContain("Bounded result:");
  });
});
