/* ============================================================
   components/graph/filters-drawer.tsx — right slide-out filters + tuning.

   Sections: Edge types · Repositories · Level of detail · (tuning panels).
   ============================================================ */

import { Dialog, DrawerContent, DialogTitle, Button } from "@/components/ui";
import {
  useGraphStore,
  type LodLevel,
  STRUCTURAL_EDGE_KINDS,
  SEMANTIC_EDGE_KINDS,
} from "@/store/use-graph-store";
import type { EdgeKind, GraphRepo } from "@/data/types";
import { TuningPanels } from "./tuning-panels";

// Distinct dot color per edge kind (toggle accent / legend). Structural kinds
// reuse the pastel scale; semantic kinds (#4252) get their own distinct hues.
const EDGE_KIND_COLOR: Record<EdgeKind, string> = {
  CALLS: "var(--pastel-1)",
  REFERENCES: "var(--pastel-2)",
  RENDERS: "var(--pastel-3)",
  DEPENDS_ON: "var(--pastel-4)",
  EXTENDS: "var(--pastel-5)",
  CONTAINS: "var(--pastel-6)",
  IMPORTS: "var(--pastel-7)",
  INJECTED_INTO: "var(--pastel-8)",
  THROWS: "var(--pastel-9)",
  CATCHES: "var(--pastel-10)",
  JOINS_COLLECTION: "var(--pastel-1)",
  HTTP_ENDPOINT_CALL: "var(--pastel-2)",
};

const EDGE_KIND_GROUPS: { title: string; kinds: EdgeKind[] }[] = [
  { title: "Structural", kinds: STRUCTURAL_EDGE_KINDS },
  { title: "Semantic", kinds: SEMANTIC_EDGE_KINDS },
];

const LODS: LodLevel[] = ["low", "mid", "high"];

// #4467 — min-degree quick toggles. 0 = show all (default), 1 = hide true
// zero-edge orphans, 2 = also hide degree-1 leaves (DTO members / types / config
// that read as a misleading "orphan ring"). Always reversible; hiding is also
// surfaced in the on-canvas low-degree badge.
const MIN_DEGREES: { value: number; label: string; hint: string }[] = [
  { value: 0, label: "All", hint: "Show every node (default)" },
  { value: 1, label: "≥1", hint: "Hide unconnected (zero-edge) nodes" },
  { value: 2, label: "≥2", hint: "Also hide degree-1 leaf nodes" },
];

export function FiltersDrawer({ repos }: { repos: GraphRepo[] }) {
  const filtersOpen = useGraphStore((s) => s.filtersOpen);
  const setFiltersOpen = useGraphStore((s) => s.setFiltersOpen);
  const enabledEdgeKinds = useGraphStore((s) => s.enabledEdgeKinds);
  const toggleEdgeKind = useGraphStore((s) => s.toggleEdgeKind);
  const activeRepos = useGraphStore((s) => s.activeRepos);
  const toggleRepo = useGraphStore((s) => s.toggleRepo);
  const lod = useGraphStore((s) => s.lod);
  const setLod = useGraphStore((s) => s.setLod);
  const minDegree = useGraphStore((s) => s.minDegree);
  const setMinDegree = useGraphStore((s) => s.setMinDegree);
  const clearAllFilters = useGraphStore((s) => s.clearAllFilters);

  return (
    <Dialog open={filtersOpen} onOpenChange={setFiltersOpen}>
      <DrawerContent className="flex flex-col">
        <DialogTitle className="text-md font-semibold text-text">Filters</DialogTitle>

        <div className="ag-scroll mt-4 min-h-0 flex-1 space-y-4">
          <section>
            <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-3">Edge types</h4>
            <div className="space-y-3">
              {EDGE_KIND_GROUPS.map((group) => (
                <div key={group.title}>
                  <h5 className="mb-1.5 text-[0.65rem] font-semibold uppercase tracking-wider text-text-4">
                    {group.title}
                  </h5>
                  <div className="flex flex-wrap gap-1.5">
                    {group.kinds.map((k) => {
                      const on = enabledEdgeKinds.has(k);
                      return (
                        <button
                          key={k}
                          onClick={() => toggleEdgeKind(k)}
                          aria-pressed={on}
                          className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 font-mono text-xs transition-colors ${
                            on
                              ? "border-transparent bg-accent-soft text-accent-strong"
                              : "border-border bg-surface text-text-3 hover:bg-surface-2"
                          }`}
                        >
                          <span
                            className="h-1.5 w-1.5 rounded-full"
                            style={{ background: EDGE_KIND_COLOR[k] }}
                          />
                          {k}
                        </button>
                      );
                    })}
                  </div>
                </div>
              ))}
            </div>
          </section>

          <section>
            <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-3">Repositories</h4>
            <div className="space-y-1.5">
              {repos.map((r) => {
                const on = activeRepos?.has(r.id) ?? false;
                return (
                  <button
                    key={r.id}
                    onClick={() => toggleRepo(r.id)}
                    aria-pressed={on}
                    className={`flex w-full items-center justify-between rounded-md border px-3 py-1.5 text-left transition-colors ${
                      on
                        ? "border-transparent bg-accent-soft text-accent-strong"
                        : "border-border bg-surface text-text-2 hover:bg-surface-2"
                    }`}
                  >
                    <span className="flex items-center gap-2">
                      <span
                        className="h-2.5 w-2.5 rounded-full"
                        style={{ background: `var(--pastel-${((r.colorIndex - 1) % 10) + 1})` }}
                      />
                      <span className="font-mono text-sm">{r.id}</span>
                    </span>
                    <span className="text-xs text-text-3">{r.language}</span>
                  </button>
                );
              })}
              {repos.length === 0 ? <p className="text-sm text-text-3">No repos.</p> : null}
            </div>
            {activeRepos ? (
              <p className="mt-1.5 text-xs text-text-3">{activeRepos.size} selected · others hidden</p>
            ) : (
              <p className="mt-1.5 text-xs text-text-3">No selection = all repos shown</p>
            )}
          </section>

          <section>
            <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-3">Level of detail</h4>
            <div className="grid grid-cols-3 gap-1">
              {LODS.map((l) => (
                <button
                  key={l}
                  onClick={() => setLod(l)}
                  aria-pressed={lod === l}
                  className={`h-7 rounded-md border text-xs font-medium uppercase transition-colors ${
                    lod === l
                      ? "border-transparent bg-accent-soft text-accent-strong"
                      : "border-border bg-surface text-text-2 hover:bg-surface-2"
                  }`}
                >
                  {l}
                </button>
              ))}
            </div>
          </section>

          <section>
            <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-3">
              Min degree
            </h4>
            <div className="grid grid-cols-3 gap-1">
              {MIN_DEGREES.map((m) => (
                <button
                  key={m.value}
                  onClick={() => setMinDegree(m.value)}
                  aria-pressed={minDegree === m.value}
                  title={m.hint}
                  className={`h-7 rounded-md border text-xs font-medium transition-colors ${
                    minDegree === m.value
                      ? "border-transparent bg-accent-soft text-accent-strong"
                      : "border-border bg-surface text-text-2 hover:bg-surface-2"
                  }`}
                >
                  {m.label}
                </button>
              ))}
            </div>
            <p className="mt-1.5 text-xs text-text-3">
              {minDegree === 0
                ? "Showing all nodes. Low-degree nodes are de-emphasized, not hidden."
                : minDegree === 1
                  ? "Hiding unconnected (zero-edge) nodes."
                  : "Hiding unconnected + degree-1 leaf nodes."}
            </p>
          </section>

          <TuningPanels />
        </div>

        <footer className="mt-4 flex items-center justify-between gap-2 border-t border-border pt-3">
          <Button variant="ghost" size="sm" onClick={clearAllFilters}>
            Clear all
          </Button>
          <Button variant="primary" size="sm" onClick={() => setFiltersOpen(false)}>
            Done
          </Button>
        </footer>
      </DrawerContent>
    </Dialog>
  );
}
