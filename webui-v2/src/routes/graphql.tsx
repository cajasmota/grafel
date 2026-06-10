/* ============================================================
   GraphQL — Resolver-effects view (#4255, epic #4249).

   Route: /g/:groupId/graphql

   GraphQL was previously only a `service_type` label on a Paths backend
   card. This screen surfaces the GraphQL extraction the graph already
   has: every framework (gqlgen / DGS / spring-graphql / pothos /
   type-graphql / async-graphql / graphql-php / lighthouse / caliban /
   graphql-kotlin / graphene / ariadne / strawberry) emits a canonical
   `verb=GRAPHQL` resolver endpoint carrying graphql_operation /
   graphql_root / graphql_field / framework. The link effect-propagation
   pass stamps effects (db_read/db_write/http_out/mutation with
   confidence) on those endpoints, and resolver-level auth is stamped as
   the flat auth contract when statically recoverable.

   Data: GET /api/graphql/{group} (handlers_graphql.go → handleGraphQL),
   raw-JSON GraphQLReport. Resolvers come grouped by parent SDL type.

   Layout mirrors Security / Links: full-height column, a summary stat
   row, an operation filter, and a grouped list of type → resolvers with
   effect badges + auth + a RefLine source ref. An SDL schema-type
   roll-up sits below as context. Reuses Card / Badge / Pill / Skeleton +
   RefLine + RepoChip.

   HONESTY: effects render only when the effect pass populated them;
   auth only when modeled. No fields are fabricated.
   ============================================================ */

import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import {
  Boxes,
  Database,
  Globe,
  Pencil,
  Lock,
  AlertTriangle,
  Network,
} from "lucide-react";

import { Badge, Card, CardBody, Pill } from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
import { cn } from "@/lib/utils";
import { useGraphQL } from "@/hooks/use-graphql";
import type {
  GraphQLEffect,
  GraphQLResolver,
  GraphQLSchemaType,
  GraphQLTypeGroup,
} from "@/data/types";

// ---------------------------------------------------------------------------
// § Operation styling
// ---------------------------------------------------------------------------

function opTone(op: string): "accent" | "warning" | "info" | "neutral" {
  switch ((op || "").toLowerCase()) {
    case "query":
      return "accent";
    case "mutation":
      return "warning";
    case "subscription":
      return "info";
    default:
      return "neutral";
  }
}

// ---------------------------------------------------------------------------
// § Effect badge
// ---------------------------------------------------------------------------

/** Map an effect token to {label, tone, Icon}. */
function effectMeta(name: string): {
  label: string;
  tone: "info" | "accent" | "warning" | "neutral";
  Icon: typeof Database;
} {
  const n = (name || "").toLowerCase();
  if (n.startsWith("db_")) {
    return {
      label: n === "db_write" ? "db write" : n === "db_read" ? "db read" : n.replace(/_/g, " "),
      tone: "info",
      Icon: Database,
    };
  }
  if (n.startsWith("http")) return { label: "http", tone: "accent", Icon: Globe };
  if (n.includes("mutation") || n.includes("write")) return { label: n.replace(/_/g, " "), tone: "warning", Icon: Pencil };
  return { label: n.replace(/_/g, " "), tone: "neutral", Icon: Database };
}

function EffectBadge({ effect }: { effect: GraphQLEffect }) {
  const { label, tone, Icon } = effectMeta(effect.name);
  const conf =
    effect.confidence != null ? ` ${Math.round(effect.confidence * 100)}%` : "";
  return (
    <Badge
      tone={tone}
      className="inline-flex items-center gap-1"
      title={
        effect.confidence != null
          ? `${effect.name} · confidence ${effect.confidence.toFixed(2)}`
          : effect.name
      }
    >
      <Icon size={11} />
      <span className="lowercase">{label}</span>
      {conf && <span className="tabular-nums opacity-70">{conf.trim()}</span>}
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
      <Network size={32} className="text-text-4" />
      <p className="text-md font-medium text-text">{title}</p>
      <p className="text-sm text-text-3 max-w-md">{hint}</p>
    </div>
  );
}

function ErrorState() {
  return (
    <div className="flex flex-col items-center py-16 text-center gap-3">
      <AlertTriangle size={32} className="text-danger" />
      <p className="text-md font-medium text-text">Could not load GraphQL resolvers</p>
      <p className="text-sm text-text-3 max-w-sm">
        The daemon returned an error. Confirm the group is indexed and the
        daemon is reachable, then retry.
      </p>
    </div>
  );
}

// ---------------------------------------------------------------------------
// § One resolver row
// ---------------------------------------------------------------------------

function ResolverRow({ resolver }: { resolver: GraphQLResolver }) {
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0 flex-wrap">
        <Badge tone={opTone(resolver.operation)} className="uppercase shrink-0">
          {resolver.operation || "field"}
        </Badge>
        <span className="font-mono text-sm text-text truncate" title={resolver.field}>
          {resolver.field}
        </span>
        {resolver.method && resolver.method !== resolver.field && (
          <span className="font-mono text-[11px] text-text-4 truncate" title={resolver.method}>
            → {resolver.method}()
          </span>
        )}

        <div className="ml-auto flex items-center gap-1.5 shrink-0 flex-wrap justify-end">
          {/* Effects */}
          {(resolver.effects?.length ?? 0) > 0 ? (
            resolver.effects.map((e) => <EffectBadge key={e.name} effect={e} />)
          ) : (
            <span
              className="text-[10px] text-text-4 italic"
              title="No effects recorded — the effect-propagation pass may not have run for this group, or no sinks were detected."
            >
              no effects recorded
            </span>
          )}

          {/* Auth */}
          {resolver.auth_required && (
            <Badge
              tone="success"
              className="inline-flex items-center gap-1"
              title={
                resolver.auth_method
                  ? `auth via ${resolver.auth_method}`
                  : "auth required"
              }
            >
              <Lock size={11} />
              {resolver.auth_roles && resolver.auth_roles.length > 0
                ? resolver.auth_roles.join(", ")
                : "auth"}
            </Badge>
          )}

          {resolver.framework && (
            <span className="text-[10px] font-mono text-text-4">{resolver.framework}</span>
          )}
        </div>
      </div>

      {resolver.source_file && (
        <RefLine
          repo={resolver.repo}
          file={resolver.source_file}
          line={resolver.start_line ?? 0}
          name={resolver.field}
          className="text-[11px]"
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § A parent-type group
// ---------------------------------------------------------------------------

function TypeSection({
  group,
  groupId,
}: {
  group: GraphQLTypeGroup;
  groupId: string;
}) {
  // Distinct repos in this type group (resolvers can span repos for the same
  // root name, e.g. "Query").
  const resolvers = group.resolvers ?? [];
  const repos = useMemo(() => {
    const set = new Set<string>();
    for (const r of resolvers) if (r.repo) set.add(r.repo);
    return Array.from(set);
  }, [resolvers]);

  return (
    <Card>
      <CardBody className="space-y-2">
        <div className="flex items-center gap-2">
          <Boxes size={13} className="text-text-4 shrink-0" />
          <span className="font-mono text-sm font-medium text-text truncate" title={group.parent_type}>
            {group.parent_type}
          </span>
          {repos.map((slug) => (
            <RepoChip key={slug} slug={slug} groupId={groupId} maxLength={18} />
          ))}
          <span className="ml-auto text-xs text-text-4 tabular-nums">
            {resolvers.length}{" "}
            {resolvers.length === 1 ? "resolver" : "resolvers"}
          </span>
        </div>
        <div className="space-y-2">
          {resolvers.map((r) => (
            <ResolverRow key={r.entity_id} resolver={r} />
          ))}
        </div>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Schema-type roll-up
// ---------------------------------------------------------------------------

function schemaKindTone(kind: string): "accent" | "info" | "warning" | "neutral" {
  switch (kind) {
    case "type":
      return "accent";
    case "interface":
      return "info";
    case "input":
      return "warning";
    default:
      return "neutral";
  }
}

function SchemaTypesSection({ types }: { types: GraphQLSchemaType[] | null | undefined }) {
  // Backend slice fields marshal to JSON `null` when empty (no `omitempty`),
  // so coalesce before any .length / .map to avoid white-screening on a group
  // that has no GraphQL data.
  const list = types ?? [];
  if (list.length === 0) return null;
  return (
    <Card>
      <CardBody className="space-y-2">
        <div className="flex items-center gap-2">
          <Database size={13} className="text-text-4 shrink-0" />
          <span className="text-sm font-medium text-text">Schema types (SDL)</span>
          <span className="ml-auto text-xs text-text-4 tabular-nums">
            {list.length}
          </span>
        </div>
        <div className="flex flex-wrap gap-1.5">
          {list.map((t) => (
            <Badge
              key={`${t.repo}:${t.name}:${t.kind}`}
              tone={schemaKindTone(t.kind)}
              className="inline-flex items-center gap-1"
              title={`${t.kind} ${t.name}${t.federated ? " · federated" : ""} · ${t.repo}`}
            >
              <span className="font-mono">{t.name}</span>
              <span className="opacity-60">{t.kind}</span>
              {t.federated && <span className="opacity-70">⨯fed</span>}
            </Badge>
          ))}
        </div>
        <p className="text-[10px] text-text-4">
          SDL definitions extracted from schema files across{" "}
          {new Set(list.map((t) => t.repo)).size} repo(s).
        </p>
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// § Screen
// ---------------------------------------------------------------------------

const OPERATIONS = ["query", "mutation", "subscription"] as const;

export default function GraphQLScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const { data, isLoading, isError } = useGraphQL(groupId);
  const [opFilter, setOpFilter] = useState<string>("all");

  const groups = useMemo<GraphQLTypeGroup[]>(() => data?.groups ?? [], [data]);

  const filtered = useMemo<GraphQLTypeGroup[]>(() => {
    if (opFilter === "all") return groups;
    return groups
      .map((g) => ({
        ...g,
        resolvers: (g.resolvers ?? []).filter((r) => r.operation === opFilter),
      }))
      .filter((g) => g.resolvers.length > 0);
  }, [groups, opFilter]);

  const hasResolvers = (data?.total_resolvers ?? 0) > 0;

  return (
    <div className="flex flex-col h-full bg-bg">
      <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4 space-y-4">
        {isLoading ? (
          <SkeletonRows />
        ) : isError ? (
          <ErrorState />
        ) : !hasResolvers ? (
          <>
            <EmptyState
              title="No GraphQL resolvers resolved"
              hint="No GraphQL resolver endpoints were extracted for this group. They appear once a schema-first or code-first GraphQL framework (gqlgen, DGS, spring-graphql, pothos, type-graphql, async-graphql, graphql-php, lighthouse, caliban, graphql-kotlin, graphene, ariadne, strawberry) is indexed."
            />
            {data && (
              <SchemaTypesSection types={data.schema_types} />
            )}
          </>
        ) : (
          <>
            {/* Summary */}
            <div className="flex flex-wrap gap-3">
              <SummaryStat label="Resolvers" value={data!.total_resolvers} />
              <SummaryStat label="Queries" value={data!.query_count} />
              <SummaryStat label="Mutations" value={data!.mutation_count} />
              <SummaryStat label="Subscriptions" value={data!.subscription_count} />
              <SummaryStat label="With effects" value={data!.with_effects_count} />
              <SummaryStat label="DB-touching" value={data!.resolvers_with_db_ops} />
              <SummaryStat label="With auth" value={data!.with_auth_count} />
            </div>

            {/* Frameworks */}
            {(data!.frameworks?.length ?? 0) > 0 && (
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-xs text-text-4">Frameworks:</span>
                {data!.frameworks.map((fw) => (
                  <Badge key={fw} tone="neutral" className="font-mono">
                    {fw}
                  </Badge>
                ))}
              </div>
            )}

            {/* Operation filter */}
            <div className="flex flex-wrap items-center gap-2">
              <Pill active={opFilter === "all"} onClick={() => setOpFilter("all")}>
                All
              </Pill>
              {OPERATIONS.map((op) => (
                <Pill
                  key={op}
                  active={opFilter === op}
                  onClick={() => setOpFilter(op)}
                >
                  {op}
                </Pill>
              ))}
            </div>

            {/* Grouped resolvers */}
            {filtered.length === 0 ? (
              <EmptyState
                title="Nothing matches this filter"
                hint="No resolvers match the selected operation filter."
              />
            ) : (
              <div className="space-y-3">
                {filtered.map((g) => (
                  <TypeSection key={g.parent_type} group={g} groupId={groupId} />
                ))}
              </div>
            )}

            {/* Schema-type roll-up (context) */}
            <SchemaTypesSection types={data!.schema_types} />
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
