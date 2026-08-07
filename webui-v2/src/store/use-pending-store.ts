/* ============================================================
   store/use-pending-store.ts — Enrichment screen UI state (#1442).

   Separate from use-app-store.ts (appearance/command palette).
   Holds: tab, filter, groupBy, focusedId, openMap (group collapse),
   drafts (unsaved hint text per candidate), savedHints (confirmed).
   ============================================================ */

import { create } from "zustand";

export type EnrichmentTab = "repairs" | "enrichments";
export type EnrichmentFilter = "all" | "high" | "stale";
export type EnrichmentGroupBy = "type" | "severity" | "repo" | "none";

interface EnrichmentState {
  tab: EnrichmentTab;
  filter: EnrichmentFilter;
  groupBy: EnrichmentGroupBy;
  /** ID of the currently focused candidate row, or null. */
  focusedId: string | null;
  /** Map of groupKey → collapsed (false means collapsed; absent/true means open). */
  openMap: Record<string, boolean>;
  /**
   * Per-ENTITY-ID hint text typed but not yet saved to the server (#1518).
   * Keyed by candidate.entityId (SubjectID), NOT by candidate.id.
   */
  drafts: Record<string, string>;
  /**
   * Per-ENTITY-ID hint text confirmed saved (from PUT response or seeded from
   * server-returned hint field) (#1518).
   * Keyed by candidate.entityId (SubjectID), NOT by candidate.id.
   */
  savedHints: Record<string, string>;

  setTab: (tab: EnrichmentTab) => void;
  setFilter: (filter: EnrichmentFilter) => void;
  setGroupBy: (groupBy: EnrichmentGroupBy) => void;
  setFocusedId: (id: string | null) => void;
  toggleGroup: (key: string) => void;
  /** Set draft hint text for an entity. Pass candidate.entityId as key. */
  setDraft: (entityId: string, text: string) => void;
  /** Confirm that a hint was successfully saved for an entity. */
  confirmSave: (entityId: string, hint: string) => void;
  /** Seed server-provided hints (from GET /candidates response) into savedHints. */
  seedServerHints: (hints: Record<string, string>) => void;
}

export const useEnrichmentStore = create<EnrichmentState>((set) => ({
  tab: "repairs",
  filter: "all",
  groupBy: "type",
  focusedId: null,
  openMap: {},
  drafts: {},
  savedHints: {},

  setTab: (tab) => set({ tab, focusedId: null }),
  setFilter: (filter) => set({ filter }),
  setGroupBy: (groupBy) => set({ groupBy }),
  setFocusedId: (focusedId) => set({ focusedId }),
  toggleGroup: (key) =>
    set((s) => ({
      openMap: { ...s.openMap, [key]: s.openMap[key] === false ? true : false },
    })),
  setDraft: (entityId, text) =>
    set((s) => ({ drafts: { ...s.drafts, [entityId]: text } })),
  confirmSave: (entityId, hint) =>
    set((s) => {
      const drafts = { ...s.drafts };
      delete drafts[entityId];
      return { drafts, savedHints: { ...s.savedHints, [entityId]: hint } };
    }),
  seedServerHints: (hints) =>
    set((s) => ({
      // Merge server hints; locally confirmed saves take precedence.
      savedHints: { ...hints, ...s.savedHints },
    })),
}));
