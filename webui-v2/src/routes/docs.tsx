/* ============================================================
   docs.tsx — Docs screen: GENERATED markdown documents (#1552).
   Route: /g/:groupId/docs  and  /g/:groupId/docs/<repoSlug/rel/path.md>

   The Docs screen renders the markdown produced by the `generate-docs`
   SKILL (run by the user's coding agent) — NOT the entity graph. The left
   pane lists generated documents grouped per-repo → category; the right
   pane renders the selected document's markdown.

   States:
   - No docs generated → DocsNotGenerated (whole screen)
   - Docs exist, none selected → DocsPickDocument (right pane)
   - Loading page → skeleton text
   - Loaded → DocsReader
   - 404 page → redirects to base /g/:groupId/docs
   ============================================================ */

import { useState, useEffect, useCallback, useRef } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Search, X } from "lucide-react";
import { Kbd } from "@/components/ui";
import { useDocsTree, useDocPage } from "@/hooks/use-docs";
import { ApiError } from "@/lib/api";
import { DocsTree } from "@/components/docs/docs-tree";
import { DocsReader } from "@/components/docs/docs-reader";
import { DocsNotGenerated, DocsPickDocument } from "@/components/docs/docs-empty";

// ── Inline DocsTopBar ─────────────────────────────────────────────────────────

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
      <span className="text-sm text-text-3 font-mono shrink-0">{group} / Docs</span>

      <div className="relative flex items-center flex-1 max-w-xs">
        <Search size={13} className="absolute left-2.5 text-text-4 pointer-events-none" />
        <input
          ref={inputRef}
          type="text"
          className="w-full pl-8 pr-8 h-7 rounded-md bg-surface border border-border text-sm text-text placeholder:text-text-4 focus:outline-none focus:ring-1 focus:ring-[var(--accent)] focus:border-[var(--accent)]"
          placeholder="Search documents…"
          value={search}
          onChange={(e) => onSearch(e.target.value)}
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

      <span className="text-xs text-text-3 shrink-0">Generated docs</span>
    </div>
  );
}

// ── DocsScreen ────────────────────────────────────────────────────────────────

export default function DocsScreen() {
  const { groupId = "demo" } = useParams<{ groupId: string }>();
  // The doc key lives in the wildcard "*" param (may contain slashes).
  const params = useParams();
  const wildcardPath = (params["*"] ?? "") as string;
  const navigate = useNavigate();

  const [search, setSearch] = useState("");
  const [selectedPath, setSelectedPath] = useState<string | null>(wildcardPath || null);

  useEffect(() => {
    setSelectedPath(wildcardPath || null);
  }, [wildcardPath]);

  const handleSelect = useCallback(
    (path: string) => {
      setSelectedPath(path);
      // Encode each segment so slashes in the key are preserved in the URL.
      const encoded = path.split("/").map(encodeURIComponent).join("/");
      navigate(`/g/${groupId}/docs/${encoded}`, { replace: false });
    },
    [groupId, navigate],
  );

  const { data: tree = [], isLoading: treeLoading } = useDocsTree(groupId);
  const {
    data: page,
    isLoading: pageLoading,
    error: pageError,
  } = useDocPage(groupId, selectedPath);

  // On 404 (doc removed/renamed), redirect to the base docs page.
  useEffect(() => {
    if (pageError instanceof ApiError && pageError.status === 404) {
      navigate(`/g/${groupId}/docs`, { replace: true });
    }
  }, [pageError, groupId, navigate]);

  // Debounce search ~120ms.
  const [debouncedSearch, setDebouncedSearch] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search), 120);
    return () => clearTimeout(t);
  }, [search]);

  const hasDocs = tree.length > 0;

  return (
    <div className="flex flex-col h-full">
      <DocsTopBar group={groupId} search={search} onSearch={setSearch} />

      {/* No documents generated yet → whole-screen agent-skill empty state. */}
      {!treeLoading && !hasDocs ? (
        <DocsNotGenerated />
      ) : (
        <div className="flex flex-1 min-h-0">
          {/* Left pane: document index */}
          {treeLoading ? (
            <div className="w-[320px] shrink-0 border-r border-border flex items-center justify-center">
              <span className="text-sm text-text-4">Loading…</span>
            </div>
          ) : (
            <DocsTree
              tree={tree}
              selectedPath={selectedPath}
              onSelect={handleSelect}
              query={debouncedSearch}
            />
          )}

          {/* Right pane: rendered markdown */}
          <div className="flex-1 overflow-y-auto">
            {!selectedPath ? (
              <DocsPickDocument />
            ) : pageLoading ? (
              <div className="mx-auto max-w-3xl px-8 py-6 space-y-3 animate-pulse">
                <div className="h-7 w-1/2 rounded bg-surface-2" />
                <div className="h-4 w-full rounded bg-surface-2" />
                <div className="h-4 w-5/6 rounded bg-surface-2" />
                <div className="h-4 w-2/3 rounded bg-surface-2" />
              </div>
            ) : page ? (
              <DocsReader page={page} />
            ) : null}
          </div>
        </div>
      )}
    </div>
  );
}
