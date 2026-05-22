/* ============================================================
   docs-tree.tsx — Left pane: tree of GENERATED markdown documents.

   The tree is per-repo → category (Overview / Modules / Reference /
   Patterns / Guides) → document leaf. Leaves carry a `path` key used to
   fetch + render the markdown. NOT an entity browser (#1552).

   - Header: "Documentation" + total document count.
   - Repo + category folders auto-expanded; collapsible.
   - Search filters by document name; matching branches auto-expand.
   ============================================================ */

import { useState, useMemo, useCallback } from "react";
import { ChevronRight, FileText, BookOpen, Boxes, ListTree, Shapes, Folder } from "lucide-react";
import type { DocNode, DocCategory } from "@/data/types";

// ── helpers ──────────────────────────────────────────────────────────────────

function countDocs(node: DocNode): number {
  if (node.type === "doc") return 1;
  return (node.children ?? []).reduce((s, c) => s + countDocs(c), 0);
}

function hasMatch(node: DocNode, q: string): boolean {
  if (!q) return false;
  if (node.name.toLowerCase().includes(q)) return true;
  return node.children?.some((c) => hasMatch(c, q)) ?? false;
}

const CATEGORY_ICON: Record<DocCategory, typeof BookOpen> = {
  overview: BookOpen,
  guide: FileText,
  modules: Boxes,
  reference: ListTree,
  patterns: Shapes,
};

function HighlightMatch({ text, query }: { text: string; query: string }) {
  if (!query) return <>{text}</>;
  const idx = text.toLowerCase().indexOf(query);
  if (idx < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-[var(--accent-soft)] text-[var(--accent)] rounded-sm not-italic">
        {text.slice(idx, idx + query.length)}
      </mark>
      {text.slice(idx + query.length)}
    </>
  );
}

// ── TreeNode ─────────────────────────────────────────────────────────────────

interface TreeNodeProps {
  node: DocNode;
  depth: number;
  selectedPath: string | null;
  onSelect: (path: string) => void;
  query: string;
  openMap: Record<string, boolean>;
  onToggle: (key: string) => void;
}

function TreeNode({ node, depth, selectedPath, onSelect, query, openMap, onToggle }: TreeNodeProps) {
  const lowerQ = query.toLowerCase();
  const isDoc = node.type === "doc";
  const nodeKey = `${node.name}:${depth}`;

  const selfMatches = query ? node.name.toLowerCase().includes(lowerQ) : false;
  const childMatches = !isDoc && (node.children?.some((c) => hasMatch(c, lowerQ)) ?? false);
  if (query && !selfMatches && !childMatches) return null;

  const paddingLeft = 12 + depth * 14;

  if (isDoc) {
    const isActive = node.path === selectedPath;
    return (
      <button
        className={[
          "flex items-center gap-1.5 w-full text-left px-2 py-1 rounded-sm text-sm transition-colors",
          isActive
            ? "bg-[var(--accent-soft)] text-[var(--accent)]"
            : "text-text-2 hover:bg-surface-2",
        ].join(" ")}
        style={{ paddingLeft }}
        onClick={() => node.path && onSelect(node.path)}
        title={node.path}
      >
        <FileText size={13} className="shrink-0 text-text-4" />
        <span className="truncate leading-none">
          <HighlightMatch text={node.name} query={query} />
        </span>
      </button>
    );
  }

  // Folder (repo or category). Auto-open the top two levels by default.
  const defaultOpen = depth <= 1;
  const isOpen = query ? true : (openMap[nodeKey] ?? defaultOpen);
  const total = countDocs(node);
  const Icon = node.category ? CATEGORY_ICON[node.category] : Folder;

  return (
    <div>
      <button
        className="flex items-center gap-1 w-full text-left px-2 py-1 rounded-sm text-sm hover:bg-surface-2 transition-colors"
        style={{ paddingLeft }}
        onClick={() => onToggle(nodeKey)}
        aria-expanded={isOpen}
      >
        <ChevronRight
          size={11}
          className={["text-text-4 shrink-0 transition-transform", isOpen ? "rotate-90" : ""].join(" ")}
        />
        {depth === 0 ? (
          <Folder size={13} className="shrink-0 text-text-4" />
        ) : (
          <Icon size={13} className="shrink-0 text-text-4" />
        )}
        <span
          className={[
            "truncate leading-none",
            depth === 0 ? "font-semibold text-text font-mono" : "text-text-2",
          ].join(" ")}
        >
          <HighlightMatch text={node.name} query={query} />
        </span>
        <span className="ml-auto text-xs text-text-4 tabular-nums shrink-0">{total}</span>
      </button>
      {isOpen &&
        node.children?.map((child, i) => (
          <TreeNode
            key={(child.path ?? child.name) + "-" + i}
            node={child}
            depth={depth + 1}
            selectedPath={selectedPath}
            onSelect={onSelect}
            query={query}
            openMap={openMap}
            onToggle={onToggle}
          />
        ))}
    </div>
  );
}

// ── DocsTree ─────────────────────────────────────────────────────────────────

export interface DocsTreeProps {
  tree: DocNode[];
  selectedPath: string | null;
  onSelect: (path: string) => void;
  query: string;
}

export function DocsTree({ tree, selectedPath, onSelect, query }: DocsTreeProps) {
  const [openMap, setOpenMap] = useState<Record<string, boolean>>({});

  const handleToggle = useCallback((key: string) => {
    // depth 0 (repo) and 1 (category) default open.
    const depthSuffix = key.slice(key.lastIndexOf(":") + 1);
    const defaultVal = depthSuffix === "0" || depthSuffix === "1";
    setOpenMap((prev) => ({ ...prev, [key]: !(prev[key] ?? defaultVal) }));
  }, []);

  const totalDocs = useMemo(() => tree.reduce((s, r) => s + countDocs(r), 0), [tree]);

  const lowerQ = query.toLowerCase();
  const noMatches = !!query && tree.every((r) => !hasMatch(r, lowerQ));

  return (
    <div className="flex flex-col h-full w-[320px] shrink-0 border-r border-border overflow-hidden">
      <div className="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
        <span className="text-sm font-medium text-text">Documentation</span>
        <span className="text-xs font-mono text-text-3 tabular-nums">
          {totalDocs.toLocaleString()}
        </span>
      </div>

      <div className="flex-1 overflow-y-auto py-1 px-1">
        {noMatches ? (
          <p className="px-3 py-4 text-sm text-text-3 text-center">
            No documents match &ldquo;{query}&rdquo;
          </p>
        ) : (
          tree.map((repo, i) => (
            <TreeNode
              key={repo.name + "-" + i}
              node={repo}
              depth={0}
              selectedPath={selectedPath}
              onSelect={onSelect}
              query={query}
              openMap={openMap}
              onToggle={handleToggle}
            />
          ))
        )}
      </div>
    </div>
  );
}
