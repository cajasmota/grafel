import type {
  RepositoryChannel,
  RepositoryDirection,
  RepositoryEvidence,
  RepositoryTopologyFilters,
} from "@/data/types";

export type RepositoryTopologyPreset =
  | "overview"
  | "dubbo"
  | "message-flow"
  | "http"
  | "requirement-impact"
  | "incident"
  | "graph-health"
  | "repository-path";

const channels: RepositoryChannel[] = ["dubbo", "http", "kafka", "rabbitmq", "other"];
const defaultChannels: RepositoryChannel[] = ["dubbo", "http", "kafka", "rabbitmq"];
const evidenceValues: RepositoryEvidence[] = ["confirmed", "inferred", "dangling", "ambiguous", "external"];
const defaultEvidence: RepositoryEvidence[] = ["confirmed", "inferred"];
const directions: RepositoryDirection[] = ["inbound", "outbound", "both"];

export function defaultRepositoryTopologyFilters(): RepositoryTopologyFilters {
  return {
    channels: [...defaultChannels], repos: [], focus: "", direction: "both", depth: 1,
    evidence: [...defaultEvidence], minCount: 1, source: "", target: "", search: "",
  };
}

export function applyRepositoryTopologyPreset(
  preset: RepositoryTopologyPreset,
  focus = "",
): RepositoryTopologyFilters {
  const next = defaultRepositoryTopologyFilters();
  switch (preset) {
    case "dubbo":
      next.channels = ["dubbo"];
      break;
    case "message-flow":
      next.channels = ["kafka", "rabbitmq"];
      break;
    case "http":
      next.channels = ["http"];
      break;
    case "requirement-impact":
      next.focus = focus;
      next.depth = 2;
      break;
    case "incident":
      next.focus = focus;
      next.depth = 3;
      next.evidence = [...evidenceValues];
      break;
    case "graph-health":
      next.channels = [...channels];
      next.evidence = ["ambiguous", "dangling", "external", "inferred"];
      break;
    case "repository-path":
      next.focus = "";
      break;
    case "overview":
      break;
  }
  return next;
}

export function parseRepositoryTopologyFilters(params: URLSearchParams): RepositoryTopologyFilters {
  const defaults = defaultRepositoryTopologyFilters();
  return {
    channels: parseEnumList(params.get("channels"), channels, defaults.channels),
    repos: parseStringList(params.get("repos")),
    focus: params.get("focus")?.trim() ?? "",
    direction: parseEnum(params.get("direction"), directions, defaults.direction),
    depth: parseDepth(params.get("depth"), defaults.depth),
    evidence: parseEnumList(params.get("evidence"), evidenceValues, defaults.evidence),
    minCount: parsePositiveInt(params.get("min_count"), defaults.minCount),
    source: params.get("source")?.trim() ?? "",
    target: params.get("target")?.trim() ?? "",
    search: params.get("q")?.trim() ?? "",
  };
}

export function serializeRepositoryTopologyFilters(filters: RepositoryTopologyFilters): URLSearchParams {
  const params = new URLSearchParams();
  params.set("channels", sortedUnique(filters.channels).join(","));
  if (filters.repos.length > 0) params.set("repos", sortedUnique(filters.repos).join(","));
  if (filters.focus) params.set("focus", filters.focus);
  params.set("direction", filters.direction);
  params.set("depth", String(filters.depth));
  params.set("evidence", sortedUnique(filters.evidence).join(","));
  params.set("min_count", String(Math.max(1, filters.minCount)));
  if (filters.source) params.set("source", filters.source);
  if (filters.target) params.set("target", filters.target);
  if (filters.search) params.set("q", filters.search);
  return params;
}

function parseEnum<T extends string>(raw: string | null, allowed: readonly T[], fallback: T): T {
  return raw !== null && allowed.includes(raw as T) ? raw as T : fallback;
}

function parseEnumList<T extends string>(raw: string | null, allowed: readonly T[], fallback: readonly T[]): T[] {
  if (!raw) return [...fallback];
  const values = sortedUnique(raw.split(",").map((value) => value.trim()).filter(Boolean));
  return values.length > 0 && values.every((value) => allowed.includes(value as T)) ? values as T[] : [...fallback];
}

function parseStringList(raw: string | null): string[] {
  return raw ? sortedUnique(raw.split(",").map((value) => value.trim()).filter(Boolean)) : [];
}

function parseDepth(raw: string | null, fallback: 1 | 2 | 3): 1 | 2 | 3 {
  const value = Number(raw);
  return value === 1 || value === 2 || value === 3 ? value : fallback;
}

function parsePositiveInt(raw: string | null, fallback: number): number {
  const value = Number(raw);
  return Number.isInteger(value) && value >= 1 ? value : fallback;
}

function sortedUnique<T extends string>(values: readonly T[]): T[] {
  return [...new Set(values)].sort() as T[];
}
