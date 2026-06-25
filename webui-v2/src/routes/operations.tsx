/* ============================================================
   Operations — daemon control, maintenance, diagnostics, quality,
   patterns, and update surfaces.

   Route: /g/:groupId/operations
   Epic: #1432 | Issue: #1444

   Sections (tab-based top level):
     1. System     — daemon status, uptime, logs viewer, restart/stop
     2. Patterns   — agent-learned patterns store: list, detail, GC, export
     3. Quality    — orphan audit + recall measurement (two sub-tabs)
     4. Updates    — version management, update-available check

   Data decisions (recorded per brief §"expose-or-skip"):
     - Rebuild / Reset group/repo: already exposed on Settings screen; deep-linked
       from here via a Section note. No duplication.
     - Cleanup: v1 /api/cleanup handlers exist; exposed via CLI-hint card
       (no safe read-only REST trigger for preview — see PR body).
     - Doctor health check: reuses useRunDoctor from settings; shown inline.
     - install / uninstall / start via launchd: system-level CLI only.
       Stop daemon button here calls POST /api/system/stop (safe destructive).
     - grafel update: check calls GET /api/updates/check (live). Apply is
       a stub that shows CLI hint — SSE streaming planned for follow-up.
   ============================================================ */

import { useState, useRef, useEffect } from "react";
import { useParams } from "react-router-dom";
import {
  Activity,
  AlertTriangle,
  CheckCircle,
  ChevronRight,
  Download,
  ExternalLink,
  HardDrive,
  Info,
  Loader2,
  RefreshCw,
  Server,
  SquareTerminal,
  ShieldAlert,
  Pin,
  PinOff,
  Trash2,
  XCircle,
  Zap,
} from "lucide-react";
import { toast } from "sonner";

import {
  Badge,
  Button,
  Card,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
  Input,
  SearchInput,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  TooltipProvider,
  InfoLabel,
  useSetInsight,
} from "@/components/ui";
import type { InsightValue } from "@/components/ui";
import {
  useSystemStatus,
  useRestartDaemon,
  useStopDaemon,
  useSystemLogs,
  useUpdateCheck,
  useApplyUpdate,
  useCleanup,
  usePatterns,
  useDeletePattern,
  usePatternGC,
  useExportPatterns,
  useOrphanAudit,
  useRunOrphanAudit,
  useQualityFixtures,
  useRunRecall,
  useQuarantine,
  useUnquarantine,
  usePinQuarantine,
} from "@/hooks/use-operations";
import { useRunDoctor } from "@/hooks/use-settings";
import { useIndexProgress } from "@/hooks/use-index-progress";
import { IndexProgressFeed } from "@/components/chrome/index-progress-feed";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { DoctorCheck, LogLine, PatternRow, SystemStatus } from "@/data/types";

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

function relativeTime(isoOrMs: string | number | null): string {
  if (!isoOrMs) return "—";
  const d = typeof isoOrMs === "number" ? new Date(isoOrMs) : new Date(isoOrMs as string);
  if (isNaN(d.getTime())) return "—";
  const diff = Date.now() - d.getTime();
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function pct(n: number): string {
  return `${(n * 100).toFixed(1)}%`;
}

// ---------------------------------------------------------------------------
// Section shell — mirrors Settings screen pattern
// ---------------------------------------------------------------------------

function Section({
  id,
  title,
  sub,
  action,
  children,
  danger,
}: {
  id?: string;
  title: string;
  sub?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
  danger?: boolean;
}) {
  return (
    <section
      id={id}
      className={cn(
        "rounded-xl border p-5",
        danger ? "border-danger/40 bg-danger/5" : "border-border bg-surface",
      )}
    >
      <header className="flex items-start justify-between gap-4 mb-4">
        <div>
          <h2 className={cn("text-base font-semibold", danger ? "text-danger" : "text-text")}>
            {title}
          </h2>
          {sub && <p className="mt-0.5 text-sm text-text-3">{sub}</p>}
        </div>
        {action}
      </header>
      <div>{children}</div>
    </section>
  );
}

// ---------------------------------------------------------------------------
// Confirm modal (reused across sections)
// ---------------------------------------------------------------------------

function ConfirmModal({
  open,
  title,
  description,
  primaryLabel,
  intent = "default",
  pending,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: React.ReactNode;
  description: React.ReactNode;
  primaryLabel: string;
  intent?: "default" | "danger";
  pending?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription asChild>
          <p className="mt-2 text-sm text-text-3">{description}</p>
        </DialogDescription>
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant={intent === "danger" ? "danger" : "primary"}
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? <Loader2 size={13} className="animate-spin" /> : null}
            {primaryLabel}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// 1. SYSTEM TAB
// ---------------------------------------------------------------------------

const SEVERITY_COLORS: Record<string, string> = {
  error: "text-danger",
  warn: "text-warning",
  debug: "text-text-4",
  info: "text-text-2",
};

function LogLineRow({ line }: { line: LogLine }) {
  return (
    <div className={cn("font-mono text-xs leading-5 px-1", SEVERITY_COLORS[line.severity] ?? "text-text-2")}>
      {line.raw}
    </div>
  );
}

function LogsPanel({ onClose }: { onClose: () => void }) {
  const [q, setQ] = useState("");
  const [sev, setSev] = useState("");
  const [debouncedQ, setDebouncedQ] = useState("");
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  const { data, isLoading, isFetching, refetch } = useSystemLogs({
    n: 200,
    q: debouncedQ || undefined,
    severity: sev || undefined,
  });

  useEffect(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => setDebouncedQ(q), 300);
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [q]);

  useEffect(() => {
    if (bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [data]);

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-3xl w-full">
        <DialogTitle className="flex items-center gap-2">
          <SquareTerminal size={15} className="text-text-3" />
          Daemon logs
          {data && (
            <span className="ml-1 text-xs text-text-4 font-normal font-mono truncate">
              {data.path}
            </span>
          )}
        </DialogTitle>

        <div className="flex items-center gap-2 mt-3">
          <SearchInput
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Filter logs…"
            className="flex-1 text-sm"
          />
          <select
            value={sev}
            onChange={(e) => setSev(e.target.value)}
            className="h-8 px-2 rounded-md border border-border bg-surface text-sm text-text-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
          >
            <option value="">All severity</option>
            <option value="error">Errors only</option>
            <option value="warn">Warnings+</option>
          </select>
          <Button variant="ghost" size="sm" onClick={() => void refetch()} disabled={isFetching}>
            {isFetching ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
          </Button>
        </div>

        <div className="mt-3 h-80 overflow-y-auto rounded-lg border border-border bg-bg-soft p-2">
          {isLoading ? (
            <div className="flex items-center justify-center h-full text-sm text-text-3">
              <Loader2 size={14} className="animate-spin mr-2" />
              Loading logs…
            </div>
          ) : !data || (data.lines?.length ?? 0) === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-sm text-text-3 gap-1">
              <SquareTerminal size={20} className="text-text-4" />
              No log entries found.
            </div>
          ) : (
            <>
              {data.lines.map((line, i) => (
                <LogLineRow key={i} line={line} />
              ))}
              <div ref={bottomRef} />
            </>
          )}
        </div>

        {data && (
          <p className="mt-1 text-xs text-text-4 text-right">{data.total} lines shown</p>
        )}
      </DialogContent>
    </Dialog>
  );
}

const STATUS_DOT: Record<string, string> = {
  running: "bg-success",
  stopped: "bg-danger",
  unhealthy: "bg-warning",
};

const STATUS_LABEL: Record<string, string> = {
  running: "Running",
  stopped: "Stopped",
  unhealthy: "Unhealthy",
};

function DaemonStatusCard({
  status,
  onRestart,
  onStop,
  onViewLogs,
}: {
  status: SystemStatus;
  onRestart: () => void;
  onStop: () => void;
  onViewLogs: () => void;
}) {
  const dot = STATUS_DOT[status.status] ?? "bg-text-4";
  const label = STATUS_LABEL[status.status] ?? status.status;

  // Memory budget state. When RSS exceeds the configured budget the daemon is
  // running over its intended footprint — surface a clear warning rather than a
  // bare "987 / 500 MB" number that reads as fine.
  const overBudget =
    status.rss_budget_mb != null &&
    status.rss_budget_mb > 0 &&
    status.rss_mb > status.rss_budget_mb;
  const memRatio =
    status.rss_budget_mb && status.rss_budget_mb > 0
      ? status.rss_mb / status.rss_budget_mb
      : 0;
  // Hard warning (red) once ~1.5× over; amber for anything above budget.
  const memSeverity = overBudget ? (memRatio >= 1.5 ? "danger" : "warning") : null;
  const memTooltip = overBudget
    ? `Over memory budget — using ${status.rss_mb.toFixed(0)} MB against a ${status.rss_budget_mb!.toFixed(
        0,
      )} MB budget (${Math.round(memRatio * 100)}%). Consider restarting the daemon or indexing fewer repositories.`
    : status.rss_budget_mb
    ? `Within budget — ${status.rss_mb.toFixed(0)} MB of ${status.rss_budget_mb.toFixed(0)} MB.`
    : undefined;

  return (
    <Card className="p-5">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-center gap-2">
          <span className={cn("size-2.5 rounded-full shrink-0", dot)} />
          <span className="font-semibold text-text">{label}</span>
          {status.status === "running" && status.uptime_human && (
            <span className="text-xs text-text-4 font-mono">{status.uptime_human}</span>
          )}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Button variant="ghost" size="sm" onClick={onViewLogs}>
            <SquareTerminal size={12} />
            Logs
          </Button>
          <Button variant="secondary" size="sm" onClick={onRestart}>
            <RefreshCw size={12} />
            Restart
          </Button>
          <Button variant="danger" size="sm" onClick={onStop}>
            Stop daemon
          </Button>
        </div>
      </div>

      <dl className="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4">
        {[
          { label: "PID", value: String(status.pid), mono: true, truncate: false },
          {
            label: "Memory",
            value: status.rss_budget_mb
              ? `${status.rss_mb.toFixed(0)} / ${status.rss_budget_mb.toFixed(0)} MB`
              : `${status.rss_mb.toFixed(0)} MB`,
            mono: true,
            truncate: false,
            memory: true,
          },
          { label: "Socket", value: status.socket_path ?? "—", mono: true, truncate: true },
          { label: "Dashboard URL", value: status.dashboard_url ?? "—", mono: true, truncate: true },
        ].map(({ label, value, mono, truncate, memory }) => (
          <div key={label}>
            <dt className="text-xs text-text-3 flex items-center gap-1.5">
              {label}
              {memory && memSeverity && (
                <Badge tone={memSeverity} className="text-[10px] px-1.5 py-0" title={memTooltip}>
                  <AlertTriangle size={9} className="mr-0.5" />
                  Over budget
                </Badge>
              )}
            </dt>
            <dd
              className={cn(
                "text-sm mt-0.5",
                mono && "font-mono",
                truncate && "truncate",
                memory && memSeverity === "danger" && "text-danger",
                memory && memSeverity === "warning" && "text-warning",
              )}
              title={memory ? memTooltip ?? value : value}
            >
              {value}
            </dd>
          </div>
        ))}
      </dl>

      <div className="mt-4 pt-4 border-t border-border-soft flex items-center justify-between gap-4">
        <div className="flex items-center gap-4 text-xs text-text-3 font-mono">
          <span>v{status.version}</span>
          {status.commit_sha && (
            <a
              href={`https://github.com/cajasmota/grafel/commit/${status.commit_sha}`}
              target="_blank"
              rel="noopener noreferrer"
              className="hover:text-accent-strong flex items-center gap-1"
            >
              {status.commit_sha.slice(0, 7)}
              <ExternalLink size={10} />
            </a>
          )}
          {status.built_at && <span>{relativeTime(status.built_at)}</span>}
        </div>
        {status.stale_build && (
          <Badge tone="warning" className="text-xs">
            Build &gt;7 days old
          </Badge>
        )}
      </div>
    </Card>
  );
}

const DOCTOR_ICON: Record<DoctorCheck["status"], React.ReactNode> = {
  ok: <CheckCircle size={13} className="text-success shrink-0" />,
  warning: <AlertTriangle size={13} className="text-warning shrink-0" />,
  info: <Info size={13} className="text-accent-strong shrink-0" />,
  error: <XCircle size={13} className="text-danger shrink-0" />,
};

function SystemTab({ groupId }: { groupId: string }) {
  const { data: status, isLoading, isError } = useSystemStatus();
  const restartDaemon = useRestartDaemon();
  const stopDaemon = useStopDaemon();
  const runDoctor = useRunDoctor(groupId);
  const cleanup = useCleanup();
  const [logsOpen, setLogsOpen] = useState(false);
  const [confirmRestart, setConfirmRestart] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);

  // #1527 — live per-repo / per-MODULE indexing activity for this group. The
  // stream is always open while on Operations; rows only appear when a
  // rebuild/index is actually running, so the section self-hides when idle.
  const indexProgress = useIndexProgress(groupId, true);

  return (
    <div className="space-y-6">
      {/* Live indexing activity (#1527) — one row per repo, or per MODULE for
          monorepos. Self-hides when nothing is indexing. */}
      {indexProgress.hasData && (
        <Section
          title="Indexing activity"
          sub="Live progress per repo — and per module for monorepos."
        >
          <IndexProgressFeed rows={indexProgress.rows} className="max-h-80 overflow-y-auto pr-0.5" />
        </Section>
      )}

      {/* Daemon status */}
      <Section title="Daemon" sub="Live process state. Auto-refreshes every 5 seconds.">
        {isLoading ? (
          <Skeleton h="h-28" className="rounded-lg" />
        ) : isError || !status ? (
          <div className="flex items-center gap-2 text-sm text-text-3 py-4">
            <AlertTriangle size={14} className="text-warning" />
            Could not reach daemon — it may be stopped. Use{" "}
            <code className="font-mono text-text-2">grafel start</code> to restart.
          </div>
        ) : (
          <DaemonStatusCard
            status={status}
            onRestart={() => setConfirmRestart(true)}
            onStop={() => setConfirmStop(true)}
            onViewLogs={() => setLogsOpen(true)}
          />
        )}
      </Section>

      {/* Maintenance cross-link */}
      <Section
        title="Maintenance"
        sub="Rebuild and reset operations live on the Group Settings screen."
      >
        <div className="rounded-lg border border-border-soft bg-surface-2 p-4 text-sm text-text-3 flex items-start gap-3">
          <Info size={14} className="text-accent-strong shrink-0 mt-0.5" />
          <div className="space-y-2">
            <p>
              <span className="text-text-2 font-medium">Rebuild / Reset</span> — trigger per-group or
              per-repo rebuild and cache-reset from{" "}
              <a
                href={`/g/${groupId}/settings#repositories`}
                className="text-accent-strong underline underline-offset-2 hover:opacity-80"
              >
                Group Settings → Repositories
              </a>.
            </p>
            <p>
              <span className="text-text-2 font-medium">Cleanup</span> — removes registry entries
              whose config file no longer exists. Preview first, then remove.
            </p>
            <div className="flex items-center gap-2">
              <Button
                variant="secondary"
                size="sm"
                disabled={cleanup.isPending}
                onClick={() => {
                  cleanup.mutate(true, {
                    onSuccess: (res) => toast.info(res.message),
                    onError: () => toast.error("Cleanup preview failed."),
                  });
                }}
              >
                Preview orphans
              </Button>
              <Button
                variant="danger"
                size="sm"
                disabled={cleanup.isPending}
                onClick={() => {
                  cleanup.mutate(false, {
                    onSuccess: (res) =>
                      res.removed > 0 ? toast.success(res.message) : toast.info(res.message),
                    onError: () => toast.error("Cleanup failed."),
                  });
                }}
              >
                Remove orphans
              </Button>
            </div>
          </div>
        </div>
      </Section>

      {/* Health check */}
      <Section
        title="Health check"
        sub="Runs grafel doctor for this group — catches stale caches, missing hooks, daemon issues."
        action={
          <Button
            variant="primary"
            size="sm"
            disabled={runDoctor.isPending}
            onClick={() => runDoctor.mutate()}
          >
            {runDoctor.isPending ? (
              <>
                <Loader2 size={12} className="animate-spin" />
                Running…
              </>
            ) : (
              "Run health check"
            )}
          </Button>
        }
      >
        {runDoctor.data ? (
          <div className="space-y-1.5">
            {(runDoctor.data as DoctorCheck[]).map((check) => (
              <div key={check.id} className="flex items-center gap-2 text-sm py-1.5">
                {DOCTOR_ICON[check.status]}
                <span className="flex-1 text-text-2">{check.label}</span>
                <span className="text-text-4 font-mono text-xs">{check.detail}</span>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-sm text-text-3 flex items-center gap-2 py-3">
            <Info size={14} />
            Click Run to check daemon status, watcher state, and pending work.
          </p>
        )}
      </Section>

      {logsOpen && <LogsPanel onClose={() => setLogsOpen(false)} />}

      <ConfirmModal
        open={confirmRestart}
        title="Restart daemon?"
        description="Sends SIGTERM — the daemon restarts automatically via launchd / systemd (KeepAlive=true). Indexing in progress resumes on restart."
        primaryLabel="Restart daemon"
        intent="default"
        pending={restartDaemon.isPending}
        onClose={() => setConfirmRestart(false)}
        onConfirm={() => {
          restartDaemon.mutate(undefined, {
            onSuccess: (d) => {
              toast.info(d.message ?? "Restart signal sent.");
              setConfirmRestart(false);
            },
            onError: () => {
              toast.error("Failed to send restart signal.");
              setConfirmRestart(false);
            },
          });
        }}
      />

      <ConfirmModal
        open={confirmStop}
        title="Stop daemon?"
        description="Sends SIGTERM. The daemon will NOT restart automatically — you'll lose the dashboard until you run 'grafel start' from a terminal."
        primaryLabel="Stop daemon"
        intent="danger"
        pending={stopDaemon.isPending}
        onClose={() => setConfirmStop(false)}
        onConfirm={() => {
          stopDaemon.mutate(undefined, {
            onSuccess: (d) => {
              toast.warning(d.message ?? "Daemon stopped.");
              setConfirmStop(false);
            },
            onError: () => {
              toast.error("Failed to stop daemon.");
              setConfirmStop(false);
            },
          });
        }}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// 2. PATTERNS TAB
// ---------------------------------------------------------------------------

const STATUS_BADGE_TONE: Record<string, "success" | "warning" | "danger" | "neutral"> = {
  active: "success",
  candidate: "warning",
  rejected: "danger",
};

function ConfidenceBar({ value }: { value: number }) {
  const p = Math.round(value * 100);
  const color = p >= 70 ? "bg-success" : p >= 40 ? "bg-warning" : "bg-danger";
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-20 rounded-full bg-surface-3 overflow-hidden">
        <div className={cn("h-full rounded-full transition-all", color)} style={{ width: `${p}%` }} />
      </div>
      <span className="text-xs font-mono tabular-nums text-text-3">{p}%</span>
    </div>
  );
}

function PatternDetailPanel({
  pattern,
  groupId,
  onClose,
}: {
  pattern: PatternRow;
  groupId: string;
  onClose: () => void;
}) {
  const deletePattern = useDeletePattern(groupId);
  const [confirmDelete, setConfirmDelete] = useState(false);

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-lg w-full">
        <DialogTitle className="flex items-center gap-2 justify-between">
          <span className="font-mono text-sm truncate">{pattern.id}</span>
          <Badge tone={STATUS_BADGE_TONE[pattern.status] ?? "neutral"}>
            {pattern.status}
          </Badge>
        </DialogTitle>

        <div className="mt-3 space-y-3 text-sm">
          <div className="grid grid-cols-2 gap-3">
            {[
              { label: "Kind", value: pattern.kind || "—" },
              { label: "Category", value: pattern.category || "—" },
              { label: "Observations", value: String(pattern.observations) },
              { label: "Last seen", value: pattern.last_seen ? relativeTime(pattern.last_seen) : "—" },
            ].map(({ label, value }) => (
              <div key={label}>
                <p className="text-xs text-text-3">{label}</p>
                <p className="text-text-2 font-mono">{value}</p>
              </div>
            ))}
          </div>

          <div>
            <p className="text-xs text-text-3 mb-1">Confidence</p>
            <ConfidenceBar value={pattern.confidence} />
          </div>

          {pattern.trigger && (
            <div>
              <p className="text-xs text-text-3 mb-1">Trigger</p>
              <p className="text-text-2">{pattern.trigger}</p>
            </div>
          )}

          {pattern.steps && pattern.steps.length > 0 && (
            <div>
              <p className="text-xs text-text-3 mb-1">Steps</p>
              <ol className="list-decimal list-inside space-y-0.5 text-text-2">
                {pattern.steps.map((s, i) => (
                  <li key={i}>{s}</li>
                ))}
              </ol>
            </div>
          )}

          {pattern.reject_reason && (
            <div className="rounded-md border border-danger/30 bg-danger/5 px-3 py-2">
              <p className="text-xs font-medium text-danger mb-0.5">Rejection reason</p>
              <p className="text-sm text-text-2">{pattern.reject_reason}</p>
            </div>
          )}

          {pattern.needs_attention && (
            <p className="flex items-center gap-1.5 text-xs text-warning">
              <AlertTriangle size={12} />
              Needs attention
            </p>
          )}
        </div>

        <div className="mt-5 flex justify-between gap-2">
          <Button variant="danger" size="sm" onClick={() => setConfirmDelete(true)}>
            <Trash2 size={12} />
            Delete
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Close
          </Button>
        </div>

        <ConfirmModal
          open={confirmDelete}
          title="Delete pattern?"
          description={`This permanently removes pattern "${pattern.id}" from the store. It cannot be undone.`}
          primaryLabel="Delete pattern"
          intent="danger"
          pending={deletePattern.isPending}
          onClose={() => setConfirmDelete(false)}
          onConfirm={() => {
            deletePattern.mutate(pattern.id, {
              onSuccess: () => {
                toast.success("Pattern deleted.");
                onClose();
              },
              onError: () => toast.error("Failed to delete pattern."),
            });
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

function PatternsTab({ groupId }: { groupId: string }) {
  const [statusFilter, setStatusFilter] = useState("");
  const [needsAttentionOnly, setNeedsAttentionOnly] = useState(false);
  const [searchQ, setSearchQ] = useState("");
  const [selected, setSelected] = useState<PatternRow | null>(null);
  const [gcResult, setGCResult] = useState<{ pruned_count: number } | null>(null);
  const [exportOpen, setExportOpen] = useState(false);
  const [exportTarget, setExportTarget] = useState("");

  const patternGC = usePatternGC(groupId);
  const exportPatterns = useExportPatterns(groupId);

  const { data, isLoading, isError } = usePatterns(groupId, {
    needs_attention: needsAttentionOnly || undefined,
    status: statusFilter || undefined,
  });

  const filtered =
    data?.patterns?.filter((p) =>
      searchQ
        ? p.id.includes(searchQ) ||
          p.kind?.toLowerCase().includes(searchQ.toLowerCase()) ||
          p.trigger?.toLowerCase().includes(searchQ.toLowerCase())
        : true,
    ) ?? [];

  const stats = data?.stats;

  return (
    <div className="space-y-5">
      {/* Stats header */}
      {stats && (
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-3">
          {[
            { label: "Total", value: stats.total, color: "" },
            { label: "Pending review", value: stats.pending_review, color: "text-warning" },
            { label: "Rejected", value: stats.rejected, color: "text-danger" },
            { label: "Stale", value: stats.stale, color: "text-text-3" },
            { label: "Needs attention", value: stats.needs_attention, color: "text-warning" },
          ].map(({ label, value, color }) => (
            <Card key={label} className="p-3 text-center">
              <p className={cn("text-xl font-mono font-semibold tabular-nums", color)}>{value}</p>
              <p className="text-xs text-text-3 mt-0.5">{label}</p>
            </Card>
          ))}
        </div>
      )}

      {/* Filters + actions */}
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput
          value={searchQ}
          onChange={(e) => setSearchQ(e.target.value)}
          placeholder="Search patterns…"
          className="w-56 text-sm"
        />
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="h-8 px-2 rounded-md border border-border bg-surface text-sm text-text-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
        >
          <option value="">All statuses</option>
          <option value="active">Active</option>
          <option value="candidate">Candidate</option>
          <option value="rejected">Rejected</option>
        </select>
        <label className="flex items-center gap-1.5 text-sm text-text-2 cursor-pointer">
          <input
            type="checkbox"
            checked={needsAttentionOnly}
            onChange={(e) => setNeedsAttentionOnly(e.target.checked)}
            className="accent-[var(--accent)]"
          />
          Needs attention only
        </label>

        <div className="ml-auto flex items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              patternGC.mutate(true, {
                onSuccess: (d) => {
                  setGCResult({ pruned_count: d.pruned_count });
                  toast.info(`GC dry-run: ${d.pruned_count} patterns would be pruned.`);
                },
                onError: () => toast.error("GC dry-run failed."),
              });
            }}
            disabled={patternGC.isPending}
          >
            {patternGC.isPending ? <Loader2 size={12} className="animate-spin" /> : <Zap size={12} />}
            Run GC
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setExportOpen(true)}>
            <Download size={12} />
            Export
          </Button>
        </div>
      </div>

      {/* GC dry-run result banner */}
      {gcResult && (
        <div className="rounded-lg border border-warning/30 bg-warning/5 p-3 flex items-start gap-3">
          <Info size={14} className="text-warning shrink-0 mt-0.5" />
          <div className="flex-1 text-sm text-text-2">
            <span className="font-medium">{gcResult.pruned_count} patterns</span> would be pruned
            (stale candidates).
            {gcResult.pruned_count > 0 && (
              <button
                type="button"
                className="ml-2 text-danger underline text-sm"
                onClick={() => {
                  patternGC.mutate(false, {
                    onSuccess: (d) => {
                      toast.success(`GC: ${d.pruned_count} patterns pruned.`);
                      setGCResult(null);
                    },
                    onError: () => toast.error("GC failed."),
                  });
                }}
              >
                Prune now
              </button>
            )}
          </div>
          <button
            type="button"
            className="text-text-4 hover:text-text text-lg leading-none"
            onClick={() => setGCResult(null)}
          >
            ×
          </button>
        </div>
      )}

      {/* Export panel */}
      {exportOpen && (
        <div className="rounded-lg border border-border bg-surface p-4 text-sm space-y-3">
          <p className="font-medium text-text">Export patterns to CLAUDE.md</p>
          <p className="text-text-3">
            Writes the approved pattern block to the given CLAUDE.md (or a repo
            path, in which case it targets &lt;repo&gt;/CLAUDE.md).
          </p>
          <div className="flex gap-2 pt-1">
            <Input
              value={exportTarget}
              onChange={(e) => setExportTarget(e.target.value)}
              placeholder="/path/to/CLAUDE.md or /path/to/repo"
              className="flex-1 font-mono text-sm"
            />
            <Button
              variant="secondary"
              size="sm"
              disabled={!exportTarget || exportPatterns.isPending}
              onClick={() => {
                const t = exportTarget.trim();
                // Treat a path ending in .md as a file; otherwise a repo dir.
                const target = t.endsWith(".md") ? { file: t } : { repo: t };
                exportPatterns.mutate(target, {
                  onSuccess: (res) => {
                    toast.success(`Exported ${res.exported} pattern(s) to ${res.target}`);
                    setExportOpen(false);
                  },
                  onError: () => toast.error("Pattern export failed."),
                });
              }}
            >
              {exportPatterns.isPending ? "Exporting…" : "Export"}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setExportOpen(false)}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      {/* Pattern list */}
      {isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} h="h-12" className="rounded-lg" />
          ))}
        </div>
      ) : isError ? (
        <div className="flex items-center gap-2 text-sm text-text-3 py-6 justify-center">
          <AlertTriangle size={14} className="text-warning" />
          Failed to load patterns.
        </div>
      ) : filtered.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-16 text-center text-text-3">
          <Zap size={28} className="text-text-4 mb-3" />
          <p className="text-sm font-medium">No patterns yet.</p>
          <p className="text-xs mt-1">
            Agents will populate this as they run on your code.
          </p>
        </div>
      ) : (
        <div className="rounded-xl border border-border overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-surface-2">
                {["ID", "Kind", "Status", "Confidence", "Observations", "Last seen", ""].map((h) => (
                  <th key={h} className="px-3 py-2 text-left text-xs font-medium text-text-3">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-border-soft">
              {filtered.map((p) => (
                <tr
                  key={p.id}
                  className="hover:bg-surface-2 cursor-pointer transition-colors"
                  onClick={() => setSelected(p)}
                >
                  <td
                    className="px-3 py-2.5 font-mono text-xs text-text-2 max-w-[180px] truncate"
                    title={p.id}
                  >
                    {p.id}
                  </td>
                  <td className="px-3 py-2.5 text-text-2">{p.kind || "—"}</td>
                  <td className="px-3 py-2.5">
                    <Badge tone={STATUS_BADGE_TONE[p.status] ?? "neutral"}>
                      {p.status}
                    </Badge>
                    {p.needs_attention && (
                      <AlertTriangle
                        size={12}
                        className="inline ml-1 text-warning"
                        aria-label="Needs attention"
                      />
                    )}
                  </td>
                  <td className="px-3 py-2.5">
                    <ConfidenceBar value={p.confidence} />
                  </td>
                  <td className="px-3 py-2.5 font-mono tabular-nums text-text-3">
                    {p.observations}
                  </td>
                  <td className="px-3 py-2.5 text-text-3 text-xs">
                    {p.last_seen ? relativeTime(p.last_seen) : "—"}
                  </td>
                  <td className="px-3 py-2.5">
                    <ChevronRight size={13} className="text-text-4" />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {selected && (
        <PatternDetailPanel
          pattern={selected}
          groupId={groupId}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// 3. QUALITY TAB — orphan audit + recall
// ---------------------------------------------------------------------------

const FIDELITY_TIP_OPS =
  "Fidelity — how complete grafel's map of your code is. Whenever your code points at something (calls a function, imports a module, calls an API), grafel links it to where that's defined. Fidelity is the percentage of those links it resolved. The rest point at things it couldn't locate yet — external libraries, dynamically-loaded code, or extraction gaps.";
const HEALTH_TIP_OPS =
  "Health is a composite score (0–100) that ALSO factors in orphan rate and recall miss — not just how many references resolved. It is computed only from a real audit run, so it can differ from Fidelity: a graph can resolve most references (high fidelity) yet still have many orphaned entities (lower health).";

// Honest banner shown above the Quality views: Fidelity measures extraction
// completeness, which is a DIFFERENT axis from documentation coverage.
function FidelityBanner() {
  return (
    <div className="flex items-start gap-2.5 rounded-lg border border-border-soft bg-surface-2 p-3 text-xs text-text-3">
      <Info size={14} className="text-accent-strong shrink-0 mt-0.5" />
      <p>
        <span className="text-text-2 font-medium">Fidelity measures extraction completeness</span>{" "}
        — the share of your code's references that grafel linked to a real target. Running the
        docs skill improves <span className="text-text-2">documentation</span> coverage, which is a
        different axis and does not change Fidelity.
      </p>
    </div>
  );
}

// PRIMARY quality view: the unresolved-references breakdown that actually
// drives Fidelity. On graphs with zero orphans the orphan audit reads
// "perfect" while Fidelity is held down entirely by these unresolved edges, so
// this is the view that explains the number.
function UnresolvedReferencesPane({ groupId }: { groupId: string }) {
  const { data, isLoading } = useOrphanAudit(groupId);
  const runAudit = useRunOrphanAudit(groupId);

  const hasRun = !!data?.has_run;
  const refs = data?.references;
  const reasons = refs?.reasons ?? [];
  const resolvedPct = refs && refs.total > 0 ? Math.round(refs.resolved_rate * 100) : null;
  const resolvedColor =
    resolvedPct == null
      ? "text-text-3"
      : resolvedPct >= 90
      ? "text-success"
      : resolvedPct >= 75
      ? "text-warning"
      : "text-danger";

  const reasonColor = (i: number) =>
    ["bg-danger", "bg-warning", "bg-accent-strong", "bg-info"][i % 4];

  return (
    <div className="space-y-5">
      <FidelityBanner />

      <div className="flex items-center justify-between">
        {hasRun ? (
          <p className="text-xs text-text-4">Last measured {relativeTime(data!.audited_at)}</p>
        ) : (
          <span />
        )}
        <Button
          variant="primary"
          size="sm"
          onClick={() =>
            runAudit.mutate(undefined, {
              onSuccess: () => toast.success("Audit complete."),
              onError: () => toast.error("Audit failed."),
            })
          }
          disabled={runAudit.isPending}
        >
          {runAudit.isPending ? (
            <>
              <Loader2 size={12} className="animate-spin" />
              Running…
            </>
          ) : (
            <>
              <Activity size={12} />
              Run audit
            </>
          )}
        </Button>
      </div>

      {isLoading ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} h="h-16" className="rounded-lg" />
          ))}
        </div>
      ) : !hasRun || !refs ? (
        <div className="flex flex-col items-center justify-center py-16 text-center text-text-3">
          <Activity size={28} className="text-text-4 mb-3" />
          <p className="text-sm font-medium">Not measured yet</p>
          <p className="text-xs mt-1">
            Run audit to see which of your code's references grafel could resolve.
          </p>
        </div>
      ) : (
        <>
          {/* Headline numbers */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <Card className="p-4 text-center">
              <p className={cn("text-xl font-mono font-semibold tabular-nums", resolvedColor)}>
                {resolvedPct == null ? "—" : `${resolvedPct}%`}
              </p>
              <div className="mt-1 flex justify-center">
                <InfoLabel label="Fidelity" hint={FIDELITY_TIP_OPS} />
              </div>
            </Card>
            <Card className="p-4 text-center">
              <p className="text-xl font-mono font-semibold tabular-nums text-text">
                {refs.total.toLocaleString()}
              </p>
              <p className="text-xs text-text-3 mt-1">References</p>
            </Card>
            <Card className="p-4 text-center">
              <p className="text-xl font-mono font-semibold tabular-nums text-success">
                {refs.resolved.toLocaleString()}
              </p>
              <p className="text-xs text-text-3 mt-1">Resolved</p>
            </Card>
            <Card className="p-4 text-center">
              <p className="text-xl font-mono font-semibold tabular-nums text-warning">
                {refs.unresolved.toLocaleString()}
              </p>
              <p className="text-xs text-text-3 mt-1">Unresolved</p>
            </Card>
          </div>

          {/* Stacked resolved/unresolved bar */}
          <div className="space-y-1.5">
            <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-surface-3">
              <div
                className="h-full bg-success"
                style={{ width: `${refs.resolved_rate * 100}%` }}
                title={`Resolved — ${refs.resolved.toLocaleString()} (${Math.round(
                  refs.resolved_rate * 100,
                )}%)`}
              />
              {reasons.map((r, i) => (
                <div
                  key={r.reason}
                  className={cn("h-full", reasonColor(i))}
                  style={{ width: `${r.pct * 100}%` }}
                  title={`${r.label} — ${r.count.toLocaleString()} (${Math.round(r.pct * 100)}%)`}
                />
              ))}
            </div>
            <div className="flex items-center gap-3 text-[11px] text-text-3">
              <span className="flex items-center gap-1">
                <span className="size-2 rounded-full bg-success" /> Resolved
              </span>
              {reasons.map((r, i) => (
                <span key={r.reason} className="flex items-center gap-1">
                  <span className={cn("size-2 rounded-full", reasonColor(i))} /> {r.label}
                </span>
              ))}
            </div>
          </div>

          {/* Unresolved by reason */}
          {reasons.length > 0 ? (
            <Section
              title="Unresolved references"
              sub="What stops grafel from linking the rest of your code's references — the reasons that drive Fidelity below 100%."
            >
              <div className="space-y-2.5">
                {reasons.map((r, i) => (
                  <div
                    key={r.reason}
                    className="rounded-lg border border-border-soft bg-surface-2 p-3"
                  >
                    <div className="flex items-center gap-3">
                      <span className={cn("size-2.5 rounded-full shrink-0", reasonColor(i))} />
                      <span className="text-sm text-text-2 font-medium flex-1">{r.label}</span>
                      <span className="text-xs font-mono tabular-nums text-text-3 w-12 text-right">
                        {(r.pct * 100).toFixed(1)}%
                      </span>
                      <span className="text-xs text-text-4 font-mono w-20 text-right">
                        {r.count.toLocaleString()}
                      </span>
                    </div>
                    <p className="text-xs text-text-3 mt-1.5 ml-[22px]">{r.description}</p>
                  </div>
                ))}
              </div>
            </Section>
          ) : (
            <div className="flex flex-col items-center justify-center py-10 text-center text-text-3">
              <CheckCircle size={24} className="text-success mb-2" />
              <p className="text-sm font-medium">All references resolved</p>
              <p className="text-xs mt-1">Every import/reference edge links to a real target.</p>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function OrphanAuditPane({ groupId }: { groupId: string }) {
  const { data, isLoading } = useOrphanAudit(groupId);
  const runAudit = useRunOrphanAudit(groupId);

  // Only a real, persisted run counts as "audited". Until then we must NOT
  // surface health/fidelity/orphan numbers as if they were measured (#1574).
  const hasRun = !!data?.has_run;

  const healthColor = !hasRun
    ? "text-text-3"
    : data!.health_score >= 80
    ? "text-success"
    : data!.health_score >= 50
    ? "text-warning"
    : "text-danger";

  const fidelityPct = data?.fidelity == null ? null : Math.round(data.fidelity * 100);
  const fidelityColor =
    fidelityPct == null
      ? "text-text-3"
      : fidelityPct >= 90
      ? "text-success"
      : fidelityPct >= 75
      ? "text-warning"
      : "text-danger";

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between">
        {hasRun ? (
          <p className="text-xs text-text-4">Last audited {relativeTime(data!.audited_at)}</p>
        ) : (
          <span />
        )}
        <Button
          variant="primary"
          size="sm"
          onClick={() =>
            runAudit.mutate(undefined, {
              onSuccess: () => toast.success("Audit complete."),
              onError: () => toast.error("Audit failed."),
            })
          }
          disabled={runAudit.isPending}
        >
          {runAudit.isPending ? (
            <>
              <Loader2 size={12} className="animate-spin" />
              Running…
            </>
          ) : (
            <>
              <Activity size={12} />
              Run audit
            </>
          )}
        </Button>
      </div>

      {isLoading ? (
        <div className="space-y-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} h="h-16" className="rounded-lg" />
          ))}
        </div>
      ) : !hasRun ? (
        <div className="flex flex-col items-center justify-center py-16 text-center text-text-3">
          <Activity size={28} className="text-text-4 mb-3" />
          <p className="text-sm font-medium">Not audited yet</p>
          <p className="text-xs mt-1">
            Run audit to measure orphans and the composite health score for this group.
          </p>
        </div>
      ) : (
        <>
          {/* Score + totals. Fidelity is the PRIMARY quality number (same as the
              Home cards); Health is the composite, clearly distinguished. */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <Card className="p-4 text-center">
              <p className={cn("text-xl font-mono font-semibold tabular-nums", fidelityColor)}>
                {fidelityPct == null ? "—" : `${fidelityPct}%`}
              </p>
              <div className="mt-1 flex justify-center">
                <InfoLabel label="Fidelity" hint={FIDELITY_TIP_OPS} />
              </div>
            </Card>
            <Card className="p-4 text-center">
              <p className={cn("text-xl font-mono font-semibold tabular-nums", healthColor)}>
                {data!.health_score ?? "—"}
              </p>
              <div className="mt-1 flex justify-center">
                <InfoLabel label="Health score" hint={HEALTH_TIP_OPS} />
              </div>
            </Card>
            <Card className="p-4 text-center">
              <p className="text-xl font-mono font-semibold tabular-nums text-warning">
                {(data!.total?.orphans ?? 0).toLocaleString()}
              </p>
              <p className="text-xs text-text-3 mt-1">
                Orphans{" "}
                <span className="text-text-4">
                  / {(data!.total?.entities ?? 0).toLocaleString()} entities
                </span>
              </p>
            </Card>
            <Card className="p-4 text-center">
              <p
                className={cn(
                  "text-xl font-mono font-semibold tabular-nums",
                  (data!.total?.orphan_rate ?? 0) > 0.2
                    ? "text-danger"
                    : (data!.total?.orphan_rate ?? 0) > 0.1
                    ? "text-warning"
                    : "text-success",
                )}
              >
                {pct(data!.total?.orphan_rate ?? 0)}
              </p>
              <p className="text-xs text-text-3 mt-1">Orphan rate</p>
            </Card>
          </div>

          {/* Per-kind */}
          {(data!.per_kind?.length ?? 0) > 0 && (
            <Section title="By entity kind">
              <div className="space-y-2">
                {data!.per_kind.map((k) => (
                  <div key={k.kind} className="flex items-center gap-3">
                    <span className="text-sm text-text-2 w-32 truncate">{k.kind}</span>
                    <div className="flex-1 h-1.5 rounded-full bg-surface-3 overflow-hidden">
                      <div
                        className={cn(
                          "h-full rounded-full",
                          k.orphan_rate > 0.2 ? "bg-danger" : k.orphan_rate > 0.1 ? "bg-warning" : "bg-success",
                        )}
                        style={{ width: `${Math.min(k.orphan_rate * 100 * 5, 100)}%` }}
                      />
                    </div>
                    <span className="text-xs font-mono tabular-nums text-text-3 w-12 text-right">
                      {pct(k.orphan_rate)}
                    </span>
                    <span className="text-xs text-text-4 font-mono w-28 text-right">
                      {(k.orphans ?? 0).toLocaleString()} / {(k.entities ?? k.count ?? 0).toLocaleString()}
                    </span>
                  </div>
                ))}
              </div>
            </Section>
          )}

          {/* Per-repo */}
          {(data!.per_repo?.length ?? 0) > 0 && (
            <Section title="By repository">
              <div className="rounded-lg border border-border overflow-hidden">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border bg-surface-2">
                      {["Repo", "Entities", "Orphans", "Orphan rate", "Risk"].map((h) => (
                        <th key={h} className="px-3 py-2 text-left text-xs font-medium text-text-3">
                          {h}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-soft">
                    {data!.per_repo.map((r) => (
                      <tr key={r.slug} className="hover:bg-surface-2">
                        <td className="px-3 py-2.5 font-mono text-xs text-text-2">{r.slug}</td>
                        <td className="px-3 py-2.5 font-mono tabular-nums text-text-3">
                          {r.entities.toLocaleString()}
                        </td>
                        <td className="px-3 py-2.5 font-mono tabular-nums text-warning">
                          {r.orphans.toLocaleString()}
                        </td>
                        <td className="px-3 py-2.5">
                          <span
                            className={cn(
                              "font-mono tabular-nums",
                              r.orphan_rate > 0.2
                                ? "text-danger"
                                : r.orphan_rate > 0.1
                                ? "text-warning"
                                : "text-success",
                            )}
                          >
                            {pct(r.orphan_rate)}
                          </span>
                        </td>
                        <td className="px-3 py-2.5 font-mono tabular-nums text-text-3">
                          {r.risk_score}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Section>
          )}

          {/* Recommendations */}
          {(data!.recommendations?.length ?? 0) > 0 && (
            <Section title="Recommendations">
              <div className="space-y-2">
                {data!.recommendations.map((rec, i) => (
                  <div
                    key={i}
                    className="flex items-start gap-3 rounded-lg border border-border-soft bg-surface-2 p-3"
                  >
                    <span className="size-5 rounded-full bg-accent/20 text-accent-strong text-xs flex items-center justify-center font-semibold shrink-0">
                      {rec.priority}
                    </span>
                    <div className="flex-1 text-sm">
                      <p className="text-text-2">{rec.issue}</p>
                      <p className="text-xs text-text-3 mt-0.5">
                        {rec.affected_repos} repo{rec.affected_repos !== 1 ? "s" : ""} ·{" "}
                        ~{rec.recoverable_entities_estimate} recoverable entities
                      </p>
                    </div>
                  </div>
                ))}
              </div>
            </Section>
          )}
        </>
      )}
    </div>
  );
}

function RecallPane({ groupId }: { groupId: string }) {
  const { data: fixturesData, isLoading: fixturesLoading } = useQualityFixtures();
  const runRecall = useRunRecall(groupId);
  const [selected, setSelected] = useState("");
  const fixtures = fixturesData?.fixtures ?? [];

  return (
    <div className="space-y-5">
      <div className="flex items-end gap-3">
        <div className="flex-1">
          <label className="block text-sm text-text-2 mb-1">Golden fixture</label>
          {fixturesLoading ? (
            <Skeleton h="h-8" className="rounded-md" />
          ) : fixtures.length === 0 ? (
            <p className="text-sm text-text-3">
              No fixtures found. Add test fixtures to{" "}
              <code className="font-mono text-text-2">testdata/</code> to enable recall measurement.
            </p>
          ) : (
            <select
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
              className="w-full h-8 px-2 rounded-md border border-border bg-surface text-sm text-text-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
            >
              <option value="">Select fixture…</option>
              {fixtures.map((f) => (
                <option key={f} value={f}>
                  {f}
                </option>
              ))}
            </select>
          )}
        </div>
        <Button
          variant="primary"
          disabled={!selected || runRecall.isPending}
          onClick={() =>
            runRecall.mutate(selected, {
              onSuccess: () => toast.success("Recall measurement complete."),
              onError: () => toast.error("Recall measurement failed."),
            })
          }
        >
          {runRecall.isPending ? (
            <>
              <Loader2 size={12} className="animate-spin" />
              Running…
            </>
          ) : (
            "Run recall"
          )}
        </Button>
      </div>

      {runRecall.data &&
        (() => {
          const r = runRecall.data;
          return (
            <div className="space-y-4">
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                {[
                  {
                    label: "Entity recall",
                    value: pct(r.entity_recall),
                    color:
                      r.entity_recall >= 0.8
                        ? "text-success"
                        : r.entity_recall >= 0.5
                        ? "text-warning"
                        : "text-danger",
                  },
                  {
                    label: "Relationship recall",
                    value: pct(r.relationship_recall),
                    color:
                      r.relationship_recall >= 0.8
                        ? "text-success"
                        : r.relationship_recall >= 0.5
                        ? "text-warning"
                        : "text-danger",
                  },
                  {
                    label: "Entities",
                    value: `${r.entity_found} / ${r.entity_expected}`,
                    color: "text-text",
                  },
                  {
                    label: "Relationships",
                    value: `${r.relationship_found} / ${r.relationship_expected}`,
                    color: "text-text",
                  },
                ].map(({ label, value, color }) => (
                  <Card key={label} className="p-4 text-center">
                    <p className={cn("text-xl font-mono font-semibold tabular-nums", color)}>
                      {value}
                    </p>
                    <p className="text-xs text-text-3 mt-1">{label}</p>
                  </Card>
                ))}
              </div>

              {(r.missing_relationships?.length ?? 0) > 0 && (
                <Section
                  title="Missing relationships"
                  sub={`${r.missing_relationships?.length ?? 0} relationships not found in this fixture`}
                >
                  <div className="rounded-lg border border-border overflow-hidden">
                    <table className="w-full text-xs">
                      <thead>
                        <tr className="border-b border-border bg-surface-2">
                          {["Source", "Target", "Kind"].map((h) => (
                            <th key={h} className="px-3 py-2 text-left font-medium text-text-3">
                              {h}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-border-soft font-mono">
                        {(r.missing_relationships ?? []).slice(0, 50).map((rel, i) => (
                          <tr key={i} className="hover:bg-surface-2">
                            <td
                              className="px-3 py-1.5 text-text-3 truncate max-w-[200px]"
                              title={rel.source_id}
                            >
                              {rel.source_id}
                            </td>
                            <td
                              className="px-3 py-1.5 text-text-3 truncate max-w-[200px]"
                              title={rel.target_id}
                            >
                              {rel.target_id}
                            </td>
                            <td className="px-3 py-1.5 text-text-2">{rel.kind}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                    {r.missing_relationships.length > 50 && (
                      <div className="px-3 py-2 text-xs text-text-3 text-center border-t border-border-soft">
                        + {r.missing_relationships.length - 50} more
                      </div>
                    )}
                  </div>
                </Section>
              )}

              <p className="text-xs text-text-4 text-right font-mono">
                {r.fixture} · {r.elapsed_ms}ms
              </p>
            </div>
          );
        })()}
    </div>
  );
}

function QualityTab({ groupId }: { groupId: string }) {
  return (
    <Tabs defaultValue="references">
      <TabsList className="mb-5">
        <TabsTrigger value="references">Unresolved references</TabsTrigger>
        <TabsTrigger value="orphans">Orphan audit</TabsTrigger>
        <TabsTrigger value="recall">Recall measurement</TabsTrigger>
      </TabsList>
      <TabsContent value="references">
        <UnresolvedReferencesPane groupId={groupId} />
      </TabsContent>
      <TabsContent value="orphans">
        <OrphanAuditPane groupId={groupId} />
      </TabsContent>
      <TabsContent value="recall">
        <RecallPane groupId={groupId} />
      </TabsContent>
    </Tabs>
  );
}

// ---------------------------------------------------------------------------
// 4. UPDATES TAB
// ---------------------------------------------------------------------------

function UpdatesTab() {
  const { data, isLoading, isError, refetch, isFetching } = useUpdateCheck();
  const applyUpdate = useApplyUpdate();

  return (
    <div className="space-y-5">
      <Section
        title="Version management"
        sub="Check for new grafel releases and refresh extraction rules."
        action={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void refetch()}
            disabled={isFetching}
          >
            {isFetching ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}
            Check now
          </Button>
        }
      >
        {isLoading ? (
          <Skeleton h="h-20" className="rounded-lg" />
        ) : isError || !data ? (
          <div className="flex items-center gap-2 text-sm text-text-3 py-4">
            <AlertTriangle size={14} className="text-warning" />
            Could not check for updates.
          </div>
        ) : (
          <div className="space-y-4">
            <dl className="grid grid-cols-2 sm:grid-cols-3 gap-4">
              {[
                { label: "Current version", value: data.current_version, mono: true },
                {
                  label: "Commit",
                  value: data.current_commit?.slice(0, 7) ?? "—",
                  mono: true,
                },
                {
                  label: "Built",
                  value: relativeTime(data.current_built_at),
                  mono: false,
                },
              ].map(({ label, value, mono }) => (
                <div key={label}>
                  <dt className="text-xs text-text-3">{label}</dt>
                  <dd className={cn("text-sm text-text-2 mt-0.5", mono && "font-mono")}>
                    {value}
                  </dd>
                </div>
              ))}
            </dl>

            {data.fetch_error ? (
              <div className="flex items-center gap-2 text-sm text-text-3 rounded-lg border border-border-soft bg-surface-2 p-3">
                <Info size={14} className="text-text-4 shrink-0" />
                Could not fetch latest release: {data.fetch_error}
              </div>
            ) : data.update_available ? (
              <div className="rounded-lg border border-accent/40 bg-accent-soft/30 p-4 space-y-3">
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <p className="text-sm font-semibold text-text">
                      Update available — v{data.latest_version}
                    </p>
                    {data.published_at && (
                      <p className="text-xs text-text-3 mt-0.5">
                        Published {relativeTime(data.published_at)}
                      </p>
                    )}
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    {data.latest_html_url && (
                      <a
                        href={data.latest_html_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 text-sm text-accent-strong hover:opacity-80"
                      >
                        Release notes
                        <ExternalLink size={12} />
                      </a>
                    )}
                    <Button
                      variant="primary"
                      size="sm"
                      disabled={applyUpdate.isPending}
                      onClick={() => {
                        toast.info("Applying update…");
                        applyUpdate.mutate(undefined, {
                          onSuccess: (res) => {
                            if (res.exit_code === 0) {
                              toast.success("Update applied. Restart the daemon to run the new version.");
                            } else {
                              toast.error(`Update exited ${res.exit_code}. Check the output.`);
                            }
                          },
                          onError: () => toast.error("Failed to apply update."),
                        });
                      }}
                    >
                      <Download size={12} />
                      {applyUpdate.isPending ? "Updating…" : "Update now"}
                    </Button>
                  </div>
                </div>
                {data.latest_body && (
                  <pre className="text-xs text-text-3 whitespace-pre-wrap font-sans max-h-32 overflow-y-auto">
                    {data.latest_body.slice(0, 500)}
                    {data.latest_body.length > 500 ? "\n…" : ""}
                  </pre>
                )}
                <code className="block font-mono text-text-2 bg-bg-soft border border-border-soft rounded px-2 py-1 text-xs">
                  grafel update
                </code>
              </div>
            ) : (
              <div className="flex items-center gap-2 text-sm text-success rounded-lg border border-success/30 bg-success/5 p-3">
                <CheckCircle size={14} className="shrink-0" />
                You&apos;re up to date.
                {data.latest_version && (
                  <span className="text-text-3 ml-1">Latest: v{data.latest_version}</span>
                )}
              </div>
            )}

            <div className="pt-2 border-t border-border-soft flex items-center justify-between gap-4">
              <div>
                <p className="text-sm font-medium text-text">Refresh extraction rules</p>
                <p className="text-xs text-text-3 mt-0.5">
                  Updates YAML detection rules without a full binary update.
                </p>
              </div>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  toast.info(
                    "Run `grafel update --refresh-rules-lite` from your terminal.",
                  );
                }}
              >
                <RefreshCw size={12} />
                Refresh rules
              </Button>
            </div>

            <p className="text-xs text-text-4 text-right">
              Checked {relativeTime(data.checked_at)}
            </p>
          </div>
        )}
      </Section>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Screen root
// ---------------------------------------------------------------------------

// Screen insight (#4655) — registered with the breadcrumb Insights button via
// useSetInsight. Module-level constant for stable identity across renders.
const OPERATIONS_INSIGHT: InsightValue = {
  storageKey: "operations",
  human: (
    <>
      Operations — run and inspect the daemon behind this group:
      system health and indexing, the learned-pattern store,
      graph-quality measurement (unresolved references, orphan audit,
      recall), and version updates.
    </>
  ),
  agent: {
    tool: "grafel_repairs",
    example:
      "Before trusting the graph to answer 'who calls this?', an agent calls grafel_repairs to review unresolved references and pending fixes — if a key dynamic-dispatch edge is still unresolved it pauses and asks for a re-index rather than reporting an incomplete caller list as complete.",
  },
};

// ---------------------------------------------------------------------------
// Quarantine tab — index-quarantine transparency surface (Q2, #5617)
//
// The self-healing watcher auto-quarantines directories that churn
// pathologically (a build-output loop, generated content) so they stop arming
// reindexes. This panel surfaces that set with operator override: un-quarantine
// or pin (so auto-heal never touches a dir).
// ---------------------------------------------------------------------------

function QuarantineTab({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useQuarantine(groupId);
  const unquarantine = useUnquarantine(groupId);
  const pin = usePinQuarantine(groupId);

  const entries = data?.entries ?? [];

  return (
    <Section
      title="Quarantined paths"
      sub="Directories the self-healing watcher auto-quarantined for pathological churn (e.g. a build-output loop). They stop arming reindexes until healed or overridden."
    >
      {isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </div>
      ) : isError ? (
        <p className="text-sm text-text-3">Could not load the quarantine set.</p>
      ) : entries.length === 0 ? (
        <div className="flex items-center gap-2 text-sm text-text-3">
          <ShieldAlert size={15} className="text-text-3" />
          No directories are quarantined.
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-border">
          <table className="w-full text-sm" data-testid="quarantine-table">
            <thead className="bg-surface-2 text-left text-xs text-text-3">
              <tr>
                <th className="px-3 py-2 font-medium">Path</th>
                <th className="px-3 py-2 font-medium">Repo</th>
                <th className="px-3 py-2 font-medium">Signal</th>
                <th className="px-3 py-2 font-medium">Detail</th>
                <th className="px-3 py-2 font-medium">Since</th>
                <th className="px-3 py-2 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => {
                const busy =
                  (unquarantine.isPending &&
                    unquarantine.variables?.repo === e.repo &&
                    unquarantine.variables?.rel === e.path) ||
                  (pin.isPending &&
                    pin.variables?.repo === e.repo &&
                    pin.variables?.rel === e.path);
                return (
                  <tr key={`${e.repo}:${e.path}`} className="border-t border-border">
                    <td className="px-3 py-2 font-mono text-xs text-text">
                      <span className="inline-flex items-center gap-1.5">
                        {e.path}
                        {e.pinned && (
                          <Badge tone="info" className="gap-1">
                            <Pin size={10} />
                            pinned
                          </Badge>
                        )}
                      </span>
                    </td>
                    <td className="px-3 py-2 text-text-3">{e.repo}</td>
                    <td className="px-3 py-2 text-text-3">{e.signal}</td>
                    <td className="px-3 py-2 text-text-3">{e.detail}</td>
                    <td className="px-3 py-2 text-text-3">{relativeTime(e.at)}</td>
                    <td className="px-3 py-2">
                      <div className="flex items-center justify-end gap-1.5">
                        <Button
                          size="sm"
                          variant="secondary"
                          disabled={busy}
                          onClick={() =>
                            pin.mutate(
                              { repo: e.repo, rel: e.path, pinned: !e.pinned },
                              {
                                onSuccess: () =>
                                  toast.success(
                                    `${e.pinned ? "Unpinned" : "Pinned"} ${e.repo}/${e.path}`,
                                  ),
                                onError: (err) => toast.error(String(err)),
                              },
                            )
                          }
                        >
                          {e.pinned ? <PinOff size={13} className="mr-1" /> : <Pin size={13} className="mr-1" />}
                          {e.pinned ? "Unpin" : "Pin"}
                        </Button>
                        <Button
                          size="sm"
                          variant="secondary"
                          disabled={busy}
                          onClick={() =>
                            unquarantine.mutate(
                              { repo: e.repo, rel: e.path },
                              {
                                onSuccess: () =>
                                  toast.success(`Un-quarantined ${e.repo}/${e.path}`),
                                onError: (err) => toast.error(String(err)),
                              },
                            )
                          }
                        >
                          <Trash2 size={13} className="mr-1" />
                          Un-quarantine
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  );
}

export default function OperationsScreen() {
  useSetInsight(OPERATIONS_INSIGHT);
  const { groupId = "" } = useParams<{ groupId: string }>();

  return (
    <TooltipProvider>
      <div className="mx-auto w-full max-w-[960px] px-6 py-8" data-testid="operations-screen">
        <header className="mb-6">
          <h1 className="text-xl font-semibold text-text flex items-center gap-2">
            <Server size={18} className="text-text-3" />
            Operations
          </h1>
          <p className="mt-1 text-sm text-text-3">
            Daemon control, pattern store, quality measurement, and version management.
          </p>
        </header>

        <div className="mb-6 space-y-3">
          
        </div>

        <Tabs defaultValue="system">
          <TabsList className="mb-6">
            <TabsTrigger value="system">
              <Activity size={13} className="mr-1.5" />
              System
            </TabsTrigger>
            <TabsTrigger value="patterns">
              <Zap size={13} className="mr-1.5" />
              Patterns
            </TabsTrigger>
            <TabsTrigger value="quality">
              <HardDrive size={13} className="mr-1.5" />
              Quality
            </TabsTrigger>
            <TabsTrigger value="quarantine">
              <ShieldAlert size={13} className="mr-1.5" />
              Quarantine
            </TabsTrigger>
            <TabsTrigger value="updates">
              <Download size={13} className="mr-1.5" />
              Updates
            </TabsTrigger>
          </TabsList>

          <TabsContent value="system">
            <SystemTab groupId={groupId} />
          </TabsContent>

          <TabsContent value="patterns">
            <PatternsTab groupId={groupId} />
          </TabsContent>

          <TabsContent value="quality">
            <QualityTab groupId={groupId} />
          </TabsContent>

          <TabsContent value="quarantine">
            <QuarantineTab groupId={groupId} />
          </TabsContent>

          <TabsContent value="updates">
            <UpdatesTab />
          </TabsContent>
        </Tabs>
      </div>
    </TooltipProvider>
  );
}
