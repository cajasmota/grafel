/* ============================================================
   Quality — Coverage / Dependency-hygiene / Anti-patterns / God-nodes /
   Trends dashboard.

   Route: /g/:groupId/quality
   Issue: #4251 | Epic: #4249

   Surfaces capability data the backend already serves but no screen
   previously rendered. Five v1 routes, all raw-JSON static graph reads
   (trends additionally reads health-history.jsonl) — NO runtime metrics:

     GET /api/quality/coverage/{group}        (handlers_coverage.go)
     GET /api/dependencies/{group}            (handlers_dependencies.go)
     GET /api/quality/anti-patterns/{group}   (handlers_nplus1.go)
     GET /api/groups/{group}/god-nodes        (handlers_graph.go)
     GET /api/quality/trends/{group}          (handlers_quality_trends.go)

   Layout mirrors Security / Topology: full-height column, a Tabs strip
   with Pill counts, and per-tab workspaces with consistent loading /
   empty / error states. Reuses the shared primitives layer (Badge, Card,
   Pill, Tabs, Skeleton) + RefLine/RepoChip rather than inventing new
   components.
   ============================================================ */

import { useState } from "react";
import { useParams } from "react-router-dom";
import {
  GaugeCircle,
  Boxes,
  Repeat,
  Crown,
  TrendingUp,
  AlertTriangle,
  CheckCircle2,
  ArrowDownRight,
  ArrowUpRight,
  Minus,
} from "lucide-react";

import {
  Badge,
  Card,
  CardHeader,
  CardTitle,
  CardBody,
  Pill,
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
import { cn } from "@/lib/utils";
import {
  useQualityCoverage,
  useDependencies,
  useAntiPatterns,
  useGodNodes,
  useQualityTrends,
} from "@/hooks/use-quality";
import type {
  UncoveredEntity,
  DirCoverage,
  PackageEntry,
  RepoDepSummary,
  NPlusOneFinding,
  GodNode,
  MetricTrend,
} from "@/data/types";

// ---------------------------------------------------------------------------
// § Shared shells (loading / empty / error) — mirror Security idioms
// ---------------------------------------------------------------------------

function SkeletonRows({ n = 5 }: { n?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: n }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-3 h-14 px-4 rounded-lg border border-border"
        >
          <Skeleton w="w-6" h="h-6" className="rounded-full shrink-0" />
          <div className="flex-1 space-y-2">
            <Skeleton w="w-1/3" />
            <Skeleton w="w-1/4" h="h-2" />
          </div>
        </div>
      ))}
    </div>
  );
}

function EmptyState({
  icon,
  title,
  hint,
}: {
  icon: React.ReactNode;
  title: string;
  hint: string;
}) {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      {icon}
      <p className="text-md font-medium text-text">{title}</p>
      <p className="text-sm text-text-3 max-w-sm">{hint}</p>
    </div>
  );
}

function ErrorState({ what }: { what: string }) {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      <AlertTriangle size={32} className="text-danger" />
      <p className="text-md font-medium text-text">Could not load {what}</p>
      <p className="text-sm text-text-3 max-w-sm">
        The daemon returned an error. Confirm the group is indexed and the
        daemon is reachable, then retry.
      </p>
    </div>
  );
}

function CountStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number | string;
  tone?: "danger" | "warning" | "info" | "success";
}) {
  const color =
    tone === "danger"
      ? "text-danger"
      : tone === "warning"
        ? "text-warning"
        : tone === "info"
          ? "text-info"
          : tone === "success"
            ? "text-success"
            : "text-text";
  return (
    <Card className="flex-1 min-w-[120px]">
      <CardBody className="py-3">
        <p className={cn("text-2xl font-semibold tabular-nums", color)}>{value}</p>
        <p className="text-xs text-text-4 mt-0.5">{label}</p>
      </CardBody>
    </Card>
  );
}

/** Best-effort short name from a graph entity id ("repo::local:hash"). */
function shortMember(id: string): string {
  const tail = (id ?? "").split("::").pop() ?? id;
  return tail.split(":").pop() || id;
}

// ---------------------------------------------------------------------------
// § Severity helpers (coverage uses "high" | "medium" | "low" strings)
// ---------------------------------------------------------------------------

const COV_SEVERITY_TONE: Record<string, "danger" | "warning" | "info"> = {
  high: "danger",
  medium: "warning",
  low: "info",
};

function CovSeverityBadge({ severity }: { severity: string }) {
  return (
    <Badge tone={COV_SEVERITY_TONE[severity] ?? "neutral"} className="capitalize shrink-0">
      {severity}
    </Badge>
  );
}

// ---------------------------------------------------------------------------
// § Coverage tab
// ---------------------------------------------------------------------------

function CoverageBar({ pct }: { pct: number }) {
  const tone =
    pct >= 80 ? "bg-success" : pct >= 50 ? "bg-warning" : "bg-danger";
  return (
    <div
      className="h-2 w-full rounded-full overflow-hidden bg-surface-2 border border-border"
      role="img"
      aria-label={`${pct.toFixed(0)}% covered`}
    >
      <div className={cn("h-full transition-all", tone)} style={{ width: `${Math.min(pct, 100)}%` }} />
    </div>
  );
}

function CoverageGauge({
  covered,
  total,
  pct,
  totalTests,
}: {
  covered: number;
  total: number;
  pct: number;
  totalTests: number;
}) {
  return (
    <Card>
      <CardHeader className="flex items-center justify-between">
        <CardTitle>Test coverage</CardTitle>
        <span className="text-2xl font-semibold tabular-nums text-text">
          {pct.toFixed(1)}%
        </span>
      </CardHeader>
      <CardBody className="space-y-3">
        <CoverageBar pct={pct} />
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-md">
          <span className="flex items-center gap-1.5 text-text-2">
            <CheckCircle2 size={13} className="text-success" />
            {covered} covered
          </span>
          <span className="text-text-2">{total - covered} uncovered</span>
          <span className="text-text-4">· {total} production entities</span>
          <span className="text-text-4">· {totalTests} tests</span>
        </div>
      </CardBody>
    </Card>
  );
}

function DirRow({ d }: { d: DirCoverage }) {
  return (
    <div className="flex items-center gap-3 px-3 py-2 rounded-lg border border-border bg-surface">
      <span className="font-mono text-xs text-text-2 truncate flex-1 min-w-0" title={d.dir}>
        {d.dir || "(root)"}
      </span>
      <div className="w-32 shrink-0">
        <CoverageBar pct={d.coverage_pct} />
      </div>
      <span className="text-xs tabular-nums text-text-3 w-16 text-right shrink-0">
        {d.coverage_pct.toFixed(0)}%
      </span>
      <span className="text-[11px] tabular-nums text-text-4 w-20 text-right shrink-0">
        {d.covered}/{d.total}
      </span>
    </div>
  );
}

function UncoveredRow({ u, repo }: { u: UncoveredEntity; repo: string }) {
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0">
        <span className="font-mono text-sm text-text truncate" title={u.name}>
          {u.name}
        </span>
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          <Badge tone="neutral" className="shrink-0">
            {u.kind}
          </Badge>
          <CovSeverityBadge severity={u.severity} />
        </div>
      </div>
      <div className="flex items-center gap-2 min-w-0 -mx-1">
        <RepoChip slug={repo} className="text-[10px] shrink-0" />
        {u.source_file ? (
          <RefLine
            repo={repo}
            file={u.source_file}
            line={u.start_line ?? 0}
            name={u.language ?? ""}
            className="text-[11px] py-0.5 px-1 min-w-0"
          />
        ) : (
          <span className="font-mono text-xs text-text-3 truncate">{u.language}</span>
        )}
      </div>
    </div>
  );
}

function CoverageTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useQualityCoverage(groupId);
  const [severity, setSeverity] = useState<"all" | "high" | "medium" | "low">("all");

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="coverage report" />;
  if (!data || data.total_production === 0) {
    return (
      <EmptyState
        icon={<GaugeCircle size={32} className="text-text-4" />}
        title="No production entities indexed"
        hint="No production entities were found for this group, so there is no coverage to report yet."
      />
    );
  }

  const uncovered =
    severity === "all"
      ? data.uncovered_entities
      : data.uncovered_entities.filter((u) => u.severity === severity);

  return (
    <div className="space-y-4">
      <CoverageGauge
        covered={data.covered_production}
        total={data.total_production}
        pct={data.coverage_pct}
        totalTests={data.total_tests}
      />

      {data.by_directory.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>By directory</CardTitle>
          </CardHeader>
          <CardBody className="space-y-1.5">
            {data.by_directory.map((d) => (
              <DirRow key={d.dir} d={d} />
            ))}
          </CardBody>
        </Card>
      )}

      <div className="flex items-center justify-between gap-3">
        <h3 className="text-md font-medium text-text-2">
          Uncovered entities
          <span className="ml-2 text-text-4 tabular-nums">{uncovered.length}</span>
        </h3>
        <div className="flex items-center gap-2">
          {(["all", "high", "medium", "low"] as const).map((s) => (
            <Pill key={s} active={severity === s} onClick={() => setSeverity(s)}>
              {s === "all" ? "All" : s[0].toUpperCase() + s.slice(1)}
            </Pill>
          ))}
        </div>
      </div>

      {uncovered.length === 0 ? (
        <EmptyState
          icon={<CheckCircle2 size={28} className="text-success" />}
          title="Nothing at this severity"
          hint="No uncovered entities match the selected severity filter."
        />
      ) : (
        <div className="space-y-2">
          {uncovered.map((u) => (
            <UncoveredRow key={u.entity_id} u={u} repo={data.group} />
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Dependency-hygiene tab
// ---------------------------------------------------------------------------

const DEP_STATUS_TONE: Record<PackageEntry["status"], "success" | "warning" | "danger"> = {
  used: "success",
  unused: "warning",
  phantom: "danger",
};

function PackageRow({ p }: { p: PackageEntry }) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-border bg-surface">
      <span className="font-mono text-sm text-text truncate min-w-0 flex-1" title={p.name}>
        {p.name}
      </span>
      {p.version && (
        <span className="text-[11px] font-mono text-text-4 shrink-0">{p.version}</span>
      )}
      <Badge tone="neutral" className="shrink-0">
        {p.dependency_kind}
      </Badge>
      <Badge tone={DEP_STATUS_TONE[p.status]} className="shrink-0">
        {p.status}
      </Badge>
    </div>
  );
}

function RepoDepCard({
  slug,
  rep,
  statusFilter,
}: {
  slug: string;
  rep: RepoDepSummary;
  statusFilter: "all" | PackageEntry["status"];
}) {
  const pkgs =
    statusFilter === "all"
      ? rep.packages
      : rep.packages.filter((p) => p.status === statusFilter);
  return (
    <Card>
      <CardHeader className="flex items-center justify-between gap-2">
        <CardTitle className="flex items-center gap-2">
          <RepoChip slug={slug} className="text-[10px]" />
          <span className="text-text-4 text-xs font-normal">{rep.package_manager}</span>
        </CardTitle>
        <div className="flex items-center gap-1.5 shrink-0">
          <Badge tone="neutral">{rep.declared} declared</Badge>
          <Badge tone="success">{rep.used} used</Badge>
          {rep.unused > 0 && <Badge tone="warning">{rep.unused} unused</Badge>}
          {rep.phantom > 0 && <Badge tone="danger">{rep.phantom} phantom</Badge>}
        </div>
      </CardHeader>
      <CardBody className="space-y-1.5">
        {pkgs.length === 0 ? (
          <p className="text-sm text-text-4 py-2">No packages match this filter.</p>
        ) : (
          pkgs.map((p) => <PackageRow key={`${p.package_manager}:${p.name}`} p={p} />)
        )}
      </CardBody>
    </Card>
  );
}

function DependenciesTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useDependencies(groupId);
  const [statusFilter, setStatusFilter] = useState<"all" | PackageEntry["status"]>("all");

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="dependency hygiene" />;
  const repoSlugs = data ? Object.keys(data.by_repo).sort() : [];
  if (!data || data.summary.declared === 0) {
    return (
      <EmptyState
        icon={<Boxes size={32} className="text-text-4" />}
        title="No declared dependencies"
        hint="No package manifests were detected for this group, so there is no dependency hygiene to report."
      />
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-3">
        <CountStat label="Declared" value={data.summary.declared} />
        <CountStat label="Used" value={data.summary.used} tone="success" />
        <CountStat label="Unused" value={data.summary.unused} tone="warning" />
        <CountStat label="Phantom" value={data.summary.phantom} tone="danger" />
      </div>

      <div className="flex items-center justify-between gap-3">
        <h3 className="text-md font-medium text-text-2">
          By repository
          <span className="ml-2 text-text-4 tabular-nums">{repoSlugs.length}</span>
        </h3>
        <div className="flex items-center gap-2">
          {(["all", "phantom", "unused", "used"] as const).map((s) => (
            <Pill
              key={s}
              active={statusFilter === s}
              onClick={() => setStatusFilter(s)}
            >
              {s === "all" ? "All" : s[0].toUpperCase() + s.slice(1)}
            </Pill>
          ))}
        </div>
      </div>

      <div className="space-y-3">
        {repoSlugs.map((slug) => (
          <RepoDepCard
            key={slug}
            slug={slug}
            rep={data.by_repo[slug]}
            statusFilter={statusFilter}
          />
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Anti-patterns (N+1) tab
// ---------------------------------------------------------------------------

function NPlusOneRow({ f }: { f: NPlusOneFinding }) {
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0">
        <Repeat size={13} className="text-warning shrink-0" />
        <span className="font-mono text-sm text-text truncate" title={f.query_name}>
          {f.query_name}
        </span>
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {f.orm && (
            <Badge tone="accent" className="shrink-0">
              {f.orm}
            </Badge>
          )}
          {f.language && (
            <Badge tone="neutral" className="shrink-0">
              {f.language}
            </Badge>
          )}
        </div>
      </div>
      <p className="text-[11px] text-text-3">
        in loop within <span className="font-mono text-text-2">{f.caller_name}</span>
      </p>
      {f.query_file && (
        <span className="font-mono text-[11px] text-text-4 truncate" title={`${f.query_file}:${f.query_line}`}>
          {f.query_file}:{f.query_line}
        </span>
      )}
      {f.suggestion && (
        <p className="text-[11px] text-text-4">suggestion: {f.suggestion}</p>
      )}
    </div>
  );
}

function AntiPatternsTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useAntiPatterns(groupId);

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="anti-patterns" />;
  if (!data || data.total_findings === 0) {
    return (
      <EmptyState
        icon={<CheckCircle2 size={32} className="text-success" />}
        title="No N+1 query anti-patterns"
        hint="No ORM query calls inside loops were detected across the indexed repos."
      />
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-3">
        <CountStat label="N+1 findings" value={data.total_findings} tone="warning" />
        <CountStat label="Entities scanned" value={data.entities_scanned} tone="info" />
      </div>

      {Object.keys(data.by_orm).length > 0 && (
        <Card>
          <CardBody className="flex flex-wrap items-center gap-2 py-3">
            <span className="text-xs text-text-4 mr-1">By ORM:</span>
            {Object.entries(data.by_orm).map(([orm, count]) => (
              <Badge key={orm} tone="neutral">
                {orm} · {count}
              </Badge>
            ))}
          </CardBody>
        </Card>
      )}

      <div className="space-y-2">
        {data.findings.map((f) => (
          <NPlusOneRow key={f.query_entity_id} f={f} />
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § God-nodes tab
// ---------------------------------------------------------------------------

function GodNodeRow({ n, max }: { n: GodNode; max: number }) {
  const widthPct = max > 0 ? (n.pagerank / max) * 100 : 0;
  return (
    <div className="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-border bg-surface">
      <Crown size={13} className="text-warning shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 min-w-0">
          <span className="font-mono text-sm text-text truncate" title={n.label}>
            {n.label || shortMember(n.id)}
          </span>
          <Badge tone="neutral" className="shrink-0">
            {n.kind}
          </Badge>
        </div>
        <div className="mt-1.5 h-1.5 w-full rounded-full overflow-hidden bg-surface-2 border border-border">
          <div className="h-full bg-accent transition-all" style={{ width: `${widthPct}%` }} />
        </div>
      </div>
      <RepoChip slug={n.repo} className="text-[10px] shrink-0" />
      <span className="text-[11px] tabular-nums text-text-3 w-16 text-right shrink-0">
        {n.pagerank.toFixed(4)}
      </span>
    </div>
  );
}

function GodNodesTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useGodNodes(groupId);

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="god-nodes" />;
  const nodes = data?.god_nodes ?? [];
  if (nodes.length === 0) {
    return (
      <EmptyState
        icon={<CheckCircle2 size={32} className="text-success" />}
        title="No god-nodes"
        hint="No high-degree structural hotspots were flagged across the indexed repos."
      />
    );
  }

  const max = nodes.reduce((m, n) => Math.max(m, n.pagerank), 0);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-3">
        <CountStat label="God-nodes" value={nodes.length} tone="warning" />
      </div>
      <div className="space-y-2">
        {nodes.map((n) => (
          <GodNodeRow key={n.id} n={n} max={max} />
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Quality-trends tab
// ---------------------------------------------------------------------------

/** Inline SVG sparkline for a metric series. */
function Sparkline({ points, lowerIsBetter }: { points: { v: number }[]; lowerIsBetter: boolean }) {
  if (points.length < 2) {
    return <div className="h-10 flex items-center text-[11px] text-text-4">insufficient history</div>;
  }
  const vals = points.map((p) => p.v);
  const min = Math.min(...vals);
  const max = Math.max(...vals);
  const span = max - min || 1;
  const W = 160;
  const H = 40;
  const step = W / (points.length - 1);
  const coords = points.map((p, i) => {
    const x = i * step;
    const y = H - ((p.v - min) / span) * H;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  const first = vals[0];
  const last = vals[vals.length - 1];
  const improved = lowerIsBetter ? last < first : last > first;
  const stroke = last === first ? "var(--color-text-4, #888)" : improved ? "#22c55e" : "#ef4444";
  return (
    <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} className="overflow-visible">
      <polyline
        points={coords.join(" ")}
        fill="none"
        stroke={stroke}
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

function DeltaPill({ delta, lowerIsBetter }: { delta?: number; lowerIsBetter: boolean }) {
  if (delta == null || delta === 0) {
    return (
      <span className="flex items-center gap-0.5 text-[11px] text-text-4">
        <Minus size={11} /> 0
      </span>
    );
  }
  const improved = lowerIsBetter ? delta < 0 : delta > 0;
  const Icon = delta > 0 ? ArrowUpRight : ArrowDownRight;
  return (
    <span
      className={cn(
        "flex items-center gap-0.5 text-[11px] tabular-nums",
        improved ? "text-success" : "text-danger",
      )}
    >
      <Icon size={11} />
      {delta > 0 ? "+" : ""}
      {delta.toFixed(1)}
    </span>
  );
}

function MetricTrendCard({ m }: { m: MetricTrend }) {
  return (
    <Card>
      <CardHeader className="flex items-center justify-between gap-2">
        <CardTitle className="text-md">{m.label}</CardTitle>
        <span className="text-xl font-semibold tabular-nums text-text">
          {m.latest != null
            ? m.unit === "%"
              ? `${m.latest.toFixed(1)}%`
              : m.latest.toFixed(0)
            : "—"}
        </span>
      </CardHeader>
      <CardBody className="space-y-2">
        <Sparkline points={m.points} lowerIsBetter={m.lower_is_better} />
        <div className="flex items-center gap-4 text-[11px] text-text-4">
          <span className="flex items-center gap-1">
            7d <DeltaPill delta={m.delta_7d} lowerIsBetter={m.lower_is_better} />
          </span>
          <span className="flex items-center gap-1">
            30d <DeltaPill delta={m.delta_30d} lowerIsBetter={m.lower_is_better} />
          </span>
          {m.goal != null && m.goal !== 0 && (
            <span className="ml-auto">
              goal {m.unit === "%" ? `${m.goal}%` : m.goal}
            </span>
          )}
        </div>
      </CardBody>
    </Card>
  );
}

function TrendsTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useQualityTrends(groupId);

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="quality trends" />;
  if (!data || data.metrics.length === 0) {
    return (
      <EmptyState
        icon={<TrendingUp size={32} className="text-text-4" />}
        title="No trend history yet"
        hint="Quality history accumulates over successive rebuilds. Re-index a few times to populate the time series."
      />
    );
  }

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
      {data.metrics.map((m) => (
        <MetricTrendCard key={m.label} m={m} />
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Screen shell
// ---------------------------------------------------------------------------

type QualityTab = "coverage" | "dependencies" | "anti-patterns" | "god-nodes" | "trends";

export default function QualityScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [tab, setTab] = useState<QualityTab>("coverage");

  // Lightweight count pills on the tab strip (re-uses the same cached queries).
  const coverage = useQualityCoverage(groupId);
  const deps = useDependencies(groupId);
  const anti = useAntiPatterns(groupId);
  const god = useGodNodes(groupId);

  const depHygiene = deps.data
    ? deps.data.summary.unused + deps.data.summary.phantom
    : 0;

  return (
    <div className="flex flex-col h-full bg-bg">
      <Tabs
        value={tab}
        onValueChange={(v) => setTab(v as QualityTab)}
        className="flex flex-col flex-1 min-h-0"
      >
        {/* Tab strip */}
        <div className="border-b border-border shrink-0 px-4">
          <TabsList className="border-0">
            <TabsTrigger value="coverage">
              <GaugeCircle size={14} className="mr-1.5" />
              Test coverage
              {!coverage.isLoading && coverage.data && (
                <Pill className="ml-1.5">{coverage.data.coverage_pct.toFixed(0)}%</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="dependencies">
              <Boxes size={14} className="mr-1.5" />
              Dependencies
              {!deps.isLoading && deps.data && depHygiene > 0 && (
                <Pill className="ml-1.5">{depHygiene}</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="anti-patterns">
              <Repeat size={14} className="mr-1.5" />
              Anti-patterns
              {!anti.isLoading && anti.data && anti.data.total_findings > 0 && (
                <Pill className="ml-1.5">{anti.data.total_findings}</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="god-nodes">
              <Crown size={14} className="mr-1.5" />
              God-nodes
              {!god.isLoading && god.data && god.data.god_nodes.length > 0 && (
                <Pill className="ml-1.5">{god.data.god_nodes.length}</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="trends">
              <TrendingUp size={14} className="mr-1.5" />
              Trends
            </TabsTrigger>
          </TabsList>
        </div>

        {/* Workspace */}
        <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4">
          <TabsContent value="coverage">
            <CoverageTab groupId={groupId} />
          </TabsContent>
          <TabsContent value="dependencies">
            <DependenciesTab groupId={groupId} />
          </TabsContent>
          <TabsContent value="anti-patterns">
            <AntiPatternsTab groupId={groupId} />
          </TabsContent>
          <TabsContent value="god-nodes">
            <GodNodesTab groupId={groupId} />
          </TabsContent>
          <TabsContent value="trends">
            <TrendsTab groupId={groupId} />
          </TabsContent>
        </div>
      </Tabs>
    </div>
  );
}
