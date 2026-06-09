/**
 * Tests for flow-to-dag.ts — adapting a process Flow (steps + branches_dag)
 * onto the shared <FlowDag> DownstreamDAGResponse payload (#4354).
 *
 * The key behaviour the old SVG renderer lacked: the persisted branches_dag
 * tree must produce the REAL fan-out edges, not a flattened primary path.
 *
 * Run with: npx vitest run src/lib/flow-to-dag.test.ts
 */
import { describe, it, expect } from "vitest";
import { flowToDagPayload, parseBranchesDag } from "./flow-to-dag";
import type { ChainStep, Process, ProcessStep } from "@/data/types";

function step(i: number, over: Partial<ProcessStep> = {}): ProcessStep {
  return {
    entity_id: `e${i}`,
    name: `step${i}`,
    step_index: i,
    source_file: `f${i}.ts`,
    repo: "api",
    edge_kind: "CALLS",
    ...over,
  };
}

function flow(over: Partial<Process> = {}): Process {
  return {
    process_id: "p1",
    label: "GET /x → handler → repo",
    repo: "api",
    entry_id: "e0",
    entry_name: "handler",
    entry_kind: "http_handler",
    terminal_id: "e2",
    step_count: 3,
    cross_stack: false,
    chain_labels: ["a", "b", "c"],
    ...over,
  };
}

describe("flowToDagPayload", () => {
  it("returns null when there are no steps", () => {
    expect(flowToDagPayload(flow(), undefined)).toBeNull();
    expect(flowToDagPayload(flow(), [])).toBeNull();
  });

  it("lays a linear chain out as sequential i → i+1 edges", () => {
    const steps = [step(0), step(1), step(2)];
    const p = flowToDagPayload(flow(), steps)!;
    expect(p.nodes.map((n) => n.id)).toEqual([
      "flow-step-0",
      "flow-step-1",
      "flow-step-2",
    ]);
    expect(p.edges).toEqual([
      { from: "flow-step-0", to: "flow-step-1", kind: "CALLS" },
      { from: "flow-step-1", to: "flow-step-2", kind: "CALLS" },
    ]);
    expect(p.branch_count).toBe(0);
  });

  it("marks the entry node endpoint for an http_handler flow", () => {
    const p = flowToDagPayload(flow(), [step(0), step(1)])!;
    expect(p.nodes[0].role).toBe("endpoint");
  });

  it("renders the real fan-out from branches_dag (the old SVG dropped these)", () => {
    // 0 → {1, 2}; 1 → 3. A fan-out at step 0.
    const tree: ChainStep = {
      step_index: 0,
      entity_id: "e0",
      label: "root",
      branches: [
        {
          step_index: 1,
          entity_id: "e1",
          label: "armA",
          branches: [
            { step_index: 3, entity_id: "e3", label: "leafA", branches: [] },
          ],
        },
        { step_index: 2, entity_id: "e2", label: "armB", branches: [] },
      ],
    };
    const steps = [step(0), step(1), step(2), step(3)];
    const p = flowToDagPayload(
      flow({ is_dag: true, branches_dag: JSON.stringify(tree) }),
      steps,
    )!;
    const pairs = p.edges.map((e) => `${e.from}->${e.to}`).sort();
    expect(pairs).toEqual(
      [
        "flow-step-0->flow-step-1",
        "flow-step-0->flow-step-2",
        "flow-step-1->flow-step-3",
      ].sort(),
    );
    // step 0 fans out → one branch point.
    expect(p.branch_count).toBe(1);
  });

  it("skips fanout_cap overflow sentinels but flags the truncation", () => {
    const tree: ChainStep = {
      step_index: 0,
      entity_id: "e0",
      label: "root",
      branches: [
        { step_index: 1, entity_id: "e1", label: "arm", branches: [] },
        { step_index: 9, entity_id: "", label: "+N more", reason: "fanout_cap", branches: [] },
      ],
    };
    const p = flowToDagPayload(
      flow({ is_dag: true, branches_dag: JSON.stringify(tree) }),
      [step(0), step(1)],
    )!;
    // the sentinel (step 9, absent from steps) produced no edge.
    expect(p.edges).toEqual([{ from: "flow-step-0", to: "flow-step-1", kind: "CALLS" }]);
    expect(p.truncation.fanout_truncated).toBe(true);
  });

  it("falls back to the linear chain when branches_dag references missing steps", () => {
    const tree: ChainStep = {
      step_index: 0,
      entity_id: "e0",
      label: "root",
      branches: [{ step_index: 5, entity_id: "e5", label: "missing", branches: [] }],
    };
    const p = flowToDagPayload(
      flow({ is_dag: true, branches_dag: JSON.stringify(tree) }),
      [step(0), step(1)],
    )!;
    expect(p.edges).toEqual([{ from: "flow-step-0", to: "flow-step-1", kind: "CALLS" }]);
  });

  it("maps data-store edge kinds to JOINS_COLLECTION", () => {
    const steps = [step(0), step(1, { edge_kind: "QUERIES", step_kind: "db_query" })];
    const p = flowToDagPayload(flow(), steps)!;
    expect(p.edges[0].kind).toBe("JOINS_COLLECTION");
    expect(p.nodes[1].role).toBe("collection");
  });
});

describe("parseBranchesDag", () => {
  it("returns null for absent or malformed input", () => {
    expect(parseBranchesDag(undefined)).toBeNull();
    expect(parseBranchesDag("not json")).toBeNull();
    expect(parseBranchesDag("{}")).toBeNull();
  });

  it("parses a well-formed tree", () => {
    const tree = { step_index: 0, entity_id: "e0", label: "r", branches: [] };
    expect(parseBranchesDag(JSON.stringify(tree))?.step_index).toBe(0);
  });
});
