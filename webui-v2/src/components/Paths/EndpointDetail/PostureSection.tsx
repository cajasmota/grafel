/**
 * PostureSection.tsx — "Posture" + "Effective contract" detail-pane sections
 * for the Paths screen (#4254, epic #4249).
 *
 * Lazy-fetches GET /api/v2/groups/:id/paths/:hash/posture (TanStack Query keyed
 * by group + path hash) when a path is opened. Surfaces two graph-derived facets
 * the flat auth_policy never exposed:
 *
 *   Posture            — per endpoint: deprecation / api_version + rate-limit
 *                        badges, a Throws/error-flow list, feature-gate pills,
 *                        and resolved auth props.
 *   Effective contract — per-verb table: default + error status codes,
 *                        serializer, permissions, and an `inherited` tag for
 *                        MRO-resolved (mixin) handlers.
 *
 * HONESTY: both sections are honest-empty. Posture rows with no facet render
 * "No posture signals"; a null contract (non-ViewSet endpoint) renders the
 * empty-state note rather than a fabricated table.
 */

import { ShieldAlert, FileSignature } from "lucide-react";
import { Badge, Skeleton } from "@/components/ui";
import { SectionHeader } from "@/components/SectionHeader";
import { usePathPosture } from "@/hooks/use-paths";
import type {
  PosturePayload,
  EffectiveContract,
  EffectiveContractResult,
} from "@/data/types";

interface PostureSectionProps {
  groupId: string;
  pathHash: string;
  postureOpen: boolean;
  contractOpen: boolean;
  onTogglePosture: () => void;
  onToggleContract: () => void;
}

/* ----------------------------------------------------------------
   Small presentational helpers
   ---------------------------------------------------------------- */

/** A labelled key→value pill row (rate-limit / deprecation / auth props). */
function PropBadges({ props, tone }: { props?: Record<string, string>; tone: "warning" | "info" | "neutral" }) {
  const entries = Object.entries(props ?? {});
  if (entries.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1">
      {entries.map(([k, v]) => (
        <Badge key={k} tone={tone}>
          <span className="opacity-70">{k}</span>
          <span className="font-mono">{v}</span>
        </Badge>
      ))}
    </div>
  );
}

/** One endpoint's posture facets, or an honest-empty note. */
function PostureRow({ p }: { p: PosturePayload }) {
  const throws = p.error_flow?.throws ?? [];
  const catches = p.error_flow?.catches ?? [];
  const gates = p.feature_gates ?? [];

  return (
    <div className="px-4 py-2.5 border-b border-border-soft last:border-b-0">
      <div className="flex items-center gap-2 mb-1.5">
        {p.method && (
          <span className="text-[10px] font-mono font-semibold text-text-3 uppercase">{p.method}</span>
        )}
        <span className="text-xs text-text-2 font-mono truncate">{p.name ?? p.path ?? p.entity_id}</span>
        <span className="text-[10px] text-text-4">{p.repo}</span>
      </div>

      {!p.has_posture ? (
        <p className="text-[11px] text-text-4 italic">No posture signals on this endpoint.</p>
      ) : (
        <div className="space-y-1.5 pl-1">
          {/* Deprecation / versioning */}
          {p.deprecation && Object.keys(p.deprecation).length > 0 && (
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-[10px] text-text-4 font-medium w-20 shrink-0">Deprecation</span>
              <PropBadges props={p.deprecation} tone="warning" />
            </div>
          )}

          {/* Rate limit */}
          {p.rate_limit && Object.keys(p.rate_limit).length > 0 && (
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-[10px] text-text-4 font-medium w-20 shrink-0">Rate limit</span>
              <PropBadges props={p.rate_limit} tone="info" />
            </div>
          )}

          {/* Throws / error flow */}
          {(throws.length > 0 || catches.length > 0) && (
            <div className="flex items-start gap-2 flex-wrap">
              <span className="text-[10px] text-text-4 font-medium w-20 shrink-0 pt-0.5">Error flow</span>
              <div className="flex flex-wrap gap-1">
                {throws.map((t) => (
                  <Badge key={`t-${t}`} tone="danger">
                    <span className="opacity-70">throws</span>
                    <span className="font-mono">{t}</span>
                  </Badge>
                ))}
                {catches.map((c) => (
                  <Badge key={`c-${c}`} tone="neutral">
                    <span className="opacity-70">catches</span>
                    <span className="font-mono">{c}</span>
                  </Badge>
                ))}
              </div>
            </div>
          )}

          {/* Feature gates */}
          {gates.length > 0 && (
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-[10px] text-text-4 font-medium w-20 shrink-0">Feature gates</span>
              <div className="flex flex-wrap gap-1">
                {gates.map((g) => (
                  <Badge key={g} tone="accent">
                    <span className="font-mono">{g}</span>
                  </Badge>
                ))}
              </div>
            </div>
          )}

          {/* Auth (gRPC/tRPC interceptor + HTTP guard props) */}
          {p.auth && Object.keys(p.auth).length > 0 && (
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-[10px] text-text-4 font-medium w-20 shrink-0">Auth</span>
              <PropBadges props={p.auth} tone="neutral" />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** Per-verb effective-contract table row. */
function ContractRow({ c }: { c: EffectiveContract }) {
  const errs = c.error_statuses ?? [];
  const perms = c.permissions ?? [];
  return (
    <tr className="border-b border-border-soft last:border-b-0 align-top">
      <td className="px-3 py-2">
        <div className="flex items-center gap-1.5">
          <span className="text-[10px] font-mono font-semibold text-text-2 uppercase">{c.verb}</span>
          {c.kind === "inherited" && (
            <Badge tone="info" title={c.source_class ? `inherited from ${c.source_class}` : "MRO-inherited handler"}>
              inherited
            </Badge>
          )}
        </div>
      </td>
      <td className="px-3 py-2">
        <div className="flex flex-wrap gap-1">
          {c.default_status ? (
            <Badge tone="success">{c.default_status}</Badge>
          ) : (
            <span className="text-[11px] text-text-4">—</span>
          )}
          {errs.map((s) => (
            <Badge key={s} tone="danger">{s}</Badge>
          ))}
        </div>
      </td>
      <td className="px-3 py-2 text-[11px] font-mono text-text-2">
        {c.serializer || <span className="text-text-4">—</span>}
      </td>
      <td className="px-3 py-2">
        {perms.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {perms.map((p) => (
              <Badge key={p} tone="neutral">
                <span className="font-mono">{p}</span>
              </Badge>
            ))}
          </div>
        ) : (
          <span className="text-[11px] text-text-4">{c.auth_required ? "auth required" : "—"}</span>
        )}
      </td>
    </tr>
  );
}

function ContractTable({ contract }: { contract: EffectiveContractResult }) {
  const groups = contract.groups ?? [];
  if (groups.length === 0) {
    return (
      <p className="px-4 py-3 text-[11px] text-text-4 italic">
        {contract.note ??
          "No effective contract — this endpoint is not backed by a DRF ViewSet (or a framework base the knowledge pack recognises)."}
      </p>
    );
  }
  return (
    <div className="py-1">
      {groups.map((g, gi) => (
        <div key={`${g.repo}-${g.class}-${gi}`}>
          <div className="flex items-center gap-2 px-4 py-1.5">
            <span className="text-xs font-medium text-text-2 font-mono">{g.class}</span>
            {g.framework && <span className="text-[10px] text-text-4">{g.framework}</span>}
          </div>
          <table className="w-full text-left">
            <thead>
              <tr className="text-[10px] text-text-4 uppercase tracking-wide">
                <th className="px-3 py-1 font-medium">Verb</th>
                <th className="px-3 py-1 font-medium">Status</th>
                <th className="px-3 py-1 font-medium">Serializer</th>
                <th className="px-3 py-1 font-medium">Permissions</th>
              </tr>
            </thead>
            <tbody>
              {g.handlers.map((c, ci) => (
                <ContractRow key={`${c.verb}-${c.path}-${ci}`} c={c} />
              ))}
            </tbody>
          </table>
        </div>
      ))}
    </div>
  );
}

/* ----------------------------------------------------------------
   PostureSection — both sections + lazy query
   ---------------------------------------------------------------- */
export function PostureSection({
  groupId,
  pathHash,
  postureOpen,
  contractOpen,
  onTogglePosture,
  onToggleContract,
}: PostureSectionProps) {
  // Lazy-fetch only when at least one of the two sections is open.
  const enabledHash = postureOpen || contractOpen ? pathHash : null;
  const { data, isLoading, isError, error } = usePathPosture(groupId, enabledHash);

  const endpoints = data?.endpoints ?? [];
  const postureCount = endpoints.filter((e) => e.has_posture).length;
  const contract = data?.contract ?? null;
  const contractCount = (contract?.groups ?? []).reduce((n, g) => n + g.handlers.length, 0);

  return (
    <>
      {/* Posture */}
      <div>
        <SectionHeader
          icon={<ShieldAlert size={14} />}
          title="Posture"
          count={data ? postureCount : undefined}
          infoText="Operational posture resolved from the graph — deprecation / API version, rate limits, thrown exceptions (error flow), and feature-flag gates. Shown only when the endpoint carries the signal."
          open={postureOpen}
          onToggle={onTogglePosture}
        />
        {postureOpen && (
          <div>
            {isLoading ? (
              <div className="px-4 py-3 space-y-2">
                <Skeleton className="h-4 w-2/3" />
                <Skeleton className="h-4 w-1/2" />
              </div>
            ) : isError ? (
              <p className="px-4 py-3 text-[11px] text-danger">
                Failed to load posture{error instanceof Error ? `: ${error.message}` : ""}.
              </p>
            ) : endpoints.length === 0 ? (
              <p className="px-4 py-3 text-[11px] text-text-4 italic">No endpoints resolved for this path.</p>
            ) : (
              endpoints.map((p) => <PostureRow key={p.entity_id} p={p} />)
            )}
          </div>
        )}
      </div>

      {/* Effective contract */}
      <div>
        <SectionHeader
          icon={<FileSignature size={14} />}
          title="Effective contract"
          count={data && contract ? contractCount : undefined}
          infoText="Per-verb effective contract resolved through the MRO/inheritance chain — success + error status codes, serializer, and permission classes. Inherited (mixin) handlers are tagged. Null for non-ViewSet endpoints."
          open={contractOpen}
          onToggle={onToggleContract}
        />
        {contractOpen && (
          <div>
            {isLoading ? (
              <div className="px-4 py-3 space-y-2">
                <Skeleton className="h-4 w-3/4" />
                <Skeleton className="h-4 w-1/2" />
              </div>
            ) : isError ? (
              <p className="px-4 py-3 text-[11px] text-danger">
                Failed to load effective contract{error instanceof Error ? `: ${error.message}` : ""}.
              </p>
            ) : !contract ? (
              <p className="px-4 py-3 text-[11px] text-text-4 italic">
                No effective contract — this endpoint is not backed by a DRF ViewSet.
              </p>
            ) : (
              <ContractTable contract={contract} />
            )}
          </div>
        )}
      </div>
    </>
  );
}
