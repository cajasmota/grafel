/**
 * TruncatedPath.tsx — presentational wrapper that truncates a file path or
 * FQN string for display, shows the full value in a Radix tooltip, and copies
 * the full string to the clipboard on click.
 *
 * Used by:
 *   - Defined-in / Called-by / Downstream rows (file paths + FQNs)
 *   - Auth section "Defined in" pointer
 *   - Any other surface that needs path/FQN truncation
 *
 * Props:
 *   value       — the full, un-truncated string
 *   display     — the already-truncated display form
 *   className   — additional class names
 *   mono        — render in font-mono (default: true)
 *   onCopied    — optional callback after a successful copy (e.g. toast)
 */

import { useState } from "react";
import { Copy } from "lucide-react";
import {
  Tooltip,
  TooltipProvider,
  TooltipTrigger,
  TooltipContent,
} from "@/components/ui";
import { cn } from "@/lib/utils";

interface TruncatedPathProps {
  /** The full, un-truncated string shown in tooltip + copied to clipboard. */
  value: string;
  /** The pre-truncated display form. If omitted, `value` is shown as-is. */
  display?: string;
  className?: string;
  /** When true renders font-mono (default true). */
  mono?: boolean;
  /** Called with `value` after a successful clipboard write. */
  onCopied?: (value: string) => void;
}

export function TruncatedPath({
  value,
  display,
  className,
  mono = true,
  onCopied,
}: TruncatedPathProps) {
  const [copied, setCopied] = useState(false);

  const shown = display ?? value;
  const isTruncated = shown !== value;

  async function handleClick(e: React.MouseEvent) {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      onCopied?.(value);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard write is best-effort; swallow permission errors silently.
    }
  }

  const content = (
    <button
      type="button"
      onClick={handleClick}
      data-testid="truncated-path"
      title={isTruncated ? undefined : value}
      className={cn(
        "inline-flex items-center gap-1 group/tp cursor-pointer",
        "text-accent hover:underline focus-visible:outline-none focus-visible:ring-2",
        "focus-visible:ring-[var(--accent-ring)] rounded-sm",
        mono && "font-mono",
        className,
      )}
    >
      <span className="truncate">{shown}</span>
      <Copy
        size={10}
        className={cn(
          "shrink-0 opacity-0 group-hover/tp:opacity-60 transition-opacity",
          copied && "opacity-100 text-success",
        )}
      />
    </button>
  );

  // Only wrap with Tooltip when the text is actually truncated — otherwise the
  // native `title` attribute on the button is sufficient.
  if (!isTruncated) {
    return content;
  }

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>{content}</TooltipTrigger>
        <TooltipContent side="top" className="font-mono text-[11px] max-w-[480px] break-all">
          {value}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
