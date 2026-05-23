/* ============================================================
   components/graph/mcp-activity-overlay.tsx — Jarvis MCP activity surface.

   A subtle, in-canvas affordance for the real-time MCP-query glow (#1157)
   plus the JARVIS replay-all controls (#1932):

     • A small "pulse badge" (bottom-right of the canvas) showing the SSE
       connection state, a live-pulsing dot while a query is active, and the
       running query count. Click it to open the log.
     • A toggle in the badge menu to turn the whole overlay (and the glow)
       on/off — default ON.
     • A slide-out log panel listing the last MAX_LOG MCP events (time /
       tool / node count) with:
         · the existing per-entry replay 🔄 (#1157 — unchanged),
         · a NEW Replay-all button (#1932-5) that walks every entry in panel
           order, glowing the graph and riding the SVG comet between hits,
         · speed slider 0.5×/1×/2× (#1932-6),
         · pause / resume + ESC shortcut (#1932-7),
         · a progress scrubber under the activity list during replay-all
           (#1932-8) — drag the playhead to scrub forward / backward; the
           backward direction triggers reverse-decay on the trail tint.
         · a graph-scoped audio toggle (#1932-12, off by default,
           persisted in localStorage["archigraph:graph:audio"]).

   The graph canvas's existing glow loop (#1157, see graph-canvas.tsx) is the
   bedrock — every per-entry replay AND each Replay-all step still flows
   through the same `onReplay(event)` so the daemon-side glow path is one
   shared code path. The SVG comet + trail + chevron rendering happens in a
   sibling component (graph-jarvis-overlay) driven by the same FlowAnim
   controller exposed here.
   ============================================================ */

import { memo, useCallback, useEffect, useRef, useState } from "react";
import { Activity, Pause, Play, RefreshCw, Square, Volume2, VolumeX, X } from "lucide-react";
import type { MCPActivityEvent } from "@/hooks/use-mcp-activity";
import type { FlowAnimController, FlowAnimSnapshot } from "@/lib/flow-animation";
import { GRAPH_SPEEDS } from "@/hooks/use-graph-jarvis-replay";
import type { JarvisStep } from "@/components/graph/graph-jarvis-overlay";

function formatTs(ts: number): string {
  const d = new Date(ts);
  const p = (n: number) => n.toString().padStart(2, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

const shortTool = (t: string) => t.replace(/^archigraph_/, "");

function nodeCount(ev: MCPActivityEvent): number {
  return (ev.returned_node_ids?.length ?? 0) + (ev.returned_edge_ids?.length ?? 0);
}

export interface MCPActivityOverlayProps {
  enabled: boolean;
  connected: boolean;
  isActive: boolean;
  totalCount: number;
  eventLog: MCPActivityEvent[];
  onToggle: (enabled: boolean) => void;
  onReplay: (event: MCPActivityEvent) => void;

  // ── #1932 replay-all wiring ───────────────────────────────────────────────
  /** The shared step-engine controller (null when there's < 2 steps). */
  replayController: FlowAnimController | null;
  /** Current rAF snapshot (driven by useSyncExternalStore in the hook). */
  replaySnapshot: FlowAnimSnapshot;
  /** Flattened step timeline (one entry per node arrival across all events). */
  replaySteps: JarvisStep[];
  /** Selected speed key (0.5 / 1 / 2). */
  speedKey: string;
  onSpeedKey: (k: string) => void;
  /** Audio toggle (off by default, persisted). */
  audioOn: boolean;
  onAudioToggle: (on: boolean) => void;
}

export const MCPActivityOverlay = memo(function MCPActivityOverlay({
  enabled,
  connected,
  isActive,
  totalCount,
  eventLog,
  onToggle,
  onReplay,
  replayController,
  replaySnapshot,
  replaySteps,
  speedKey,
  onSpeedKey,
  audioOn,
  onAudioToggle,
}: MCPActivityOverlayProps) {
  const [panelOpen, setPanelOpen] = useState(false);
  const logRef = useRef<HTMLDivElement>(null);

  // Escape closes the panel — unless a replay is mid-flight, in which case
  // the replay hook intercepts ESC for pause/resume FIRST (capture phase),
  // so the panel close only fires when no replay is active.
  useEffect(() => {
    if (!panelOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      // If the replay hook already handled this (pause/resume), don't close.
      const snap = replayController?.getSnapshot();
      if (snap && (snap.running || snap.paused)) return;
      e.stopPropagation();
      setPanelOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [panelOpen, replayController]);

  // Keep the log scrolled to the newest entry.
  useEffect(() => {
    if (panelOpen && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [eventLog.length, panelOpen]);

  const toggleEnabled = useCallback(() => onToggle(!enabled), [enabled, onToggle]);

  // ── replay-all controls ────────────────────────────────────────────────────
  // The button shows Stop while a replay is running OR paused. The scrubber
  // also stays visible while the playhead has advanced past 0 even if the
  // engine isn't currently animating (so a user can keep dragging after a
  // scrub stopped the engine).
  const isReplaying = replaySnapshot.running || replaySnapshot.paused;
  const scrubberVisible =
    isReplaying || (replayController !== null && replaySnapshot.playhead > 0);
  const canReplay = !!replayController && replaySteps.length >= 2;
  const startOrStop = useCallback(() => {
    if (!replayController) return;
    if (isReplaying) {
      replayController.stop();
      replayController.reset();
    } else {
      replayController.start();
    }
  }, [replayController, isReplaying]);
  const pauseOrResume = useCallback(() => {
    if (!replayController) return;
    if (replaySnapshot.paused) replayController.resume();
    else if (replaySnapshot.running) replayController.pause();
  }, [replayController, replaySnapshot.paused, replaySnapshot.running]);

  // ── badge state ──────────────────────────────────────────────────────────
  const dotClass = !enabled
    ? "bg-text-4"
    : isActive || isReplaying
      ? "bg-amber-400"
      : connected
        ? "bg-emerald-400"
        : "bg-text-4";
  const label = !enabled
    ? "MCP activity overlay off"
    : isReplaying
      ? "MCP replay in progress"
      : isActive
        ? "MCP query active"
        : connected
          ? `MCP activity connected — ${totalCount} ${totalCount === 1 ? "query" : "queries"}`
          : "MCP activity stream disconnected";

  return (
    <div className="absolute bottom-3 right-24 z-30 flex flex-col items-end gap-1.5">
      {/* ── slide-out log panel ─────────────────────────────────────────── */}
      {panelOpen && enabled ? (
        <div
          className="mb-1 w-80 overflow-hidden rounded-lg border border-border bg-surface/95 shadow-lg backdrop-blur-sm"
          data-testid="mcp-activity-panel"
        >
          <div className="flex items-center justify-between border-b border-border px-3 py-2">
            <span className="flex items-center gap-1.5 text-xs font-semibold text-text-2">
              <Activity size={12} className="text-amber-400" /> MCP activity
              {isReplaying ? (
                <span
                  className="ml-1 rounded bg-amber-400/15 px-1.5 py-px text-[10px] font-medium text-amber-400"
                  aria-live="polite"
                >
                  replay {replaySnapshot.paused ? "paused" : "running"}
                </span>
              ) : null}
            </span>
            <button
              onClick={() => setPanelOpen(false)}
              aria-label="Close MCP activity log"
              className="rounded p-0.5 text-text-3 hover:text-text"
            >
              <X size={13} />
            </button>
          </div>

          {/* ── Replay-all toolbar (#1932) ─────────────────────────────── */}
          <div className="flex items-center gap-1.5 border-b border-border px-3 py-1.5">
            <button
              onClick={startOrStop}
              disabled={!canReplay}
              aria-label={isReplaying ? "Stop replay" : "Replay all queries"}
              title={isReplaying ? "Stop" : "Replay all"}
              data-testid="mcp-replay-all-toggle"
              className="inline-flex items-center gap-1 rounded border border-border bg-surface px-2 py-0.5 text-[11px] font-medium text-text-2 hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {isReplaying ? <Square size={11} /> : <Play size={11} />}
              {isReplaying ? "Stop" : "Replay all"}
            </button>
            <button
              onClick={pauseOrResume}
              disabled={!isReplaying}
              aria-label={replaySnapshot.paused ? "Resume replay" : "Pause replay"}
              title={replaySnapshot.paused ? "Resume (ESC)" : "Pause (ESC)"}
              data-testid="mcp-replay-pause"
              className="inline-flex items-center gap-1 rounded border border-border bg-surface px-1.5 py-0.5 text-[11px] text-text-3 hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {replaySnapshot.paused ? <Play size={11} /> : <Pause size={11} />}
            </button>
            {/* Speed segmented control */}
            <div className="ml-auto inline-flex overflow-hidden rounded border border-border" role="radiogroup" aria-label="Replay speed">
              {GRAPH_SPEEDS.map((s) => (
                <button
                  key={s.key}
                  role="radio"
                  aria-checked={speedKey === s.key}
                  onClick={() => onSpeedKey(s.key)}
                  data-testid={`mcp-replay-speed-${s.key}`}
                  className={`px-1.5 py-0.5 text-[10px] font-mono tabular-nums transition-colors ${
                    speedKey === s.key
                      ? "bg-accent/20 text-text"
                      : "text-text-3 hover:bg-surface-2"
                  }`}
                >
                  {s.label}
                </button>
              ))}
            </div>
          </div>

          {/* ── Activity list ───────────────────────────────────────────── */}
          <div ref={logRef} className="max-h-64 overflow-y-auto">
            {eventLog.length === 0 ? (
              <p className="px-3 py-4 text-center text-xs text-text-4">
                No MCP queries yet. Run an archigraph MCP tool and watch the graph glow.
              </p>
            ) : (
              eventLog.map((ev, i) => (
                <div
                  key={`${ev.timestamp}-${i}`}
                  className="flex items-center gap-2 border-b border-border/50 px-3 py-1.5 text-xs last:border-0"
                  data-testid="mcp-activity-entry"
                >
                  <span className="font-mono tabular-nums text-text-4">{formatTs(ev.timestamp)}</span>
                  <span className="min-w-0 flex-1 truncate font-medium text-text-2">
                    {shortTool(ev.tool_name)}
                  </span>
                  {nodeCount(ev) > 0 ? (
                    <span className="tabular-nums text-text-3">{nodeCount(ev)}</span>
                  ) : null}
                  <button
                    onClick={() => onReplay(ev)}
                    aria-label="Replay this query's glow"
                    title="Replay glow"
                    className="rounded p-0.5 text-text-3 hover:text-amber-400"
                    disabled={nodeCount(ev) === 0}
                  >
                    <RefreshCw size={11} />
                  </button>
                </div>
              ))
            )}
          </div>

          {/* ── Progress scrubber (#1932-8) ─────────────────────────────── */}
          {scrubberVisible && replayController ? (
            <ReplayScrubber
              controller={replayController}
              snapshot={replaySnapshot}
              steps={replaySteps}
            />
          ) : null}

          {/* ── Footer: settings ─────────────────────────────────────────── */}
          <div className="flex items-center justify-between border-t border-border px-3 py-1.5">
            <span className="text-[11px] text-text-4">Glow on MCP query</span>
            <div className="flex items-center gap-1.5">
              <button
                onClick={() => onAudioToggle(!audioOn)}
                role="switch"
                aria-checked={audioOn}
                aria-label="Toggle replay audio"
                title={audioOn ? "Audio on — click to mute" : "Audio off — click to enable"}
                data-testid="mcp-replay-audio-toggle"
                className={`rounded p-0.5 transition-colors ${
                  audioOn ? "text-amber-400" : "text-text-4 hover:text-text-3"
                }`}
              >
                {audioOn ? <Volume2 size={12} /> : <VolumeX size={12} />}
              </button>
              <button
                onClick={toggleEnabled}
                role="switch"
                aria-checked={enabled}
                aria-label="Toggle MCP activity glow"
                className={`relative h-4 w-7 rounded-full transition-colors ${
                  enabled ? "bg-accent" : "bg-surface-2"
                }`}
              >
                <span
                  className={`absolute top-0.5 h-3 w-3 rounded-full bg-white transition-transform ${
                    enabled ? "translate-x-3.5" : "translate-x-0.5"
                  }`}
                />
              </button>
            </div>
          </div>
        </div>
      ) : null}

      {/* ── pulse badge ─────────────────────────────────────────────────── */}
      <button
        type="button"
        onClick={() => (enabled ? setPanelOpen((p) => !p) : toggleEnabled())}
        aria-label={label}
        title={label}
        data-testid="mcp-activity-badge"
        className="flex items-center gap-1.5 rounded-md border border-border bg-surface/85 px-2 py-1 text-[11px] font-semibold tracking-wide text-text-2 backdrop-blur-sm transition-colors hover:bg-surface-2"
        style={isActive || isReplaying ? { boxShadow: "0 0 8px rgba(255,176,59,0.45)" } : undefined}
      >
        <span
          aria-hidden
          data-testid="mcp-activity-dot"
          className={`h-1.5 w-1.5 rounded-full ${dotClass} ${isActive || isReplaying ? "animate-pulse" : ""}`}
        />
        <Activity size={11} aria-hidden className={enabled ? "" : "opacity-40"} />
        <span className="tabular-nums">{enabled ? (totalCount > 0 ? totalCount : "MCP") : "off"}</span>
      </button>
    </div>
  );
});

// ─── Progress scrubber (#1932-8) ───────────────────────────────────────────
// Draggable playhead under the activity list. Scrubs the FlowAnim controller
// instantly (the engine's `lastScrubDir` drives reverse-decay rendering in
// the overlay layer). Hover shows a small label with the step's tool name.

interface ReplayScrubberProps {
  controller: FlowAnimController;
  snapshot: FlowAnimSnapshot;
  steps: JarvisStep[];
}

function ReplayScrubber({ controller, snapshot, steps }: ReplayScrubberProps) {
  const trackRef = useRef<HTMLDivElement>(null);
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);
  const [dragging, setDragging] = useState(false);

  const total = steps.length;
  if (total < 2) return null;

  const segments = total - 1;
  const base = Math.max(0, snapshot.playhead - 1);
  const inFlight = snapshot.running && !snapshot.paused && snapshot.edgeProgress > 0;
  const frac = inFlight
    ? Math.min(1, (base + snapshot.edgeProgress) / segments)
    : snapshot.playhead === 0
      ? 0
      : Math.min(1, base / segments);

  function idxFromEvent(e: React.PointerEvent | PointerEvent) {
    const el = trackRef.current;
    if (!el) return 0;
    const rect = el.getBoundingClientRect();
    const x = (e as PointerEvent).clientX - rect.left;
    const t = Math.max(0, Math.min(1, x / Math.max(1, rect.width)));
    return Math.round(t * segments) + 1;
  }

  return (
    <div
      className="flex items-center gap-2 border-t border-border bg-surface px-3 py-1.5"
      data-testid="mcp-replay-scrubber"
    >
      <span className="w-12 flex-none font-mono text-[10px] tabular-nums text-text-4">
        {snapshot.playhead} / {total}
      </span>
      <div
        ref={trackRef}
        className="relative h-5 flex-1 cursor-pointer select-none"
        onPointerDown={(e) => {
          e.currentTarget.setPointerCapture(e.pointerId);
          setDragging(true);
          controller.scrubTo(idxFromEvent(e));
        }}
        onPointerMove={(e) => {
          if (dragging) controller.scrubTo(idxFromEvent(e));
        }}
        onPointerUp={(e) => {
          try {
            e.currentTarget.releasePointerCapture(e.pointerId);
          } catch {
            /* ignore */
          }
          setDragging(false);
        }}
        onMouseMove={(e) => {
          const el = trackRef.current;
          if (!el) return;
          const rect = el.getBoundingClientRect();
          const t = (e.clientX - rect.left) / Math.max(1, rect.width);
          const idx = Math.max(0, Math.min(total - 1, Math.round(t * (total - 1))));
          setHoverIdx(idx);
        }}
        onMouseLeave={() => setHoverIdx(null)}
      >
        <div
          className="absolute left-0 right-0 top-1/2 h-[3px] -translate-y-1/2 rounded-full"
          style={{ background: "var(--border)" }}
        />
        <div
          className="absolute left-0 top-1/2 h-[3px] -translate-y-1/2 rounded-full"
          style={{
            width: `${frac * 100}%`,
            background: "var(--ag-graph-accent, #60a5fa)",
            transition: dragging || inFlight ? "none" : "width 120ms linear",
          }}
        />
        {/* Ticks — one per step, capped so a long timeline doesn't crowd. */}
        {Array.from({ length: Math.min(total, 60) }, (_, i) => {
          const idx = Math.round((i / Math.max(1, Math.min(total, 60) - 1)) * (total - 1));
          return (
            <span
              key={i}
              className="absolute top-1/2 -translate-y-1/2"
              style={{
                left: `${(idx / segments) * 100}%`,
                width: 1,
                height: idx === 0 || idx === total - 1 ? 9 : 5,
                background: "var(--text-4)",
                transform: "translate(-0.5px, -50%)",
              }}
            />
          );
        })}
        <span
          className="pointer-events-none absolute top-1/2 -translate-y-1/2 rounded-full"
          style={{
            left: `${frac * 100}%`,
            transform: "translate(-50%, -50%)",
            width: 11,
            height: 11,
            background: "var(--ag-graph-accent, #60a5fa)",
            boxShadow: "0 0 0 2px var(--surface), 0 0 4px var(--ag-graph-accent, #60a5fa)",
            transition: dragging || inFlight ? "none" : "left 120ms linear",
          }}
        />
        {hoverIdx != null && steps[hoverIdx] ? (
          <span
            className="pointer-events-none absolute -top-5 max-w-[14rem] truncate rounded-xs border border-border bg-surface-2 px-1.5 py-0.5 font-mono text-[9px] text-text-2"
            style={{ left: `${(hoverIdx / segments) * 100}%`, transform: "translateX(-50%)" }}
          >
            {hoverIdx + 1}. {steps[hoverIdx].label ?? steps[hoverIdx].nodeId}
          </span>
        ) : null}
      </div>
    </div>
  );
}
