/* ============================================================
   components/graph/communities-popover.tsx — top-right communities picker.

   Lists communities; clicking one focuses its nodes (non-members dim).
   ============================================================ */

import { useState } from "react";
import { Popover, PopoverTrigger, PopoverContent, Pill, SearchInput } from "@/components/ui";
import { useGraphStore } from "@/store/use-graph-store";
import type { GraphCommunity } from "@/data/types";

export function CommunitiesPopover({ communities }: { communities: GraphCommunity[] }) {
  const open = useGraphStore((s) => s.communitiesOpen);
  const setOpen = useGraphStore((s) => s.setCommunitiesOpen);
  const focusedCommunityId = useGraphStore((s) => s.focusedCommunityId);
  const setFocusedCommunity = useGraphStore((s) => s.setFocusedCommunity);
  const [q, setQ] = useState("");

  const focused = communities.find((c) => c.id === focusedCommunityId);
  const filtered = communities.filter((c) =>
    c.label.toLowerCase().includes(q.toLowerCase()),
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Pill active={focusedCommunityId != null} count={focused ? undefined : communities.length}>
          {focused ? focused.label : "Communities"}
        </Pill>
      </PopoverTrigger>
      <PopoverContent className="w-[320px] p-0">
        <div className="border-b border-border p-2.5">
          <SearchInput
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Filter communities…"
            autoFocus
          />
        </div>
        {focusedCommunityId != null ? (
          <button
            onClick={() => {
              setFocusedCommunity(null);
              setOpen(false);
            }}
            className="flex w-full items-center justify-between border-b border-border px-3 py-2 text-left text-sm text-accent-strong hover:bg-surface-2"
          >
            Show all
          </button>
        ) : null}
        <ul className="ag-scroll max-h-[320px] overflow-auto py-1">
          {filtered.map((c) => (
            <li key={`${c.repo}-${c.id}`}>
              <button
                onClick={() => {
                  setFocusedCommunity(c.id === focusedCommunityId ? null : c.id);
                  setOpen(false);
                }}
                aria-pressed={c.id === focusedCommunityId}
                className={`flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left hover:bg-surface-2 ${
                  c.id === focusedCommunityId ? "bg-accent-soft" : ""
                }`}
              >
                <span className="flex min-w-0 items-center gap-2">
                  <span
                    className="h-2.5 w-2.5 shrink-0 rounded-full"
                    style={{ background: `var(--pastel-${((c.colorIndex - 1) % 10) + 1})` }}
                  />
                  <span className="truncate text-sm text-text-2">{c.label}</span>
                </span>
                <span className="font-mono text-xs tabular-nums text-text-3">{c.size}</span>
              </button>
            </li>
          ))}
          {filtered.length === 0 ? (
            <li className="px-3 py-2 text-sm text-text-3">No communities match.</li>
          ) : null}
        </ul>
        <div className="border-t border-border px-3 py-2 text-xs text-text-3">
          Click a community to focus its nodes
        </div>
      </PopoverContent>
    </Popover>
  );
}
