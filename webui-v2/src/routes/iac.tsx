/* ============================================================
   IaC / Infrastructure view (#4256, epic #4249).

   Route: /g/:groupId/iac

   The IaC extraction (terraform / aws-cdk / pulumi / cloudformation /
   sam / serverless-framework / bicep) had no dashboard surface. This
   screen surfaces it: every tool emits resource entities carrying a
   cross-tool resource_category, a tool-native type, and a curated set
   of typed scalar config properties (instance_type / memory_size /
   timeout / runtime / engine / version / port / …). Resource-to-resource
   edges model IAM grants, event-source wiring, plain dependencies, and
   stack/app/module topology.

   Data: GET /api/iac/{group} (handlers_iac.go → handleIaC), raw-JSON
   IaCReport. Resources come grouped by iac_tool.

   Layout mirrors GraphQL / Security: full-height column, a summary stat
   row, a tool filter, and a grouped list of tool → resources with a
   property table and relation badges (grants / event-sources /
   dependencies / topology) + a RefLine source ref. Reuses Card / Badge /
   Pill / Skeleton + RefLine + RepoChip.

   HONESTY: properties + relations render only when genuinely extracted.
   Terraform's resource_category is recomputed from the type (it lives in
   Metadata, not Properties); some props/edges are tool-partial. Absent
   facets are omitted, never fabricated. A repo with no IaC shows a clean
   empty state.
   ============================================================ */

import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import {
  Boxes,
  Server,
  Database,
  Key,
  Zap,
  GitBranch,
  Layers,
  AlertTriangle,
  List,
  Network,
  ChevronDown,
  ChevronRight,
  Link2,
} from "lucide-react";

import {
  Badge,
  Card,
  CardBody,
  Pill,
  InsightBanner,
  DefTerm,
} from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
import { cn } from "@/lib/utils";
import { useIaC } from "@/hooks/use-iac";
import { IaCDiagram } from "@/components/iac-diagram";
import type {
  IaCRelation,
  IaCResource,
  IaCToolGroup,
} from "@/data/types";

// ---------------------------------------------------------------------------
// § Category styling
// ---------------------------------------------------------------------------

function categoryTone(
  cat: string,
): "accent" | "info" | "warning" | "success" | "danger" | "neutral" {
  switch ((cat || "").toLowerCase()) {
    case "datastore":
    case "cache":
      return "info";
    case "queue":
    case "topic":
    case "stream":
      return "warning";
    case "function":
      return "accent";
    case "secret":
      return "danger";
    case "network":
    case "compute":
    case "storage":
      return "success";
    default:
      return "neutral";
  }
}

// ---------------------------------------------------------------------------
// § Relation facet styling
// ---------------------------------------------------------------------------

function relationMeta(facet: string): {
  label: string;
  tone: "accent" | "info" | "warning" | "success" | "danger" | "neutral";
  Icon: typeof Database;
} {
  switch ((facet || "").toLowerCase()) {
    case "grant":
      return { label: "grant", tone: "danger", Icon: Key };
    case "event_source":
      return { label: "event source", tone: "warning", Icon: Zap };
    case "topology":
      return { label: "topology", tone: "info", Icon: Layers };
    case "trigger":
      return { label: "trigger", tone: "accent", Icon: Zap };
    case "output":
      return { label: "output", tone: "neutral", Icon: GitBranch };
    default:
      return { label: "depends on", tone: "neutral", Icon: GitBranch };
  }
}

/** Short hex hash like `483f81a80cea83a1` — an unresolved raw entity id. */
function isRawId(s: string): boolean {
  return /^[0-9a-f]{12,}$/i.test(s);
}

/**
 * #4588: Terraform (and other HCL-ish tools) emit interpolation references —
 * `local.*`, `var.*`, `data.*`, `module.*`, `each.*`, `count.*`, and synthetic
 * `calls` targets — as plain DEPENDS_ON edges. These are NOT real
 * resource-to-resource dependencies; they are expression-level references that
 * flat-dumped into the card as noisy duplicate `depends on → local.*` badges.
 * We bucket them out as "references" and de-emphasize them.
 */
const INTERP_PREFIXES = [
  "local.",
  "var.",
  "data.",
  "module.",
  "each.",
  "count.",
  "path.",
  "terraform.",
  "self.",
];

function isInterpolationRef(rel: IaCRelation): boolean {
  const t = (rel.target || "").toLowerCase();
  if (t === "calls" || t === "local" || t === "var" || t === "data") return true;
  return INTERP_PREFIXES.some((p) => t.startsWith(p));
}

/**
 * A stable identity for de-duplication. Repeated edges to the same endpoint
 * with the same facet collapse into one badge (#4588 — the card was rendering
 * a dozen identical `depends on → calls` chips).
 */
function relationKey(rel: IaCRelation): string {
  const target = (rel.target_entity_id || rel.target_id || rel.target || "")
    .toLowerCase();
  return `${rel.facet}|${rel.direction}|${target}|${(rel.detail || "").toLowerCase()}`;
}

/** Dedupe a relation list, preserving first-seen order. */
function dedupeRelations(rels: IaCRelation[]): IaCRelation[] {
  const seen = new Set<string>();
  const out: IaCRelation[] = [];
  for (const r of rels) {
    const k = relationKey(r);
    if (seen.has(k)) continue;
    seen.add(k);
    out.push(r);
  }
  return out;
}

interface RelationGroup {
  facet: string;
  label: string;
  tone: ReturnType<typeof relationMeta>["tone"];
  Icon: ReturnType<typeof relationMeta>["Icon"];
  relations: IaCRelation[];
}

/**
 * Split a resource's relations into (a) real, grouped resource dependencies
 * keyed by facet, and (b) a de-emphasized bucket of interpolation references.
 * Both halves are de-duplicated.
 */
function classifyRelations(rels: IaCRelation[]): {
  groups: RelationGroup[];
  references: IaCRelation[];
} {
  const deduped = dedupeRelations(rels ?? []);
  const real: IaCRelation[] = [];
  const references: IaCRelation[] = [];
  for (const r of deduped) {
    (isInterpolationRef(r) ? references : real).push(r);
  }

  const byFacet = new Map<string, IaCRelation[]>();
  for (const r of real) {
    const f = (r.facet || "dependency").toLowerCase();
    const arr = byFacet.get(f);
    if (arr) arr.push(r);
    else byFacet.set(f, [r]);
  }

  // Stable, meaningful ordering: grants → event sources → topology → rest.
  const order = ["grant", "event_source", "trigger", "topology", "output"];
  const facets = Array.from(byFacet.keys()).sort((a, b) => {
    const ia = order.indexOf(a);
    const ib = order.indexOf(b);
    return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib) || a.localeCompare(b);
  });

  const groups: RelationGroup[] = facets.map((f) => {
    const meta = relationMeta(f);
    return {
      facet: f,
      label: meta.label,
      tone: meta.tone,
      Icon: meta.Icon,
      relations: byFacet.get(f)!,
    };
  });

  return { groups, references };
}

/** Resolved display text for a relation target, with unresolved fallback. */
function relationDisplay(relation: IaCRelation): {
  display: string;
  unresolved: boolean;
} {
  const unresolved =
    relation.target_resolved === false || isRawId(relation.target);
  return {
    unresolved,
    display: unresolved
      ? relation.kind.toLowerCase().replace(/_/g, " ")
      : relation.target,
  };
}

function RelationBadge({
  relation,
  muted = false,
}: {
  relation: IaCRelation;
  muted?: boolean;
}) {
  const { label, tone, Icon } = relationMeta(relation.facet);
  const arrow = relation.direction === "in" ? "←" : "→";
  const detail = relation.detail ? `.${relation.detail}` : "";

  // #4495: when the backend could not resolve the endpoint to a display name
  // (target_resolved === false), the target is a meaningless raw entity-id
  // hash. Render a friendlier fallback — the relation kind as the label, with
  // the raw id available on hover — instead of dumping the hash inline.
  const { display, unresolved } = relationDisplay(relation);
  const rawId = relation.target_id || relation.target;

  return (
    <Badge
      tone={muted ? "neutral" : tone}
      className={cn(
        "inline-flex items-center gap-1 max-w-[18rem] min-w-0",
        muted && "opacity-60",
      )}
      title={
        unresolved
          ? `${label}${detail} ${arrow} <unresolved ${relation.kind} target> (id ${rawId})`
          : `${label}${detail} ${arrow} ${relation.target} (${relation.kind})`
      }
    >
      <Icon size={11} className="shrink-0" />
      <span className="lowercase shrink-0">{label}</span>
      {/* #4576: long targets truncate inside the badge instead of pushing the
          chip past the card edge; the min-w-0 + truncate keeps the arrow glued
          to the (elided) target. */}
      <span
        className={cn(
          "font-mono opacity-70 truncate min-w-0",
          unresolved && "italic",
        )}
      >
        {arrow} {display}
      </span>
    </Badge>
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
      <Server size={32} className="text-text-4" />
      <p className="text-md font-medium text-text">{title}</p>
      <p className="text-sm text-text-3 max-w-md">{hint}</p>
    </div>
  );
}

function ErrorState() {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      <AlertTriangle size={32} className="text-danger" />
      <p className="text-md font-medium text-text">Could not load infrastructure</p>
      <p className="text-sm text-text-3 max-w-sm">
        The daemon returned an error. Confirm the group is indexed and the
        daemon is reachable, then retry.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § One resource row
// ---------------------------------------------------------------------------

/** A summary pill: a relation-group label + count + the top-N target names. */
function RelationSummaryPill({ group }: { group: RelationGroup }) {
  const { Icon, label, tone, relations } = group;
  const TOP = 3;
  const names = relations.map((r) => relationDisplay(r).display);
  const shown = names.slice(0, TOP);
  const extra = names.length - shown.length;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-[11px] min-w-0 max-w-full",
        toneClasses(tone),
      )}
      title={`${relations.length} ${label}${relations.length === 1 ? "" : "s"}: ${names.join(", ")}`}
    >
      <Icon size={11} className="shrink-0" />
      <span className="font-medium capitalize shrink-0">
        {pluralFacet(label, relations.length)}
      </span>
      <span className="tabular-nums opacity-70 shrink-0">{relations.length}</span>
      <span className="font-mono opacity-60 truncate min-w-0">
        {shown.join(", ")}
        {extra > 0 ? ` +${extra} more` : ""}
      </span>
    </span>
  );
}

/** Soft tonal border/bg/text classes for the summary pills. */
function toneClasses(
  tone: "accent" | "info" | "warning" | "success" | "danger" | "neutral",
): string {
  switch (tone) {
    case "accent":
      return "border-accent/40 bg-accent/10 text-accent";
    case "info":
      return "border-info/40 bg-info/10 text-info";
    case "warning":
      return "border-warning/40 bg-warning/10 text-warning";
    case "success":
      return "border-success/40 bg-success/10 text-success";
    case "danger":
      return "border-danger/40 bg-danger/10 text-danger";
    default:
      return "border-border bg-surface-2 text-text-3";
  }
}

function pluralFacet(label: string, n: number): string {
  if (n === 1) return label;
  if (label === "topology") return "topology";
  return `${label}s`;
}

/** The full grouped relation list shown in the expanded detail panel. */
function RelationDetail({
  groups,
  references,
  resource,
}: {
  groups: RelationGroup[];
  references: IaCRelation[];
  resource: IaCResource;
}) {
  return (
    <div className="space-y-3">
      {/* Typed config properties */}
      {(resource.properties?.length ?? 0) > 0 && (
        <div className="space-y-1">
          <p className="text-[10px] font-medium uppercase tracking-wide text-text-4">
            Config
          </p>
          <div className="flex flex-wrap gap-1.5">
            {resource.properties.map((p) => (
              <span
                key={p.key}
                className="inline-flex items-center gap-1 rounded border border-border bg-surface-2 px-1.5 py-0.5 text-[10px] font-mono text-text-3"
                title={`${p.key} = ${p.value}`}
              >
                <span className="text-text-4">{p.key}</span>
                <span className="text-text">{p.value}</span>
              </span>
            ))}
          </div>
        </div>
      )}

      {/* Real, grouped resource dependencies — each fully resolvable. */}
      {groups.map((g) => (
        <div key={g.facet} className="space-y-1">
          <p className="text-[10px] font-medium uppercase tracking-wide text-text-4">
            {pluralFacet(g.label, g.relations.length)}
            <span className="ml-1 tabular-nums opacity-70">
              {g.relations.length}
            </span>
          </p>
          <div className="flex flex-wrap items-center gap-1.5 min-w-0">
            {g.relations.map((rel, i) => (
              <RelationBadge
                key={`${rel.facet}:${rel.target_id || rel.target}:${i}`}
                relation={rel}
              />
            ))}
          </div>
        </div>
      ))}

      {/* De-emphasized Terraform interpolation references — NOT real resource
          deps. Visually bucketed + muted so they don't read as dependencies. */}
      {references.length > 0 && (
        <div className="space-y-1">
          <p className="inline-flex items-center gap-1 text-[10px] font-medium uppercase tracking-wide text-text-4">
            <Link2 size={10} className="shrink-0" /> references
            <span className="ml-0.5 tabular-nums opacity-70">
              {references.length}
            </span>
            <span className="ml-1 font-normal normal-case opacity-70">
              (interpolation, not resource deps)
            </span>
          </p>
          <div className="flex flex-wrap items-center gap-1.5 min-w-0">
            {references.map((rel, i) => (
              <RelationBadge
                key={`ref:${rel.target_id || rel.target}:${i}`}
                relation={rel}
                muted
              />
            ))}
          </div>
        </div>
      )}

      {resource.source_file && (
        <RefLine
          repo={resource.repo}
          file={resource.source_file}
          line={resource.start_line ?? 0}
          name={resource.name}
          className="text-[11px]"
        />
      )}
    </div>
  );
}

function ResourceCard({
  resource,
  groupId,
  showRepo,
}: {
  resource: IaCResource;
  groupId: string;
  showRepo: boolean;
}) {
  const [expanded, setExpanded] = useState(false);

  const { groups, references } = useMemo(
    () => classifyRelations(resource.relations ?? []),
    [resource.relations],
  );

  const hasDetail =
    groups.length > 0 ||
    references.length > 0 ||
    (resource.properties?.length ?? 0) > 0 ||
    !!resource.source_file;

  const moduleLeaf = resource.module
    ? resource.module.split("/").slice(-1)[0]
    : null;

  return (
    <div className="flex flex-col rounded-lg border border-border bg-surface transition-colors hover:bg-surface-2">
      {/* Clean header: category · name · type · file:line · repo */}
      <button
        type="button"
        onClick={() => hasDetail && setExpanded((e) => !e)}
        aria-expanded={expanded}
        className={cn(
          "flex flex-col gap-1.5 px-3 py-2.5 text-left rounded-lg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
          !hasDetail && "cursor-default",
        )}
      >
        <div className="flex items-center gap-2 min-w-0">
          {resource.category && (
            <Badge
              tone={categoryTone(resource.category)}
              className="uppercase shrink-0"
            >
              {resource.category}
            </Badge>
          )}
          <span
            className="font-mono text-sm text-text truncate min-w-0"
            title={resource.name}
          >
            {resource.name}
          </span>
          {resource.resource_type && (
            <span
              className="font-mono text-[11px] text-text-4 truncate shrink-0 max-w-[40%]"
              title={resource.resource_type}
            >
              {resource.resource_type}
            </span>
          )}
          {hasDetail && (
            <span className="ml-auto shrink-0 text-text-4">
              {expanded ? (
                <ChevronDown size={14} />
              ) : (
                <ChevronRight size={14} />
              )}
            </span>
          )}
        </div>

        {/* Sub-header: module + file:line + repo, low-chrome. */}
        <div className="flex items-center gap-2 min-w-0 text-[11px] text-text-4">
          {moduleLeaf && (
            <span
              className="inline-flex items-center gap-1 shrink-0"
              title={`Module: ${resource.module}`}
            >
              <Layers size={10} className="shrink-0" />
              <span className="truncate max-w-[12rem]">{moduleLeaf}</span>
            </span>
          )}
          {resource.source_file && (
            <span
              className="font-mono truncate min-w-0"
              title={`${resource.source_file}:${resource.start_line ?? 0}`}
            >
              {resource.source_file}
              {resource.start_line ? `:${resource.start_line}` : ""}
            </span>
          )}
          {showRepo && resource.repo && (
            <RepoChip
              slug={resource.repo}
              groupId={groupId}
              maxLength={16}
            />
          )}
        </div>

        {/* Summarized relationship counts — grouped, deduped, top-N + "+N more".
            #4588: replaces the flat dump of a dozen duplicate badges. */}
        {(groups.length > 0 || references.length > 0) && !expanded && (
          <div className="flex flex-wrap items-center gap-1.5 min-w-0">
            {groups.map((g) => (
              <RelationSummaryPill key={g.facet} group={g} />
            ))}
            {references.length > 0 && (
              <span
                className="inline-flex items-center gap-1 rounded-md border border-border bg-surface-2 px-2 py-0.5 text-[11px] text-text-4 opacity-70"
                title={`${references.length} interpolation reference(s) — not real resource dependencies`}
              >
                <Link2 size={11} className="shrink-0" />
                references
                <span className="tabular-nums">{references.length}</span>
              </span>
            )}
          </div>
        )}
      </button>

      {/* Click-to-expand detail panel: full grouped relationship list. */}
      {expanded && hasDetail && (
        <div className="border-t border-border px-3 py-2.5">
          <RelationDetail
            groups={groups}
            references={references}
            resource={resource}
          />
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § A tool group
// ---------------------------------------------------------------------------

function ToolSection({
  group,
  groupId,
}: {
  group: IaCToolGroup;
  groupId: string;
}) {
  const resources = group.resources ?? [];
  const repos = useMemo(() => {
    const set = new Set<string>();
    for (const r of resources) if (r.repo) set.add(r.repo);
    return Array.from(set);
  }, [resources]);

  return (
    <Card>
      <CardBody className="space-y-2">
        <div className="flex items-center gap-2">
          <Boxes size={13} className="text-text-4 shrink-0" />
          <span className="font-mono text-sm font-medium text-text truncate" title={group.tool}>
            {group.tool}
          </span>
          {repos.map((slug) => (
            <RepoChip key={slug} slug={slug} groupId={groupId} maxLength={18} />
          ))}
          <span className="ml-auto text-xs text-text-4 tabular-nums">
            {group.count} {group.count === 1 ? "resource" : "resources"}
          </span>
        </div>
        <div className="space-y-2">
          {resources.map((r) => (
            <ResourceCard
              key={r.entity_id}
              resource={r}
              groupId={groupId}
              showRepo={repos.length > 1}
            />
          ))}
        </div>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Category roll-up (context)
// ---------------------------------------------------------------------------

function CategorySection({ counts }: { counts: Record<string, number> | null | undefined }) {
  const entries = useMemo(
    () =>
      Object.entries(counts ?? {}).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])),
    [counts],
  );
  if (entries.length === 0) return null;
  return (
    <Card>
      <CardBody className="space-y-2">
        <div className="flex items-center gap-2">
          <Database size={13} className="text-text-4 shrink-0" />
          <span className="text-sm font-medium text-text">By resource category</span>
          <span className="ml-auto text-xs text-text-4 tabular-nums">
            {entries.length}
          </span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {entries.map(([cat, n]) => (
            <Badge
              key={cat}
              tone={categoryTone(cat)}
              className="inline-flex items-center gap-1"
              title={`${n} ${cat} resource(s)`}
            >
              <span className="uppercase">{cat}</span>
              <span className="tabular-nums opacity-70">{n}</span>
            </Badge>
          ))}
        </div>
        <p className="text-[10px] text-text-4">
          Cross-tool resource_category — the single join key shared by every IaC
          tool. For Terraform it is recomputed from the resource type.
        </p>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Screen
// ---------------------------------------------------------------------------

type IaCView = "list" | "diagram";

/** Shared insight banner for both List and Diagram views (#4576 / #4604). */
function IaCInsight() {
  return (
    <InsightBanner
      storageKey="iac"
      human={
        <>
          Your infrastructure-as-code defined across Terraform/OpenTofu, AWS
          CDK, Pulumi, CloudFormation/SAM, Serverless Framework, and Bicep —
          every resource, its config, and how resources wire to each other (IAM
          grants, event sources, dependencies, and stack/module topology). Each
          resource is normalized into a shared{" "}
          <DefTerm
            term="resource category"
            def="A cross-tool join key (function, datastore, queue, secret, network…) that normalizes each tool's native resource type into one shared vocabulary."
          />
          .
        </>
      }
      agent={{
        tool: "archigraph_topology",
        example:
          "Before editing a Terraform module that defines a Lambda, an agent calls archigraph_topology to see which queues, datastores and IAM grants that function depends on across stacks — so it doesn't remove a network or secret another service still wires into.",
      }}
    />
  );
}

/** List | Diagram view toggle. */
function ViewToggle({ view, onChange }: { view: IaCView; onChange: (v: IaCView) => void }) {
  return (
    <div className="inline-flex overflow-hidden rounded-md border border-border">
      <button
        type="button"
        onClick={() => onChange("list")}
        className={cn(
          "inline-flex h-7 items-center gap-1 px-2.5 text-xs transition-colors",
          view === "list" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
        )}
        title="List view — resources grouped by tool"
      >
        <List size={12} /> List
      </button>
      <button
        type="button"
        onClick={() => onChange("diagram")}
        className={cn(
          "inline-flex h-7 items-center gap-1 border-l border-border px-2.5 text-xs transition-colors",
          view === "diagram" ? "bg-accent text-accent-text" : "bg-surface text-text-3 hover:bg-surface-2",
        )}
        title="Diagram view — the resource graph as an architecture diagram"
      >
        <Network size={12} /> Diagram
      </button>
    </div>
  );
}

export default function IaCScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const { data, isLoading, isError } = useIaC(groupId);
  const [toolFilter, setToolFilter] = useState<string>("all");
  const [view, setView] = useState<IaCView>("list");

  const groups = useMemo<IaCToolGroup[]>(() => data?.groups ?? [], [data]);

  const filtered = useMemo<IaCToolGroup[]>(() => {
    if (toolFilter === "all") return groups;
    return groups.filter((g) => g.tool === toolFilter);
  }, [groups, toolFilter]);

  const hasResources = (data?.total_resources ?? 0) > 0;

  // The diagram view fills the full height itself (its own canvas + controls),
  // so it is rendered outside the scrolling list container.
  if (!isLoading && !isError && hasResources && view === "diagram") {
    return (
      <div className="flex h-full flex-col bg-bg">
        <div className="space-y-2 border-b border-border bg-surface px-4 py-3">
          <div className="flex items-center gap-2">
            <ViewToggle view={view} onChange={setView} />
            <span className="text-xs text-text-4">
              Resource graph — resources by category, relations, grouped by
              module.
            </span>
          </div>
          <IaCInsight />
        </div>
        <div className="min-h-0 flex-1">
          <IaCDiagram report={data!} />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full bg-bg">
      <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4 space-y-4">
        {isLoading ? (
          <SkeletonRows />
        ) : isError ? (
          <ErrorState />
        ) : !hasResources ? (
          <EmptyState
            title="No infrastructure resources resolved"
            hint="No IaC resources were extracted for this group. They appear once a Terraform/OpenTofu, AWS CDK, Pulumi, CloudFormation/SAM, Serverless Framework, or Azure Bicep definition is indexed."
          />
        ) : (
          <>
            {/* View toggle (List | Diagram) */}
            <div className="flex items-center gap-2">
              <ViewToggle view={view} onChange={setView} />
            </div>

            {/* #4576: List view now carries the same insight banner
                the Diagram view does. */}
            <IaCInsight />

            {/* Summary */}
            <div className="flex flex-wrap gap-3">
              <SummaryStat label="Resources" value={data!.total_resources} />
              <SummaryStat label="With config" value={data!.with_props_count} />
              <SummaryStat label="IAM grants" value={data!.total_grants} />
              <SummaryStat label="Event sources" value={data!.total_event_sources} />
              <SummaryStat label="Dependencies" value={data!.total_dependencies} />
              <SummaryStat label="Outputs" value={data!.total_outputs} />
            </div>

            {/* Tool filter */}
            <div className="flex flex-wrap items-center gap-2">
              <Pill active={toolFilter === "all"} onClick={() => setToolFilter("all")}>
                All
              </Pill>
              {(data!.tools ?? []).map((t) => (
                <Pill
                  key={t}
                  active={toolFilter === t}
                  onClick={() => setToolFilter(t)}
                >
                  {t}
                </Pill>
              ))}
            </div>

            {/* Grouped resources */}
            {filtered.length === 0 ? (
              <EmptyState
                title="Nothing matches this filter"
                hint="No resources match the selected tool filter."
              />
            ) : (
              <div className="space-y-3">
                {filtered.map((g) => (
                  <ToolSection key={g.tool} group={g} groupId={groupId} />
                ))}
              </div>
            )}

            {/* Category roll-up (context) */}
            <CategorySection counts={data!.counts_by_category} />
          </>
        )}
      </div>
    </div>
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
