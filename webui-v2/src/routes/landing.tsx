/* ============================================================
   Landing — group selector + create-group + empty state.

   The entry point at `/`. Renders WITHOUT the in-group chrome
   (no NavRail / breadcrumb): there's no group context yet. Per
   docs/screens/landing.md.

   Data: useGroups() (GET /api/v2/groups), useCreateGroup()
   (POST /api/v2/groups), useMeta() (daemon version for the popover).
   ============================================================ */

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowRight, Plus, MoreHorizontal } from "lucide-react";
import { toast } from "sonner";

import { Button, Card, InfoLabel, Kbd, Skeleton } from "@/components/ui";
import { LandingTopBar } from "@/components/chrome/landing-top-bar";
import { ScanWizard } from "@/components/chrome/scan-wizard";
import { Constellation, seedFromString } from "@/components/viz/constellation";
import { useGroups } from "@/hooks/use-groups";
import { useMeta } from "@/hooks/use-meta";
import type { Group, GroupHealth } from "@/data/types";
import { cn } from "@/lib/utils";

/* ---------- helpers ---------- */

const HEALTH: Record<GroupHealth, { label: string; dot: string }> = {
  healthy: { label: "Healthy", dot: "var(--success)" },
  warning: { label: "Low fidelity", dot: "var(--warning)" },
  degraded: { label: "Needs work", dot: "var(--danger)" },
  unindexed: { label: "Not indexed", dot: "var(--text-4)" },
};

function fidelityColor(f: number | null): string {
  if (f == null) return "var(--text-4)";
  if (f >= 0.8) return "var(--success)";
  if (f >= 0.5) return "var(--warning)";
  return "var(--danger)";
}

function relativeTime(ms: number | null): string {
  if (!ms) return "never indexed";
  const diff = Date.now() - ms;
  const min = Math.floor(diff / 60000);
  if (min < 1) return "just now";
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  if (day < 7) return `${day}d ago`;
  return `${Math.floor(day / 7)}w ago`;
}

/* ---------- info tips ---------- */

const ENTITIES_TIP =
  "Every function, class, hook, component, endpoint and module archigraph extracted — the nodes of the graph.";
const REPOS_TIP =
  "Top-level git repositories in this group. Monorepos count as one; open the group to see sub-packages.";
const FIDELITY_TIP =
  "Confidence that the graph matches your codebase. Drops with stale, orphaned, or low-confidence entities.";

/* ---------- group card ---------- */

function GroupCard({ group, onPick, onManage }: { group: Group; onPick: () => void; onManage: () => void }) {
  const isEmpty = group.health === "unindexed";
  const health = HEALTH[group.health];

  return (
    <Card className="group relative p-0 overflow-hidden text-left transition-all duration-150 hover:-translate-y-0.5 hover:shadow-[var(--shadow-3)] focus-within:-translate-y-0.5">
      <button
        type="button"
        onClick={onPick}
        className="block w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] rounded-lg"
        data-testid={`group-card-${group.id}`}
      >
        {/* viz band */}
        <div className="relative h-[92px] bg-bg-soft border-b border-border-soft">
          <Constellation
            seed={seedFromString(group.id)}
            clusters={Math.min(6, Math.max(2, group.repos.length || 2))}
            empty={isEmpty}
            className="h-full w-full"
          />
        </div>

        {/* body */}
        <div className="p-4">
          <div className="flex items-center gap-2">
            <span
              className="size-2 rounded-full shrink-0"
              style={{ background: health.dot }}
              title={health.label}
              aria-label={health.label}
            />
            <span className="font-mono font-semibold text-text truncate">{group.name}</span>
            <span className="ml-auto text-sm text-text-4 whitespace-nowrap">
              {relativeTime(group.indexedAt)}
            </span>
          </div>

          {isEmpty ? (
            <div className="mt-3">
              <p className="text-sm text-text-3">No entities indexed yet.</p>
              <span className="mt-2 inline-flex items-center gap-1 text-md font-medium text-accent-strong">
                Index this group
                <ArrowRight size={12} />
              </span>
            </div>
          ) : (
            <>
              <dl className="mt-3 grid grid-cols-3 gap-2">
                <div>
                  <dt className="text-xs text-text-3">
                    <InfoLabel label="Entities" hint={ENTITIES_TIP} />
                  </dt>
                  <dd className="font-mono text-lg font-semibold text-text tabular-nums">
                    {group.entityCount.toLocaleString()}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-text-3">
                    <InfoLabel label="Repos" hint={REPOS_TIP} />
                  </dt>
                  <dd className="font-mono text-lg text-text-2 tabular-nums">{group.repos.length}</dd>
                </div>
                <div>
                  <dt className="text-xs text-text-3">
                    <InfoLabel label="Fidelity" hint={FIDELITY_TIP} />
                  </dt>
                  <dd
                    className="font-mono text-lg tabular-nums"
                    style={{ color: fidelityColor(group.fidelity) }}
                  >
                    {group.fidelity == null ? "—" : `${Math.round(group.fidelity * 100)}%`}
                  </dd>
                </div>
              </dl>

              {group.repos.length > 0 && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  {group.repos.slice(0, 3).map((r) => (
                    <span
                      key={r}
                      className="inline-flex items-center h-5 px-2 rounded-full bg-surface-2 border border-border text-xs font-mono text-text-2"
                    >
                      {r}
                    </span>
                  ))}
                  {group.repos.length > 3 && (
                    <span className="inline-flex items-center h-5 px-2 rounded-full bg-surface-2 border border-border text-xs text-text-3">
                      +{group.repos.length - 3}
                    </span>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      </button>

      {/* manage kebab — focusable for keyboard users, not just hover */}
      {!isEmpty && (
        <button
          type="button"
          onClick={onManage}
          title="Group settings"
          aria-label={`Settings for ${group.name}`}
          className={cn(
            "absolute right-2 top-2 inline-flex items-center justify-center size-7 rounded-md",
            "bg-overlay text-text-3 opacity-0 transition-opacity",
            "hover:bg-surface-2 hover:text-text",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)] focus-visible:opacity-100",
            "group-hover:opacity-100",
          )}
          style={{ background: "var(--overlay)" }}
        >
          <MoreHorizontal size={15} />
        </button>
      )}
    </Card>
  );
}

/* ---------- add-group tile ---------- */

function AddGroupCard({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid="add-group"
      className={cn(
        "min-h-[200px] rounded-lg border border-dashed border-border-strong bg-transparent",
        "grid place-items-center text-center transition-colors",
        "hover:border-accent hover:bg-accent-soft/30",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
      )}
    >
      <div className="flex flex-col items-center gap-2 px-4">
        <span className="inline-flex items-center justify-center size-10 rounded-full border border-border-strong text-text-3">
          <Plus size={22} />
        </span>
        <span className="text-md font-medium text-text-2">Add a group</span>
        <span className="text-sm text-text-4">Point archigraph at one or more repos</span>
      </div>
    </button>
  );
}

/* ---------- empty state ---------- */

function EmptyArt() {
  return (
    <svg viewBox="0 0 200 120" width="200" height="120" aria-hidden>
      <defs>
        <linearGradient id="ag-empty" x1="0" x2="1" y1="0" y2="1">
          <stop offset="0" stopColor="var(--accent)" stopOpacity="0.6" />
          <stop offset="1" stopColor="var(--accent-strong)" stopOpacity="0.95" />
        </linearGradient>
      </defs>
      <g stroke="var(--border)" strokeWidth="0.6" fill="none">
        <line x1="60" y1="40" x2="80" y2="60" />
        <line x1="80" y1="60" x2="100" y2="46" />
        <line x1="100" y1="46" x2="118" y2="62" />
        <line x1="118" y1="62" x2="138" y2="50" />
        <line x1="80" y1="60" x2="98" y2="82" />
        <line x1="98" y1="82" x2="118" y2="62" />
      </g>
      <g>
        <circle cx="60" cy="40" r="3" fill="var(--pastel-1)" />
        <circle cx="80" cy="60" r="3.4" fill="var(--pastel-5)" />
        <circle cx="100" cy="46" r="3" fill="var(--pastel-2)" />
        <circle cx="118" cy="62" r="3.8" fill="url(#ag-empty)" />
        <circle cx="138" cy="50" r="3" fill="var(--pastel-3)" />
        <circle cx="98" cy="82" r="3" fill="var(--pastel-7)" />
      </g>
    </svg>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <section className="flex flex-col items-center text-center py-20">
      <EmptyArt />
      <h1 className="mt-6 text-2xl font-semibold text-text">Index your first group.</h1>
      <p className="mt-2 max-w-md text-md text-text-3">
        Point archigraph at one or more repositories to build a queryable graph of every function,
        class, endpoint, and the edges between them.
      </p>
      <div className="mt-6 flex flex-wrap items-center justify-center gap-3">
        <Button onClick={onCreate} className="gap-2">
          Create your first group
          <ArrowRight size={14} />
        </Button>
        <Button
          variant="ghost"
          onClick={() => toast.info("Joining a teammate's group isn't wired yet.")}
        >
          Join a teammate's group
        </Button>
      </div>
      <button
        type="button"
        onClick={() => toast.info("Re-scanning ~/.config/archigraph isn't wired yet.")}
        className="mt-5 text-sm text-text-4 hover:text-text-2"
      >
        Already configured via CLI? Re-scan local groups →
      </button>
    </section>
  );
}

/* ---------- screen ---------- */

export default function Landing() {
  const navigate = useNavigate();
  const { data: groups, isLoading, isError } = useGroups();
  const { data: meta } = useMeta();
  const [wizardOpen, setWizardOpen] = useState(false);

  const list = groups ?? [];
  const takenNames = list.map((g) => g.id);

  function pick(g: Group) {
    if (g.health === "unindexed") {
      toast.info(`“${g.name}” has no indexed entities yet — onboarding flow is not wired.`);
      return;
    }
    navigate(`/g/${g.id}/graph`);
  }

  return (
    <div className="flex flex-col h-full bg-bg">
      <LandingTopBar version={meta?.version} />

      <main className="flex-1 min-h-0 ag-scroll">
        <div className="mx-auto w-full max-w-[1080px] px-6 py-12">
          {isError ? (
            <div className="py-20 text-center">
              <h1 className="text-xl font-semibold text-text">Couldn't reach the daemon.</h1>
              <p className="mt-2 text-md text-text-3">
                Make sure archigraph is running, then reload.
              </p>
            </div>
          ) : isLoading ? (
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} h="h-[200px]" className="rounded-lg" />
              ))}
            </div>
          ) : list.length === 0 ? (
            <EmptyState onCreate={() => setWizardOpen(true)} />
          ) : (
            <>
              <header className="mb-8">
                <h1 className="text-2xl font-semibold text-text">Pick a group to explore.</h1>
                <p className="mt-1 text-md text-text-3">
                  Each group is a constellation of repos indexed into a single dependency graph.
                </p>
              </header>

              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                {list.map((g) => (
                  <GroupCard
                    key={g.id}
                    group={g}
                    onPick={() => pick(g)}
                    onManage={() => navigate(`/g/${g.id}/settings`)}
                  />
                ))}
                <AddGroupCard onClick={() => setWizardOpen(true)} />
              </div>

              <footer className="mt-10 flex items-center justify-center gap-1.5 text-sm text-text-4">
                <span>Tip: press</span>
                <Kbd>⌘</Kbd>
                <Kbd>K</Kbd>
                <span>to open the command palette anywhere in archigraph.</span>
              </footer>
            </>
          )}
        </div>
      </main>

      <ScanWizard
        open={wizardOpen}
        onOpenChange={setWizardOpen}
        mode="create"
        takenNames={takenNames}
        onIndexed={(group) => navigate(`/g/${group}/graph`)}
      />
    </div>
  );
}
