/* ============================================================
   docs.tsx — Docs screen: entity browser + documentation reader.
   Route: /g/:groupId/docs  and  /g/:groupId/docs/:entityId

   Layout: two-pane (DocsTree fixed 320px left + scrollable article right).
   The DocsTopBar is rendered inline here (not via AppShell TopBar) because
   it needs a center search input + right "Updated X ago" hint that differ
   from the standard TopBar chrome.

   States:
   - Empty     — no entityId → DocsEmpty
   - Loading   — DocsEntitySkeleton
   - Loaded    — DocsEntity (may have stub=true → EntityStub)
   - 404       — redirects to base /g/:groupId/docs
   ============================================================ */

import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Search, X } from "lucide-react";
import { Kbd } from "@/components/ui";
import { useDocsTree, useDocsEntity } from "@/hooks/use-docs";
import { ApiError } from "@/lib/api";
import { DocsTree } from "@/components/docs/docs-tree";
import { DocsEntity } from "@/components/docs/docs-entity";
import { DocsEmpty } from "@/components/docs/docs-empty";
import { DocsEntitySkeleton } from "@/components/docs/docs-skeleton";

// ── Inline DocsTopBar ─────────────────────────────────────────────────────────
// Differs from AppShell TopBar: center search + right "Updated" hint.

function DocsTopBar({
  group,
  search,
  onSearch,
}: {
  group: string;
  search: string;
  onSearch: (v: string) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);

  // "/" shortcut — focus search input (not when already in an input).
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "/" && document.activeElement?.tagName !== "INPUT") {
        e.preventDefault();
        inputRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  return (
    <div className="flex items-center justify-between h-11 shrink-0 px-4 gap-4 border-b border-border bg-bg">
      {/* Left: compact breadcrumb */}
      <span className="text-sm text-text-3 font-mono shrink-0">{group} / Docs</span>

      {/* Center: search */}
      <div className="relative flex items-center flex-1 max-w-xs">
        <Search size={13} className="absolute left-2.5 text-text-4 pointer-events-none" />
        <input
          ref={inputRef}
          type="text"
          className="w-full pl-8 pr-8 h-7 rounded-md bg-surface border border-border text-sm text-text placeholder:text-text-4 focus:outline-none focus:ring-1 focus:ring-[var(--accent)] focus:border-[var(--accent)]"
          placeholder="Search docs by entity name…"
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          autoFocus
        />
        {search ? (
          <button
            className="absolute right-2 text-text-4 hover:text-text-2"
            onClick={() => onSearch("")}
            aria-label="Clear"
          >
            <X size={11} />
          </button>
        ) : (
          <Kbd className="absolute right-2 text-[10px]">/</Kbd>
        )}
      </div>

      {/* Right: last-updated hint (static; real value pending metadata endpoint) */}
      <span className="text-xs text-text-3 shrink-0">Updated just now</span>
    </div>
  );
}

// ── DocsScreen ────────────────────────────────────────────────────────────────

export default function DocsScreen() {
  const { groupId = "demo", entityId } = useParams<{
    groupId: string;
    entityId?: string;
  }>();
  const navigate = useNavigate();

  const [search, setSearch] = useState("");
  const [selectedId, setSelectedId] = useState<string | null>(entityId ?? null);

  // Keep local selectedId in sync with URL.
  useEffect(() => {
    setSelectedId(entityId ?? null);
  }, [entityId]);

  const handleSelect = useCallback(
    (id: string) => {
      setSelectedId(id);
      navigate(`/g/${groupId}/docs/${encodeURIComponent(id)}`, { replace: false });
    },
    [groupId, navigate],
  );

  const { data: tree = [], isLoading: treeLoading } = useDocsTree(groupId);
  const {
    data: entity,
    isLoading: entityLoading,
    error: entityError,
  } = useDocsEntity(groupId, selectedId);

  // On 404, redirect to the base docs page (entity not in graph).
  useEffect(() => {
    if (entityError instanceof ApiError && entityError.status === 404) {
      navigate(`/g/${groupId}/docs`, { replace: true });
    }
  }, [entityError, groupId, navigate]);

  // Debounce search ~120ms per spec edge-cases section.
  const [debouncedSearch, setDebouncedSearch] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), 120);
    return () => clearTimeout(t);
  }, [search]);

  return (
    <div className="flex flex-col h-full">
      <DocsTopBar group={groupId} search={search} onSearch={setSearch} />

      <div className="flex flex-1 min-h-0">
        {/* Left pane: entity tree */}
        {treeLoading ? (
          <div className="w-[320px] shrink-0 border-r border-border flex items-center justify-center">
            <span className="text-sm text-text-4">Loading…</span>
          </div>
        ) : (
          <DocsTree
            tree={tree}
            selectedId={selectedId}
            onSelect={handleSelect}
            query={debouncedSearch}
          />
        )}

        {/* Right pane: entity detail */}
        <div className="flex-1 overflow-y-auto">
          {!selectedId ? (
            <DocsEmpty />
          ) : entityLoading ? (
            <DocsEntitySkeleton />
          ) : entity ? (
            <DocsEntity entity={entity} />
          ) : null}
        </div>
      </div>
    </div>
  );
}
