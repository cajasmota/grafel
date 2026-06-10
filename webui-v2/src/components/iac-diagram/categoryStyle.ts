/* ============================================================
   components/iac-diagram/categoryStyle.ts — resource_category styling.

   The single cross-tool join key every IaC tool emits is resource_category
   (datastore/queue/topic/stream/function/cache/secret/network/compute/
   storage/other — see internal/types/iac_resource_category.go). The
   architecture diagram (#4526) colors + icons each resource node by that
   category so a modularized stack reads at a glance. Colors reference the
   theme CSS vars (tone palette) used elsewhere in the dashboard rather than
   hard-coded hex, so they track light/dark.
   ============================================================ */

import {
  Database,
  HardDrive,
  Network,
  Cpu,
  Inbox,
  Radio,
  Waves,
  Zap,
  Boxes,
  KeyRound,
  Package,
  type LucideIcon,
} from "lucide-react";

export interface CategoryStyle {
  /** Lower-cased canonical category key. */
  key: string;
  /** Display label (upper-cased at render). */
  label: string;
  Icon: LucideIcon;
  /** CSS color var for the node accent (border / icon / chip). */
  color: string;
  /** Translucent background tint var pairing with `color`. */
  tint: string;
}

// Each category maps to one of the dashboard tone palette vars. We reuse the
// existing --info / --warning / --accent / --danger / --success / --text-4
// channels so the diagram stays on-theme without introducing new tokens.
const STYLES: Record<string, Omit<CategoryStyle, "key">> = {
  datastore: { label: "Datastore", Icon: Database, color: "var(--info)", tint: "var(--info-bg, rgba(56,139,253,0.12))" },
  cache: { label: "Cache", Icon: HardDrive, color: "var(--info)", tint: "var(--info-bg, rgba(56,139,253,0.12))" },
  queue: { label: "Queue", Icon: Inbox, color: "var(--warning)", tint: "var(--warning-bg, rgba(210,153,34,0.12))" },
  topic: { label: "Topic", Icon: Radio, color: "var(--warning)", tint: "var(--warning-bg, rgba(210,153,34,0.12))" },
  stream: { label: "Stream", Icon: Waves, color: "var(--warning)", tint: "var(--warning-bg, rgba(210,153,34,0.12))" },
  function: { label: "Function", Icon: Zap, color: "var(--accent)", tint: "var(--accent-bg, rgba(124,108,255,0.14))" },
  secret: { label: "Secret", Icon: KeyRound, color: "var(--danger)", tint: "var(--danger-bg, rgba(248,81,73,0.12))" },
  network: { label: "Network", Icon: Network, color: "var(--success)", tint: "var(--success-bg, rgba(63,185,80,0.12))" },
  compute: { label: "Compute", Icon: Cpu, color: "var(--success)", tint: "var(--success-bg, rgba(63,185,80,0.12))" },
  storage: { label: "Storage", Icon: HardDrive, color: "var(--success)", tint: "var(--success-bg, rgba(63,185,80,0.12))" },
  // #4625 — a Terraform module instance rendered as a child-stack aggregate node.
  module: { label: "Module", Icon: Package, color: "var(--accent)", tint: "var(--accent-bg, rgba(124,108,255,0.14))" },
};

const FALLBACK: Omit<CategoryStyle, "key"> = {
  label: "Other",
  Icon: Boxes,
  color: "var(--text-4)",
  tint: "var(--surface-2, rgba(110,118,129,0.10))",
};

/** Resolve the styling for a resource_category (case-insensitive, "" → other). */
export function categoryStyle(category?: string): CategoryStyle {
  const key = (category || "other").toLowerCase();
  const base = STYLES[key] ?? FALLBACK;
  return { key, ...base };
}
