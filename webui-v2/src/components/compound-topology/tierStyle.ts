/* tierStyle.ts — per-tier color + label for the compound topology lanes. */

import type { CompoundTier, CompoundEdgeType } from "@/data/types";

export interface TierStyle {
  label: string;
  color: string;
  tint: string;
}

export function tierStyle(tier: CompoundTier): TierStyle {
  switch (tier) {
    case "client":
      return { label: "Client", color: "var(--info)", tint: "color-mix(in srgb, var(--info) 14%, transparent)" };
    case "edge":
      return { label: "Edge", color: "var(--accent)", tint: "color-mix(in srgb, var(--accent) 14%, transparent)" };
    case "auth":
      return { label: "Auth", color: "var(--danger)", tint: "color-mix(in srgb, var(--danger) 14%, transparent)" };
    case "compute":
      return { label: "Compute", color: "var(--text-2)", tint: "color-mix(in srgb, var(--text-2) 12%, transparent)" };
    case "data":
      return { label: "Data", color: "var(--success)", tint: "color-mix(in srgb, var(--success) 14%, transparent)" };
    case "messaging":
      return { label: "Messaging", color: "var(--warning)", tint: "color-mix(in srgb, var(--warning) 14%, transparent)" };
    case "external":
      return { label: "External", color: "var(--text-4)", tint: "color-mix(in srgb, var(--text-4) 12%, transparent)" };
    default:
      return { label: tier, color: "var(--text-3)", tint: "transparent" };
  }
}

export function edgeStroke(type: CompoundEdgeType): string {
  switch (type) {
    case "reads":
      return "var(--text-3)";
    case "writes":
      return "var(--success)";
    case "invokes":
      return "var(--accent)";
    case "consumes":
      return "var(--warning)";
    case "routes":
      return "var(--info)";
    case "depends":
    default:
      return "var(--text-4)";
  }
}
