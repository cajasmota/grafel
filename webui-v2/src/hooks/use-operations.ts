/* ============================================================
   hooks/use-operations.ts — Operations screen data hooks.

   Per the Lego layering: screens never call api.* directly;
   they go through TanStack Query hooks defined here.

   Covers: System status, logs, updates, patterns, quality/orphan audit,
   recall measurement.
   ============================================================ */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

// ---------------------------------------------------------------------------
// System panel
// ---------------------------------------------------------------------------

export const systemStatusKey = () => ["ops", "system"] as const;

/** Polls GET /api/system every 5 s. */
export function useSystemStatus() {
  return useQuery({
    queryKey: systemStatusKey(),
    queryFn: () => api.getSystemStatus(),
    refetchInterval: 5000,
    retry: 1,
  });
}

export function useRestartDaemon() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.restartDaemon(),
    onSuccess: () => void qc.invalidateQueries({ queryKey: systemStatusKey() }),
  });
}

export function useStopDaemon() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.stopDaemon(),
    onSuccess: () => void qc.invalidateQueries({ queryKey: systemStatusKey() }),
  });
}

export const logsKey = (params: { n?: number; q?: string; severity?: string }) =>
  ["ops", "logs", params] as const;

export function useSystemLogs(params?: { n?: number; q?: string; severity?: string }) {
  return useQuery({
    queryKey: logsKey(params ?? {}),
    queryFn: () => api.getSystemLogs(params),
    retry: false,
  });
}

// ---------------------------------------------------------------------------
// Updates
// ---------------------------------------------------------------------------

export const updateCheckKey = () => ["ops", "updates"] as const;

export function useUpdateCheck() {
  return useQuery({
    queryKey: updateCheckKey(),
    queryFn: () => api.checkForUpdates(),
    // Don't auto-refresh — user triggers manually or on mount.
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
}

/** Runs `grafel update` (#1512). Re-checks the version on success. */
export function useApplyUpdate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.applyUpdate(),
    onSuccess: () => void qc.invalidateQueries({ queryKey: updateCheckKey() }),
  });
}

// ---------------------------------------------------------------------------
// Maintenance — cleanup orphaned registry entries (#1512)
// ---------------------------------------------------------------------------

/**
 * Previews (dryRun:true, default) or executes (false) registry cleanup.
 * Wraps POST /api/v2/maintenance/cleanup.
 */
export function useCleanup() {
  return useMutation({
    mutationFn: (dryRun: boolean) => api.runCleanup(dryRun),
  });
}

// ---------------------------------------------------------------------------
// Index quarantine — transparency surface (Q2, #5617)
// ---------------------------------------------------------------------------

export const quarantineKey = (groupId: string) => ["ops", "quarantine", groupId] as const;

/** Lists auto-quarantined directories for a group. */
export function useQuarantine(groupId: string) {
  return useQuery({
    queryKey: quarantineKey(groupId),
    queryFn: () => api.getQuarantine(groupId),
    enabled: !!groupId,
    retry: 1,
  });
}

/** Manual un-quarantine of a directory. */
export function useUnquarantine(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { repo: string; rel: string }) => api.unquarantine(groupId, v.repo, v.rel),
    onSuccess: () => void qc.invalidateQueries({ queryKey: quarantineKey(groupId) }),
  });
}

/** Pin (never auto-heal) or unpin a quarantined directory. */
export function usePinQuarantine(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { repo: string; rel: string; pinned: boolean }) =>
      api.pinQuarantine(groupId, v.repo, v.rel, v.pinned),
    onSuccess: () => void qc.invalidateQueries({ queryKey: quarantineKey(groupId) }),
  });
}

// ---------------------------------------------------------------------------
// Patterns
// ---------------------------------------------------------------------------

export const patternsKey = (groupId: string, filters?: { needs_attention?: boolean; status?: string; confidence_min?: number }) =>
  ["ops", "patterns", groupId, filters] as const;

export function usePatterns(groupId: string, filters?: { needs_attention?: boolean; status?: string; confidence_min?: number }) {
  return useQuery({
    queryKey: patternsKey(groupId, filters),
    queryFn: () => api.listPatterns(groupId, filters),
    enabled: !!groupId,
    retry: 1,
  });
}

export function useDeletePattern(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patternId: string) => api.deletePattern(groupId, patternId),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["ops", "patterns", groupId] }),
  });
}

export function usePatternGC(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (dryRun: boolean) => api.runPatternGC(groupId, dryRun),
    onSuccess: (_data, dryRun) => {
      // Only invalidate on actual GC (not dry-run preview).
      if (!dryRun) void qc.invalidateQueries({ queryKey: ["ops", "patterns", groupId] });
    },
  });
}

export function useExportPatterns(groupId: string) {
  return useMutation({
    mutationFn: (target: { file?: string; repo?: string }) => api.exportPatterns(groupId, target),
  });
}

// ---------------------------------------------------------------------------
// Quality — orphan audit
// ---------------------------------------------------------------------------

export const orphanAuditKey = (groupId: string) => ["ops", "orphan-audit", groupId] as const;

export function useOrphanAudit(groupId: string) {
  return useQuery({
    queryKey: orphanAuditKey(groupId),
    queryFn: () => api.getOrphanAudit(groupId),
    enabled: !!groupId,
    // Don't auto-refresh; user triggers manually.
    staleTime: Infinity,
    retry: 1,
  });
}

export function useRunOrphanAudit(groupId: string) {
  const qc = useQueryClient();
  return useMutation({
    // POST actually runs + persists the audit (GET only reads the last result).
    mutationFn: () => api.runOrphanAudit(groupId),
    onSuccess: (data) => {
      qc.setQueryData(orphanAuditKey(groupId), data);
    },
  });
}

// ---------------------------------------------------------------------------
// Quality — recall measurement
// ---------------------------------------------------------------------------

export const fixturesKey = () => ["ops", "quality", "fixtures"] as const;

export function useQualityFixtures() {
  return useQuery({
    queryKey: fixturesKey(),
    queryFn: () => api.listQualityFixtures(),
    staleTime: 10 * 60 * 1000,
    retry: false,
  });
}

export function useRunRecall(groupId?: string) {
  return useMutation({
    mutationFn: (fixture: string) => api.runRecall(fixture, groupId),
  });
}
