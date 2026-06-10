/* ============================================================
   Security — Auth-Coverage, Secrets & Import-Cycles dashboard.

   Route: /g/:groupId/security
   Issue: #4250 | Epic: #4249

   Surfaces capability data the backend already serves
   (internal/dashboard/handlers_security.go) but no screen previously
   rendered. Three v1 routes, all static graph reads — NO runtime metrics:

     GET /api/security/auth-coverage/{group}
     GET /api/security/secrets/{group}
     GET /api/security/cycles/{group}

   Layout mirrors Topology / Operations: TopBar-less full-height column,
   a Tabs strip with Pill counts, and per-tab workspaces with consistent
   loading / empty / error states. Reuses the shared primitives layer
   (Badge, Card, Pill, Tabs, Skeleton) + RefLine/RepoChip rather than
   inventing new components.
   ============================================================ */

import { useState } from "react";
import { useParams } from "react-router-dom";
import {
  ShieldAlert,
  ShieldCheck,
  KeyRound,
  RefreshCw,
  AlertTriangle,
  Lock,
  Unlock,
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
  TabCount,
  useSetInsight,
  DefTerm,
} from "@/components/ui";
import type { InsightValue } from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
import { useScope } from "@/lib/scope-context";
import { cn } from "@/lib/utils";
import {
  useAuthCoverage,
  useSecrets,
  useSecurityCycles,
} from "@/hooks/use-security";
import type {
  AuthEndpointFinding,
  SecuritySecretFinding,
  CycleFinding,
  SecuritySeverity,
} from "@/data/types";

// ---------------------------------------------------------------------------
// § Severity helpers
// ---------------------------------------------------------------------------

const SEVERITY_TONE: Record<SecuritySeverity, "danger" | "warning" | "info"> = {
  error: "danger",
  warn: "warning",
  info: "info",
};

const SEVERITY_LABEL: Record<SecuritySeverity, string> = {
  error: "high",
  warn: "medium",
  info: "low",
};

function SeverityBadge({ severity }: { severity: SecuritySeverity }) {
  return (
    <Badge tone={SEVERITY_TONE[severity]} className="capitalize shrink-0">
      {SEVERITY_LABEL[severity]}
    </Badge>
  );
}

const SEVERITY_FILTERS: { value: "all" | SecuritySeverity; label: string }[] = [
  { value: "all", label: "All" },
  { value: "error", label: "High" },
  { value: "warn", label: "Medium" },
  { value: "info", label: "Low" },
];

// ---------------------------------------------------------------------------
// § Public-by-design detection (#4571)
//
// The backend collapses every auth signal — including explicit @Public /
// AllowAny / permitAll exemptions — into a single verdict, but the
// auth-coverage wire shape (AuthEndpointFinding) does NOT yet carry a public
// flag: an intentionally-public route arrives as has_auth=false with no
// evidence, indistinguishable from a route that simply forgot its guard.
// Until the backend surfaces an explicit `public`/`auth_required=false` flag
// (follow-up: backend public-flag on AuthEndpointFinding), we degrade
// gracefully on the client by recognising two signals:
//
//   1. An explicit public guard stamp echoed in auth_evidence
//      (@Public / AllowAny / permitAll / public).
//   2. A small allow-list of canonically-public route names
//      (login / register / reset-password / …) that are public by design.
//
// Such routes get a neutral "public by design" badge instead of High-danger
// copy, and are demoted out of High severity so they stop dominating the
// ranked list. This is a heuristic safety net — a real backend flag would be
// cleaner and is tracked as a follow-up.
// ---------------------------------------------------------------------------

const PUBLIC_EVIDENCE_RE = /@?public|allow\s*any|allowany|permit\s*all|permitall/i;

/** Canonically-public route name/path fragments (auth bootstrap, health). */
const PUBLIC_ROUTE_RE =
  /(^|[^a-z])(login|signin|sign-in|logout|register|signup|sign-up|reset[-_]?password|forgot[-_]?password|password[-_]?reset|oauth|callback|webhook|health|healthz|readyz|liveness|ping|public)([^a-z]|$)/i;

/** True when an uncovered endpoint looks intentionally public, not a gap. */
function isPublicByDesign(f: AuthEndpointFinding): boolean {
  if (f.has_auth) return false;
  if (f.auth_evidence && PUBLIC_EVIDENCE_RE.test(f.auth_evidence)) return true;
  const target = `${f.path ?? ""} ${f.name ?? ""}`;
  return PUBLIC_ROUTE_RE.test(target);
}

/**
 * Effective per-row severity after public-by-design demotion. A route that is
 * public by design is never a High finding — its "missing auth" is intentional
 * — so we floor it at "info" for both the badge and the severity filter/count.
 */
function effectiveSeverity(f: AuthEndpointFinding): SecuritySeverity {
  return isPublicByDesign(f) ? "info" : f.severity;
}

// ---------------------------------------------------------------------------
// § Shared state shells (loading / empty / error) — mirror Topology idioms
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
// § Coverage gauge — covered / uncovered % bar
// ---------------------------------------------------------------------------

function CoverageGauge({
  covered,
  uncovered,
  total,
  pct,
}: {
  covered: number;
  uncovered: number;
  total: number;
  pct: number;
}) {
  const coveredPct = total > 0 ? (covered / total) * 100 : 0;
  return (
    <Card>
      <CardHeader className="flex items-center justify-between">
        <CardTitle>Auth coverage</CardTitle>
        <span className="text-2xl font-semibold tabular-nums text-text">
          {pct.toFixed(0)}%
        </span>
      </CardHeader>
      <CardBody className="space-y-3">
        <div
          className="h-3 w-full rounded-full overflow-hidden bg-surface-2 border border-border"
          role="img"
          aria-label={`${covered} of ${total} endpoints covered by auth`}
        >
          <div
            className="h-full bg-success transition-all"
            style={{ width: `${coveredPct}%` }}
          />
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-md">
          <span className="flex items-center gap-1.5 text-text-2">
            <Lock size={13} className="text-success" />
            {covered} covered
          </span>
          <span className="flex items-center gap-1.5 text-text-2">
            <Unlock size={13} className="text-danger" />
            {uncovered} uncovered
          </span>
          <span className="text-text-4">· {total} endpoints total</span>
        </div>
      </CardBody>
    </Card>
  );
}

function CountStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone?: "danger" | "warning" | "info";
}) {
  const color =
    tone === "danger"
      ? "text-danger"
      : tone === "warning"
        ? "text-warning"
        : tone === "info"
          ? "text-info"
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

// ---------------------------------------------------------------------------
// § Severity filter row
// ---------------------------------------------------------------------------

function SeverityFilter({
  value,
  onChange,
}: {
  value: "all" | SecuritySeverity;
  onChange: (v: "all" | SecuritySeverity) => void;
}) {
  return (
    <div className="flex items-center gap-2">
      {SEVERITY_FILTERS.map((f) => (
        <Pill
          key={f.value}
          active={value === f.value}
          onClick={() => onChange(f.value)}
        >
          {f.label}
        </Pill>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Auth-coverage tab
// ---------------------------------------------------------------------------

function AuthFindingRow({
  f,
  multiRepo,
}: {
  f: AuthEndpointFinding;
  multiRepo: boolean;
}) {
  const publicByDesign = isPublicByDesign(f);
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      {/* Primary line: fixed-width columns so rows align like a table.
          [lock] [VERB] [path …] [sensitive/IDOR/severity badges] */}
      <div className="grid grid-cols-[14px_3rem_minmax(0,1fr)_auto] items-center gap-2 min-w-0">
        {f.has_auth ? (
          <Lock size={13} className="text-success shrink-0" />
        ) : (
          <Unlock
            size={13}
            className={cn(
              "shrink-0",
              publicByDesign ? "text-text-4" : "text-danger",
            )}
          />
        )}
        {f.method ? (
          <span className="text-[10px] font-mono uppercase text-center px-1.5 py-0.5 rounded bg-surface-2 text-text-3 border border-border">
            {f.method}
          </span>
        ) : (
          <span aria-hidden />
        )}
        <span className="font-mono text-sm text-text truncate" title={f.path || f.name}>
          {f.path || f.name}
        </span>
        <div className="flex items-center gap-1.5 shrink-0 justify-end">
          {f.sensitive_op && !publicByDesign && (
            <Badge tone="danger" className="shrink-0">
              sensitive
            </Badge>
          )}
          {f.idor_risk && (
            <Badge tone="warning" className="shrink-0">
              IDOR
            </Badge>
          )}
          {publicByDesign ? (
            <Badge
              tone="neutral"
              className="shrink-0"
              title="No auth guard — but this route looks public by design (auth bootstrap / health / explicit @Public). Treated as informational, not a High finding."
            >
              public by design
            </Badge>
          ) : (
            <SeverityBadge severity={f.severity} />
          )}
        </div>
      </div>
      {/* Secondary line: [repo (multi-repo only)] [source-file ref] aligned
          on a fixed leading column so the refs line up across rows (#4500). */}
      <div className="grid grid-cols-[8rem_minmax(0,1fr)] items-center gap-2 min-w-0 -mx-1 pl-1">
        {multiRepo ? (
          <RepoChip slug={f.repo} className="text-[10px] shrink-0" />
        ) : (
          <span aria-hidden />
        )}
        {f.source_file ? (
          <RefLine
            repo={f.repo}
            file={f.source_file}
            line={f.start_line ?? 0}
            name={f.name}
            showRepoChip={false}
            className="text-[11px] py-0.5 px-1 min-w-0"
          />
        ) : (
          <span className="font-mono text-xs text-text-3 truncate">{f.name}</span>
        )}
      </div>
      {!f.has_auth && !publicByDesign && (f.sensitive_op || f.idor_risk) && (
        <p className="text-[11px] text-danger/90">
          {f.sensitive_op && "Sensitive operation with no detected auth policy. "}
          {f.idor_risk && "User-scoped path param — possible IDOR."}
        </p>
      )}
      {!f.has_auth && publicByDesign && (
        <p className="text-[11px] text-text-4">
          Public by design — no auth guard expected
          {f.idor_risk ? "; review the user-scoped path param even so." : "."}
        </p>
      )}
      {f.has_auth && f.auth_evidence && (
        <p className="text-[11px] text-text-4">auth: {f.auth_evidence}</p>
      )}
    </div>
  );
}

function AuthCoverageTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useAuthCoverage(groupId);
  const [severity, setSeverity] = useState<"all" | SecuritySeverity>("all");
  const { matchesScope } = useScope();

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="auth coverage" />;
  if (!data || data.total_endpoints === 0) {
    return (
      <EmptyState
        icon={<ShieldCheck size={32} className="text-text-4" />}
        title="No HTTP endpoints indexed"
        hint="No HTTP endpoint definitions were found for this group, so there is no auth surface to report yet."
      />
    );
  }

  // #4637: honor the active repo/module scope. Findings carry a `repo`, so
  // repo scope filters precisely; module scope degrades to repo-level.
  const allFindings = (data.findings ?? []).filter((f) => matchesScope(f.repo));
  // Severity here is the *effective* severity: routes that look public by
  // design (#4571) are demoted out of High so they neither dominate the list
  // nor inflate the High count. publicCount is surfaced separately below.
  const findings =
    severity === "all"
      ? allFindings
      : allFindings.filter((f) => effectiveSeverity(f) === severity);

  const publicCount = allFindings.filter(isPublicByDesign).length;
  const errorCount = allFindings.filter(
    (f) => effectiveSeverity(f) === "error",
  ).length;
  const warnCount = allFindings.filter(
    (f) => effectiveSeverity(f) === "warn",
  ).length;
  // Everything not effective-High / -Medium is informational (covered routes +
  // demoted public-by-design routes + genuinely low findings).
  const infoCount = allFindings.length - errorCount - warnCount;

  // Repo badge is redundant for a single-repo group — gate on >1 repo,
  // matching the convention used elsewhere in the dashboard (#4500).
  const multiRepo = new Set(data.findings.map((f) => f.repo)).size > 1;

  return (
    <div className="space-y-4">
      <CoverageGauge
        covered={data.covered_count}
        uncovered={data.uncovered_count}
        total={data.total_endpoints}
        pct={data.coverage_pct}
      />

      <div className="flex flex-wrap gap-3">
        <CountStat label="High severity" value={errorCount} tone="danger" />
        <CountStat label="Medium severity" value={warnCount} tone="warning" />
        <CountStat label="Low / covered" value={infoCount} tone="info" />
        {publicCount > 0 && (
          <CountStat label="Public by design" value={publicCount} />
        )}
      </div>

      <div className="flex items-center justify-between gap-3">
        <h3 className="text-md font-medium text-text-2">
          Ranked findings
          <span className="ml-2 text-text-4 tabular-nums">{findings.length}</span>
        </h3>
        <SeverityFilter value={severity} onChange={setSeverity} />
      </div>

      {findings.length === 0 ? (
        <EmptyState
          icon={<ShieldCheck size={28} className="text-success" />}
          title="Nothing at this severity"
          hint="No findings match the selected severity filter."
        />
      ) : (
        <div className="space-y-2">
          {findings.map((f) => (
            <AuthFindingRow key={f.entity_id} f={f} multiRepo={multiRepo} />
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Secrets tab
// ---------------------------------------------------------------------------

function SecretFindingRow({
  f,
  multiRepo,
}: {
  f: SecuritySecretFinding;
  multiRepo: boolean;
}) {
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      {/* Primary line: fixed-width columns to align rows like a table. */}
      <div className="grid grid-cols-[14px_minmax(0,1fr)_auto] items-center gap-2 min-w-0">
        <KeyRound size={13} className="text-warning shrink-0" />
        <span className="font-mono text-sm text-text truncate" title={f.name}>
          {f.name}
        </span>
        <div className="flex items-center gap-1.5 shrink-0 justify-end">
          <Badge tone="neutral" className="shrink-0">
            {f.category.replace(/_/g, " ")}
          </Badge>
          {f.provider && (
            <Badge tone="accent" className="shrink-0">
              {f.provider}
            </Badge>
          )}
          <SeverityBadge severity={f.severity} />
        </div>
      </div>
      {/* Secondary line: [repo (multi-repo only)] [source-file ref] (#4500). */}
      <div className="grid grid-cols-[8rem_minmax(0,1fr)] items-center gap-2 min-w-0 -mx-1 pl-1">
        {multiRepo ? (
          <RepoChip slug={f.repo} className="text-[10px] shrink-0" />
        ) : (
          <span aria-hidden />
        )}
        {f.source_file ? (
          <RefLine
            repo={f.repo}
            file={f.source_file}
            line={f.start_line ?? 0}
            name={f.language ?? ""}
            showRepoChip={false}
            className="text-[11px] py-0.5 px-1 min-w-0"
          />
        ) : (
          <span className="font-mono text-xs text-text-3 truncate">{f.repo}</span>
        )}
      </div>
      {f.remediation && (
        <p className="text-[11px] text-text-4">
          remediation: {f.remediation.replace(/_/g, " ")}
        </p>
      )}
    </div>
  );
}

function SecretsTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useSecrets(groupId);
  const [severity, setSeverity] = useState<"all" | SecuritySeverity>("all");
  const { matchesScope } = useScope();

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="secrets report" />;
  if (!data || data.total_findings === 0) {
    return (
      <EmptyState
        icon={<ShieldCheck size={32} className="text-success" />}
        title="No secret findings"
        hint="No hardcoded credentials or secrets-management integrations were detected in this group."
      />
    );
  }

  // #4637: scope the listed secret findings to the active repo/module.
  const allFindings = (data.findings ?? []).filter((f) => matchesScope(f.repo));
  const findings =
    severity === "all"
      ? allFindings
      : allFindings.filter((f) => f.severity === severity);

  const multiRepo = new Set(allFindings.map((f) => f.repo)).size > 1;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-3">
        <CountStat label="High severity" value={data.error_count} tone="danger" />
        <CountStat label="Medium severity" value={data.warn_count} tone="warning" />
        <CountStat label="Low / info" value={data.info_count} tone="info" />
      </div>

      {Object.keys(data.by_category ?? {}).length > 0 && (
        <Card>
          <CardBody className="flex flex-wrap items-center gap-2 py-3">
            <span className="text-xs text-text-4 mr-1">By category:</span>
            {Object.entries(data.by_category ?? {}).map(([cat, count]) => (
              <Badge key={cat} tone="neutral">
                {cat.replace(/_/g, " ")} · {count}
              </Badge>
            ))}
          </CardBody>
        </Card>
      )}

      <div className="flex items-center justify-between gap-3">
        <h3 className="text-md font-medium text-text-2">
          Findings
          <span className="ml-2 text-text-4 tabular-nums">{findings.length}</span>
        </h3>
        <SeverityFilter value={severity} onChange={setSeverity} />
      </div>

      {findings.length === 0 ? (
        <EmptyState
          icon={<ShieldCheck size={28} className="text-success" />}
          title="Nothing at this severity"
          hint="No findings match the selected severity filter."
        />
      ) : (
        <div className="space-y-2">
          {findings.map((f) => (
            <SecretFindingRow key={f.entity_id} f={f} multiRepo={multiRepo} />
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Import-cycles tab
// ---------------------------------------------------------------------------

/** Looks like an opaque content hash (long hex / base-ish blob, no separators). */
function looksLikeHash(s: string): boolean {
  return /^[0-9a-f]{8,}$/i.test(s) || (/^[A-Za-z0-9_-]{16,}$/.test(s) && !/[./\\]/.test(s));
}

/**
 * Best-effort human-readable member name from a graph entity id
 * ("repo::local:hash") for the import-cycle list (#4580).
 *
 * Module ids encode a path-like "local" segment before an opaque hash tail.
 * The previous implementation always returned the bare hash, which is
 * meaningless to a reader. We now strip the repo prefix and any trailing
 * hash, then surface the last one or two path segments (a recognisable
 * module/file name); only if nothing readable remains do we fall back to a
 * short hash. The full id is always kept as the row's tooltip.
 */
function shortMember(id: string): string {
  if (!id) return id;
  // Drop the "repo::" prefix if present.
  const afterRepo = id.includes("::") ? id.slice(id.indexOf("::") + 2) : id;
  // Split the local part on ":"; drop a trailing opaque hash segment.
  const segs = afterRepo.split(":").filter(Boolean);
  while (segs.length > 1 && looksLikeHash(segs[segs.length - 1])) {
    segs.pop();
  }
  const local = segs.join(":") || afterRepo;

  // Derive a readable name from path-like structure (slashes or dots).
  const pathParts = local.split(/[/\\]/).filter(Boolean);
  let name = pathParts.length ? pathParts[pathParts.length - 1] : local;
  // Strip a file extension for a cleaner module label.
  name = name.replace(/\.(ts|tsx|js|jsx|py|go|java|kt|rb|rs|cs|cpp|c|h)$/i, "");

  if (name && !looksLikeHash(name)) {
    // Prefix the parent dir for disambiguation when it adds signal.
    if (pathParts.length > 1) {
      const parent = pathParts[pathParts.length - 2];
      if (parent && !looksLikeHash(parent)) return `${parent}/${name}`;
    }
    return name;
  }
  // Nothing readable — fall back to a short hash so the chip isn't empty.
  const tail = afterRepo.split(/[:/\\]/).pop() || id;
  return tail.length > 10 ? `${tail.slice(0, 8)}…` : tail;
}

function CycleFindingRow({ c }: { c: CycleFinding }) {
  return (
    <div className="flex flex-col gap-2 px-3 py-2.5 rounded-lg border border-border bg-surface">
      <div className="flex items-center gap-2 min-w-0">
        <RefreshCw size={13} className="text-warning shrink-0" />
        <span className="text-sm text-text">
          Cycle of {c.size} {c.size === 1 ? "module" : "modules"}
        </span>
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          <RepoChip slug={c.repo} className="text-[10px]" />
          <SeverityBadge severity={c.severity} />
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-1.5">
        {(c.members ?? []).map((m, i) => (
          <span key={m} className="flex items-center gap-1.5">
            {i > 0 && <span className="text-text-4 select-none">→</span>}
            <span
              className="font-mono text-xs text-text-2 px-1.5 py-0.5 rounded bg-surface-2 border border-border"
              title={m}
            >
              {shortMember(m)}
            </span>
          </span>
        ))}
      </div>
      {c.suggested_extraction_id && (
        <p className="text-[11px] text-text-4">
          suggested extraction point:{" "}
          <span className="font-mono text-text-3">
            {shortMember(c.suggested_extraction_id)}
          </span>
        </p>
      )}
    </div>
  );
}

function CyclesTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useSecurityCycles(groupId);
  const [severity, setSeverity] = useState<"all" | SecuritySeverity>("all");
  const { matchesScope } = useScope();

  if (isLoading) return <SkeletonRows />;
  if (isError) return <ErrorState what="import cycles" />;
  if (!data || data.total_cycles === 0) {
    return (
      <EmptyState
        icon={<ShieldCheck size={32} className="text-success" />}
        title="No import cycles"
        hint="No circular import dependencies were detected across the indexed repos."
      />
    );
  }

  // #4637: scope import cycles to the active repo/module (cycles carry a repo).
  const allFindings = (data.findings ?? []).filter((c) => matchesScope(c.repo));
  const findings =
    severity === "all"
      ? allFindings
      : allFindings.filter((c) => c.severity === severity);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-3">
        <CountStat label="High (>5)" value={data.error_count} tone="danger" />
        <CountStat label="Medium (3-5)" value={data.warn_count} tone="warning" />
        <CountStat label="Low (2)" value={data.info_count} tone="info" />
      </div>

      <div className="flex items-center justify-between gap-3">
        <h3 className="text-md font-medium text-text-2">
          Cycles
          <span className="ml-2 text-text-4 tabular-nums">{findings.length}</span>
        </h3>
        <SeverityFilter value={severity} onChange={setSeverity} />
      </div>

      {findings.length === 0 ? (
        <EmptyState
          icon={<ShieldCheck size={28} className="text-success" />}
          title="Nothing at this severity"
          hint="No cycles match the selected severity filter."
        />
      ) : (
        <div className="space-y-2">
          {findings.map((c, i) => (
            <CycleFindingRow key={`${c.repo}-${i}`} c={c} />
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Screen shell
// ---------------------------------------------------------------------------

type SecurityTab = "auth" | "secrets" | "cycles";

/**
 * Per-tab insights (#4655). Registered with the breadcrumb Insights button via
 * useSetInsight based on the ACTIVE tab — switching tabs re-registers a new
 * object identity so the button re-glows and the popover updates. Module-level
 * constants for stable identity; replaces the inline per-tab <InsightBanner>.
 */
const SECURITY_INSIGHTS: Record<SecurityTab, InsightValue> = {
  auth: {
    storageKey: "security.auth",
    human: (
      <>
        Which HTTP endpoints are protected by an{" "}
        <DefTerm
          term="auth guard"
          def="A decorator, middleware, interceptor, or framework security rule that requires the caller to be authenticated before the handler runs."
        />{" "}
        and which are exposed without one. Routes that are{" "}
        <DefTerm
          term="public by design"
          def="An endpoint with no auth guard that is intentionally open — e.g. login, register, password-reset, health checks, or an explicit @Public/AllowAny exemption."
        />{" "}
        are flagged as informational rather than High so genuine gaps stand
        out.
      </>
    ),
    agent: {
      tool: "archigraph_auth_coverage",
      example:
        "Reviewing a PR that adds GET /admin/users, an agent calls archigraph_auth_coverage to verify the new route picked up an auth guard — and blocks the merge when it finds the endpoint exposed without one and not on the public-by-design allowlist.",
    },
  },
  secrets: {
    storageKey: "security.secrets",
    human: (
      <>
        Credentials found in the codebase: both{" "}
        <DefTerm
          term="hardcoded"
          def="A password, API key, token, or private key written directly into source code instead of being injected from a secrets store at runtime."
        />{" "}
        secrets that should be removed and detected{" "}
        <DefTerm
          term="secrets-management"
          def="An integration with a dedicated secrets store (Vault, AWS Secrets Manager, …) — the recommended way to supply credentials."
        />{" "}
        integrations that are doing the right thing. Each row links to the
        exact source location.
      </>
    ),
    agent: {
      tool: "archigraph_secrets",
      example:
        "Scanning a PR diff, an agent calls archigraph_secrets and finds a newly-added AWS key string literal in a config file; it blocks the merge, points at the exact line, and suggests moving it into the existing Vault integration instead.",
    },
  },
  cycles: {
    storageKey: "security.cycles",
    human: (
      <>
        Circular{" "}
        <DefTerm
          term="import"
          def="A group of modules that depend on each other in a loop (A imports B imports C imports A), so none can be built, tested, or reasoned about in isolation."
        />{" "}
        dependencies between modules. Cycles make code harder to test and
        refactor; each finding lists the modules in the loop and a suggested{" "}
        <DefTerm
          term="extraction point"
          def="The weakest edge in the cycle — the dependency most cheaply broken (e.g. by moving a shared type to its own module) to untangle the loop."
        />{" "}
        to break it. Severity scales with the number of modules involved.
      </>
    ),
    agent: {
      tool: "archigraph_import_cycles",
      example:
        "After a refactor introduces a new A→B→A loop, an agent calls archigraph_import_cycles to locate the suggested extraction point and proposes moving the shared type into its own module — breaking the cycle with the smallest possible diff.",
    },
  },
};

export default function SecurityScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [tab, setTab] = useState<SecurityTab>("auth");

  // #4655: register the ACTIVE tab's insight with the breadcrumb Insights
  // button. Switching tabs swaps the object identity → the button re-glows and
  // the popover updates. Clears on unmount (navigating away).
  useSetInsight(SECURITY_INSIGHTS[tab]);

  // Lightweight count pills on the tab strip (re-uses the same cached queries).
  const auth = useAuthCoverage(groupId);
  const secrets = useSecrets(groupId);
  const cycles = useSecurityCycles(groupId);

  return (
    <div className="flex flex-col h-full bg-bg">
      <Tabs
        value={tab}
        onValueChange={(v) => setTab(v as typeof tab)}
        className="flex flex-col flex-1 min-h-0"
      >
        {/* Tab strip */}
        <div className="border-b border-border shrink-0 px-4">
          <TabsList className="border-0">
            {/* Counts mean different things per tab, so each TabCount carries
                an explicit label (its tooltip + aria-label) and all three use
                the same hideOnZero zero-handling for consistency (#4572). */}
            <TabsTrigger value="auth">
              <ShieldAlert size={14} className="mr-1.5" />
              Auth coverage
              {!auth.isLoading && auth.data && (
                <TabCount
                  value={auth.data.uncovered_count}
                  tone="warning"
                  active={tab === "auth"}
                  label="endpoints with no detected auth guard"
                  hideOnZero
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="secrets">
              <KeyRound size={14} className="mr-1.5" />
              Secrets
              {!secrets.isLoading && secrets.data && (
                <TabCount
                  value={secrets.data.total_findings}
                  tone="warning"
                  active={tab === "secrets"}
                  label="secret findings (hardcoded + secrets-management)"
                  hideOnZero
                />
              )}
            </TabsTrigger>
            <TabsTrigger value="cycles">
              <RefreshCw size={14} className="mr-1.5" />
              Import cycles
              {!cycles.isLoading && cycles.data && (
                <TabCount
                  value={cycles.data.total_cycles}
                  tone="warning"
                  active={tab === "cycles"}
                  label="circular import dependencies detected"
                  hideOnZero
                />
              )}
            </TabsTrigger>
          </TabsList>
        </div>

        {/* Workspace */}
        <div className="flex-1 min-h-0 overflow-y-auto ag-scroll px-4 py-4">
          <TabsContent value="auth">
            <AuthCoverageTab groupId={groupId} />
          </TabsContent>
          <TabsContent value="secrets">
            <SecretsTab groupId={groupId} />
          </TabsContent>
          <TabsContent value="cycles">
            <CyclesTab groupId={groupId} />
          </TabsContent>
        </div>
      </Tabs>
    </div>
  );
}
