/* ============================================================
   docs-tree.tsx — Left pane: recursive entity tree.

   - Header: "Documentation index" + total entity count.
   - Repo nodes auto-expanded (depth=0); deeper folders default collapsed.
   - Search: matching branches auto-expand; non-matching nodes hidden.
   - Matches inline-highlighted with <mark>.
   - Uses <button> for keyboard accessibility.

   NOTE: For groups with 10k+ leaf nodes, replace the recursive render
   with @tanstack/react-virtual (deferred per YAGNI — not in package.json).
   ============================================================ */

import { useState, useMemo, useCallback } from "react";
import { ChevronRight } from "lucide-react";
import { TypeGlyph } from "./type-glyph";
import type { DocsTreeNode } from "@/data/types";

// ── helpers ──────────────────────────────────────────────────────────────────

function countLeaves(node: DocsTreeNode): number {
  if (!node.children) return 1;
  return node.children.reduce((sum, c) => sum + countLeaves(c), 0);
}

function hasMatch(node: DocsTreeNode, q: string): boolean {
  if (!q) return false;
  if (node.name.toLowerCase().includes(q)) return true;
  return node.children?.some((c) => hasMatch(c, q)) ?? false;
}

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
  node: DocsTreeNode;
  depth: number;
  selectedId: string | null;
  onSelect: (id: string) => void;
  query: string;
  openMap: Record<string, boolean>;
  onToggle: (key: string) => void;
}

function TreeNode({ node, depth, selectedId, onSelect, query, openMap, onToggle }: TreeNodeProps) {
  const isLeaf = !node.children;
  const nodeKey = `${node.name}:${depth}`;
  const lowerQ = query.toLowerCase();

  const selfMatches = query ? node.name.toLowerCase().includes(lowerQ) : false;
  const childMatches = !isLeaf && (node.children?.some((c) => hasMatch(c, lowerQ)) ?? false);

  // Hide nodes with no matching descendants when searching.
  if (query && !selfMatches && !childMatches) return null;

  const paddingLeft = 12 + depth * 14;

  if (isLeaf) {
    const isActive = node.id === selectedId;
    return (
      <button
        className={[
          "flex items-center gap-1.5 w-full text-left px-2 py-1 rounded-sm text-sm font-mono transition-colors",
          isActive
            ? "bg-[var(--accent-soft)] text-[var(--accent)]"
            : "text-text-2 hover:bg-surface-2",
        ].join(" ")}
        style={{ paddingLeft }}
        onClick={() => node.id && onSelect(node.id)}
        title={node.name}
      >
        <TypeGlyph type={node.type} />
        <span className="truncate leading-none">
          <HighlightMatch text={node.name} query={query} />
        </span>
      </button>
    );
  }

  // Folder / repo node.
  // Repo nodes (depth=0) default open; deeper folders default closed.
  const defaultOpen = depth === 0;
  const isOpen = query ? true : (openMap[nodeKey] ?? defaultOpen);
  const totalLeaves = countLeaves(node);

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
          className={[
            "text-text-4 shrink-0 transition-transform",
            isOpen ? "rotate-90" : "",
          ].join(" ")}
        />
        <span
          className={[
            "truncate font-mono leading-none",
            node.type === "repo" ? "font-semibold text-text" : "text-text-2",
          ].join(" ")}
        >
          <HighlightMatch text={node.name} query={query} />
        </span>
        {node.type !== "repo" && isOpen && (
          <span className="ml-auto text-xs text-text-4 tabular-nums shrink-0">
            {totalLeaves}
          </span>
        )}
      </button>
      {isOpen &&
        node.children?.map((child, i) => (
          <TreeNode
            key={(child.id ?? child.name) + "-" + i}
            node={child}
            depth={depth + 1}
            selectedId={selectedId}
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
  tree: DocsTreeNode[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  query: string;
}

export function DocsTree({ tree, selectedId, onSelect, query }: DocsTreeProps) {
  const [openMap, setOpenMap] = useState<Record<string, boolean>>({});

  const handleToggle = useCallback((key: string) => {
    // Default for depth-0 keys (repo) is open; deeper folders are closed.
    const defaultVal = key.endsWith(":0");
    setOpenMap((prev) => ({ ...prev, [key]: !(prev[key] ?? defaultVal) }));
  }, []);

  const totalEntities = useMemo(
    () => tree.reduce((sum, r) => sum + countLeaves(r), 0),
    [tree],
  );

  const lowerQ = query.toLowerCase();
  const noMatches = !!query && tree.every((r) => !hasMatch(r, lowerQ));

  return (
    <div className="flex flex-col h-full w-[320px] shrink-0 border-r border-border overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border shrink-0">
        <span className="text-sm font-medium text-text">Documentation index</span>
        <span className="text-xs font-mono text-text-3 tabular-nums">
          {totalEntities.toLocaleString()}
        </span>
      </div>

      {/* Tree list */}
      <div className="flex-1 overflow-y-auto py-1 px-1">
        {noMatches ? (
          <p className="px-3 py-4 text-sm text-text-3 text-center">
            No entities match &ldquo;{query}&rdquo;
          </p>
        ) : (
          tree.map((repo, i) => (
            <TreeNode
              key={repo.name + "-" + i}
              node={repo}
              depth={0}
              selectedId={selectedId}
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
