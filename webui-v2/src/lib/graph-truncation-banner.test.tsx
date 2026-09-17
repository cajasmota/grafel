import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { GraphTruncationBanner } from "@/components/graph/graph-truncation-banner";
import { normalizeGraphPayload } from "@/hooks/use-graph";
import type { GraphPayloadWire } from "@/data/types";

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

describe("GraphTruncationBanner", () => {
  it("renders backend truncation metadata after hook normalization", () => {
    const data = normalizeGraphPayload(graphPayloadWire({
      node_truncated: true,
      edge_truncated: false,
      limits: { node_cap: 500, edge_cap: 4_000 },
    }));

    const html = renderToStaticMarkup(<GraphTruncationBanner data={data} />);

    expect(data.nodeTruncated).toBe(true);
    expect(data.edgeTruncated).toBe(false);
    expect(html).toContain("Bounded result:");
    expect(html).toContain("0 / 12 nodes");
    expect(html).toContain("0 / 34 edges");
    expect(html).toContain("Caps: 500 nodes, 4,000 edges.");
  });

  it("renders nothing when the backend reports a complete graph", () => {
    const data = normalizeGraphPayload(graphPayloadWire());
    expect(renderToStaticMarkup(<GraphTruncationBanner data={data} />)).toBe("");
  });
});
