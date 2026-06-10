/* ============================================================
   insight-button.tsx — breadcrumb "Insights" button + popover (#4655).

   Lives in the TopBar breadcrumb bar. A compact button (lightbulb icon +
   "Insights" label) that opens a popover rendering the active screen's
   InsightBanner two-column content (green human / yellow agent).

   The active insight is registered by the current page/tab via useSetInsight
   (see ui/insight-context). When that insight CHANGES (navigation or tab
   switch) the button pulses with a glowing ring for ~4s so users notice fresh
   context is available. When no insight is registered the button is hidden.
   ============================================================ */

import { useEffect, useRef, useState } from "react";
import { Lightbulb } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger, InsightBanner } from "@/components/ui";
import { useInsight } from "@/components/ui/insight-context";
import { cn } from "@/lib/utils";

/** How long the glow ring stays on after the insight changes. */
const GLOW_MS = 4000;

export function InsightButton() {
  const { insight, glowNonce } = useInsight();
  const [glowing, setGlowing] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Pulse the ring whenever the active insight changes (nonce bump). Skip the
  // initial render (nonce 0) so a fresh load doesn't glow with no interaction.
  useEffect(() => {
    if (glowNonce === 0) return;
    setGlowing(true);
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setGlowing(false), GLOW_MS);
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, [glowNonce]);

  // No insight registered for this screen → nothing to show.
  if (!insight) return null;

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label="Show screen insights"
          className={cn(
            "relative inline-flex items-center gap-1.5 h-7 pl-2 pr-2.5 rounded-md border text-xs font-medium",
            "border-border bg-surface text-text-2 transition-colors hover:bg-surface-2 hover:text-text",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
            glowing && "ag-insight-glow border-warning/60 text-text",
          )}
        >
          <Lightbulb size={13} className={cn("shrink-0", glowing ? "text-warning" : "text-text-4")} />
          <span>Insights</span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[34rem] max-w-[90vw] p-0">
        <div className="p-2">
          <InsightBanner
            human={insight.human}
            agent={insight.agent}
            storageKey={insight.storageKey}
          />
        </div>
      </PopoverContent>
    </Popover>
  );
}
