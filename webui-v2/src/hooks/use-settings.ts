/* ============================================================
   hooks/use-settings.ts — Settings-screen data hooks.

   Per the Lego layering: screens never call api.* directly;
   they go through TanStack Query hooks defined here.
   All mutations invalidate the settingsQueryKey so the screen
   stays consistent after live-saves.
   ============================================================ */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
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

/** Enqueues a full group rebuild (stub → 202 Accepted). */
export function useRebuildGroup(groupId: string) {
  return useMutation({
    mutationFn: () => api.rebuildGroup(groupId),
  });
}

/** Deletes the group. Does NOT invalidate — caller navigates away. */
export function useDeleteGroup(groupId: string) {
  return useMutation({
    mutationFn: () => api.deleteGroup(groupId),
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

/** Enqueues a single-repo rebuild (stub). */
export function useRebuildRepo(groupId: string) {
  return useMutation({
    mutationFn: (repoSlug: string) => api.rebuildRepo(groupId, repoSlug),
  });
}

/** Resets repo cache + rebuild (stub). */
export function useResetRepo(groupId: string) {
  return useMutation({
    mutationFn: (repoSlug: string) => api.resetRepo(groupId, repoSlug),
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

/** Runs archigraph doctor for the group. Returns a DoctorCheck[]. */
export function useRunDoctor(groupId: string) {
  return useMutation({
    mutationFn: () => api.runDoctor(groupId),
  });
}
