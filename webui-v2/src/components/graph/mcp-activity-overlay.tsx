/* ============================================================
   components/graph/mcp-activity-overlay.tsx — Jarvis MCP activity surface.

   A subtle, in-canvas affordance for the real-time MCP-query glow (#1157):
     • A small "pulse badge" (bottom-right of the canvas) showing the SSE
       connection state, a live-pulsing dot while a query is active, and the
       running query count. Click it to open the log.
     • A toggle in the badge menu to turn the whole overlay (and the glow)
       on/off — default ON.
     • A slide-out log panel listing the last 50 MCP events (time / tool /
       node count) with a "replay" button that re-triggers the glow.

   Ported + restyled to WebUI v2 tokens from the deleted v1 dashboard overlay
   (#1232). Theme-aware via the same surface/border/text tokens the rest of v2
   uses, so it follows the dark/light toggle automatically.
   ============================================================ */

import { memo, useCallback, useEffect, useRef, useState } from "react";
import { Activity, RefreshCw, X } from "lucide-react";
import type { MCPActivityEvent } from "@/hooks/use-mcp-activity";

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
}

export const MCPActivityOverlay = memo(function MCPActivityOverlay({
  enabled,
  connected,
  isActive,
  totalCount,
  eventLog,
  onToggle,
  onReplay,
}: MCPActivityOverlayProps) {
  const [panelOpen, setPanelOpen] = useState(false);
  const logRef = useRef<HTMLDivElement>(null);

  // Escape closes the panel.
  useEffect(() => {
    if (!panelOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        setPanelOpen(false);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [panelOpen]);

  // Keep the log scrolled to the newest entry.
  useEffect(() => {
    if (panelOpen && logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight;
  }, [eventLog.length, panelOpen]);

  const toggleEnabled = useCallback(() => onToggle(!enabled), [enabled, onToggle]);

  // ── badge state ──────────────────────────────────────────────────────────
  const dotClass = !enabled
    ? "bg-text-4"
    : isActive
      ? "bg-amber-400"
      : connected
        ? "bg-emerald-400"
        : "bg-text-4";
  const label = !enabled
    ? "MCP activity overlay off"
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
          className="mb-1 w-72 overflow-hidden rounded-lg border border-border bg-surface/95 shadow-lg backdrop-blur-sm"
          data-testid="mcp-activity-panel"
        >
          <div className="flex items-center justify-between border-b border-border px-3 py-2">
            <span className="flex items-center gap-1.5 text-xs font-semibold text-text-2">
              <Activity size={12} className="text-amber-400" /> MCP activity
            </span>
            <button
              onClick={() => setPanelOpen(false)}
              aria-label="Close MCP activity log"
              className="rounded p-0.5 text-text-3 hover:text-text"
            >
              <X size={13} />
            </button>
          </div>
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
          <div className="flex items-center justify-between border-t border-border px-3 py-1.5">
            <span className="text-[11px] text-text-4">Glow on MCP query</span>
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
      ) : null}

      {/* ── pulse badge ─────────────────────────────────────────────────── */}
      <button
        type="button"
        onClick={() => (enabled ? setPanelOpen((p) => !p) : toggleEnabled())}
        aria-label={label}
        title={label}
        data-testid="mcp-activity-badge"
        className="flex items-center gap-1.5 rounded-md border border-border bg-surface/85 px-2 py-1 text-[11px] font-semibold tracking-wide text-text-2 backdrop-blur-sm transition-colors hover:bg-surface-2"
        style={isActive ? { boxShadow: "0 0 8px rgba(255,176,59,0.45)" } : undefined}
      >
        <span
          aria-hidden
          data-testid="mcp-activity-dot"
          className={`h-1.5 w-1.5 rounded-full ${dotClass} ${isActive ? "animate-pulse" : ""}`}
        />
        <Activity size={11} aria-hidden className={enabled ? "" : "opacity-40"} />
        <span className="tabular-nums">{enabled ? (totalCount > 0 ? totalCount : "MCP") : "off"}</span>
      </button>
    </div>
  );
});
