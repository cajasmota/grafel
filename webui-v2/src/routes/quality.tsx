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

import { useMemo, useState } from "react";
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
  Info,
  ChevronRight,
  Folder,
  FolderOpen,
  FileCode2,
  FileText,
  Box,
  Braces,
  Hexagon,
  Variable,
  Network,
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
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  TabCount,
  DefTerm,
  CoverageProvenanceBanner,
  useSetInsight,
} from "@/components/ui";
import type { InsightValue } from "@/components/ui";
import type { CoverageSourceState } from "@/lib/coverage-provenance";
import { coverageStateFromReport } from "@/lib/coverage-provenance";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { useSourcePeek } from "@/components/SourcePeek";
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
  FileUncovered,
  DirCoverage,
  FileCoverage,
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

// ---------------------------------------------------------------------------
// § Explanation primitives (#4507) — per-tab headers + per-metric tooltips
// ---------------------------------------------------------------------------

/**
 * A hoverable info icon that reveals a metric definition: what it means, how
 * it's computed, and the goal. Sits next to a metric label/title.
 */
function MetricInfo({ hint }: { hint: React.ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label="What is this metric?"
          className="inline-flex items-center justify-center text-text-4 hover:text-text-2 transition-colors cursor-help shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] rounded-full"
        >
          <Info size={13} />
        </button>
      </TooltipTrigger>
      <TooltipContent className="max-w-[18rem] leading-relaxed">{hint}</TooltipContent>
    </Tooltip>
  );
}

/** A metric stat card with a label, value, tone, and a definition tooltip. */
function MetricStat({
  label,
  value,
  hint,
  tone,
}: {
  label: string;
  value: number | string;
  hint: React.ReactNode;
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
    <Card className="flex-1 min-w-[140px]">
      <CardBody className="py-3">
        <p className={cn("text-2xl font-semibold tabular-nums", color)}>{value}</p>
        <p className="mt-0.5 flex items-center gap-1 text-xs text-text-4">
          {label}
          <MetricInfo hint={hint} />
        </p>
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

/**
 * TwoBandCoverageBar renders REACH coverage (executed by a test) as the solid
 * primary band and, behind it, the CONTRACT-covered-only slice (shape-asserted
 * offline but never executed) as a hatched secondary band (#4662). The two are
 * honestly distinct: reach is the headline %, contract-only is the extra slice
 * an offline contract spec asserts the shape of without calling the handler.
 */
function TwoBandCoverageBar({
  reachPct,
  contractPct,
}: {
  reachPct: number;
  contractPct: number;
}) {
  const tone =
    reachPct >= 80 ? "bg-success" : reachPct >= 50 ? "bg-warning" : "bg-danger";
  // The contract band is the extra width beyond reach (the union minus reach),
  // clamped so the two never exceed 100%.
  const reachW = Math.min(reachPct, 100);
  const contractW = Math.max(0, Math.min(contractPct, 100) - reachW);
  return (
    <div
      className="relative h-2 w-full rounded-full overflow-hidden bg-surface-2 border border-border"
      role="img"
      aria-label={`${reachPct.toFixed(0)}% reach-covered, ${contractPct.toFixed(0)}% including contract-covered`}
    >
      {/* secondary contract-covered band (hatched, behind, starts after reach) */}
      {contractW > 0 && (
        <div
          className="absolute inset-y-0 bg-info/40"
          style={{
            left: `${reachW}%`,
            width: `${contractW}%`,
            backgroundImage:
              "repeating-linear-gradient(45deg, transparent, transparent 3px, rgba(255,255,255,0.25) 3px, rgba(255,255,255,0.25) 6px)",
          }}
        />
      )}
      {/* primary reach band (solid) */}
      <div
        className={cn("absolute inset-y-0 left-0 transition-all", tone)}
        style={{ width: `${reachW}%` }}
      />
    </div>
  );
}

function CoverageGauge({
  covered,
  total,
  pct,
  totalTests,
  contractOnly,
  contractPct,
}: {
  covered: number;
  total: number;
  pct: number;
  totalTests: number;
  /** Endpoints shape-asserted by an offline contract spec but not executed (#4662). */
  contractOnly: number;
  /** Union (reach + contract-only) % — drives the secondary band (#4662). */
  contractPct: number;
}) {
  const trulyUncovered = Math.max(0, total - covered - contractOnly);
  return (
    <Card>
      <CardHeader className="flex items-center justify-between">
        <CardTitle className="flex items-center gap-1.5">
          Test coverage
          <MetricInfo
            hint={
              <>
                <strong>Reach coverage</strong> = % of production entities a test
                actually <em>executes</em> (a test CALLS the handler), covered ÷
                production entities. This is the headline figure — structural, not
                line coverage. Goal 80%+.
                <br />
                <br />
                <strong>Contract-covered</strong> (hatched band) = endpoints whose
                shape an <em>offline contract spec asserts</em> but{" "}
                <em>no test calls</em>. These are not dangerously untested; they
                are shape-verified offline. The band is shown behind reach so the
                gap between "executed" and "shape-asserted" is honest and visible.
              </>
            }
          />
        </CardTitle>
        <span className="text-2xl font-semibold tabular-nums text-text">
          {pct.toFixed(1)}%
        </span>
      </CardHeader>
      <CardBody className="space-y-3">
        <TwoBandCoverageBar reachPct={pct} contractPct={contractPct} />
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-md">
          <span className="flex items-center gap-1.5 text-text-2">
            <CheckCircle2 size={13} className="text-success" />
            {covered} reach-covered
          </span>
          {contractOnly > 0 && (
            <span
              className="flex items-center gap-1.5 text-text-2"
              title="An offline contract spec asserts these endpoints' shape, but no test calls them."
            >
              <FileText size={13} className="text-info" />
              {contractOnly} contract-covered ({contractPct.toFixed(1)}% incl.)
            </span>
          )}
          <span className="text-text-2">{trulyUncovered} uncovered</span>
          <span className="text-text-4">· {total} production entities</span>
          <span className="text-text-4">· {totalTests} tests</span>
        </div>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Coverage tree (#4511) — nest flat per-directory rows into a folder tree,
//   aggregating covered/total up from the leaves.
// ---------------------------------------------------------------------------

interface CovTreeNode {
  /** Last path segment (display name). */
  name: string;
  /** Full path from root, used as a stable key. */
  path: string;
  /** depth from root (0 = top level). */
  depth: number;
  covered: number;
  total: number;
  /** True when this node is a source file (deepest leaf), not a directory. */
  isFile: boolean;
  children: CovTreeNode[];
  /**
   * Uncovered entities owned by this file (#4636). Only ever set on file nodes;
   * rendered as slim leaf rows when the file is expanded.
   */
  uncovered?: UncoveredEntity[];
  /** Count of uncovered entities the backend trimmed past its per-file cap. */
  uncoveredMore?: number;
}

/**
 * Build a nested folder tree from the flat per-file coverage list (falling
 * back to the per-directory list when no files are available).
 *
 * Files are the deepest leaves: a file like `src/modules/aoc/aoc.controller.ts`
 * nests under each of its directory segments, with the basename as a file leaf.
 * Each file's covered/total counts roll up into every ancestor directory so
 * folders report aggregates that match the backend's directory rollups.
 * Intermediate folders the backend never emitted directly are synthesised so
 * the hierarchy is complete.
 */
function buildCoverageTree(
  dirs: DirCoverage[],
  files: FileCoverage[],
  byFileUncovered?: Record<string, FileUncovered>,
): CovTreeNode[] {
  const root: CovTreeNode = {
    name: "",
    path: "",
    depth: -1,
    covered: 0,
    total: 0,
    isFile: false,
    children: [],
  };

  const childByPath = new Map<string, CovTreeNode>();
  const ensureChild = (parent: CovTreeNode, name: string, isFile: boolean): CovTreeNode => {
    const path = parent.path ? `${parent.path}/${name}` : name;
    let node = childByPath.get(path);
    if (!node) {
      node = {
        name,
        path,
        depth: parent.depth + 1,
        covered: 0,
        total: 0,
        isFile,
        children: [],
      };
      childByPath.set(path, node);
      parent.children.push(node);
    }
    return node;
  };

  if (files.length > 0) {
    // File-level tree: walk directory segments, then add the file basename as a
    // leaf. Roll each file's counts up into every ancestor directory.
    for (const f of files) {
      const fileSegs = (f.file || "").split("/").filter(Boolean);
      if (fileSegs.length === 0) continue;
      const base = fileSegs[fileSegs.length - 1];
      const dirSegs = fileSegs.slice(0, -1);
      let cursor = root;
      for (const seg of dirSegs) {
        cursor = ensureChild(cursor, seg, false);
        cursor.covered += f.covered;
        cursor.total += f.total;
      }
      const fileNode = ensureChild(cursor, base, true);
      fileNode.covered += f.covered;
      fileNode.total += f.total;
      // Attach this file's uncovered entities as leaf data (#4636). Keyed by the
      // full forward-slash path, which matches FileCoverage.file.
      const fu = byFileUncovered?.[f.file];
      if (fu) {
        fileNode.uncovered = [...(fileNode.uncovered ?? []), ...(fu.entities ?? [])];
        if (fu.more) fileNode.uncoveredMore = (fileNode.uncoveredMore ?? 0) + fu.more;
      }
    }
  } else {
    // Fallback: directory-only tree (legacy payloads without by_file).
    for (const d of dirs) {
      const segments = (d.dir || "(root)").split("/").filter(Boolean);
      if (segments.length === 0) segments.push("(root)");
      let cursor = root;
      for (const seg of segments) {
        cursor = ensureChild(cursor, seg, false);
        cursor.covered += d.covered;
        cursor.total += d.total;
      }
    }
  }

  const sortRec = (nodes: CovTreeNode[]) => {
    // Directories before files, then alphabetical.
    nodes.sort((a, b) => {
      if (a.isFile !== b.isFile) return a.isFile ? 1 : -1;
      return a.name.localeCompare(b.name);
    });
    nodes.forEach((n) => sortRec(n.children));
  };
  sortRec(root.children);
  return root.children;
}

// Severity → dot color for the slim uncovered leaf rows (#4636). High=red,
// Medium=amber, Low=neutral.
const COV_SEVERITY_DOT: Record<string, string> = {
  high: "bg-danger",
  medium: "bg-warning",
  low: "bg-text-4",
};

// Minimal kind → icon map for uncovered-entity leaves. Keeps the slim row
// visually scannable without a heavy per-row chip. Falls back to a generic box.
function kindIcon(kind: string) {
  const k = (kind || "").toLowerCase();
  if (k.includes("class") || k.includes("struct") || k.includes("interface") || k.includes("type"))
    return Hexagon;
  if (k.includes("func") || k.includes("method") || k.includes("constructor")) return Braces;
  if (k.includes("endpoint") || k.includes("route") || k.includes("handler")) return Network;
  if (k.includes("var") || k.includes("const") || k.includes("field") || k.includes("prop"))
    return Variable;
  return Box;
}

type CovSeverity = "all" | "high" | "medium" | "low";

/**
 * Slim uncovered-entity leaf row (#4636) — a deep tree leaf, so it stays
 * compact: kind icon · entity name · severity dot · `:line`. Clicking opens the
 * shared source-peek modal (no per-row repo chip; the repo is the tree root).
 */
function UncoveredLeafRow({
  u,
  repo,
  groupId,
  depth,
}: {
  u: UncoveredEntity;
  repo: string;
  groupId: string;
  depth: number;
}) {
  const { openSourcePeek } = useSourcePeek();
  // Prefer the entity's own repo slug (stamped by the aggregator, #4551).
  const entityRepo = u.repo || repo;
  const Icon = kindIcon(u.kind);
  const canPeek = !!u.source_file;
  return (
    <div
      className={cn(
        "flex items-center gap-2 pr-3 py-1 rounded-lg border border-transparent",
        "hover:bg-surface-2 transition-colors",
        canPeek && "cursor-pointer",
      )}
      style={{ paddingLeft: `${0.5 + depth * 1.1}rem` }}
      onClick={
        canPeek
          ? () =>
              openSourcePeek({
                groupId,
                file: u.source_file,
                line: u.start_line ?? 0,
                repo: entityRepo,
              })
          : undefined
      }
      role={canPeek ? "button" : undefined}
      title={`${u.kind} ${u.name}${u.source_file ? ` · ${u.source_file}:${u.start_line ?? 0}` : ""}`}
    >
      {/* Align with the chevron column of directory/file rows. */}
      <span className="w-4 shrink-0" />
      <Icon size={12} className="shrink-0 text-text-4" />
      <span className="font-mono text-xs text-text-2 truncate min-w-0 flex-1" title={u.name}>
        {u.name}
      </span>
      {u.state === "contract-only" && (
        <span
          className="shrink-0 inline-flex items-center gap-0.5 rounded px-1 py-px text-[9px] font-medium uppercase tracking-wide text-info bg-info/10"
          title="An offline contract spec asserts this endpoint's shape, but no test calls it."
        >
          <FileText size={9} />
          contract
        </span>
      )}
      <span
        className={cn("h-1.5 w-1.5 rounded-full shrink-0", COV_SEVERITY_DOT[u.severity] ?? "bg-text-4")}
        title={`${u.severity} severity`}
      />
      <span className="font-mono text-[11px] tabular-nums text-text-4 shrink-0">
        :{u.start_line ?? 0}
      </span>
    </div>
  );
}

function CovTreeRow({
  node,
  expanded,
  toggle,
  severity,
  groupId,
  repo,
}: {
  node: CovTreeNode;
  expanded: Set<string>;
  toggle: (path: string) => void;
  severity: CovSeverity;
  groupId: string;
  repo: string;
}) {
  // Uncovered leaves under a file, narrowed by the active severity filter.
  const leaves =
    node.isFile && node.uncovered
      ? severity === "all"
        ? node.uncovered
        : node.uncovered.filter((u) => u.severity === severity)
      : [];
  const moreCount =
    node.isFile && severity === "all" ? node.uncoveredMore ?? 0 : 0;
  const hasLeaves = leaves.length > 0 || moreCount > 0;
  const hasChildren = node.children.length > 0 || hasLeaves;
  const isOpen = expanded.has(node.path);
  const pct = node.total > 0 ? (node.covered / node.total) * 100 : 0;

  return (
    <div>
      <div
        className={cn(
          "flex items-center gap-2 pr-3 py-1.5 rounded-lg border border-transparent",
          "hover:bg-surface-2 transition-colors",
          hasChildren && "cursor-pointer",
        )}
        style={{ paddingLeft: `${0.5 + node.depth * 1.1}rem` }}
        onClick={hasChildren ? () => toggle(node.path) : undefined}
        role={hasChildren ? "button" : undefined}
        aria-expanded={hasChildren ? isOpen : undefined}
      >
        <span className="w-4 shrink-0 text-text-4">
          {hasChildren ? (
            <ChevronRight
              size={13}
              className={cn("transition-transform", isOpen && "rotate-90")}
            />
          ) : null}
        </span>
        <span className="shrink-0 text-text-4">
          {node.isFile ? (
            <FileCode2 size={13} className="text-text-3" />
          ) : hasChildren ? (
            isOpen ? (
              <FolderOpen size={13} className="text-accent" />
            ) : (
              <Folder size={13} className="text-accent" />
            )
          ) : (
            <Folder size={13} className="text-text-4" />
          )}
        </span>
        <span className="font-mono text-xs text-text-2 truncate flex-1 min-w-0" title={node.path}>
          {node.name}
        </span>
        <div className="w-28 shrink-0 hidden sm:block">
          <CoverageBar pct={pct} />
        </div>
        <span className="text-xs tabular-nums text-text-3 w-12 text-right shrink-0">
          {pct.toFixed(0)}%
        </span>
        <span className="text-[11px] tabular-nums text-text-4 w-16 text-right shrink-0">
          {node.covered}/{node.total}
        </span>
      </div>
      {hasChildren && isOpen && (
        <div>
          {node.children.map((c) => (
            <CovTreeRow
              key={c.path}
              node={c}
              expanded={expanded}
              toggle={toggle}
              severity={severity}
              groupId={groupId}
              repo={repo}
            />
          ))}
          {leaves.map((u) => (
            <UncoveredLeafRow
              key={u.entity_id}
              u={u}
              repo={repo}
              groupId={groupId}
              depth={node.depth + 1}
            />
          ))}
          {moreCount > 0 && (
            <div
              className="flex items-center gap-2 pr-3 py-0.5 text-[11px] text-text-4 italic"
              style={{ paddingLeft: `${0.5 + (node.depth + 1) * 1.1 + 1.25}rem` }}
            >
              +{moreCount} more uncovered
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function CoverageTree({
  dirs,
  files,
  byFileUncovered,
  severity,
  groupId,
  repo,
}: {
  dirs: DirCoverage[];
  files: FileCoverage[];
  byFileUncovered?: Record<string, FileUncovered>;
  severity: CovSeverity;
  groupId: string;
  repo: string;
}) {
  const tree = useMemo(
    () => buildCoverageTree(dirs, files, byFileUncovered),
    [dirs, files, byFileUncovered],
  );
  // Default: top-level directories expanded; files (and their uncovered leaves)
  // collapsed — the uncovered entities reveal on demand (#4552 deferred this).
  const [expanded, setExpanded] = useState<Set<string>>(
    () => new Set(tree.filter((n) => !n.isFile).map((n) => n.path)),
  );
  const toggle = (path: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });

  return (
    <div className="space-y-0.5">
      {tree.map((n) => (
        <CovTreeRow
          key={n.path}
          node={n}
          expanded={expanded}
          toggle={toggle}
          severity={severity}
          groupId={groupId}
          repo={repo}
        />
      ))}
    </div>
  );
}

function UncoveredRow({ u, repo }: { u: UncoveredEntity; repo: string }) {
  // Prefer the entity's own repo slug (stamped by the aggregator, #4551) so
  // source-peek resolves through the correct repo root in a multi-repo group.
  // The `repo` prop is the group-level slug and is only a last-resort fallback.
  const entityRepo = u.repo || repo;
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0">
        <span className="font-mono text-sm text-text truncate" title={u.name}>
          {u.name}
        </span>
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {u.state === "contract-only" && (
            <Badge
              tone="info"
              className="shrink-0 inline-flex items-center gap-0.5"
              title="An offline contract spec asserts this endpoint's shape, but no test calls it."
            >
              <FileText size={11} />
              contract-covered
            </Badge>
          )}
          <Badge tone="neutral" className="shrink-0">
            {u.kind}
          </Badge>
          <CovSeverityBadge severity={u.severity} />
        </div>
      </div>
      <div className="flex items-center gap-2 min-w-0 -mx-1">
        <RepoChip slug={entityRepo} className="text-[10px] shrink-0" />
        {u.source_file ? (
          <RefLine
            repo={entityRepo}
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

  const uncoveredEntities = data.uncovered_entities ?? [];
  const uncovered =
    severity === "all"
      ? uncoveredEntities
      : uncoveredEntities.filter((u) => u.severity === severity);

  // The tree carries uncovered entities as file leaves (#4636) whenever the
  // backend serves by_file_uncovered. The flat list below is only a fallback
  // for older backends that don't.
  const byFileUncovered = data.by_file_uncovered;
  const treeCarriesUncovered =
    !!byFileUncovered && Object.keys(byFileUncovered).length > 0;
  // Total uncovered leaves shown in the tree at the current severity (for the
  // header count when the tree is the primary surface).
  const treeUncoveredCount = treeCarriesUncovered
    ? Object.values(byFileUncovered).reduce(
        (sum, fu) =>
          sum +
          (severity === "all"
            ? (fu.entities?.length ?? 0)
            : (fu.entities ?? []).filter((u) => u.severity === severity).length),
        0,
      )
    : uncovered.length;

  const severityFilter = (
    <div className="flex items-center gap-2">
      {(["all", "high", "medium", "low"] as const).map((s) => (
        <Pill key={s} active={severity === s} onClick={() => setSeverity(s)}>
          {s === "all" ? "All" : s[0].toUpperCase() + s.slice(1)}
        </Pill>
      ))}
    </div>
  );

  // Coverage provenance (#5066, wires #5038). The dashboard's headline
  // `coverage_pct` is graph-derived REACH coverage (static test-reachability),
  // not a measured line %. When a coverage report was ingested (#5036) the
  // endpoint now carries `line_coverage` — its presence is the authoritative
  // "report ingestion ran" signal, so we populate `line` (authoritative % +
  // measured-at) and mark ingestion configured, and the banner upgrades to the
  // line-coverage state automatically. Otherwise we report reachability and the
  // banner surfaces the "how to enable" affordance.
  const lineCov = data.line_coverage;
  const coverageProvenance: CoverageSourceState =
    coverageStateFromReport(lineCov);

  return (
    <div className="space-y-4">
      <CoverageProvenanceBanner state={coverageProvenance} />
      {lineCov && (
        // Real ingested line coverage (#5066) — the authoritative executed
        // line %, shown distinctly from the reach-coverage gauge below so the
        // two numbers are never conflated.
        <div className="flex flex-wrap items-center gap-2 text-sm">
          <Badge tone="success">
            Line coverage {lineCov.coverage_pct.toFixed(1)}%
          </Badge>
          <span className="text-text-4 tabular-nums">
            {lineCov.covered_lines.toLocaleString()} /{" "}
            {lineCov.total_lines.toLocaleString()} lines (
            {lineCov.source.toUpperCase()})
          </span>
        </div>
      )}
      <CoverageGauge
        covered={data.covered_production}
        total={data.total_production}
        pct={data.coverage_pct}
        totalTests={data.total_tests}
        contractOnly={data.contract_covered_only ?? 0}
        contractPct={data.contract_covered_pct ?? data.coverage_pct}
      />

      {(data.by_directory?.length ?? 0) > 0 && (
        <Card>
          <CardHeader className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle className="flex items-center gap-1.5">
              By directory
              <MetricInfo
                hint={
                  <>
                    Coverage rolled up into a folder tree, drilling directory →
                    file → entity. Each folder aggregates the covered ÷ total
                    counts beneath it; expand a folder to reach its files, and
                    expand a file to see its individual uncovered entities. The
                    severity filter narrows which uncovered entities appear. Bar
                    color: red &lt;50%, amber 50–80%, green 80%+.
                  </>
                }
              />
              {treeCarriesUncovered && (
                <span className="ml-1 text-text-4 tabular-nums font-normal text-xs">
                  {treeUncoveredCount} uncovered
                </span>
              )}
            </CardTitle>
            {treeCarriesUncovered ? (
              severityFilter
            ) : (
              <span className="text-[11px] text-text-4">expand to drill in</span>
            )}
          </CardHeader>
          <CardBody className="space-y-1.5">
            <CoverageTree
              dirs={data.by_directory}
              files={data.by_file ?? []}
              byFileUncovered={data.by_file_uncovered}
              severity={severity}
              groupId={groupId}
              repo={data.group}
            />
          </CardBody>
        </Card>
      )}

      {/* Fallback flat list — only for older backends that don't nest uncovered
          entities under their file in the tree (#4636). */}
      {!treeCarriesUncovered && (
        <>
          <div className="flex items-center justify-between gap-3">
            <h3 className="text-md font-medium text-text-2">
              Uncovered entities
              <span className="ml-2 text-text-4 tabular-nums">{uncovered.length}</span>
            </h3>
            {severityFilter}
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
        </>
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
  const packages = rep.packages ?? [];
  const pkgs =
    statusFilter === "all"
      ? packages
      : packages.filter((p) => p.status === statusFilter);
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
  const repoSlugs = data ? Object.keys(data.by_repo ?? {}).sort() : [];
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
        <MetricStat
          label="Declared"
          value={data.summary.declared}
          hint={
            <>
              <strong>Declared</strong> = packages listed in the manifest
              (package.json, requirements.txt, go.mod…). The baseline the other
              counts are measured against.
            </>
          }
        />
        <MetricStat
          label="Used"
          value={data.summary.used}
          tone="success"
          hint={
            <>
              <strong>Used</strong> = declared packages with at least one matching
              IMPORTS edge in the code. These are healthy. Higher is better.
            </>
          }
        />
        <MetricStat
          label="Unused"
          value={data.summary.unused}
          tone="warning"
          hint={
            <>
              <strong>Unused</strong> = declared in the manifest but never imported.
              Dead dependencies — candidates to remove to shrink the install and
              attack surface. Goal 0.
            </>
          }
        />
        <MetricStat
          label="Phantom"
          value={data.summary.phantom}
          tone="danger"
          hint={
            <>
              <strong>Phantom</strong> = imported in code but not declared in any
              manifest. Relies on a transitive/implicit install and can break at any
              time — should be declared explicitly. Goal 0.
            </>
          }
        />
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
        <MetricStat
          label="N+1 findings"
          value={data.total_findings}
          tone="warning"
          hint={
            <>
              <strong>N+1 findings</strong> = count of ORM query calls detected
              inside a loop (a CALLS edge to a query method from a loop body). Each
              is a candidate to batch/eager-load into a single query. Goal 0.
            </>
          }
        />
        <MetricStat
          label="Entities scanned"
          value={data.entities_scanned}
          tone="info"
          hint={
            <>
              <strong>Entities scanned</strong> = number of functions/methods the
              N+1 detector inspected for loop-wrapped queries. Context for how broad
              the scan was — not a quality target itself.
            </>
          }
        />
      </div>

      {Object.keys(data.by_orm ?? {}).length > 0 && (
        <Card>
          <CardBody className="flex flex-wrap items-center gap-2 py-3">
            <span className="text-xs text-text-4 mr-1">By ORM:</span>
            {Object.entries(data.by_orm ?? {}).map(([orm, count]) => (
              <Badge key={orm} tone="neutral">
                {orm} · {count}
              </Badge>
            ))}
          </CardBody>
        </Card>
      )}

      <div className="space-y-2">
        {(data.findings ?? []).map((f) => (
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
        <MetricStat
          label="God-nodes"
          value={nodes.length}
          tone="warning"
          hint={
            <>
              <strong>God-node</strong> = a high-centrality hub entity (many things
              depend on or route through it), scored by PageRank over the dependency
              graph. High scores signal a refactor candidate — splitting it reduces
              blast radius. Fewer is better.
            </>
          }
        />
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

/** Per-metric definitions keyed by the backend label (#4507). */
const METRIC_HINTS: Record<string, React.ReactNode> = {
  "Health score": (
    <>
      <strong>Health score</strong> = composite 0–100 blending orphan rate, bug
      rate and test coverage into one figure. A quick at-a-glance signal; drill
      into the individual metrics for the why. Higher is better; goal 90+.
    </>
  ),
  "Orphan rate": (
    <>
      <strong>Orphan rate</strong> = % of entities with no graph edges at all
      (nothing imports, calls or references them). High values mean dead or
      disconnected code, or gaps in extraction. Lower is better.
    </>
  ),
  "Bug rate": (
    <>
      <strong>Bug rate</strong> = density of bug-risk findings (e.g. error-prone
      patterns) relative to entities scanned. A heuristic smell signal, not a test
      pass-rate. Lower is better.
    </>
  ),
  "Test coverage": (
    <>
      <strong>Test coverage</strong> = % of production entities reached by a TESTS
      edge (structural reachability, not line coverage). Higher is better; goal 80%+.
    </>
  ),
  "Import cycles": (
    <>
      <strong>Import cycles</strong> = number of circular import chains (module A →
      B → A) in the IMPORTS graph. Cycles hurt build/test isolation and signal tangled
      modules. Lower is better; goal 0.
    </>
  ),
  "Auth-uncovered endpoints": (
    <>
      <strong>Auth-uncovered endpoints</strong> = HTTP endpoints with no detected
      authentication guard/middleware on their route. Potential unauthenticated
      surface to review. Lower is better; goal 0.
    </>
  ),
  "Secret findings": (
    <>
      <strong>Secret findings</strong> = count of likely hardcoded secrets
      (keys, tokens, credentials) detected in the source. Each should be rotated and
      moved to a secret store. Lower is better; goal 0.
    </>
  ),
};

/**
 * A metric has real, plottable trend history only when there are at least two
 * snapshots AND the values are not all identical. A single snapshot (or a flat
 * line) is not a time-series — drawing a sparkline for it would be fabricated
 * (#4506), so callers fall back to an honest "no trend data yet" state.
 */
function hasRealTrend(points: { v: number }[]): boolean {
  if (points.length < 2) return false;
  const first = points[0].v;
  return points.some((p) => p.v !== first);
}

/** Inline SVG sparkline for a metric series. Only call when hasRealTrend(). */
function Sparkline({ points, lowerIsBetter }: { points: { v: number }[]; lowerIsBetter: boolean }) {
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
  const stroke = last === first ? "var(--color-text-4, #888)" : improved ? "var(--color-success)" : "var(--color-danger)";
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
  const real = hasRealTrend(m.points);
  const hint = METRIC_HINTS[m.label];
  const goal = m.goal != null && m.goal !== 0
    ? `goal ${m.unit === "%" ? `${m.goal}%` : m.goal}`
    : null;

  return (
    <Card>
      <CardHeader className="flex items-center justify-between gap-2">
        <CardTitle className="text-md flex items-center gap-1.5">
          {m.label}
          {hint && <MetricInfo hint={hint} />}
        </CardTitle>
        <span className="text-xl font-semibold tabular-nums text-text">
          {m.latest != null
            ? m.unit === "%"
              ? `${m.latest.toFixed(1)}%`
              : m.latest.toFixed(0)
            : "—"}
        </span>
      </CardHeader>
      <CardBody className="space-y-2">
        {real ? (
          <>
            <Sparkline points={m.points} lowerIsBetter={m.lower_is_better} />
            <div className="flex items-center gap-4 text-[11px] text-text-4">
              <span className="flex items-center gap-1">
                7d <DeltaPill delta={m.delta_7d} lowerIsBetter={m.lower_is_better} />
              </span>
              <span className="flex items-center gap-1">
                30d <DeltaPill delta={m.delta_30d} lowerIsBetter={m.lower_is_better} />
              </span>
              {goal && <span className="ml-auto">{goal}</span>}
            </div>
          </>
        ) : (
          <div className="flex flex-col gap-1.5 h-[58px] justify-center">
            <p className="text-[11px] text-text-4 leading-snug">
              No trend data yet — snapshots accumulate over time.
            </p>
            {goal && <span className="text-[11px] text-text-4">{goal}</span>}
          </div>
        )}
      </CardBody>
    </Card>
  );
}

function TrendsTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useQualityTrends(groupId);

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="quality trends" />;
  if (!data || (data.metrics?.length ?? 0) === 0) {
    return (
      <EmptyState
        icon={<TrendingUp size={32} className="text-text-4" />}
        title="No trend history yet"
        hint="Quality history accumulates over successive rebuilds. Re-index a few times to populate the time series."
      />
    );
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        {data.metrics.map((m) => (
          <MetricTrendCard key={m.label} m={m} />
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Screen shell
// ---------------------------------------------------------------------------

type QualityTab = "coverage" | "dependencies" | "anti-patterns" | "god-nodes" | "trends";

/**
 * Per-tab insights (#4655). Registered with the breadcrumb Insights button via
 * useSetInsight based on the ACTIVE tab — switching tabs re-registers a new
 * object identity so the button re-glows and the popover updates. These are
 * module-level constants so each tab's identity is stable across renders (only
 * a real tab change swaps the registered value). Moved here out of the page
 * body, which previously rendered an inline <InsightBanner> per tab.
 */
const QUALITY_INSIGHTS: Record<QualityTab, InsightValue> = {
  coverage: {
    storageKey: "quality.coverage",
    human: (
      <>
        Structural test coverage in two honest bands. The headline{" "}
        <strong>reach coverage</strong> = which production entities a test
        actually executes (a test CALLS the handler) via a{" "}
        <DefTerm
          term="TESTS/CALLS edge"
          def="A graph edge linking a test entity to the production entity it executes. Reach coverage is reachability over these edges, not executed-line coverage."
        />
        . The secondary <strong>contract-covered</strong> band marks endpoints
        whose shape an offline contract spec asserts but which no test calls —
        shape-verified, not dangerously untested. This is graph reachability,
        not line coverage.
      </>
    ),
    agent: {
      tool: "archigraph_test_coverage",
      example:
        "Before writing tests for a payments module, an agent calls archigraph_test_coverage to list endpoints with no reach-coverage, distinguishes the contract-covered-only ones (shape already asserted offline) from the truly uncovered, and generates execution tests targeting exactly the real gaps instead of duplicating existing contract specs.",
    },
  },
  dependencies: {
    storageKey: "quality.deps",
    human: (
      <>
        Dependency hygiene — declared third-party packages cross-checked against
        what the code actually imports. Surfaces unused (declared, never
        imported) and phantom (imported, never declared) packages per repository.
      </>
    ),
    agent: {
      tool: "archigraph_import_cycles",
      example:
        "Before splitting a large module, an agent calls archigraph_import_cycles to confirm the move won't introduce a circular import, and cross-references phantom packages so it adds the right dependency to package.json instead of guessing.",
    },
  },
  "anti-patterns": {
    storageKey: "quality.antipatterns",
    human: (
      <>
        Anti-patterns — code shapes the indexer flags as likely performance or
        correctness smells. Currently surfaces N+1 query patterns: an ORM query
        executed inside a loop, which issues one query per iteration.
      </>
    ),
    agent: {
      tool: "archigraph_graph_patterns",
      example:
        "Reviewing a PR that adds a loop over orders, an agent calls archigraph_graph_patterns to detect a new N+1 query it introduced, then suggests a bulk prefetch or select_related fix before approving.",
    },
  },
  "god-nodes": {
    storageKey: "quality.godnodes",
    human: (
      <>
        God-nodes — high-centrality hub entities that many other things depend on
        or route through. They concentrate risk: a change here ripples widely.
        Ranked by PageRank over the dependency graph; refactor candidates.
      </>
    ),
    agent: {
      tool: "archigraph_impact_radius",
      example:
        "Before refactoring a high-PageRank service class, an agent calls archigraph_impact_radius to enumerate every downstream caller and route, then scopes the change set and flags the blast radius for human review rather than editing blind.",
    },
  },
  trends: {
    storageKey: "quality.trends",
    human: (
      <>
        Trends — how each quality metric moves across successive re-indexes. A
        sparkline appears only once there's genuine multi-snapshot history;
        freshly-indexed groups show the current value with a goal until history
        builds up.
      </>
    ),
    agent: {
      tool: "archigraph_test_coverage",
      example:
        "Running in CI after each index, an agent calls archigraph_test_coverage across snapshots to detect that coverage dropped 4% on the last merge, then opens a follow-up issue naming the newly-uncovered entities responsible for the regression.",
    },
  },
};

export default function QualityScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [tab, setTab] = useState<QualityTab>("coverage");

  // #4655: register the ACTIVE tab's insight with the breadcrumb Insights
  // button. Switching tabs passes a new object identity → the button re-glows
  // and the popover content updates. Clears on unmount (navigating away).
  useSetInsight(QUALITY_INSIGHTS[tab]);

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
          <TabsList className="border-0 gap-1">
            <TabsTrigger value="coverage" className="flex items-center gap-1.5">
              <GaugeCircle size={14} />
              Test coverage
              {!coverage.isLoading && coverage.data && (
                <TabCount
                  value={Math.round(coverage.data.coverage_pct)}
                  suffix="%"
                  tone="neutral"
                  label="percent of production entities reached by a test"
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="dependencies" className="flex items-center gap-1.5">
              <Boxes size={14} />
              Dependencies
              {!deps.isLoading && deps.data && depHygiene > 0 && (
                <TabCount
                  value={depHygiene}
                  tone="warning"
                  label="dependency-hygiene issues (cycles + orphans)"
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="anti-patterns" className="flex items-center gap-1.5">
              <Repeat size={14} />
              Anti-patterns
              {!anti.isLoading && anti.data && anti.data.total_findings > 0 && (
                <TabCount
                  value={anti.data.total_findings}
                  tone="warning"
                  label="anti-pattern findings"
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="god-nodes" className="flex items-center gap-1.5">
              <Crown size={14} />
              God-nodes
              {!god.isLoading && god.data && god.data.god_nodes.length > 0 && (
                <TabCount
                  value={god.data.god_nodes.length}
                  tone="warning"
                  label="god-nodes (overly-connected entities)"
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="trends" className="flex items-center gap-1.5">
              <TrendingUp size={14} />
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
