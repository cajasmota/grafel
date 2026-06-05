/* ============================================================
   Links — Cross-repo call map (#4253, epic #4249).

   Route: /g/:groupId/links

   Surfaces capability data the backend already serves but no screen
   previously rendered: the resolved cross-repo link records from
   GET /api/groups/{group}/links (handlers_graph.go → handleGroupLinks).
   Each record is a directed edge between two entities that live in
   different repos — a frontend fetch resolving onto a backend endpoint,
   a publisher onto a topic, a gRPC client onto a service, etc.

   `source`/`target` are canonical prefixed entity ids "<repo>::<localId>"
   (normalizeLinkEndpoints rewrites them at load), so the repo of each
   side is the segment before "::" and the displayable label is derived
   from the local id tail.

   Layout mirrors Security / Quality: full-height column, a summary stat
   row, a kind filter, and a grouped list of source-repo → target-repo
   call edges with repo chips + a confidence meter. Reuses the shared
   primitives (Card, Badge, Pill, Skeleton) + RepoChip.
   ============================================================ */

import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { ArrowRight, GitBranch, Link2, AlertTriangle } from "lucide-react";

import {
  Badge,
  Card,
  CardBody,
  Pill,
} from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RepoChip } from "@/lib/repo-color";
import { cn } from "@/lib/utils";
import { useGroupLinks } from "@/hooks/use-links";
import type { CrossRepoLink } from "@/data/types";

// ---------------------------------------------------------------------------
// § Entity-id parsing — "<repo>::<localId>"; label = readable local tail.
// ---------------------------------------------------------------------------

function splitEntity(id: string): { repo: string; label: string } {
  const raw = id ?? "";
  const sep = raw.indexOf("::");
  if (sep === -1) {
    return { repo: "", label: shortLabel(raw) };
  }
  return { repo: raw.slice(0, sep), label: shortLabel(raw.slice(sep + 2)) };
}

/** Best-effort short, readable label from a local entity id. */
function shortLabel(local: string): string {
  if (!local) return "—";
  // Local ids often look like "kind:hash" or "pkg/Type.method:hash"; keep the
  // most meaningful trailing segment but drop a bare trailing hash.
  const parts = local.split(":");
  // If the last segment is a hex-ish hash, prefer the segment before it.
  const last = parts[parts.length - 1] ?? local;
  const isHashy = /^[0-9a-f]{6,}$/i.test(last);
  const chosen = isHashy && parts.length > 1 ? parts[parts.length - 2] : last;
  return chosen || local;
}

// ---------------------------------------------------------------------------
// § Kind styling
// ---------------------------------------------------------------------------

/** Normalise a link kind to a stable display token. */
function kindLabel(kind: string): string {
  return (kind || "LINK").replace(/_/g, " ");
}

function kindTone(kind: string): "accent" | "info" | "warning" | "neutral" {
  const k = (kind || "").toUpperCase();
  if (k.includes("HTTP") || k.includes("FETCH")) return "accent";
  if (k.includes("GRPC")) return "info";
  if (k.includes("PUBLISH") || k.includes("SUBSCRIBE") || k.includes("TOPIC") || k.includes("QUEUE"))
    return "warning";
  return "neutral";
}

// ---------------------------------------------------------------------------
// § Confidence meter
// ---------------------------------------------------------------------------

function ConfidenceMeter({ value }: { value: number | undefined }) {
  if (value == null) {
    return <span className="text-[10px] text-text-4 italic">conf —</span>;
  }
  const pct = Math.max(0, Math.min(1, value)) * 100;
  const tone =
    value >= 0.8 ? "var(--success)" : value >= 0.5 ? "var(--warning)" : "var(--danger)";
  return (
    <span className="inline-flex items-center gap-1.5 shrink-0" title={`confidence ${value.toFixed(2)}`}>
      <span className="h-1.5 w-12 rounded-full overflow-hidden bg-surface-2 border border-border">
        <span className="block h-full" style={{ width: `${pct}%`, background: tone }} />
      </span>
      <span className="text-[10px] tabular-nums text-text-4">{pct.toFixed(0)}%</span>
    </span>
  );
}

// ---------------------------------------------------------------------------
// § Shared state shells
// ---------------------------------------------------------------------------

function SkeletonRows({ n = 6 }: { n?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: n }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-3 h-12 px-4 rounded-lg border border-border"
        >
          <Skeleton w="w-1/4" />
          <Skeleton w="w-6" h="h-2" />
          <Skeleton w="w-1/3" />
        </div>
      ))}
    </div>
  );
}

function EmptyState({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      <Link2 size={32} className="text-text-4" />
      <p className="text-md font-medium text-text">{title}</p>
      <p className="text-sm text-text-3 max-w-md">{hint}</p>
    </div>
  );
}

function ErrorState() {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      <AlertTriangle size={32} className="text-danger" />
      <p className="text-md font-medium text-text">Could not load cross-repo links</p>
      <p className="text-sm text-text-3 max-w-sm">
        The daemon returned an error. Confirm the group is indexed and the
        daemon is reachable, then retry.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § One link row
// ---------------------------------------------------------------------------

function LinkRow({ link, groupId }: { link: CrossRepoLink; groupId: string }) {
  const src = splitEntity(link.source);
  const tgt = splitEntity(link.target);
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0">
        {/* Source */}
        <div className="flex items-center gap-1.5 min-w-0">
          {src.repo && <RepoChip slug={src.repo} groupId={groupId} maxLength={18} />}
          <span className="font-mono text-xs text-text-2 truncate" title={link.source}>
            {src.label}
          </span>
        </div>

        <ArrowRight size={13} className="text-text-4 shrink-0" />

        {/* Target */}
        <div className="flex items-center gap-1.5 min-w-0">
          {tgt.repo && <RepoChip slug={tgt.repo} groupId={groupId} maxLength={18} />}
          <span className="font-mono text-xs text-text truncate" title={link.target}>
            {tgt.label}
          </span>
        </div>

        <div className="ml-auto flex items-center gap-2 shrink-0">
          {link.method && (
            <span className="text-[10px] font-mono uppercase px-1.5 py-0.5 rounded bg-surface-2 text-text-3 border border-border">
              {link.method}
            </span>
          )}
          <Badge tone={kindTone(link.kind)} className="uppercase shrink-0">
            {kindLabel(link.kind)}
          </Badge>
          <ConfidenceMeter value={link.confidence} />
        </div>
      </div>
      {link.channel && (
        <p className="text-[11px] text-text-4 pl-1">
          channel: <span className="font-mono text-text-3">{link.channel}</span>
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § A repo-pair group
// ---------------------------------------------------------------------------

interface RepoPairGroup {
  key: string;
  sourceRepo: string;
  targetRepo: string;
  links: CrossRepoLink[];
}

function RepoPairSection({ group, groupId }: { group: RepoPairGroup; groupId: string }) {
  return (
    <Card>
      <CardBody className="space-y-2">
        <div className="flex items-center gap-2">
          <GitBranch size={13} className="text-text-4 shrink-0" />
          <RepoChip slug={group.sourceRepo || "(unknown)"} groupId={groupId} />
          <ArrowRight size={12} className="text-text-4 shrink-0" />
          <RepoChip slug={group.targetRepo || "(unknown)"} groupId={groupId} />
          <span className="ml-auto text-xs text-text-4 tabular-nums">
            {group.links.length} {group.links.length === 1 ? "link" : "links"}
          </span>
        </div>
        <div className="space-y-2">
          {group.links.map((l, i) => (
            <LinkRow key={`${l.source}->${l.target}:${i}`} link={l} groupId={groupId} />
          ))}
        </div>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Screen
// ---------------------------------------------------------------------------

export default function LinksScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const { data, isLoading, isError } = useGroupLinks(groupId);
  const [kindFilter, setKindFilter] = useState<string>("all");

  const links = useMemo(() => data?.links ?? [], [data]);

  // Distinct kinds for the filter strip.
  const kinds = useMemo(() => {
    const set = new Set<string>();
    for (const l of links) set.add((l.kind || "LINK").toUpperCase());
    return Array.from(set).sort();
  }, [links]);

  const filtered = useMemo(
    () =>
      kindFilter === "all"
        ? links
        : links.filter((l) => (l.kind || "LINK").toUpperCase() === kindFilter),
    [links, kindFilter],
  );

  // Group by source-repo → target-repo pair.
  const groups = useMemo<RepoPairGroup[]>(() => {
    const byPair = new Map<string, RepoPairGroup>();
    for (const l of filtered) {
      const s = splitEntity(l.source).repo;
      const t = splitEntity(l.target).repo;
      const key = `${s}=>${t}`;
      let g = byPair.get(key);
      if (!g) {
        g = { key, sourceRepo: s, targetRepo: t, links: [] };
        byPair.set(key, g);
      }
      g.links.push(l);
    }
    return Array.from(byPair.values()).sort(
      (a, b) =>
        b.links.length - a.links.length ||
        a.sourceRepo.localeCompare(b.sourceRepo) ||
        a.targetRepo.localeCompare(b.targetRepo),
    );
  }, [filtered]);

  // Distinct repos touched (for the summary).
  const repoCount = useMemo(() => {
    const set = new Set<string>();
    for (const l of links) {
      const s = splitEntity(l.source).repo;
      const t = splitEntity(l.target).repo;
      if (s) set.add(s);
      if (t) set.add(t);
    }
    return set.size;
  }, [links]);

  return (
    <div className="flex flex-col h-full bg-bg">
      <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4 space-y-4">
        {isLoading ? (
          <SkeletonRows />
        ) : isError ? (
          <ErrorState />
        ) : links.length === 0 ? (
          <EmptyState
            title="No cross-repo links resolved"
            hint="No links between repositories were resolved for this group yet. Cross-repo links appear once a frontend fetch, gRPC call, or message publish is matched to a handler/topic in another indexed repo."
          />
        ) : (
          <>
            {/* Summary */}
            <div className="flex flex-wrap gap-3">
              <SummaryStat label="Cross-repo links" value={links.length} />
              <SummaryStat label="Repos connected" value={repoCount} />
              <SummaryStat label="Repo pairs" value={
                new Set(links.map((l) => `${splitEntity(l.source).repo}=>${splitEntity(l.target).repo}`)).size
              } />
            </div>

            {/* Kind filter */}
            {kinds.length > 1 && (
              <div className="flex flex-wrap items-center gap-2">
                <Pill active={kindFilter === "all"} onClick={() => setKindFilter("all")}>
                  All
                </Pill>
                {kinds.map((k) => (
                  <Pill key={k} active={kindFilter === k} onClick={() => setKindFilter(k)}>
                    {kindLabel(k)}
                  </Pill>
                ))}
              </div>
            )}

            {/* Grouped call map */}
            {groups.length === 0 ? (
              <EmptyState
                title="Nothing matches this filter"
                hint="No cross-repo links match the selected kind filter."
              />
            ) : (
              <div className="space-y-3">
                {groups.map((g) => (
                  <RepoPairSection key={g.key} group={g} groupId={groupId} />
                ))}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function SummaryStat({ label, value }: { label: string; value: number }) {
  return (
    <Card className={cn("flex-1 min-w-[140px]")}>
      <CardBody className="py-3">
        <p className="text-2xl font-semibold tabular-nums text-text">{value}</p>
        <p className="text-xs text-text-4 mt-0.5">{label}</p>
      </CardBody>
    </Card>
  );
}
