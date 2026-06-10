/**
 * effect-badge.ts — shared effect-kind → badge label + token-tinted class.
 *
 * The single source of truth for how an effect primitive (db_read / db_write /
 * http_out / fs / queue / …) is presented as a small tinted badge. Consumed by:
 *   - the FlowDag downstream-DAG node cards (per-node effect chips), and
 *   - the Paths-detail "Side effects" panel, which surfaces the endpoint's
 *     EFFECTIVE effects aggregated from its downstream CALLS (#4489).
 *
 * Keeping it here (rather than inline in FlowDagNode) means both surfaces tint
 * "DB write" the same warning token, so an effect reads identically wherever it
 * appears.
 */

/** Effect kind → short badge label + tone class. Unknown keys fall through to a humanized generic. */
const EFFECT_BADGE: Record<string, { label: string; cls: string }> = {
  db_read: { label: "DB read", cls: "text-info bg-info-soft" },
  db_write: { label: "DB write", cls: "text-warning bg-warning-soft" },
  http_out: { label: "HTTP", cls: "text-accent-strong bg-accent-soft" },
  fs: { label: "FS", cls: "text-text-3 bg-surface-2" },
  fs_read: { label: "FS read", cls: "text-text-3 bg-surface-2" },
  fs_write: { label: "FS write", cls: "text-text-3 bg-surface-2" },
  queue: { label: "Queue", cls: "text-accent-strong bg-accent-soft" },
};

export function effectBadge(effect: string): { label: string; cls: string } {
  return (
    EFFECT_BADGE[effect] ?? {
      // Humanize an unknown effect key (db_read → "db read") rather than drop it.
      label: effect.replace(/_/g, " "),
      cls: "text-text-3 bg-surface-2",
    }
  );
}
