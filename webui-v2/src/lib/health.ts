/* ============================================================
   lib/health.ts — shared health → UI mapping for WebUI v2.

   Single source of truth for converting a GroupHealth string
   (emitted by the backend) into a color token and display label.
   All three surfaces that show a status dot (command-palette,
   top-bar, landing cards) import from here so they stay in sync.

   Health bands (server-side, from v2_fidelity.go):
     fidelity >= 0.97  → "healthy"    green
     fidelity >= 0.90  → "warning"    amber
     fidelity  < 0.90  → "degraded"   amber  (not yet critical)
     never indexed     → "unindexed"  slate

   "critical" (red) is reserved for future use — no backend value
   currently maps to red so no dot should be red today.
   ============================================================ */

import type { GroupHealth } from "@/data/types";

export interface HealthDisplay {
  /** CSS color value (use as `style={{ background: color }}`). */
  color: string;
  /** Short human-readable label. */
  label: string;
}

const HEALTH_DISPLAY: Record<GroupHealth, HealthDisplay> = {
  healthy:   { color: "var(--success)", label: "Healthy" },
  warning:   { color: "var(--warning)", label: "Low fidelity" },
  degraded:  { color: "var(--warning)", label: "Degraded" },
  unindexed: { color: "var(--text-4)",  label: "Not indexed" },
};

/**
 * Returns { color, label } for a given GroupHealth value.
 * Falls back to the "unindexed" style if the value is unexpected.
 */
export function healthDisplay(health: GroupHealth | string | undefined): HealthDisplay {
  if (health && health in HEALTH_DISPLAY) {
    return HEALTH_DISPLAY[health as GroupHealth];
  }
  return HEALTH_DISPLAY.unindexed;
}

/**
 * Returns a human-readable tooltip string for the health dot,
 * including the fidelity percentage when available.
 *
 * Examples:
 *   healthTooltip("degraded", 0.56)   → "Health: degraded — Fidelity 56%"
 *   healthTooltip("healthy", 0.99)    → "Health: healthy — Fidelity 99%"
 *   healthTooltip("unindexed", null)  → "Health: not indexed"
 */
export function healthTooltip(health: GroupHealth | string, fidelity: number | null | undefined): string {
  const base = `Health: ${health}`;
  if (fidelity == null) return base;
  return `${base} — Fidelity ${Math.round(fidelity * 100)}%`;
}
