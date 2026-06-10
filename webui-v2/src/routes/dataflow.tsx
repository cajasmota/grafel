/* ============================================================
   Data-flow & Taint view (#4265, epic #4249).

   Route: /g/:groupId/taint

   The taint analysis (internal/links/taint_flow.go) and the data-flow
   link pass (internal/links/dataflow_pass.go) had no dashboard surface.
   This screen surfaces both:

     - FINDINGS tab — ranked source→sink security findings: a tainted
       request input reaches a dangerous sink (SQL exec / shell / fs /
       template / SSRF / …) with no sanitizer on the path. Each finding
       carries a vulnerability category, an aggregated confidence, the
       resolved source + sink entity refs, and the inter-procedural path
       (expandable). Ranked by confidence descending.

     - FLOWS tab — the raw request-input → sink DATA_FLOWS_TO edges: the
       tainted request `field`, the `sink_kind`, and the hop_path the
       sniffer followed. A flow is a single observed data movement; a
       finding is a flow the taint pass judged exploitable.

   Data: GET /api/dataflow/{group} (handlers_dataflow.go → handleDataflow),
   raw-JSON DataflowReport.

   Layout mirrors Security / IaC: full-height column, a Tabs strip with
   count pills, a summary stat row, a category / sink-kind filter, and a
   ranked list with source→sink rows, sink-kind / category badges, a
   confidence pill, an expandable hop-path, and a RefLine source ref.
   Reuses Badge / Card / Pill / Tabs / Skeleton + RefLine + RepoChip.

   HONESTY: only genuinely-extracted flows/findings render. The taint pass
   drops anything below the confidence floor and never fabricates a flow it
   could not soundly follow; when neither sidecar exists the screen shows a
   clean empty state. Unresolved endpoints show their raw key tail — never
   an invented name.
   ============================================================ */

import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import {
  ShieldAlert,
  Waypoints,
  ArrowRight,
  ChevronRight,
  AlertTriangle,
  Crosshair,
  Target,
  ListFilter,
  Info,
} from "lucide-react";

import {
  Badge,
  Card,
  CardBody,
  Pill,
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  AgentUsage,
} from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
import { cn } from "@/lib/utils";
import { useDataflow } from "@/hooks/use-dataflow";
import type {
  DataflowEndpoint,
  DataflowReport,
  SecurityFindingView,
  TaintFlow,
} from "@/data/types";

type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

// ---------------------------------------------------------------------------
// § Styling helpers
// ---------------------------------------------------------------------------

/** Vulnerability category → tone + readable label. */
function categoryMeta(cat: string): { label: string; tone: Tone } {
  switch ((cat || "").toLowerCase()) {
    case "sql_injection":
      return { label: "SQL injection", tone: "danger" };
    case "command_injection":
      return { label: "Command injection", tone: "danger" };
    case "deserialization":
      return { label: "Unsafe deserialization", tone: "danger" };
    case "ssrf":
      return { label: "SSRF", tone: "warning" };
    case "path_traversal":
      return { label: "Path traversal", tone: "warning" };
    case "xss":
      return { label: "XSS", tone: "warning" };
    case "redos":
      return { label: "ReDoS", tone: "info" };
    default:
      return { label: cat || "finding", tone: "neutral" };
  }
}

/** sink_kind → tone. */
function sinkKindTone(kind: string): Tone {
  switch ((kind || "").toLowerCase()) {
    case "sql":
    case "command":
    case "shell":
    case "exec":
      return "danger";
    case "fs":
    case "path":
    case "template":
      return "warning";
    case "http":
    case "url":
      return "info";
    default:
      return "neutral";
  }
}

/** Confidence → tone (high = danger because high-confidence taint is worse). */
function confidenceTone(c: number): Tone {
  if (c >= 0.85) return "danger";
  if (c >= 0.7) return "warning";
  return "info";
}

/** Plain-language definition of a sink_kind — what kind of dangerous operation it is. */
function sinkKindDescription(kind: string): string {
  switch ((kind || "").toLowerCase()) {
    case "sql":
      return "a SQL query / database statement";
    case "db_write":
    case "db":
      return "a database write";
    case "db_read":
      return "a database read";
    case "command":
    case "shell":
    case "exec":
      return "an OS command / shell execution";
    case "fs":
    case "file":
    case "path":
      return "a filesystem path / file operation";
    case "template":
      return "a template render";
    case "http":
    case "url":
      return "an outbound HTTP request";
    case "log":
      return "a log sink";
    default:
      return kind ? `a ${kind} sink` : "a sensitive sink";
  }
}

/** Human-readable label for a sink_kind (keeps the raw token but title-cased). */
function sinkKindLabel(kind: string): string {
  return (kind || "sink").replace(/_/g, " ");
}

/** What the confidence number means, in words. */
function confidenceMeaning(c: number): string {
  const pct = Math.round(c * 100);
  if (c >= 0.85)
    return `High confidence (${pct}%): the analyzer is very sure tainted data reaches this sink along the traced path.`;
  if (c >= 0.7)
    return `Moderate confidence (${pct}%): the path is likely but involves hops or inference the analyzer is less certain about.`;
  return `Lower confidence (${pct}%): a plausible flow, but with weaker evidence on the path.`;
}

/** Plain-language one-liner describing a flow: "<source> data flows into <sink-desc> in <sink-entity>". */
function flowSentence(flow: TaintFlow): string {
  const src = flow.field
    ? `request field \`${flow.field}\``
    : flow.source?.name
      ? `data from ${flow.source.name}`
      : "tainted request data";
  const sinkDesc = sinkKindDescription(flow.sink_kind ?? "");
  const where = flow.sink?.name ? ` in ${flow.sink.name}` : "";
  return `${src} flows into ${sinkDesc}${where}.`;
}

function ConfidencePill({ value }: { value: number }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge tone={confidenceTone(value)} className="tabular-nums shrink-0 cursor-help">
          {Math.round(value * 100)}%
        </Badge>
      </TooltipTrigger>
      <TooltipContent>{confidenceMeaning(value)}</TooltipContent>
    </Tooltip>
  );
}

// ---------------------------------------------------------------------------
// § Endpoint ref (source / sink)
// ---------------------------------------------------------------------------

function EndpointRef({
  endpoint,
  groupId,
  icon,
}: {
  endpoint: DataflowEndpoint;
  groupId: string;
  icon: "source" | "sink";
}) {
  const Icon = icon === "source" ? Crosshair : Target;
  return (
    <span className="inline-flex items-center gap-1.5 min-w-0">
      <Icon size={12} className="text-text-4 shrink-0" />
      <span className="font-mono text-sm text-text truncate" title={endpoint.entity_id}>
        {endpoint.name}
      </span>
      {endpoint.primitive && (
        <span
          className="font-mono text-[10px] text-text-4 shrink-0"
          title={`taint primitive: ${endpoint.primitive}`}
        >
          {endpoint.primitive}
        </span>
      )}
      {endpoint.repo && (
        <RepoChip slug={endpoint.repo} groupId={groupId} maxLength={14} />
      )}
    </span>
  );
}

function EndpointRefLine({ endpoint }: { endpoint: DataflowEndpoint }) {
  if (!endpoint.source_file || !endpoint.repo) return null;
  return (
    <RefLine
      repo={endpoint.repo}
      file={endpoint.source_file}
      line={endpoint.line ?? 0}
      name={endpoint.name}
      className="text-[11px]"
    />
  );
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
          className="flex items-center gap-3 h-14 px-4 rounded-lg border border-border"
        >
          <Skeleton w="w-1/4" />
          <Skeleton w="w-6" h="h-2" />
          <Skeleton w="w-1/4" />
          <Skeleton w="w-10" h="h-4" />
        </div>
      ))}
    </div>
  );
}

function EmptyState({ title, hint }: { title: string; hint: string }) {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      <Waypoints size={32} className="text-text-4" />
      <p className="text-md font-medium text-text">{title}</p>
      <p className="text-sm text-text-3 max-w-md">{hint}</p>
    </div>
  );
}

function ErrorState() {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      <AlertTriangle size={32} className="text-danger" />
      <p className="text-md font-medium text-text">Could not load data-flow analysis</p>
      <p className="text-sm text-text-3 max-w-sm">
        The daemon returned an error. Confirm the group is indexed and the daemon
        is reachable, then retry.
      </p>
    </div>
  );
}

/** Inline count badge for a tab trigger. A plain <span> (never a nested button)
 *  so it doesn't disturb the trigger's baseline / active underline. */
function TabCount({ value, active }: { value: number; active?: boolean }) {
  return (
    <span
      className={cn(
        "ml-1.5 inline-flex items-center justify-center min-w-[18px] h-[18px] px-1.5",
        "rounded-full text-[11px] font-medium tabular-nums leading-none transition-colors",
        active
          ? "bg-accent-soft text-accent-strong"
          : "bg-surface-2 text-text-3",
      )}
    >
      {value}
    </span>
  );
}

function SummaryStat({ label, value }: { label: string; value: number }) {
  return (
    <Card className={cn("flex-1 min-w-[120px]")}>
      <CardBody className="py-3">
        <p className="text-2xl font-semibold tabular-nums text-text">{value}</p>
        <p className="text-xs text-text-4 mt-0.5">{label}</p>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Purpose header — explains what taint / data-flow analysis is
// ---------------------------------------------------------------------------

function DefTerm({ term, def }: { term: string; def: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-help underline decoration-dotted decoration-text-4 underline-offset-2 text-text-2">
          {term}
        </span>
      </TooltipTrigger>
      <TooltipContent>{def}</TooltipContent>
    </Tooltip>
  );
}

function PurposeHeader() {
  return (
    <Card>
      <CardBody className="py-3 flex gap-3">
        <Info size={16} className="text-text-4 shrink-0 mt-0.5" />
        <div className="space-y-1.5 min-w-0">
          <p className="text-md font-medium text-text">Taint &amp; data-flow analysis</p>
          <p className="text-sm text-text-3 leading-relaxed">
            This view traces how untrusted data (typically{" "}
            <DefTerm
              term="request input"
              def="A taint source — data entering the system from outside, e.g. an HTTP request body, query param, or header."
            />
            ) moves through the code until it reaches a{" "}
            <DefTerm
              term="sensitive sink"
              def="An operation where untrusted data is dangerous — a SQL query, OS command, filesystem path, template render, or outbound HTTP call (a DB_WRITE is one such sink kind)."
            />
            . Each path carries a{" "}
            <DefTerm
              term="confidence"
              def="How sure the analyzer is that the data actually reaches the sink along the traced path. Higher = stronger evidence."
            />{" "}
            score.
          </p>
          <p className="text-sm text-text-3 leading-relaxed">
            <span className="text-text-2 font-medium">Findings</span> are flows the
            taint pass judged risky — an unsanitized source→sink path, tagged with a{" "}
            <DefTerm
              term="vuln category"
              def="The class of vulnerability the path resembles: SQL injection, command injection, path traversal, SSRF, XSS, etc."
            />
            .{" "}
            <span className="text-text-2 font-medium">Flows</span> are every traced
            source→sink data movement, grouped by{" "}
            <DefTerm
              term="sink kind"
              def="What kind of operation the data lands in (sql, db_write, command, fs, http, template, …). Findings are the subset of flows that are exploitable."
            />
            .
          </p>
        </div>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § One finding row (source → sink, ranked, expandable path)
// ---------------------------------------------------------------------------

function FindingRow({
  finding,
  groupId,
}: {
  finding: SecurityFindingView;
  groupId: string;
}) {
  const [open, setOpen] = useState(false);
  const { label, tone } = categoryMeta(finding.category);
  const hasPath = (finding.path?.length ?? 0) > 2; // more than just [source, sink]

  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0 flex-wrap">
        <Badge tone={tone} className="shrink-0">
          {label}
        </Badge>
        <EndpointRef endpoint={finding.source} groupId={groupId} icon="source" />
        <ArrowRight size={13} className="text-text-4 shrink-0" />
        <EndpointRef endpoint={finding.sink} groupId={groupId} icon="sink" />
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {finding.hops > 0 && (
            <span
              className="text-[11px] text-text-4 tabular-nums"
              title={`${finding.hops} call hop(s) from source to sink`}
            >
              {finding.hops} hop{finding.hops === 1 ? "" : "s"}
            </span>
          )}
          <ConfidencePill value={finding.confidence} />
        </div>
      </div>

      {finding.explanation && (
        <p className="text-[12px] text-text-3 leading-snug">{finding.explanation}</p>
      )}

      {/* Source + sink refs */}
      <div className="flex flex-col gap-0.5">
        <EndpointRefLine endpoint={finding.source} />
        <EndpointRefLine endpoint={finding.sink} />
      </div>

      {/* Expandable inter-procedural path */}
      {hasPath && (
        <div>
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            className="inline-flex items-center gap-1 text-[11px] text-text-3 hover:text-text"
          >
            <ChevronRight
              size={12}
              className={cn("transition-transform", open && "rotate-90")}
            />
            {open ? "Hide" : "Show"} path ({finding.path.length} nodes)
          </button>
          {open && (
            <div className="mt-1 flex flex-wrap items-center gap-1 pl-4">
              {finding.path.map((step, i) => (
                <span key={`${step.entity_id}:${i}`} className="inline-flex items-center gap-1">
                  {i > 0 && <ArrowRight size={11} className="text-text-4" />}
                  <span
                    className="font-mono text-[11px] text-text-3"
                    title={step.entity_id}
                  >
                    {step.name}
                  </span>
                </span>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § One flow row (request-input → sink edge)
// ---------------------------------------------------------------------------

function FlowRow({ flow, groupId }: { flow: TaintFlow; groupId: string }) {
  const [open, setOpen] = useState(false);
  // hop_path is an "a -> b -> c" chain when the sniffer crossed functions.
  const steps = useMemo(
    () =>
      (flow.hop_path ?? "")
        .split(/\s*->\s*/)
        .map((s) => s.trim())
        .filter(Boolean),
    [flow.hop_path],
  );
  const hasPath = steps.length > 2; // more than just source + sink

  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0 flex-wrap">
        {flow.sink_kind && (
          <Tooltip>
            <TooltipTrigger asChild>
              <Badge
                tone={sinkKindTone(flow.sink_kind)}
                className="capitalize shrink-0 cursor-help"
              >
                {sinkKindLabel(flow.sink_kind)}
              </Badge>
            </TooltipTrigger>
            <TooltipContent>
              Sink kind — the tainted data reaches {sinkKindDescription(flow.sink_kind)}.
            </TooltipContent>
          </Tooltip>
        )}
        <EndpointRef endpoint={flow.source} groupId={groupId} icon="source" />
        {flow.field && (
          <span
            className="font-mono text-[11px] text-text-4 shrink-0"
            title={`tainted request field: ${flow.field}`}
          >
            .{flow.field}
          </span>
        )}
        <ArrowRight size={13} className="text-text-4 shrink-0" />
        <EndpointRef endpoint={flow.sink} groupId={groupId} icon="sink" />
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {(flow.hop_count ?? 0) > 0 && (
            <span
              className="text-[11px] text-text-4 tabular-nums"
              title={`${flow.hop_count} call hop(s) the data crossed from source to sink`}
            >
              {flow.hop_count} hop{flow.hop_count === 1 ? "" : "s"}
            </span>
          )}
          {flow.confidence > 0 && <ConfidencePill value={flow.confidence} />}
        </div>
      </div>

      {/* Plain-language one-liner */}
      <p className="text-[12px] text-text-3 leading-snug">{flowSentence(flow)}</p>

      {/* Source + sink refs */}
      <div className="flex flex-col gap-0.5">
        <EndpointRefLine endpoint={flow.source} />
        <EndpointRefLine endpoint={flow.sink} />
      </div>

      {/* Expandable source→sink path */}
      {hasPath ? (
        <div>
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            className="inline-flex items-center gap-1 text-[11px] text-text-3 hover:text-text"
          >
            <ChevronRight
              size={12}
              className={cn("transition-transform", open && "rotate-90")}
            />
            {open ? "Hide" : "Show"} path ({steps.length} steps)
          </button>
          {open && (
            <div className="mt-1 flex flex-wrap items-center gap-1 pl-4">
              {steps.map((step, i) => (
                <span key={`${step}:${i}`} className="inline-flex items-center gap-1">
                  {i > 0 && <ArrowRight size={11} className="text-text-4" />}
                  <span className="font-mono text-[11px] text-text-3">{step}</span>
                </span>
              ))}
            </div>
          )}
        </div>
      ) : (
        flow.hop_path && (
          <p
            className="font-mono text-[11px] text-text-4 truncate"
            title={flow.hop_path}
          >
            {flow.hop_path}
          </p>
        )
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Findings tab
// ---------------------------------------------------------------------------

function FindingsTab({
  data,
  groupId,
}: {
  data: DataflowReport;
  groupId: string;
}) {
  const [category, setCategory] = useState<string>("all");

  const categories = useMemo(
    () =>
      Object.entries(data.findings_by_category ?? {}).sort(
        (a, b) => b[1] - a[1] || a[0].localeCompare(b[0]),
      ),
    [data.findings_by_category],
  );

  const filtered = useMemo(() => {
    const findings = data.findings ?? [];
    return category === "all"
      ? findings
      : findings.filter((f) => f.category === category);
  }, [data.findings, category]);

  if (data.total_findings === 0) {
    return (
      <EmptyState
        title="No taint findings"
        hint="The taint pass found no request-input → dangerous-sink path that crosses the confidence floor for this group. Findings appear once an indexed repo has a tainted source reaching an unsanitized sink."
      />
    );
  }

  return (
    <div className="space-y-3">
      {categories.length > 1 && (
        <div className="flex flex-wrap items-center gap-2">
          <ListFilter size={13} className="text-text-4" />
          <Pill active={category === "all"} onClick={() => setCategory("all")}>
            All ({data.total_findings})
          </Pill>
          {categories.map(([cat, n]) => (
            <Pill key={cat} active={category === cat} onClick={() => setCategory(cat)}>
              {categoryMeta(cat).label} ({n})
            </Pill>
          ))}
        </div>
      )}
      <div className="space-y-2">
        {filtered.map((f) => (
          <FindingRow key={f.fingerprint} finding={f} groupId={groupId} />
        ))}
      </div>
      <p className="text-[10px] text-text-4">
        Ranked by aggregated taint confidence. The pass drops findings below the{" "}
        {Math.round(data.confidence_floor * 100)}% floor — anything shown crossed it
        and was not sanitized on the path.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Flows tab
// ---------------------------------------------------------------------------

function FlowsTab({ data, groupId }: { data: DataflowReport; groupId: string }) {
  const [sinkKind, setSinkKind] = useState<string>("all");

  const kinds = useMemo(
    () =>
      Object.entries(data.flows_by_sink_kind ?? {}).sort(
        (a, b) => b[1] - a[1] || a[0].localeCompare(b[0]),
      ),
    [data.flows_by_sink_kind],
  );

  const filtered = useMemo(() => {
    const flows = data.flows ?? [];
    return sinkKind === "all"
      ? flows
      : flows.filter((f) => f.sink_kind === sinkKind);
  }, [data.flows, sinkKind]);

  if (data.total_flows === 0) {
    return (
      <EmptyState
        title="No data flows resolved"
        hint="No request-input → sink DATA_FLOWS_TO edges were recorded for this group. They appear once the data-flow link pass soundly follows a request field into a sink."
      />
    );
  }

  return (
    <div className="space-y-3">
      {kinds.length > 1 && (
        <div className="flex flex-wrap items-center gap-2">
          <ListFilter size={13} className="text-text-4" />
          <Pill active={sinkKind === "all"} onClick={() => setSinkKind("all")}>
            All ({data.total_flows})
          </Pill>
          {kinds.map(([k, n]) => (
            <Pill key={k} active={sinkKind === k} onClick={() => setSinkKind(k)}>
              {k} ({n})
            </Pill>
          ))}
        </div>
      )}
      <div className="space-y-2">
        {filtered.map((f) => (
          <FlowRow key={f.id} flow={f} groupId={groupId} />
        ))}
      </div>
      <p className="text-[10px] text-text-4">
        Each row is one observed request-input → sink movement the sniffer followed
        (intra-function + bounded inter-procedural hops). A flow becomes a finding
        only when the taint pass judges it reaches a dangerous sink unsanitized.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Screen
// ---------------------------------------------------------------------------

export default function DataflowScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const { data, isLoading, isError } = useDataflow(groupId);
  const [tab, setTab] = useState<"findings" | "flows">("findings");

  const hasAny =
    (data?.total_findings ?? 0) > 0 || (data?.total_flows ?? 0) > 0;

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
            title="No data-flow or taint analysis available"
            hint="Neither taint findings nor request-input → sink flows were extracted for this group. They appear once an indexed repo has request handlers that reach data sinks (SQL / shell / fs / template / HTTP) — the taint and data-flow link passes then populate this view."
          />
        </div>
      ) : (
        <Tabs
          value={tab}
          onValueChange={(v) => setTab(v as typeof tab)}
          className="flex flex-col flex-1 min-h-0"
        >
          <div className="border-b border-border shrink-0 px-4">
            <TabsList className="border-0">
              <TabsTrigger value="findings" className="inline-flex items-center">
                <ShieldAlert size={14} className="mr-1.5" />
                Findings
                <TabCount value={data!.total_findings} active={tab === "findings"} />
              </TabsTrigger>
              <TabsTrigger value="flows" className="inline-flex items-center">
                <Waypoints size={14} className="mr-1.5" />
                Flows
                <TabCount value={data!.total_flows} active={tab === "flows"} />
              </TabsTrigger>
            </TabsList>
          </div>

          <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4 space-y-4">
            <PurposeHeader />

            <AgentUsage
              tool="archigraph_data_flows"
              example="An agent reviewing a PR checks if user input reaches a DB/shell sink unsanitized."
            />

            {/* Summary */}
            <div className="flex flex-wrap gap-3">
              <SummaryStat label="Findings" value={data!.total_findings} />
              <SummaryStat label="Flows" value={data!.total_flows} />
              <SummaryStat
                label="Vuln categories"
                value={Object.keys(data!.findings_by_category ?? {}).length}
              />
              <SummaryStat
                label="Sink kinds"
                value={Object.keys(data!.flows_by_sink_kind ?? {}).length}
              />
            </div>

            <TabsContent value="findings">
              <FindingsTab data={data!} groupId={groupId} />
            </TabsContent>
            <TabsContent value="flows">
              <FlowsTab data={data!} groupId={groupId} />
            </TabsContent>
          </div>
        </Tabs>
      )}
    </div>
  );
}
