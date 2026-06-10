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

   Layout mirrors Security / Quality: full-height column, a screen
   description + agent-usage banner, a summary stat row, a kind filter, and
   a grouped list of source → target call edges with repo chips + a
   confidence meter. Reuses the shared primitives (Card, Badge, Pill,
   Skeleton, InsightBanner) + RepoChip.

   #4582 polish:
     • Endpoints resolve a readable name from the id tail; bare content-hash
       ids (no source-derived name) are flagged "unnamed" with the raw id in
       a tooltip instead of printing an opaque hex blob.
     • Rows are clickable — they deep-link into the graph focused on the
       target entity (?node=<id>), and the confidence meter explains what
       high/medium/low actually means.
     • When the group spans a single repo (repo pairs ≤ 1) the links are
       INTRA-repo data flows, not cross-repo, so the headings/labels adapt
       ("Data-flow links", "within <repo>") to avoid the cross-repo mislabel.
   ============================================================ */

import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { ArrowRight, GitBranch, Link2, AlertTriangle, PanelRightOpen } from "lucide-react";

import { LinkDetailPanel } from "./links-detail-panel";

import {
  Badge,
  Card,
  CardBody,
  Pill,
  useSetInsight,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@/components/ui";
import type { InsightValue } from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RepoChip } from "@/lib/repo-color";
import { useScope } from "@/lib/scope-context";
import { cn } from "@/lib/utils";
import { useGroupLinks } from "@/hooks/use-links";
import type { CrossRepoLink } from "@/data/types";

// ---------------------------------------------------------------------------
// § Entity-id parsing — "<repo>::<localId>".
//
//   The link record carries only ids (`source`/`target`), no separate
//   name/file fields, so the readable label is derived from the id tail.
//   A localId looks like one of:
//     • "pkg/Type.method:hash"  → readable symbol  ("Type.method")
//     • "GET /api/orders:hash"  → readable route   ("GET /api/orders")
//     • "00f991b585f18f21"      → a bare content hash with no name segment
//       (the entity has no source-derived name) → flagged as "unnamed".
//
//   We surface the best readable name we can, keep the raw id as a tooltip,
//   and expose `named` so the UI can mark hash-only endpoints honestly
//   instead of printing an opaque hex blob (#4582).
// ---------------------------------------------------------------------------

interface Endpoint {
  /** Repo slug (segment before "::"), or "" when unprefixed. */
  repo: string;
  /** Best readable label for the entity. */
  label: string;
  /** False when the id is a bare hash with no human-readable name segment. */
  named: boolean;
  /** Raw, fully-qualified id (kept for tooltips + graph deep-link). */
  id: string;
}

function splitEntity(id: string): Endpoint {
  const raw = id ?? "";
  const sep = raw.indexOf("::");
  const repo = sep === -1 ? "" : raw.slice(0, sep);
  const local = sep === -1 ? raw : raw.slice(sep + 2);
  const { label, named } = readableLabel(local);
  return { repo, label, named, id: raw };
}

/** A bare lowercase/upper hex blob of ≥8 chars with no other structure. */
function isBareHash(s: string): boolean {
  return /^[0-9a-f]{8,}$/i.test(s);
}

/**
 * Best-effort readable label from a local entity id, plus whether a real
 * name was found. Local ids look like "name:hash"; if the name half is itself
 * a bare hash (or the whole id is) there is no human name to show.
 */
function readableLabel(local: string): { label: string; named: boolean } {
  if (!local) return { label: "—", named: false };
  // Whole id is a bare content hash — no name segment at all.
  if (isBareHash(local)) {
    return { label: `unnamed · ${shorten(local)}`, named: false };
  }
  const parts = local.split(":");
  const last = parts[parts.length - 1] ?? local;
  // Drop a trailing disambiguation hash to recover the name part.
  const name =
    isBareHash(last) && parts.length > 1 ? parts.slice(0, -1).join(":") : local;
  if (!name || isBareHash(name)) {
    return { label: `unnamed · ${shorten(local)}`, named: false };
  }
  return { label: name, named: true };
}

/** Compact a long hex id for inline display. */
function shorten(s: string): string {
  return s.length > 10 ? `${s.slice(0, 7)}…` : s;
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

/** Plain-language meaning of a resolution-confidence band. */
function confidenceMeaning(value: number): string {
  if (value >= 0.8)
    return "High — the source call was matched to this target with strong signals (exact route/topic/type).";
  if (value >= 0.5)
    return "Medium — a probable match inferred from partial signals; verify before relying on it.";
  return "Low — a weak or heuristic match; the real target may differ.";
}

function ConfidenceMeter({ value }: { value: number | undefined }) {
  if (value == null) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="text-[10px] text-text-4 italic cursor-help">conf —</span>
        </TooltipTrigger>
        <TooltipContent>
          No resolution confidence was recorded for this link.
        </TooltipContent>
      </Tooltip>
    );
  }
  const pct = Math.max(0, Math.min(1, value)) * 100;
  const tone =
    value >= 0.8 ? "var(--success)" : value >= 0.5 ? "var(--warning)" : "var(--danger)";
  const band = value >= 0.8 ? "high" : value >= 0.5 ? "medium" : "low";
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex items-center gap-1.5 shrink-0 cursor-help">
          <span className="h-1.5 w-12 rounded-full overflow-hidden bg-surface-2 border border-border">
            <span className="block h-full" style={{ width: `${pct}%`, background: tone }} />
          </span>
          <span className="text-[10px] tabular-nums text-text-4">{pct.toFixed(0)}%</span>
        </span>
      </TooltipTrigger>
      <TooltipContent>
        <span className="block font-medium capitalize">{band} confidence ({value.toFixed(2)})</span>
        <span className="block text-text-3">{confidenceMeaning(value)}</span>
      </TooltipContent>
    </Tooltip>
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

/**
 * One endpoint of a link: repo chip + readable name, with the raw id as a
 * hover tooltip. Hash-only (unnamed) endpoints are visually de-emphasised and
 * carry an explanatory tooltip instead of an opaque hex string (#4582).
 */
function EndpointLabel({
  ep,
  groupId,
  emphasis,
}: {
  ep: Endpoint;
  groupId: string;
  emphasis: "source" | "target";
}) {
  const text = (
    <span
      className={cn(
        "font-mono text-xs truncate",
        ep.named
          ? emphasis === "target"
            ? "text-text"
            : "text-text-2"
          : "text-text-4 italic",
      )}
    >
      {ep.label}
    </span>
  );
  return (
    <span className="flex items-center gap-1.5 min-w-0">
      {ep.repo && <RepoChip slug={ep.repo} groupId={groupId} maxLength={18} />}
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="min-w-0 truncate cursor-help">{text}</span>
        </TooltipTrigger>
        <TooltipContent>
          <span className="block font-mono text-[11px] break-all">{ep.id || "—"}</span>
          {!ep.named && (
            <span className="block text-text-3 mt-0.5">
              This entity has no source-derived name; only its content-hash id
              is known. Open it in the graph to inspect its source.
            </span>
          )}
        </TooltipContent>
      </Tooltip>
    </span>
  );
}

function LinkRow({
  link,
  groupId,
  onSelect,
}: {
  link: CrossRepoLink;
  groupId: string;
  onSelect: (link: CrossRepoLink) => void;
}) {
  const src = splitEntity(link.source);
  const tgt = splitEntity(link.target);
  // The primary click now opens the data-flow detail panel (#4648) instead of
  // deep-linking into the graph; the panel keeps a secondary "open in graph"
  // for power users.
  return (
    <button
      type="button"
      onClick={() => onSelect(link)}
      title="Open the data-flow detail panel"
      className="group w-full text-left flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 hover:border-accent/40 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
    >
      <div className="flex items-center gap-2 min-w-0">
        <EndpointLabel ep={src} groupId={groupId} emphasis="source" />
        <ArrowRight size={13} className="text-text-4 shrink-0" />
        <EndpointLabel ep={tgt} groupId={groupId} emphasis="target" />

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
          <PanelRightOpen
            size={13}
            className="text-text-4 opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
          />
        </div>
      </div>
      {link.channel && (
        <p className="text-[11px] text-text-4 pl-1">
          channel: <span className="font-mono text-text-3">{link.channel}</span>
        </p>
      )}
    </button>
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

function RepoPairSection({
  group,
  groupId,
  isCrossRepo,
  onSelect,
}: {
  group: RepoPairGroup;
  groupId: string;
  isCrossRepo: boolean;
  onSelect: (link: CrossRepoLink) => void;
}) {
  // Within a single repo (source === target) it's an intra-repo data flow,
  // so collapse the duplicate repo chip into a clear "within" label (#4582).
  const sameRepo = group.sourceRepo === group.targetRepo;
  return (
    <Card>
      <CardBody className="space-y-2">
        <div className="flex items-center gap-2">
          <GitBranch size={13} className="text-text-4 shrink-0" />
          {sameRepo && !isCrossRepo ? (
            <>
              <span className="text-xs text-text-3">within</span>
              <RepoChip slug={group.sourceRepo || "(unknown)"} groupId={groupId} />
            </>
          ) : (
            <>
              <RepoChip slug={group.sourceRepo || "(unknown)"} groupId={groupId} />
              <ArrowRight size={12} className="text-text-4 shrink-0" />
              <RepoChip slug={group.targetRepo || "(unknown)"} groupId={groupId} />
            </>
          )}
          <span className="ml-auto text-xs text-text-4 tabular-nums">
            {group.links.length}{" "}
            {group.links.length === 1
              ? isCrossRepo
                ? "link"
                : "flow"
              : isCrossRepo
                ? "links"
                : "flows"}
          </span>
        </div>
        <div className="space-y-2">
          {group.links.map((l, i) => (
            <LinkRow
              key={`${l.source}->${l.target}:${i}`}
              link={l}
              groupId={groupId}
              onSelect={onSelect}
            />
          ))}
        </div>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Screen
// ---------------------------------------------------------------------------

// Screen insight (#4655) — registered with the breadcrumb Insights button via
// useSetInsight. Module-level constant for stable identity across renders.
const LINKS_INSIGHT: InsightValue = {
  storageKey: "links",
  human: (
    <>
      This view maps the resolved calls between entities — a frontend
      fetch landing on a backend endpoint, a publisher reaching a topic,
      a gRPC client reaching a service. Each row is one directed link:
      where a request originates (source) and what it reaches (target),
      with the link kind and how confident the resolver is in the match.
      When the group spans more than one repository these are cross-repo
      links; when it's a single repo they're intra-repo data flows.
      Click a row to open its data-flow detail — source, sink, the
      resolver's confidence, and a jump to the endpoint or graph.
    </>
  ),
  agent: {
    tool: "archigraph_cross_links",
    example:
      "Debugging a frontend call that hits a 404, an agent calls archigraph_cross_links to confirm which backend endpoint the fetch actually resolves to across repos — and spots that the client points at /v1/users while the server only serves /v2/users.",
  },
};

export default function LinksScreen() {
  useSetInsight(LINKS_INSIGHT);
  const { groupId = "" } = useParams<{ groupId: string }>();
  const { data, isLoading, isError } = useGroupLinks(groupId);
  const { matchesScope } = useScope();
  const [kindFilter, setKindFilter] = useState<string>("all");
  // Data-flow detail panel selection (#4648). Holds the clicked link; `open`
  // drives the drawer so closing keeps the last link mounted for the exit
  // animation without flashing empty.
  const [selected, setSelected] = useState<CrossRepoLink | null>(null);
  const [panelOpen, setPanelOpen] = useState(false);

  // #4637: scope cross-repo links to the active repo/module. A link is kept
  // when EITHER endpoint lives in the scoped repo, so a repo's inbound and
  // outbound edges both remain visible when you focus it.
  const links = useMemo(
    () =>
      (data?.links ?? []).filter((l) => {
        const s = splitEntity(l.source).repo;
        const t = splitEntity(l.target).repo;
        return matchesScope(s) || matchesScope(t);
      }),
    [data, matchesScope],
  );

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

  // Distinct source→target repo pairs. When ≤ 1 every link lives inside a
  // single repo, so the data is INTRA-repo data flow, not cross-repo (#4582).
  const repoPairCount = useMemo(
    () =>
      new Set(
        links.map(
          (l) => `${splitEntity(l.source).repo}=>${splitEntity(l.target).repo}`,
        ),
      ).size,
    [links],
  );

  // True cross-repo only when more than one repo is connected by ≥1 pair.
  const isCrossRepo = repoCount > 1 && repoPairCount > 1;

  // Labels adapt to the cross- vs intra-repo reality.
  const linksLabel = isCrossRepo ? "Cross-repo links" : "Data-flow links";
  const pairsLabel = isCrossRepo ? "Repo pairs" : "Same-repo flows";

  return (
    <div className="flex flex-col h-full bg-bg">
      <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4 space-y-4">
        
        {isLoading ? (
          <SkeletonRows />
        ) : isError ? (
          <ErrorState />
        ) : links.length === 0 ? (
          <EmptyState
            title="No links resolved"
            hint="No data-flow links were resolved for this group yet. Links appear once a frontend fetch, gRPC call, or message publish is matched to a handler/topic — within a repo or across repos in the same group."
          />
        ) : (
          <>
            {/* Summary */}
            <div className="flex flex-wrap gap-3">
              <SummaryStat label={linksLabel} value={links.length} />
              <SummaryStat label="Repos connected" value={repoCount} />
              <SummaryStat label={pairsLabel} value={repoPairCount} />
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
                  <RepoPairSection
                    key={g.key}
                    group={g}
                    groupId={groupId}
                    isCrossRepo={isCrossRepo}
                    onSelect={(l) => {
                      setSelected(l);
                      setPanelOpen(true);
                    }}
                  />
                ))}
              </div>
            )}
          </>
        )}
      </div>

      <LinkDetailPanel
        link={selected}
        groupId={groupId}
        open={panelOpen}
        onOpenChange={setPanelOpen}
      />
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
