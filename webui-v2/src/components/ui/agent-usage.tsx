import { useCallback, useState } from "react";
import { Bot, ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

export interface AgentUsageProps {
  /** The MCP tool name this data backs, e.g. "archigraph_test_coverage". */
  tool: string;
  /** A one-line use case, e.g. "An agent lists untested endpoints…". */
  example: string;
  /**
   * When true (default) the strip can be collapsed/expanded and the
   * collapsed state is persisted in localStorage.
   */
  collapsible?: boolean;
}

const STORAGE_PREFIX = "ag.agentUsage.collapsed.";

function storageKey(tool: string) {
  return `${STORAGE_PREFIX}${tool}`;
}

function readCollapsed(tool: string): boolean {
  try {
    return localStorage.getItem(storageKey(tool)) === "1";
  } catch {
    return false;
  }
}

/**
 * The "How agents use this data" banner (#4574 part 2). A subtle, theme-tokened
 * callout that tells humans how the same data is consumed by AI agents via an
 * MCP tool. Unobtrusive and (by default) collapsible, with the collapsed state
 * persisted per-tool in localStorage so power users can hide it for good.
 */
export function AgentUsage({ tool, example, collapsible = true }: AgentUsageProps) {
  const [collapsed, setCollapsed] = useState(() =>
    collapsible ? readCollapsed(tool) : false,
  );

  const toggle = useCallback(() => {
    setCollapsed((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(storageKey(tool), next ? "1" : "0");
      } catch {
        /* ignore quota / privacy-mode errors */
      }
      return next;
    });
  }, [tool]);

  const header = (
    <span className="inline-flex items-center gap-1.5 text-text-3">
      <Bot size={13} className="text-text-4 shrink-0" />
      <span className="text-xs font-medium uppercase tracking-wide">
        How agents use this data
      </span>
    </span>
  );

  return (
    <div className="rounded-md border border-border bg-surface-2/40 px-3 py-2 max-w-3xl">
      {collapsible ? (
        <button
          type="button"
          onClick={toggle}
          aria-expanded={!collapsed}
          className="flex w-full items-center justify-between gap-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] rounded-sm"
        >
          {header}
          {collapsed ? (
            <ChevronRight size={14} className="text-text-4 shrink-0" />
          ) : (
            <ChevronDown size={14} className="text-text-4 shrink-0" />
          )}
        </button>
      ) : (
        header
      )}

      {!collapsed && (
        <p
          className={cn(
            "text-sm text-text-3 leading-relaxed",
            collapsible && "mt-1.5",
          )}
        >
          <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[12px] text-text-2">
            {tool}
          </code>{" "}
          — {example}
        </p>
      )}
    </div>
  );
}
