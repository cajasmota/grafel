import { describe, expect, it } from "vitest";
import type { RepositoryTopologyDetail, RepositoryTopologyFilters } from "@/data/types";
import {
  mergeRepositoryTopologyFilters,
  repositoryRecordLinks,
} from "./repository-topology-view";

const filters: RepositoryTopologyFilters = {
  channels: ["dubbo", "http"],
  repos: [],
  focus: "client",
  direction: "both",
  depth: 2,
  evidence: ["confirmed", "inferred"],
  minCount: 1,
  source: "",
  target: "",
  search: "",
};

describe("repository topology view state", () => {
  it("keeps focus and path modes mutually exclusive", () => {
    expect(mergeRepositoryTopologyFilters(filters, { source: "client", target: "rule" }))
      .toMatchObject({ focus: "", source: "client", target: "rule" });
    expect(mergeRepositoryTopologyFilters({ ...filters, focus: "", source: "client", target: "rule" }, { focus: "gateway" }))
      .toMatchObject({ focus: "gateway", source: "", target: "" });
  });

  it("creates only evidence-backed drill-down links", () => {
    const record: RepositoryTopologyDetail = {
      id: "link-1",
      source: "client",
      target: "rule",
      channel: "dubbo",
      identifier: "com.example.RuleService",
      label: "RuleService",
      evidence: "confirmed",
      source_entity: "client:RuleClient.call",
      properties: {},
    };
    expect(repositoryRecordLinks("sample-platform", record)).toEqual([
      { label: "Open Dubbo", href: "/g/sample-platform/dubbo" },
      { label: "Open source entity", href: "/g/sample-platform/graph?lod=mid&repos=client&node=client%3ARuleClient.call" },
    ]);

    expect(repositoryRecordLinks("sample-platform", {
      ...record,
      channel: "kafka",
      source_entity: undefined,
      target_entity: undefined,
      identifier: undefined,
      label: "orders.created",
    })).toEqual([
      { label: "Open message topology", href: "/g/sample-platform/topology?channel=orders.created" },
    ]);

    expect(repositoryRecordLinks("sample-platform", {
      ...record,
      channel: "other",
      source_entity: undefined,
      target_entity: undefined,
      identifier: undefined,
      label: "unresolved",
    })).toEqual([]);
  });
});
