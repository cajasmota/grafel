/* ============================================================
   errors.tsx — 404 + status/error pages.

   A single <ErrorScreen> component plus an ERR_VARIANTS config
   covers all six full-page error states:

     notFound       — 404, minimal chrome (no group context)
     groupGone      — referenced group deleted/renamed, minimal chrome
     daemonDown     — daemon unreachable, full chrome (sidebar useful)
     indexerFailed  — last repo index errored, full chrome
     upgrading      — daemon restarting on new version, full chrome
     offline        — websocket dropped, full chrome

   Routes wired in router.tsx:
     /error/404               → notFound
     /error/daemon-down       → daemonDown
     /error/upgrading         → upgrading
     /g/:groupId/missing      → groupGone (rendered inside group context)

   The `*` wildcard renders NotFound (see not-found.tsx) which calls
   through to <ErrorPage variant="notFound" />.

   Per docs/screens/errors.md — #1443, epic #1432.
   ============================================================ */

import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import {
  Home,
  RefreshCw,
  ExternalLink,
  Plus,
  Info,
} from "lucide-react";
import { Button } from "@/components/ui";
import { ErrorConstellation, type ErrorVariant } from "@/components/viz/error-constellation";
import { cn } from "@/lib/utils";

/* ------------------------------------------------------------------ *
 * Variant spec types
 * ------------------------------------------------------------------ */

interface ActionSpec {
  label: string;
  href?: string;
  action?: "retry" | "rebuild" | "logs" | "notes" | "create";
  Icon?: typeof Home;
}

interface ErrorVariantSpec {
  chrome: "minimal" | "full";
  severity: "neutral" | "danger" | "warn" | "info";
  code: string;
  title: React.ReactNode;
  sub: React.ReactNode;
  primary: ActionSpec | null;
  secondary: ActionSpec | null;
  details: { k: string; v: string }[];
}

/* ------------------------------------------------------------------ *
 * Variant config
 * ------------------------------------------------------------------ */

const ERR_VARIANTS: Record<ErrorVariant, ErrorVariantSpec> = {
  notFound: {
    chrome: "minimal",
    severity: "neutral",
    code: "404",
    title: "We couldn't find that page.",
    sub: "The link may be stale, the surface may have been renamed, or the group it belongs to no longer exists.",
    primary: { label: "Back to groups", href: "/", Icon: Home },
    secondary: null,
    details: [
      { k: "Path", v: typeof window !== "undefined" ? window.location.pathname : "—" },
      { k: "Referrer", v: typeof document !== "undefined" ? document.referrer || "—" : "—" },
    ],
  },
  groupGone: {
    chrome: "minimal",
    severity: "neutral",
    code: "Group not found",
    title: "This group doesn't exist anymore.",
    sub: "It may have been deleted, renamed, or moved to another machine. Your other groups are still here.",
    primary: { label: "Back to groups", href: "/", Icon: Home },
    secondary: { label: "Create a new group", href: "/", Icon: Plus },
    details: [
      { k: "Last seen", v: "—" },
      { k: "Config file", v: "—" },
    ],
  },
  daemonDown: {
    chrome: "full",
    severity: "danger",
    code: "Daemon unreachable",
    title: "archigraph can't reach the daemon.",
    sub: "Your repos are safe — only the local indexing service stopped responding. The dashboard will reconnect automatically once it's back.",
    primary: { label: "Try reconnecting", action: "retry", Icon: RefreshCw },
    secondary: { label: "View daemon logs", action: "logs", Icon: ExternalLink },
    details: [
      { k: "Socket", v: "~/.archigraph/sockets/daemon.sock" },
      { k: "Last contact", v: "—" },
      { k: "Daemon PID", v: "— (not responding)" },
      {
        k: "How to recover",
        v: "Open a terminal and run `archigraph restart`, or check the logs for crashes.",
      },
    ],
  },
  indexerFailed: {
    chrome: "full",
    severity: "warn",
    code: "Indexer failed",
    title: "The last index of this repo errored out.",
    sub: "archigraph rolled back to the previous good graph. Re-run the indexer once the underlying issue is fixed; the rest of the group is unaffected.",
    primary: { label: "Rebuild repo", action: "rebuild", Icon: RefreshCw },
    secondary: { label: "View error log", action: "logs", Icon: ExternalLink },
    details: [
      { k: "Phase", v: "—" },
      { k: "Repo", v: "—" },
      { k: "Error", v: "—" },
      { k: "Started", v: "—" },
    ],
  },
  upgrading: {
    chrome: "full",
    severity: "info",
    code: "Upgrading",
    title: "Updating archigraph in the background…",
    sub: "The daemon is restarting on a new version. Indexing is paused; surfaces are read-only until it's back. Usually under a minute.",
    primary: { label: "View release notes", action: "notes", Icon: ExternalLink },
    secondary: null,
    details: [
      { k: "From", v: "—" },
      { k: "To", v: "—" },
      { k: "Started", v: "—" },
    ],
  },
  offline: {
    chrome: "full",
    severity: "neutral",
    code: "Offline",
    title: "Live updates paused.",
    sub: "The websocket to the daemon dropped. The dashboard keeps showing the last fetched state; new index events won't stream in until the connection returns.",
    primary: { label: "Retry connection", action: "retry", Icon: RefreshCw },
    secondary: null,
    details: [
      { k: "Dropped at", v: "—" },
      { k: "Retries", v: "0 attempts" },
    ],
  },
};

/* ------------------------------------------------------------------ *
 * Severity → token mapping
 * ------------------------------------------------------------------ */

const SEVERITY_CODE_COLOR: Record<string, string> = {
  neutral: "text-text-3",
  danger: "text-danger",
  warn: "text-warning",
  info: "text-info",
};

/* ------------------------------------------------------------------ *
 * <ErrorScreen> — the variant-aware body block
 * ------------------------------------------------------------------ */

interface ErrorScreenProps {
  variant: ErrorVariant;
  /** Dynamic overrides for template fields (e.g. group name, repo slug). */
  context?: {
    groupId?: string;
    repoSlug?: string;
    errorPhase?: string;
    errorMessage?: string;
    lastContact?: string;
    droppedAt?: string;
    retries?: number;
    fromVersion?: string;
    toVersion?: string;
    startedAt?: string;
    configPath?: string;
  };
  /** Called when an action button with `action` fires. */
  onAction?: (action: string) => void;
  /** Whether a retry RPC is in flight (for retry-capable variants). */
  retrying?: boolean;
}

function ErrorScreen({
  variant,
  context = {},
  onAction,
  retrying = false,
}: ErrorScreenProps) {
  const spec = ERR_VARIANTS[variant];
  const [detailsOpen, setDetailsOpen] = useState(false);

  if (!spec) return null;

  // Apply context to dynamic details
  const resolvedDetails = resolveDetails(spec.details, variant, context);

  // Resolve dynamic title
  const resolvedTitle = resolveTitleNode(spec.title, variant, context);

  const codeColor = SEVERITY_CODE_COLOR[spec.severity] ?? "text-text-3";

  function handleAction(a: ActionSpec, e: React.MouseEvent) {
    if (a.action) {
      e.preventDefault();
      onAction?.(a.action);
    }
  }

  return (
    <div className="flex flex-col items-center justify-center min-h-full py-16 px-4">
      <div className="flex flex-col items-center text-center max-w-md w-full gap-5">
        {/* illustration */}
        <ErrorConstellation variant={variant} />

        {/* code label */}
        <p
          className={cn("font-mono text-sm uppercase tracking-wider", codeColor)}
          aria-hidden="true"
        >
          {spec.code}
        </p>

        {/* headline */}
        <h1 className="text-2xl font-semibold text-text -mt-2">{resolvedTitle}</h1>

        {/* sub copy */}
        <p className="text-md text-text-3 leading-relaxed">{spec.sub}</p>

        {/* action row */}
        <div className="flex items-center gap-3 flex-wrap justify-center mt-1">
          {spec.primary && (
            <Button
              asChild={!!spec.primary.href}
              variant="primary"
              onClick={(e) => spec.primary && handleAction(spec.primary, e)}
              disabled={retrying && spec.primary.action === "retry"}
              aria-label={spec.primary.label}
            >
              {spec.primary.href ? (
                <Link to={spec.primary.href}>
                  {spec.primary.Icon && (
                    <spec.primary.Icon size={14} aria-hidden="true" />
                  )}
                  {retrying && spec.primary.action === "retry"
                    ? "Retrying…"
                    : spec.primary.label}
                </Link>
              ) : (
                <>
                  {spec.primary.Icon && (
                    <spec.primary.Icon
                      size={14}
                      aria-hidden="true"
                      className={
                        retrying && spec.primary.action === "retry"
                          ? "animate-spin"
                          : undefined
                      }
                    />
                  )}
                  {retrying && spec.primary.action === "retry"
                    ? "Retrying…"
                    : spec.primary.label}
                </>
              )}
            </Button>
          )}
          {spec.secondary && (
            <Button
              asChild={!!spec.secondary.href}
              variant="secondary"
              onClick={(e) => spec.secondary && handleAction(spec.secondary, e)}
              aria-label={spec.secondary.label}
            >
              {spec.secondary.href ? (
                <Link to={spec.secondary.href}>
                  {spec.secondary.Icon && (
                    <spec.secondary.Icon size={14} aria-hidden="true" />
                  )}
                  {spec.secondary.label}
                </Link>
              ) : (
                <>
                  {spec.secondary.Icon && (
                    <spec.secondary.Icon size={14} aria-hidden="true" />
                  )}
                  {spec.secondary.label}
                </>
              )}
            </Button>
          )}
        </div>

        {/* collapsible technical details */}
        {resolvedDetails.length > 0 && (
          <details
            className="w-full text-left mt-2"
            open={detailsOpen}
            onToggle={(e) => setDetailsOpen((e.target as HTMLDetailsElement).open)}
          >
            <summary className="inline-flex items-center gap-1.5 text-sm text-text-3 cursor-pointer select-none hover:text-text-2 transition-colors list-none">
              <Info size={12} aria-hidden="true" />
              <span>Technical details</span>
            </summary>

            <dl className="mt-3 rounded-md border border-border bg-surface p-3 text-sm space-y-1.5">
              {resolvedDetails.map((d) => (
                <div key={d.k} className="flex gap-3">
                  <dt className="shrink-0 w-28 text-text-3">{d.k}</dt>
                  <dd className="font-mono text-text-2 break-all">{d.v}</dd>
                </div>
              ))}
            </dl>
          </details>
        )}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ *
 * Dynamic context resolution helpers
 * ------------------------------------------------------------------ */

function resolveTitleNode(
  title: React.ReactNode,
  variant: ErrorVariant,
  ctx: ErrorScreenProps["context"],
): React.ReactNode {
  if (variant === "groupGone" && ctx?.groupId) {
    return (
      <>
        <code className="font-mono text-text-2 bg-surface px-1 py-0.5 rounded text-xl">
          {ctx.groupId}
        </code>{" "}
        doesn't exist anymore.
      </>
    );
  }
  if (variant === "indexerFailed" && ctx?.repoSlug) {
    return (
      <>
        The last index of{" "}
        <code className="font-mono text-text-2 bg-surface px-1 py-0.5 rounded text-xl">
          {ctx.repoSlug}
        </code>{" "}
        errored out.
      </>
    );
  }
  return title;
}

function resolveDetails(
  base: { k: string; v: string }[],
  variant: ErrorVariant,
  ctx: ErrorScreenProps["context"],
): { k: string; v: string }[] {
  if (!ctx) return base;

  if (variant === "notFound") {
    return [
      { k: "Path", v: typeof window !== "undefined" ? window.location.pathname : "—" },
      {
        k: "Referrer",
        v: typeof document !== "undefined" ? document.referrer || "—" : "—",
      },
    ];
  }
  if (variant === "groupGone") {
    return [
      { k: "Last seen", v: "—" },
      { k: "Config file", v: ctx.configPath ?? "—" },
    ];
  }
  if (variant === "daemonDown") {
    return [
      { k: "Socket", v: "~/.archigraph/sockets/daemon.sock" },
      { k: "Last contact", v: ctx.lastContact ?? "—" },
      { k: "Daemon PID", v: "— (not responding)" },
      {
        k: "How to recover",
        v: "Open a terminal and run `archigraph restart`, or check the logs for crashes.",
      },
    ];
  }
  if (variant === "indexerFailed") {
    return [
      { k: "Phase", v: ctx.errorPhase ?? "—" },
      { k: "Repo", v: ctx.repoSlug ?? "—" },
      { k: "Error", v: ctx.errorMessage ?? "—" },
      { k: "Started", v: ctx.startedAt ?? "—" },
    ];
  }
  if (variant === "upgrading") {
    return [
      { k: "From", v: ctx.fromVersion ?? "—" },
      { k: "To", v: ctx.toVersion ?? "—" },
      { k: "Started", v: ctx.startedAt ?? "—" },
    ];
  }
  if (variant === "offline") {
    return [
      { k: "Dropped at", v: ctx.droppedAt ?? "—" },
      { k: "Retries", v: ctx.retries != null ? `${ctx.retries} attempts` : "0 attempts" },
    ];
  }
  return base;
}

/* ------------------------------------------------------------------ *
 * Chrome wrappers
 * ------------------------------------------------------------------ */

/** Minimal chrome — brand top-bar only; no sidebar. Used for 404 / groupGone. */
function MinimalChrome({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex flex-col h-full bg-bg">
      <header className="flex items-center h-16 shrink-0 px-6 border-b border-border">
        <Link
          to="/"
          className="flex items-center gap-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] rounded"
          aria-label="archigraph — back to all groups"
        >
          <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
            <defs>
              <linearGradient id="er-brand" x1="0" x2="1" y1="0" y2="1">
                <stop offset="0" stopColor="var(--accent)" />
                <stop offset="1" stopColor="var(--accent-strong)" />
              </linearGradient>
            </defs>
            <circle cx="6" cy="6" r="2.6" fill="url(#er-brand)" />
            <circle cx="18" cy="6" r="2.0" fill="var(--accent)" opacity=".7" />
            <circle cx="12" cy="18" r="2.6" fill="var(--accent-strong)" />
            <path
              d="M7.6 7.6l3 8M16 7.6l-3 8M8 6h8"
              stroke="var(--accent)"
              strokeWidth="1.4"
              fill="none"
            />
          </svg>
          <span className="font-mono text-md font-semibold text-text">archigraph</span>
        </Link>
      </header>
      <main className="flex-1 min-h-0 ag-scroll">{children}</main>
    </div>
  );
}

/* ------------------------------------------------------------------ *
 * Public <ErrorPage> — the page component exported for each route.
 * ------------------------------------------------------------------ */

export interface ErrorPageProps {
  variant?: ErrorVariant;
  context?: ErrorScreenProps["context"];
  onAction?: (action: string) => void;
  retrying?: boolean;
}

/**
 * Top-level error page.
 *
 * - Minimal-chrome variants (notFound, groupGone) render with the brand
 *   bar only — no sidebar (no group context to show).
 * - Full-chrome variants should be mounted inside <AppShell> at an in-group
 *   route (e.g. /g/:groupId/missing). The shell already provides the sidebar.
 *
 * Per errors.md: "404 inside a group context → minimal chrome anyway."
 */
export default function ErrorPage({
  variant = "notFound",
  context,
  onAction,
  retrying,
}: ErrorPageProps) {
  const spec = ERR_VARIANTS[variant];

  const screenBody = (
    <ErrorScreen
      variant={variant}
      context={context}
      onAction={onAction}
      retrying={retrying}
    />
  );

  if (spec?.chrome === "minimal") {
    return <MinimalChrome>{screenBody}</MinimalChrome>;
  }

  // Full-chrome: caller is responsible for mounting inside AppShell.
  // We just render the content area block so the shell's sidebar + topbar wrap it.
  return <div className="h-full bg-bg">{screenBody}</div>;
}

/* ------------------------------------------------------------------ *
 * Named route components — wired individually in router.tsx.
 * ------------------------------------------------------------------ */

/** /error/404 — generic not-found (also the `*` wildcard fallback). */
export function NotFoundPage() {
  return <ErrorPage variant="notFound" />;
}

/** /g/:groupId/missing — group was deleted / renamed. */
export function GroupGonePage() {
  const { groupId } = useParams<{ groupId: string }>();
  return <ErrorPage variant="groupGone" context={{ groupId }} />;
}

/** /error/daemon-down — daemon unreachable. */
export function DaemonDownPage() {
  return (
    <div className="h-full bg-bg">
      <ErrorScreen variant="daemonDown" />
    </div>
  );
}

/** /error/upgrading — daemon restarting on new version. */
export function UpgradingPage() {
  return (
    <div className="h-full bg-bg">
      <ErrorScreen variant="upgrading" />
    </div>
  );
}
