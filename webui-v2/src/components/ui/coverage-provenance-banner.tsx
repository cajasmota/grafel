import { useState } from "react";
import {
  ChevronDown,
  ClipboardList,
  GaugeCircle,
  Layers,
  AlertTriangle,
  Bot,
  Plug,
} from "lucide-react";
import { cn } from "@/lib/utils";
import {
  resolveCoverageProvenance,
  COVERAGE_DEFINITIONS,
  type CoverageSourceState,
  type CoverageProvenanceKind,
} from "@/lib/coverage-provenance";

export interface CoverageProvenanceBannerProps {
  /**
   * Raw availability state for the current view. When omitted (or empty) the
   * banner degrades to the capability-coverage explanation — never a
   * misleading authoritative "%". This is the expected v1 state until ingested
   * line coverage (#5036) is wired into the dashboard data layer.
   */
  state?: CoverageSourceState | null;
  className?: string;
}

const KIND_ICON: Record<CoverageProvenanceKind, typeof GaugeCircle> = {
  line: GaugeCircle,
  reachability: Layers,
  capability: ClipboardList,
};

const TONE_CLASSES: Record<
  "success" | "info" | "neutral",
  { border: string; bg: string; icon: string }
> = {
  success: { border: "border-l-success", bg: "bg-success/5", icon: "text-success" },
  info: { border: "border-l-info", bg: "bg-info/5", icon: "text-info" },
  neutral: { border: "border-l-border", bg: "bg-surface-2/40", icon: "text-text-4" },
};

/**
 * CoverageProvenanceBanner (#5038) — whenever the dashboard shows a coverage
 * number, this banner states HOW it was measured (line ▸ reachability ▸
 * capability), whether report ingestion is available, what it means for
 * agents/MCP, and (for ingested line coverage) when it was measured + whether
 * it is stale. All branch selection lives in {@link resolveCoverageProvenance}
 * — this component is a pure renderer over that descriptor, and degrades
 * gracefully to the capability explanation when nothing is wired.
 */
export function CoverageProvenanceBanner({ state, className }: CoverageProvenanceBannerProps) {
  const [open, setOpen] = useState(false);
  const p = resolveCoverageProvenance(state);
  const Icon = KIND_ICON[p.kind];
  const tone = TONE_CLASSES[p.tone];

  return (
    <div
      className={cn(
        "w-full overflow-hidden rounded-lg border border-border border-l-4 bg-surface",
        tone.border,
        tone.bg,
        className,
      )}
    >
      <div className="px-4 py-3">
        <div className="flex items-start gap-2.5">
          <Icon size={16} className={cn("mt-0.5 shrink-0", tone.icon)} aria-hidden />
          <div className="min-w-0 flex-1 space-y-1.5">
            <div className="flex items-center gap-2">
              <span className="text-xs font-semibold uppercase tracking-wide text-text-2">
                {p.label}
              </span>
              {p.freshness?.stale && (
                <span
                  className="inline-flex items-center gap-1 rounded-full border border-warning/40 bg-warning/10 px-1.5 py-px text-[10px] font-medium text-warning"
                  title="Coverage was measured before the most recent index — it may be stale."
                >
                  <AlertTriangle size={10} aria-hidden />
                  may be stale
                </span>
              )}
            </div>

            <p className="text-sm leading-relaxed text-text-3">{p.method}</p>

            {p.freshness?.measuredAt && !p.freshness.stale && (
              <p className="text-xs text-text-4">Measured {p.freshness.measuredAt}.</p>
            )}

            {/* Agents / MCP meaning */}
            <p className="flex items-start gap-1.5 text-xs leading-relaxed text-text-4">
              <Bot size={12} className="mt-0.5 shrink-0" aria-hidden />
              <span>{p.agentMeaning}</span>
            </p>

            {/* Availability — how to turn on real line coverage */}
            {p.howToEnable && (
              <p className="flex items-start gap-1.5 text-xs leading-relaxed text-text-4">
                <Plug size={12} className="mt-0.5 shrink-0" aria-hidden />
                <span>{p.howToEnable}</span>
              </p>
            )}

            {/* Self-documenting expandable: defines all three coverage types. */}
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              className="inline-flex items-center gap-1 text-xs text-text-3 transition-colors hover:text-text-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] rounded"
            >
              <ChevronDown
                size={13}
                className={cn("transition-transform", open && "rotate-180")}
                aria-hidden
              />
              {open ? "Hide" : "What are the coverage types?"}
            </button>

            {open && (
              <dl className="mt-1 space-y-2 border-t border-border pt-2">
                {COVERAGE_DEFINITIONS.map((d) => (
                  <div key={d.kind}>
                    <dt
                      className={cn(
                        "text-xs font-semibold",
                        d.kind === p.kind ? tone.icon : "text-text-2",
                      )}
                    >
                      {d.title}
                      {d.kind === p.kind && (
                        <span className="ml-1.5 font-normal text-text-4">
                          (what you're seeing)
                        </span>
                      )}
                    </dt>
                    <dd className="text-xs leading-relaxed text-text-4">{d.body}</dd>
                  </div>
                ))}
              </dl>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
