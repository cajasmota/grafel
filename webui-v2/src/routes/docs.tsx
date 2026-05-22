/* ============================================================
   docs.tsx — Docs screen: GENERATED markdown documents (#1552, #1584).
   Route: /g/:groupId/docs  and  /g/:groupId/docs/<repoSlug/rel/path.md>

   States:
   - No skill-generated docs → DocsNotGenerated (whole screen) with copy-prompt CTA
   - Docs exist, none selected → DocsPickDocument (right pane)
   - Loading page → skeleton text
   - Loaded → DocsReader
   - 404 page → redirects to base /g/:groupId/docs
   ============================================================ */

import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { Search, X, ChevronLeft } from "lucide-react";
import { Kbd } from "@/components/ui";
import { useDocsTree, useDocPage } from "@/hooks/use-docs";
import { ApiError } from "@/lib/api";
import { DocsTree } from "@/components/docs/docs-tree";
import { DocsReader } from "@/components/docs/docs-reader";
import { DocsNotGenerated, DocsPickDocument } from "@/components/docs/docs-empty";
import { DocsChooser, BusinessNotGenerated, type DocTier } from "@/components/docs/docs-chooser";

// Walk a doc tree collecting every leaf doc path key.
function collectPaths(nodes: { path?: string; children?: unknown }[]): Set<string> {
  const out = new Set<string>();
  const walk = (n: { path?: string; children?: { path?: string; children?: unknown }[] }) => {
    if (n.path) out.add(n.path);
    n.children?.forEach((c) => walk(c as never));
  };
  nodes.forEach((n) => walk(n as never));
  return out;
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
  // null = show the Technical/Business chooser entry. Set once the user picks
  // a tier (or implied by a deep-linked document).
  const [tier, setTier] = useState<DocTier | null>(null);

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

  const { data: treeResult, isLoading: treeLoading } = useDocsTree(groupId);
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

  // Split tree into skill-generated nodes vs raw repo-doc nodes.
  const skillNodes = useMemo(
    () => (treeResult?.nodes ?? []).filter((n) => !n.isRepoDocs),
    [treeResult],
  );
  const repoDocNodes = useMemo(
    () => (treeResult?.nodes ?? []).filter((n) => n.isRepoDocs),
    [treeResult],
  );

  const businessNodes = useMemo(
    () => treeResult?.businessNodes ?? [],
    [treeResult],
  );
  const hasBusinessDocs = businessNodes.length > 0;

  const hasSkillDocs =
    !treeLoading && (treeResult?.skillGenerated ?? false) && skillNodes.length > 0;

  // A deep-linked document implies its tier: infer from the business path set.
  const businessPathSet = useMemo(() => collectPaths(businessNodes), [businessNodes]);
  useEffect(() => {
    if (!selectedPath) return;
    setTier((prev) => prev ?? (businessPathSet.has(selectedPath) ? "business" : "technical"));
  }, [selectedPath, businessPathSet]);

  // Returning to the chooser: clear tier + any open document.
  const backToChooser = useCallback(() => {
    setTier(null);
    setSelectedPath(null);
    setSearch("");
    navigate(`/g/${groupId}/docs`, { replace: false });
  }, [groupId, navigate]);

  // Keyboard: "/" focuses search input
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "/" && document.activeElement?.tagName !== "INPUT") {
        e.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  // No skill-generated docs at all → onboarding CTA (whole screen, no chooser).
  if (!treeLoading && !hasSkillDocs) {
    return (
      <div className="flex flex-col h-full">
        <DocsNotGenerated groupId={groupId} />
      </div>
    );
  }

  // Docs entry: Technical vs Business chooser (no tier picked yet).
  if (!treeLoading && tier === null) {
    return (
      <div className="flex flex-col h-full">
        <DocsChooser onPick={setTier} hasBusiness={hasBusinessDocs} />
      </div>
    );
  }

  // Business tier selected but no business docs present → empty onboarding.
  if (tier === "business" && !hasBusinessDocs && !treeLoading) {
    return (
      <div className="flex flex-col h-full">
        <TierHeader tier="business" onBack={backToChooser} />
        <BusinessNotGenerated />
      </div>
    );
  }

  const activeNodes = tier === "business" ? businessNodes : skillNodes;
  const activeRepoDocs = tier === "business" ? [] : repoDocNodes;

  return (
    <div className="flex flex-col h-full">
      {/* Controls row: back-to-chooser + search for the document tree */}
      <div className="flex items-center gap-3 h-10 shrink-0 px-4 border-b border-border bg-bg">
        <button
          onClick={backToChooser}
          className="flex items-center gap-1 text-xs text-text-3 hover:text-text shrink-0 -ml-1 pr-1"
          aria-label="Back to documentation chooser"
        >
          <ChevronLeft size={14} />
          {tier === "business" ? "Business" : "Technical"}
        </button>
        <div className="relative flex items-center flex-1 max-w-xs">
          <Search size={13} className="absolute left-2.5 text-text-4 pointer-events-none" />
          <input
            ref={searchRef}
            type="text"
            className="w-full pl-8 pr-8 h-7 rounded-md bg-surface border border-border text-sm text-text placeholder:text-text-4 focus:outline-none focus:ring-1 focus:ring-[var(--accent)] focus:border-[var(--accent)]"
            placeholder="Search documents…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {search ? (
            <button
              className="absolute right-2 text-text-4 hover:text-text-2"
              onClick={() => setSearch("")}
              aria-label="Clear"
            >
              <X size={11} />
            </button>
          ) : (
            <Kbd className="absolute right-2 text-[10px]">/</Kbd>
          )}
        </div>
        {hasSkillDocs && (
          <span className="text-xs text-text-3 ml-auto shrink-0">Generated docs</span>
        )}
      </div>

      <div className="flex flex-1 min-h-0">
        {/* Left pane: document index */}
        {treeLoading ? (
          <div className="w-[320px] shrink-0 border-r border-border flex items-center justify-center">
            <span className="text-sm text-text-4">Loading…</span>
          </div>
        ) : (
          <DocsTree
            tree={activeNodes}
            repoDocs={activeRepoDocs}
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
    </div>
  );
}

// Slim header strip with a back-to-chooser control (used by tier empty states).
function TierHeader({ tier, onBack }: { tier: DocTier; onBack: () => void }) {
  return (
    <div className="flex items-center gap-3 h-10 shrink-0 px-4 border-b border-border bg-bg">
      <button
        onClick={onBack}
        className="flex items-center gap-1 text-xs text-text-3 hover:text-text shrink-0 -ml-1"
        aria-label="Back to documentation chooser"
      >
        <ChevronLeft size={14} />
        {tier === "business" ? "Business" : "Technical"}
      </button>
    </div>
  );
}
