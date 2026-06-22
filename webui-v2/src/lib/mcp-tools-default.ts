/* ============================================================
   mcp-tools-default.ts — pure helpers for the wizard's "Configure MCP for
   which tools?" step (#5344).

   The smart (B) + remembered (C) default is computed on the BACKEND (see
   internal/install/mcptools) and delivered as `defaultSelected` per tool.
   The frontend's only job is to derive the initial checkbox set from that and
   to decide whether the step is even worth showing.
   ============================================================ */

import type { MCPToolStatus } from "@/data/types";

/**
 * defaultSelectedIds returns the IDs of the tools the backend marked as
 * default-selected — the initial checkbox state for the MCP step.
 */
export function defaultSelectedIds(tools: MCPToolStatus[]): string[] {
  return tools.filter((t) => t.defaultSelected).map((t) => t.id);
}

/**
 * shouldShowMCPStep reports whether the MCP picker should be shown. With ≤1
 * detected tool there is no meaningful choice, so the step is skipped and the
 * single tool (if any) is auto-used.
 */
export function shouldShowMCPStep(tools: MCPToolStatus[]): boolean {
  return tools.length > 1;
}

/**
 * autoMCPSelection returns the MCP selection to use when the picker is SKIPPED
 * (≤1 tool): the single tool's id when exactly one is detected, else undefined
 * (no detected tools → back-compat, register all downstream).
 */
export function autoMCPSelection(tools: MCPToolStatus[]): string[] | undefined {
  if (tools.length === 1) return [tools[0].id];
  return undefined;
}
