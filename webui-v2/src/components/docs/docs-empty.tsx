/* ============================================================
   docs-empty.tsx — Docs screen empty states (#1552, #1584).

   Two cases:

   • DocsNotGenerated — the generate-docs SKILL has not run for this group.
     Shows a copyable prompt the user pastes into their coding agent.

   • DocsPickDocument — documents exist but none is selected yet.
   ============================================================ */

import { useState, useCallback } from "react";
import { BookOpen, Sparkles, Copy, Check } from "lucide-react";

interface DocsNotGeneratedProps {
  /** The current archigraph group slug — used in the description. */
  groupId: string;
}

// Whole-screen state: no skill-generated documents for this group.
export function DocsNotGenerated({ groupId }: DocsNotGeneratedProps) {
  const [copied, setCopied] = useState(false);

  // The prompt the user pastes into their coding agent (Claude Code, Cursor, etc.)
  const prompt = `/generate-docs`;

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(prompt);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard API unavailable — select the text instead
      const el = document.getElementById("docs-skill-prompt");
      if (el) {
        const range = document.createRange();
        range.selectNodeContents(el);
        const sel = window.getSelection();
        sel?.removeAllRanges();
        sel?.addRange(range);
      }
    }
  }, [prompt]);

  return (
    <div className="flex flex-1 items-center justify-center px-6">
      <div className="flex flex-col items-center text-center max-w-lg gap-5">
        <span className="text-text-4" aria-hidden="true">
          <Sparkles size={36} strokeWidth={1.25} />
        </span>
        <div className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-text">No generated docs yet</h2>
          <p className="text-sm text-text-3 leading-relaxed">
            Documentation for the{" "}
            <span className="font-mono text-text-2">{groupId}</span> group is produced
            by your coding agent running the{" "}
            <span className="font-mono text-text-2">generate-docs</span> skill. There
            is no CLI command — ask your agent directly.
          </p>
        </div>

        {/* Copy-paste CTA */}
        <div className="w-full rounded-lg border border-border bg-surface p-4 flex flex-col gap-3">
          <p className="text-xs font-medium text-text-2 text-left">
            Paste this into your coding agent (Claude Code, Cursor, etc.):
          </p>
          <div className="flex items-center gap-2 rounded-md bg-surface-2 border border-border px-3 py-2">
            <code
              id="docs-skill-prompt"
              className="flex-1 text-sm font-mono text-[var(--accent)] select-all"
            >
              {prompt}
            </code>
            <button
              onClick={handleCopy}
              aria-label={copied ? "Copied" : "Copy prompt"}
              className="shrink-0 p-1 rounded text-text-4 hover:text-text-2 hover:bg-surface transition-colors"
            >
              {copied ? (
                <Check size={14} className="text-green-500" />
              ) : (
                <Copy size={14} />
              )}
            </button>
          </div>
          <p className="text-xs text-text-4 text-left leading-relaxed">
            The skill walks the knowledge graph and writes navigable markdown
            (overview, per-module deep-dives, reference, patterns) into each
            repo&rsquo;s <span className="font-mono">docs/</span> folder. Once
            complete, those documents appear here grouped by repo and category.
          </p>
        </div>
      </div>
    </div>
  );
}

// Right-pane state: documents exist, but the user hasn't opened one.
export function DocsPickDocument() {
  return (
    <div className="flex flex-col items-center justify-center h-full gap-3 px-6 text-center">
      <span className="text-text-4" aria-hidden="true">
        <BookOpen size={32} strokeWidth={1.25} />
      </span>
      <h2 className="text-base font-medium text-text">Pick a document</h2>
      <p className="text-sm text-text-3 max-w-xs">
        Choose a document from the index on the left, or search by name above.
        Each page renders the markdown your coding agent generated.
      </p>
    </div>
  );
}
