/* ============================================================
   routes/compare.tsx — Graph diff compare view (PH5 / #2093).

   Route: /g/:groupId/compare
   Params: ?refA=<ref>&refB=<ref>&repo=<slug>&filter=<bucket>&kind=<kind>

   Layout: three-panel
     ┌─────────────────────────────────────────────────────────┐
     │ DiffSummaryBanner                                       │
     ├──────────────┬──────────────────┬───────────────────────┤
     │ RefGraphPanel│ ChangeList       │ RefGraphPanel         │
     │ (refA, left) │ (center)         │ (refB, right)         │
     └──────────────┴──────────────────┴───────────────────────┘

   All URL params are preserved on navigation / reload for shareability.
   ============================================================ */

import { useCallback, useMemo, useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { GitCompare, Loader2, AlertTriangle } from "lucide-react";

import { useDiff } from "@/hooks/use-diff";
import { useRefs } from "@/hooks/use-refs";
import { DiffSummaryBanner } from "@/components/Compare/diff-summary-banner";
import { ChangeList, type ChangeFilter } from "@/components/Compare/change-list";
import { RefGraphPanel } from "@/components/Compare/ref-graph-panel";
import type { DiffEntityEntry } from "@/data/types";

// ---------------------------------------------------------------------------
// URL param helpers
// ---------------------------------------------------------------------------

const PARAM_REF_A = "refA";
const PARAM_REF_B = "refB";
const PARAM_REPO = "repo";
const PARAM_FILTER = "filter";
const PARAM_KIND = "kind";

function useCompareParams() {
  const [searchParams, setSearchParams] = useSearchParams();

  const refA = searchParams.get(PARAM_REF_A) ?? "";
  const refB = searchParams.get(PARAM_REF_B) ?? "";
  const repo = searchParams.get(PARAM_REPO) ?? "";
  const filter = (searchParams.get(PARAM_FILTER) as ChangeFilter | null) ?? "all";
  const kind = searchParams.get(PARAM_KIND) ?? "";

  const setParam = useCallback(
    (key: string, value: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value === null || value === "") {
            next.delete(key);
          } else {
            next.set(key, value);
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  return {
    refA,
    refB,
    repo,
    filter,
    kind,
    setRefA: (v: string) => setParam(PARAM_REF_A, v),
    setRefB: (v: string) => setParam(PARAM_REF_B, v),
    setRepo: (v: string) => setParam(PARAM_REPO, v),
    setFilter: (v: ChangeFilter) => setParam(PARAM_FILTER, v === "all" ? null : v),
    setKind: (v: string) => setParam(PARAM_KIND, v || null),
  };
}

// ---------------------------------------------------------------------------
// Ref + Repo selector row
// ---------------------------------------------------------------------------

interface SelectorRowProps {
  groupId: string;
  repo: string;
  refA: string;
  refB: string;
  onRepoChange: (v: string) => void;
  onRefAChange: (v: string) => void;
  onRefBChange: (v: string) => void;
}

function SelectorRow({
  groupId,
  repo,
  refA,
  refB,
  onRepoChange,
  onRefAChange,
  onRefBChange,
}: SelectorRowProps) {
  const { byRepo, isLoading } = useRefs(groupId);
  const repoSlugs = Object.keys(byRepo);
  const refsForRepo = byRepo[repo] ?? [];

  return (
    <div className="flex items-center gap-3 px-4 py-2 border-b border-border bg-surface-1 flex-wrap">
      {/* Repo picker */}
      <div className="flex items-center gap-1.5">
        <label className="text-xs text-text-3">Repo</label>
        <select
          className="text-xs bg-surface-2 border border-border rounded px-2 py-1 text-text-1 min-w-[120px]"
          value={repo}
          onChange={(e) => onRepoChange(e.target.value)}
          disabled={isLoading || repoSlugs.length === 0}
        >
          {repo === "" && <option value="">— select repo —</option>}
          {repoSlugs.map((slug) => (
            <option key={slug} value={slug}>{slug}</option>
          ))}
        </select>
      </div>

      {/* refA picker */}
      <div className="flex items-center gap-1.5">
        <label className="text-xs text-text-3 font-medium">Base (refA)</label>
        <select
          className="text-xs bg-surface-2 border border-border rounded px-2 py-1 text-text-1 min-w-[130px]"
          value={refA}
          onChange={(e) => onRefAChange(e.target.value)}
          disabled={!repo || refsForRepo.length === 0}
        >
          {refA === "" && <option value="">— select ref —</option>}
          {refsForRepo.map((r) => (
            <option key={r.name} value={r.name}>{r.name}</option>
          ))}
        </select>
      </div>

      <span className="text-text-3 text-xs">←</span>

      {/* refB picker */}
      <div className="flex items-center gap-1.5">
        <label className="text-xs text-accent font-medium">Head (refB)</label>
        <select
          className="text-xs bg-surface-2 border border-border rounded px-2 py-1 text-text-1 min-w-[130px]"
          value={refB}
          onChange={(e) => onRefBChange(e.target.value)}
          disabled={!repo || refsForRepo.length === 0}
        >
          {refB === "" && <option value="">— select ref —</option>}
          {refsForRepo.map((r) => (
            <option key={r.name} value={r.name}>{r.name}</option>
          ))}
        </select>
      </div>

      {isLoading && <Loader2 className="size-4 animate-spin text-text-3" />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty / setup placeholder
// ---------------------------------------------------------------------------

function SetupPlaceholder({ message }: { message: string }) {
  return (
    <div className="flex flex-col items-center justify-center h-full gap-3 text-text-3">
      <GitCompare className="size-8 opacity-40" />
      <p className="text-sm">{message}</p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Error placeholder
// ---------------------------------------------------------------------------

function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="flex items-center gap-3 px-4 py-3 bg-danger-soft border-b border-danger/20 text-danger text-sm">
      <AlertTriangle className="size-4 shrink-0" />
      <span>{message}</span>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main compare screen
// ---------------------------------------------------------------------------

export default function CompareScreen() {
  const { groupId = "" } = useParams();
  const { refA, refB, repo, filter, kind, setRefA, setRefB, setRepo, setFilter, setKind } =
    useCompareParams();

  // Highlighted entity (clicked in change list).
  const [selectedEntityId, setSelectedEntityId] = useState<string | undefined>();

  // Fetch diff.
  const {
    data: diff,
    isLoading,
    isError,
    error,
  } = useDiff(groupId, repo, refA || null, refB || null);

  // Derive entity sets for side panels.
  const entitySetA = useMemo((): DiffEntityEntry[] => {
    if (!diff) return [];
    // refA panel: entities that exist in refA = all entities minus "added" (which are new in refB).
    return [
      ...diff.entities.removed,   // only in refA
      ...diff.entities.modified,  // in both, but changed
      // entities in both that didn't change: we don't have a "same" bucket from the API,
      // so we approximate: (refB_modified ∪ same) vs (refA_removed ∪ modified).
      // For a side panel this is good enough — we show removed + modified for refA.
    ];
  }, [diff]);

  const entitySetB = useMemo((): DiffEntityEntry[] => {
    if (!diff) return [];
    // refB panel: entities that exist in refB = all entities minus "removed" (which are gone in refB).
    return [
      ...diff.entities.added,     // only in refB
      ...diff.entities.modified,  // in both, but changed
    ];
  }, [diff]);

  // Highlighted IDs for side panels.
  const highlightedIds = useMemo(() => {
    if (!selectedEntityId) return new Set<string>();
    return new Set([selectedEntityId]);
  }, [selectedEntityId]);

  const handleEntitySelect = useCallback(
    (entry: DiffEntityEntry) => {
      setSelectedEntityId((prev) => (prev === entry.id ? undefined : entry.id));
    },
    [],
  );

  // Determine what to render in the main area.
  const needsSetup = !repo || !refA || !refB;
  const sameRef = refA && refB && refA === refB;

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Selector row */}
      <SelectorRow
        groupId={groupId}
        repo={repo}
        refA={refA}
        refB={refB}
        onRepoChange={setRepo}
        onRefAChange={setRefA}
        onRefBChange={setRefB}
      />

      {/* Error banner */}
      {isError && (
        <ErrorBanner
          message={
            (error as Error)?.message ??
            `Could not load diff for ${refA} → ${refB}. Make sure both refs are indexed.`
          }
        />
      )}

      {/* Main content area */}
      <div className="flex-1 min-h-0 overflow-hidden">
        {needsSetup ? (
          <SetupPlaceholder message="Select a repo, base ref (refA), and head ref (refB) to compare." />
        ) : isLoading ? (
          <div className="flex items-center justify-center h-full gap-2 text-text-3">
            <Loader2 className="size-5 animate-spin" />
            <span className="text-sm">Loading diff…</span>
          </div>
        ) : sameRef ? (
          <div className="flex flex-col h-full">
            <DiffSummaryBanner
              refA={refA}
              refB={refB}
              summary={{
                entities_added: 0,
                entities_removed: 0,
                entities_modified: 0,
                relationships_added: 0,
                relationships_removed: 0,
                files_changed: 0,
              }}
            />
            <SetupPlaceholder message="No differences — both refs point to the same graph." />
          </div>
        ) : diff ? (
          <div className="flex flex-col h-full">
            {/* Summary banner */}
            <DiffSummaryBanner refA={refA} refB={refB} summary={diff.summary} />

            {/* Three-panel layout */}
            <div className="flex flex-1 min-h-0 gap-0 divide-x divide-border">
              {/* Left panel — refA */}
              <RefGraphPanel
                ref={refA}
                entities={entitySetA}
                highlightedEntityIds={highlightedIds}
                label="refA"
                className="w-[240px] shrink-0 rounded-none border-0 border-r border-border"
              />

              {/* Center panel — change list */}
              <div className="flex-1 min-w-0 flex flex-col">
                <ChangeList
                  diff={diff}
                  filter={filter}
                  onFilterChange={setFilter}
                  kindFilter={kind}
                  onKindFilterChange={setKind}
                  onEntitySelect={handleEntitySelect}
                  selectedEntityId={selectedEntityId}
                  className="h-full"
                />
              </div>

              {/* Right panel — refB */}
              <RefGraphPanel
                ref={refB}
                entities={entitySetB}
                highlightedEntityIds={highlightedIds}
                label="refB"
                className="w-[240px] shrink-0 rounded-none border-0 border-l border-border"
              />
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
