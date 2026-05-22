/* ============================================================
   skeleton.tsx — Shared loading-placeholder primitive.

   Usage:
     <Skeleton />                       — full-width line, default height
     <Skeleton w="w-48" h="h-4" />     — Tailwind width/height classes
     <Skeleton className="h-20 w-full rounded-md" />

   The shimmer is driven by the ag-skeleton CSS class (app.css) which
   uses --skeleton-base / --skeleton-highlight tokens.  Those tokens are
   defined for light, dark, warm-light, and warm-dark in tokens.css, so
   every palette looks correct automatically.

   Motion: the animation is suppressed via @media (prefers-reduced-motion)
   in app.css — no extra work needed here.
   ============================================================ */

import { cn } from "@/lib/utils";

interface SkeletonProps {
  /** Tailwind width class, e.g. "w-48" or "w-full" (default: "w-full") */
  w?: string;
  /** Tailwind height class, e.g. "h-3" or "h-6" (default: "h-3") */
  h?: string;
  /** Extra Tailwind classes — merged last so they can override w/h */
  className?: string;
}

export function Skeleton({ w = "w-full", h = "h-3", className }: SkeletonProps) {
  return (
    <div
      aria-hidden="true"
      className={cn("rounded ag-skeleton", w, h, className)}
    />
  );
}
