/* ============================================================
   Compare/change-list.tsx — scrollable entity change list for the
   compare view (PH5 / #2093).

   Renders three sections (Added / Removed / Modified) with filter chips
   to show/hide each bucket. Supports further filtering by entity kind.
   Clicking a row fires an onSelect callback so the parent can highlight
   the entity in the side-panel graph views.
   ============================================================ */

import { useState, useMemo } from "react";
import { Plus, Minus, Pencil, ChevronDown, ChevronRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { DiffEntityEntry, DiffResult } from "@/data/types";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type ChangeFilter = "added" | "removed" | "modified" | "all";

export interface ChangeListProps {
  diff: DiffResult;
  /** Currently active bucket filter. */
  filter: ChangeFilter;
  onFilterChange: (f: ChangeFilter) => void;
  /** Currently active kind filter ("" = all kinds). */
  kindFilter: string;
  onKindFilterChange: (kind: string) => void;
  /** Called when the user selects an entity row. */
  onEntitySelect?: (entry: DiffEntityEntry, bucket: "added" | "removed" | "modified") => void;
  /** ID of the currently selected entity (highlighted row). */
  selectedEntityId?: string;
  className?: string;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const BUCKET_META = {
  added: {
    icon: Plus,
    label: "Added",
    tone: "success" as const,
    countKey: "entities_added" as const,
  },
  removed: {
    icon: Minus,
    label: "Removed",
    tone: "danger" as const,
    countKey: "entities_removed" as const,
  },
  modified: {
    icon: Pencil,
    label: "Modified",
    tone: "warning" as const,
    countKey: "entities_modified" as const,
  },
} as const;

// ---------------------------------------------------------------------------
// Section component
// ---------------------------------------------------------------------------

interface BucketSectionProps {
  bucket: "added" | "removed" | "modified";
  entries: DiffEntityEntry[];
  kindFilter: string;
  onEntitySelect?: (e: DiffEntityEntry, b: "added" | "removed" | "modified") => void;
  selectedEntityId?: string;
}

function BucketSection({ bucket, entries, kindFilter, onEntitySelect, selectedEntityId }: BucketSectionProps) {
  const [expanded, setExpanded] = useState(true);

  const filtered = kindFilter
    ? entries.filter((e) => e.kind.toLowerCase() === kindFilter.toLowerCase())
    : entries;

  if (filtered.length === 0) return null;

  const meta = BUCKET_META[bucket];
  const Icon = meta.icon;

  return (
    <div className="mb-3">
      {/* Section header */}
      <button
        className="flex items-center gap-1.5 w-full px-2 py-1 rounded hover:bg-surface-2 transition-colors text-left"
        onClick={() => setExpanded((v) => !v)}
      >
        {expanded ? (
          <ChevronDown className="size-3.5 text-text-3 shrink-0" />
        ) : (
          <ChevronRight className="size-3.5 text-text-3 shrink-0" />
        )}
        <Icon className="size-3.5 shrink-0" />
        <span className="text-xs font-medium text-text-1">{meta.label}</span>
        <Badge tone={meta.tone} className="ml-auto">
          {filtered.length}
        </Badge>
      </button>

      {/* Rows */}
      {expanded && (
        <ul className="mt-0.5 space-y-0.5">
          {filtered.map((entry) => (
            <EntityRow
              key={entry.id}
              entry={entry}
              bucket={bucket}
              isSelected={selectedEntityId === entry.id}
              onSelect={onEntitySelect}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Entity row
// ---------------------------------------------------------------------------

interface EntityRowProps {
  entry: DiffEntityEntry;
  bucket: "added" | "removed" | "modified";
  isSelected: boolean;
  onSelect?: (e: DiffEntityEntry, b: "added" | "removed" | "modified") => void;
}

function EntityRow({ entry, bucket, isSelected, onSelect }: EntityRowProps) {
  const borderColor =
    bucket === "added"
      ? "border-l-success"
      : bucket === "removed"
        ? "border-l-danger"
        : "border-l-warning";

  return (
    <li>
      <button
        className={cn(
          "w-full text-left px-3 py-1.5 rounded border-l-2 transition-colors",
          "hover:bg-surface-2 focus-visible:ring-1 focus-visible:ring-accent",
          borderColor,
          isSelected && "bg-accent-soft",
        )}
        onClick={() => onSelect?.(entry, bucket)}
      >
        <div className="flex items-start gap-2">
          <span className="text-xs text-text-3 mt-0.5 shrink-0 w-[80px] truncate">
            {entry.kind}
          </span>
          <span className="text-xs font-mono text-text-1 truncate flex-1">{entry.name}</span>
        </div>
        <div className="text-[10px] text-text-3 mt-0.5 truncate pl-[88px]">
          {entry.source_file}
        </div>
        {entry.modified_fields && entry.modified_fields.length > 0 && (
          <div className="flex flex-wrap gap-1 mt-1 pl-[88px]">
            {entry.modified_fields.map((f) => (
              <span
                key={f}
                className="text-[10px] px-1 rounded bg-warning-soft text-warning font-medium"
              >
                {f}
              </span>
            ))}
          </div>
        )}
      </button>
    </li>
  );
}

// ---------------------------------------------------------------------------
// Filter chips row
// ---------------------------------------------------------------------------

interface FilterChipsProps {
  filter: ChangeFilter;
  onFilterChange: (f: ChangeFilter) => void;
  diff: DiffResult;
  kindFilter: string;
  onKindFilterChange: (k: string) => void;
  allKinds: string[];
}

function FilterChips({
  filter,
  onFilterChange,
  diff,
  kindFilter,
  onKindFilterChange,
  allKinds,
}: FilterChipsProps) {
  const { summary } = diff;

  const bucketChips: { id: ChangeFilter; label: string; count: number; tone: "success" | "danger" | "warning" | "neutral" }[] = [
    { id: "all", label: "All", count: summary.entities_added + summary.entities_removed + summary.entities_modified, tone: "neutral" },
    { id: "added", label: "+Added", count: summary.entities_added, tone: "success" },
    { id: "removed", label: "−Removed", count: summary.entities_removed, tone: "danger" },
    { id: "modified", label: "~Modified", count: summary.entities_modified, tone: "warning" },
  ];

  return (
    <div className="flex flex-wrap gap-1.5 px-2 py-2 border-b border-border">
      {bucketChips.map((chip) => (
        <button
          key={chip.id}
          onClick={() => onFilterChange(chip.id)}
          className={cn(
            "inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium transition-colors",
            filter === chip.id
              ? chip.id === "added"
                ? "bg-success text-white"
                : chip.id === "removed"
                  ? "bg-danger text-white"
                  : chip.id === "modified"
                    ? "bg-warning text-white"
                    : "bg-accent text-white"
              : "bg-surface-2 text-text-2 hover:bg-surface-3",
          )}
        >
          {chip.label}
          <span className="opacity-70">{chip.count}</span>
        </button>
      ))}

      {allKinds.length > 0 && (
        <div className="w-full flex flex-wrap gap-1 mt-1">
          <button
            className={cn(
              "text-[10px] px-1.5 py-0.5 rounded-full transition-colors",
              kindFilter === ""
                ? "bg-surface-3 text-text-1 font-medium"
                : "bg-surface-2 text-text-3 hover:bg-surface-3",
            )}
            onClick={() => onKindFilterChange("")}
          >
            all kinds
          </button>
          {allKinds.map((k) => (
            <button
              key={k}
              className={cn(
                "text-[10px] px-1.5 py-0.5 rounded-full transition-colors",
                kindFilter === k
                  ? "bg-surface-3 text-text-1 font-medium"
                  : "bg-surface-2 text-text-3 hover:bg-surface-3",
              )}
              onClick={() => onKindFilterChange(k)}
            >
              {k}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// ChangeList
// ---------------------------------------------------------------------------

export function ChangeList({
  diff,
  filter,
  onFilterChange,
  kindFilter,
  onKindFilterChange,
  onEntitySelect,
  selectedEntityId,
  className,
}: ChangeListProps) {
  // Collect all unique kinds across all buckets for the kind filter chips.
  const allKinds = useMemo(() => {
    const kinds = new Set<string>();
    for (const e of [...diff.entities.added, ...diff.entities.removed, ...diff.entities.modified]) {
      if (e.kind) kinds.add(e.kind);
    }
    return Array.from(kinds).sort();
  }, [diff]);

  // Determine which buckets to show based on the filter.
  const showAdded = filter === "all" || filter === "added";
  const showRemoved = filter === "all" || filter === "removed";
  const showModified = filter === "all" || filter === "modified";

  const totalVisible =
    (showAdded ? diff.entities.added.length : 0) +
    (showRemoved ? diff.entities.removed.length : 0) +
    (showModified ? diff.entities.modified.length : 0);

  return (
    <div className={cn("flex flex-col h-full overflow-hidden", className)}>
      <FilterChips
        filter={filter}
        onFilterChange={onFilterChange}
        diff={diff}
        kindFilter={kindFilter}
        onKindFilterChange={onKindFilterChange}
        allKinds={allKinds}
      />

      <div className="flex-1 overflow-y-auto px-1 py-2">
        {totalVisible === 0 ? (
          <div className="flex flex-col items-center justify-center h-32 text-text-3 text-sm">
            No changes matching the current filter.
          </div>
        ) : (
          <>
            {showAdded && (
              <BucketSection
                bucket="added"
                entries={diff.entities.added}
                kindFilter={kindFilter}
                onEntitySelect={onEntitySelect}
                selectedEntityId={selectedEntityId}
              />
            )}
            {showRemoved && (
              <BucketSection
                bucket="removed"
                entries={diff.entities.removed}
                kindFilter={kindFilter}
                onEntitySelect={onEntitySelect}
                selectedEntityId={selectedEntityId}
              />
            )}
            {showModified && (
              <BucketSection
                bucket="modified"
                entries={diff.entities.modified}
                kindFilter={kindFilter}
                onEntitySelect={onEntitySelect}
                selectedEntityId={selectedEntityId}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}
