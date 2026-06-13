import { GaugeCircle, Layers, ClipboardList } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/badge";
import {
  resolveCoverageKindIndicator,
  type CoverageSourceState,
  type CoverageProvenanceKind,
} from "@/lib/coverage-provenance";

/**
 * CoverageKindIndicator (#5067) — a compact inline chip that sits next to an
 * individual coverage "%" on a diagram surface (file-tree / module / endpoint
 * row, and where cheap a topology node) so the user can always tell WHICH of
 * the three archigraph coverages that specific number is.
 *
 * The full {@link CoverageProvenanceBanner} (#5038) disambiguates a whole
 * surface; this disambiguates one row. Both share {@link
 * resolveCoverageProvenance} so the precedence (line ▸ reachability ▸
 * capability) and tones never drift apart.
 *
 * THREE distinct visual treatments — the whole point per #5038/#5067 — so the
 * kind is legible at a glance and not by color alone:
 *
 *   - line        → success tone + gauge icon  ("Line")        authoritative %
 *   - reachability→ info tone + layers icon    ("Reach")       NOT a measured %
 *   - capability  → neutral tone + clipboard   ("Capability")  NOT test coverage
 *
 * Degradation: when `state` is absent/empty the underlying resolver returns the
 * capability default, so we render the neutral "Capability" chip — never a
 * misleading authoritative treatment.
 */

const KIND_ICON: Record<CoverageProvenanceKind, typeof GaugeCircle> = {
  line: GaugeCircle,
  reachability: Layers,
  capability: ClipboardList,
};

export interface CoverageKindIndicatorProps {
  /** Raw availability state for this row/node; absent ⇒ capability default. */
  state?: CoverageSourceState | null;
  /**
   * Render only the icon (+ tooltip), dropping the text label. For dense rows /
   * diagram nodes where horizontal space is scarce. The kind is still conveyed
   * by icon + tone + tooltip, never color alone (the tooltip names the kind).
   */
  iconOnly?: boolean;
  className?: string;
}

export function CoverageKindIndicator({
  state,
  iconOnly = false,
  className,
}: CoverageKindIndicatorProps) {
  const ind = resolveCoverageKindIndicator(state);
  const Icon = KIND_ICON[ind.kind];

  return (
    <Badge
      tone={ind.tone}
      title={ind.title}
      aria-label={`${ind.label} — ${ind.title}`}
      data-coverage-kind={ind.kind}
      className={cn("gap-1 px-1.5", className)}
    >
      <Icon size={11} aria-hidden className="shrink-0" />
      {iconOnly ? null : <span className="leading-none">{ind.short}</span>}
    </Badge>
  );
}
