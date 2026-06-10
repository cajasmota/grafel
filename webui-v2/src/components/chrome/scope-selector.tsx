/* ============================================================
   chrome/scope-selector.tsx — topbar repo/module SCOPE picker (#4637).

   Renders in the TopBar (between the breadcrumb and the ref selector) for
   groups that contain MORE THAN ONE repo, or a monorepo with multiple
   declared modules. Single-repo groups render nothing — no clutter.

   Clicking opens a Popover listing "All" plus every repo and, for
   monorepos, each module nested under its parent repo. Picking a scope
   writes `?scope=` (via the shared ScopeProvider) which every in-group
   screen reads to filter its lists/tables.

   Mirrors the visual language of the RefSelector so the two topbar pickers
   feel like siblings, and reuses RepoChip for repo identity colour.
   ============================================================ */

import { useState } from "react";
import { Layers, ChevronsUpDown, Check, FolderTree } from "lucide-react";
import { Popover, PopoverTrigger, PopoverContent } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { RepoChip } from "@/lib/repo-color";
import { useScope, type ScopeOption } from "@/lib/scope-context";

function ScopeRow({
  option,
  groupId,
  isSelected,
  onSelect,
}: {
  option: ScopeOption;
  groupId: string;
  isSelected: boolean;
  onSelect: (value: string) => void;
}) {
  const isModule = option.kind === "module";
  return (
    <button
      role="option"
      aria-selected={isSelected}
      onClick={() => onSelect(option.value)}
      className={cn(
        "w-full flex items-center gap-2 px-2.5 py-1.5 text-left rounded-md text-md",
        "hover:bg-surface-2 transition-colors",
        // Indent module rows under their parent repo.
        isModule && "pl-7",
        isSelected && "bg-accent-soft text-accent-strong",
      )}
    >
      {option.kind === "all" ? (
        <>
          <Layers size={13} className="shrink-0 text-text-3" />
          <span className="flex-1 text-sm font-medium">All repos &amp; modules</span>
        </>
      ) : isModule ? (
        <>
          <FolderTree size={12} className="shrink-0 text-text-4" />
          <span className="flex-1 truncate font-mono text-xs">{option.label}</span>
        </>
      ) : (
        <>
          <RepoChip slug={option.repo} groupId={groupId} maxLength={24} />
          <span className="flex-1" />
        </>
      )}
      {isSelected && <Check size={13} className="shrink-0 text-accent-strong" />}
    </button>
  );
}

export interface ScopeSelectorProps {
  groupId: string;
}

export function ScopeSelector({ groupId }: ScopeSelectorProps) {
  const [open, setOpen] = useState(false);
  const { options, active, hasMultiple, setScope } = useScope();

  // Single-repo, non-monorepo groups: nothing to scope — render nothing.
  if (!hasMultiple) return null;

  function handleSelect(value: string) {
    setScope(value);
    setOpen(false);
  }

  const isAll = active.kind === "all";

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          aria-label="Filter by repo or module"
          aria-haspopup="listbox"
          aria-expanded={open}
          className={cn(
            "inline-flex items-center gap-1.5 h-7 pl-2 pr-1.5 rounded-md border text-md transition-colors",
            isAll
              ? "border-border bg-surface text-text-2 hover:bg-surface-2"
              : "border-accent/40 bg-accent-soft text-accent-strong hover:bg-accent-soft/80",
          )}
          data-testid="scope-selector-trigger"
        >
          <Layers size={12} className="shrink-0" />
          {isAll ? (
            <span className="text-xs">All</span>
          ) : active.kind === "module" ? (
            <span className="font-mono text-xs truncate max-w-[160px]">{active.label}</span>
          ) : (
            <span className="font-mono text-xs truncate max-w-[160px]">{active.repo}</span>
          )}
          <ChevronsUpDown size={12} className="shrink-0 opacity-60 ml-0.5" />
        </button>
      </PopoverTrigger>

      <PopoverContent
        align="start"
        className="w-[300px] p-1 max-h-[480px] overflow-y-auto"
        role="listbox"
        aria-label="Select scope"
        data-testid="scope-selector-popover"
      >
        <p className="px-2.5 py-1 text-[10px] font-semibold uppercase tracking-wider text-text-4 select-none">
          Scope
        </p>
        {options.map((opt) => (
          <ScopeRow
            key={opt.value || "__all__"}
            option={opt}
            groupId={groupId}
            isSelected={opt.value === active.value}
            onSelect={handleSelect}
          />
        ))}
      </PopoverContent>
    </Popover>
  );
}
