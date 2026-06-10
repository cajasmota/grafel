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
} from "lucide-react";

import { Badge, Card, CardBody, Pill } from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
import { cn } from "@/lib/utils";
import { useIaC } from "@/hooks/use-iac";
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

function RelationBadge({ relation }: { relation: IaCRelation }) {
  const { label, tone, Icon } = relationMeta(relation.facet);
  const arrow = relation.direction === "in" ? "←" : "→";
  const detail = relation.detail ? `.${relation.detail}` : "";

  // #4495: when the backend could not resolve the endpoint to a display name
  // (target_resolved === false), the target is a meaningless raw entity-id
  // hash. Render a friendlier fallback — the relation kind as the label, with
  // the raw id available on hover — instead of dumping the hash inline.
  const unresolved =
    relation.target_resolved === false || isRawId(relation.target);
  const display = unresolved
    ? relation.kind.toLowerCase().replace(/_/g, " ")
    : relation.target;
  const rawId = relation.target_id || relation.target;

  return (
    <Badge
      tone={tone}
      className="inline-flex items-center gap-1 max-w-full min-w-0"
      title={
        unresolved
          ? `${label}${detail} ${arrow} <unresolved ${relation.kind} target> (id ${rawId})`
          : `${label}${detail} ${arrow} ${relation.target} (${relation.kind})`
      }
    >
      <Icon size={11} className="shrink-0" />
      <span className="lowercase shrink-0">{label}</span>
      <span
        className={cn(
          "font-mono opacity-70 truncate",
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

function ResourceRow({ resource }: { resource: IaCResource }) {
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0 flex-wrap">
        {resource.category && (
          <Badge tone={categoryTone(resource.category)} className="uppercase shrink-0">
            {resource.category}
          </Badge>
        )}
        <span className="font-mono text-sm text-text truncate" title={resource.name}>
          {resource.name}
        </span>
        {resource.resource_type && (
          <span
            className="font-mono text-[11px] text-text-4 truncate"
            title={resource.resource_type}
          >
            {resource.resource_type}
          </span>
        )}
      </div>

      {/* Relation facets (grants / event-sources / dependencies / topology).
          #4495: own full-width wrapping row so chips never overflow the card
          edge — they wrap to multiple lines and each chip truncates long
          targets internally. */}
      {resource.relations.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 min-w-0">
          {resource.relations.map((rel, i) => (
            <RelationBadge
              key={`${rel.facet}:${rel.target_id || rel.target}:${i}`}
              relation={rel}
            />
          ))}
        </div>
      )}

      {/* Typed config properties */}
      {resource.properties.length > 0 && (
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
  const repos = useMemo(() => {
    const set = new Set<string>();
    for (const r of group.resources) if (r.repo) set.add(r.repo);
    return Array.from(set);
  }, [group.resources]);

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
          {group.resources.map((r) => (
            <ResourceRow key={r.entity_id} resource={r} />
          ))}
        </div>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Category roll-up (context)
// ---------------------------------------------------------------------------

function CategorySection({ counts }: { counts: Record<string, number> }) {
  const entries = useMemo(
    () =>
      Object.entries(counts).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])),
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

export default function IaCScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const { data, isLoading, isError } = useIaC(groupId);
  const [toolFilter, setToolFilter] = useState<string>("all");

  const groups = useMemo<IaCToolGroup[]>(() => data?.groups ?? [], [data]);

  const filtered = useMemo<IaCToolGroup[]>(() => {
    if (toolFilter === "all") return groups;
    return groups.filter((g) => g.tool === toolFilter);
  }, [groups, toolFilter]);

  const hasResources = (data?.total_resources ?? 0) > 0;

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
              {data!.tools.map((t) => (
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
