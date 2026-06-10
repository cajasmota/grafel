/* ============================================================
   links-detail-panel.tsx — Data-flow detail side panel (#4648).

   Primary click on a Links row now opens THIS panel instead of deep-linking
   into the graph (low value). It explains a single resolved data-flow:

     Source  → Sink
       resolved name · file:line · source-peek    (the originating call)
       the `this.service.create@N` style call · file:line · source-peek,
       with a sink-kind label (DB-write, HTTP-call, message-publish, …).

     Why / confidence  — the % the resolver explains as "confidence this flow
       is real" (NOT a risk score).

     Actions
       • Open endpoint — when either end maps to an http_endpoint_definition,
         deep-link to its Paths detail (resolved by matching the route name
         against the loaded Paths list to recover its path_hash).
       • View source — both ends, via the shared source-peek (useSourcePeek).
       • Open in graph — secondary, kept for power users.

   Payload (#4596 enrichment on CrossRepoLink): source/target name +
   qualified_name + file + line, plus kind / confidence / method / channel.
   The hop-path between source and sink is NOT in the payload, so per the
   ticket we render source / sink / source-peek / endpoint-link now and note
   the intermediate hops as a follow-up (see HOPS_FOLLOWUP below).
   ============================================================ */

import { useMemo } from "react";
import { Link } from "react-router-dom";
import {
  ArrowDown,
  ExternalLink,
  FileCode2,
  GitBranch,
  Network,
} from "lucide-react";

import {
  Badge,
  Button,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui";
import {
  Dialog,
  DrawerContent,
  DialogTitle,
  DialogClose,
} from "@/components/ui/dialog";
import { RepoChip } from "@/lib/repo-color";
import { cn } from "@/lib/utils";
import { useSourcePeek } from "@/components/SourcePeek";
import { usePaths } from "@/hooks/use-paths";
import type { CrossRepoLink, PathBackend, PathRoute } from "@/data/types";

// The intermediate hop-path (source-call → … → sink) is not carried on the
// link payload today; tracked as a follow-up so the panel can render the full
// chain (a ShapeTree-style trace) once the backend emits it.
const HOPS_FOLLOWUP =
  "Intermediate hops aren't in the link payload yet — only the resolved source and sink are shown (follow-up).";

// ---------------------------------------------------------------------------
// § Endpoint-id parsing (shared shape with links.tsx).
// ---------------------------------------------------------------------------

function repoOf(id: string): string {
  const sep = (id ?? "").indexOf("::");
  return sep === -1 ? "" : id.slice(0, sep);
}

function localOf(id: string): string {
  const sep = (id ?? "").indexOf("::");
  return sep === -1 ? (id ?? "") : id.slice(sep + 2);
}

function isBareHash(s: string): boolean {
  return /^[0-9a-f]{8,}$/i.test(s);
}

/** Best readable name for an endpoint, preferring the #4596 enrichment. */
function endpointName(
  enrichedName: string | undefined,
  rawId: string,
): { label: string; named: boolean } {
  if (enrichedName && enrichedName.trim()) {
    return { label: enrichedName.trim(), named: true };
  }
  const local = localOf(rawId);
  if (!local) return { label: "—", named: false };
  if (isBareHash(local)) return { label: "unnamed", named: false };
  const parts = local.split(":");
  const last = parts[parts.length - 1] ?? local;
  const name =
    isBareHash(last) && parts.length > 1 ? parts.slice(0, -1).join(":") : local;
  if (!name || isBareHash(name)) return { label: "unnamed", named: false };
  return { label: name, named: true };
}

// ---------------------------------------------------------------------------
// § Sink-kind label — what the target end actually IS (DB-write, etc.).
// ---------------------------------------------------------------------------

interface SinkKind {
  label: string;
  tone: "accent" | "info" | "warning" | "neutral";
}

function sinkKind(kind: string): SinkKind {
  const k = (kind || "").toUpperCase();
  if (k.includes("HTTP") || k.includes("FETCH") || k.includes("REST"))
    return { label: "HTTP call", tone: "accent" };
  if (k.includes("GRPC")) return { label: "gRPC call", tone: "info" };
  if (k.includes("PUBLISH")) return { label: "message publish", tone: "warning" };
  if (k.includes("SUBSCRIBE") || k.includes("CONSUME"))
    return { label: "message consume", tone: "warning" };
  if (k.includes("TOPIC") || k.includes("QUEUE"))
    return { label: "queue / topic", tone: "warning" };
  if (k.includes("DB") || k.includes("QUERY") || k.includes("WRITE") || k.includes("SQL"))
    return { label: "DB write", tone: "warning" };
  return { label: (kind || "link").replace(/_/g, " "), tone: "neutral" };
}

// ---------------------------------------------------------------------------
// § Confidence wording — "% explained: how sure the resolver is this flow is
//   real". Explicitly NOT a risk/severity score.
// ---------------------------------------------------------------------------

function confidenceBand(value: number): { band: string; tone: string } {
  if (value >= 0.8) return { band: "high", tone: "var(--success)" };
  if (value >= 0.5) return { band: "medium", tone: "var(--warning)" };
  return { band: "low", tone: "var(--danger)" };
}

function confidenceMeaning(value: number): string {
  if (value >= 0.8)
    return "Strong signals (exact route / topic / type) matched this call to its sink — this flow is very likely real.";
  if (value >= 0.5)
    return "A probable match inferred from partial signals — verify before relying on it.";
  return "A weak or heuristic match — the real sink may differ.";
}

// ---------------------------------------------------------------------------
// § Endpoint resolution — map an end of the link to a Paths detail route.
//
//   The link payload has no path_hash, so we recover it by matching the
//   resolved route name (e.g. "GET /api/orders") against the loaded Paths
//   list. Only HTTP-kind links can map to an http_endpoint_definition.
// ---------------------------------------------------------------------------

/** Flatten every PathRoute across all backends/controller groups once. */
function allRoutes(backends: PathBackend[] | undefined): PathRoute[] {
  if (!backends) return [];
  const out: PathRoute[] = [];
  for (const b of backends) for (const g of b.groups) out.push(...g.routes);
  return out;
}

/** Normalise a route string for tolerant matching ("GET /api/x" → "get /api/x"). */
function normRoute(s: string): string {
  return (s || "").trim().toLowerCase().replace(/\s+/g, " ");
}

/**
 * Resolve the Paths deep-link for an endpoint end of the link, or null when it
 * doesn't map to a known http_endpoint_definition. The route name may carry a
 * leading verb ("GET /api/orders") or be a bare path; we try both.
 */
function resolveEndpointHref(
  groupId: string,
  name: string | undefined,
  method: string | undefined,
  routes: PathRoute[],
): string | null {
  if (!name || routes.length === 0) return null;
  const want = normRoute(name);
  // The route name might be "<VERB> <path>"; split a leading verb off if any.
  const verbMatch = name.match(/^([A-Z]+)\s+(\/.*)$/);
  const wantPath = verbMatch ? normRoute(verbMatch[2]) : want;
  const wantVerb = (verbMatch?.[1] ?? method ?? "").toUpperCase();

  let hit: PathRoute | undefined;
  for (const r of routes) {
    const rp = normRoute(r.path);
    if (rp === want || rp === wantPath || normRoute(`${r.verbs[0] ?? ""} ${r.path}`) === want) {
      hit = r;
      break;
    }
  }
  if (!hit) return null;

  const params = new URLSearchParams();
  params.set("path", hit.path_hash);
  const verb =
    (wantVerb && hit.verbs.includes(wantVerb as PathRoute["verbs"][number])
      ? wantVerb
      : hit.verbs[0]) ?? "";
  if (verb) params.set("verb", verb);
  return `/g/${groupId}/paths?${params.toString()}`;
}

// ---------------------------------------------------------------------------
// § Endpoint card (one end of the flow).
// ---------------------------------------------------------------------------

function EndpointCard({
  role,
  repo,
  name,
  named,
  file,
  line,
  groupId,
  badge,
}: {
  role: "source" | "sink";
  repo: string;
  name: string;
  named: boolean;
  file?: string;
  line?: number;
  groupId: string;
  badge?: React.ReactNode;
}) {
  const { openSourcePeek } = useSourcePeek();
  const ref = file ? `${file}:${line ?? 0}` : "";

  return (
    <div className="rounded-lg border border-border bg-surface p-3 space-y-2">
      <div className="flex items-center gap-2">
        <span className="text-[10px] font-semibold uppercase tracking-wide text-text-4">
          {role === "source" ? "Source" : "Sink"}
        </span>
        {repo && <RepoChip slug={repo} groupId={groupId} maxLength={20} />}
        {badge && <span className="ml-auto">{badge}</span>}
      </div>
      <p
        className={cn(
          "font-mono text-sm break-all",
          named ? "text-text" : "text-text-4 italic",
        )}
        title={name}
      >
        {name}
      </p>
      {ref ? (
        <button
          type="button"
          onClick={() => openSourcePeek({ groupId, file: file!, line: line ?? 0, repo })}
          className="inline-flex items-center gap-1.5 font-mono text-[11px] tabular-nums text-accent hover:underline"
          title={`View source — ${ref}`}
        >
          <FileCode2 size={12} className="shrink-0" />
          <span className="truncate">{ref}</span>
        </button>
      ) : (
        <p className="text-[11px] text-text-4 italic">
          No resolved source location for this end.
        </p>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § The panel.
// ---------------------------------------------------------------------------

export function LinkDetailPanel({
  link,
  groupId,
  open,
  onOpenChange,
}: {
  link: CrossRepoLink | null;
  groupId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  // Paths list — used to resolve either end to its http_endpoint_definition
  // detail. Cheap (cached, shared with the Paths screen) and only meaningful
  // for HTTP-kind links, but loading it unconditionally keeps hooks stable.
  const { data: pathsData } = usePaths(open ? groupId : "");
  const routes = useMemo(() => allRoutes(pathsData?.backends), [pathsData]);

  const view = useMemo(() => {
    if (!link) return null;
    const src = endpointName(link.source_name, link.source);
    const tgt = endpointName(link.target_name, link.target);
    const srcRepo = repoOf(link.source);
    const tgtRepo = repoOf(link.target);
    const sk = sinkKind(link.kind);

    const sourceEndpointHref = resolveEndpointHref(
      groupId,
      link.source_name ?? localOf(link.source),
      link.method,
      routes,
    );
    const sinkEndpointHref = resolveEndpointHref(
      groupId,
      link.target_name ?? localOf(link.target),
      link.method,
      routes,
    );
    return {
      src,
      tgt,
      srcRepo,
      tgtRepo,
      sk,
      sourceEndpointHref,
      sinkEndpointHref,
    };
  }, [link, groupId, routes]);

  if (!link || !view) return null;

  const conf = link.confidence;
  const graphHref = `/g/${groupId}/graph?node=${encodeURIComponent(
    link.target || link.source,
  )}`;

  // Prefer the sink endpoint for "Open endpoint"; fall back to the source.
  const endpointHref = view.sinkEndpointHref ?? view.sourceEndpointHref;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DrawerContent
        className="w-[440px] max-w-[92vw] flex flex-col p-0"
        aria-describedby={undefined}
        onOpenAutoFocus={(e) => e.preventDefault()}
      >
        {/* Header */}
        <div className="flex items-start gap-2 px-4 py-3 border-b border-border">
          <Network size={16} className="text-accent mt-0.5 shrink-0" />
          <div className="min-w-0 flex-1">
            <DialogTitle className="text-md">Data-flow detail</DialogTitle>
            <p className="text-xs text-text-3 mt-0.5">
              How this call resolves from its source to its sink.
            </p>
          </div>
          <DialogClose className="text-text-3 hover:text-text rounded-sm shrink-0 mt-0.5">
            <span className="text-xs">Esc</span>
            <span className="sr-only">Close</span>
          </DialogClose>
        </div>

        {/* Body */}
        <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4 space-y-4">
          {/* Source → Sink */}
          <div className="space-y-2">
            <EndpointCard
              role="source"
              repo={view.srcRepo}
              name={view.src.label}
              named={view.src.named}
              file={link.source_file}
              line={link.source_line}
              groupId={groupId}
            />
            <div className="flex items-center justify-center">
              <ArrowDown size={16} className="text-text-4" />
            </div>
            <EndpointCard
              role="sink"
              repo={view.tgtRepo}
              name={view.tgt.label}
              named={view.tgt.named}
              file={link.target_file}
              line={link.target_line}
              groupId={groupId}
              badge={
                <Badge tone={view.sk.tone} className="uppercase">
                  {view.sk.label}
                </Badge>
              }
            />
          </div>

          {/* Transport detail (method / channel) */}
          {(link.method || link.channel) && (
            <div className="flex flex-wrap items-center gap-2 text-[11px] text-text-3">
              {link.method && (
                <span className="font-mono uppercase px-1.5 py-0.5 rounded bg-surface-2 border border-border">
                  {link.method}
                </span>
              )}
              {link.channel && (
                <span>
                  channel:{" "}
                  <span className="font-mono text-text-2">{link.channel}</span>
                </span>
              )}
            </div>
          )}

          {/* Why / confidence */}
          <div className="rounded-lg border border-border bg-surface-2/40 p-3 space-y-2">
            <p className="text-[10px] font-semibold uppercase tracking-wide text-text-4">
              Why · confidence
            </p>
            {conf == null ? (
              <p className="text-xs text-text-3">
                No resolution confidence was recorded for this flow.
              </p>
            ) : (
              <>
                <div className="flex items-center gap-2">
                  <span className="h-2 w-24 rounded-full overflow-hidden bg-surface-2 border border-border">
                    <span
                      className="block h-full"
                      style={{
                        width: `${Math.max(0, Math.min(1, conf)) * 100}%`,
                        background: confidenceBand(conf).tone,
                      }}
                    />
                  </span>
                  <span className="text-sm tabular-nums font-medium text-text">
                    {(Math.max(0, Math.min(1, conf)) * 100).toFixed(0)}%
                  </span>
                  <span className="text-xs capitalize text-text-3">
                    {confidenceBand(conf).band}
                  </span>
                </div>
                <p className="text-xs text-text-3">{confidenceMeaning(conf)}</p>
              </>
            )}
            <p className="text-[11px] text-text-4">
              This is the resolver's confidence that the flow is real — not a
              risk or severity score.
            </p>
          </div>

          {/* Hops follow-up note */}
          <div className="flex items-start gap-2 text-[11px] text-text-4">
            <GitBranch size={12} className="mt-0.5 shrink-0" />
            <p>{HOPS_FOLLOWUP}</p>
          </div>
        </div>

        {/* Actions footer */}
        <div className="border-t border-border px-4 py-3 flex flex-wrap items-center gap-2">
          {endpointHref ? (
            <Button asChild variant="primary" size="sm">
              <Link to={endpointHref}>
                <ExternalLink size={13} />
                Open endpoint
              </Link>
            </Button>
          ) : (
            <Tooltip>
              <TooltipTrigger asChild>
                <span>
                  <Button variant="primary" size="sm" disabled>
                    <ExternalLink size={13} />
                    Open endpoint
                  </Button>
                </span>
              </TooltipTrigger>
              <TooltipContent>
                Neither end maps to a known HTTP endpoint definition.
              </TooltipContent>
            </Tooltip>
          )}

          <Button asChild variant="secondary" size="sm" className="ml-auto">
            <Link to={graphHref}>
              <Network size={13} />
              Open in graph
            </Link>
          </Button>
        </div>
      </DrawerContent>
    </Dialog>
  );
}
