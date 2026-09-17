import { describe, expect, it } from "vitest";
import {
  applyRepositoryTopologyPreset,
  parseRepositoryTopologyFilters,
  serializeRepositoryTopologyFilters,
} from "./repository-topology-filters";

describe("repository topology filters", () => {
  it("defaults to connected communication channels", () => {
    expect(parseRepositoryTopologyFilters(new URLSearchParams())).toEqual({
      channels: ["dubbo", "http", "kafka", "rabbitmq"],
      repos: [],
      focus: "",
      direction: "both",
      depth: 1,
      evidence: ["confirmed", "inferred"],
      minCount: 1,
      source: "",
      target: "",
      search: "",
    });
  });

  it("normalizes valid URL values and rejects invalid values", () => {
    const params = new URLSearchParams(
      "channels=other,dubbo,dubbo&repos=rule,client,rule&focus=client&direction=outbound&depth=3&evidence=external,confirmed&min_count=2&q=OrderService",
    );
    expect(parseRepositoryTopologyFilters(params)).toEqual({
      channels: ["dubbo", "other"],
      repos: ["client", "rule"],
      focus: "client",
      direction: "outbound",
      depth: 3,
      evidence: ["confirmed", "external"],
      minCount: 2,
      source: "",
      target: "",
      search: "OrderService",
    });

    expect(parseRepositoryTopologyFilters(new URLSearchParams("channels=smtp&direction=sideways&depth=8&evidence=guess&min_count=0")))
      .toEqual(parseRepositoryTopologyFilters(new URLSearchParams()));
  });

  it("expands requirement impact into explicit state", () => {
    const filters = applyRepositoryTopologyPreset("requirement-impact", "client");
    expect(filters.depth).toBe(2);
    expect(filters.focus).toBe("client");
    expect(serializeRepositoryTopologyFilters(filters).get("focus")).toBe("client");
  });

  it("expands every preset without hidden preset state", () => {
    expect(applyRepositoryTopologyPreset("overview").channels).toEqual(["dubbo", "http", "kafka", "rabbitmq"]);
    expect(applyRepositoryTopologyPreset("dubbo").channels).toEqual(["dubbo"]);
    expect(applyRepositoryTopologyPreset("message-flow").channels).toEqual(["kafka", "rabbitmq"]);
    expect(applyRepositoryTopologyPreset("http").channels).toEqual(["http"]);
    expect(applyRepositoryTopologyPreset("incident", "client")).toMatchObject({ focus: "client", depth: 3 });
    expect(applyRepositoryTopologyPreset("graph-health").evidence).toEqual(["ambiguous", "dangling", "external", "inferred"]);
    expect(applyRepositoryTopologyPreset("repository-path")).toMatchObject({ focus: "", source: "", target: "" });
  });

  it("serializes arrays and fields deterministically", () => {
    const filters = parseRepositoryTopologyFilters(new URLSearchParams(
      "repos=rule,client&channels=other,dubbo&evidence=external,confirmed&direction=inbound&depth=2&min_count=3&source=client&target=rule&q=order",
    ));
    expect(serializeRepositoryTopologyFilters(filters).toString()).toBe(
      "channels=dubbo%2Cother&repos=client%2Crule&direction=inbound&depth=2&evidence=confirmed%2Cexternal&min_count=3&source=client&target=rule&q=order",
    );
  });
});
