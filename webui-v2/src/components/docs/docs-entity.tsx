/* ============================================================
   docs-entity.tsx — Right-pane entity article.

   Sections (top to bottom):
   1. Head — TypeBadge · name (mono, 26px) · meta row (repo chip + file) ·
             ghost action buttons (Open in editor · View in Graph)
   2. Signature — code block (mono, 12.5px, line-height 1.6, scroll-x on overflow)
   3. Description — paragraph (max-width 64ch), AI-generated chip if applicable
   4. Parameters — 3-column grid (name / type / description)
   5. Returns — single row: type (mono) + description
   6. Response shapes (http_endpoint only) — status code + JSON shape rows
   7. Called by / Calls — collapsible chip lists (capped at 50 visible)
   8. EntityStub — when stub=true, replaces body sections
   ============================================================ */

import React, { useState } from "react";
import { ChevronRight, Code2, ExternalLink, Sparkles, Info } from "lucide-react";
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from "@/components/ui";
import { TypeBadge } from "./type-glyph";
import type { DocsEntityDetail } from "@/data/types";

const AI_TOOLTIP_TEXT = (
  <>
    <strong>AI-generated</strong> — archigraph synthesized this description from
    the source code because no docstring was present. Review for accuracy before
    relying on it.
  </>
);

// ── SectionLabel ─────────────────────────────────────────────────────────────

function SectionLabel({
  children,
  collapsible,
  open,
  onToggle,
}: {
  children: React.ReactNode;
  collapsible?: boolean;
  open?: boolean;
  onToggle?: () => void;
}) {
  const inner = (
    <span className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-text-3">
      {collapsible && (
        <ChevronRight
          size={11}
          className={["transition-transform", open ? "rotate-90" : ""].join(" ")}
        />
      )}
      {children}
    </span>
  );
  if (collapsible) {
    return (
      <button className="flex items-center mb-3" onClick={onToggle}>
        {inner}
      </button>
    );
  }
  return <div className="mb-3">{inner}</div>;
}

// ── RefList ──────────────────────────────────────────────────────────────────

function RefList({
  label,
  hint,
  names,
  empty,
}: {
  label: string;
  hint: React.ReactNode;
  names: string[];
  empty: string;
}) {
  const [open, setOpen] = useState(true);
  // Cap visible to 50 per spec; show overflow count.
  const visible = names.slice(0, 50);
  const overflow = names.length - visible.length;

  return (
    <section className="border-t border-border pt-5 mt-1">
      <SectionLabel collapsible open={open} onToggle={() => setOpen((v) => !v)}>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="inline-flex items-center gap-1 cursor-help">
              {label} &middot; {names.length}
              <Info size={10} className="text-text-4" tabIndex={0} aria-label="More info" />
            </span>
          </TooltipTrigger>
          <TooltipContent>{hint}</TooltipContent>
        </Tooltip>
      </SectionLabel>
      {open &&
        (visible.length === 0 ? (
          <p className="text-sm text-text-3">{empty}</p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {visible.map((n) => (
              // Render as plain text when the entity may not be cross-indexed.
              // Future: resolve to link when backend confirms entity exists.
              <span
                key={n}
                className="font-mono text-xs px-2 py-1 rounded-md bg-surface border border-border text-text-2"
              >
                {n}
              </span>
            ))}
            {overflow > 0 && (
              <span className="text-xs text-text-3 self-center">
                +{overflow} more
              </span>
            )}
          </div>
        ))}
    </section>
  );
}

// ── EntityHead ───────────────────────────────────────────────────────────────

function EntityHead({ entity }: { entity: DocsEntityDetail }) {
  return (
    <header className="mb-8">
      <div className="flex items-center gap-2 mb-2">
        <TypeBadge type={entity.type} />
        <h1 className="font-mono text-[26px] font-semibold text-text leading-none">
          {entity.name}
        </h1>
      </div>
      <div className="flex flex-wrap items-center gap-2 mb-4">
        <span className="font-mono text-xs font-semibold px-2 py-0.5 rounded-full bg-surface border border-border text-text-2">
          {entity.repo}
        </span>
        <span className="font-mono text-xs text-text-3">
          {entity.file}
          {entity.line ? `:${entity.line}` : ""}
        </span>
      </div>
      <div className="flex gap-2">
        <button className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-border bg-surface text-xs text-text-2 hover:bg-surface-2 transition-colors">
          <Code2 size={11} />
          Open in editor
        </button>
        <button className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md border border-border bg-surface text-xs text-text-2 hover:bg-surface-2 transition-colors">
          <ExternalLink size={11} />
          View in Graph
        </button>
      </div>
    </header>
  );
}

// ── EntityStub ───────────────────────────────────────────────────────────────

function EntityStub({ entity }: { entity: DocsEntityDetail }) {
  return (
    <article className="max-w-[760px] mx-auto px-8 py-8">
      <EntityHead entity={entity} />
      <div className="mt-8 flex items-start gap-2 rounded-lg border border-border bg-surface p-4 text-sm text-text-2">
        <Info size={14} className="shrink-0 mt-0.5 text-text-3" />
        <span>
          This entity has no generated documentation yet. Run{" "}
          <code className="font-mono text-xs bg-surface-2 px-1 py-0.5 rounded">
            archigraph generate-docs
          </code>{" "}
          on the group to populate.
        </span>
      </div>
    </article>
  );
}

// ── DocsEntity ────────────────────────────────────────────────────────────────

export interface DocsEntityProps {
  entity: DocsEntityDetail;
}

export function DocsEntity({ entity }: DocsEntityProps) {
  if (entity.stub) {
    return (
      <TooltipProvider>
        <EntityStub entity={entity} />
      </TooltipProvider>
    );
  }

  return (
    <TooltipProvider>
      <article className="max-w-[760px] mx-auto px-8 py-8">
        <EntityHead entity={entity} />

        {/* Signature */}
        {entity.signature && (
          <section className="mb-8">
            <SectionLabel>Signature</SectionLabel>
            <pre className="overflow-x-auto rounded-md bg-surface border border-border px-4 py-3 text-[12.5px] leading-[1.6] font-mono text-text-2">
              <code>{entity.signature}</code>
            </pre>
          </section>
        )}

        {/* Description */}
        {entity.description && (
          <section className="mb-8">
            <SectionLabel>
              <span className="flex items-center gap-2">
                Description
                {entity.aiGenerated && (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span
                        className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full text-xs cursor-help"
                        style={{
                          background: "color-mix(in srgb, var(--pastel-2) 16%, transparent)",
                          color: "var(--pastel-2-ink)",
                        }}
                        tabIndex={0}
                      >
                        <Sparkles size={9} />
                        AI-generated
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>{AI_TOOLTIP_TEXT}</TooltipContent>
                  </Tooltip>
                )}
              </span>
            </SectionLabel>
            <p className="text-sm text-text-2 leading-relaxed max-w-[64ch]">
              {entity.description}
            </p>
          </section>
        )}

        {/* Parameters */}
        {entity.params.length > 0 && (
          <section className="mb-8">
            <SectionLabel>Parameters</SectionLabel>
            <div className="grid grid-cols-[auto_auto_1fr] gap-x-4 gap-y-2 text-sm">
              {entity.params.map((p) => (
                <React.Fragment key={p.name}>
                  <span className="font-mono text-text font-medium whitespace-nowrap">
                    {p.name}
                  </span>
                  <span className="font-mono text-text-3 whitespace-nowrap">
                    {p.type}
                  </span>
                  <span className="text-text-2">
                    {p.desc}
                  </span>
                </React.Fragment>
              ))}
            </div>
          </section>
        )}

        {/* Returns */}
        {entity.returns && (
          <section className="mb-8">
            <SectionLabel>Returns</SectionLabel>
            <div className="flex items-baseline gap-3 text-sm">
              <span className="font-mono text-text">{entity.returns.type}</span>
              {entity.returns.desc && entity.returns.desc !== "—" && (
                <span className="text-text-2">{entity.returns.desc}</span>
              )}
            </div>
          </section>
        )}

        {/* Response shapes — http_endpoint only */}
        {entity.responseShapes && entity.responseShapes.length > 0 && (
          <section className="mb-8">
            <SectionLabel>Response shapes</SectionLabel>
            <div className="flex flex-col gap-2">
              {entity.responseShapes.map((rs) => (
                <div key={rs.status} className="flex items-start gap-3 text-sm">
                  <span
                    className="font-mono font-semibold px-1.5 py-0.5 rounded text-xs shrink-0"
                    style={
                      rs.status < 400
                        ? {
                            background: "color-mix(in srgb, var(--pastel-2) 16%, transparent)",
                            color: "var(--pastel-2-ink)",
                          }
                        : {
                            background: "color-mix(in srgb, var(--pastel-5) 16%, transparent)",
                            color: "var(--pastel-5-ink)",
                          }
                    }
                  >
                    {rs.status}
                  </span>
                  <code className="font-mono text-xs text-text-2">{rs.shape}</code>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* Called by / Calls */}
        <RefList
          label="Called by"
          hint={
            <><strong>Called by</strong> — every entity in the graph that invokes or renders this one. Click to jump to its doc page.</>
          }
          names={entity.callers}
          empty="No incoming edges. This may be a top-level entry point."
        />
        <RefList
          label="Calls"
          hint={
            <><strong>Calls</strong> — entities this one depends on directly. Cross-repo edges are highlighted in the graph view.</>
          }
          names={entity.callees}
          empty="No outgoing edges from this entity."
        />
      </article>
    </TooltipProvider>
  );
}
