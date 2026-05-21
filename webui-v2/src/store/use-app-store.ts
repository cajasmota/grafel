/* ============================================================
   store/use-app-store.ts — global UI state (Zustand).

   Holds the three appearance knobs (theme / palette / density) and the
   global command-palette flag. Per-screen state (graph focus, filters)
   is added by screen tickets. Side-effects (writing data-* attributes
   to <html>) live in use-theme-sync.ts, not here.
   ============================================================ */

import { create } from "zustand";

export type Theme = "light" | "dark";
export type Palette = "cool" | "warm";
export type Density = "compact" | "comfortable" | "cozy";

function persisted<T extends string>(key: string, fallback: T): T {
  if (typeof localStorage === "undefined") return fallback;
  return (localStorage.getItem(key) as T) ?? fallback;
}

interface AppState {
  theme: Theme;
  palette: Palette;
  density: Density;
  commandOpen: boolean;

  toggleTheme: () => void;
  setTheme: (t: Theme) => void;
  setPalette: (p: Palette) => void;
  setDensity: (d: Density) => void;
  setCommandOpen: (open: boolean) => void;
}

export const useAppStore = create<AppState>((set) => ({
  theme: persisted<Theme>("ag.theme", "light"),
  palette: persisted<Palette>("ag.palette", "cool"),
  density: persisted<Density>("ag.density", "comfortable"),
  commandOpen: false,

  toggleTheme: () => set((s) => ({ theme: s.theme === "dark" ? "light" : "dark" })),
  setTheme: (theme) => set({ theme }),
  setPalette: (palette) => set({ palette }),
  setDensity: (density) => set({ density }),
  setCommandOpen: (commandOpen) => set({ commandOpen }),
}));
