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
} from "@/components/ui";
import { Skeleton } from "@/components/ui/skeleton";
import { RefLine } from "@/components/RefLine";
import { RepoChip } from "@/lib/repo-color";
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

function AuthFindingRow({ f }: { f: AuthEndpointFinding }) {
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0">
        {f.has_auth ? (
          <Lock size={13} className="text-success shrink-0" />
        ) : (
          <Unlock size={13} className="text-danger shrink-0" />
        )}
        {f.method && (
          <span className="text-[10px] font-mono uppercase px-1.5 py-0.5 rounded bg-surface-2 text-text-3 border border-border shrink-0">
            {f.method}
          </span>
        )}
        <span className="font-mono text-sm text-text truncate" title={f.path || f.name}>
          {f.path || f.name}
        </span>
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
          {f.sensitive_op && (
            <Badge tone="danger" className="shrink-0">
              sensitive
            </Badge>
          )}
          {f.idor_risk && (
            <Badge tone="warning" className="shrink-0">
              IDOR
            </Badge>
          )}
          <SeverityBadge severity={f.severity} />
        </div>
      </div>
      <div className="flex items-center gap-2 min-w-0 -mx-1">
        <RepoChip slug={f.repo} className="text-[10px] shrink-0" />
        {f.source_file ? (
          <RefLine
            repo={f.repo}
            file={f.source_file}
            line={f.start_line ?? 0}
            name={f.name}
            className="text-[11px] py-0.5 px-1 min-w-0"
          />
        ) : (
          <span className="font-mono text-xs text-text-3 truncate">{f.name}</span>
        )}
      </div>
      {!f.has_auth && (f.sensitive_op || f.idor_risk) && (
        <p className="text-[11px] text-danger/90">
          {f.sensitive_op && "Sensitive operation with no detected auth policy. "}
          {f.idor_risk && "User-scoped path param — possible IDOR."}
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

  const findings =
    severity === "all"
      ? data.findings
      : data.findings.filter((f) => f.severity === severity);

  return (
    <div className="space-y-4">
      <CoverageGauge
        covered={data.covered_count}
        uncovered={data.uncovered_count}
        total={data.total_endpoints}
        pct={data.coverage_pct}
      />

      <div className="flex flex-wrap gap-3">
        <CountStat label="High severity" value={data.error_count} tone="danger" />
        <CountStat label="Medium severity" value={data.warn_count} tone="warning" />
        <CountStat label="Low / covered" value={data.info_count} tone="info" />
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
            <AuthFindingRow key={f.entity_id} f={f} />
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Secrets tab
// ---------------------------------------------------------------------------

function SecretFindingRow({ f }: { f: SecuritySecretFinding }) {
  return (
    <div className="flex flex-col gap-1.5 px-3 py-2.5 rounded-lg border border-border bg-surface hover:bg-surface-2 transition-colors">
      <div className="flex items-center gap-2 min-w-0">
        <KeyRound size={13} className="text-warning shrink-0" />
        <span className="font-mono text-sm text-text truncate" title={f.name}>
          {f.name}
        </span>
        <div className="ml-auto flex items-center gap-1.5 shrink-0">
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
      <div className="flex items-center gap-2 min-w-0 -mx-1">
        <RepoChip slug={f.repo} className="text-[10px] shrink-0" />
        {f.source_file ? (
          <RefLine
            repo={f.repo}
            file={f.source_file}
            line={f.start_line ?? 0}
            name={f.language ?? ""}
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

  const findings =
    severity === "all"
      ? data.findings
      : data.findings.filter((f) => f.severity === severity);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-3">
        <CountStat label="High severity" value={data.error_count} tone="danger" />
        <CountStat label="Medium severity" value={data.warn_count} tone="warning" />
        <CountStat label="Low / info" value={data.info_count} tone="info" />
      </div>

      {Object.keys(data.by_category).length > 0 && (
        <Card>
          <CardBody className="flex flex-wrap items-center gap-2 py-3">
            <span className="text-xs text-text-4 mr-1">By category:</span>
            {Object.entries(data.by_category).map(([cat, count]) => (
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
            <SecretFindingRow key={f.entity_id} f={f} />
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// § Import-cycles tab
// ---------------------------------------------------------------------------

/** Best-effort short name from a graph entity id ("repo::local:hash"). */
function shortMember(id: string): string {
  const tail = (id ?? "").split("::").pop() ?? id;
  return tail.split(":").pop() || id;
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
        {c.members.map((m, i) => (
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

  const findings =
    severity === "all"
      ? data.findings
      : data.findings.filter((c) => c.severity === severity);

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

export default function SecurityScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const [tab, setTab] = useState<"auth" | "secrets" | "cycles">("auth");

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
            <TabsTrigger value="auth">
              <ShieldAlert size={14} className="mr-1.5" />
              Auth coverage
              {!auth.isLoading && auth.data && (
                <Pill className="ml-1.5">{auth.data.uncovered_count}</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="secrets">
              <KeyRound size={14} className="mr-1.5" />
              Secrets
              {!secrets.isLoading && secrets.data && secrets.data.total_findings > 0 && (
                <Pill className="ml-1.5">{secrets.data.total_findings}</Pill>
              )}
            </TabsTrigger>
            <TabsTrigger value="cycles">
              <RefreshCw size={14} className="mr-1.5" />
              Import cycles
              {!cycles.isLoading && cycles.data && cycles.data.total_cycles > 0 && (
                <Pill className="ml-1.5">{cycles.data.total_cycles}</Pill>
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
