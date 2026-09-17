import { describe, expect, it } from "vitest";
import { graphRequestMode, graphRequestSearch } from "./graph-request-options";

describe("graph request options", () => {
  it("serializes lod and repositories", () => {
    expect(graphRequestSearch({ lod: "high", repos: ["rules", "client", "client"] }).toString())
      .toBe("lod=high&repos=client%2Crules");
  });

  it("serializes the optional kind filter", () => {
    expect(graphRequestSearch({ lod: "full", filterKind: "Method" }).toString())
      .toBe("lod=full&filter_kind=Method");
  });

  it("disables entity requests in module overview", () => {
    expect(graphRequestMode(true, "error")).toEqual({
      streamEnabled: false,
      fallbackEnabled: false,
      modulesEnabled: true,
    });
  });

  it("enables fallback only after a stream error", () => {
    expect(graphRequestMode(false, "streaming")).toEqual({
      streamEnabled: true,
      fallbackEnabled: false,
      modulesEnabled: false,
    });
    expect(graphRequestMode(false, "error")).toEqual({
      streamEnabled: true,
      fallbackEnabled: true,
      modulesEnabled: false,
    });
  });
});
