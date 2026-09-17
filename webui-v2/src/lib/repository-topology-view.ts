import type {
  RepositoryTopologyDetail,
  RepositoryTopologyFilters,
} from "@/data/types";

export interface RepositoryRecordLink {
  label: string;
  href: string;
}

export function mergeRepositoryTopologyFilters(
  current: RepositoryTopologyFilters,
  patch: Partial<RepositoryTopologyFilters>,
): RepositoryTopologyFilters {
  const next = { ...current, ...patch };
  if (patch.focus) {
    next.source = "";
    next.target = "";
  } else if (patch.source || patch.target) {
    next.focus = "";
  }
  return next;
}

export function repositoryRecordLinks(
  groupId: string,
  detail: RepositoryTopologyDetail,
): RepositoryRecordLink[] {
  const base = `/g/${encodeURIComponent(groupId)}`;
  const links: RepositoryRecordLink[] = [];
  if (detail.channel === "dubbo") {
    links.push({ label: "Open Dubbo", href: `${base}/dubbo` });
  } else if ((detail.channel === "kafka" || detail.channel === "rabbitmq") && detail.label) {
    links.push({
      label: "Open message topology",
      href: `${base}/topology?channel=${encodeURIComponent(detail.label)}`,
    });
  }

  const entity = detail.source_entity || detail.target_entity;
  const repository = detail.source_entity ? detail.source : detail.target;
  if (entity && repository) {
    const params = new URLSearchParams({ lod: "mid", repos: repository, node: entity });
    links.push({
      label: detail.source_entity ? "Open source entity" : "Open target entity",
      href: `${base}/graph?${params.toString()}`,
    });
  }
  return links;
}
