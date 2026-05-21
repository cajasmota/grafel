/* ============================================================
   lib/api.ts — typed archigraph daemon client.

   Thin, typed fetch wrapper. Every screen's data hook calls through
   this client (never raw fetch), so auth headers, base URL, and error
   normalization live in exactly one place.

   The daemon base URL is configurable via VITE_AG_API_BASE so the new
   UI never hardcodes the live :47274 daemon during development.
   ============================================================ */

import type { Group, Entity, Community } from "@/data/types";

const BASE = import.meta.env.VITE_AG_API_BASE ?? "/api";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
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

/**
 * The typed surface of the daemon. Screen tickets add methods here as
 * they need them; nothing else in the app issues network calls.
 */
export const api = {
  listGroups: () => request<Group[]>("/groups"),
  getGroup: (groupId: string) => request<Group>(`/groups/${groupId}`),
  listCommunities: (groupId: string) => request<Community[]>(`/groups/${groupId}/communities`),
  searchEntities: (groupId: string, q: string) =>
    request<Entity[]>(`/groups/${groupId}/entities?q=${encodeURIComponent(q)}`),
};

export type Api = typeof api;
