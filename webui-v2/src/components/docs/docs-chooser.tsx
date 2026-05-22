/* ============================================================
   docs-chooser.tsx — Docs entry chooser: Technical vs Business (#1622).

   At the Docs entry the user picks one of two documentation tiers:

   • Technical — the per-repo engineer-facing doc tree (overview / modules /
     reference / patterns / guides + cross-cutting). Works as today.
   • Business — a SEPARATE, non-per-repo product/business view (capabilities,
     domain / glossary, user journeys, business rules). Rendered when present;
     otherwise a clear onboarding empty state (docs skill, business tier).

   This file also exports the Business empty state shown when no business docs
   have been generated yet.
   ============================================================ */

import { Code2, Briefcase, Sparkles, ChevronRight } from "lucide-react";

export type DocTier = "technical" | "business";

interface DocsChooserProps {
  onPick: (tier: DocTier) => void;
  /** Whether any business documents are present (controls the card subtitle). */
  hasBusiness: boolean;
}

// Entry screen: two cards. Engineer-facing vs product/business.
export function DocsChooser({ onPick, hasBusiness }: DocsChooserProps) {
  return (
    <div className="flex flex-1 items-center justify-center px-6">
      <div className="flex flex-col items-center text-center max-w-2xl gap-6">
        <div className="flex flex-col gap-1.5">
          <h2 className="text-lg font-medium text-text">Documentation</h2>
          <p className="text-sm text-text-3">
            Choose the documentation you want to read.
          </p>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 w-full">
          <ChooserCard
            icon={<Code2 size={26} strokeWidth={1.5} />}
            title="Technical documentation"
            subtitle="Engineer-facing"
            body="Per-repo deep-dives: overview, modules, reference, patterns, and cross-cutting guides (architecture, cross-repo flows)."
            onClick={() => onPick("technical")}
            cta="Browse technical docs"
          />
          <ChooserCard
            icon={<Briefcase size={26} strokeWidth={1.5} />}
            title="Business documentation"
            subtitle={hasBusiness ? "Product & domain" : "Not generated yet"}
            body="Product capabilities, domain glossary, user journeys, and business rules — a non-technical view of what the system does."
            onClick={() => onPick("business")}
            cta={hasBusiness ? "Browse business docs" : "Learn more"}
            muted={!hasBusiness}
          />
        </div>
      </div>
    </div>
  );
}

interface ChooserCardProps {
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  body: string;
  cta: string;
  onClick: () => void;
  muted?: boolean;
}

function ChooserCard({ icon, title, subtitle, body, cta, onClick, muted }: ChooserCardProps) {
  return (
    <button
      onClick={onClick}
      className={[
        "group flex flex-col items-start gap-3 text-left rounded-xl border bg-surface p-5 transition-colors",
        "border-border hover:border-[var(--accent)] hover:bg-surface-2 focus:outline-none focus:ring-1 focus:ring-[var(--accent)]",
      ].join(" ")}
    >
      <span
        className={[
          "rounded-lg p-2.5 transition-colors",
          muted ? "text-text-4 bg-surface-2" : "text-[var(--accent)] bg-[var(--accent-soft)]",
        ].join(" ")}
      >
        {icon}
      </span>
      <div className="flex flex-col gap-1">
        <span className="text-[0.7rem] uppercase tracking-wide text-text-4">{subtitle}</span>
        <h3 className="text-base font-semibold text-text">{title}</h3>
      </div>
      <p className="text-sm text-text-3 leading-relaxed">{body}</p>
      <span className="mt-1 inline-flex items-center gap-1 text-sm font-medium text-[var(--accent)] group-hover:gap-1.5 transition-all">
        {cta}
        <ChevronRight size={14} />
      </span>
    </button>
  );
}

// Whole-pane state: Business tier selected but no business docs exist yet.
export function BusinessNotGenerated() {
  return (
    <div className="flex flex-1 items-center justify-center px-6">
      <div className="flex flex-col items-center text-center max-w-lg gap-5">
        <span className="text-text-4" aria-hidden="true">
          <Briefcase size={36} strokeWidth={1.25} />
        </span>
        <div className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-text">No business documentation yet</h2>
          <p className="text-sm text-text-3 leading-relaxed">
            Business documentation — product capabilities, a domain glossary,
            user journeys, and business rules — is produced by the{" "}
            <span className="font-mono text-text-2">generate-docs</span> skill on
            the <span className="font-medium text-text-2">business</span> tier. It
            describes what the system does in product terms, separate from the
            engineer-facing technical docs.
          </p>
          <p className="text-sm text-text-3 leading-relaxed">
            None has been generated for this group yet. Run the docs skill
            (business tier) with your coding agent to populate this view.
          </p>
        </div>
        <div className="w-full rounded-lg border border-border bg-surface p-4 flex items-center gap-2">
          <Sparkles size={16} className="shrink-0 text-text-4" />
          <code className="text-sm font-mono text-[var(--accent)] select-all">
            /generate-docs business
          </code>
        </div>
      </div>
    </div>
  );
}
