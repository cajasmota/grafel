/* ============================================================
   AuthSeverityBadge — inline auth-coverage severity badge for the
   Paths screen (#4253, epic #4249).

   Renders a compact, severity-coloured badge derived from an
   AuthEndpointFinding (auth-coverage report). Shows the most
   important signal: NO-AUTH, sensitive op, or IDOR risk. Covered
   endpoints render nothing (the existing Lock chip already signals
   "authenticated"), keeping the rail uncluttered.

   Two sizes:
     - "row"    → tiny dot+label for the left-rail route rows
     - "header" → slightly larger pill for the detail header chip row
   ============================================================ */

import { ShieldAlert, ShieldOff, KeyRound } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AuthEndpointFinding } from "@/data/types";

/** Pick the single most-severe label for a finding. */
function classify(f: AuthEndpointFinding): {
  label: string;
  title: string;
  tone: "danger" | "warning";
  Icon: typeof ShieldAlert;
} | null {
  // No auth detected — the headline risk.
  if (!f.has_auth) {
    if (f.sensitive_op) {
      return {
        label: "NO-AUTH · sensitive",
        title: "No auth policy detected on a sensitive operation",
        tone: "danger",
        Icon: ShieldOff,
      };
    }
    if (f.idor_risk) {
      return {
        label: "NO-AUTH · IDOR",
        title: "No auth policy + user-scoped path param (possible IDOR)",
        tone: "danger",
        Icon: ShieldOff,
      };
    }
    return {
      label: "NO-AUTH",
      title: "No auth policy detected for this endpoint",
      tone: f.severity === "error" ? "danger" : "warning",
      Icon: ShieldOff,
    };
  }
  // Authenticated but flagged as sensitive / IDOR-prone.
  if (f.sensitive_op) {
    return {
      label: "sensitive",
      title: "Sensitive operation — review auth scope",
      tone: "warning",
      Icon: KeyRound,
    };
  }
  if (f.idor_risk) {
    return {
      label: "IDOR",
      title: "User-scoped path param — possible IDOR",
      tone: "warning",
      Icon: ShieldAlert,
    };
  }
  return null;
}

const TONE_CLASSES: Record<"danger" | "warning", string> = {
  danger: "bg-danger-soft text-danger border-danger-soft",
  warning: "bg-warning-soft text-warning border-warning-soft",
};

export function AuthSeverityBadge({
  finding,
  variant = "row",
}: {
  finding: AuthEndpointFinding | undefined;
  variant?: "row" | "header";
}) {
  if (!finding) return null;
  const c = classify(finding);
  if (!c) return null;

  if (variant === "header") {
    return (
      <span
        data-testid="auth-severity-badge"
        title={c.title}
        className={cn(
          "inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full font-medium border",
          TONE_CLASSES[c.tone],
        )}
      >
        <c.Icon size={11} className="shrink-0" />
        {c.label}
      </span>
    );
  }

  // Compact row variant: dot + short label, fits the right-meta cluster.
  return (
    <span
      data-testid="auth-severity-badge"
      title={c.title}
      className={cn(
        "inline-flex items-center gap-0.5 h-4 px-1 rounded text-[9px] font-semibold border select-none",
        TONE_CLASSES[c.tone],
      )}
    >
      <c.Icon size={9} className="shrink-0" />
      {c.label}
    </span>
  );
}
