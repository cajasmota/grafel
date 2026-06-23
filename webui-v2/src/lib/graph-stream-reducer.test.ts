/**
 * Increment 2 of epic #5446 — progressive graph stream reducer tests.
 *
 * Guards the pure accumulation seam the SSE consumer drives:
 *   meta → chunk → chunk → done builds up the SAME normalized GraphPayload the
 *   full-payload fetch produces, with the progress denominator from `meta`.
 */
import { describe, it, expect } from "vitest";
import {
  initialStreamState,
  applyMeta,
  applyChunk,
  applyDone,
  type GraphStreamMetaWire,
  type GraphStreamChunkWire,
} from "./graph-stream-reducer";

const meta: GraphStreamMetaWire = {
  total_nodes: 3,
  total_edges: 2,
  communities: [{ id: 1, label: "core", repo: "r", size: 3, color_index: 0 }],
  repos: [{ id: "r", language: "go", color_index: 0 }],
};

const chunk1: GraphStreamChunkWire = {
  nodes: [
    { id: "a", label: "A", kind: "func", repo: "r", degree: 2, pagerank: 0.9 },
    { id: "b", label: "B", kind: "func", repo: "r", degree: 1, pagerank: 0.5, community_id: 1 },
  ],
  edges: [{ source: "a", target: "b", kind: "CALLS" }],
};

const chunk2: GraphStreamChunkWire = {
  nodes: [{ id: "c", label: "", kind: "type", repo: "r", degree: 1, pagerank: 0.1, source_file: "x/y.go" }],
  edges: [{ source: "a", target: "c", kind: "REFERENCES" }],
};

describe("graph-stream-reducer", () => {
  it("initial state is an empty, renderable payload", () => {
    const s = initialStreamState();
    expect(s.payload.nodes).toEqual([]);
    expect(s.payload.edges).toEqual([]);
    expect(s.hasMeta).toBe(false);
    expect(s.done).toBe(false);
    expect(s.totalNodes).toBe(0);
  });

  it("applyMeta seeds totals + legend metadata and the totalNodeCount denominator", () => {
    const s = applyMeta(initialStreamState(), meta);
    expect(s.hasMeta).toBe(true);
    expect(s.totalNodes).toBe(3);
    expect(s.totalEdges).toBe(2);
    expect(s.payload.totalNodeCount).toBe(3);
    expect(s.payload.communities).toEqual([
      { id: 1, label: "core", repo: "r", size: 3, colorIndex: 0 },
    ]);
    expect(s.payload.repos).toEqual([{ id: "r", language: "go", colorIndex: 0 }]);
    // No nodes yet — meta carries only totals + legend.
    expect(s.payload.nodes).toEqual([]);
  });

  it("applyChunk appends and normalizes nodes/edges to the growing arrays", () => {
    let s = applyMeta(initialStreamState(), meta);
    s = applyChunk(s, chunk1);
    expect(s.payload.nodes).toHaveLength(2);
    expect(s.payload.edges).toHaveLength(1);
    // snake_case → camelCase normalization (pagerank → pageRank, community_id → communityId).
    expect(s.payload.nodes[0]).toMatchObject({ id: "a", label: "A", pageRank: 0.9, communityId: null });
    expect(s.payload.nodes[1]).toMatchObject({ communityId: 1 });

    s = applyChunk(s, chunk2);
    expect(s.payload.nodes).toHaveLength(3);
    expect(s.payload.edges).toHaveLength(2);
    // label falls back to id when blank; source_file → sourceFile.
    expect(s.payload.nodes[2]).toMatchObject({ id: "c", label: "c", sourceFile: "x/y.go" });
    // Accumulation reaches the meta totals.
    expect(s.payload.nodes.length).toBe(s.totalNodes);
    expect(s.payload.edges.length).toBe(s.totalEdges);
  });

  it("applyChunk returns a new payload reference each time (immutable update)", () => {
    let s = applyMeta(initialStreamState(), meta);
    const before = s.payload;
    s = applyChunk(s, chunk1);
    expect(s.payload).not.toBe(before);
    expect(s.payload.nodes).not.toBe(before.nodes);
  });

  it("applyChunk tolerates empty/edgeless chunks without mutating arrays", () => {
    let s = applyMeta(initialStreamState(), meta);
    s = applyChunk(s, chunk1);
    const nodesRef = s.payload.nodes;
    s = applyChunk(s, { nodes: [], edges: [] });
    // Nothing appended → same array reference is reused.
    expect(s.payload.nodes).toBe(nodesRef);
    expect(s.payload.nodes).toHaveLength(2);
  });

  it("applyDone marks the stream finished", () => {
    let s = applyMeta(initialStreamState(), meta);
    s = applyChunk(s, chunk1);
    expect(s.done).toBe(false);
    s = applyDone(s);
    expect(s.done).toBe(true);
    // done preserves the accumulated payload.
    expect(s.payload.nodes).toHaveLength(2);
  });
});
