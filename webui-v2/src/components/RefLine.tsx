/* ============================================================
   RefLine.tsx — canonical single-line entity reference row.

   Issue #1910: unified format across Defined-in / Called-by /
   Downstream in the endpoint detail pane (and future detail pages).

   Issue #1934: file path is a full clickable link with RTL
   ellipsis on overflow; per-row kind/framework chips removed.

   Issue #1957: layout overhaul:
     - 60/40 column split: path column (60%) | name column (40%)
     - repo chip right-anchored (after name column)
     - center-ellipsis on path overflow: head span LTR-truncates,
       tail span (filename:line) never shrinks, ellipsis in between
     - native `title` attr on row for full-path hover tooltip

   Row layout:
     [path-col 60% center-ellipsis]  [name-col 40%]  [repo chip]

   Props:
     repo   — owning repository slug (shown as a small colored chip)
     file   — source file path (full relative path shown as a link)
     line   — source line number
     name   — entity / caller name (regular weight)
   ============================================================ */

import { cn } from "@/lib/utils";
import { getRepoColor } from "@/lib/repo-color";

export interface RefLineProps {
  repo: string;
  file: string;
  line: number;
  name: string;
  /** Accessibility: full title on hover (defaults to "repo · file:line  name") */
  title?: string;
  className?: string;
  /** Called when the file path link is clicked. Receives "file:line" string. */
  onFileClick?: (fileRef: string) => void;
}

// repoColorIndex is kept for backwards compat with any callsite that still
// uses --pastel-N variables directly, but RefLine now delegates to getRepoColor
// from repo-color.ts for consistent chip rendering across views (#1946).

/**
 * Split a file path into head and tail for center-ellipsis rendering.
 *
 * The tail always contains the filename (last segment) plus the line suffix.
 * The head contains everything before the filename (directory prefix).
 *
 * Example: "src/main/java/com/example/TransfersController.java:48"
 *   tail → "TransfersController.java:48"
 *   head → "src/main/java/com/example/"
 */
function splitPathForEllipsis(
  file: string,
  line: number,
): { head: string; tail: string } {
  const fileLabel = file ? `${file}:${line}` : line > 0 ? `:${line}` : "";
  if (!fileLabel) return { head: "", tail: "" };

  const lastSlash = file.lastIndexOf("/");
  if (lastSlash < 0) {
    return { head: "", tail: fileLabel };
  }

  const head = file.slice(0, lastSlash + 1);
  const filename = file.slice(lastSlash + 1);
  const tail = `${filename}:${line}`;
  return { head, tail };
}

/**
 * RefLine — one-line entity reference used in Defined-in, Called-by, and
 * Downstream sections. Keeps all three sections visually consistent and
 * scannable.
 *
 * Issue #1957 layout:
 *   [path-col 60%] [name-col 40%] [repo chip right]
 *
 * The path column uses the two-span center-ellipsis trick: the head span
 * (directory prefix) truncates on its right with LTR ellipsis; the tail span
 * (filename:line) never shrinks so the important part is always visible.
 *
 * A native `title` attribute on the row gives the full path as a hover tooltip.
 */
export function RefLine({
  repo,
  file,
  line,
  name,
  title,
  className,
  onFileClick,
}: RefLineProps) {
  const repoColors = getRepoColor(repo);
  const fileLabel = file ? `${file}:${line}` : line > 0 ? `:${line}` : "";
  const derivedTitle = title ?? `${repo} · ${file}:${line}  ${name}`;
  const { head, tail } = splitPathForEllipsis(file, line);

  return (
    <div
      className={cn(
        "flex items-center gap-2 py-1 px-4 min-w-0",
        "hover:bg-surface-2 transition-colors duration-75",
        className,
      )}
      title={derivedTitle}
    >
      {/* Path column — 60% width, center-ellipsis via two-span trick.
          Head (directory prefix) truncates LTR; tail (filename:line) shrink-0. */}
      {fileLabel && (
        <button
          type="button"
          onClick={() => onFileClick?.(fileLabel)}
          title={fileLabel}
          className={cn(
            "flex items-center min-w-0 text-left",
            "font-mono text-[11px] tabular-nums",
            "text-accent hover:underline cursor-pointer",
          )}
          style={{ flexBasis: "60%", flexShrink: 1, flexGrow: 0, minWidth: 0 }}
        >
          {head ? (
            <>
              <span className="overflow-hidden whitespace-nowrap text-ellipsis min-w-0 shrink">
                {head}
              </span>
              <span className="shrink-0 whitespace-nowrap">{tail}</span>
            </>
          ) : (
            <span className="overflow-hidden whitespace-nowrap text-ellipsis min-w-0">
              {tail || fileLabel}
            </span>
          )}
        </button>
      )}

      {/* Name column — 40% width, right-truncates on overflow */}
      <span
        className="text-xs text-text truncate font-mono min-w-0"
        title={name}
        style={{ flexBasis: "40%", flexShrink: 1, flexGrow: 0 }}
      >
        {name}
      </span>

      {/* Repo chip — right-anchored via ml-auto, never squished.
          Uses repo-color.ts resolver for theme-aware stable color (#1946). */}
      {repo && (
        <span
          className={cn(
            "shrink-0 inline-flex items-center h-[18px] px-1.5 rounded ml-auto",
            "text-[10px] font-semibold font-mono leading-none select-none",
          )}
          style={{
            background: repoColors.background,
            color: repoColors.foreground,
            border: `1px solid ${repoColors.border}`,
          }}
          title={repo}
        >
          {repo}
        </span>
      )}
    </div>
  );
}
