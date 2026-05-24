/* ============================================================
   lib/use-ref-state.ts — URL-backed current-ref state (PH4 / #2092).

   Reads/writes the `?ref=` search-param. All in-group screens pass this
   value through to their data hooks, so switching the ref in the TopBar
   triggers data refreshes on every view without additional state wiring.

   Encoding: slashes inside ref names are percent-encoded (%2F) — the
   standard URLSearchParams behaviour. Callers that build href strings must
   use encodeURIComponent() for the VALUE; the URL path itself is never
   modified (only the search param).

   Default behaviour: when ?ref is absent, currentRef is null and each
   screen's data hook omits the ref param — the daemon uses the repo's
   current HEAD.
   ============================================================ */

import { useCallback } from "react";
import { useSearchParams } from "react-router-dom";

const REF_PARAM = "ref";

export interface UseRefStateResult {
  /** The current ref name from ?ref=, or null if absent (HEAD). */
  currentRef: string | null;
  /** Navigate to the same page with ?ref=<newRef>. Pass null to clear. */
  setRef: (ref: string | null) => void;
}

/**
 * Hook that binds the `?ref=` URL search param to readable/writable state.
 *
 * Usage:
 *   const { currentRef, setRef } = useRefState();
 *
 * When the user picks a different ref from the RefSelector dropdown,
 * call `setRef(name)` and all hooks that read `currentRef` will refetch.
 */
export function useRefState(): UseRefStateResult {
  const [searchParams, setSearchParams] = useSearchParams();

  const currentRef = searchParams.get(REF_PARAM);

  const setRef = useCallback(
    (ref: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (ref === null || ref === "") {
            next.delete(REF_PARAM);
          } else {
            next.set(REF_PARAM, ref);
          }
          return next;
        },
        // Replace vs push: keeps browser history clean while navigating refs.
        { replace: true },
      );
    },
    [setSearchParams],
  );

  return { currentRef, setRef };
}
