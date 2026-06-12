/* ============================================================
   components/compound-topology/unify.ts

   Model 3 of the compound-topology epic (#4810) — the "unified infra+code
   architecture diagram". ONE compound canvas that interleaves infra resources
   AND code modules/services together (vs Model 2's two side-by-side lenses).

   The unification is REAL, not synthesised:

     - The infra lens payload (group_by=infra) already places EVERY entity —
       both IaC resources and the code that uses them — inside ONE infra
       containment hierarchy (cloud → network → service, with code nested where
       it belongs). So the interleaving already exists in the graph; we render
       it as a single canvas.

     - On top of that we classify each node as `infra` (an IaC resource:
       datastore/queue/function/network/compute…) vs `code` (a module, service,
       endpoint, handler…). The class is derived from the node's kind first,
       then its tier as a fallback. This is what lets us VISUALLY distinguish
       the two layers (icon/colour/shape) so the single picture stays legible —
       the TEAM-reference architecture-diagram style.

     - We then mark the typed usage edges that CROSS the code↔infra boundary
       (e.g. "service X writes to queue Y", "module A reads table B"). These are
       the real cross-links — the same shared-identity + typed-usage edges
       Model 2 surfaced — and in the unified diagram we draw them with emphasis
       so you SEE the code-to-infra wiring at a glance.

   This module is pure (no React / DOM) so the classification + edge-marking is
   unit-testable.
   ============================================================ */

import type {
  CompoundNode,
  CompoundEdge,
  CompoundTier,
} from "@/data/types";

/**
 * A node in the unified diagram is one of two classes. `infra` = an IaC /
 * deployed resource; `code` = a code entity (module/service/endpoint). The
 * distinction drives the icon/colour/shape so the single canvas reads as two
 * interleaved layers rather than an undifferentiated hairball.
 */
export type NodeClass = "infra" | "code";

/**
 * Kinds that are unambiguously infra resources. Provider-agnostic (matches the
 * generic names the compound backend emits): datastore/queue/topic/function/
 * bucket/network/cluster/cloud/gateway/load-balancer/cache/secret/… plus the
 * IaC umbrella kinds. Matched case-insensitively as a substring so e.g.
 * "datastore", "dynamodb_table", "sqs_queue", "s3_bucket" all classify.
 */
const INFRA_KIND_HINTS = [
  "datastore",
  "database",
  "table",
  "queue",
  "topic",
  "subject",
  "stream",
  "bucket",
  "storage",
  "cache",
  "function",
  "lambda",
  "network",
  "vpc",
  "subnet",
  "cluster",
  "cloud",
  "gateway",
  "balancer",
  "loadbalancer",
  "cdn",
  "dns",
  "secret",
  "resource",
  "infra",
  "container",
  "pod",
  "node_group",
  "instance",
  "broker",
  "registry",
];

/**
 * Tiers that, absent a clearer kind signal, indicate an infra resource. `data`
 * and `messaging` lanes are datastores / queues; `edge` is gateways/CDNs.
 * `client`/`auth`/`compute`/`external` lean code (a service, handler, SDK call).
 */
const INFRA_TIERS: ReadonlySet<CompoundTier> = new Set<CompoundTier>([
  "data",
  "messaging",
]);

/**
 * classifyNode decides whether a node is an infra resource or a code entity.
 * Kind wins (it is the most specific signal); tier is the fallback. Defaults to
 * "code" so an unknown entity reads as application code rather than infra.
 */
export function classifyNode(node: {
  kind: string;
  tier: CompoundTier;
}): NodeClass {
  const kind = (node.kind || "").toLowerCase();
  for (const hint of INFRA_KIND_HINTS) {
    if (kind.includes(hint)) return "infra";
  }
  if (INFRA_TIERS.has(node.tier)) return "infra";
  return "code";
}

/** A map from node id → its unified class, for the whole node set. */
export function classifyNodes(
  nodes: CompoundNode[] | undefined,
): Map<string, NodeClass> {
  const out = new Map<string, NodeClass>();
  for (const n of nodes ?? []) out.set(n.id, classifyNode(n));
  return out;
}

/**
 * isCrossBoundary returns true when an edge joins a code node to an infra node
 * (in either direction) — the real code↔infra usage links we want to emphasise
 * in the unified diagram. Edges where the class of an endpoint is unknown
 * (endpoint not on the canvas) are NOT cross-boundary.
 */
export function isCrossBoundary(
  edge: { source: string; target: string },
  classes: Map<string, NodeClass>,
): boolean {
  const s = classes.get(edge.source);
  const t = classes.get(edge.target);
  if (!s || !t) return false;
  return s !== t;
}

/** Per-class counts + the number of real cross-boundary (code↔infra) edges. */
export interface UnifiedStats {
  infraNodes: number;
  codeNodes: number;
  crossEdges: number;
  totalEdges: number;
}

/**
 * unifiedStats summarises the interleaved diagram: how many infra vs code nodes
 * are present and how many real cross-boundary usage edges wire them together.
 * Drives the legend / empty-state copy. When there are zero cross-boundary
 * edges the caller surfaces the #4983 limitation (deployment links missing).
 */
export function unifiedStats(
  nodes: CompoundNode[] | undefined,
  edges: CompoundEdge[] | undefined,
): UnifiedStats {
  const classes = classifyNodes(nodes);
  let infraNodes = 0;
  let codeNodes = 0;
  for (const c of classes.values()) {
    if (c === "infra") infraNodes++;
    else codeNodes++;
  }
  let crossEdges = 0;
  const all = edges ?? [];
  for (const e of all) {
    if (isCrossBoundary(e, classes)) crossEdges++;
  }
  return { infraNodes, codeNodes, crossEdges, totalEdges: all.length };
}
