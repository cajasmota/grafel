/* ============================================================
   CommandPalette — ⌘K PROJECT switcher (#1572).

   Its only job is switching the current PROJECT (group). It does
   NOT navigate screens (that's the left sidebar) and does NOT
   search entities. Opens via:
     • ⌘K / Ctrl+K keyboard shortcut (global listener here).
     • TopBar project-switcher button (calls setCommandOpen(true)).

   Selecting a project navigates to that group, KEEPING the current
   screen when the route supports it (else falls back to graph).

   Rendered once inside AppShell so the groupId param + current
   pathname are always available. Zero business logic outside this
   file.
   ============================================================ */

import { useEffect, useState, useCallback } from "react";
import { Command } from "cmdk";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { useNavigate, useParams, useLocation } from "react-router-dom";
import { Search, Check } from "lucide-react";
import { cn } from "@/lib/utils";
import { healthDisplay, healthTooltip } from "@/lib/health";
import { useAppStore } from "@/store/use-app-store";
import { useGroups } from "@/hooks/use-groups";
import { SCREEN_SEGMENTS } from "./screens";

/** Segment after the group id in the current path (default graph). */
function currentSegment(pathname: string, groupId: string | undefined): string {
  if (!groupId) return "graph";
  const prefix = `/g/${groupId}/`;
  if (!pathname.startsWith(prefix)) return "graph";
  const seg = pathname.slice(prefix.length).split("/")[0] ?? "";
  return seg || "graph";
}

export function CommandPalette() {
  const { groupId } = useParams<{ groupId: string }>();
  const { pathname } = useLocation();
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

  const { data: groups = [] } = useGroups();

  const close = useCallback(() => setOpen(false), [setOpen]);

  // Keep the same screen across a project switch when the route supports it.
  const seg = currentSegment(pathname, groupId);
  const targetScreen = SCREEN_SEGMENTS.has(seg) ? seg : "graph";

  function switchToGroup(gid: string) {
    if (gid !== groupId) navigate(`/g/${gid}/${targetScreen}`);
    close();
  }

  return (
    <DialogPrimitive.Root open={open} onOpenChange={setOpen}>
      <DialogPrimitive.Portal>
        {/* Overlay */}
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-[2px] data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />

        {/* Palette panel */}
        <DialogPrimitive.Content
          aria-label="Switch project"
          className={cn(
            "fixed left-1/2 top-[15%] z-50 w-full max-w-[480px] -translate-x-1/2",
            "rounded-xl border border-border bg-surface shadow-[var(--shadow-4)]",
            "focus-visible:outline-none",
            "data-[state=open]:animate-in data-[state=closed]:animate-out",
            "data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
            "data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95",
            "data-[state=closed]:slide-out-to-top-[2%] data-[state=open]:slide-in-from-top-[2%]",
          )}
        >
          <DialogPrimitive.Title className="sr-only">Switch project</DialogPrimitive.Title>
          <DialogPrimitive.Description className="sr-only">
            Pick a project to switch to. Use arrow keys to move, Enter to select, Escape to close.
          </DialogPrimitive.Description>

          <Command className="flex flex-col">
            {/* Search input */}
            <div className="flex items-center gap-2 px-4 border-b border-border h-12">
              <Search size={15} className="text-text-3 shrink-0" />
              <Command.Input
                value={query}
                onValueChange={setQuery}
                placeholder="Switch project…"
                className={cn(
                  "flex-1 bg-transparent text-md text-text placeholder:text-text-4",
                  "focus-visible:outline-none border-none ring-0",
                )}
              />
            </div>

            {/* Results list */}
            <Command.List className="ag-scroll max-h-[400px] py-2 overflow-y-auto">
              <Command.Empty className="px-4 py-8 text-center text-md text-text-3">
                No projects found.
              </Command.Empty>

              <Command.Group
                heading="Projects"
                className="[&_[cmdk-group-heading]]:px-4 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-text-4 [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide"
              >
                {groups.map((g) => {
                  const active = g.id === groupId;
                  const hd = healthDisplay(g.health);
                  const tip = healthTooltip(g.health, g.fidelity);
                  return (
                    <PaletteItem
                      key={g.id}
                      value={`project ${g.name} ${g.id}`}
                      onSelect={() => switchToGroup(g.id)}
                    >
                      <span
                        className="size-2.5 rounded-full shrink-0"
                        style={{ background: hd.color }}
                        title={tip}
                        aria-label={tip}
                      />
                      <span className={cn("font-mono", active && "font-medium text-text")}>{g.name}</span>
                      {active && <Check size={14} className="ml-auto text-accent shrink-0" />}
                    </PaletteItem>
                  );
                })}
              </Command.Group>
            </Command.List>

            {/* Footer */}
            <div className="flex items-center gap-3 px-4 py-2 border-t border-border text-xs text-text-4">
              <span className="flex items-center gap-1">
                <kbd className="kbd">↑↓</kbd> navigate
              </span>
              <span className="flex items-center gap-1">
                <kbd className="kbd">↵</kbd> switch
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
