/* ============================================================
   store/use-pending-store.ts — Pending screen UI state (#1442).

   Separate from use-app-store.ts (appearance/command palette).
   Holds: tab, filter, groupBy, focusedId, openMap (group collapse),
   drafts (unsaved hint text per candidate), savedHints (confirmed).
   ============================================================ */

import { create } from "zustand";

export type PendingTab = "repairs" | "enrichments";
export type PendingFilter = "all" | "high" | "stale";
export type PendingGroupBy = "type" | "severity" | "repo" | "none";

interface PendingState {
  tab: PendingTab;
  filter: PendingFilter;
  groupBy: PendingGroupBy;
  /** ID of the currently focused candidate row, or null. */
  focusedId: string | null;
  /** Map of groupKey → collapsed (false means collapsed; absent/true means open). */
  openMap: Record<string, boolean>;
  /** Per-candidate-id hint text typed but not yet saved to the server. */
  drafts: Record<string, string>;
  /** Per-candidate-id hint text that has been confirmed saved (from PUT response). */
  savedHints: Record<string, string>;

  setTab: (tab: PendingTab) => void;
  setFilter: (filter: PendingFilter) => void;
  setGroupBy: (groupBy: PendingGroupBy) => void;
  setFocusedId: (id: string | null) => void;
  toggleGroup: (key: string) => void;
  setDraft: (id: string, text: string) => void;
  confirmSave: (id: string, hint: string) => void;
}

export const usePendingStore = create<PendingState>((set) => ({
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
  setDraft: (id, text) => set((s) => ({ drafts: { ...s.drafts, [id]: text } })),
  confirmSave: (id, hint) =>
    set((s) => {
      const drafts = { ...s.drafts };
      delete drafts[id];
      return { drafts, savedHints: { ...s.savedHints, [id]: hint } };
    }),
}));
