import { useMemo, useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";
import { RepositoryTopologyCanvas } from "@/components/repository-topology/repository-topology-canvas";
import { RepositoryTopologyFilters } from "@/components/repository-topology/repository-topology-filters";
import { RepositoryTopologyToolbar } from "@/components/repository-topology/repository-topology-toolbar";
import { RepositoryDetailPanel } from "@/components/repository-topology/repository-detail-panel";
import { EdgeDetailPanel } from "@/components/repository-topology/edge-detail-panel";
import { RepositoryTopologyState, RepositoryTopologyTruncation } from "@/components/repository-topology/repository-topology-state";
import { useRepositoryTopology } from "@/hooks/use-repository-topology";
import { applyRepositoryTopologyPreset, defaultRepositoryTopologyFilters, parseRepositoryTopologyFilters, serializeRepositoryTopologyFilters, type RepositoryTopologyPreset } from "@/lib/repository-topology-filters";
import { mergeRepositoryTopologyFilters } from "@/lib/repository-topology-view";
import type { RepositoryTopologyDirection } from "@/lib/repository-topology-layout";

export default function RepositoryTopologyScreen() {
  const { groupId = "" } = useParams();
  const [searchParams, setSearchParams] = useSearchParams();
  const [rankdir, setRankdir] = useState<RepositoryTopologyDirection>("LR");
  const filters = useMemo(() => parseRepositoryTopologyFilters(searchParams), [searchParams]);
  const topology = useRepositoryTopology(groupId, filters);
  const selectedRepositoryId = searchParams.get("repo") ?? "";
  const selectedEdgeId = searchParams.get("edge") ?? "";
  const edgePage = Math.max(1, Number(searchParams.get("edge_page")) || 1);
  const selectedRepository = topology.data?.nodes.find((node) => node.id === selectedRepositoryId);
  const selectedEdge = topology.data?.edges.find((edge) => edge.id === selectedEdgeId);
  const repositories = topology.data?.facets.repositories.map((facet) => facet.value) ?? [];

  function writeFilters(next: typeof filters) { setSearchParams(serializeRepositoryTopologyFilters(next)); }
  function patchFilters(patch: Partial<typeof filters>) { writeFilters(mergeRepositoryTopologyFilters(filters, patch)); }
  function setSelection(key: "repo" | "edge", value: string, page?: number) { const next = serializeRepositoryTopologyFilters(filters); if (value) next.set(key, value); if (page) next.set("edge_page", String(page)); setSearchParams(next); }
  function applyPreset(preset: RepositoryTopologyPreset) { writeFilters(applyRepositoryTopologyPreset(preset, filters.focus || repositories[0] || "")); }

  return <div className="flex h-full min-h-0 flex-col bg-surface-0"><header className="border-b border-border px-4 py-3"><h1 className="text-lg font-semibold text-text-1">Repository Topology</h1><p className="text-xs text-text-3">Bounded cross-repository communication and evidence map</p></header><RepositoryTopologyToolbar rankdir={rankdir} onRankdirChange={setRankdir} onPreset={applyPreset} onReset={() => writeFilters(defaultRepositoryTopologyFilters())} />{topology.data && <RepositoryTopologyTruncation data={topology.data} />}<div className="flex min-h-0 flex-1"><RepositoryTopologyFilters filters={filters} repositories={repositories} onChange={patchFilters} /><main className="relative min-w-0 flex-1 p-3"><RepositoryTopologyState loading={topology.isLoading} error={topology.error as Error | null} data={topology.data} onRetry={() => topology.refetch()} />{topology.data && <RepositoryTopologyCanvas className="h-full" nodes={topology.data.nodes} edges={topology.data.edges} rankdir={rankdir} selectedRepositoryId={selectedRepositoryId} selectedEdgeId={selectedEdgeId} onRepositorySelect={(repository) => setSelection("repo", repository.id)} onEdgeSelect={(edge) => setSelection("edge", edge.id, 1)} />}</main>{selectedRepository && <RepositoryDetailPanel groupId={groupId} repository={selectedRepository} edges={topology.data?.edges ?? []} onClose={() => setSelection("repo", "")} onFocus={() => patchFilters({ focus: selectedRepository.repository })} />}{selectedEdge && <EdgeDetailPanel groupId={groupId} edge={selectedEdge} filters={filters} page={edgePage} onPageChange={(page) => setSelection("edge", selectedEdge.id, page)} onClose={() => setSelection("edge", "")} />}</div></div>;
}
