import type { ReactNode } from "react";
import { Tooltip, TooltipTrigger, TooltipContent } from "./tooltip";

export interface ScreenDescriptionTerm {
  /** The word/phrase to render as a hover-definable term. */
  term: string;
  /** The definition revealed on hover. */
  def: ReactNode;
}

/**
 * A hoverable, dotted-underlined term that reveals its definition on hover.
 * Usable inside an {@link InsightBanner}'s `human` node (or anywhere) to define
 * jargon inline without leaving the sentence.
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
