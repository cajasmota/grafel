/* ============================================================
   hooks/use-wizard.ts — shared create-group / add-repo scan wizard (#1517).

   The wizard is used by BOTH Landing (create-group) and Settings
   (add-repo). It scans a server-side path, previews the detected
   stack + monorepo layout, then creates/registers + indexes with
   live job progress.

   Per the Lego layering, screens go through these hooks; they never
   call api.* directly.
   ============================================================ */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { WizardRepo } from "@/data/types";
import { groupsQueryKey } from "@/hooks/use-groups";
import { settingsQueryKey } from "@/hooks/use-settings";

/** Detect step: resolve + inspect a server-side path. */
export function useScanInspect() {
  return useMutation({
    mutationFn: (path: string) => api.scanInspect(path),
  });
}

/** Create-group-from-scan: create + register + enqueue index job. Returns a JobAck. */
export function useCreateGroupFromScan() {
  return useMutation({
    mutationFn: ({ name, repos }: { name: string; repos: WizardRepo[] }) =>
      api.createGroupFromScan(name, repos),
  });
}

/** Add-repo-from-scan: register repos into an existing group + index. Returns a JobAck. */
export function useScanReposIntoGroup(groupId: string) {
  return useMutation({
    mutationFn: (repos: WizardRepo[]) => api.scanReposIntoGroup(groupId, repos),
  });
}

/**
 * Polls a wizard index job until it reaches a terminal state. Pass null to
 * disable. On completion, invalidates BOTH the Landing groups list and the
 * group's Settings detail so freshly-indexed counts appear everywhere.
 *
 * The backend also exposes an SSE stream at the job's stream_url; this hook
 * uses 1 s polling (the same proven pattern as Settings' useActionJob) to keep
 * the wizard resilient when the EventSource connection is interrupted.
 */
export function useWizardJob(jobId: string | null, groupId?: string) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["wizard-job", jobId],
    queryFn: async () => {
      const job = await api.getJob(jobId as string);
      if (job.status === "done" || job.status === "failed") {
        void qc.invalidateQueries({ queryKey: groupsQueryKey });
        if (groupId) void qc.invalidateQueries({ queryKey: settingsQueryKey(groupId) });
      }
      return job;
    },
    enabled: !!jobId,
    refetchInterval: (query) => {
      const s = query.state.data?.status;
      return s === "done" || s === "failed" ? false : 1000;
    },
  });
}
