/* ============================================================
   Settings — per-group management surface.

   Route: /g/:groupId/settings
   Per docs/screens/settings.md and design_handoff_grafel prototype.

   Sections (top to bottom):
     1. Header card — group name, health, stats, Rebuild button
     2. Repositories — repo list, monorepo expansion, Add repo
     3. Features — watchers + git-hooks live toggles
     4. Group docs — docs path input
     5. Health check — Doctor results
     6. Danger zone — Delete group

   Modals: ConfirmModal, DeleteGroupModal, RemoveRepoModal, AddRepoModal

   Data: useSettingsGroup (GET /api/v2/groups/:id)
         All mutations through use-settings.ts hooks.
   ============================================================ */

import { useState, useRef, useEffect } from "react";
import { useParams, useNavigate } from "react-router-dom";
import {
  ChevronRight,
  Plus,
  MoreHorizontal,
  RefreshCw,
  Info,
  AlertTriangle,
  CheckCircle,
  Loader2,
  Download,
  Upload,
} from "lucide-react";
import { toast } from "sonner";

import { api } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";

import {
  Button,
  Card,
  Input,
  InfoLabel,
  Tooltip,
  TooltipTrigger,
  TooltipContent,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@/components/ui";
import {
  useSettingsGroup,
  usePatchFeatures,
  usePatchDocs,
  useRebuildGroup,
  useDeleteGroup,
  useRemoveRepo,
  useRebuildRepo,
  useResetRepo,
  usePatchMonorepo,
  useRunDoctor,
  useActionJob,
  useTools,
  usePutTools,
} from "@/hooks/use-settings";
import { ScanWizard } from "@/components/chrome/scan-wizard";
import { ApiError } from "@/lib/api";
import type { SettingsRepo, SettingsGroup, DoctorCheck, MonorepoPkg, ToolApplyResult } from "@/data/types";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Determine whether a group is a monorepo group (a single git root split into
 * N module paths, each registered as a Repo entry with a non-null .monorepo
 * field) vs a true multi-repo group (N separate git roots, each without
 * per-repo monorepo metadata).
 *
 * Detection heuristic (client-only, no extra API field needed):
 *   • Monorepo group  — every repo in the group has a non-null .monorepo
 *     field (i.e. was registered with detected modules). Noun = "module",
 *     count = group.repos.length (each entry IS a module).
 *   • Multi-repo group — at least one repo has .monorepo == null (separate
 *     git roots). Noun = "repo", count = group.repos.length.
 *
 * Edge case: empty group → defaults to "repo".
 */
function groupRepoNoun(group: SettingsGroup): { count: number; noun: string; nounPlural: string } {
  const isMonorepoGroup =
    group.repos.length > 0 && group.repos.every((r) => r.monorepo != null);
  if (isMonorepoGroup) {
    return { count: group.repos.length, noun: "module", nounPlural: "modules" };
  }
  return { count: group.repos.length, noun: "repo", nounPlural: "repos" };
}

function relativeTime(ms: number | null): string {
  if (!ms) return "never";
  const diff = Date.now() - ms;
  const min = Math.floor(diff / 60000);
  if (min < 1) return "just now";
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  return `${Math.floor(hr / 24)}d ago`;
}

function fidelityColor(f: number): string {
  if (f >= 0.8) return "var(--success)";
  if (f >= 0.5) return "var(--warning)";
  return "var(--danger)";
}

// ---------------------------------------------------------------------------
// Stack badge
// ---------------------------------------------------------------------------

const STACK_COLORS: Record<string, string> = {
  "react-native": "var(--pastel-1)",
  react: "var(--pastel-1)",
  next: "var(--pastel-5)",
  node: "var(--pastel-2)",
  typescript: "var(--pastel-9)",
  python: "var(--pastel-6)",
  go: "var(--pastel-9)",
  java: "var(--pastel-3)",
  config: "var(--text-4)",
};

function StackBadge({ stack }: { stack: string }) {
  const color = STACK_COLORS[stack] ?? "var(--text-4)";
  const label = stack || "unknown";
  return (
    <span className="inline-flex items-center gap-1.5 h-5 px-2 rounded-full border border-border bg-surface-2 text-xs font-mono text-text-2">
      <span className="size-2 rounded-full shrink-0" style={{ background: color }} />
      {label}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Section shell
// ---------------------------------------------------------------------------

function Section({
  id,
  title,
  sub,
  action,
  children,
  danger,
}: {
  id?: string;
  title: string;
  sub?: string;
  action?: React.ReactNode;
  children: React.ReactNode;
  danger?: boolean;
}) {
  return (
    <section
      id={id}
      className={cn(
        "rounded-xl border p-5",
        danger
          ? "border-danger/40 bg-danger/5"
          : "border-border bg-surface",
      )}
    >
      <header className="flex items-start justify-between gap-4 mb-4">
        <div>
          <h2 className={cn("text-base font-semibold", danger ? "text-danger" : "text-text")}>
            {title}
          </h2>
          {sub && <p className="mt-0.5 text-sm text-text-3">{sub}</p>}
        </div>
        {action}
      </header>
      <div>{children}</div>
    </section>
  );
}

// ---------------------------------------------------------------------------
// 1. Header card
// ---------------------------------------------------------------------------

const HEALTH_CONFIG = {
  healthy: { label: "Healthy", color: "var(--success)" },
  warning: { label: "Needs attention", color: "var(--warning)" },
  // <0.90 fidelity. Keep the (danger) color, but use calmer wording — a bare
  // "Critical" reads like the tool is broken, when a low score is usually just
  // a fresh/large codebase mid-resolution. Always paired with the explainer.
  degraded: { label: "Low fidelity", color: "var(--danger)" },
  unindexed: { label: "Not indexed", color: "var(--text-4)" },
} as const;

/**
 * Plain-language explainer for what "Fidelity" means and how the status tiers
 * are derived. Reused on both the header status badge and the Fidelity metric
 * so the red/amber state never appears without context. Rendered as rich
 * tooltip content (the Tooltip / InfoLabel `hint` accepts a ReactNode).
 */
function FidelityExplainer() {
  return (
    <div className="space-y-1.5 text-text-2">
      <p>
        <span className="font-medium text-text">Fidelity</span> measures how
        completely the resolver linked your codebase&apos;s imports and
        references — i.e. graph quality. It&apos;s roughly{" "}
        <span className="font-mono">100% − unresolved-reference rate</span>{" "}
        (unresolved imports/references and orphaned entities).
      </p>
      <ul className="space-y-0.5">
        <li>
          <span className="inline-block size-2 rounded-full align-middle mr-1.5" style={{ background: "var(--success)" }} />
          Healthy — ≥ 97%
        </li>
        <li>
          <span className="inline-block size-2 rounded-full align-middle mr-1.5" style={{ background: "var(--warning)" }} />
          Needs attention — ≥ 90%
        </li>
        <li>
          <span className="inline-block size-2 rounded-full align-middle mr-1.5" style={{ background: "var(--danger)" }} />
          Low fidelity — &lt; 90%
        </li>
      </ul>
      <p className="text-text-3">
        A lower score is expected for a fresh or large codebase that&apos;s
        still mid-resolution — it climbs as more references are linked.
      </p>
    </div>
  );
}

function HeaderCard({ group, onRebuild }: { group: SettingsGroup; onRebuild: () => void }) {
  const h = HEALTH_CONFIG[group.health];
  const { count: repoCount, noun: repoNoun } = groupRepoNoun(group);
  const repoLabel = repoNoun === "module" ? "Modules" : "Repositories";
  const repoHint =
    repoNoun === "module"
      ? "Detected sub-modules within this monorepo. Each module is indexed independently."
      : "Top-level git repos indexed in this group.";

  return (
    <Card className="p-5">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-center gap-2 min-w-0">
          <span className="size-2.5 rounded-full shrink-0" style={{ background: h.color }} aria-label={h.label} />
          <h1 className="font-mono text-xl font-semibold text-text truncate">{group.name}</h1>
          <Tooltip>
            <TooltipTrigger asChild>
              <span
                className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full border text-text-2 cursor-help"
                style={{ borderColor: h.color, color: h.color, background: `color-mix(in srgb, ${h.color} 10%, transparent)` }}
              >
                {h.label}
                <Info size={11} className="opacity-70" />
              </span>
            </TooltipTrigger>
            <TooltipContent className="max-w-sm">
              <FidelityExplainer />
            </TooltipContent>
          </Tooltip>
        </div>
        <Button variant="secondary" size="sm" onClick={onRebuild} className="shrink-0">
          <RefreshCw size={12} />
          Rebuild group
        </Button>
      </div>

      {/* Always pair a non-healthy status with a plain-language caption so the
          badge color never reads as "the tool is broken". */}
      {(group.health === "degraded" || group.health === "warning") && group.indexedAt != null && (
        <p className="mt-3 flex items-start gap-1.5 text-xs text-text-3">
          <Info size={12} className="mt-px shrink-0 text-text-4" />
          <span>
            Fidelity is {Math.round(group.fidelity * 100)}% — the resolver
            hasn&apos;t linked every import &amp; reference yet. This is normal
            for a fresh or large codebase and climbs as indexing completes.
          </span>
        </p>
      )}

      <dl className="mt-4 grid grid-cols-4 gap-4">
        {[
          {
            key: "repos",
            label: repoLabel,
            hint: repoHint,
            value: repoCount,
            mono: true,
          },
          {
            key: "entities",
            label: "Entities",
            hint: "Every function, class, hook, endpoint, and module grafel extracted.",
            value: group.entities.toLocaleString(),
            mono: true,
          },
          {
            key: "fidelity",
            label: "Fidelity",
            hint: <FidelityExplainer />,
            value: group.indexedAt != null ? `${Math.round(group.fidelity * 100)}%` : "—",
            mono: true,
            style: group.indexedAt ? { color: fidelityColor(group.fidelity) } : undefined,
          },
          {
            key: "indexed",
            label: "Last indexed",
            hint: "Most recent time grafel re-scanned any repo in this group.",
            value: relativeTime(group.indexedAt),
            mono: true,
          },
        ].map(({ key, label, hint, value, mono, style }) => (
          <div key={key}>
            <dt className="text-xs text-text-3">
              <InfoLabel label={label} hint={hint} />
            </dt>
            <dd className={cn("text-lg tabular-nums mt-0.5", mono && "font-mono")} style={style}>
              {value}
            </dd>
          </div>
        ))}
      </dl>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// 2. Repositories
// ---------------------------------------------------------------------------

function RepoMoreMenu({
  onRebuild,
  onReset,
  onRedetect,
  onPauseWatcher,
  onRemove,
}: {
  onRebuild: () => void;
  onReset: () => void;
  onRedetect: () => void;
  onPauseWatcher: () => void;
  onRemove: () => void;
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

  const fire = (fn: () => void) => {
    setOpen(false);
    fn();
  };

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        aria-label="More repo actions"
        onClick={() => setOpen((v) => !v)}
        className={cn(
          "inline-flex items-center justify-center size-7 rounded-md text-text-3",
          "hover:bg-surface-2 hover:text-text transition-colors",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
        )}
      >
        <MoreHorizontal size={14} />
      </button>
      {open && (
        <div
          role="menu"
          className={cn(
            "absolute right-0 top-8 z-20 min-w-[180px] rounded-lg border border-border bg-surface shadow-[var(--shadow-3)]",
            "py-1",
          )}
        >
          {[
            { label: "Rebuild repo", fn: onRebuild },
            { label: "Reset cache & rebuild", fn: onReset },
            { label: "Re-detect stack", fn: onRedetect },
            { label: "Pause watcher", fn: onPauseWatcher },
          ].map(({ label, fn }) => (
            <button
              key={label}
              role="menuitem"
              className="block w-full text-left px-3 py-1.5 text-sm text-text-2 hover:bg-surface-2"
              onClick={() => fire(fn)}
            >
              {label}
            </button>
          ))}
          <div className="my-1 border-t border-border-soft" />
          <button
            role="menuitem"
            className="block w-full text-left px-3 py-1.5 text-sm text-danger hover:bg-danger/10"
            onClick={() => fire(onRemove)}
          >
            Remove from group
          </button>
        </div>
      )}
    </div>
  );
}

function MonorepoPanel({
  repo,
  onToggle,
}: {
  repo: SettingsRepo;
  onToggle: (pkg: MonorepoPkg) => void;
}) {
  const mono = repo.monorepo!;
  const [showAll, setShowAll] = useState(false);
  const indexed = mono.packages.filter((p) => p.indexed).length;
  const visible = showAll ? mono.packages : mono.packages.slice(0, 10);

  return (
    <div className="mt-2 ml-6 border border-border-soft rounded-lg overflow-hidden">
      <div className="px-3 py-2 flex items-center justify-between border-b border-border-soft bg-bg-soft text-xs text-text-3">
        <span>
          Detected via <code className="font-mono text-text-2">{mono.detector}</code> · pick which packages to index
        </span>
        <span className="font-mono">
          {indexed} of {mono.packages.length} indexed
        </span>
      </div>
      <div className="divide-y divide-border-soft">
        {visible.map((pkg) => (
          <label
            key={pkg.path}
            className={cn(
              "flex items-center gap-3 px-3 py-2 cursor-pointer text-sm",
              "hover:bg-surface-2 transition-colors",
              pkg.indexed && "bg-accent-soft/20",
            )}
          >
            <input
              type="checkbox"
              checked={pkg.indexed}
              onChange={() => onToggle(pkg)}
              className="accent-[var(--accent)] rounded"
            />
            <span className="flex-1 font-mono text-text-2 truncate">{pkg.path}</span>
            <StackBadge stack={pkg.stack} />
            {pkg.files > 0 && (
              <span className="text-xs text-text-4 font-mono tabular-nums shrink-0">
                {pkg.files.toLocaleString()} files
              </span>
            )}
          </label>
        ))}
        {!showAll && mono.packages.length > 10 && (
          <button
            type="button"
            className="w-full py-2 text-sm text-accent-strong hover:bg-surface-2 text-center"
            onClick={() => setShowAll(true)}
          >
            Show all {mono.packages.length} packages
          </button>
        )}
      </div>
    </div>
  );
}

function RepoRow({
  repo,
  onRemove,
  onRebuild,
  onReset,
  onRedetect,
  onPauseWatcher,
  onTogglePackage,
}: {
  repo: SettingsRepo;
  onRemove: () => void;
  onRebuild: () => void;
  onReset: () => void;
  onRedetect: () => void;
  onPauseWatcher: () => void;
  onTogglePackage: (pkg: MonorepoPkg) => void;
}) {
  const isMono = !!repo.monorepo;
  const [expanded, setExpanded] = useState(isMono);
  const indexedPkgs = repo.monorepo?.packages?.filter((p) => p.indexed).length ?? 0;

  return (
    <div className="border border-border-soft rounded-lg overflow-hidden">
      <div className="flex items-center gap-2 px-3 py-2.5 bg-surface">
        {/* expand chevron or bullet */}
        <button
          type="button"
          aria-label={isMono ? "Toggle monorepo packages" : undefined}
          disabled={!isMono}
          onClick={() => isMono && setExpanded((v) => !v)}
          className={cn(
            "size-5 inline-flex items-center justify-center rounded text-text-3 shrink-0",
            isMono
              ? "hover:bg-surface-2 hover:text-text cursor-pointer"
              : "cursor-default",
          )}
        >
          {isMono ? (
            <ChevronRight
              size={13}
              className={cn("transition-transform duration-150", expanded && "rotate-90")}
            />
          ) : (
            <span className="size-1.5 rounded-full bg-text-4" />
          )}
        </button>

        {/* name + badges */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="font-mono text-sm font-medium text-text">{repo.slug}</span>
            <StackBadge stack={repo.stack} />
            {isMono && (
              <span className="text-xs font-mono text-text-3 border border-border-soft rounded px-1.5">
                Monorepo · {indexedPkgs}/{repo.monorepo!.packages.length} indexed
              </span>
            )}
          </div>
          <div className="font-mono text-xs text-text-4 truncate mt-0.5">{repo.path}</div>
        </div>

        {/* meta stats */}
        <div className="hidden sm:flex items-center gap-4 shrink-0">
          {[
            { label: "Files", hint: "Source files included in indexing.", value: repo.files.toLocaleString() },
            { label: "Entities", hint: "Extracted functions, classes, hooks, etc.", value: repo.entities.toLocaleString() },
          ].map(({ label, hint, value }) => (
            <div key={label} className="text-right">
              <div className="font-mono text-sm tabular-nums text-text-2">{value}</div>
              <div className="text-xs text-text-4">
                <InfoLabel label={label} hint={hint} />
              </div>
            </div>
          ))}
          <div className="text-right">
            <div className="font-mono text-sm tabular-nums text-text-2">{relativeTime(repo.indexedAt)}</div>
            <div className="text-xs text-text-4">indexed</div>
          </div>
        </div>

        {/* actions */}
        <div className="flex items-center gap-1 shrink-0">
          <button
            type="button"
            title="Rebuild this repo"
            onClick={onRebuild}
            className={cn(
              "inline-flex items-center justify-center size-7 rounded-md text-text-3",
              "hover:bg-surface-2 hover:text-text transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
            )}
          >
            <RefreshCw size={12} />
          </button>
          <RepoMoreMenu
            onRebuild={onRebuild}
            onReset={onReset}
            onRedetect={onRedetect}
            onPauseWatcher={onPauseWatcher}
            onRemove={onRemove}
          />
        </div>
      </div>

      {/* monorepo panel */}
      {isMono && expanded && (
        <div className="px-3 pb-3">
          <MonorepoPanel repo={repo} onToggle={onTogglePackage} />
        </div>
      )}
    </div>
  );
}

function RepositoriesSection({
  group,
  groupId,
  onRemove,
  onAddRepo,
}: {
  group: SettingsGroup;
  groupId: string;
  onRemove: (repo: SettingsRepo) => void;
  onAddRepo: () => void;
}) {
  const rebuildRepo = useRebuildRepo(groupId);
  const resetRepo = useResetRepo(groupId);
  const patchMonorepo = usePatchMonorepo(groupId);

  // Track the in-flight async repo job (#1512): poll until terminal, then toast.
  const [repoJobId, setRepoJobId] = useState<string | null>(null);
  const repoJob = useActionJob(groupId, repoJobId);
  useEffect(() => {
    if (!repoJob.data) return;
    if (repoJob.data.status === "done") {
      toast.success(repoJob.data.message ?? "Rebuild complete.");
      setRepoJobId(null);
    } else if (repoJob.data.status === "failed") {
      toast.error(repoJob.data.error ?? "Rebuild failed.");
      setRepoJobId(null);
    }
  }, [repoJob.data]);

  const handleTogglePackage = (repo: SettingsRepo, pkg: MonorepoPkg) => {
    if (!repo.monorepo) return;
    const current = repo.monorepo.packages.map((p) =>
      p.path === pkg.path ? { ...p, indexed: !p.indexed } : p,
    );
    const selected = current.filter((p) => p.indexed).map((p) => p.path);
    patchMonorepo.mutate(
      { repoSlug: repo.slug, packages: selected },
      { onError: () => toast.error("Failed to save package selection.") },
    );
  };

  const { count: repoCount, noun: repoNoun, nounPlural: repoNounPlural } = groupRepoNoun(group);
  const sectionTitle = repoNoun === "module" ? "Modules" : "Repositories";
  const sectionSub = `${repoCount} ${repoNounPlural} indexed in this group.`;

  return (
    <Section
      id="repositories"
      title={sectionTitle}
      sub={sectionSub}
      action={
        <Button variant="ghost" size="sm" onClick={onAddRepo}>
          <Plus size={13} />
          Add repo
        </Button>
      }
    >
      <div className="space-y-2">
        {group.repos.length === 0 && (
          <p className="text-sm text-text-3 text-center py-6">
            No repos yet.{" "}
            <button type="button" onClick={onAddRepo} className="text-accent-strong underline">
              Add one.
            </button>
          </p>
        )}
        {group.repos.map((repo) => (
          <RepoRow
            key={repo.slug}
            repo={repo}
            onRemove={() => onRemove(repo)}
            onRebuild={() =>
              rebuildRepo.mutate(repo.slug, {
                onSuccess: (d) => {
                  toast.info("Rebuild queued.");
                  setRepoJobId(d.job_id);
                },
                onError: () => toast.error("Failed to queue rebuild."),
              })
            }
            onReset={() =>
              resetRepo.mutate(repo.slug, {
                onSuccess: (d) => {
                  toast.info("Reset queued.");
                  setRepoJobId(d.job_id);
                },
                onError: () => toast.error("Failed to queue reset."),
              })
            }
            onRedetect={() => toast.info("Re-detect stack: wiring tracked in epic #1432.")}
            onPauseWatcher={() => toast.info("Pause watcher: wiring tracked in epic #1432.")}
            onTogglePackage={(pkg) => handleTogglePackage(repo, pkg)}
          />
        ))}
      </div>
    </Section>
  );
}

// ---------------------------------------------------------------------------
// 3. Features
// ---------------------------------------------------------------------------

function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative inline-flex h-5 w-9 items-center rounded-full shrink-0 transition-colors duration-150",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--accent-ring)]",
        checked ? "bg-accent" : "bg-border-strong",
      )}
    >
      <span
        className={cn(
          "inline-block size-3.5 rounded-full bg-white shadow-sm transition-transform duration-150",
          checked ? "translate-x-4" : "translate-x-0.5",
        )}
      />
    </button>
  );
}

function ToggleRow({
  title,
  desc,
  checked,
  pending,
  onChange,
}: {
  title: string;
  desc: string;
  checked: boolean;
  pending?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-4 py-3 first:pt-0 last:pb-0">
      <div className="min-w-0">
        <div className="text-sm font-medium text-text">{title}</div>
        <div className="text-xs text-text-3 mt-0.5">{desc}</div>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        {pending && <Loader2 size={13} className="animate-spin text-text-3" />}
        <Switch checked={checked} onChange={onChange} label={title} />
      </div>
    </div>
  );
}

function FeaturesSection({ group, groupId }: { group: SettingsGroup; groupId: string }) {
  const patchFeatures = usePatchFeatures(groupId);
  const [local, setLocal] = useState(group.features);

  // Sync local state when group data refreshes.
  useEffect(() => setLocal(group.features), [group.features]);

  const toggle = (key: "watchers" | "gitHooks") => {
    const next = { ...local, [key]: !local[key] };
    setLocal(next);
    patchFeatures.mutate(next, {
      onError: () => {
        // Revert on error.
        setLocal(local);
        toast.error("Failed to save feature toggle.");
      },
    });
  };

  return (
    <Section
      id="features"
      title="Features"
      sub="Changes save instantly and apply to every repo in this group."
    >
      <div className="divide-y divide-border-soft">
        <ToggleRow
          title="Filesystem watchers"
          desc="Auto-reindex repos when files change on disk. Low overhead; keeps the graph fresh."
          checked={local.watchers}
          pending={patchFeatures.isPending}
          onChange={() => toggle("watchers")}
        />
        <ToggleRow
          title="Git commit hooks"
          desc="A git commit triggers a partial reindex of the touched files in that repo."
          checked={local.gitHooks}
          pending={patchFeatures.isPending}
          onChange={() => toggle("gitHooks")}
        />
      </div>
    </Section>
  );
}

// ---------------------------------------------------------------------------
// 4. Group docs
// ---------------------------------------------------------------------------

function DocsSection({ group, groupId }: { group: SettingsGroup; groupId: string }) {
  const patchDocs = usePatchDocs(groupId);
  const [path, setPath] = useState(group.docsPath ?? "");

  useEffect(() => setPath(group.docsPath ?? ""), [group.docsPath]);

  const save = () => {
    if (path === (group.docsPath ?? "")) return;
    patchDocs.mutate(path, {
      onSuccess: () => toast.success("Docs path saved."),
      onError: () => toast.error("Failed to save docs path."),
    });
  };

  return (
    <Section
      id="docs"
      title="Group docs"
      sub="Shared prose used by /generate-docs and the Docs surface. Leave blank to skip."
    >
      <div className="flex gap-2">
        <Input
          value={path}
          onChange={(e) => setPath(e.target.value)}
          onBlur={save}
          placeholder="/path/to/docs"
          className="flex-1 font-mono text-sm"
        />
        <Button
          variant="secondary"
          size="sm"
          onClick={() => toast.info("showDirectoryPicker is not wired yet.")}
        >
          Browse…
        </Button>
      </div>
    </Section>
  );
}

// ---------------------------------------------------------------------------
// 4b. AI coding tools (#5257, epic #5252)
//
// Checklist of every AI coding tool grafel can install into, with each tool's
// enabled + (detected) state. Saving applies the delta IN-PROCESS via
// PUT /api/v2/groups/:id/tools (the daemon stays up — no `grafel install`
// subprocess, no daemon restart) and surfaces a per-tool written/removed
// summary.
//
// Create-time selection: the SPA's create-group flow (ScanWizard mode="create")
// indexes a new group asynchronously and does not yet thread a tool selection
// into the create request — that is CLI-only today via `grafel install`
// (#5256). This panel covers post-create editing for every group.
// ---------------------------------------------------------------------------

const TOOL_ACTION_STYLE: Record<ToolApplyResult["action"], string> = {
  written: "text-success",
  removed: "text-warning",
  unchanged: "text-text-4",
  error: "text-danger",
};

function ToolsSection({ groupId }: { groupId: string }) {
  const { data, isLoading, isError } = useTools(groupId);
  const putTools = usePutTools(groupId);

  // Local checked-set, seeded from the server and kept in sync on refetch.
  const [checked, setChecked] = useState<Record<string, boolean>>({});
  const [summary, setSummary] = useState<ToolApplyResult[] | null>(null);

  useEffect(() => {
    if (!data) return;
    const next: Record<string, boolean> = {};
    for (const t of data.tools) next[t.id] = t.enabled;
    setChecked(next);
  }, [data]);

  const dirty =
    !!data &&
    data.tools.some((t) => !!checked[t.id] !== t.enabled);

  const save = () => {
    const tools = Object.entries(checked)
      .filter(([, on]) => on)
      .map(([id]) => id);
    setSummary(null);
    putTools.mutate(tools, {
      onSuccess: (reply) => {
        setSummary(reply.summary);
        const changed = reply.summary.filter((s) => s.action === "written" || s.action === "removed").length;
        const errs = reply.summary.filter((s) => s.action === "error").length;
        if (errs > 0) toast.error(`${errs} tool${errs !== 1 ? "s" : ""} failed to apply.`);
        else toast.success(changed > 0 ? `Updated ${changed} tool${changed !== 1 ? "s" : ""}.` : "No changes.");
      },
      onError: () => toast.error("Failed to save tool selection."),
    });
  };

  return (
    <Section
      id="tools"
      title="AI coding tools"
      sub="Pick which AI coding tools grafel installs its MCP entry + rules files into. Changes apply instantly across every repo in this group — the daemon stays up."
      action={
        <Button
          variant="primary"
          size="sm"
          disabled={!dirty || putTools.isPending}
          onClick={save}
          data-testid="btn-save-tools"
        >
          {putTools.isPending ? <Loader2 size={12} className="animate-spin" /> : null}
          Save tools
        </Button>
      }
    >
      {isLoading ? (
        <div className="space-y-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} h="h-9" className="rounded-lg" />
          ))}
        </div>
      ) : isError || !data ? (
        <p className="text-sm text-text-3 py-4 text-center">Failed to load tool status.</p>
      ) : (
        <>
          <div className="divide-y divide-border-soft">
            {data.tools.map((t) => {
              const applied = summary?.find((s) => s.id === t.id);
              return (
                <label
                  key={t.id}
                  className="flex items-center gap-3 py-2.5 cursor-pointer first:pt-0 last:pb-0"
                >
                  <input
                    type="checkbox"
                    checked={!!checked[t.id]}
                    onChange={(e) => setChecked((c) => ({ ...c, [t.id]: e.target.checked }))}
                    className="accent-[var(--accent)] rounded"
                    data-testid={`tool-checkbox-${t.id}`}
                  />
                  <span className="flex-1 min-w-0">
                    <span className="text-sm font-medium text-text">{t.displayName}</span>
                    <span className="ml-2 font-mono text-xs text-text-4">{t.id}</span>
                  </span>
                  {t.detected && (
                    <span className="text-xs font-mono text-success border border-success/40 rounded px-1.5 py-0.5">
                      detected
                    </span>
                  )}
                  {applied && applied.action !== "unchanged" && (
                    <span className={cn("text-xs font-mono", TOOL_ACTION_STYLE[applied.action])}>
                      {applied.action}
                    </span>
                  )}
                </label>
              );
            })}
          </div>
          {!data.explicit && (
            <p className="mt-3 flex items-start gap-1.5 text-xs text-text-3">
              <Info size={12} className="mt-px shrink-0 text-text-4" />
              <span>
                No explicit selection yet — every supported tool is enabled by
                default. Saving records an explicit set.
              </span>
            </p>
          )}
        </>
      )}
    </Section>
  );
}

// ---------------------------------------------------------------------------
// 5. Health check
// ---------------------------------------------------------------------------

const STATUS_ICON: Record<DoctorCheck["status"], React.ReactNode> = {
  ok: <CheckCircle size={13} className="text-success shrink-0" />,
  warning: <AlertTriangle size={13} className="text-warning shrink-0" />,
  info: <Info size={13} className="text-accent-strong shrink-0" />,
  error: <AlertTriangle size={13} className="text-danger shrink-0" />,
};

function HealthSection({ groupId }: { groupId: string }) {
  const runDoctor = useRunDoctor(groupId);

  return (
    <Section
      id="health"
      title="Health check"
      sub="Runs grafel doctor across this group. Catches stale caches, missing hooks, daemon issues."
      action={
        <Button
          variant="primary"
          size="sm"
          disabled={runDoctor.isPending}
          onClick={() => runDoctor.mutate()}
        >
          {runDoctor.isPending ? (
            <>
              <Loader2 size={12} className="animate-spin" />
              Running…
            </>
          ) : (
            "Run health check"
          )}
        </Button>
      }
    >
      {runDoctor.data ? (
        <div className="space-y-1.5">
          {runDoctor.data.map((check) => (
            <div key={check.id} className="flex items-center gap-2 text-sm py-1.5">
              {STATUS_ICON[check.status]}
              <span className="flex-1 text-text-2">{check.label}</span>
              <span className="text-text-4 font-mono text-xs">{check.detail}</span>
            </div>
          ))}
        </div>
      ) : (
        <div className="flex items-center gap-2 text-sm text-text-3 py-4 justify-center">
          <Info size={14} />
          No recent health check. Click Run to see daemon status, watcher state, and pending work.
        </div>
      )}
    </Section>
  );
}

// ---------------------------------------------------------------------------
// 6. Export / Import (#1627)
//
// Parallel to the docs export shipped in #1658, this section lets users
// archive the entire indexed graph + fleet config for a group (or download
// the graph + docs together) and import an archive back to recreate the
// group. Designed extensibly: today supports zip + (graph|all); future
// formats slot into the same UI.
// ---------------------------------------------------------------------------

function ExportImportSection({ group, groupId }: { group: SettingsGroup; groupId: string }) {
  const [kind, setKind] = useState<"graph" | "all">("graph");
  const [importOpen, setImportOpen] = useState(false);

  const exportHref = api.graphExportUrl(groupId, { format: "zip", kind });

  return (
    <Section
      id="export-import"
      title="Export & Import"
      sub="Back up this group as a zip, or restore one from a previous backup. Designed to enable machine-to-machine transfers."
    >
      <div className="space-y-4">
        {/* Export controls */}
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="text-sm font-medium text-text">Export this group</div>
            <div className="text-xs text-text-3 mt-0.5">
              Streams the indexed graph (graph.fb, enrichments, links, embeddings)
              plus the fleet config as a single zip. <code className="font-mono">all</code>{" "}
              also bundles the generated docs.
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            <div
              role="radiogroup"
              aria-label="Export contents"
              className="inline-flex rounded-md border border-border-soft overflow-hidden"
            >
              {(["graph", "all"] as const).map((k) => (
                <button
                  key={k}
                  type="button"
                  role="radio"
                  aria-checked={kind === k}
                  onClick={() => setKind(k)}
                  className={cn(
                    "px-2.5 py-1 text-xs font-mono",
                    kind === k
                      ? "bg-accent text-white"
                      : "bg-surface text-text-2 hover:bg-surface-2",
                  )}
                >
                  {k}
                </button>
              ))}
            </div>
            <Button asChild variant="secondary" size="sm">
              <a
                href={exportHref}
                download={`${group.name}-${kind}.zip`}
                data-testid="btn-graph-export"
              >
                <Download size={12} />
                Download
              </a>
            </Button>
          </div>
        </div>

        <div className="border-t border-border-soft" />

        {/* Import controls */}
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="text-sm font-medium text-text">Import a group</div>
            <div className="text-xs text-text-3 mt-0.5">
              Restore a group from a zip previously produced by Export. Touches
              registry state; requires explicit confirmation.
            </div>
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setImportOpen(true)}
            data-testid="btn-graph-import"
          >
            <Upload size={12} />
            Import…
          </Button>
        </div>
      </div>

      <ImportGroupModal open={importOpen} onClose={() => setImportOpen(false)} />
    </Section>
  );
}

function ImportGroupModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [file, setFile] = useState<File | null>(null);
  const [nameOverride, setNameOverride] = useState("");
  const [force, setForce] = useState(false);
  const [pending, setPending] = useState(false);
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  const reset = () => {
    setFile(null);
    setNameOverride("");
    setForce(false);
    setPending(false);
  };

  const handleClose = () => {
    if (pending) return;
    reset();
    onClose();
  };

  const handleImport = async () => {
    if (!file) return;
    setPending(true);
    try {
      const reply = await api.graphImport(file, {
        force,
        name: nameOverride.trim() || undefined,
      });
      toast.success(`Imported "${reply.group}" (${reply.repos.length} repos).`);
      // Force a meta refresh so the new group shows up in the sidebar / landing.
      queryClient.invalidateQueries({ queryKey: ["meta"] });
      queryClient.invalidateQueries({ queryKey: ["groups"] });
      reset();
      onClose();
      navigate(`/g/${encodeURIComponent(reply.group)}/settings`);
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : "Import failed.";
      toast.error(msg);
      setPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && handleClose()}>
      <DialogContent>
        <DialogTitle>Import a group</DialogTitle>
        <DialogDescription>
          Restore a group from a zip archive previously produced by Export. The
          imported store is written under the daemon&apos;s grafel home; source
          repos on disk are untouched.
        </DialogDescription>

        <div className="mt-4 space-y-3">
          <label className="block">
            <span className="block text-sm text-text-2 mb-1">Archive (.zip)</span>
            <input
              type="file"
              accept=".zip,application/zip"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
              className="block w-full text-sm text-text-2 file:mr-3 file:rounded-md file:border-0 file:bg-surface-2 file:px-3 file:py-1.5 file:text-sm file:text-text file:hover:bg-surface"
              data-testid="import-file-input"
            />
            {file && (
              <span className="block mt-1 text-xs text-text-3 font-mono truncate">
                {file.name} · {(file.size / 1024).toFixed(1)} KB
              </span>
            )}
          </label>

          <label className="block">
            <span className="block text-sm text-text-2 mb-1">
              Register as (optional)
            </span>
            <Input
              value={nameOverride}
              onChange={(e) => setNameOverride(e.target.value)}
              placeholder="leave blank to keep the archive's group name"
              className="font-mono text-sm"
            />
          </label>

          <label className="flex items-start gap-2 text-sm cursor-pointer">
            <input
              type="checkbox"
              checked={force}
              onChange={(e) => setForce(e.target.checked)}
              className="mt-0.5 accent-[var(--accent)]"
            />
            <span>
              <span className="font-medium text-text">Overwrite existing group</span>
              <span className="text-text-3">
                {" "}
                — required if a group with this name already exists. Replaces its
                fleet config + cached store.
              </span>
            </span>
          </label>

          <ul className="mt-2 space-y-1.5 text-xs text-text-3">
            <li className="flex items-start gap-2">
              <Info size={12} className="text-accent-strong mt-0.5 shrink-0" />
              The daemon validates the archive layout before writing anything.
            </li>
            <li className="flex items-start gap-2">
              <AlertTriangle size={12} className="text-warning mt-0.5 shrink-0" />
              Overwrite replaces the existing group&apos;s cached graph + fleet config.
            </li>
          </ul>
        </div>

        <div className="mt-5 flex justify-end gap-2">
          <Button variant="ghost" onClick={handleClose} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant="primary"
            disabled={!file || pending}
            onClick={handleImport}
            data-testid="btn-import-confirm"
          >
            {pending ? <Loader2 size={13} className="animate-spin" /> : <Upload size={13} />}
            Import group
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// 7. Danger zone
// ---------------------------------------------------------------------------

function DangerZone({ group, groupId }: { group: SettingsGroup; groupId: string }) {
  const [open, setOpen] = useState(false);
  return (
    <Section id="danger" title="Danger zone" danger>
      <div className="flex items-center justify-between gap-4">
        <div>
          <div className="text-sm font-medium text-text">Delete this group</div>
          <div className="text-xs text-text-3 mt-0.5">
            Removes the fleet config + all cached graphs. Source code is untouched.
          </div>
        </div>
        <Button variant="danger" size="sm" onClick={() => setOpen(true)} data-testid="btn-delete-group">
          Delete group
        </Button>
      </div>
      <DeleteGroupModal
        open={open}
        groupId={groupId}
        groupName={group.name}
        repoCount={group.repos.length}
        onClose={() => setOpen(false)}
      />
    </Section>
  );
}

// ---------------------------------------------------------------------------
// Modals
// ---------------------------------------------------------------------------

function ConfirmModal({
  open,
  title,
  description,
  bullets,
  primaryLabel,
  intent = "default",
  pending,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: React.ReactNode;
  description: React.ReactNode;
  bullets?: { kind: "info" | "warn" | "ok"; text: string }[];
  primaryLabel: string;
  intent?: "default" | "danger";
  pending?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription asChild>
          <p className="mt-2 text-sm text-text-3">{description}</p>
        </DialogDescription>
        {bullets && (
          <ul className="mt-3 space-y-1.5">
            {bullets.map((b, i) => (
              <li key={i} className="flex items-start gap-2 text-sm">
                {b.kind === "ok" ? (
                  <CheckCircle size={13} className="text-success mt-0.5 shrink-0" />
                ) : b.kind === "warn" ? (
                  <AlertTriangle size={13} className="text-warning mt-0.5 shrink-0" />
                ) : (
                  <Info size={13} className="text-accent-strong mt-0.5 shrink-0" />
                )}
                <span className="text-text-2">{b.text}</span>
              </li>
            ))}
          </ul>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant={intent === "danger" ? "danger" : "primary"}
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? <Loader2 size={13} className="animate-spin" /> : null}
            {primaryLabel}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DeleteGroupModal({
  open,
  groupId,
  groupName,
  repoCount,
  onClose,
}: {
  open: boolean;
  groupId: string;
  groupName: string;
  repoCount: number;
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const deleteGroup = useDeleteGroup(groupId);
  const [typed, setTyped] = useState("");
  const match = typed === groupName;

  const handleDelete = async () => {
    try {
      await deleteGroup.mutateAsync();
      toast.success(`Group "${groupName}" deleted.`);
      navigate("/");
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : "Failed to delete group.";
      toast.error(msg);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && !deleteGroup.isPending && onClose()}>
      <DialogContent>
        <DialogTitle>
          Delete <span className="font-mono">{groupName}</span>?
        </DialogTitle>
        <DialogDescription>
          This permanently removes the group from grafel. It cannot be undone.
        </DialogDescription>

        <div className="mt-4 space-y-3">
          <div>
            <p className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-1.5">Will be removed</p>
            <ul className="space-y-1 text-sm text-text-2">
              <li>
                <code className="font-mono text-text">~/.config/grafel/{groupName}.fleet.json</code>
              </li>
              <li>
                {repoCount} cached graph{repoCount !== 1 ? "s" : ""} for this group
              </li>
              <li>Filesystem watchers + git hooks in each repo</li>
            </ul>
          </div>
          <div>
            <p className="text-xs font-semibold text-text-3 uppercase tracking-wide mb-1.5">Will NOT be removed</p>
            <ul className="space-y-1 text-sm text-text-3">
              <li>Your repository source code (untouched on disk)</li>
              <li>The grafel daemon</li>
              <li>Other groups</li>
            </ul>
          </div>

          <div>
            <label className="block text-sm text-text-2 mb-1">
              Type <span className="font-mono font-semibold text-text">{groupName}</span> to confirm
            </label>
            <Input
              autoFocus
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder={groupName}
              data-testid="delete-confirm-input"
            />
          </div>
        </div>

        <div className="mt-5 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose} disabled={deleteGroup.isPending}>
            Cancel
          </Button>
          <Button
            variant="danger"
            disabled={!match || deleteGroup.isPending}
            onClick={handleDelete}
            data-testid="btn-delete-confirm"
          >
            {deleteGroup.isPending ? (
              <Loader2 size={13} className="animate-spin" />
            ) : null}
            Delete <span className="font-mono ml-1">{groupName}</span> forever
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function RemoveRepoModal({
  open,
  groupId,
  repo,
  onClose,
}: {
  open: boolean;
  groupId: string;
  repo: SettingsRepo | null;
  onClose: () => void;
}) {
  const removeRepo = useRemoveRepo(groupId);
  const [keepCache, setKeepCache] = useState(false);

  if (!repo) return null;

  const handleRemove = async () => {
    try {
      await removeRepo.mutateAsync({ repoSlug: repo.slug, keepCache });
      toast.success(`Repo "${repo.slug}" removed from group.`);
      onClose();
    } catch (e) {
      const msg = e instanceof ApiError ? e.message : "Failed to remove repo.";
      toast.error(msg);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && !removeRepo.isPending && onClose()}>
      <DialogContent>
        <DialogTitle>
          Remove <span className="font-mono">{repo.slug}</span>?
        </DialogTitle>
        <DialogDescription>
          Removes this repo from the group. Other repos stay indexed.
        </DialogDescription>

        <ul className="mt-4 space-y-1.5 text-sm text-text-2">
          <li className="flex items-start gap-2">
            <AlertTriangle size={13} className="text-warning mt-0.5 shrink-0" />
            The repo entry is removed from fleet.json
          </li>
          <li className="flex items-start gap-2">
            <AlertTriangle size={13} className="text-warning mt-0.5 shrink-0" />
            Filesystem watcher + git hook for this repo are torn down
          </li>
          <li className="flex items-start gap-2">
            <Info size={13} className="text-accent-strong mt-0.5 shrink-0" />
            Cross-repo edges become dangling until next group rebuild
          </li>
          {!keepCache && (
            <li className="flex items-start gap-2">
              <AlertTriangle size={13} className="text-warning mt-0.5 shrink-0" />
              Cached graph is cleaned up from disk
            </li>
          )}
        </ul>

        <label className="mt-4 flex items-start gap-2 text-sm cursor-pointer">
          <input
            type="checkbox"
            checked={keepCache}
            onChange={(e) => setKeepCache(e.target.checked)}
            className="mt-0.5 accent-[var(--accent)]"
          />
          <span>
            <span className="font-medium text-text">Keep cached graph on disk</span>
            <span className="text-text-3"> — useful if you want to re-add this repo later without re-indexing.</span>
          </span>
        </label>

        <div className="mt-5 flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose} disabled={removeRepo.isPending}>
            Cancel
          </Button>
          <Button
            variant="danger"
            disabled={removeRepo.isPending}
            onClick={handleRemove}
          >
            {removeRepo.isPending ? <Loader2 size={13} className="animate-spin" /> : null}
            Remove repo
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Rebuild confirm specs (single source of truth per design doc)
// ---------------------------------------------------------------------------

type ConfirmKind = "rebuild-group" | "rebuild-repo" | "reset-repo" | "redetect-stack" | "pause-watcher";

interface ConfirmCtx {
  group: SettingsGroup;
  repo?: SettingsRepo;
}

const CONFIRM_SPECS: Record<
  ConfirmKind,
  (ctx: ConfirmCtx) => {
    title: React.ReactNode;
    intent: "default" | "danger";
    primaryLabel: string;
    description: string;
    bullets: { kind: "info" | "warn" | "ok"; text: string }[];
  }
> = {
  "rebuild-group": ({ group }) => ({
    title: (
      <>
        Rebuild <span className="font-mono">{group.name}</span>?
      </>
    ),
    intent: "default",
    primaryLabel: "Rebuild group",
    description:
      "Re-index every repo in this group. Cached AST is reused where possible, so it's much faster than a reset.",
    bullets: [
      { kind: "info", text: `Scans ${group.repos.length} repos and re-runs extraction + algorithms.` },
      { kind: "info", text: "Typically 10–60 seconds per repo. Dashboard stays live during indexing." },
      { kind: "ok", text: "Non-destructive — the current graph is only replaced when each repo finishes." },
    ],
  }),
  "rebuild-repo": ({ repo }) => ({
    title: (
      <>
        Rebuild <span className="font-mono">{repo?.slug}</span>?
      </>
    ),
    intent: "default",
    primaryLabel: "Rebuild repo",
    description: "Re-index this one repository. The rest of the group stays untouched.",
    bullets: [
      { kind: "info", text: `~${(repo?.files ?? 0).toLocaleString()} files will be re-scanned.` },
      { kind: "info", text: "Cross-repo edges that touched this repo refresh on completion." },
      { kind: "ok", text: "Existing cache is reused — typically a few seconds." },
    ],
  }),
  "reset-repo": ({ repo }) => ({
    title: (
      <>
        Reset cache & rebuild <span className="font-mono">{repo?.slug}</span>?
      </>
    ),
    intent: "danger",
    primaryLabel: "Reset & rebuild",
    description:
      "Wipes the cached AST + graph for this repo, then re-indexes from scratch. Use when the graph looks wrong.",
    bullets: [
      { kind: "warn", text: `Deletes ${repo?.slug}/.grafel/ (cached AST freed).` },
      { kind: "warn", text: "Slower than rebuild — no cache to reuse. Expect 30–120 seconds." },
      { kind: "ok", text: "Repo source code is untouched on disk." },
    ],
  }),
  "redetect-stack": ({ repo }) => ({
    title: (
      <>
        Re-detect stack for <span className="font-mono">{repo?.slug}</span>?
      </>
    ),
    intent: "default",
    primaryLabel: "Re-detect",
    description: "Re-runs the stack heuristics (manifests, lockfiles, language stats) against this repo.",
    bullets: [
      { kind: "info", text: "Quick — runs in seconds, no re-indexing yet." },
      { kind: "info", text: "If the detected stack changes, a partial rebuild kicks off automatically." },
    ],
  }),
  "pause-watcher": ({ repo }) => ({
    title: (
      <>
        Pause watcher for <span className="font-mono">{repo?.slug}</span>?
      </>
    ),
    intent: "default",
    primaryLabel: "Pause watcher",
    description: "Stops auto-indexing on file changes for this repo. Re-enable anytime from the same menu.",
    bullets: [
      { kind: "info", text: "Manual rebuild still works while paused." },
      { kind: "warn", text: "The graph will drift from source until re-enabled or rebuilt." },
    ],
  }),
};

// ---------------------------------------------------------------------------
// Screen root
// ---------------------------------------------------------------------------

export default function SettingsScreen() {
  const { groupId = "" } = useParams<{ groupId: string }>();
  const navigate = useNavigate();
  const { data: group, isLoading, isError, error } = useSettingsGroup(groupId);
  const rebuildGroup = useRebuildGroup(groupId);

  // Track the in-flight async group-rebuild job (#1512) and toast on completion.
  const [groupJobId, setGroupJobId] = useState<string | null>(null);
  const groupJob = useActionJob(groupId, groupJobId);
  useEffect(() => {
    if (!groupJob.data) return;
    if (groupJob.data.status === "done") {
      toast.success(groupJob.data.message ?? "Rebuild complete.");
      setGroupJobId(null);
    } else if (groupJob.data.status === "failed") {
      toast.error(groupJob.data.error ?? "Rebuild failed.");
      setGroupJobId(null);
    }
  }, [groupJob.data]);

  const [removeRepo, setRemoveRepo] = useState<SettingsRepo | null>(null);
  const [addRepoOpen, setAddRepoOpen] = useState(false);
  const [confirm, setConfirm] = useState<{ kind: ConfirmKind; repo?: SettingsRepo } | null>(null);

  // Redirect on group-not-found.
  useEffect(() => {
    if (isError && error instanceof ApiError && error.status === 404) {
      navigate("/", { replace: true });
    }
  }, [isError, error, navigate]);

  if (isLoading) {
    return (
      <div className="mx-auto w-full max-w-[880px] px-6 py-10 space-y-4">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} h="h-24" className="rounded-xl" />
        ))}
      </div>
    );
  }

  if (isError || !group) {
    return (
      <div className="flex flex-col items-center justify-center h-full py-20 text-center">
        <h2 className="text-xl font-semibold text-text">Group not found.</h2>
        <p className="mt-2 text-md text-text-3">
          It may have been deleted or renamed.
        </p>
        <Button className="mt-6" onClick={() => navigate("/")}>
          Back to groups
        </Button>
      </div>
    );
  }

  const confirmSpec =
    confirm && CONFIRM_SPECS[confirm.kind]
      ? CONFIRM_SPECS[confirm.kind]({ group, repo: confirm.repo })
      : null;

  return (
    <div className="mx-auto w-full max-w-[880px] px-6 py-10 space-y-8" data-testid="settings-screen">
      {/* 1. Header */}
      <HeaderCard
        group={group}
        onRebuild={() => setConfirm({ kind: "rebuild-group" })}
      />

      {/* 2. Repositories */}
      <RepositoriesSection
        group={group}
        groupId={groupId}
        onRemove={(repo) => setRemoveRepo(repo)}
        onAddRepo={() => setAddRepoOpen(true)}
      />

      {/* 3. Features */}
      <FeaturesSection group={group} groupId={groupId} />

      {/* 4. Group docs */}
      <DocsSection group={group} groupId={groupId} />

      {/* 4b. AI coding tools (#5257) */}
      <ToolsSection groupId={groupId} />

      {/* 5. Health check */}
      <HealthSection groupId={groupId} />

      {/* 6. Export & Import (#1627) */}
      <ExportImportSection group={group} groupId={groupId} />

      {/* 7. Danger zone */}
      <DangerZone group={group} groupId={groupId} />

      {/* --- Modals --- */}
      <RemoveRepoModal
        open={!!removeRepo}
        groupId={groupId}
        repo={removeRepo}
        onClose={() => setRemoveRepo(null)}
      />

      <ScanWizard
        open={addRepoOpen}
        onOpenChange={setAddRepoOpen}
        mode="add-repo"
        groupId={groupId}
        groupName={group.name}
        existingPaths={group.repos.map((r) => r.path)}
      />

      {confirmSpec && (
        <ConfirmModal
          open
          title={confirmSpec.title}
          description={confirmSpec.description}
          bullets={confirmSpec.bullets}
          primaryLabel={confirmSpec.primaryLabel}
          intent={confirmSpec.intent}
          pending={rebuildGroup.isPending}
          onClose={() => setConfirm(null)}
          onConfirm={() => {
            if (confirm?.kind === "rebuild-group") {
              rebuildGroup.mutate(undefined, {
                onSuccess: (d) => {
                  toast.info("Rebuild queued.");
                  setGroupJobId(d.job_id);
                  setConfirm(null);
                },
                onError: () => {
                  toast.error("Failed to queue rebuild.");
                  setConfirm(null);
                },
              });
            } else {
              // Other confirm kinds are stubs.
              toast.info(`Action "${confirm?.kind}" is tracked in epic #1432.`);
              setConfirm(null);
            }
          }}
        />
      )}
    </div>
  );
}
