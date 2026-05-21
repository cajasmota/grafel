/* ============================================================
   components/graph/tuning-panels.tsx — live tuning controls.

   Four collapsible sections (Node Sizing / Simulation / Rendering /
   Group-by), localStorage-persisted via use-graph-store. Lesson ported from
   v1: the owner tunes the galaxy live and the settings survive reloads.

   Rendered inside the Filters drawer as a fourth section so the screen has
   one slide-out surface, not two competing panels.
   ============================================================ */

import { useGraphStore, type GroupByMode } from "@/store/use-graph-store";
import { Button } from "@/components/ui";

function Slider({
  label,
  value,
  min,
  max,
  step,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  onChange: (v: number) => void;
}) {
  return (
    <label className="block">
      <div className="flex items-center justify-between text-sm text-text-3">
        <span>{label}</span>
        <span className="font-mono tabular-nums text-text-2">{value}</span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(e) => onChange(parseFloat(e.target.value))}
        className="mt-1 w-full accent-[var(--accent)]"
      />
    </label>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="border-t border-border pt-3">
      <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-3">{title}</h4>
      <div className="space-y-2.5">{children}</div>
    </div>
  );
}

const GROUP_BY: { id: GroupByMode; label: string }[] = [
  { id: "repo", label: "Repo" },
  { id: "community", label: "Community" },
  { id: "module", label: "Module" },
  { id: "none", label: "None" },
];

export function TuningPanels() {
  const { simulation, nodeSizing, render, groupBy } = useGraphStore();
  const { setSimulation, setNodeSizing, setRender, setGroupBy, requestRelayout } = useGraphStore();

  return (
    <div className="space-y-3">
      <Section title="Group by">
        <div className="grid grid-cols-4 gap-1">
          {GROUP_BY.map((g) => (
            <button
              key={g.id}
              onClick={() => setGroupBy(g.id)}
              aria-pressed={groupBy === g.id}
              className={`h-7 rounded-md border text-xs font-medium transition-colors ${
                groupBy === g.id
                  ? "border-transparent bg-accent-soft text-accent-strong"
                  : "border-border bg-surface text-text-2 hover:bg-surface-2"
              }`}
            >
              {g.label}
            </button>
          ))}
        </div>
      </Section>

      <Section title="Node sizing">
        <Slider label="Base size" value={nodeSizing.baseSize} min={40} max={240} step={10} onChange={(v) => setNodeSizing({ baseSize: v })} />
        <Slider label="Degree scale" value={nodeSizing.degreeScale} min={0} max={80} step={5} onChange={(v) => setNodeSizing({ degreeScale: v })} />
      </Section>

      <Section title="Simulation">
        <Slider label="Repulsion" value={simulation.repulsion} min={0.1} max={4} step={0.1} onChange={(v) => setSimulation({ repulsion: v })} />
        <Slider label="Link distance" value={simulation.linkDistance} min={2} max={40} step={1} onChange={(v) => setSimulation({ linkDistance: v })} />
        <Slider label="Center force" value={simulation.center} min={0} max={1} step={0.05} onChange={(v) => setSimulation({ center: v })} />
        <Slider label="Settle cap (s)" value={simulation.settleTime} min={0.5} max={6} step={0.5} onChange={(v) => setSimulation({ settleTime: v })} />
        <Button variant="secondary" size="sm" className="w-full" onClick={requestRelayout}>
          Re-layout
        </Button>
      </Section>

      <Section title="Rendering">
        <Slider label="Point opacity" value={render.pointOpacity} min={0.2} max={1} step={0.02} onChange={(v) => setRender({ pointOpacity: v })} />
        <Slider label="Point size scale" value={render.pointSizeScale} min={0.05} max={1} step={0.01} onChange={(v) => setRender({ pointSizeScale: v })} />
        <Slider label="Link opacity" value={render.linkOpacity} min={0} max={1} step={0.02} onChange={(v) => setRender({ linkOpacity: v })} />
        <label className="flex items-center justify-between text-sm text-text-3">
          <span>Show links</span>
          <input type="checkbox" checked={render.showLinks} onChange={(e) => setRender({ showLinks: e.target.checked })} className="accent-[var(--accent)]" />
        </label>
      </Section>
    </div>
  );
}
