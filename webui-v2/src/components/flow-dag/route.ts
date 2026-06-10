/* ============================================================
   components/flow-dag/route.ts — click-to-highlight route (#4479).

   Given a clicked tree instance, compute the set of instance ids that form its
   full route through the endpoint: its single ancestor chain back to the root
   (trivial in a tree — one parent each) PLUS its entire forward subtree (all
   branches). The renderer dims everything off this set.
   ============================================================ */

import type { TreeInstance } from "./layout";

/**
 * Instance ids on the full route of `focusId`: ancestors (root-ward chain) +
 * the focus itself + descendants (its whole forward subtree). Returns an empty
 * set if `focusId` is not found.
 */
export function routeInstanceIds(
  instances: TreeInstance[],
  focusId: string,
): Set<string> {
  const byId = new Map<string, TreeInstance>();
  const childrenOf = new Map<string, string[]>();
  for (const inst of instances) {
    byId.set(inst.id, inst);
    if (inst.parentId != null) {
      const list = childrenOf.get(inst.parentId);
      if (list) list.push(inst.id);
      else childrenOf.set(inst.parentId, [inst.id]);
    }
  }

  if (!byId.has(focusId)) return new Set();

  const route = new Set<string>();

  // Ancestor chain: walk parentId up to the root (one parent per node).
  let cur: string | null = focusId;
  while (cur != null) {
    if (route.has(cur)) break; // defensive — a tree can't loop
    route.add(cur);
    cur = byId.get(cur)?.parentId ?? null;
  }

  // Forward subtree: BFS down the children adjacency from the focus.
  const queue = [focusId];
  while (queue.length > 0) {
    const id = queue.shift()!;
    for (const childId of childrenOf.get(id) ?? []) {
      if (route.has(childId)) continue;
      route.add(childId);
      queue.push(childId);
    }
  }

  return route;
}
