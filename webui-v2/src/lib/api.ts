/* ============================================================
   lib/api.ts — typed archigraph daemon client.

   Thin, typed fetch wrapper. Every screen's data hook calls through
   this client (never raw fetch), so auth headers, base URL, and error
   normalization live in exactly one place.

   The daemon base URL is configurable via VITE_AG_API_BASE so the new
   UI never hardcodes the live :47274 daemon during development.
   ============================================================ */

import type {
  Group,
  Entity,
  Community,
  GraphPayloadWire,
  EntityDetailWire,
} from "@/data/types";

const BASE = import.meta.env.VITE_AG_API_BASE ?? "/api";
const BASE_V2 = import.meta.env.VITE_AG_API_BASE_V2 ?? "/api/v2";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    /** Machine-readable code from a v2 error envelope, when present. */
    public code?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json", ...init?.headers },
    ...init,
  });
  if (!res.ok) {
    throw new ApiError(res.status, `${init?.method ?? "GET"} ${path} failed: ${res.status}`);
  }
  return (await res.json()) as T;
}

/* ------------------------------------------------------------------ *
 * v2 envelope helpers — every /api/v2 response is wrapped in
 *   { ok: true, data, pagination? } | { ok: false, error: { code, message } }
 * (see internal/dashboard/API_V2.md). requestV2 unwraps `data` or throws
 * a typed ApiError carrying the canonical error code.
 * ------------------------------------------------------------------ */

interface V2Ok<T> {
  ok: true;
  data: T;
  pagination?: { limit: number; offset: number; total: number };
}
interface V2Err {
  ok: false;
  error: { code: string; message: string };
}

async function requestV2<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE_V2}${path}`, {
    headers: { "Content-Type": "application/json", ...init?.headers },
    ...init,
  });
  let body: V2Ok<T> | V2Err;
  try {
    body = (await res.json()) as V2Ok<T> | V2Err;
  } catch {
    throw new ApiError(res.status, `${init?.method ?? "GET"} ${path} failed: ${res.status}`);
  }
  if (!body.ok) {
    throw new ApiError(res.status, body.error.message, body.error.code);
  }
  return body.data;
}

/**
 * The typed surface of the daemon. Screen tickets add methods here as
 * they need them; nothing else in the app issues network calls.
 */
export interface Meta {
  version: string;
  api_versions: string[];
  groups: string[];
}

export const api = {
  /** v2 — daemon bootstrap: version, supported surfaces, group slugs. */
  getMeta: () => requestV2<Meta>("/meta"),
  /** v2 — rich group list for the Landing screen. */
  listGroups: () => requestV2<Group[]>("/groups"),
  /** v2 — create an empty group from a name (Landing wizard). */
  createGroup: (name: string) =>
    requestV2<Group>("/groups", { method: "POST", body: JSON.stringify({ name }) }),

  /** v2 — the full graph payload (nodes/edges/communities/repos) for the Graph
   *  screen. `params` maps to the daemon's repo/kind filters. */
  getGraph: (groupId: string, params?: { repos?: string[]; filterKind?: string }) => {
    const qs = new URLSearchParams();
    if (params?.repos && params.repos.length > 0) qs.set("repos", params.repos.join(","));
    if (params?.filterKind) qs.set("filter_kind", params.filterKind);
    const suffix = qs.toString() ? `?${qs.toString()}` : "";
    return requestV2<GraphPayloadWire>(`/graph/${encodeURIComponent(groupId)}${suffix}`);
  },

  // --- v1 surfaces still used by other (placeholder) screens ---
  getGroup: (groupId: string) => request<Group>(`/groups/${groupId}`),
  /** v1 — Tier-3 entity detail for the inspector (lazy, on node click). */
  getEntityDetail: (groupId: string, entityId: string) =>
    request<EntityDetailWire>(
      `/graph/${encodeURIComponent(groupId)}/entity/${encodeURIComponent(entityId)}`,
    ),
  listCommunities: (groupId: string) => request<Community[]>(`/groups/${groupId}/communities`),
  searchEntities: (groupId: string, q: string) =>
    request<Entity[]>(`/groups/${groupId}/entities?q=${encodeURIComponent(q)}`),
};

export type Api = typeof api;
