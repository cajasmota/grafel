/* ============================================================
   routes/pending.tsx — Repair + enrichment inbox (#1442, EPIC #1432).

   Layout (three horizontal strips):
   1. Stat bar (unresolved pill) — 40px
   2. Tab bar (44px): tabs left · filter + groupBy right
   3. Split pane: left list (440px) + right detail (flex-1)

   The AppShell TopBar handles the outer breadcrumb (archigraph › group › Pending).
   ============================================================ */

import { useState, useMemo, useRef, useEffect } from "react";
import { useParams } from "react-router-dom";
import { Wrench, Sparkles, ChevronRight, Copy, ExternalLink } from "lucide-react";
import { toast } from "sonner";

import { useCandidates, useSaveHint } from "@/hooks/use-pending";
import { usePendingStore } from "@/store/use-pending-store";
import {
  Badge,
  Button,
  Tabs,
  TabsList,
  TabsTrigger,
  Skeleton,
  ScreenDescription,
  AgentUsage,
} from "@/components/ui";
import { cn } from "@/lib/utils";

import type {
  RepairCandidate,
  Candidate,
  Severity,
  RepairIssueType,
  EnrichmentType,
} from "@/data/types";

// ---------------------------------------------------------------------------
// Constants / metadata maps
// ---------------------------------------------------------------------------

const SEVERITY_META: Record<
  Severity,
  { tone: "danger" | "warning" | "neutral"; label: string; color: string }
> = {
  critical: { tone: "danger",  label: "Critical", color: "var(--danger)"  },
  warning:  { tone: "warning", label: "Warning",  color: "var(--warning)" },
  info:     { tone: "neutral", label: "Info",      color: "var(--text-4)"  },
};

const ISSUE_LABEL: Record<RepairIssueType, string> = {
  missing_docstring:  "Missing docstring",
  dead_code:          "Likely dead code",
  mismatched_handler: "Duplicate handler",
  untyped_params:     "Untyped signature",
  broken_link:        "Broken cross-repo edge",
  stale_cache:        "Stale cache",
};

const ENRICHMENT_LABEL: Record<EnrichmentType, string> = {
  summary:            "Generate summary",
  param_descriptions: "Generate param docs",
  relationship_tag:   "Suggest relationship",
  tags:               "Suggest tags",
};

// Pastel color map for entity type dots (matches prototype TYPE_DOT).
const TYPE_COLOR: Record<string, string> = {
  function:      "var(--pastel-6, #7ba7bc)",
  component:     "var(--pastel-1, #e88c8c)",
  hook:          "var(--pastel-2, #d4a96a)",
  class:         "var(--pastel-3, #8cc98c)",
  method:        "var(--pastel-4, #a08cd4)",
  http_endpoint: "var(--pastel-5, #e8c46a)",
};

const AGENTS = [
  { id: "claude",   label: "Claude Code", note: "open://..." },
  { id: "cursor",   label: "Cursor",      note: "cursor://..." },
  { id: "windsurf", label: "Windsurf",    note: "windsurf://..." },
] as const;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function relTime(ms: number): string {
  const m = Math.floor((Date.now() - ms) / 60_000);
  if (m < 1)  return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function isRepair(c: Candidate): c is RepairCandidate {
  return "issueType" in c;
}

function candidateLabel(c: Candidate, tab: "repairs" | "enrichments"): string {
  if (tab === "repairs" && isRepair(c))
    return ISSUE_LABEL[c.issueType] ?? c.issueType;
  if (!isRepair(c))
    return ENRICHMENT_LABEL[c.enrichmentType] ?? c.enrichmentType;
  return "";
}

function buildPrompt(
  c: Candidate,
  hint: string,
  tab: "repairs" | "enrichments",
): string {
  const label = candidateLabel(c, tab);
  const verb =
    tab === "repairs" ? `Fix the ${label} issue` : `Generate ${label}`;
  const hintSection = hint ? `\nHint from the team:\n${hint}\n` : "";
  return `${verb} on \`${c.entity.name}\` (${c.entity.type}) in ${c.entity.repo}.

File: ${c.entity.file}

What archigraph detected:
${c.description}
${hintSection}
After making the change, run \`archigraph rebuild ${c.entity.repo}\` to refresh the graph.`;
}

// ---------------------------------------------------------------------------
// AgentMenu dropdown
// ---------------------------------------------------------------------------

function AgentMenu({
  onPick,
}: {
  onPick: (agent: (typeof AGENTS)[number]) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", handler);
    return () => document.removeEventListener("keydown", handler);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <ExternalLink size={13} />
        Open in agent
        <ChevronRight
          size={11}
          className={cn("transition-transform", open && "rotate-90")}
        />
      </Button>

      {open && (
        <div
          role="menu"
          className="absolute bottom-full right-0 mb-1 w-52 rounded-lg border border-border bg-surface shadow-[var(--shadow-4)] py-1 z-50"
        >
          {AGENTS.map((a) => (
            <button
              key={a.id}
              role="menuitem"
              className="w-full flex flex-col items-start px-3 py-2 text-left hover:bg-surface-2 focus-visible:outline-none focus-visible:bg-surface-2"
              onClick={() => {
                setOpen(false);
                onPick(a);
              }}
            >
              <span className="text-sm font-medium text-text">{a.label}</span>
              <span className="text-xs font-mono text-text-4">{a.note}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// ConfidencePill
// ---------------------------------------------------------------------------

function ConfidencePill({
  value,
  compact = false,
}: {
  value: number;
  compact?: boolean;
}) {
  const pct = Math.round(value * 100);
  const tone =
    value >= 0.8 ? "success" : value >= 0.5 ? "warning" : "neutral";
  return (
    <Badge tone={tone} className={cn(compact && "text-[10px] h-4 px-1.5")}>
      {pct}%{!compact && " conf."}
    </Badge>
  );
}

// ---------------------------------------------------------------------------
// ListRow
// ---------------------------------------------------------------------------

interface ListRowProps {
  item: Candidate;
  tab: "repairs" | "enrichments";
  focused: boolean;
  hasHint: boolean;
  onFocus: (id: string) => void;
}

function ListRow({ item, tab, focused, hasHint, onFocus }: ListRowProps) {
  const sev =
    tab === "repairs" && isRepair(item) ? SEVERITY_META[item.severity] : null;
  const label = candidateLabel(item, tab);
  const typeColor = TYPE_COLOR[item.entity.type] ?? "var(--text-4)";

  return (
    <button
      className={cn(
        "w-full flex items-stretch gap-0 text-left transition-colors duration-[80ms]",
        "border-b border-border-soft last:border-0",
        focused
          ? "bg-surface border-l-[3px] border-l-accent"
          : "hover:bg-surface-2",
      )}
      onClick={() => onFocus(item.id)}
    >
      {/* Severity bar */}
      <span
        className="w-1 shrink-0 self-stretch"
        style={{ background: sev ? sev.color : "var(--accent)" }}
        aria-label={sev?.label ?? "enrichment"}
      />

      {/* Main content */}
      <div className="flex-1 min-w-0 px-3 py-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="font-mono text-[13px] text-text truncate">
            {item.entity.name}
          </span>
          <span className="inline-flex items-center gap-1 shrink-0">
            <span
              className="size-1.5 rounded-full"
              style={{ background: typeColor }}
            />
            <span className="font-mono text-xs text-text-3">
              {item.entity.type}
            </span>
          </span>
        </div>
        <div className="flex items-center gap-1.5 mt-0.5 text-xs text-text-3">
          <span>{label}</span>
          <span className="text-text-4">·</span>
          <span className="font-mono text-text-4 truncate">
            {item.entity.repo}
          </span>
        </div>
      </div>

      {/* Right column */}
      <div className="flex flex-col items-end justify-center gap-1 px-3 py-2 shrink-0">
        {hasHint && (
          <Badge tone="success" className="text-[10px] h-4 px-1.5">
            hint
          </Badge>
        )}
        <ConfidencePill value={item.confidence} compact />
        <span className="text-[11px] text-text-4">
          {relTime(item.detectedAt)}
        </span>
      </div>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Group (collapsible section)
// ---------------------------------------------------------------------------

interface GroupSectionProps {
  id: string;
  label: string;
  items: Candidate[];
  open: boolean;
  onToggle: () => void;
  tab: "repairs" | "enrichments";
  focusedId: string | null;
  savedHints: Record<string, string>;
  onFocus: (id: string) => void;
}

function GroupSection({
  label,
  items,
  open,
  onToggle,
  tab,
  focusedId,
  savedHints,
  onFocus,
}: GroupSectionProps) {
  return (
    <div>
      <button
        className="sticky top-0 z-10 w-full flex items-center gap-2 px-3 py-1.5 bg-bg-soft border-b border-border-soft"
        onClick={onToggle}
        aria-expanded={open}
      >
        <ChevronRight
          size={12}
          className={cn(
            "text-text-4 transition-transform",
            open && "rotate-90",
          )}
        />
        <h3 className="flex-1 text-left text-[11px] font-semibold uppercase tracking-wide font-mono text-text-3">
          {label}
        </h3>
        <span className="font-mono text-[11px] text-text-4">
          {items.length}
        </span>
      </button>
      {open &&
        items.map((item) => (
          <ListRow
            key={item.id}
            item={item}
            tab={tab}
            focused={focusedId === item.id}
            hasHint={!!savedHints[item.id]}
            onFocus={onFocus}
          />
        ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// DetailPane
// ---------------------------------------------------------------------------

interface DetailPaneProps {
  item: Candidate | null;
  tab: "repairs" | "enrichments";
  draft: string;
  savedHint: string;
  onDraftChange: (v: string) => void;
  onSave: () => void;
  saving: boolean;
  groupId: string;
}

function DetailPane({
  item,
  tab,
  draft,
  savedHint,
  onDraftChange,
  onSave,
  saving,
  groupId,
}: DetailPaneProps) {
  if (!item) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-3 text-center px-8">
        <Wrench size={28} strokeWidth={1.4} className="text-text-4" />
        <p className="text-sm font-medium text-text-2">Pick a suggestion</p>
        <p className="text-xs text-text-4 max-w-[28ch]">
          Choose any row to see what archigraph detected, then hand it off to
          your agent.
        </p>
      </div>
    );
  }

  const label = candidateLabel(item, tab);
  const sev =
    tab === "repairs" && isRepair(item) ? SEVERITY_META[item.severity] : null;
  const typeColor = TYPE_COLOR[item.entity.type] ?? "var(--text-4)";
  const dirty = draft !== savedHint;
  const prompt = buildPrompt(item, savedHint || draft, tab);

  const handleCopyPrompt = async () => {
    try {
      await navigator.clipboard.writeText(prompt);
      toast.success("Prompt copied to clipboard.");
    } catch {
      toast.warning("Couldn't access clipboard — copy manually.");
    }
  };

  const handleAgentPick = (agent: (typeof AGENTS)[number]) => {
    toast.success(
      `Would deep-link to ${agent.label} with the prompt prefilled.`,
    );
  };

  return (
    <div className="flex flex-col h-full max-w-[720px] mx-auto">
      {/* Header */}
      <header className="shrink-0 px-6 pt-5 pb-4 border-b border-border-soft">
        <div className="flex items-center gap-2 flex-wrap mb-2">
          {sev && (
            <Badge
              tone={
                sev.tone === "danger"
                  ? "danger"
                  : sev.tone === "warning"
                    ? "warning"
                    : "neutral"
              }
            >
              {sev.label}
            </Badge>
          )}
          <span className="text-sm font-medium text-text">{label}</span>
          <span className="ml-auto">
            <ConfidencePill value={item.confidence} />
          </span>
        </div>

        <div className="flex items-center gap-2 flex-wrap">
          <a
            href={`/g/${groupId}/docs`}
            className="font-mono text-[20px] font-semibold text-accent hover:underline truncate"
          >
            {item.entity.name}
          </a>
          <span className="inline-flex items-center gap-1">
            <span
              className="size-2 rounded-full"
              style={{ background: typeColor }}
            />
            <span className="font-mono text-xs text-text-3">
              {item.entity.type}
            </span>
          </span>
          <Badge tone="neutral">{item.entity.repo}</Badge>
        </div>

        <div className="mt-1 font-mono text-xs text-text-4">
          {item.entity.file}
        </div>
      </header>

      {/* Body */}
      <div className="flex-1 overflow-y-auto px-6 py-4 flex flex-col gap-6">
        {/* Section 1: What archigraph detected */}
        <section>
          <h4 className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-2">
            What archigraph detected
          </h4>
          <p className="text-sm text-text leading-relaxed max-w-[64ch]">
            {item.description}
          </p>
        </section>

        {/* Section 2: Hint textarea */}
        <section>
          <div
            className="flex items-center gap-2 mb-2"
            id={`hint-label-${item.id}`}
          >
            <h4 className="text-xs font-semibold text-text-3 uppercase tracking-wide">
              Hint for your agent
            </h4>
            <span className="text-xs text-text-4">— optional</span>
            {savedHint && !dirty && (
              <span className="text-xs text-success font-medium ml-auto">
                Saved
              </span>
            )}
            {dirty && (
              <span className="text-xs text-text-4 ml-auto">
                Unsaved changes
              </span>
            )}
          </div>

          <textarea
            aria-labelledby={`hint-label-${item.id}`}
            className={cn(
              "w-full min-h-[64px] max-h-[160px] px-3 py-2 resize-vertical rounded-md",
              "border border-border bg-surface text-sm text-text",
              "placeholder:text-text-4 focus:outline-none focus:ring-2 focus:ring-[var(--accent-ring)] focus:border-accent",
              "transition-colors",
            )}
            placeholder="e.g. 'this is a public API — keep the wording neutral and reference the migration guide if relevant'"
            value={draft}
            onChange={(e) => onDraftChange(e.target.value)}
            rows={3}
          />

          <div className="flex items-start gap-3 mt-2">
            <p className="flex-1 text-xs text-text-4 leading-relaxed">
              archigraph doesn&apos;t write code or prose itself. The hint is
              persisted and included when you hand the task off to your agent.
            </p>
            <Button
              variant="primary"
              size="sm"
              onClick={onSave}
              disabled={!dirty || saving}
              aria-disabled={!dirty}
              className="shrink-0"
            >
              {dirty ? (saving ? "Saving…" : "Save hint") : "Saved"}
            </Button>
          </div>
        </section>

        {/* Section 3: Prompt preview */}
        <section>
          <h4 className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-2">
            Generated prompt preview
          </h4>
          <pre className="rounded-md bg-surface-2 border border-border-soft px-4 py-3 text-[12px] font-mono text-text-2 whitespace-pre-wrap leading-relaxed overflow-x-auto">
            <code>{prompt}</code>
          </pre>
        </section>
      </div>

      {/* Footer (sticky bottom) */}
      <footer className="shrink-0 flex items-center justify-between gap-4 px-6 py-3 border-t border-border-soft bg-bg">
        <p className="text-xs text-text-4">
          Only the agent can resolve this candidate — archigraph will clear it
          on the next index.
        </p>
        <div className="flex items-center gap-2 shrink-0">
          <Button variant="ghost" size="sm" onClick={handleCopyPrompt}>
            <Copy size={13} />
            Copy prompt
          </Button>
          <AgentMenu onPick={handleAgentPick} />
        </div>
      </footer>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Loading skeleton
// ---------------------------------------------------------------------------

function PendingSkeleton() {
  return (
    <div className="flex flex-1 min-h-0">
      <aside className="w-[440px] shrink-0 flex flex-col gap-0 bg-bg-soft border-r border-border overflow-y-auto">
        {Array.from({ length: 6 }).map((_, i) => (
          <div
            key={i}
            className="px-3 py-2 border-b border-border-soft flex flex-col gap-1.5"
          >
            <Skeleton w="w-3/4" />
            <Skeleton w="w-1/2" h="h-2.5" />
          </div>
        ))}
      </aside>
      <section className="flex-1 flex items-center justify-center">
        <div className="text-sm text-text-4">Loading candidates…</div>
      </section>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main screen
// ---------------------------------------------------------------------------

export default function PendingScreen() {
  const { groupId = "demo" } = useParams();
  const { data, isLoading, isError } = useCandidates(groupId);
  const saveHintMutation = useSaveHint(groupId);

  const {
    tab,
    filter,
    groupBy,
    focusedId,
    openMap,
    drafts,
    savedHints,
    setTab,
    setFilter,
    setGroupBy,
    setFocusedId,
    toggleGroup,
    setDraft,
    confirmSave,
    seedServerHints,
  } = usePendingStore();

  // All items for the current tab.
  const allItems: Candidate[] = useMemo(() => {
    if (!data) return [];
    return (tab === "repairs" ? data.repairs : data.enrichments) ?? [];
  }, [data, tab]);

  // Auto-select first row when tab switches or data loads (nothing focused yet).
  useEffect(() => {
    if (allItems.length > 0 && !focusedId) {
      setFocusedId(allItems[0].id);
    }
  }, [allItems, focusedId, setFocusedId]);

  // Seed server-provided hints into the store so they populate the input on
  // first render without a separate PUT round-trip (#1518).
  useEffect(() => {
    if (!data) return;
    const hints: Record<string, string> = {};
    for (const c of [...(data.repairs ?? []), ...(data.enrichments ?? [])]) {
      if (c.hint) hints[c.entityId] = c.hint;
    }
    seedServerHints(hints);
  }, [data, seedServerHints]);

  // Filter candidates.
  const filtered = useMemo(
    () =>
      allItems.filter((item) => {
        if (filter === "high") return item.confidence >= 0.85;
        if (filter === "stale")
          return Date.now() - item.detectedAt > 86_400_000;
        return true;
      }),
    [allItems, filter],
  );

  // Group candidates.
  type GroupEntry = { id: string; label: string; items: Candidate[] };
  const groups: GroupEntry[] = useMemo(() => {
    const map = new Map<string, GroupEntry>();
    for (const item of filtered) {
      let key: string;
      let label: string;
      if (groupBy === "type") {
        key = isRepair(item) ? item.issueType : item.enrichmentType;
        label = candidateLabel(item, tab);
      } else if (
        groupBy === "severity" &&
        tab === "repairs" &&
        isRepair(item)
      ) {
        key = item.severity;
        label = SEVERITY_META[item.severity].label;
      } else if (groupBy === "repo") {
        key = item.entity?.repo ?? "unknown";
        label = item.entity?.repo ?? "unknown";
      } else {
        key = "_all";
        label = "All";
      }
      if (!map.has(key)) map.set(key, { id: key, label, items: [] });
      map.get(key)!.items.push(item);
    }
    return Array.from(map.values());
  }, [filtered, groupBy, tab]);

  const totalUnresolved =
    (data?.repairs?.length ?? 0) + (data?.enrichments?.length ?? 0);
  const focusedItem = focusedId
    ? allItems.find((i) => i.id === focusedId) ?? null
    : null;
  // Hints are keyed by entityId (stable), not by the ephemeral candidate id (#1518).
  const focusedEntityId = focusedItem?.entityId ?? null;
  const focusedDraft = focusedEntityId
    ? (drafts[focusedEntityId] ?? savedHints[focusedEntityId] ?? "")
    : "";
  const focusedSaved = focusedEntityId ? (savedHints[focusedEntityId] ?? "") : "";

  const handleSaveHint = () => {
    if (!focusedItem) return;
    saveHintMutation.mutate(
      { entityId: focusedItem.entityId, hint: focusedDraft },
      {
        onSuccess: () => {
          confirmSave(focusedItem.entityId, focusedDraft);
          toast.success("Hint saved.");
        },
        onError: () => {
          toast.error("Couldn't save hint — try again.");
        },
      },
    );
  };

  return (
    <div className="flex flex-col h-full">
      {/* Stat bar */}
      <div className="shrink-0 flex items-center justify-end px-4 h-10 border-b border-border-soft bg-bg">
        {isError ? (
          <span className="text-xs text-danger">
            Couldn&apos;t load candidates.
          </span>
        ) : (
          <Badge tone="neutral" className="font-mono">
            {totalUnresolved} unresolved
          </Badge>
        )}
      </div>

      {/* Intro */}
      <div className="shrink-0 px-4 pt-3 space-y-2 border-b border-border-soft bg-bg">
        <ScreenDescription>
          Pending candidates — edits archigraph has queued but not yet applied to
          the graph: repairs (resolving an unresolved reference) and enrichments
          (adding inferred metadata). Review each, then accept or dismiss it.
        </ScreenDescription>
        <AgentUsage
          tool="archigraph_enrichments"
          example="An agent checks pending enrichments/repairs queued for the graph."
        />
      </div>

      {/* Tab bar */}
      <div className="shrink-0 flex items-center gap-3 px-4 h-11 border-b border-border-soft bg-bg">
        <Tabs
          value={tab}
          onValueChange={(v) => setTab(v as "repairs" | "enrichments")}
        >
          <TabsList className="border-0 gap-0">
            <TabsTrigger value="repairs" className="gap-1.5">
              <Wrench size={14} />
              Repair candidates
              <Badge
                tone={tab === "repairs" ? "accent" : "neutral"}
                className="text-[10px] h-4 px-1.5"
              >
                {data?.repairs?.length ?? 0}
              </Badge>
            </TabsTrigger>
            <TabsTrigger value="enrichments" className="gap-1.5">
              <Sparkles size={14} />
              Enrichment candidates
              <Badge
                tone={tab === "enrichments" ? "accent" : "neutral"}
                className="text-[10px] h-4 px-1.5"
              >
                {data?.enrichments?.length ?? 0}
              </Badge>
            </TabsTrigger>
          </TabsList>
        </Tabs>

        <div className="flex-1" />

        {/* Filter segmented control */}
        <div className="inline-flex items-center h-7 rounded-md border border-border-soft bg-surface-2 p-0.5 gap-0.5">
          {(["all", "high", "stale"] as const).map((f) => (
            <button
              key={f}
              onClick={() => setFilter(f)}
              className={cn(
                "h-6 px-2.5 rounded text-xs font-medium transition-colors",
                filter === f
                  ? "bg-surface text-text shadow-sm"
                  : "text-text-3 hover:text-text",
              )}
            >
              {f === "all" ? "All" : f === "high" ? "High conf." : ">24h"}
            </button>
          ))}
        </div>

        {/* Group by select */}
        <div className="inline-flex items-center gap-1.5 text-xs">
          <span className="text-text-4">Group by</span>
          <select
            className="h-7 px-2 bg-surface border border-border rounded-md text-xs font-mono text-text focus:outline-none focus:ring-2 focus:ring-[var(--accent-ring)]"
            value={groupBy}
            onChange={(e) => setGroupBy(e.target.value as typeof groupBy)}
          >
            <option value="type">Issue type</option>
            <option value="severity">Severity</option>
            <option value="repo">Repository</option>
            <option value="none">None</option>
          </select>
        </div>
      </div>

      {/* Split pane */}
      {isLoading ? (
        <PendingSkeleton />
      ) : (
        <div className="flex flex-1 min-h-0">
          {/* Left list */}
          <aside className="w-[440px] shrink-0 flex flex-col bg-bg-soft border-r border-border overflow-y-auto">
            {groups.length === 0 ? (
              <div className="flex items-center gap-2 justify-center h-24 text-xs text-text-4">
                No suggestions match this filter.
              </div>
            ) : (
              groups.map((g) => (
                <GroupSection
                  key={g.id}
                  id={g.id}
                  label={g.label}
                  items={g.items}
                  open={openMap[g.id] !== false}
                  onToggle={() => toggleGroup(g.id)}
                  tab={tab}
                  focusedId={focusedId}
                  savedHints={savedHints}
                  onFocus={setFocusedId}
                />
              ))
            )}
          </aside>

          {/* Right detail */}
          <section className="flex-1 min-w-0 overflow-hidden">
            <DetailPane
              item={focusedItem}
              tab={tab}
              draft={focusedDraft}
              savedHint={focusedSaved}
              onDraftChange={(v) => focusedEntityId && setDraft(focusedEntityId, v)}
              onSave={handleSaveHint}
              saving={saveHintMutation.isPending}
              groupId={groupId}
            />
          </section>
        </div>
      )}
    </div>
  );
}
