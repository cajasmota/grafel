/* ============================================================
   hooks/use-daemon-mode.ts — TanStack Query hooks for the
   daemon mode switcher (S7a of #2149, #2169).

   Wraps GET/POST /api/v2/daemon/mode so components never call
   the api client directly.
   ============================================================ */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

/** Stable query key for the daemon mode. */
export const daemonModeQueryKey = ["daemon", "mode"] as const;

/**
 * Fetches the currently configured daemon mode and the mode catalogue.
 * Refetches every 30 seconds so the badge stays consistent if the mode
 * is changed externally (e.g. via CLI).
 */
export function useDaemonMode() {
  return useQuery({
    queryKey: daemonModeQueryKey,
    queryFn: () => api.getDaemonMode(),
    staleTime: 30_000,
    refetchInterval: 30_000,
  });
}

/**
 * Switches the active daemon mode. On success the daemon is restarted
 * by the backend. The caller should poll /api/v2/meta (or useDaemonMode)
 * to confirm the daemon came back up.
 *
 * Invalidates daemonModeQueryKey after a brief delay so the badge
 * reflects the new mode once the daemon has restarted.
 */
export function useSetDaemonMode() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (newMode: string) => api.setDaemonMode(newMode),
    onSuccess: () => {
      // Delay invalidation so the new daemon has time to start up.
      setTimeout(() => {
        void qc.invalidateQueries({ queryKey: daemonModeQueryKey });
      }, 2_500);
    },
  });
}
