/* ============================================================
   SourcePeekProvider.tsx — app-wide source-peek controller (#4499).

   Mounts a single <SourcePeek> modal and exposes openSourcePeek() through
   context so ANY file:line ref renderer (RefLine, FlowDag cards, DI/quality
   links, IaC) can open it without threading open-state through every screen.

   Usage:
     const { openSourcePeek } = useSourcePeek();
     openSourcePeek({ groupId, file, line, repo });
   ============================================================ */

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { SourcePeek, type SourcePeekTarget } from "./SourcePeek";

interface SourcePeekContextValue {
  openSourcePeek: (target: SourcePeekTarget) => void;
}

const SourcePeekContext = createContext<SourcePeekContextValue | null>(null);

export function SourcePeekProvider({ children }: { children: ReactNode }) {
  const [target, setTarget] = useState<SourcePeekTarget | null>(null);
  const [open, setOpen] = useState(false);

  const openSourcePeek = useCallback((t: SourcePeekTarget) => {
    if (!t.file || !t.groupId) return;
    setTarget(t);
    setOpen(true);
  }, []);

  const value = useMemo(() => ({ openSourcePeek }), [openSourcePeek]);

  return (
    <SourcePeekContext.Provider value={value}>
      {children}
      {target && (
        <SourcePeek
          groupId={target.groupId}
          file={target.file}
          line={target.line}
          repo={target.repo}
          open={open}
          onOpenChange={setOpen}
        />
      )}
    </SourcePeekContext.Provider>
  );
}

/**
 * useSourcePeek — open the shared source-peek modal for a file:line ref.
 * Returns a no-op opener when used outside the provider (so a stray callsite
 * never crashes the tree), but in practice the provider wraps the whole app.
 */
export function useSourcePeek(): SourcePeekContextValue {
  const ctx = useContext(SourcePeekContext);
  if (!ctx) {
    return { openSourcePeek: () => {} };
  }
  return ctx;
}
