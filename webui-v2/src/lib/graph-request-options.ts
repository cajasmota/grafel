export type LodLevel = "low" | "mid" | "high" | "full";

export interface GraphRequestParams {
  repos?: string[];
  filterKind?: string;
  lod: LodLevel;
}

export function graphRequestSearch(params: GraphRequestParams): URLSearchParams {
  const search = new URLSearchParams();
  search.set("lod", params.lod);
  const repos = [...new Set(params.repos ?? [])].sort();
  if (repos.length > 0) search.set("repos", repos.join(","));
  if (params.filterKind) search.set("filter_kind", params.filterKind);
  return search;
}

export function graphRequestMode(moduleOverview: boolean, streamPhase: string) {
  return {
    streamEnabled: !moduleOverview,
    fallbackEnabled: !moduleOverview && streamPhase === "error",
    modulesEnabled: moduleOverview,
  };
}
