/* ============================================================
   insight-context.tsx — app-wide "current insight" register (#4655).

   Phase A of moving the per-screen InsightBanner out of page bodies and
   into a glowing breadcrumb "Insights" button + popover (see InsightButton).

   Pages/tabs register the CURRENT screen's insight via useSetInsight(...),
   and the breadcrumb button consumes the active value to render the banner
   inside a popover. When the registered insight CHANGES identity (navigation
   or tab switch) the button glows for ~4s; a `glowNonce` is bumped on every
   change so the button can re-trigger its pulse, debounced so rapid changes
   don't spam.

   Usage (in a route or tab):
     useSetInsight({
       human: <>…</>,
       agent: { tool: "archigraph_test_coverage", example: "…" },
     });

   `useSetInsight` registers on mount / when its value changes (by identity)
   and CLEARS on unmount, so navigating away leaves no stale insight behind.
   ============================================================ */

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { InsightBannerAgent } from "./insight-banner";

/** The payload a page/tab registers as the current screen insight. */
export interface InsightValue {
  /** Plain-language, human-facing description (LEFT column). */
  human: ReactNode;
  /** How agents consume the same data via an MCP tool (RIGHT column). */
  agent: InsightBannerAgent;
  /**
   * Optional persistence key forwarded to the InsightBanner (collapsed state).
   * Mostly cosmetic in the popover but kept for parity with inline banners.
   */
  storageKey?: string;
}

interface InsightContextValue {
  /** The active insight, or null when no screen has registered one. */
  insight: InsightValue | null;
  /**
   * Monotonic counter bumped whenever the active insight changes identity.
   * The button watches this to (re)trigger its ~4s glow.
   */
  glowNonce: number;
  /** Register/replace the current insight (null clears). Used by useSetInsight. */
  setInsight: (value: InsightValue | null) => void;
}

const InsightContext = createContext<InsightContextValue | null>(null);

export function InsightProvider({ children }: { children: ReactNode }) {
  const [insight, setInsightState] = useState<InsightValue | null>(null);
  const [glowNonce, setGlowNonce] = useState(0);
  // Debounce identity-change glow bumps so rapid re-registers (e.g. a tab that
  // re-renders twice) don't fire multiple pulses in quick succession.
  const glowTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const setInsight = useCallback((value: InsightValue | null) => {
    setInsightState((prev) => {
      // Only bump the glow when the value actually changes identity AND there
      // is a new (non-null) insight to show — clearing shouldn't glow.
      if (prev !== value && value != null) {
        if (glowTimer.current) clearTimeout(glowTimer.current);
        glowTimer.current = setTimeout(() => {
          setGlowNonce((n) => n + 1);
          glowTimer.current = null;
        }, 120);
      }
      return value;
    });
  }, []);

  useEffect(
    () => () => {
      if (glowTimer.current) clearTimeout(glowTimer.current);
    },
    [],
  );

  const value = useMemo(
    () => ({ insight, glowNonce, setInsight }),
    [insight, glowNonce, setInsight],
  );

  return <InsightContext.Provider value={value}>{children}</InsightContext.Provider>;
}

/** Read the active insight + glow nonce. Used by the breadcrumb InsightButton. */
export function useInsight(): InsightContextValue {
  const ctx = useContext(InsightContext);
  if (!ctx) {
    // Safe fallback so a stray consumer never crashes the tree.
    return { insight: null, glowNonce: 0, setInsight: () => {} };
  }
  return ctx;
}

/**
 * useSetInsight — register the CURRENT screen/tab insight from a page or tab.
 *
 * Sets on mount and whenever `value` changes by identity (so a tab switch that
 * passes a new object re-registers and re-glows the button), and CLEARS on
 * unmount so navigating away never leaves a stale insight behind.
 *
 * Pass `null` to explicitly register "no insight" (button hides).
 */
export function useSetInsight(value: InsightValue | null): void {
  const { setInsight } = useInsight();

  useEffect(() => {
    setInsight(value);
    return () => setInsight(null);
    // Re-run on identity change of `value` — callers should pass a stable/memoized
    // object per logical insight (e.g. one object per active tab).
  }, [value, setInsight]);
}
