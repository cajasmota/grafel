/* ============================================================
   routes/paths.tsx — Paths / API & Endpoints Explorer

   Design contract: docs/screens/paths.md
   Layout: two-pane — 520px list rail (left) + flex detail (right).
   At <1180px the detail collapses into a right drawer overlay.

   Data: usePaths, usePathDetail, useOrphans via TanStack Query.
   All network calls go through lib/api.ts (never raw fetch).
   ============================================================ */

import { useState, useMemo, useCallback, useRef, useEffect } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import {
  Search, X, ChevronRight, Lock, ExternalLink, Copy,
  Database, Zap, Globe, FlaskConical, TestTube, Server,
  Layers, Box, List, Maximize2,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge, Tabs, TabsList, TabsTrigger, TabsContent, Skeleton } from "@/components/ui";
import { usePaths, usePathDetail, useOrphans } from "@/hooks/use-paths";
import type {
  PathBackend, ControllerGroupShape, PathRoute, PathDetail,
  OrphanCaller, HttpVerb, OrphanReason, PathEntity,
} from "@/data/types";

/* ============================================================
   Color / semantic tokens — all via CSS vars, no hardcoded hex
   ============================================================ */

/** HTTP verb → token-mapped color class pair [bg/text, border]. */
const VERB_COLORS: Record<string, { bg: string; text: string; border: string }> = {
  GET:     { bg: "bg-[var(--pastel-1)]",  text: "text-[var(--pastel-1-ink)]",  border: "border-[var(--pastel-1)]" },
  POST:    { bg: "bg-[var(--pastel-2)]",  text: "text-[var(--pastel-2-ink)]",  border: "border-[var(--pastel-2)]" },
  PUT:     { bg: "bg-[var(--pastel-6)]",  text: "text-[var(--pastel-6-ink)]",  border: "border-[var(--pastel-6)]" },
  PATCH:   { bg: "bg-[var(--pastel-6)]",  text: "text-[var(--pastel-6-ink)]",  border: "border-[var(--pastel-6)]" },
  DELETE:  { bg: "bg-[var(--pastel-4)]",  text: "text-[var(--pastel-4-ink)]",  border: "border-[var(--pastel-4)]" },
  GRPC:    { bg: "bg-[var(--pastel-9)]",  text: "text-[var(--pastel-9-ink)]",  border: "border-[var(--pastel-9)]" },
  HEAD:    { bg: "bg-surface-2",           text: "text-text-3",                  border: "border-border" },
  OPTIONS: { bg: "bg-surface-2",           text: "text-text-3",                  border: "border-border" },
  ANY:     { bg: "bg-surface-2",           text: "text-text-3",                  border: "border-border" },
};

const SERVICE_TYPE_COLORS: Record<string, string> = {
  REST:    "bg-[var(--pastel-1)] text-[var(--pastel-1-ink)]",
  gRPC:    "bg-[var(--pastel-9)] text-[var(--pastel-9-ink)]",
  GraphQL: "bg-[var(--pastel-5)] text-[var(--pastel-5-ink)]",
};

const PARAM_IN_COLORS: Record<string, string> = {
  path:   "bg-[var(--info-soft)] text-[var(--info)]",
  query:  "bg-[var(--pastel-1)] text-[var(--pastel-1-ink)]",
  body:   "bg-[var(--success-soft)] text-[var(--success)]",
  header: "bg-surface-2 text-text-3",
};

const DOWNSTREAM_COLORS: Record<string, { dot: string; label: string; icon: React.ReactNode }> = {
  db:       { dot: "var(--warning)",       label: "DB",       icon: <Database size={12} /> },
  event:    { dot: "var(--pastel-4-ink)",  label: "Events",   icon: <Zap size={12} /> },
  queue:    { dot: "var(--pastel-4-ink)",  label: "Queues",   icon: <Layers size={12} /> },
  external: { dot: "var(--pastel-1-ink)",  label: "External", icon: <Globe size={12} /> },
  grpc:     { dot: "var(--pastel-9-ink)",  label: "gRPC",     icon: <Box size={12} /> },
};

const SEVERITY_STYLES: Record<OrphanReason, { dot: string; label: string; classes: string }> = {
  no_handler_found:  { dot: "var(--danger)",  label: "no handler",    classes: "bg-danger-soft text-danger" },
  dynamic_baseurl:   { dot: "var(--warning)", label: "dynamic baseURL", classes: "bg-warning-soft text-warning" },
  template_literal:  { dot: "var(--text-4)",  label: "template literal", classes: "bg-surface-2 text-text-3 border border-dashed border-border-strong" },
};

/* ============================================================
   Helpers
   ============================================================ */

function VerbChip({ verb, lg = false }: { verb: string; lg?: boolean }) {
  const c = VERB_COLORS[verb] ?? VERB_COLORS.ANY;
  return (
    <span
      className={cn(
        "inline-flex items-center h-5 px-1.5 rounded text-[10px] font-semibold border select-none shrink-0",
        c.bg, c.text, c.border,
        lg && "h-6 px-2 text-xs",
      )}
    >
      {verb}
    </span>
  );
}

/** Monospace path with dynamic {segments} and ${var} highlighted amber. */
function PathString({ path, className }: { path: string; className?: string }) {
  const parts: React.ReactNode[] = [];
  let i = 0;
  const re = /(\{[\w]+\}|\$\{[\w]+\})/g;
  let m;
  while ((m = re.exec(path)) !== null) {
    if (m.index > i) parts.push(<span key={`s${i}`}>{path.slice(i, m.index)}</span>);
    parts.push(
      <span key={`d${m.index}`} className="text-warning font-semibold">
        {m[0]}
      </span>,
    );
    i = m.index + m[0].length;
  }
  if (i < path.length) parts.push(<span key="tail">{path.slice(i)}</span>);
  return (
    <span className={cn("font-mono break-all", className)}>
      {parts}
    </span>
  );
}

function statusCodeClass(code: number): string {
  const c = Math.floor(code / 100);
  if (c === 2) return "text-success";
  if (c === 3) return "text-warning";
  return "text-danger";
}

function kindIcon(kind: string): React.ReactNode {
  switch (kind.toLowerCase()) {
    case "component": return <Box size={12} />;
    case "datastore":
    case "db":        return <Database size={12} />;
    case "event":     return <Zap size={12} />;
    case "queue":     return <Layers size={12} />;
    case "service":   return <Server size={12} />;
    case "externalapi": return <Globe size={12} />;
    case "function":  return <FlaskConical size={12} />;
    case "test":      return <TestTube size={12} />;
    default:          return <Box size={12} />;
  }
}

/* ============================================================
   Skeleton primitives — use shared Skeleton from UI layer
   ============================================================ */

function ListSkeleton() {
  return (
    <div className="p-3 space-y-2">
      {[80, 60, 70, 50, 65].map((w, i) => (
        <Skeleton key={i} w={`w-[${w}%]`} />
      ))}
    </div>
  );
}

function DetailSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton w="w-48" h="h-4" />
      <Skeleton w="w-72" h="h-6" />
      <Skeleton w="w-full" />
      <Skeleton w="w-4/5" />
      <Skeleton w="w-3/4" />
    </div>
  );
}

/* ============================================================
   List panel — route row + controller + backend sections
   ============================================================ */

function RouteRow({
  route,
  selected,
  onClick,
}: {
  route: PathRoute;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={`route-row-${route.path_hash}`}
      className={cn(
        "w-full text-left px-3 py-2 flex items-start gap-2 min-w-0",
        "transition-colors duration-100 focus-visible:outline-none",
        "focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] rounded-sm",
        selected
          ? "bg-accent-soft border-l-[3px] border-accent pl-[calc(0.75rem-3px)]"
          : "hover:bg-surface-2 border-l-[3px] border-transparent pl-[calc(0.75rem-3px)]",
      )}
    >
      {/* Verbs + path */}
      <div className="flex-1 min-w-0">
        <div className="flex flex-wrap items-center gap-1 mb-0.5">
          {route.verbs.map((v) => <VerbChip key={v} verb={v} />)}
          {route.is_webhook && (
            <span className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[10px] font-medium bg-[var(--info-soft)] text-[var(--info)] border border-[var(--info-soft)]">
              🪝 {route.webhook_provider ?? "webhook"}
            </span>
          )}
        </div>
        <PathString path={route.path} className="text-xs text-text-2 leading-tight" />
      </div>
      {/* Right meta */}
      <div className="flex items-center gap-1 shrink-0 mt-0.5">
        {route.multiplicity > route.verbs.length && (
          <span className="text-[10px] text-text-4 tabular-nums">×{route.multiplicity}</span>
        )}
        {route.auth && (
          <span title="Authenticated">
            <Lock size={10} className="text-success" />
          </span>
        )}
        {route.repos.length > 1 ? (
          <span className="text-[10px] text-text-4 font-mono">+{route.repos.length}</span>
        ) : route.repos[0] ? (
          <span className="text-[10px] text-text-4 font-mono truncate max-w-[64px]">{route.repos[0]}</span>
        ) : null}
      </div>
    </button>
  );
}

function ControllerSection({
  backend,
  group,
  openMap,
  toggle,
  selectedHash,
  onSelect,
  search,
}: {
  backend: PathBackend;
  group: ControllerGroupShape;
  openMap: Record<string, boolean>;
  toggle: (k: string) => void;
  selectedHash: string | null;
  onSelect: (r: PathRoute) => void;
  search: string;
}) {
  const key = `${backend.id}::${group.id}`;
  const open = openMap[key] !== false;

  const matches = search
    ? group.routes.filter((r) => r.path.toLowerCase().includes(search.toLowerCase()))
    : group.routes;

  if (search && matches.length === 0) return null;

  return (
    <div>
      <button
        type="button"
        onClick={() => toggle(key)}
        className={cn(
          "w-full text-left flex items-center gap-1.5 px-3 py-1.5",
          "bg-bg-soft border-b border-border-soft text-xs text-text-2 font-medium",
          "hover:bg-surface-2 transition-colors duration-100",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
          "sticky z-[2]",
        )}
        style={{ top: "36px" }}
      >
        <ChevronRight
          size={11}
          className={cn("shrink-0 text-text-4 transition-transform duration-150", open && "rotate-90")}
        />
        <span className="font-mono truncate">{group.label}</span>
        <span className="text-text-4 font-normal truncate max-w-[120px]">· {group.file}</span>
        {group.is_webhook && <span className="text-[10px] text-info ml-1">🪝</span>}
        <span className="ml-auto text-text-4 tabular-nums">{matches.length}</span>
      </button>

      {(open || !!search) &&
        matches.map((route) => (
          <RouteRow
            key={route.path_hash}
            route={route}
            selected={selectedHash === route.path_hash}
            onClick={() => onSelect(route)}
          />
        ))}
    </div>
  );
}

function BackendSection({
  backend,
  openMap,
  toggle,
  selectedHash,
  onSelect,
  search,
}: {
  backend: PathBackend;
  openMap: Record<string, boolean>;
  toggle: (k: string) => void;
  selectedHash: string | null;
  onSelect: (r: PathRoute) => void;
  search: string;
}) {
  const key = `be::${backend.id}`;
  const open = openMap[key] !== false;

  const filteredGroups = search
    ? backend.groups.filter((g) =>
        g.routes.some((r) => r.path.toLowerCase().includes(search.toLowerCase())),
      )
    : backend.groups;

  if (search && filteredGroups.length === 0) return null;

  const totalEndpoints = backend.groups.reduce(
    (sum, g) => sum + g.routes.reduce((s, r) => s + (r.handlers_count || r.verbs.length), 0),
    0,
  );

  const svcClass =
    SERVICE_TYPE_COLORS[backend.service_type] ?? "bg-surface-2 text-text-3";

  const svcBorderColor =
    backend.service_type === "gRPC"
      ? "var(--pastel-9-ink)"
      : backend.service_type === "GraphQL"
        ? "var(--pastel-5-ink)"
        : "var(--pastel-1-ink)";

  return (
    <div
      className="border-b border-border"
      style={{
        background: `color-mix(in srgb, ${
          backend.service_type === "gRPC" ? "var(--pastel-9)" :
          backend.service_type === "GraphQL" ? "var(--pastel-5)" : "var(--pastel-1)"
        } 6%, transparent)`,
      }}
    >
      {/* Backend header — sticky */}
      <button
        type="button"
        onClick={() => toggle(key)}
        className={cn(
          "w-full text-left flex items-center gap-2 px-3 py-2",
          "sticky top-0 z-[3]",
          "text-sm font-semibold text-text",
          "hover:brightness-95 transition-all duration-100",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
        )}
        style={{
          background: "inherit",
          borderLeft: `3px solid ${svcBorderColor}`,
          paddingLeft: "calc(0.75rem - 3px)",
        }}
      >
        <ChevronRight
          size={13}
          className={cn(
            "shrink-0 text-text-3 transition-transform duration-150",
            open && "rotate-90",
          )}
        />
        <span className="font-mono">{backend.label}</span>
        <span className={cn("text-[10px] font-semibold px-1.5 py-0.5 rounded", svcClass)}>
          {backend.service_type}
        </span>
        {backend.cross_backend_refs && (
          <span className="inline-flex items-center gap-1 text-[10px] text-text-3 border border-dashed border-border-strong px-1 rounded">
            <ExternalLink size={9} /> cross-refs
          </span>
        )}
        {backend.any_rate > 0 && (
          <span className="text-[10px] text-text-4">ANY {backend.any_rate}</span>
        )}
        <span className="ml-auto text-xs text-text-3 tabular-nums">{totalEndpoints} endpoints</span>
      </button>

      {(open || !!search) &&
        filteredGroups.map((group) => (
          <ControllerSection
            key={group.id}
            backend={backend}
            group={group}
            openMap={openMap}
            toggle={toggle}
            selectedHash={selectedHash}
            onSelect={onSelect}
            search={search}
          />
        ))}
    </div>
  );
}

function FlatRouteList({
  backends,
  search,
  selectedHash,
  onSelect,
}: {
  backends: PathBackend[];
  search: string;
  selectedHash: string | null;
  onSelect: (r: PathRoute) => void;
}) {
  const allRoutes = useMemo(() => {
    const out: PathRoute[] = [];
    for (const b of backends) {
      for (const g of b.groups) {
        for (const r of g.routes) {
          out.push(r);
        }
      }
    }
    return out;
  }, [backends]);

  const filtered = search
    ? allRoutes.filter((r) => r.path.toLowerCase().includes(search.toLowerCase()))
    : allRoutes;

  if (filtered.length === 0) return null;

  return (
    <div>
      {filtered.map((route) => (
        <RouteRow
          key={route.path_hash}
          route={route}
          selected={selectedHash === route.path_hash}
          onClick={() => onSelect(route)}
        />
      ))}
    </div>
  );
}

/* ============================================================
   Detail pane — Swagger++ sections
   ============================================================ */

function SectionHeader({
  icon,
  title,
  count,
  open,
  onToggle,
}: {
  icon: React.ReactNode;
  title: string;
  count?: number;
  open: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className={cn(
        "w-full flex items-center gap-2 px-4 py-2.5 text-sm font-medium text-text-2",
        "border-b border-border bg-bg-soft hover:bg-surface-2 transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
      )}
    >
      <span className="text-text-3">{icon}</span>
      {title}
      {count !== undefined && (
        <span className="ml-1 text-xs text-text-4 tabular-nums">({count})</span>
      )}
      <ChevronRight
        size={13}
        className={cn(
          "ml-auto text-text-4 transition-transform duration-150",
          open && "rotate-90",
        )}
      />
    </button>
  );
}

function EntityRow({ entity }: { entity: PathEntity }) {
  return (
    <div className="flex items-center gap-2 py-1.5 px-4 hover:bg-surface-2 rounded group">
      <span className="text-text-4 shrink-0">{kindIcon(entity.kind)}</span>
      <span
        className="font-mono text-xs text-text truncate flex-1"
        title={entity.qualified_name}
      >
        {entity.label}
      </span>
      {entity.edge && (
        <span className="text-[10px] text-text-4 font-mono shrink-0">{entity.edge}</span>
      )}
      <span className="text-[10px] text-text-4 font-mono shrink-0 truncate max-w-[140px]">
        {entity.source_file}:{entity.start_line}
      </span>
      <span className="text-[10px] text-text-3 font-mono shrink-0">{entity.repo}</span>
    </div>
  );
}

function DetailPane({ detail }: { detail: PathDetail }) {
  const [verbFilter, setVerbFilter] = useState<string>("all");
  const [openSections, setOpenSections] = useState<Record<string, boolean>>({
    description: true,
    parameters: true,
    response: true,
    defined: true,
    calledby: true,
    downstream: true,
    sideeffects: false,
    tests: false,
  });

  const toggleSection = useCallback((k: string) => {
    setOpenSections((p) => ({ ...p, [k]: !p[k] }));
  }, []);

  // Filter verb-scoped data
  const filteredParams =
    verbFilter === "all"
      ? detail.parameters
      : detail.parameters.filter((p) => !p.verbs || p.verbs.includes(verbFilter as HttpVerb));

  const filteredShapes =
    verbFilter === "all"
      ? detail.response_shapes
      : detail.response_shapes.filter((s) => s.verb === verbFilter);

  const filteredHandlers =
    verbFilter === "all"
      ? detail.handlers
      : detail.handlers.filter((h) => h.verb === verbFilter);

  const totalDownstream =
    (detail.outbound.db?.length ?? 0) +
    (detail.outbound.event?.length ?? 0) +
    (detail.outbound.queue?.length ?? 0) +
    (detail.outbound.external?.length ?? 0) +
    (detail.outbound.grpc?.length ?? 0);

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Sticky header */}
      <div className="px-4 pt-4 pb-3 border-b border-border bg-surface shrink-0">
        {/* Large verb chips + path */}
        <div className="flex flex-wrap items-start gap-2 mb-2">
          <div className="flex flex-wrap gap-1.5 items-center">
            {detail.verbs.map((v) => <VerbChip key={v} verb={v} lg />)}
            {detail.is_webhook && (
              <span className="inline-flex items-center gap-1 h-6 px-2 rounded text-xs font-medium bg-[var(--info-soft)] text-[var(--info)]">
                🪝 {detail.webhook_provider ?? "webhook"}
              </span>
            )}
          </div>
          {/* Actions */}
          <div className="ml-auto flex items-center gap-1.5 shrink-0">
            <button
              type="button"
              title="Copy path hash"
              onClick={() => void navigator.clipboard.writeText(detail.path_hash)}
              className="inline-flex items-center gap-1 h-6 px-2 rounded text-xs text-text-3 hover:bg-surface-2 transition-colors"
            >
              <Copy size={11} /> {detail.path_hash.slice(0, 8)}…
            </button>
          </div>
        </div>

        <PathString
          path={detail.path}
          className="text-xl text-text leading-snug block mb-2"
        />

        {/* Chip row: auth, framework, repos */}
        <div className="flex flex-wrap items-center gap-1.5">
          {detail.auth ? (
            <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-success-soft text-success border border-success-soft">
              <Lock size={10} /> Auth · {detail.auth_scheme ?? "Bearer"}
            </span>
          ) : (
            <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-warning-soft text-warning">
              No auth
            </span>
          )}
          {detail.handlers[0]?.framework && (
            <span className="text-xs px-2 py-0.5 rounded-full bg-surface-2 text-text-3 font-mono">
              {detail.handlers[0].framework}
            </span>
          )}
          {detail.repos.slice(0, 2).map((r) => (
            <span key={r} className="text-xs px-2 py-0.5 rounded-full bg-surface-2 text-text-3 font-mono">
              {r}
            </span>
          ))}
          {detail.repos.length > 2 && (
            <span className="text-xs text-text-4">+{detail.repos.length - 2}</span>
          )}
        </div>
      </div>

      {/* Verb filter strip — only when >1 verb */}
      {detail.verbs.length > 1 && (
        <div className="flex items-center gap-1 px-4 py-2 border-b border-border bg-bg-soft shrink-0 overflow-x-auto">
          <span className="text-xs text-text-4 mr-1 shrink-0">View</span>
          <button
            type="button"
            onClick={() => setVerbFilter("all")}
            className={cn(
              "shrink-0 h-6 px-2 rounded text-xs transition-colors",
              verbFilter === "all"
                ? "bg-accent-soft text-accent-strong font-medium"
                : "text-text-3 hover:bg-surface-2",
            )}
          >
            All · {detail.verbs.length} verbs
          </button>
          {detail.verbs.map((v) => {
            const params = detail.parameters.filter((p) => !p.verbs || p.verbs.includes(v));
            return (
              <button
                key={v}
                type="button"
                onClick={() => setVerbFilter(v)}
                className={cn(
                  "shrink-0 h-6 px-2 rounded text-xs transition-colors flex items-center gap-1",
                  verbFilter === v
                    ? "bg-accent-soft text-accent-strong font-medium"
                    : "text-text-3 hover:bg-surface-2",
                )}
              >
                <VerbChip verb={v} />
                {params.length > 0 && <span className="text-text-4">· {params.length} params</span>}
              </button>
            );
          })}
        </div>
      )}

      {/* Scrollable sections */}
      <div className="flex-1 overflow-y-auto ag-scroll">
        {/* 1. Description */}
        <div>
          <SectionHeader
            icon={<Maximize2 size={14} />}
            title="Description"
            open={openSections.description}
            onToggle={() => toggleSection("description")}
          />
          {openSections.description && (
            <div className="px-4 py-3">
              {detail.description.has_docs ? (
                <>
                  <p className="text-sm text-text-2 leading-relaxed">{detail.description.summary}</p>
                  <div className="mt-2 flex items-center gap-2">
                    {detail.description.ai_generated && (
                      <span className="text-[10px] px-2 py-0.5 rounded-full bg-[var(--info-soft)] text-[var(--info)]">
                        AI-generated
                      </span>
                    )}
                    {detail.description.docs_path && (
                      <span className="text-xs text-accent font-mono">
                        {detail.description.docs_path}
                      </span>
                    )}
                  </div>
                </>
              ) : (
                <p className="text-sm text-text-4 italic">
                  No documentation yet. Run{" "}
                  <span className="font-mono not-italic text-text-3">/generate-docs</span> to create it.
                </p>
              )}
            </div>
          )}
        </div>

        {/* 2. Parameters */}
        <div>
          <SectionHeader
            icon={<List size={14} />}
            title="Parameters"
            count={filteredParams.length}
            open={openSections.parameters}
            onToggle={() => toggleSection("parameters")}
          />
          {openSections.parameters && (
            <div className="px-4 py-2">
              {filteredParams.length === 0 ? (
                <p className="text-xs text-text-4 py-1">None</p>
              ) : (
                <table className="w-full text-xs">
                  <thead>
                    <tr className="border-b border-border">
                      <th className="text-left py-1.5 text-text-4 font-medium pr-3">Name</th>
                      <th className="text-left py-1.5 text-text-4 font-medium pr-3">In</th>
                      <th className="text-left py-1.5 text-text-4 font-medium pr-3">Type</th>
                      <th className="text-left py-1.5 text-text-4 font-medium">Description</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredParams.map((p, i) => (
                      <tr key={i} className="border-b border-border-soft last:border-0">
                        <td className="py-1.5 pr-3 font-mono text-text">
                          {p.name}
                          {p.required && <span className="text-danger ml-0.5">*</span>}
                        </td>
                        <td className="py-1.5 pr-3">
                          <span className={cn("px-1.5 py-0.5 rounded-sm text-[10px] font-medium", PARAM_IN_COLORS[p.in] ?? "bg-surface-2 text-text-3")}>
                            {p.in}
                          </span>
                        </td>
                        <td className="py-1.5 pr-3 font-mono text-text-3">{p.type}</td>
                        <td className="py-1.5 text-text-3 leading-snug">{p.desc}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          )}
        </div>

        {/* 3. Response shapes */}
        <div>
          <SectionHeader
            icon={<Box size={14} />}
            title="Response shapes"
            count={filteredShapes.length}
            open={openSections.response}
            onToggle={() => toggleSection("response")}
          />
          {openSections.response && (
            <div className="px-4 py-2 space-y-2">
              {filteredShapes.length === 0 ? (
                <p className="text-xs text-text-4">None</p>
              ) : (
                filteredShapes.map((shape, i) => (
                  <div key={i} className="rounded-md border border-border overflow-hidden">
                    <div className="flex items-center gap-2 px-3 py-2 bg-bg-soft border-b border-border">
                      <VerbChip verb={shape.verb} />
                      <div className="flex flex-wrap gap-1">
                        {shape.status_codes.map((sc) => (
                          <span key={sc} className={cn("text-xs font-mono font-semibold", statusCodeClass(sc))}>
                            {sc}
                          </span>
                        ))}
                      </div>
                    </div>
                    <div className="px-3 py-2">
                      {shape.dynamic ? (
                        <span className="text-xs text-warning italic">
                          Dynamic — shape determined at runtime
                        </span>
                      ) : shape.keys && shape.keys.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {shape.keys.map((k) => (
                            <span key={k} className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-surface-2 text-text-2">
                              {k}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <span className="text-xs text-text-4">No body</span>
                      )}
                    </div>
                  </div>
                ))
              )}
            </div>
          )}
        </div>

        {/* 4. Defined in (handlers) */}
        <div>
          <SectionHeader
            icon={<Server size={14} />}
            title="Defined in"
            count={filteredHandlers.length}
            open={openSections.defined}
            onToggle={() => toggleSection("defined")}
          />
          {openSections.defined && (
            <div className="px-4 py-2 space-y-2">
              {filteredHandlers.length === 0 ? (
                <div className="rounded-md border border-warning bg-warning-soft/50 px-3 py-2 text-sm text-warning">
                  Orphan call — no backend handler found. Check the Orphan callers tab.
                </div>
              ) : (
                filteredHandlers.map((h, i) => {
                  const verbBorderColor =
                    h.verb === "GET" ? "var(--pastel-1-ink)"
                    : h.verb === "POST" ? "var(--pastel-2-ink)"
                    : h.verb === "DELETE" ? "var(--pastel-4-ink)"
                    : h.verb === "GRPC" ? "var(--pastel-9-ink)"
                    : "var(--pastel-6-ink)";

                  return (
                    <div
                      key={i}
                      className="rounded-md border border-border overflow-hidden"
                      style={{ borderLeftWidth: "3px", borderLeftColor: verbBorderColor }}
                    >
                      <div className="flex items-center gap-2 px-3 py-2 bg-bg-soft border-b border-border">
                        <VerbChip verb={h.verb} />
                        <span className="font-mono text-xs text-text truncate flex-1" title={h.qualified_name}>
                          {h.qualified_name}
                        </span>
                        <span
                          className="text-[10px] font-mono text-text-4 shrink-0"
                          title={`${h.source_file}:${h.start_line}`}
                        >
                          {h.source_file.split("/").slice(-1)[0]}:{h.start_line}
                        </span>
                      </div>
                      <div className="px-3 py-2">
                        {h.has_docs && h.docs_summary ? (
                          <p className="text-xs text-text-2 leading-relaxed">{h.docs_summary}</p>
                        ) : (
                          <p className="text-xs text-text-4 italic">No handler docs yet.</p>
                        )}
                        <div className="mt-1.5 flex flex-wrap gap-1">
                          {h.framework && (
                            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-2 text-text-3">
                              {h.framework}
                            </span>
                          )}
                          {h.language && (
                            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-2 text-text-3">
                              {h.language}
                            </span>
                          )}
                          {h.repo && (
                            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-2 text-text-3">
                              {h.repo}
                            </span>
                          )}
                          {h.auth && (
                            <span className="inline-flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded bg-success-soft text-success">
                              <Lock size={8} /> {h.auth}
                            </span>
                          )}
                        </div>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
          )}
        </div>

        {/* 5. Called by */}
        <div>
          <SectionHeader
            icon={<Box size={14} />}
            title="Called by"
            count={detail.inbound_fetches.length}
            open={openSections.calledby}
            onToggle={() => toggleSection("calledby")}
          />
          {openSections.calledby && (
            <div className="py-1">
              {detail.inbound_fetches.length === 0 ? (
                <p className="px-4 py-2 text-xs text-text-4">None</p>
              ) : (
                detail.inbound_fetches.map((e, i) => <EntityRow key={i} entity={e} />)
              )}
            </div>
          )}
        </div>

        {/* 6. Downstream */}
        <div>
          <SectionHeader
            icon={<Database size={14} />}
            title="Downstream"
            count={totalDownstream}
            open={openSections.downstream}
            onToggle={() => toggleSection("downstream")}
          />
          {openSections.downstream && (
            <div className="py-1">
              {totalDownstream === 0 ? (
                <p className="px-4 py-2 text-xs text-text-4">None</p>
              ) : (
                Object.entries(DOWNSTREAM_COLORS).map(([kind, meta]) => {
                  const entities = detail.outbound[kind as keyof typeof detail.outbound];
                  if (!entities || entities.length === 0) return null;
                  return (
                    <div key={kind}>
                      <div className="flex items-center gap-1.5 px-4 py-1.5">
                        <span className="size-1.5 rounded-full shrink-0" style={{ background: meta.dot }} />
                        <span className="text-xs text-text-3 font-medium">{meta.label}</span>
                        <span className="text-xs text-text-4 tabular-nums">{entities.length}</span>
                      </div>
                      {entities.map((e, i) => <EntityRow key={i} entity={e} />)}
                    </div>
                  );
                })
              )}
            </div>
          )}
        </div>

        {/* 7. Side effects */}
        <div>
          <SectionHeader
            icon={<Zap size={14} />}
            title="Side effects"
            count={detail.side_effects.length}
            open={openSections.sideeffects}
            onToggle={() => toggleSection("sideeffects")}
          />
          {openSections.sideeffects && (
            <div className="py-1">
              {detail.side_effects.length === 0 ? (
                <p className="px-4 py-2 text-xs text-text-4">None</p>
              ) : (
                detail.side_effects.map((e, i) => <EntityRow key={i} entity={e} />)
              )}
            </div>
          )}
        </div>

        {/* 8. Tests */}
        <div>
          <SectionHeader
            icon={<TestTube size={14} />}
            title="Tests"
            count={detail.tests.length}
            open={openSections.tests}
            onToggle={() => toggleSection("tests")}
          />
          {openSections.tests && (
            <div className="py-1">
              {detail.tests.length === 0 ? (
                <p className="px-4 py-2 text-xs text-text-4">None</p>
              ) : (
                detail.tests.map((e, i) => <EntityRow key={i} entity={e} />)
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

/* ============================================================
   Orphan callers tab
   ============================================================ */

function OrphanRow({ orphan }: { orphan: OrphanCaller }) {
  const [expanded, setExpanded] = useState(false);
  const sev = SEVERITY_STYLES[orphan.reason];

  return (
    <div className="border-b border-border-soft last:border-0">
      <button
        type="button"
        onClick={() => setExpanded((p) => !p)}
        className="w-full text-left flex items-start gap-3 px-4 py-3 hover:bg-surface-2 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
      >
        <VerbChip verb={orphan.method} />
        <div className="flex-1 min-w-0">
          <PathString path={orphan.url_pattern} className="text-sm" />
          <div className="mt-1 flex items-center gap-2 flex-wrap">
            <span className="font-mono text-xs text-text-3">{orphan.caller_label}</span>
            <span className="font-mono text-xs text-text-4 truncate max-w-[200px]">
              {orphan.caller_file}:{orphan.caller_line}
            </span>
          </div>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          <span className={cn("text-[10px] px-2 py-0.5 rounded-full font-medium", sev.classes)}>
            {sev.label}
          </span>
          <ChevronRight
            size={12}
            className={cn(
              "text-text-4 transition-transform duration-150",
              expanded && "rotate-90",
            )}
          />
        </div>
      </button>
      {expanded && orphan.repair_hint && (
        <div className="px-4 pb-3" style={{ paddingLeft: "calc(1rem + 36px + 12px)" }}>
          <p className="text-xs text-text-3 leading-relaxed bg-bg-soft rounded-md p-2 border border-border-soft">
            {orphan.repair_hint}
          </p>
        </div>
      )}
    </div>
  );
}

function OrphansPanel({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useOrphans(groupId, true);

  if (isLoading) {
    return (
      <div className="p-4 space-y-2">
        {[1, 2, 3].map((i) => (
          <Skeleton key={i} h="h-14" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div className="p-6 text-center">
        <p className="text-sm text-text-3">Couldn't load orphan callers.</p>
      </div>
    );
  }

  const orphans = data?.orphans ?? [];

  if (orphans.length === 0) {
    return (
      <div className="p-8 text-center">
        <p className="text-lg font-semibold text-text">No orphan callers found.</p>
        <p className="mt-1 text-sm text-text-3">
          All frontend FETCH calls resolve to a backend handler.
        </p>
      </div>
    );
  }

  const totals = data?.totals ?? { no_handler_found: 0, dynamic_baseurl: 0, template_literal: 0 };

  const bySeverity: Record<OrphanReason, OrphanCaller[]> = {
    no_handler_found: orphans.filter((o) => o.reason === "no_handler_found"),
    dynamic_baseurl: orphans.filter((o) => o.reason === "dynamic_baseurl"),
    template_literal: orphans.filter((o) => o.reason === "template_literal"),
  };

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Header */}
      <div className="px-4 py-3 border-b border-border flex items-center gap-3 flex-wrap shrink-0">
        <div>
          <h2 className="text-sm font-semibold text-text">Orphan callers · {orphans.length}</h2>
          <p className="text-xs text-text-4 mt-0.5">
            Frontend FETCH calls that resolve to no backend handler.
          </p>
        </div>
        <div className="ml-auto flex flex-wrap gap-1.5">
          {totals.no_handler_found > 0 && (
            <Badge dot="var(--danger)" tone="danger">
              {totals.no_handler_found} no handler
            </Badge>
          )}
          {totals.dynamic_baseurl > 0 && (
            <Badge dot="var(--warning)" tone="warning">
              {totals.dynamic_baseurl} dynamic baseURL
            </Badge>
          )}
          {totals.template_literal > 0 && (
            <Badge tone="neutral">
              {totals.template_literal} template literal
            </Badge>
          )}
        </div>
      </div>

      {/* Severity groups */}
      <div className="flex-1 overflow-y-auto ag-scroll">
        {(["no_handler_found", "dynamic_baseurl", "template_literal"] as OrphanReason[]).map((reason) => {
          const group = bySeverity[reason];
          if (group.length === 0) return null;
          const sev = SEVERITY_STYLES[reason];
          return (
            <div key={reason}>
              <div className="flex items-center gap-2 px-4 py-2 bg-bg-soft border-b border-border sticky top-0 z-[2]">
                <span className="size-2 rounded-full shrink-0" style={{ background: sev.dot }} />
                <span className="text-xs font-medium text-text-2">{sev.label}</span>
                <span className="text-xs text-text-4 tabular-nums">{group.length}</span>
              </div>
              {group.map((orphan) => (
                <OrphanRow key={orphan.id} orphan={orphan} />
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/* ============================================================
   Stat item helper
   ============================================================ */

function StatItem({ label, value }: { label: string; value: number }) {
  return (
    <span className="text-xs text-text-3 tabular-nums">
      <span className="font-mono font-medium text-text-2">{value.toLocaleString()}</span>{" "}
      {label}
    </span>
  );
}

/* ============================================================
   Main screen
   ============================================================ */

export default function PathsScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();

  // URL state
  const activeTab = (searchParams.get("tab") ?? "endpoints") as "endpoints" | "orphans";
  const selectedHash = searchParams.get("path");

  // Local list state
  const [search, setSearch] = useState("");
  const [groupedView, setGroupedView] = useState(true);
  const [openMap, setOpenMap] = useState<Record<string, boolean>>({});
  const searchRef = useRef<HTMLInputElement>(null);

  // Data
  const { data: pathsData, isLoading, isError } = usePaths(groupId);
  const { data: detail, isLoading: isDetailLoading } = usePathDetail(groupId, selectedHash);

  const backends = pathsData?.backends ?? [];
  const totals = pathsData?.totals;

  // "/" key focuses search
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      if (
        e.key === "/" &&
        !(e.target instanceof HTMLInputElement) &&
        !(e.target instanceof HTMLTextAreaElement)
      ) {
        e.preventDefault();
        searchRef.current?.focus();
      }
    }
    window.addEventListener("keydown", handleKey);
    return () => window.removeEventListener("keydown", handleKey);
  }, []);

  const selectRoute = useCallback(
    (hash: string) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        next.set("path", hash);
        next.set("tab", "endpoints");
        return next;
      });
    },
    [setSearchParams],
  );

  const setTab = useCallback(
    (tab: string) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        next.set("tab", tab);
        return next;
      });
    },
    [setSearchParams],
  );

  const toggleOpen = useCallback((k: string) => {
    setOpenMap((p) => ({ ...p, [k]: p[k] === false ? true : false }));
  }, []);

  const expandAll = useCallback(() => {
    const next: Record<string, boolean> = {};
    for (const b of backends) {
      next[`be::${b.id}`] = true;
      for (const g of b.groups) {
        next[`${b.id}::${g.id}`] = true;
      }
    }
    setOpenMap(next);
  }, [backends]);

  const collapseAll = useCallback(() => {
    const next: Record<string, boolean> = {};
    for (const b of backends) {
      next[`be::${b.id}`] = false;
      for (const g of b.groups) {
        next[`${b.id}::${g.id}`] = false;
      }
    }
    setOpenMap(next);
  }, [backends]);

  const filteredCount = useMemo(() => {
    if (!search) return totals?.routes ?? 0;
    return backends.reduce(
      (sum, b) =>
        sum +
        b.groups.reduce(
          (gs, g) =>
            gs + g.routes.filter((r) => r.path.toLowerCase().includes(search.toLowerCase())).length,
          0,
        ),
      0,
    );
  }, [search, backends, totals]);

  return (
    <div className="flex flex-col h-full overflow-hidden bg-bg" data-testid="paths-screen">
      {/* Sub-stats bar */}
      <div className="flex items-center gap-4 px-4 h-9 bg-bg-soft border-b border-border shrink-0">
        {totals ? (
          <>
            <StatItem label="paths" value={totals.routes} />
            <div className="w-px h-3 bg-border" />
            <StatItem label="endpoints" value={totals.endpoints} />
            <div className="w-px h-3 bg-border" />
            <StatItem label="controllers" value={totals.controllers} />
            <div className="w-px h-3 bg-border" />
            <StatItem label="backends" value={totals.backends} />
          </>
        ) : isLoading ? (
          <div className="flex items-center gap-3">
            <Skeleton w="w-20" h="h-2.5" />
            <Skeleton w="w-20" h="h-2.5" />
          </div>
        ) : null}
      </div>

      {/* Tabs */}
      <Tabs
        value={activeTab}
        onValueChange={setTab}
        className="flex flex-col flex-1 min-h-0"
      >
        <TabsList className="px-4 shrink-0 bg-surface border-b border-border rounded-none">
          <TabsTrigger value="endpoints">
            Endpoints
            {totals && (
              <span className="ml-1.5 text-text-4 text-xs tabular-nums">{totals.routes}</span>
            )}
          </TabsTrigger>
          <TabsTrigger value="orphans">
            Orphan callers
          </TabsTrigger>
        </TabsList>

        {/* Endpoints tab */}
        <TabsContent value="endpoints" className="flex-1 min-h-0 flex flex-col mt-0">
          <div className="flex flex-1 min-h-0">
            {/* List rail — 520px */}
            <aside
              className="w-[520px] shrink-0 flex flex-col border-r border-border overflow-hidden"
              data-testid="paths-list-rail"
            >
              {/* Toolbar */}
              <div className="flex items-center gap-2 px-3 py-2 border-b border-border shrink-0 bg-surface">
                {/* Search */}
                <div className="flex-1 flex items-center gap-1.5 h-7 rounded-md border border-border bg-bg-soft px-2 focus-within:ring-2 focus-within:ring-[var(--accent-ring)] focus-within:border-accent transition-all">
                  <Search size={12} className="text-text-4 shrink-0" />
                  <input
                    ref={searchRef}
                    type="text"
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    onKeyDown={(e) => e.key === "Escape" && setSearch("")}
                    placeholder="Search routes…"
                    aria-label="Search routes"
                    className="flex-1 bg-transparent text-xs text-text placeholder-text-4 outline-none min-w-0"
                  />
                  {search ? (
                    <>
                      <span className="text-[10px] text-text-4 tabular-nums shrink-0">
                        {filteredCount} of {totals?.routes ?? "…"}
                      </span>
                      <button
                        type="button"
                        onClick={() => setSearch("")}
                        aria-label="Clear search"
                        className="shrink-0 text-text-4 hover:text-text transition-colors"
                      >
                        <X size={10} />
                      </button>
                    </>
                  ) : (
                    <span className="text-[10px] text-text-4 shrink-0">
                      {totals?.routes ?? "…"} routes
                    </span>
                  )}
                </div>

                {/* Grouped / Flat */}
                <div className="flex items-center rounded-md border border-border overflow-hidden shrink-0">
                  <button
                    type="button"
                    onClick={() => setGroupedView(true)}
                    aria-label="Grouped view"
                    title="Grouped"
                    className={cn(
                      "h-7 w-7 flex items-center justify-center text-text-3 transition-colors",
                      groupedView ? "bg-accent-soft text-accent-strong" : "hover:bg-surface-2",
                    )}
                  >
                    <Layers size={13} />
                  </button>
                  <button
                    type="button"
                    onClick={() => setGroupedView(false)}
                    aria-label="Flat view"
                    title="Flat"
                    className={cn(
                      "h-7 w-7 flex items-center justify-center text-text-3 border-l border-border transition-colors",
                      !groupedView ? "bg-accent-soft text-accent-strong" : "hover:bg-surface-2",
                    )}
                  >
                    <List size={13} />
                  </button>
                </div>

                {/* Expand / Collapse */}
                {groupedView && (
                  <>
                    <button
                      type="button"
                      onClick={expandAll}
                      title="Expand all"
                      className="h-7 w-7 flex items-center justify-center rounded border border-border text-text-3 hover:bg-surface-2 shrink-0 transition-colors"
                    >
                      <Maximize2 size={12} />
                    </button>
                    <button
                      type="button"
                      onClick={collapseAll}
                      title="Collapse all"
                      className="h-7 w-7 flex items-center justify-center rounded border border-border text-text-3 hover:bg-surface-2 shrink-0 transition-colors"
                    >
                      <ChevronRight size={12} className="rotate-90" />
                    </button>
                  </>
                )}
              </div>

              {/* Route list */}
              <div className="flex-1 overflow-y-auto ag-scroll">
                {isLoading ? (
                  <ListSkeleton />
                ) : isError ? (
                  <div className="p-4 text-center">
                    <p className="text-sm text-text-3">Couldn't load paths.</p>
                  </div>
                ) : backends.length === 0 ? (
                  <div className="p-6 text-center">
                    <p className="text-sm text-text-3">No endpoints indexed yet.</p>
                    <p className="mt-1 text-xs text-text-4">Run the indexer, then reload.</p>
                  </div>
                ) : search && filteredCount === 0 ? (
                  <div className="p-6 text-center flex flex-col items-center gap-3">
                    <Search size={20} className="text-text-4" />
                    <p className="text-sm text-text-2">No routes match "{search}"</p>
                    <p className="text-xs text-text-4">
                      Clear the search to see all {totals?.routes ?? ""} routes.
                    </p>
                    <button
                      type="button"
                      onClick={() => setSearch("")}
                      className="text-xs text-accent hover:underline"
                    >
                      Clear search
                    </button>
                  </div>
                ) : groupedView ? (
                  backends.map((b) => (
                    <BackendSection
                      key={b.id}
                      backend={b}
                      openMap={openMap}
                      toggle={toggleOpen}
                      selectedHash={selectedHash}
                      onSelect={(r) => selectRoute(r.path_hash)}
                      search={search}
                    />
                  ))
                ) : (
                  <FlatRouteList
                    backends={backends}
                    search={search}
                    selectedHash={selectedHash}
                    onSelect={(r) => selectRoute(r.path_hash)}
                  />
                )}
              </div>
            </aside>

            {/* Detail pane */}
            <div
              className="flex-1 min-w-0 overflow-hidden bg-surface"
              data-testid="paths-detail-pane"
            >
              {!selectedHash ? (
                <div className="h-full flex flex-col items-center justify-center text-center p-8">
                  <Server size={32} className="text-text-4 mb-3" />
                  <p className="text-md font-medium text-text-2">Select a route</p>
                  <p className="mt-1 text-sm text-text-4">
                    Click any route to see its Swagger++ breakdown.
                  </p>
                </div>
              ) : isDetailLoading ? (
                <DetailSkeleton />
              ) : !detail ? (
                <div className="h-full flex flex-col items-center justify-center text-center p-8">
                  <p className="text-sm text-text-3">Route detail not found.</p>
                </div>
              ) : (
                <DetailPane detail={detail} />
              )}
            </div>
          </div>
        </TabsContent>

        {/* Orphans tab */}
        <TabsContent value="orphans" className="flex-1 min-h-0 mt-0">
          {activeTab === "orphans" && <OrphansPanel groupId={groupId} />}
        </TabsContent>
      </Tabs>
    </div>
  );
}
