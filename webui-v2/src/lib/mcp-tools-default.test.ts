import { describe, it, expect } from "vitest";

import {
  defaultSelectedIds,
  shouldShowMCPStep,
  autoMCPSelection,
} from "./mcp-tools-default";
import type { MCPToolStatus } from "@/data/types";

function tool(id: string, defaultSelected: boolean, hasGrafel = false): MCPToolStatus {
  return { id, displayName: id, hasGrafel, defaultSelected };
}

describe("defaultSelectedIds", () => {
  it("returns only the backend-default-selected tools, in order", () => {
    const tools = [
      tool("claude", true),
      tool("cursor", false),
      tool("windsurf", true),
    ];
    expect(defaultSelectedIds(tools)).toEqual(["claude", "windsurf"]);
  });

  it("returns [] when none are default-selected", () => {
    expect(defaultSelectedIds([tool("a", false), tool("b", false)])).toEqual([]);
  });
});

describe("shouldShowMCPStep", () => {
  it("hides the step for 0 or 1 detected tool", () => {
    expect(shouldShowMCPStep([])).toBe(false);
    expect(shouldShowMCPStep([tool("claude", true)])).toBe(false);
  });

  it("shows the step when more than one tool is detected", () => {
    expect(shouldShowMCPStep([tool("claude", true), tool("cursor", false)])).toBe(true);
  });
});

describe("autoMCPSelection", () => {
  it("auto-selects the single detected tool", () => {
    expect(autoMCPSelection([tool("claude", false)])).toEqual(["claude"]);
  });

  it("returns undefined (back-compat) when no tools are detected", () => {
    expect(autoMCPSelection([])).toBeUndefined();
  });

  it("returns undefined when >1 tool (caller shows the picker instead)", () => {
    expect(autoMCPSelection([tool("a", true), tool("b", true)])).toBeUndefined();
  });
});
