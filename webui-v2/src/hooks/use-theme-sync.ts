/* ============================================================
   hooks/use-theme-sync.ts — the only place that mutates <html>.

   Subscribes to the appearance knobs in the store and writes them to
   data-theme / data-palette / data-density on <html> + persists to
   localStorage. tokens.css re-themes the whole tree off these one
   attribute changes — no JS recalculation, no flash.
   ============================================================ */

import { useEffect } from "react";
import { useAppStore } from "@/store/use-app-store";

export function useThemeSync() {
  const theme = useAppStore((s) => s.theme);
  const palette = useAppStore((s) => s.palette);
  const density = useAppStore((s) => s.density);

  useEffect(() => {
    const root = document.documentElement;
    root.setAttribute("data-theme", theme);
    root.setAttribute("data-palette", palette);
    root.setAttribute("data-density", density);
    try {
      localStorage.setItem("ag.theme", theme);
      localStorage.setItem("ag.palette", palette);
      localStorage.setItem("ag.density", density);
    } catch {
      /* storage unavailable — attributes still applied */
    }
  }, [theme, palette, density]);
}
