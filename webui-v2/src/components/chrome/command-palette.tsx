/* ============================================================
   CommandPalette — ⌘K global command palette.

   Opens via:
     • ⌘K keyboard shortcut (global listener in this component).
     • TopBar "Quick jump · ⌘K" button (calls setCommandOpen(true)).

   Action groups:
     1. Navigate — jump to any screen in the current group.
     2. Entities — search entities via /api/groups/:id/entities?q=
     3. Groups   — switch to a different group (navigates to its graph).

   Rendered once inside AppShell so the groupId param is always
   available. Zero business logic outside this file.
   ============================================================ */

import { useEffect, useState, useCallback } from "react";
import { Command } from "cmdk";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { useNavigate, useParams } from "react-router-dom";
import {
  Network,
  Workflow,
  Radio,
  Route as RouteIcon,
  FileText,
  Settings,
  Inbox,
  Wrench,
  LayoutGrid,
  Search,
  Box,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/store/use-app-store";
import { useGroups } from "@/hooks/use-groups";
import { useEntitySearch } from "@/hooks/use-entity-search";

// ---------------------------------------------------------------------------
// Static navigation actions
// ---------------------------------------------------------------------------

interface NavAction {
  id: string;
  label: string;
  path: string;
  Icon: React.ElementType;
}

const NAV_ACTIONS: NavAction[] = [
  { id: "graph",      label: "Graph",          path: "graph",      Icon: Network   },
  { id: "flows",      label: "Flows",          path: "flows",      Icon: Workflow  },
  { id: "topology",   label: "Topology",       path: "topology",   Icon: Radio     },
  { id: "paths",      label: "Paths",          path: "paths",      Icon: RouteIcon },
  { id: "docs",       label: "Docs",           path: "docs",       Icon: FileText  },
  { id: "settings",   label: "Group settings", path: "settings",   Icon: Settings  },
  { id: "pending",    label: "Pending",        path: "pending",    Icon: Inbox     },
  { id: "operations", label: "Operations",     path: "operations", Icon: Wrench    },
  { id: "landing",    label: "All groups",     path: "/",          Icon: LayoutGrid },
];

// ---------------------------------------------------------------------------
// CommandPalette
// ---------------------------------------------------------------------------

export function CommandPalette() {
  const { groupId } = useParams<{ groupId: string }>();
  const open = useAppStore((s) => s.commandOpen);
  const setOpen = useAppStore((s) => s.setCommandOpen);
  const navigate = useNavigate();

  const [query, setQuery] = useState("");

  // Reset query when palette closes.
  useEffect(() => {
    if (!open) setQuery("");
  }, [open]);

  // Global ⌘K / Ctrl+K listener.
  useEffect(() => {
    function down(e: KeyboardEvent) {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        setOpen(true);
      }
    }
    document.addEventListener("keydown", down);
    return () => document.removeEventListener("keydown", down);
  }, [setOpen]);

  // Data hooks.
  const { data: groups = [] } = useGroups();
  const { data: entities = [], isFetching: entitiesFetching } = useEntitySearch(groupId, query);

  const close = useCallback(() => setOpen(false), [setOpen]);

  function navigateTo(path: string) {
    if (path === "/") {
      navigate("/");
    } else if (groupId) {
      navigate(`/g/${groupId}/${path}`);
    }
    close();
  }

  function navigateToEntity(entityId: string) {
    // Docs no longer browses entities (#1552 — it renders generated markdown).
    // Entity search jumps to the Graph surface focused on the node instead.
    if (groupId) {
      navigate(`/g/${groupId}/graph?node=${encodeURIComponent(entityId)}`);
    }
    close();
  }

  function navigateToGroup(gid: string) {
    navigate(`/g/${gid}/graph`);
    close();
  }

  const hasEntityResults = query.trim().length > 0;

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Portal>
        {/* Overlay */}
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-[2px] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />

        {/* Palette panel */}
        <DialogPrimitive.Content
          aria-label="Command palette"
          className={cn(
            "fixed left-1/2 top-[15%] z-50 w-full max-w-[560px] -translate-x-1/2",
            "rounded-xl border border-border bg-surface shadow-[var(--shadow-4)]",
            "focus-visible:outline-none",
            "data-[state=open]:animate-in data-[state=closed]:animate-out",
            "data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
            "data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
            "data-[state=closed]:slide-out-to-top-[2%] data-[state=open]:slide-in-from-top-[2%]",
          )}
        >
          <DialogPrimitive.Title className="sr-only">Command palette</DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">
            Search and navigate. Use arrow keys to move between items, Enter to select, Escape to close.
          </DialogPrimitive.Description>

          <Command
            className="flex flex-col"
          >
            {/* Search input */}
            <div className="flex items-center gap-2 px-4 border-b border-border h-12">
              <Search size={15} className="text-text-3 shrink-0" />
              <Command.Input
                value={query}
                onValueChange={setQuery}
                placeholder="Search or navigate…"
                className={cn(
                  "flex-1 bg-transparent text-md text-text placeholder:text-text-4",
                  "focus-visible:outline-none border-none ring-0",
                )}
              />
              {entitiesFetching && (
                <span className="text-xs text-text-4">searching…</span>
              )}
            </div>

            {/* Results list */}
            <Command.List className="ag-scroll max-h-[400px] py-2 overflow-y-auto">
              <Command.Empty className="px-4 py-8 text-center text-md text-text-3">
                No results found.
              </Command.Empty>

              {/* Navigate group */}
              <Command.Group
                heading="Navigate"
                className="[&_[cmdk-group-heading]]:px-4 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-text-4 [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide"
              >
                {NAV_ACTIONS.map(({ id, label, path, Icon }) => (
                  <PaletteItem
                    key={id}
                    value={`navigate ${label}`}
                    onSelect={() => navigateTo(path)}
                  >
                    <Icon size={15} className="text-text-3 shrink-0" />
                    <span>{label}</span>
                  </PaletteItem>
                ))}
              </Command.Group>

              {/* Entity search results — only when query is non-empty */}
              {hasEntityResults && (
                <Command.Group
                  heading="Entities"
                  className="[&_[cmdk-group-heading]]:px-4 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-text-4 [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide"
                >
                  {entities.length === 0 && !entitiesFetching && (
                    <div className="px-4 py-2 text-md text-text-4">No entities matched.</div>
                  )}
                  {entities.map((entity) => (
                    <PaletteItem
                      key={entity.id}
                      value={`entity ${entity.qualifiedName} ${entity.kind}`}
                      onSelect={() => navigateToEntity(entity.id)}
                    >
                      <Box size={15} className="text-text-3 shrink-0" />
                      <span className="font-mono text-sm truncate">{entity.qualifiedName}</span>
                      <span className="ml-auto text-xs text-text-4 shrink-0">{entity.kind}</span>
                    </PaletteItem>
                  ))}
                </Command.Group>
              )}

              {/* Groups */}
              <Command.Group
                heading="Groups"
                className="[&_[cmdk-group-heading]]:px-4 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-text-4 [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide"
              >
                {groups.map((g) => (
                  <PaletteItem
                    key={g.id}
                    value={`group ${g.name} ${g.id}`}
                    onSelect={() => navigateToGroup(g.id)}
                  >
                    <span className="size-2 rounded-full bg-accent shrink-0" aria-hidden />
                    <span className="font-mono">{g.name}</span>
                  </PaletteItem>
                ))}
              </Command.Group>
            </Command.List>

            {/* Footer */}
            <div className="flex items-center gap-3 px-4 py-2 border-t border-border text-xs text-text-4">
              <span className="flex items-center gap-1">
                <kbd className="kbd">↑↓</kbd> navigate
              </span>
              <span className="flex items-center gap-1">
                <kbd className="kbd">↵</kbd> select
              </span>
              <span className="flex items-center gap-1">
                <kbd className="kbd">Esc</kbd> close
              </span>
            </div>
          </Command>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

// ---------------------------------------------------------------------------
// PaletteItem — a single selectable row in the palette
// ---------------------------------------------------------------------------

interface PaletteItemProps {
  value: string;
  onSelect: () => void;
  children: React.ReactNode;
}

function PaletteItem({ value, onSelect, children }: PaletteItemProps) {
  return (
    <Command.Item
      value={value}
      onSelect={onSelect}
      className={cn(
        "flex items-center gap-2.5 px-4 py-2 mx-1 rounded-md",
        "text-md text-text cursor-pointer select-none",
        "data-[selected=true]:bg-surface-2 data-[selected=true]:text-text",
        "transition-colors duration-[80ms]",
      )}
    >
      {children}
    </Command.Item>
  );
}
