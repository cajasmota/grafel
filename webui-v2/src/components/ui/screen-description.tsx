import type { ReactNode } from "react";
import { Tooltip, TooltipTrigger, TooltipContent } from "./tooltip";

export interface ScreenDescriptionTerm {
  /** The word/phrase to render as a hover-definable term. */
  term: string;
  /** The definition revealed on hover. */
  def: ReactNode;
}

export interface ScreenDescriptionProps {
  /** The 1-2 sentence plain-language description of the screen/tab. */
  children: ReactNode;
  /**
   * Optional inline glossary. Any occurrence of `term` inside the text
   * children is *not* auto-replaced; instead these are exposed via the
   * {@link DefTerm} helper for callers that want hover-definitions inline.
   * Rendered as a trailing legend so the terms are discoverable.
   */
  terms?: ScreenDescriptionTerm[];
}

/**
 * A compact plain-language description block for a screen or tab
 * (#4574 part 1). Generalizes Quality's TabHeader and Taint's PurposeHeader
 * lead-in: a short, low-chrome paragraph that tells a non-expert what the
 * view shows, with optional inline hover-definitions for jargon.
 */
export function ScreenDescription({ children, terms }: ScreenDescriptionProps) {
  return (
    <div className="-mt-1 mb-1 max-w-3xl space-y-1">
      <p className="text-sm text-text-3 leading-relaxed">{children}</p>
      {terms && terms.length > 0 && (
        <p className="text-xs text-text-4 leading-relaxed">
          {terms.map((t, i) => (
            <span key={t.term}>
              {i > 0 && <span className="mx-1.5">·</span>}
              <DefTerm term={t.term} def={t.def} />
            </span>
          ))}
        </p>
      )}
    </div>
  );
}

/**
 * A hoverable, dotted-underlined term that reveals its definition on hover.
 * Generalizes Taint's DefTerm (dataflow.tsx). Usable standalone inside a
 * {@link ScreenDescription}'s children to define jargon inline.
 */
export function DefTerm({ term, def }: ScreenDescriptionTerm) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-help underline decoration-dotted decoration-text-4 underline-offset-2 text-text-2">
          {term}
        </span>
      </TooltipTrigger>
      <TooltipContent>{def}</TooltipContent>
    </Tooltip>
  );
}
