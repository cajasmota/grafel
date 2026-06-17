/* ============================================================
   hooks/use-settings.ts — Settings-screen data hooks.

   Per the Lego layering: screens never call api.* directly;
   they go through TanStack Query hooks defined here.
   All mutations invalidate the settingsQueryKey so the screen
   stays consistent after live-saves.
   ============================================================ */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { groupsQueryKey } from "@/hooks/use-groups";
import type { SettingsFeatures } from "@/data/types";

/** Query key for the settings detail of a group. */
export const settingsQueryKey = (groupId: string) => ["settings", groupId] as const;

/**
 * Fetches the full SettingsGroup shape for the settings screen.
 * Returns null data when the group is missing (404).
 */
export function useSettingsGroup(groupId: string) {
  return useQuery({
    queryKey: settingsQueryKey(groupId),
    queryFn: () => api.getSettingsGroup(groupId),
    retry: (failureCount, error) => {
      // Don't retry 404 — the group is genuinely gone.
      if (error && typeof error === "object" && "status" in error && (error as { status: number }).status === 404) {
        return false;
      }
      return failureCount < 2;
    },
  });
}

/** Live-saves feature toggles. Invalidates the settings query on success. */
export function usePatchFeatures(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (features: SettingsFeatures) => api.patchFeatures(groupId, features),
    onSuccess: () => void qc.invalidateQueries({ queryKey: settingsQueryKey(groupId) }),
  });
}

/** Saves the docs path. Invalidates the settings query on success. */
export function usePatchDocs(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (docsPath: string) => api.patchDocs(groupId, docsPath),
    onSuccess: () => void qc.invalidateQueries({ queryKey: settingsQueryKey(groupId) }),
  });
}

/**
 * Triggers an async full-group rebuild (#1512). Resolves to a JobAck (202) —
 * the caller feeds job_id into useActionJob to track progress.
 */
export function useRebuildGroup(groupId: string) {
  return useMutation({
    mutationFn: () => api.rebuildGroup(groupId),
  });
}

/** Deletes the group. Invalidates the groups list so Landing + project switcher
 *  refresh immediately; caller is also responsible for navigating away. */
export function useDeleteGroup(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.deleteGroup(groupId),
    onSuccess: () => {
      // Remove the deleted group from the landing cache immediately so no
      // stale card lingers, then invalidate to trigger a fresh fetch.
      void qc.invalidateQueries({ queryKey: groupsQueryKey });
      // Also remove any cached settings for this group so stale :groupId routes
      // cannot serve data for a group that no longer exists.
      void qc.removeQueries({ queryKey: settingsQueryKey(groupId) });
    },
  });
}

/** Adds a repo to the group. Invalidates settings on success. */
export function useAddRepo(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ slug, path }: { slug: string; path: string }) =>
      api.addRepo(groupId, slug, path),
    onSuccess: () => void qc.invalidateQueries({ queryKey: settingsQueryKey(groupId) }),
  });
}

/** Removes a repo from the group. Invalidates settings on success. */
export function useRemoveRepo(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ repoSlug, keepCache }: { repoSlug: string; keepCache?: boolean }) =>
      api.removeRepo(groupId, repoSlug, keepCache),
    onSuccess: () => void qc.invalidateQueries({ queryKey: settingsQueryKey(groupId) }),
  });
}

/** Triggers an async single-repo rebuild (#1512). Resolves to a JobAck. */
export function useRebuildRepo(groupId: string) {
  return useMutation({
    mutationFn: (repoSlug: string) => api.rebuildRepo(groupId, repoSlug),
  });
}

/** Triggers an async repo cache-wipe + rebuild (#1512). Resolves to a JobAck. */
export function useResetRepo(groupId: string) {
  return useMutation({
    mutationFn: (repoSlug: string) => api.resetRepo(groupId, repoSlug),
  });
}

/**
 * Polls an async action job (#1512) until it reaches a terminal state.
 * Pass null to disable. On completion, invalidates the settings query so the
 * screen reflects the freshly-indexed entity counts.
 */
export function useActionJob(groupId: string, jobId: string | null) {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["action-job", jobId],
    queryFn: async () => {
      const job = await api.getJob(jobId as string);
      if (job.status === "done" || job.status === "failed") {
        void qc.invalidateQueries({ queryKey: settingsQueryKey(groupId) });
      }
      return job;
    },
    enabled: !!jobId,
    // Poll every second while running; stop once terminal.
    refetchInterval: (query) => {
      const s = query.state.data?.status;
      return s === "done" || s === "failed" ? false : 1000;
    },
  });
}

/** Updates monorepo package selection. Invalidates settings on success. */
export function usePatchMonorepo(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ repoSlug, packages }: { repoSlug: string; packages: string[] }) =>
      api.patchMonorepo(groupId, repoSlug, packages),
    onSuccess: () => void qc.invalidateQueries({ queryKey: settingsQueryKey(groupId) }),
  });
}

/** Runs grafel doctor for the group. Returns a DoctorCheck[]. */
export function useRunDoctor(groupId: string) {
  return useMutation({
    mutationFn: () => api.runDoctor(groupId),
  });
}

// ── AI-tool selection settings (#5257, epic #5252) ───────────────────────────

/** Query key for the AI-tool selection status of a group. */
export const toolsQueryKey = (groupId: string) => ["tools", groupId] as const;

/** Fetches the AI-tool selection status (enabled + detected per adapter). */
export function useTools(groupId: string) {
  return useQuery({
    queryKey: toolsQueryKey(groupId),
    queryFn: () => api.getTools(groupId),
  });
}

/**
 * Saves the desired enabled tool-set. The server applies the delta in-process
 * (rules files + MCP entries) and returns a per-tool summary. Invalidates the
 * tools query on success so the panel reflects the persisted state.
 */
export function usePutTools(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (tools: string[]) => api.putTools(groupId, tools),
    onSuccess: () => void qc.invalidateQueries({ queryKey: toolsQueryKey(groupId) }),
  });
}
