/* ============================================================
   Error-flow view (#4267, epic #4249).

   Route: /g/:groupId/errorflow

   The graph models error flow via two edge kinds that both
   originate at a CALLABLE and point at a synthetic, file-agnostic
   SCOPE.ExceptionType node (Name "exception:<Type>"):

     THROWS  : raising function/method/endpoint → exception type
     CATCHES : handling function/method         → exception type

   Identical type names converge — across files AND languages — to
   ONE exception node, so a NotFound raised in services.py and
   caught in views.py share a node. The per-endpoint Paths posture
   (#4263) already shows a single endpoint's throws/catches; THIS
   screen is the GROUP-LEVEL ROLLUP, inverted around the exception
   type: for each exception, who can raise it and who handles it —
   so you can spot a type thrown in five places and caught nowhere.

   Data: GET /api/errorflow/{group} (handlers_errorflow.go →
   handleErrorFlow), raw-JSON ErrorFlowReport.

   HONESTY on "uncaught": only TYPED throws/catches are recorded
   (bare `except:` / untyped `catch(e){}` emit no edge), so CATCHES
   edges are genuinely sparse. An exception thrown but with no
   CATCHES edge anywhere in the indexed graph is flagged `uncaught`
   with reason "no_catcher_in_graph" — which may be a genuine leak
   OR a throw caught by an untyped / out-of-scope / cross-repo
   handler the indexer did not model. We render that as a cautious
   warning ("no catcher in graph"), NEVER as a proven-leak claim.
   A repo with no exception modelling shows a clean empty state.
   ============================================================ */

import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import {
  Flame,
  ShieldAlert,
  ShieldCheck,
  ArrowUpFromLine,
  ArrowDownToLine,
  ChevronRight,
  ListFilter,
  AlertTriangle,
  Bug,
} from "lucide-react";

import { Badge, Card, CardBody, Pill } from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
import { cn } from "@/lib/utils";
import { useErrorFlow } from "@/hooks/use-errorflow";
import type {
  ErrorFlowException,
  ErrorFlowReport,
  ErrorFlowSite,
} from "@/data/types";

type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

// ---------------------------------------------------------------------------
// § Styling helpers
// ---------------------------------------------------------------------------

/** A short, readable label for a graph entity Kind (SCOPE.Function → Function). */
function kindLabel(kind?: string): string {
  if (!kind) return "";
  return kind.includes(".") ? kind.split(".").pop()! : kind;
}

/** Entity-Kind → tone so endpoints vs functions vs methods read distinctly. */
function kindTone(kind?: string): Tone {
  switch (kindLabel(kind).toLowerCase()) {
    case "endpoint":
    case "controller":
    case "resolver":
      return "info";
    case "function":
    case "method":
      return "accent";
    default:
      return "neutral";
  }
}

// ---------------------------------------------------------------------------
// § Shared shells
// ---------------------------------------------------------------------------

function SkeletonRows({ n = 6 }: { n?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: n }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-3 h-16 px-4 rounded-lg border border-border"
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
      <Bug size={32} className="text-text-4" />
      <p className="text-md font-medium text-text">{title}</p>
      <p className="text-sm text-text-3 max-w-md">{hint}</p>
    </div>
  );
}

function ErrorState() {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      <AlertTriangle size={32} className="text-danger" />
      <p className="text-md font-medium text-text">Could not load error-flow map</p>
      <p className="text-sm text-text-3 max-w-sm">
        The daemon returned an error. Confirm the group is indexed and the daemon
        is reachable, then retry.
      </p>
    </div>
  );
}

function SummaryStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone?: Tone;
}) {
  return (
    <Card className={cn("flex-1 min-w-[120px]")}>
      <CardBody className="py-3">
        <p
          className={cn(
            "text-2xl font-semibold tabular-nums",
            tone === "danger" && value > 0 ? "text-danger" : "text-text",
          )}
        >
          {value}
        </p>
        <p className="text-xs text-text-4 mt-0.5">{label}</p>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Callable label + ref (thrower / catcher site)
// ---------------------------------------------------------------------------

function SiteName({ site }: { site: ErrorFlowSite }) {
  return (
    <span className="inline-flex items-center gap-1.5 min-w-0">
      <span
        className="font-mono text-sm text-text truncate"
        title={site.entity_id}
      >
        {site.name}
      </span>
      {site.kind && (
        <Badge tone={kindTone(site.kind)} className="shrink-0">
          {kindLabel(site.kind)}
        </Badge>
      )}
    </span>
  );
}

function SiteRow({
  site,
  groupId,
  direction,
}: {
  site: ErrorFlowSite;
  groupId: string;
  direction: "throw" | "catch";
}) {
  const Icon = direction === "throw" ? ArrowUpFromLine : ArrowDownToLine;
  const iconClass = direction === "throw" ? "text-danger" : "text-success";
  return (
    <div className="flex flex-col gap-0.5 pl-6 py-1">
      <div className="flex items-center gap-2 min-w-0 flex-wrap">
        <Icon size={12} className={cn("shrink-0", iconClass)} />
        <SiteName site={site} />
        {site.pattern && (
          <span
            className="font-mono text-[10px] text-text-4 shrink-0"
            title={`detector pattern: ${site.pattern}`}
          >
            {site.pattern}
          </span>
        )}
        {site.repo && (
          <RepoChip slug={site.repo} groupId={groupId} maxLength={14} />
        )}
      </div>
      {site.repo && site.source_file && (
        <RefLine
          repo={site.repo}
          file={site.source_file}
          line={site.start_line ?? 0}
          name={site.name}
          className="text-[11px]"
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § One collapsible site list (throwers or catchers)
// ---------------------------------------------------------------------------

const COLLAPSE_AT = 5;

function SiteList({
  label,
  sites,
  groupId,
  direction,
}: {
  label: string;
  sites: ErrorFlowSite[];
  groupId: string;
  direction: "throw" | "catch";
}) {
  const [open, setOpen] = useState(false);
  const many = sites.length > COLLAPSE_AT;
  const shown = many && !open ? sites.slice(0, COLLAPSE_AT) : sites;

  return (
    <div className="space-y-0.5">
      <p className="text-[11px] uppercase tracking-wide text-text-4 pl-1">
        {label} ({sites.length})
      </p>
      <div className="border-l border-border/60 ml-1">
        {shown.map((s) => (
          <SiteRow
            key={`${direction}:${s.entity_id}`}
            site={s}
            groupId={groupId}
            direction={direction}
          />
        ))}
      </div>
      {many && (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="inline-flex items-center gap-1 text-[11px] text-text-3 hover:text-text pl-6"
        >
          <ChevronRight
            size={12}
            className={cn("transition-transform", open && "rotate-90")}
          />
          {open
            ? "Show fewer"
            : `Show ${sites.length - COLLAPSE_AT} more`}
        </button>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § One exception card (type + its throwers + catchers)
// ---------------------------------------------------------------------------

function ExceptionCard({
  exc,
  groupId,
}: {
  exc: ErrorFlowException;
  groupId: string;
}) {
  return (
    <Card
      className={cn(
        exc.uncaught && "border-warning/60",
      )}
    >
      <CardBody className="py-2.5 space-y-2">
        {/* Exception header */}
        <div className="flex items-center gap-2 min-w-0 flex-wrap">
          <Flame size={14} className="text-danger shrink-0" />
          <span
            className="font-mono text-sm font-medium text-text truncate"
            title={exc.type}
          >
            {exc.type}
          </span>
          {exc.uncaught ? (
            <Badge
              tone="warning"
              className="shrink-0 inline-flex items-center gap-1"
              title="Thrown but no typed catcher was found in the indexed graph. This may be a genuine uncaught throw OR caught by an untyped / out-of-scope handler the indexer did not model."
            >
              <ShieldAlert size={11} />
              no catcher in graph
            </Badge>
          ) : (
            exc.catch_count > 0 && (
              <Badge
                tone="success"
                className="shrink-0 inline-flex items-center gap-1"
                title={`Caught by ${exc.catch_count} handler(s) in the graph.`}
              >
                <ShieldCheck size={11} />
                caught
              </Badge>
            )
          )}
          <span className="ml-auto text-[11px] text-text-4 tabular-nums shrink-0">
            {exc.throw_count} throw{exc.throw_count === 1 ? "" : "s"} ·{" "}
            {exc.catch_count} catch{exc.catch_count === 1 ? "" : "es"}
          </span>
        </div>

        {exc.throwers.length > 0 && (
          <SiteList
            label="Thrown by"
            sites={exc.throwers}
            groupId={groupId}
            direction="throw"
          />
        )}
        {exc.catchers.length > 0 && (
          <SiteList
            label="Caught by"
            sites={exc.catchers}
            groupId={groupId}
            direction="catch"
          />
        )}
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Screen body
// ---------------------------------------------------------------------------

type FilterMode = "all" | "uncaught" | "caught";

function ErrorFlowBody({
  data,
  groupId,
}: {
  data: ErrorFlowReport;
  groupId: string;
}) {
  const [filter, setFilter] = useState<FilterMode>("all");

  const filtered = useMemo(() => {
    switch (filter) {
      case "uncaught":
        return data.exceptions.filter((e) => e.uncaught);
      case "caught":
        return data.exceptions.filter((e) => !e.uncaught);
      default:
        return data.exceptions;
    }
  }, [data.exceptions, filter]);

  const caughtCount = data.total_exceptions - data.total_uncaught;

  return (
    <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4 space-y-4">
      {/* Summary */}
      <div className="flex flex-wrap gap-3">
        <SummaryStat label="Exception types" value={data.total_exceptions} />
        <SummaryStat
          label="No catcher in graph"
          value={data.total_uncaught}
          tone="danger"
        />
        <SummaryStat label="Throws" value={data.total_throws} />
        <SummaryStat label="Catches" value={data.total_catches} />
      </div>

      {/* Filter */}
      {data.total_uncaught > 0 && caughtCount > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <ListFilter size={13} className="text-text-4" />
          <Pill active={filter === "all"} onClick={() => setFilter("all")}>
            All ({data.total_exceptions})
          </Pill>
          <Pill
            active={filter === "uncaught"}
            onClick={() => setFilter("uncaught")}
          >
            No catcher ({data.total_uncaught})
          </Pill>
          <Pill active={filter === "caught"} onClick={() => setFilter("caught")}>
            Caught ({caughtCount})
          </Pill>
        </div>
      )}

      {/* Exceptions */}
      <div className="space-y-2">
        {filtered.map((exc) => (
          <ExceptionCard key={exc.type} exc={exc} groupId={groupId} />
        ))}
      </div>

      <p className="text-[10px] text-text-4">
        Each card is a genuine SCOPE.ExceptionType node: identical type names
        converge — across files and languages — to one node, so its throwers
        (THROWS) and catchers (CATCHES) form the error contract. Only TYPED
        throws/catches are recorded — bare <code>except:</code> / untyped{" "}
        <code>catch(e)</code> emit no edge. A “no catcher in graph” badge means
        no typed catcher was indexed: it may be genuinely uncaught OR caught by
        an untyped / out-of-scope / cross-repo handler — it is a cautious
        warning, not a proven leak.
      </p>
    </div>
  );
}

export default function ErrorFlowScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const { data, isLoading, isError } = useErrorFlow(groupId);

  const hasAny = (data?.total_exceptions ?? 0) > 0;

  return (
    <div className="flex flex-col h-full bg-bg">
      {isLoading ? (
        <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4">
          <SkeletonRows />
        </div>
      ) : isError ? (
        <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4">
          <ErrorState />
        </div>
      ) : !hasAny ? (
        <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4">
          <EmptyState
            title="No error-flow modelling found"
            hint="No THROWS / CATCHES edges were extracted for this group. The error-flow map appears once an indexed repo has TYPED throws/catches — only typed raises (raise NotFound / throw new ValidationError) and typed catches are recorded; bare `except:` and untyped `catch(e)` emit no edge by design."
          />
        </div>
      ) : (
        <>
          <div className="border-b border-border shrink-0 px-4 py-2.5 flex items-center gap-2">
            <Flame size={15} className="text-text-3" />
            <h2 className="text-sm font-medium text-text">Error flow</h2>
            <span className="text-[11px] text-text-4">
              exception types → who throws &amp; who catches them
            </span>
          </div>
          <ErrorFlowBody data={data!} groupId={groupId} />
        </>
      )}
    </div>
  );
}
