/* ============================================================
   SourcePeek.tsx — shared "source peek" modal (#4499).

   Clicking any file:line ref in the dashboard (Taint paths, Flow cards,
   Paths "defined-in", Security findings, IaC, DI/quality links) opens this
   CENTERED modal showing the actual source for that line — the get_source
   equivalent for the UI.

   - Fetches GET /api/v2/groups/:id/source (a window around the line, or the
     whole file when small) via useSource.
   - Syntax-highlights with react-syntax-highlighter's PrismAsyncLight build:
     grammars are lazy-loaded per language, so only the languages actually
     viewed are pulled in (keeps the initial bundle lean).
   - Centers/scrolls to the target line and highlights it.
   - Shows the file path + repo, a copy-path affordance, and loading / error /
     empty states.

   Wiring is app-wide via <SourcePeekProvider> + useSourcePeek(): any ref
   renderer calls openSourcePeek({ groupId, file, line, repo }) and the single
   mounted modal handles the rest.
   ============================================================ */

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { PrismAsyncLight as SyntaxHighlighter } from "react-syntax-highlighter";
import { Check, Copy, FileCode2, Loader2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { getRepoColor } from "@/lib/repo-color";
import { useSource } from "@/hooks/use-source";
import { sourcePeekPrismTheme } from "./syntax-theme";

/** The ref a caller asks to peek at. */
export interface SourcePeekTarget {
  groupId: string;
  file: string;
  line: number;
  /** Optional repo slug to pin resolution when the same path exists twice. */
  repo?: string;
}

interface SourcePeekProps extends SourcePeekTarget {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * basename — last path segment, for the modal title (full path shown below).
 */
function basename(p: string): string {
  const i = p.lastIndexOf("/");
  return i < 0 ? p : p.slice(i + 1);
}

export function SourcePeek({
  groupId,
  file,
  line,
  repo,
  open,
  onOpenChange,
}: SourcePeekProps) {
  // Only fetch while the modal is open (lazy).
  const { data, isLoading, isError, error } = useSource(
    groupId,
    open ? file : null,
    line,
    repo,
    open,
  );

  const [copied, setCopied] = useState(false);
  const targetRef = useRef<HTMLDivElement | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Center the highlighted line once the content paints.
  useLayoutEffect(() => {
    if (!open || !data) return;
    // Defer to next frame so the highlighter has rendered the rows.
    const id = requestAnimationFrame(() => {
      const el = targetRef.current;
      const scroller = scrollRef.current;
      if (el && scroller) {
        const elTop = el.offsetTop;
        const center = elTop - scroller.clientHeight / 2 + el.clientHeight / 2;
        scroller.scrollTop = Math.max(0, center);
      }
    });
    return () => cancelAnimationFrame(id);
  }, [open, data]);

  // Reset the copied affordance when the target changes.
  useEffect(() => {
    setCopied(false);
  }, [file, line, open]);

  const fileLabel = `${file}${line > 0 ? `:${line}` : ""}`;
  const repoColors = repo ? getRepoColor(repo) : null;

  const code = useMemo(() => {
    if (!data) return "";
    return data.lines.map((l) => l.text).join("\n");
  }, [data]);

  const startLine = data?.start_line ?? 1;

  const copyPath = () => {
    void navigator.clipboard
      ?.writeText(fileLabel)
      .then(() => {
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1500);
      })
      .catch(() => {
        /* clipboard unavailable — no-op */
      });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="p-0 flex flex-col overflow-hidden max-w-[min(1000px,94vw)] w-full h-[80vh]"
      >
        {/* Header — file basename + path + repo chip + copy affordance. */}
        <div className="px-5 pt-4 pb-3 border-b border-border shrink-0">
          <DialogTitle className="flex items-center gap-2 min-w-0">
            <FileCode2 size={16} className="text-text-3 shrink-0" />
            <span className="truncate font-mono text-md">{basename(file)}</span>
            {data?.repo && repoColors && (
              <span
                className="shrink-0 inline-flex items-center h-[18px] px-1.5 rounded text-[10px] font-semibold font-mono leading-none select-none"
                style={{
                  background: repoColors.background,
                  color: repoColors.foreground,
                  border: `1px solid ${repoColors.border}`,
                }}
                title={data.repo}
              >
                {data.repo}
              </span>
            )}
          </DialogTitle>
          <DialogDescription className="flex items-center gap-2 min-w-0">
            <span className="truncate font-mono text-[11px]" title={fileLabel}>
              {fileLabel}
            </span>
            <button
              type="button"
              onClick={copyPath}
              title="Copy file:line"
              className="shrink-0 inline-flex items-center gap-1 text-text-3 hover:text-text rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]"
            >
              {copied ? <Check size={12} /> : <Copy size={12} />}
              <span className="sr-only">Copy path</span>
            </button>
            {data?.truncated && (
              <span className="shrink-0 text-[10px] text-text-4">
                lines {data.start_line}–{data.end_line} of {data.total_lines}
              </span>
            )}
          </DialogDescription>
        </div>

        {/* Body — loading / error / empty / code. */}
        <div ref={scrollRef} className="flex-1 min-h-0 overflow-auto ag-scroll bg-surface-2">
          {isLoading && (
            <div className="flex items-center justify-center h-full text-text-3 gap-2 text-sm">
              <Loader2 size={16} className="animate-spin" />
              Loading source…
            </div>
          )}

          {isError && !isLoading && (
            <div className="flex flex-col items-center justify-center h-full text-text-3 gap-1 text-sm px-6 text-center">
              <span className="text-text">Could not load source</span>
              <span className="text-[11px] text-text-4">
                {(error as Error)?.message ??
                  "The file may not exist in the indexed working tree."}
              </span>
            </div>
          )}

          {!isLoading && !isError && data && data.lines.length === 0 && (
            <div className="flex items-center justify-center h-full text-text-4 text-sm">
              Empty file.
            </div>
          )}

          {!isLoading && !isError && data && data.lines.length > 0 && (
            <SyntaxHighlighter
              language={data.language || "text"}
              style={sourcePeekPrismTheme}
              showLineNumbers
              startingLineNumber={startLine}
              wrapLines
              lineNumberStyle={{
                minWidth: "3.25em",
                paddingRight: "1em",
                textAlign: "right",
                color: "var(--text-4, #6c7086)",
                userSelect: "none",
              }}
              customStyle={{
                margin: 0,
                padding: "0.75rem 0",
                background: "transparent",
                fontSize: "12px",
              }}
              lineProps={(lineNumber: number) => {
                const isTarget = line > 0 && lineNumber === line;
                return {
                  ref: isTarget
                    ? (el: HTMLDivElement | null) => {
                        targetRef.current = el;
                      }
                    : undefined,
                  className: cn(
                    "block w-full",
                    isTarget && "bg-[var(--accent-soft,rgba(137,180,250,0.18))]",
                  ),
                  style: isTarget
                    ? {
                        display: "block",
                        boxShadow:
                          "inset 2px 0 0 var(--accent, #89b4fa)",
                      }
                    : { display: "block" },
                };
              }}
            >
              {code}
            </SyntaxHighlighter>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
