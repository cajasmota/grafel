import { useCallback, useState, type ReactNode } from "react";
import { Bot, Info, UserRound, X } from "lucide-react";

export interface InsightBannerAgent {
  /** The MCP tool name this data backs, e.g. "archigraph_test_coverage". */
  tool: string;
  /**
   * A concrete, scenario-driven example of how an agent uses this data —
   * not a single bland line. e.g. "Before adding a test, an agent calls
   * archigraph_test_coverage to find endpoints with no TESTS edge, then …".
   */
  example: ReactNode;
}

export interface InsightBannerProps {
  /**
   * The plain-language, human-facing description of the screen/tab. Keep it
   * short. `DefTerm` (hover glossary) is usable inside this node.
   */
  human: ReactNode;
  /** How AI agents consume the same data via an MCP tool. */
  agent: InsightBannerAgent;
  /**
   * Persistence key for the collapsed state. Falls back to a single global
   * key so a user who collapses one banner sees them collapsed everywhere.
   */
  storageKey?: string;
}

const STORAGE_PREFIX = "ag.insightBanner.collapsed";
const GLOBAL_KEY = STORAGE_PREFIX;

function keyFor(storageKey?: string): string {
  return storageKey ? `${STORAGE_PREFIX}.${storageKey}` : GLOBAL_KEY;
}

function readCollapsed(storageKey?: string): boolean {
  try {
    return localStorage.getItem(keyFor(storageKey)) === "1";
  } catch {
    return false;
  }
}

function writeCollapsed(storageKey: string | undefined, collapsed: boolean) {
  try {
    localStorage.setItem(keyFor(storageKey), collapsed ? "1" : "0");
  } catch {
    /* ignore quota / privacy-mode errors */
  }
}

/**
 * The unified per-screen insight banner (#4604). One rounded card with two
 * side-by-side columns: LEFT = what this screen means for a human, RIGHT = how
 * AI agents consume the same data via an MCP tool. Collapsible (state persisted
 * in localStorage) so power users can reclaim vertical space. Replaces the
 * separate {@link ScreenDescription} + {@link AgentUsage} pair.
 */
export function InsightBanner({ human, agent, storageKey }: InsightBannerProps) {
  const [collapsed, setCollapsed] = useState(() => readCollapsed(storageKey));

  const setAndPersist = useCallback(
    (next: boolean) => {
      setCollapsed(next);
      writeCollapsed(storageKey, next);
    },
    [storageKey],
  );

  if (collapsed) {
    return (
      <div className="mb-3 max-w-4xl">
        <button
          type="button"
          onClick={() => setAndPersist(false)}
          aria-expanded={false}
          className="inline-flex items-center gap-1.5 rounded-md border border-border bg-surface-2/40 px-2.5 py-1 text-xs text-text-3 transition-colors hover:bg-surface-2 hover:text-text-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
        >
          <Info size={13} className="shrink-0 text-text-4" />
          <span>Show insight</span>
        </button>
      </div>
    );
  }

  return (
    <div className="relative mb-3 max-w-4xl overflow-hidden rounded-lg border border-border bg-surface">
      <button
        type="button"
        onClick={() => setAndPersist(true)}
        aria-label="Collapse insight banner"
        title="Collapse"
        className="absolute right-1.5 top-1.5 z-10 rounded-md p-1 text-text-4 transition-colors hover:bg-surface-2 hover:text-text-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
      >
        <X size={14} />
      </button>

      <div className="grid grid-cols-1 sm:grid-cols-2">
        {/* LEFT — human */}
        <div className="border-l-4 border-success bg-success/5 px-4 py-3">
          <div className="mb-1 inline-flex items-center gap-1.5">
            <UserRound size={14} className="shrink-0 text-success" />
            <span className="text-xs font-semibold uppercase tracking-wide text-text-2">
              For you
            </span>
          </div>
          <div className="text-sm leading-relaxed text-text-3">{human}</div>
        </div>

        {/* RIGHT — agent */}
        <div className="border-l-4 border-warning bg-warning/5 px-4 py-3">
          <div className="mb-1 inline-flex items-center gap-1.5">
            <Bot size={14} className="shrink-0 text-warning" />
            <span className="text-xs font-semibold uppercase tracking-wide text-text-2">
              How agents use this data
            </span>
          </div>
          <p className="text-sm leading-relaxed text-text-3">
            <code className="rounded bg-surface-2 px-1.5 py-0.5 font-mono text-[12px] text-text-2">
              {agent.tool}
            </code>{" "}
            — {agent.example}
          </p>
        </div>
      </div>
    </div>
  );
}
