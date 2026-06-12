/* unify.test.ts — Model 3 (#4810) unified infra+code classification + the real
   code↔infra cross-boundary edge detection that powers the single interleaved
   architecture diagram. Pure data, no React. */

import { describe, it, expect } from "vitest";
import {
  classifyNode,
  classifyNodes,
  isCrossBoundary,
  unifiedStats,
} from "./unify";
import type { CompoundNode, CompoundEdge } from "@/data/types";

const node = (
  id: string,
  kind: string,
  tier: CompoundNode["tier"],
): CompoundNode => ({ id, label: id, kind, tier, repo: "api", zone_path: [] });

describe("classifyNode", () => {
  it("classifies IaC resource kinds as infra (provider-agnostic, substring)", () => {
    expect(classifyNode({ kind: "Datastore", tier: "data" })).toBe("infra");
    expect(classifyNode({ kind: "dynamodb_table", tier: "data" })).toBe("infra");
    expect(classifyNode({ kind: "sqs_queue", tier: "messaging" })).toBe("infra");
    expect(classifyNode({ kind: "s3_bucket", tier: "compute" })).toBe("infra");
    expect(classifyNode({ kind: "LambdaFunction", tier: "compute" })).toBe("infra");
    expect(classifyNode({ kind: "VPC", tier: "edge" })).toBe("infra");
  });

  it("classifies code entity kinds as code", () => {
    expect(classifyNode({ kind: "Service", tier: "compute" })).toBe("code");
    expect(classifyNode({ kind: "HTTPEndpoint", tier: "edge" })).toBe("code");
    expect(classifyNode({ kind: "Module", tier: "compute" })).toBe("code");
    expect(classifyNode({ kind: "Controller", tier: "edge" })).toBe("code");
  });

  it("falls back to tier when the kind is unrecognised", () => {
    // data / messaging lanes are infra by tier even with a generic kind.
    expect(classifyNode({ kind: "Thing", tier: "data" })).toBe("infra");
    expect(classifyNode({ kind: "Thing", tier: "messaging" })).toBe("infra");
    // everything else defaults to code.
    expect(classifyNode({ kind: "Thing", tier: "compute" })).toBe("code");
    expect(classifyNode({ kind: "", tier: "client" })).toBe("code");
  });
});

describe("classifyNodes", () => {
  it("maps every node id to its class", () => {
    const m = classifyNodes([
      node("db", "Datastore", "data"),
      node("svc", "Service", "compute"),
    ]);
    expect(m.get("db")).toBe("infra");
    expect(m.get("svc")).toBe("code");
    expect(m.size).toBe(2);
  });

  it("tolerates undefined", () => {
    expect(classifyNodes(undefined).size).toBe(0);
  });
});

describe("isCrossBoundary", () => {
  const classes = classifyNodes([
    node("db", "Datastore", "data"),
    node("q", "Queue", "messaging"),
    node("svc", "Service", "compute"),
  ]);

  it("is true for a code→infra edge (real usage link)", () => {
    expect(isCrossBoundary({ source: "svc", target: "db" }, classes)).toBe(true);
    expect(isCrossBoundary({ source: "q", target: "svc" }, classes)).toBe(true);
  });

  it("is false for an intra-layer edge", () => {
    expect(isCrossBoundary({ source: "db", target: "q" }, classes)).toBe(false);
  });

  it("is false when an endpoint is not on the canvas (e.g. a collapsed-zone id)", () => {
    expect(isCrossBoundary({ source: "svc", target: "zone:x" }, classes)).toBe(false);
  });
});

describe("unifiedStats", () => {
  const nodes = [
    node("db", "Datastore", "data"),
    node("q", "Queue", "messaging"),
    node("svc", "Service", "compute"),
    node("ep", "HTTPEndpoint", "edge"),
  ];
  const edge = (s: string, t: string): CompoundEdge => ({
    source: s,
    target: t,
    type: "reads",
    label: "reads",
    agg_key: `reads ${s} ${t}`,
  });

  it("counts infra/code nodes and real cross-boundary edges", () => {
    const stats = unifiedStats(nodes, [
      edge("svc", "db"), // cross
      edge("svc", "q"), // cross
      edge("ep", "svc"), // intra (code→code)
      edge("db", "q"), // intra (infra→infra)
    ]);
    expect(stats.infraNodes).toBe(2);
    expect(stats.codeNodes).toBe(2);
    expect(stats.crossEdges).toBe(2);
    expect(stats.totalEdges).toBe(4);
  });

  it("reports zero cross edges when the graph has no code↔infra usage links (#4983)", () => {
    const stats = unifiedStats(nodes, [edge("ep", "svc")]);
    expect(stats.crossEdges).toBe(0);
  });
});
