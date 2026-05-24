/* ============================================================
   chrome/monorepo-module-list.tsx — monorepo module subtree in the
   nav-rail (M3 / #2180).

   Mirrors the WorktreeList pattern. Shows repos that have declared
   modules as an expandable chevron tree:
     ▶ parent-repo   36     ← collapsed row with module count
     └ payments             ← expanded: N module rows indented
     └ orders
     └ …

   Only rendered when the group's /api/v2/groups response includes a
   non-empty `monorepos` map. Graceful degradation when absent.
   ============================================================ */

import { useState } from "react";
import { ChevronRight, Package } from "lucide-react";
import { cn } from "@/lib/utils";
import { useGroupMonorepos } from "@/hooks/use-group-monorepos";

interface ModuleRowProps {
  subPath: string;
}

function ModuleRow({ subPath }: ModuleRowProps) {
  // Extract the leaf name for display; show full sub-path as tooltip.
  const parts = subPath.split("/");
  const leafName = parts[parts.length - 1];

  return (
    <div
      className="group/mod relative flex items-center h-8 rounded-md px-2.5 mx-2 gap-2 text-text-3 w-[calc(100%-16px)]"
      title={subPath}
      data-testid={`module-row-${subPath}`}
    >
      {/* Indent visual */}
      <span className="w-3 shrink-0 opacity-0 group-hover/rail:opacity-100" aria-hidden>
        └
      </span>

      <Package size={13} className="shrink-0 text-text-3" aria-label="module" />

      <span className="flex-1 truncate font-mono text-xs opacity-0 group-hover/rail:opacity-100 transition-opacity whitespace-nowrap">
        {leafName}
      </span>
    </div>
  );
}

interface ParentRepoRowProps {
  slug: string;
  moduleCount: number;
  isExpanded: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}

function ParentRepoRow({ slug, moduleCount, isExpanded, onToggle, children }: ParentRepoRowProps) {
  return (
    <div className="flex flex-col gap-0">
      {/* Parent repo header row */}
      <button
        onClick={onToggle}
        title={`${slug} · ${moduleCount} modules`}
        className={cn(
          "group/parent relative flex items-center h-9 rounded-md px-2.5 mx-2 gap-2",
          "text-text-2 transition-colors duration-[120ms] w-[calc(100%-16px)]",
          "hover:bg-surface-2",
        )}
        data-testid={`monorepo-parent-${slug}`}
      >
        {/* Chevron — rotates when expanded */}
        <ChevronRight
          size={13}
          className={cn(
            "shrink-0 text-text-3 transition-transform duration-150 opacity-0 group-hover/rail:opacity-100",
            isExpanded && "rotate-90",
          )}
        />

        <span className="flex-1 truncate font-mono text-xs opacity-0 group-hover/rail:opacity-100 transition-opacity whitespace-nowrap text-left">
          {slug}
        </span>

        {/* Module count badge */}
        <span className="shrink-0 text-[9px] tabular-nums text-text-4 opacity-0 group-hover/rail:opacity-100 transition-opacity">
          {moduleCount}
        </span>
      </button>

      {/* Expanded module rows */}
      {isExpanded && children}
    </div>
  );
}

export interface MonorepoModuleListProps {
  /** Resolved group ID (from URL param). */
  groupId: string;
}

/**
 * Renders the monorepo module subtree in the nav-rail.
 * Returns null when the group has no monorepo repos (graceful degradation).
 */
export function MonorepoModuleList({ groupId }: MonorepoModuleListProps) {
  const { monorepos, isLoading, isError } = useGroupMonorepos(groupId);
  // Track expanded state per parent repo slug.
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  if (isLoading || isError || !monorepos) return null;

  const entries = Object.entries(monorepos).filter(([, modules]) => modules.length > 0);
  if (entries.length === 0) return null;

  function toggleSlug(slug: string) {
    setExpanded((prev) => ({ ...prev, [slug]: !prev[slug] }));
  }

  return (
    <div
      className="flex flex-col gap-0 mt-1"
      aria-label="Monorepo modules"
      data-testid="monorepo-module-list"
    >
      <div className="my-1.5 mx-3 border-t border-border" />

      {/* Section header — only visible when rail is expanded */}
      <p className="px-3 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-text-4 select-none opacity-0 group-hover/rail:opacity-100 transition-opacity whitespace-nowrap">
        Modules
      </p>

      {entries.map(([slug, modules]) => (
        <ParentRepoRow
          key={slug}
          slug={slug}
          moduleCount={modules.length}
          isExpanded={!!expanded[slug]}
          onToggle={() => toggleSlug(slug)}
        >
          {modules.map((subPath) => (
            <ModuleRow key={subPath} subPath={subPath} />
          ))}
        </ParentRepoRow>
      ))}
    </div>
  );
}
