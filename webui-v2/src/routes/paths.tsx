/* ============================================================
   routes/paths.tsx — Paths / API & Endpoints Explorer

   Design contract: docs/screens/paths.md
   Layout: two-pane — 520px list rail (left) + flex detail (right).
   At <1180px the detail collapses into a right drawer overlay.

   Data: usePaths, usePathDetail, useOrphans via TanStack Query.
   All network calls go through lib/api.ts (never raw fetch).

   Issue #1961: unified column layout rules:
     - Entity-ref sections (Defined in / Called by / Downstream / Side effects):
       all use RefLine with 60%/40% path/name split + repo chip right-anchored.
       No verb/kind/framework chips on any row — only the repo chip.
     - Param/response sections (Parameters / Response shapes):
       both use table-fixed with identical <colgroup> widths:
       Name 22% | In 12% | Type 28% | Description 38%.
   ============================================================ */

import { useState, useMemo, useCallback, useRef, useEffect, createContext, useContext } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import {
  Search, X, ChevronRight, ChevronLeft, Lock, ExternalLink, Copy,
  Database, Zap, Globe, TestTube, Server,
  Layers, Box, List, Maximize2, FolderTree, Boxes, AlertTriangle,
  Workflow,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge, Tabs, TabsList, TabsTrigger, TabsContent, Skeleton } from "@/components/ui";
import {
  Dialog, DialogContent, DialogTitle, DialogDescription,
} from "@/components/ui";
import { FlowDag } from "@/components/flow-dag";
import { RefLine } from "@/components/RefLine";
import { getRepoColor } from "@/lib/repo-color";
import { SectionHeader } from "@/components/SectionHeader";
import { ShapeTree, type ShapeTreeRow } from "@/components/ShapeTree";
import { AuthSection } from "@/components/Paths/EndpointDetail/AuthSection";
import { PostureSection } from "@/components/Paths/EndpointDetail/PostureSection";
import { AuthSeverityBadge } from "@/components/Paths/AuthSeverityBadge";
import {
  usePaths, usePathDetail, useOrphans, useAuthCoverageIndex,
  type AuthCoverageIndex,
} from "@/hooks/use-paths";
import type {
  PathBackend, ControllerGroupShape, PathRoute, PathDetail,
  OrphanCaller, HttpVerb, OrphanReason, PathEntity, HandlerDetail,
  PathParameter, ResponseShape, PerStatusResponse,
} from "@/data/types";

/* ============================================================
   Auth-coverage context (#4253) — provides a (verb, path) → finding
   resolver to deeply-nested rows without prop-drilling. Defaults to a
   no-op resolver so components render fine when no provider is mounted.
   ============================================================ */
const AuthCoverageContext = createContext<AuthCoverageIndex>({
  isLoading: false,
  lookup: () => undefined,
  lookupAny: () => undefined,
});

function useAuthFor() {
  return useContext(AuthCoverageContext);
}

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

/** Conventional verb sort order — matches Swagger / OpenAPI display convention. */
const VERB_ORDER: Record<string, number> = {
  GET: 0, POST: 1, PUT: 2, PATCH: 3, DELETE: 4, HEAD: 5, OPTIONS: 6, GRPC: 7, ANY: 8,
};

/** A single row in the flattened verb list — one route × one verb. */
interface VerbRow {
  route: PathRoute;
  verb: string;
  /** Stable key used for selection: `<path_hash>:<verb>` */
  rowKey: string;
}

/** Flatten a PathRoute array into per-verb rows, sorted by path asc then verb order. */
function flattenToVerbRows(routes: PathRoute[]): VerbRow[] {
  const rows: VerbRow[] = [];
  for (const route of routes) {
    const verbs = (route.verbs ?? []).slice().sort(
      (a, b) => (VERB_ORDER[a] ?? 99) - (VERB_ORDER[b] ?? 99),
    );
    for (const verb of verbs) {
      rows.push({ route, verb, rowKey: `${route.path_hash}:${verb}` });
    }
  }
  // Sort by path asc, then by verb order within the same path
  rows.sort((a, b) => {
    const pc = (a.route.path ?? "").localeCompare(b.route.path ?? "");
    if (pc !== 0) return pc;
    return (VERB_ORDER[a.verb] ?? 99) - (VERB_ORDER[b.verb] ?? 99);
  });
  return rows;
}

/* ============================================================
   Helpers
   ============================================================ */

function VerbChip({ verb, lg = false }: { verb: string; lg?: boolean }) {
  const c = VERB_COLORS[verb] ?? VERB_COLORS.ANY;
  return (
    <span
      className={cn(
        "inline-flex items-center justify-center h-5 min-w-14 rounded text-[10px] font-semibold border select-none shrink-0",
        c.bg, c.text, c.border,
        lg && "h-6 min-w-16 text-xs",
      )}
    >
      {verb}
    </span>
  );
}

/** Monospace path with dynamic {segments} and ${var} highlighted amber. */
function PathString({ path, className }: { path: string; className?: string }) {
  path = path ?? "";
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

/** Swagger-style one-verb-per-row. Each verb gets its own independently clickable row. */
function VerbRouteRow({
  row,
  selected,
  onClick,
}: {
  row: VerbRow;
  selected: boolean;
  onClick: () => void;
}) {
  const { route, verb } = row;
  const auth = useAuthFor();
  const finding = auth.lookup(verb, route.path);
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={`route-row-${route.path_hash}-${verb}`}
      className={cn(
        "w-full text-left px-3 py-1.5 flex items-center gap-2 min-w-0",
        "transition-colors duration-100 focus-visible:outline-none",
        "focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] rounded-sm",
        selected
          ? "bg-accent-soft border-l-[3px] border-accent pl-[calc(0.75rem-3px)]"
          : "hover:bg-surface-2 border-l-[3px] border-transparent pl-[calc(0.75rem-3px)]",
      )}
    >
      {/* Fixed-width verb chip column so all paths align */}
      <span className="w-[56px] shrink-0 flex items-center gap-1">
        <VerbChip verb={verb} />
        {route.is_webhook && (
          <span className="inline-flex items-center h-4 px-1 rounded text-[9px] font-medium bg-[var(--info-soft)] text-[var(--info)] border border-[var(--info-soft)]">
            🪝
          </span>
        )}
      </span>
      {/* Path */}
      <PathString path={route.path} className="flex-1 text-xs text-text-2 leading-tight min-w-0 truncate" />
      {/* Right meta */}
      <div className="flex items-center gap-1 shrink-0">
        <AuthSeverityBadge finding={finding} variant="row" />
        {route.auth && (
          <span title="Authenticated">
            <Lock size={10} className="text-success" />
          </span>
        )}
        {(route.repos?.length ?? 0) > 1 ? (
          <span className="text-[10px] text-text-4 font-mono">+{route.repos?.length}</span>
        ) : route.repos?.[0] ? (
          <span className="text-[10px] text-text-4 font-mono truncate max-w-[72px]">{route.repos[0]}</span>
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
  isRowSelected,
  onSelect,
  search,
}: {
  backend: PathBackend;
  group: ControllerGroupShape;
  openMap: Record<string, boolean>;
  toggle: (k: string) => void;
  selectedRowKey: string | null;
  isRowSelected: (rowKey: string) => boolean;
  onSelect: (r: PathRoute, verb: string) => void;
  search: string;
}) {
  const key = `${backend.id}::${group.id}`;
  const open = openMap[key] !== false;

  const groupRoutes = group.routes ?? [];
  const matchedRoutes = search
    ? groupRoutes.filter((r) => (r.path ?? "").toLowerCase().includes(search.toLowerCase()))
    : groupRoutes;

  if (search && matchedRoutes.length === 0) return null;

  const verbRows = flattenToVerbRows(matchedRoutes);

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
        <span className="ml-auto text-text-4 tabular-nums">{verbRows.length}</span>
      </button>

      {(open || !!search) &&
        verbRows.map((row) => (
          <VerbRouteRow
            key={row.rowKey}
            row={row}
            selected={isRowSelected(row.rowKey)}
            onClick={() => onSelect(row.route, row.verb)}
          />
        ))}
    </div>
  );
}

function BackendSection({
  backend,
  openMap,
  toggle,
  isRowSelected,
  onSelect,
  search,
}: {
  backend: PathBackend;
  openMap: Record<string, boolean>;
  toggle: (k: string) => void;
  selectedRowKey: string | null;
  isRowSelected: (rowKey: string) => boolean;
  onSelect: (r: PathRoute, verb: string) => void;
  search: string;
}) {
  const key = `be::${backend.id}`;
  const open = openMap[key] !== false;

  const backendGroups = backend.groups ?? [];
  const filteredGroups = search
    ? backendGroups.filter((g) =>
        (g.routes ?? []).some((r) => (r.path ?? "").toLowerCase().includes(search.toLowerCase())),
      )
    : backendGroups;

  if (search && filteredGroups.length === 0) return null;

  const totalEndpoints = backendGroups.reduce(
    (sum, g) =>
      sum +
      (g.routes ?? []).reduce((s, r) => s + (r.handlers_count || (r.verbs?.length ?? 0)), 0),
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
            selectedRowKey={null}
            isRowSelected={isRowSelected}
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
  isRowSelected,
  onSelect,
}: {
  backends: PathBackend[];
  search: string;
  selectedRowKey: string | null;
  isRowSelected: (rowKey: string) => boolean;
  onSelect: (r: PathRoute, verb: string) => void;
}) {
  const allRoutes = useMemo(() => {
    const out: PathRoute[] = [];
    for (const b of backends ?? []) {
      for (const g of b.groups ?? []) {
        for (const r of g.routes ?? []) {
          out.push(r);
        }
      }
    }
    return out;
  }, [backends]);

  const filteredRoutes = search
    ? allRoutes.filter((r) => (r.path ?? "").toLowerCase().includes(search.toLowerCase()))
    : allRoutes;

  const verbRows = useMemo(() => flattenToVerbRows(filteredRoutes), [filteredRoutes]);

  if (verbRows.length === 0) return null;

  return (
    <div>
      {verbRows.map((row) => (
        <VerbRouteRow
          key={row.rowKey}
          row={row}
          selected={isRowSelected(row.rowKey)}
          onClick={() => onSelect(row.route, row.verb)}
        />
      ))}
    </div>
  );
}

/* ============================================================
   Detail pane — Swagger++ sections
   ============================================================ */

/** EntityRow — wraps RefLine for PathEntity values (Downstream, Side-effects, Tests).
 *  Issue #1934: kind chip dropped from row — redundant with header-level metadata. */
function EntityRow({ entity }: { entity: PathEntity }) {
  return (
    <RefLine
      repo={entity.repo ?? ""}
      file={entity.source_file ?? ""}
      line={entity.start_line ?? 0}
      name={entity.label ?? entity.qualified_name ?? ""}
    />
  );
}

/** HandlerRefLine — renders a handler detail using the canonical RefLine format.
 *  Issue #1910: Defined-in section collapses from verbose card to one-line ref.
 *  Issue #1934: framework/kind chip removed from RefLine — shown in header strip.
 *  Issue #1957: verb chip dropped from Defined-in rows — verb is already shown
 *               in the panel header. Repo chip is now right-anchored inside RefLine.
 *  Issue #1961: auth/docs trailing badges dropped — only repo chip allowed on row.
 *               Auth is visible in the sticky header chip row above. */
function HandlerRefLine({ handler }: { handler: HandlerDetail }) {
  return (
    <RefLine
      repo={handler.repo ?? ""}
      file={handler.source_file ?? ""}
      line={handler.start_line ?? 0}
      name={handler.qualified_name ?? handler.verb}
    />
  );
}

/**
 * #1938 Phase 1 — per-status-code tab strip above the Response ShapeTree.
 * Renders one chip per status code; clicking selects it and re-renders the
 * ShapeTree for that status's type. Default-selected = lowest 2xx code.
 */
function StatusCodeChip({
  code,
  selected,
  onClick,
}: {
  code: number;
  selected: boolean;
  onClick: () => void;
}) {
  const tone =
    code >= 200 && code < 300
      ? { bg: "bg-[var(--pastel-2)]", text: "text-[var(--pastel-2-ink)]", border: "border-[var(--pastel-2)]" }
      : code >= 400 && code < 500
        ? { bg: "bg-[var(--pastel-4)]", text: "text-[var(--pastel-4-ink)]", border: "border-[var(--pastel-4)]" }
        : code >= 500
          ? { bg: "bg-danger-soft", text: "text-danger", border: "border-danger-soft" }
          : { bg: "bg-surface-2", text: "text-text-3", border: "border-border" };
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "inline-flex items-center justify-center h-5 px-2 rounded text-[11px] font-mono font-semibold border select-none transition-all",
        tone.bg, tone.text, tone.border,
        selected ? "ring-2 ring-accent ring-offset-1" : "opacity-70 hover:opacity-100",
      )}
    >
      {code}
    </button>
  );
}

function PerStatusResponseStrip({
  entries,
  groupId,
}: {
  entries: PerStatusResponse[];
  groupId: string;
}) {
  // Default to lowest 2xx code, or first entry if no 2xx.
  const defaultCode = entries.find((e) => e.status_code >= 200 && e.status_code < 300)?.status_code
    ?? entries[0]?.status_code;
  const [selected, setSelected] = useState<number | undefined>(defaultCode);
  const activeEntry = entries.find((e) => e.status_code === selected);

  if (entries.length === 0) return null;

  return (
    <div data-testid="per-status-strip">
      {/* Tab strip */}
      <div className="flex items-center gap-1.5 px-4 py-2 border-b border-border-soft">
        <span className="text-[10px] text-text-4 font-medium mr-1">Status</span>
        {entries.map((e) => (
          <StatusCodeChip
            key={e.status_code}
            code={e.status_code}
            selected={selected === e.status_code}
            onClick={() => setSelected(e.status_code)}
          />
        ))}
      </div>
      {/* ShapeTree for the selected status */}
      {activeEntry && (
        <ShapeTree
          groupId={groupId}
          rows={
            activeEntry.has_children && activeEntry.type_entity_id
              ? [
                  {
                    key: `status-${activeEntry.status_code}`,
                    name: "response",
                    inLabel: String(activeEntry.status_code),
                    inTone: "status" as ShapeTreeRow["inTone"],
                    type: activeEntry.type_name ?? "—",
                    type_entity_id: activeEntry.type_entity_id,
                    type_name_fallback: activeEntry.type_name,
                    has_children: true,
                  },
                ]
              : activeEntry.type_name
                ? [
                    {
                      key: `status-${activeEntry.status_code}`,
                      name: "response",
                      inLabel: String(activeEntry.status_code),
                      inTone: "status" as ShapeTreeRow["inTone"],
                      type: activeEntry.type_name,
                      has_children: false,
                    },
                  ]
                : []
          }
        />
      )}
    </div>
  );
}

/**
 * Refs #1935 Phase 1 — project a PathParameter onto the ShapeTree's
 * top-level row shape. Body params whose type resolves to a known
 * class entity carry `type_entity_id` + `has_children=true` so the
 * tree renders the expand glyph; everything else stays a terminal
 * row identical to the old table layout.
 *
 * #2113: cookie + form are now valid `in` values from the Java extractor.
 * Map them to the correct ShapeTreeRow tone so the colour-coded chips render.
 */
function paramToShapeTreeRow(p: PathParameter): ShapeTreeRow {
  // Map the `in` string to the union allowed by ShapeTreeRow["inTone"].
  // Unmapped values (e.g. "matrix") fall through to "path" as a safe default.
  const toneMap: Record<string, ShapeTreeRow["inTone"]> = {
    path:   "path",
    query:  "query",
    body:   "body",
    header: "header",
    cookie: "cookie",
    form:   "form",
  };
  return {
    key: `${p.in}:${p.name}`,
    name: p.name,
    inLabel: p.in,
    inTone: toneMap[p.in] ?? "path",
    type: p.type,
    desc: p.desc,
    required: p.required,
    type_entity_id: p.type_entity_id,
    type_name_fallback: p.type,
    has_children: !!p.has_children,
  };
}

/**
 * Refs #1935 Phase 1 — project a ResponseShape into one ShapeTree row
 * per status code. A response shape with no `type_name` resolved (the
 * common non-Java case) still renders, falling back to the legacy
 * "keys" list as the type token so the visual replacement is
 * non-regressive for languages outside Phase 1 scope.
 *
 * #2113: the [PUT] / [POST] verb chip is DEFINITIVELY removed from the
 * response row. The verb is already shown in the panel header. The `inLabel`
 * now shows ONLY the status code (e.g. "200"), or is omitted for shapes with
 * no resolved status code. This was previously `s.verb` or `${s.verb} ${status}`.
 */
function responseToShapeTreeRows(s: ResponseShape, idx: number): ShapeTreeRow[] {
  const statusCodes = (s.status_codes ?? []).length > 0 ? s.status_codes : [undefined as number | undefined];
  return statusCodes.map((status, j) => {
    const hasBody = !s.dynamic && (s.type_name || (s.keys && s.keys.length > 0));
    const typeDisplay = s.type_name
      ? s.type_name
      : s.dynamic
        ? "Dynamic"
        : hasBody
          ? (s.keys ?? []).join(", ")
          : "—";
    // #2113: show only the status code, NOT the verb (verb is in the panel header).
    const statusLabel = status !== undefined ? String(status) : undefined;
    return {
      key: `${idx}-${statusLabel ?? "nsc"}-${j}`,
      name: hasBody ? "response" : "(none)",
      inLabel: statusLabel,           // "200", "404", … or undefined (no chip)
      inTone: "status" as ShapeTreeRow["inTone"],
      type: typeDisplay,
      desc: s.dynamic ? "Shape determined at runtime" : undefined,
      type_entity_id: s.type_entity_id,
      type_name_fallback: s.type_name,
      has_children: !!s.has_children,
    };
  });
}

function DetailPane({ detail: rawDetail, initialVerb, groupId }: { detail: PathDetail; initialVerb?: string; groupId: string }) {
  const authForDetail = useAuthFor();
  // Real polyglot data can omit array/object fields entirely. Normalize once so
  // every downstream access is null-safe (#1536).
  const detail = {
    ...rawDetail,
    verbs: rawDetail.verbs ?? [],
    repos: rawDetail.repos ?? [],
    parameters: rawDetail.parameters ?? [],
    response_shapes: rawDetail.response_shapes ?? [],
    handlers: rawDetail.handlers ?? [],
    inbound_fetches: rawDetail.inbound_fetches ?? [],
    side_effects: rawDetail.side_effects ?? [],
    tests: rawDetail.tests ?? [],
    path_hash: rawDetail.path_hash ?? "",
    path: rawDetail.path ?? "",
    outbound: rawDetail.outbound ?? {},
  } as PathDetail;
  // Default to the verb selected in the list row (from URL), fall back to "all".
  const [verbFilter] = useState<string>(() => {
    if (initialVerb && (rawDetail.verbs ?? []).includes(initialVerb as HttpVerb)) return initialVerb;
    return "all";
  });
  const [openSections, setOpenSections] = useState<Record<string, boolean>>({
    description: true,
    auth: true,
    posture: false,
    contract: false,
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

  // Downstream-DAG modal (#4350) — the shared <FlowDag> rooted at this
  // endpoint. The verb passed disambiguates a multi-verb path hash (falls back
  // to the first verb when the list filter is "all").
  const [dagOpen, setDagOpen] = useState(false);
  const dagVerb = verbFilter !== "all" ? verbFilter : detail.verbs[0];

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
            {verbFilter !== "all" && <VerbChip key={verbFilter} verb={verbFilter} lg />}
            {verbFilter === "all" && detail.verbs.map((v) => <VerbChip key={v} verb={v} lg />)}
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
          <AuthSeverityBadge
            finding={authForDetail.lookupAny(detail.verbs, detail.path)}
            variant="header"
          />
          {detail.auth ? (
            <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-success-soft text-success border border-success-soft">
              <Lock size={10} /> Auth · {detail.auth_scheme ?? "Bearer"}
            </span>
          ) : (
            <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-surface-2 text-text-3">
              Auth: unknown
            </span>
          )}
          {detail.handlers[0]?.framework && (
            <span className="text-xs px-2 py-0.5 rounded-full bg-surface-2 text-text-3 font-mono">
              {detail.handlers[0].framework}
            </span>
          )}
          {detail.repos.slice(0, 2).map((r) => {
            // #4341: route the header repo tag through the shared repo-color
            // resolver (same as RefLine, #4323) so it gets the luminance-based
            // WCAG-AA foreground/background/border instead of low-contrast
            // bg-surface-2/text-text-3. Layout (pill shape) is unchanged.
            const repoColors = getRepoColor(r);
            return (
              <span
                key={r}
                className="text-xs px-2 py-0.5 rounded-full font-mono"
                style={{
                  background: repoColors.background,
                  color: repoColors.foreground,
                  border: `1px solid ${repoColors.border}`,
                }}
                title={r}
              >
                {r}
              </span>
            );
          })}
          {detail.repos.length > 2 && (
            <span className="text-xs text-text-4">+{detail.repos.length - 2}</span>
          )}
        </div>
      </div>

      {/* Scrollable sections */}
      <div className="flex-1 overflow-y-auto ag-scroll">
        {/* 1. Description */}
        <div>
          <SectionHeader
            icon={<Maximize2 size={14} />}
            title="Description"
            infoText="Human-prose description of this endpoint, generated by an LLM from the surrounding code. Edit via the archigraph docs skill."
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
                  No documentation yet — ask your coding agent to generate docs
                  for this group with the archigraph docs skill.
                </p>
              )}
            </div>
          )}
        </div>

        {/* 1b. Auth section (#2113) — rendered between Description and Parameters.
              Always shown (even for public endpoints) so auth posture is
              immediately visible without expanding a separate section.
              Uses auth_policy when present (Java, #1942 Phase 1); falls back
              to legacy auth/auth_scheme flags. */}
        <AuthSection detail={detail} />

        {/* 1c. Posture + Effective contract (#4254) — lazy-fetched sibling
              route. Posture surfaces deprecation / rate-limit / error-flow /
              feature-gates; Effective contract surfaces the per-verb status
              codes / serializer / permissions with an MRO-inherited tag. Both
              honest-empty (collapsed by default). */}
        <PostureSection
          groupId={groupId}
          pathHash={detail.path_hash}
          postureOpen={openSections.posture}
          contractOpen={openSections.contract}
          onTogglePosture={() => toggleSection("posture")}
          onToggleContract={() => toggleSection("contract")}
        />

        {/* 2. Parameters — ShapeTree subtree (#1935 Phase 1).
            Each parameter row is its own top-level entry; body params
            whose type resolves to a known class expand into a CONTAINS
            field subtree on click. */}
        <div>
          <SectionHeader
            icon={<List size={14} />}
            title="Parameters"
            count={filteredParams.length}
            infoText="Inputs the endpoint accepts — derived from path segments + handler method parameters (path, query, header, cookie, body, form)."
            open={openSections.parameters}
            onToggle={() => toggleSection("parameters")}
          />
          {openSections.parameters && (
            <ShapeTree
              groupId={groupId}
              rows={filteredParams.map((p) => paramToShapeTreeRow(p))}
            />
          )}
        </div>

        {/* 3. Response — ShapeTree subtree (#1935 Phase 1). Replaces the
            former "Response shapes" table. Each response shape becomes a
            top-level row; expandable when the return type resolves to a
            user-defined DTO.
            #1938 Phase 1 — when per_status_responses is non-empty (Java
            @APIResponse annotations present), render a per-status tab strip
            above the ShapeTree; the strip defaults to the lowest 2xx tab. */}
        <div>
          <SectionHeader
            icon={<Box size={14} />}
            title="Response"
            count={
              (detail.per_status_responses?.length ?? 0) > 0
                ? detail.per_status_responses!.length
                : filteredShapes.length
            }
            infoText="Response body types per HTTP status code, derived from handler return types + framework annotations. Expandable when the return type is a user-defined DTO."
            open={openSections.response}
            onToggle={() => toggleSection("response")}
          />
          {openSections.response && (
            (detail.per_status_responses?.length ?? 0) > 0
              ? (
                <PerStatusResponseStrip
                  entries={detail.per_status_responses!}
                  groupId={groupId}
                />
              )
              : (
                <ShapeTree
                  groupId={groupId}
                  rows={filteredShapes.flatMap((s, idx) => responseToShapeTreeRows(s, idx))}
                />
              )
          )}
        </div>

        {/* 4. Defined in (handlers) */}
        <div>
          <SectionHeader
            icon={<Server size={14} />}
            title="Defined in"
            count={filteredHandlers.length}
            infoText="Source location(s) where this endpoint is registered. Each row links to the file:line of the handler function."
            open={openSections.defined}
            onToggle={() => toggleSection("defined")}
          />
          {/* Issue #1910: Defined-in uses canonical RefLine row format. */}
          {openSections.defined && (
            <div className="py-1">
              {filteredHandlers.length === 0 ? (
                <div className="mx-4 my-2 rounded-md border border-warning bg-warning-soft/50 px-3 py-2 text-sm text-warning">
                  Orphan call — no backend handler found. Check the Orphan callers tab.
                </div>
              ) : (
                filteredHandlers.map((h, i) => (
                  <HandlerRefLine key={i} handler={h} />
                ))
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
            infoText="Inbound HTTP calls — code in OTHER repos that fetches this endpoint via http_endpoint_call edges."
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
          {/* The SectionHeader is a full-width button, so the "Flow" trigger is
              overlaid on the title's right edge (left of the chevron) rather
              than nested inside it — a nested <button> is invalid. It opens the
              shared <FlowDag> modal (#4350) rooted at this endpoint. */}
          <div className="relative">
            <SectionHeader
              icon={<Database size={14} />}
              title="Downstream"
              count={totalDownstream}
              infoText="Outbound dependencies — services / DB / external APIs this endpoint calls during request handling."
              open={openSections.downstream}
              onToggle={() => toggleSection("downstream")}
            />
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setDagOpen(true);
              }}
              title="Open the downstream flow DAG"
              className={cn(
                "absolute right-9 top-1/2 -translate-y-1/2 z-10",
                "inline-flex items-center gap-1 h-6 px-2 rounded text-xs font-medium",
                "text-text-3 bg-surface-2 hover:bg-bg-soft transition-colors",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
              )}
            >
              <Workflow size={12} /> Flow
            </button>
          </div>
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
            infoText="DB writes, queue publishes, file writes, or other observable mutations performed by this endpoint."
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
            infoText="Test cases that exercise this endpoint, found via REFERENCES edges from test files."
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

      {/* Downstream-DAG modal (#4350) — shared <FlowDag>. Mounted only while
          open so the DAG fetch is lazy; reuses the house Dialog primitive. */}
      <Dialog open={dagOpen} onOpenChange={setDagOpen}>
        <DialogContent
          className="max-w-[min(1100px,94vw)] w-full h-[82vh] p-0 flex flex-col overflow-hidden"
        >
          <div className="px-5 pt-4 pb-3 border-b border-border shrink-0">
            <DialogTitle className="flex items-center gap-2">
              <Workflow size={16} className="text-text-3" />
              Downstream flow
            </DialogTitle>
            <DialogDescription>
              {detail.path} — the endpoint's downstream as a branching DAG.
            </DialogDescription>
          </div>
          <div className="flex-1 min-h-0">
            {dagOpen && (
              <FlowDag
                groupId={groupId}
                pathHash={detail.path_hash}
                verb={dagVerb}
                enabled={dagOpen}
                className="h-full"
              />
            )}
          </div>
        </DialogContent>
      </Dialog>
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
   Backends overview — drill-down screen 1
   ============================================================ */

/** Accent color for a service type, used on overview cards + section borders. */
function serviceAccent(type: string): string {
  return type === "gRPC"
    ? "var(--pastel-9-ink)"
    : type === "GraphQL"
      ? "var(--pastel-5-ink)"
      : "var(--pastel-1-ink)";
}

function backendStats(b: PathBackend) {
  const groups = b.groups ?? [];
  let routes = 0;
  let endpoints = 0;
  const verbCounts: Record<string, number> = {};
  const repoSet = new Set<string>();
  for (const g of groups) {
    for (const r of g.routes ?? []) {
      routes += 1;
      endpoints += r.handlers_count || (r.verbs?.length ?? 0);
      for (const v of r.verbs ?? []) verbCounts[v] = (verbCounts[v] ?? 0) + 1;
      for (const rp of r.repos ?? []) repoSet.add(rp);
    }
  }
  return { controllers: groups.length, routes, endpoints, verbCounts, repos: [...repoSet] };
}

function BackendCard({
  backend,
  onOpen,
}: {
  backend: PathBackend;
  onOpen: () => void;
}) {
  const { controllers, routes, endpoints, verbCounts } = backendStats(backend);
  const accent = serviceAccent(backend.service_type);
  const svcClass = SERVICE_TYPE_COLORS[backend.service_type] ?? "bg-surface-2 text-text-3";
  const verbs = Object.entries(verbCounts).sort((a, b) => b[1] - a[1]);

  return (
    <button
      type="button"
      onClick={onOpen}
      data-testid={`backend-card-${backend.id}`}
      className={cn(
        "group text-left rounded-lg border border-border bg-surface overflow-hidden",
        "hover:border-border-strong hover:shadow-sm transition-all duration-100",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
      )}
      style={{ borderTop: `3px solid ${accent}` }}
    >
      <div className="p-4">
        <div className="flex items-start gap-2">
          <Boxes size={16} className="text-text-3 mt-0.5 shrink-0" />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="font-mono text-sm font-semibold text-text truncate">{backend.label}</span>
              <span className={cn("text-[10px] font-semibold px-1.5 py-0.5 rounded", svcClass)}>
                {backend.service_type}
              </span>
            </div>
            {backend.id !== backend.label && (
              <span className="block text-[11px] text-text-4 font-mono truncate mt-0.5">{backend.id}</span>
            )}
          </div>
          <ChevronRight
            size={16}
            className="text-text-4 shrink-0 group-hover:translate-x-0.5 transition-transform"
          />
        </div>

        {/* Counts */}
        <div className="mt-3 flex items-center gap-4">
          <div className="flex flex-col">
            <span className="text-lg font-semibold text-text tabular-nums leading-none">{routes}</span>
            <span className="text-[10px] text-text-4 mt-0.5">paths</span>
          </div>
          <div className="flex flex-col">
            <span className="text-lg font-semibold text-text tabular-nums leading-none">{endpoints}</span>
            <span className="text-[10px] text-text-4 mt-0.5">endpoints</span>
          </div>
          <div className="flex flex-col">
            <span className="text-lg font-semibold text-text tabular-nums leading-none">{controllers}</span>
            <span className="text-[10px] text-text-4 mt-0.5">controllers</span>
          </div>
        </div>

        {/* Verb breakdown */}
        {verbs.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center gap-1">
            {verbs.map(([v, n]) => (
              <span key={v} className="inline-flex items-center gap-1">
                <VerbChip verb={v} />
                <span className="text-[10px] text-text-4 tabular-nums">{n}</span>
              </span>
            ))}
          </div>
        )}

        {/* Footer chips */}
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          {backend.language && (
            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-2 text-text-3">
              {backend.language}
            </span>
          )}
          {backend.framework && (
            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-2 text-text-3">
              {backend.framework}
            </span>
          )}
          {backend.cross_backend_refs && (
            <span className="inline-flex items-center gap-1 text-[10px] text-text-3 border border-dashed border-border-strong px-1 rounded">
              <ExternalLink size={9} /> cross-refs
            </span>
          )}
          {backend.any_rate > 0 && (
            <span className="text-[10px] text-warning">ANY {backend.any_rate}</span>
          )}
        </div>
      </div>
    </button>
  );
}

function BackendsOverview({
  backends,
  search,
  onOpenBackend,
}: {
  backends: PathBackend[];
  search: string;
  onOpenBackend: (id: string) => void;
}) {
  const filtered = useMemo(() => {
    if (!search) return backends;
    const q = search.toLowerCase();
    return backends.filter(
      (b) =>
        b.label.toLowerCase().includes(q) ||
        b.id.toLowerCase().includes(q) ||
        (b.groups ?? []).some((g) =>
          (g.routes ?? []).some((r) => (r.path ?? "").toLowerCase().includes(q)),
        ),
    );
  }, [backends, search]);

  if (filtered.length === 0) {
    return (
      <div className="p-10 text-center flex flex-col items-center gap-3">
        <Search size={22} className="text-text-4" />
        <p className="text-sm text-text-2">No backends match "{search}"</p>
      </div>
    );
  }

  return (
    <div className="p-4 overflow-y-auto ag-scroll h-full">
      <div className="grid gap-3 [grid-template-columns:repeat(auto-fill,minmax(260px,1fr))]">
        {filtered.map((b) => (
          <BackendCard key={b.id} backend={b} onOpen={() => onOpenBackend(b.id)} />
        ))}
      </div>
    </div>
  );
}

/* ============================================================
   Breadcrumb — drill-down navigation
   ============================================================ */

function Breadcrumb({
  backend,
  pathLabel,
  onAll,
  onBackend,
}: {
  backend: PathBackend | null;
  pathLabel: string | null;
  onAll: () => void;
  onBackend: () => void;
}) {
  return (
    <nav className="flex items-center gap-1 text-xs min-w-0" aria-label="Breadcrumb">
      <button
        type="button"
        onClick={onAll}
        className={cn(
          "inline-flex items-center gap-1 px-1.5 h-6 rounded hover:bg-surface-2 transition-colors shrink-0",
          backend ? "text-text-3" : "text-text font-medium",
        )}
      >
        <FolderTree size={12} /> All backends
      </button>
      {backend && (
        <>
          <ChevronRight size={11} className="text-text-4 shrink-0" />
          <button
            type="button"
            onClick={onBackend}
            className={cn(
              "inline-flex items-center gap-1 px-1.5 h-6 rounded hover:bg-surface-2 transition-colors min-w-0",
              pathLabel ? "text-text-3" : "text-text font-medium",
            )}
          >
            <Boxes size={12} className="shrink-0" />
            <span className="font-mono truncate max-w-[180px]">{backend.label}</span>
          </button>
        </>
      )}
      {backend && pathLabel && (
        <>
          <ChevronRight size={11} className="text-text-4 shrink-0" />
          <span className="inline-flex items-center px-1.5 h-6 text-text font-mono font-medium truncate max-w-[220px]">
            {pathLabel}
          </span>
        </>
      )}
    </nav>
  );
}

/* ============================================================
   Main screen
   ============================================================ */

export default function PathsScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [searchParams, setSearchParams] = useSearchParams();

  // URL state — backend drives the drill-down level.
  const activeTab = (searchParams.get("tab") ?? "endpoints") as "endpoints" | "orphans";
  const selectedHash = searchParams.get("path");
  const selectedVerb = searchParams.get("verb") ?? undefined;
  const selectedBackendId = searchParams.get("backend");

  // Stable row key: "<hash>:<verb>" for exact-verb highlight; just "<hash>" for legacy deep-links.
  const selectedRowKey = selectedHash
    ? selectedVerb ? `${selectedHash}:${selectedVerb}` : selectedHash
    : null;

  // Local list state
  const [search, setSearch] = useState("");
  const [groupedView, setGroupedView] = useState(true);
  const [openMap, setOpenMap] = useState<Record<string, boolean>>({});
  const searchRef = useRef<HTMLInputElement>(null);

  // Data
  const { data: pathsData, isLoading, isError } = usePaths(groupId);
  const { data: detail, isLoading: isDetailLoading } = usePathDetail(groupId, selectedHash);
  // Auth-coverage overlay (#4253) — reuses the Security screen's cached query
  // and indexes findings by method+path so rows + detail header can badge.
  const authIndex = useAuthCoverageIndex(groupId);

  const allBackends = pathsData?.backends ?? [];
  const totals = pathsData?.totals;
  const orphanCount = totals?.orphans;

  // Selected backend (drill-down level 2). When set, the rail scopes to it.
  const selectedBackend = useMemo(
    () => allBackends.find((b) => b.id === selectedBackendId) ?? null,
    [allBackends, selectedBackendId],
  );
  // Backends rendered in the rail/tree: just the selected one, or all (legacy).
  const backends = selectedBackend ? [selectedBackend] : allBackends;

  // Auto-enter the single backend so a one-service platform skips the overview.
  useEffect(() => {
    if (
      activeTab === "endpoints" &&
      !selectedBackendId &&
      !selectedHash &&
      allBackends.length === 1
    ) {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        next.set("backend", allBackends[0].id);
        return next;
      });
    }
  }, [activeTab, selectedBackendId, selectedHash, allBackends, setSearchParams]);

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

  /** isRowSelected: matches exact <hash>:<verb> or any verb when legacy hash-only key. */
  const isRowSelected = useCallback(
    (rowKey: string): boolean => {
      if (!selectedRowKey) return false;
      if (rowKey === selectedRowKey) return true;
      // Legacy deep-link has no ":" — highlight all verb rows for that path
      if (!selectedRowKey.includes(":")) return rowKey.startsWith(`${selectedRowKey}:`);
      return false;
    },
    [selectedRowKey],
  );

  const selectRoute = useCallback(
    (hash: string, verb?: string, backendId?: string) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        next.set("path", hash);
        next.set("tab", "endpoints");
        if (verb) next.set("verb", verb); else next.delete("verb");
        if (backendId) next.set("backend", backendId);
        return next;
      });
    },
    [setSearchParams],
  );

  const openBackend = useCallback(
    (id: string) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        next.set("backend", id);
        next.delete("path");
        return next;
      });
      setSearch("");
    },
    [setSearchParams],
  );

  const goAllBackends = useCallback(() => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete("backend");
      next.delete("path");
      return next;
    });
    setSearch("");
  }, [setSearchParams]);

  const goBackendRoot = useCallback(() => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete("path");
      return next;
    });
  }, [setSearchParams]);

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
      for (const g of b.groups ?? []) {
        next[`${b.id}::${g.id}`] = true;
      }
    }
    setOpenMap(next);
  }, [backends]);

  const collapseAll = useCallback(() => {
    const next: Record<string, boolean> = {};
    for (const b of backends) {
      next[`be::${b.id}`] = false;
      for (const g of b.groups ?? []) {
        next[`${b.id}::${g.id}`] = false;
      }
    }
    setOpenMap(next);
  }, [backends]);

  // Routes within the scoped (selected) backend — drives the rail labels so the
  // count reflects the backend you drilled into, not the whole platform.
  const scopedRouteCount = useMemo(
    () =>
      backends.reduce(
        (sum, b) => sum + (b.groups ?? []).reduce((gs, g) => gs + (g.routes ?? []).length, 0),
        0,
      ),
    [backends],
  );

  const filteredCount = useMemo(() => {
    if (!search) return scopedRouteCount;
    return backends.reduce(
      (sum, b) =>
        sum +
        (b.groups ?? []).reduce(
          (gs, g) =>
            gs +
            (g.routes ?? []).filter((r) =>
              (r.path ?? "").toLowerCase().includes(search.toLowerCase()),
            ).length,
          0,
        ),
      0,
    );
  }, [search, backends, scopedRouteCount]);

  return (
    <AuthCoverageContext.Provider value={authIndex}>
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
            {orphanCount !== undefined && orphanCount > 0 ? (
              <span
                className="ml-1.5 inline-flex items-center gap-0.5 text-xs tabular-nums text-warning"
                data-testid="orphan-count-badge"
              >
                <AlertTriangle size={11} className="shrink-0" />
                {orphanCount}
              </span>
            ) : orphanCount !== undefined ? (
              <span className="ml-1.5 text-text-4 text-xs tabular-nums" data-testid="orphan-count-badge">
                {orphanCount}
              </span>
            ) : null}
          </TabsTrigger>
        </TabsList>

        {/* Endpoints tab */}
        <TabsContent value="endpoints" className="flex-1 min-h-0 flex flex-col mt-0">
          {/* Breadcrumb bar */}
          <div className="flex items-center gap-2 px-3 h-9 border-b border-border bg-surface shrink-0">
            {selectedBackend && (
              <button
                type="button"
                onClick={selectedHash ? goBackendRoot : goAllBackends}
                aria-label="Back"
                title="Back"
                className="h-6 w-6 flex items-center justify-center rounded text-text-3 hover:bg-surface-2 shrink-0 transition-colors"
              >
                <ChevronLeft size={14} />
              </button>
            )}
            <Breadcrumb
              backend={selectedBackend}
              pathLabel={selectedHash ? (detail?.path ?? "endpoint") : null}
              onAll={goAllBackends}
              onBackend={goBackendRoot}
            />
            {/* Overview-level search */}
            {!selectedBackend && allBackends.length > 1 && (
              <div className="ml-auto flex items-center gap-1.5 h-7 rounded-md border border-border bg-bg-soft px-2 w-64 focus-within:ring-2 focus-within:ring-[var(--accent-ring)] focus-within:border-accent transition-all">
                <Search size={12} className="text-text-4 shrink-0" />
                <input
                  ref={searchRef}
                  type="text"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  onKeyDown={(e) => e.key === "Escape" && setSearch("")}
                  placeholder="Filter backends…"
                  aria-label="Filter backends"
                  className="flex-1 bg-transparent text-xs text-text placeholder-text-4 outline-none min-w-0"
                />
              </div>
            )}
          </div>

          {/* Overview (no backend selected) */}
          {!selectedBackend ? (
            isLoading ? (
              <ListSkeleton />
            ) : isError ? (
              <div className="p-6 text-center">
                <p className="text-sm text-text-3">Couldn't load paths.</p>
              </div>
            ) : allBackends.length === 0 ? (
              <div className="p-8 text-center">
                <p className="text-sm text-text-3">No endpoints indexed yet.</p>
                <p className="mt-1 text-xs text-text-4">Run the indexer, then reload.</p>
              </div>
            ) : (
              <BackendsOverview
                backends={allBackends}
                search={search}
                onOpenBackend={openBackend}
              />
            )
          ) : (
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
                        {filteredCount} of {scopedRouteCount}
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
                      {scopedRouteCount} routes
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
                      Clear the search to see all {scopedRouteCount} routes.
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
                      selectedRowKey={selectedRowKey}
                      isRowSelected={isRowSelected}
                      onSelect={(r, verb) => selectRoute(r.path_hash, verb, b.id)}
                      search={search}
                    />
                  ))
                ) : (
                  <FlatRouteList
                    backends={backends}
                    search={search}
                    selectedRowKey={selectedRowKey}
                    isRowSelected={isRowSelected}
                    onSelect={(r, verb) => selectRoute(r.path_hash, verb, selectedBackend?.id)}
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
                <DetailPane detail={detail} initialVerb={selectedVerb} groupId={groupId} />
              )}
            </div>
          </div>
          )}
        </TabsContent>

        {/* Orphans tab */}
        <TabsContent value="orphans" className="flex-1 min-h-0 mt-0">
          {activeTab === "orphans" && <OrphansPanel groupId={groupId} />}
        </TabsContent>
      </Tabs>
    </div>
    </AuthCoverageContext.Provider>
  );
}
