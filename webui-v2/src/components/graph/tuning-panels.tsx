/* ============================================================
   components/graph/tuning-panels.tsx — live tuning controls.

   Four collapsible sections (Node Sizing / Simulation / Rendering /
   Group-by), localStorage-persisted via use-graph-store. Lesson ported from
   v1: the owner tunes the galaxy live and the settings survive reloads.

   Rendered inside the Filters drawer as a fourth section so the screen has
   one slide-out surface, not two competing panels.

   Visibility gate
   ───────────────
   All panels are guarded by TUNING_PANELS_VISIBLE below.
   • true  → panels are always shown (current state — owner tunes defaults)
   • false → panels are hidden; useful once good defaults are baked in

   "Copy all settings" button exports the current simulation + nodeSizing +
   render config as formatted JSON to the clipboard. Paste the result into
   use-graph-store.ts DEFAULT_* constants once you've found good values.
   ============================================================ */

// ── Visibility gate — flip to false once the owner has captured good values ──
export const TUNING_PANELS_VISIBLE = true;

import { useRef, useCallback } from "react";
import { Copy, Check, RotateCcw } from "lucide-react";
import { useState } from "react";
import {
  useGraphStore,
  type GroupByMode,
  DEFAULT_SIMULATION,
  DEFAULT_NODE_SIZING,
  DEFAULT_RENDER,
} from "@/store/use-graph-store";
import { Button } from "@/components/ui";

// ── Shared primitives ─────────────────────────────────────────────────────────

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
  const decimals = step < 1 ? (step < 0.05 ? 2 : 2) : 0;
  const display = decimals > 0 ? value.toFixed(decimals) : String(value);
  return (
    <label className="block">
      <div className="flex items-center justify-between text-sm text-text-3">
        <span>{label}</span>
        <span className="font-mono tabular-nums text-text-2">{display}</span>
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

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between text-sm text-text-3">
      <span>{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent)] ${
          checked ? "bg-[var(--accent)]" : "bg-border"
        }`}
        aria-label={label}
      >
        <span
          className={`inline-block h-3.5 w-3.5 rounded-full bg-white shadow transition-transform ${
            checked ? "translate-x-4" : "translate-x-0.5"
          }`}
        />
      </button>
    </div>
  );
}

function Section({
  title,
  children,
  modified,
  onReset,
}: {
  title: string;
  children: React.ReactNode;
  modified?: boolean;
  onReset?: () => void;
}) {
  return (
    <div className="border-t border-border pt-3">
      <div className="mb-2 flex items-center justify-between">
        <h4 className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-text-3">
          {title}
          {modified && (
            <span
              className="h-1.5 w-1.5 rounded-full bg-[var(--accent)]"
              title="Modified from defaults"
              aria-label="Modified from defaults"
            />
          )}
        </h4>
        {onReset && modified && (
          <button
            type="button"
            onClick={onReset}
            aria-label={`Reset ${title} to defaults`}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-text-3 hover:bg-surface-2 hover:text-text"
          >
            <RotateCcw size={10} />
            Reset
          </button>
        )}
      </div>
      <div className="space-y-2.5">{children}</div>
    </div>
  );
}

// ── Group-by tab row ──────────────────────────────────────────────────────────

const GROUP_BY: { id: GroupByMode; label: string }[] = [
  { id: "repo", label: "Repo" },
  { id: "community", label: "Community" },
  { id: "module", label: "Module" },
  { id: "none", label: "None" },
];

// ── Main component ────────────────────────────────────────────────────────────

export function TuningPanels() {
  const { simulation, nodeSizing, render, groupBy } = useGraphStore();
  const { setSimulation, setNodeSizing, setRender, setGroupBy, requestRelayout } = useGraphStore();

  // ── Copy all settings ──────────────────────────────────────────────────────
  const [copied, setCopied] = useState(false);
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const copyAllSettings = useCallback(() => {
    const payload = JSON.stringify({ simulation, nodeSizing, render }, null, 2);
    navigator.clipboard.writeText(payload).then(() => {
      setCopied(true);
      if (copyTimerRef.current) clearTimeout(copyTimerRef.current);
      copyTimerRef.current = setTimeout(() => setCopied(false), 2000);
    }).catch(() => {
      /* clipboard blocked */
    });
  }, [simulation, nodeSizing, render]);

  // ── Modified flags (dot indicator + reset availability) ───────────────────
  const simModified = (
    simulation.repulsion     !== DEFAULT_SIMULATION.repulsion     ||
    simulation.linkSpring    !== DEFAULT_SIMULATION.linkSpring    ||
    simulation.linkDistance  !== DEFAULT_SIMULATION.linkDistance  ||
    simulation.friction      !== DEFAULT_SIMULATION.friction      ||
    simulation.center        !== DEFAULT_SIMULATION.center        ||
    simulation.settleTime    !== DEFAULT_SIMULATION.settleTime
  );
  const sizingModified = (
    nodeSizing.baseSize      !== DEFAULT_NODE_SIZING.baseSize     ||
    nodeSizing.degreeScale   !== DEFAULT_NODE_SIZING.degreeScale  ||
    nodeSizing.maxMultiplier !== DEFAULT_NODE_SIZING.maxMultiplier
  );
  const renderModified = (
    render.pointOpacity      !== DEFAULT_RENDER.pointOpacity      ||
    render.pointSizeScale    !== DEFAULT_RENDER.pointSizeScale    ||
    render.scalePointsOnZoom !== DEFAULT_RENDER.scalePointsOnZoom ||
    render.maxPointSize      !== DEFAULT_RENDER.maxPointSize      ||
    render.linkWidthScale    !== DEFAULT_RENDER.linkWidthScale    ||
    render.linkOpacity       !== DEFAULT_RENDER.linkOpacity       ||
    render.showLinks         !== DEFAULT_RENDER.showLinks
  );

  if (!TUNING_PANELS_VISIBLE) return null;

  return (
    <div className="space-y-3">

      {/* ── Group by ──────────────────────────────────────────────────────── */}
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

      {/* ── Node sizing ───────────────────────────────────────────────────── */}
      <Section
        title="Node sizing"
        modified={sizingModified}
        onReset={() => setNodeSizing(DEFAULT_NODE_SIZING)}
      >
        <Slider
          label="Base size"
          value={nodeSizing.baseSize}
          min={40}
          max={320}
          step={10}
          onChange={(v) => setNodeSizing({ baseSize: v })}
        />
        <Slider
          label="Degree scale"
          value={nodeSizing.degreeScale}
          min={0}
          max={120}
          step={5}
          onChange={(v) => setNodeSizing({ degreeScale: v })}
        />
        <Slider
          label="Max multiplier"
          value={nodeSizing.maxMultiplier}
          min={1.0}
          max={8.0}
          step={0.25}
          onChange={(v) => setNodeSizing({ maxMultiplier: v })}
        />
      </Section>

      {/* ── Simulation ────────────────────────────────────────────────────── */}
      <Section
        title="Simulation"
        modified={simModified}
        onReset={() => setSimulation(DEFAULT_SIMULATION)}
      >
        <Slider
          label="Repulsion"
          value={simulation.repulsion}
          min={0.1}
          max={6.0}
          step={0.1}
          onChange={(v) => setSimulation({ repulsion: v })}
        />
        <Slider
          label="Link spring"
          value={simulation.linkSpring}
          min={0.0}
          max={3.0}
          step={0.05}
          onChange={(v) => setSimulation({ linkSpring: v })}
        />
        <Slider
          label="Link distance"
          value={simulation.linkDistance}
          min={1}
          max={60}
          step={1}
          onChange={(v) => setSimulation({ linkDistance: v })}
        />
        <Slider
          label="Center force"
          value={simulation.center}
          min={0}
          max={1.0}
          step={0.01}
          onChange={(v) => setSimulation({ center: v })}
        />
        <Slider
          label="Friction"
          value={simulation.friction}
          min={0.5}
          max={0.99}
          step={0.01}
          onChange={(v) => setSimulation({ friction: v })}
        />
        <Slider
          label="Settle cap (s)"
          value={simulation.settleTime}
          min={0.5}
          max={6.0}
          step={0.5}
          onChange={(v) => setSimulation({ settleTime: v })}
        />
        <Button variant="secondary" size="sm" className="w-full" onClick={requestRelayout}>
          Re-layout
        </Button>
      </Section>

      {/* ── Rendering ─────────────────────────────────────────────────────── */}
      <Section
        title="Rendering"
        modified={renderModified}
        onReset={() => setRender(DEFAULT_RENDER)}
      >
        <Slider
          label="Point opacity"
          value={render.pointOpacity}
          min={0.05}
          max={1.0}
          step={0.01}
          onChange={(v) => setRender({ pointOpacity: v })}
        />
        <Slider
          label="Point size scale"
          value={render.pointSizeScale}
          min={0.01}
          max={2.0}
          step={0.01}
          onChange={(v) => setRender({ pointSizeScale: v })}
        />
        <Slider
          label="Max point size (px)"
          value={render.maxPointSize}
          min={4}
          max={200}
          step={2}
          onChange={(v) => setRender({ maxPointSize: v })}
        />
        <Slider
          label="Link opacity"
          value={render.linkOpacity}
          min={0.0}
          max={1.0}
          step={0.01}
          onChange={(v) => setRender({ linkOpacity: v })}
        />
        <Slider
          label="Link width scale"
          value={render.linkWidthScale}
          min={0.05}
          max={3.0}
          step={0.05}
          onChange={(v) => setRender({ linkWidthScale: v })}
        />
        <Toggle
          label="Scale on zoom"
          checked={render.scalePointsOnZoom}
          onChange={(v) => setRender({ scalePointsOnZoom: v })}
        />
        <Toggle
          label="Show links"
          checked={render.showLinks}
          onChange={(v) => setRender({ showLinks: v })}
        />
      </Section>

      {/* ── Copy all settings ─────────────────────────────────────────────── */}
      <div className="border-t border-border pt-3">
        <p className="mb-2 text-xs text-text-3">
          Copy current knobs as JSON → paste into{" "}
          <code className="font-mono text-[11px]">use-graph-store.ts</code> DEFAULT_* constants.
        </p>
        <Button
          variant="secondary"
          size="sm"
          className="flex w-full items-center justify-center gap-1.5"
          onClick={copyAllSettings}
          aria-label="Copy all tuning settings as JSON"
        >
          {copied ? (
            <>
              <Check size={13} className="text-[var(--accent)]" />
              Copied!
            </>
          ) : (
            <>
              <Copy size={13} />
              Copy all settings
            </>
          )}
        </Button>
      </div>

    </div>
  );
}
