/**
 * AuthSection.tsx — Auth section rendered between Description and Parameters
 * in the endpoint detail pane (#2113).
 *
 * Data source: `detail.auth_policy` (structured, from #1942 Phase 1) when
 * available; falls back to legacy `detail.auth` + `detail.auth_scheme` flags
 * for non-Java / pre-Phase-1 endpoints.
 *
 * Visual:
 *   🔐 Auth · Bearer
 *      Defined in:  SecurityFilter.java:42   → JWTValidator.validate(token)
 *      Roles:       [admin, user]
 *      Scopes:      [read:users]
 *      Annotation:  @RolesAllowed({"admin","user"})
 *
 *   🔓 Public · @PermitAll
 */

import { Lock, LockOpen } from "lucide-react";
import { cn } from "@/lib/utils";
import type { PathDetail } from "@/data/types";
import { truncateFilePath } from "@/lib/path-truncate";
import { truncateFqn } from "@/lib/fqn-truncate";
import { TruncatedPath } from "@/components/common/TruncatedPath";

interface AuthSectionProps {
  detail: PathDetail;
}

/* ---------------------------------------------------------------
   Chip tone → CSS classes
   --------------------------------------------------------------- */
function chipToneClass(tone?: string): string {
  switch (tone) {
    case "accent":
      return "bg-success-soft text-success border border-success-soft";
    case "warning":
      return "bg-warning-soft text-warning border border-warning-soft";
    case "muted":
    default:
      return "bg-surface-2 text-text-3 border border-border";
  }
}

/* ---------------------------------------------------------------
   Helper: method label → human-readable scheme string
   --------------------------------------------------------------- */
function methodToScheme(method: string, authScheme?: string): string {
  if (authScheme) return authScheme;
  switch (method) {
    case "annotation":   return "Bearer";
    case "config":       return "Bearer (config)";
    case "framework_default": return "framework default";
    case "public":       return "PermitAll";
    default:             return method;
  }
}

/* ---------------------------------------------------------------
   AuthSection
   --------------------------------------------------------------- */
export function AuthSection({ detail }: AuthSectionProps) {
  const { auth, auth_scheme, auth_policy, auth_chip, auth_chip_tone } = detail;

  // Derive display state from the most detailed source available.
  const policy = auth_policy;

  // Determine public vs authenticated
  const isPublic = policy
    ? !policy.required
    : !auth;

  // Chip label: use backend-resolved label when available, otherwise synthesise.
  const chipLabel = auth_chip
    ? auth_chip
    : isPublic
      ? "Public"
      : `Auth · ${methodToScheme(policy?.method ?? "unknown", auth_scheme)}`;

  const tone: string = auth_chip_tone ?? (isPublic ? "muted" : "accent");

  return (
    <div
      data-testid="auth-section"
      className="px-4 py-3 border-b border-border-soft"
    >
      {/* Main chip row */}
      <div className="flex items-center gap-2">
        {isPublic ? (
          <LockOpen size={13} className="text-text-3 shrink-0" />
        ) : (
          <Lock size={13} className="text-success shrink-0" />
        )}
        <span
          data-testid="auth-chip"
          className={cn(
            "inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full font-medium",
            chipToneClass(tone),
          )}
        >
          {chipLabel}
        </span>

        {/* Confidence badge when not high */}
        {policy && policy.confidence !== "high" && (
          <span className="text-[10px] text-text-4 italic">
            ({policy.confidence} confidence)
          </span>
        )}
      </div>

      {/* Structured detail rows — only when auth_policy is present */}
      {policy && (
        <div className="mt-2 space-y-1 pl-5">
          {/* Source chain — "Defined in" pointers */}
          {(policy.source_chain?.length ?? 0) > 0 && (
            <div className="flex flex-col gap-0.5">
              <span className="text-[10px] text-text-4 font-medium uppercase tracking-wide">
                Defined in
              </span>
              {policy.source_chain!.map((sig, i) => {
                const fileTrunc = sig.file
                  ? truncateFilePath(`${sig.file}:${sig.line}`)
                  : null;
                const nameTrunc = sig.text
                  ? truncateFqn(sig.text)
                  : null;
                return (
                  <div key={i} className="flex items-center gap-3 flex-wrap">
                    {fileTrunc && (
                      <TruncatedPath
                        value={fileTrunc.full}
                        display={fileTrunc.display}
                        className="text-[11px]"
                      />
                    )}
                    {nameTrunc && (
                      <>
                        {fileTrunc && (
                          <span className="text-text-4 shrink-0">→</span>
                        )}
                        <TruncatedPath
                          value={nameTrunc.full}
                          display={nameTrunc.display}
                          className="text-[11px] text-text-2"
                        />
                      </>
                    )}
                    {sig.kind && (
                      <span className="text-[10px] text-text-4 font-mono bg-surface-2 px-1 rounded">
                        {sig.kind}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          )}

          {/* Roles */}
          {(policy.roles?.length ?? 0) > 0 && (
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-[10px] text-text-4 font-medium w-16 shrink-0">Roles</span>
              <div className="flex flex-wrap gap-1">
                {policy.roles!.map((role) => (
                  <span
                    key={role}
                    className="inline-flex items-center h-4 px-1.5 rounded text-[10px] font-mono bg-accent-soft text-accent border border-accent-soft"
                  >
                    {role}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Scopes */}
          {(policy.scopes?.length ?? 0) > 0 && (
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-[10px] text-text-4 font-medium w-16 shrink-0">Scopes</span>
              <div className="flex flex-wrap gap-1">
                {policy.scopes!.map((scope) => (
                  <span
                    key={scope}
                    className="inline-flex items-center h-4 px-1.5 rounded text-[10px] font-mono bg-surface-2 text-text-3 border border-border"
                  >
                    {scope}
                  </span>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Fallback when no structured policy */}
      {!policy && !auth && (
        <p className="mt-1 pl-5 text-[11px] text-text-4 italic">
          Auth detail not yet exposed by backend for this endpoint.
        </p>
      )}
    </div>
  );
}
