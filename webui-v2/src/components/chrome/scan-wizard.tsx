/* ============================================================
   ScanWizard — shared create-group / add-repo scan→detect→index wizard (#1517).

   Used by BOTH Landing (mode="create") and Settings (mode="add-repo").
   Three steps:

     1. Pick directory — a SERVER-SIDE folder browser (#1529). The daemon
        runs on this machine, so GET /api/v2/fs/list walks ITS filesystem:
        the user navigates folders and "Select this folder" yields a real
        ABSOLUTE path — no manual paste required. (The browser File System
        Access API only yields an opaque handle with no on-disk path, so it
        can't tell the daemon WHICH directory to index — that's why the
        daemon lists its own filesystem instead.) A manual path field is
        kept as a fallback for users who prefer to paste. (See v2_fs.go /
        v2_wizard.go for the matching backend notes.)

     2. Detect — POST /api/v2/scan/inspect previews the detected stack +
        monorepo layout + suggested group/slug. The user confirms.

     3. Index — POST /api/v2/groups/from-scan (create) or
        /api/v2/groups/{g}/repos/scan (add-repo) enqueues an async job; the
        wizard streams queued→running→done via the job poller (#1522 pattern).
   ============================================================ */

import { useEffect, useState } from "react";
import {
  CheckCircle2,
  Folder,
  ChevronRight,
  CornerLeftUp,
  Loader2,
  AlertTriangle,
  ArrowRight,
  Check,
  CheckSquare,
  Square,
} from "lucide-react";
import { toast } from "sonner";

import { Button, Input, Badge } from "@/components/ui";
import {
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
} from "@/components/ui";
import {
  useScanInspect,
  useCreateGroupFromScan,
  useScanReposIntoGroup,
  useWizardJob,
  useFsList,
  useMCPToolsDetect,
} from "@/hooks/use-wizard";
import { useIndexProgress } from "@/hooks/use-index-progress";
import { aggregateProgress, overallPhaseLabel } from "@/lib/index-progress-fold";
import { IndexProgressFeed } from "@/components/chrome/index-progress-feed";
import { ApiError } from "@/lib/api";
import type { ScanInspectReply, WizardRepo } from "@/data/types";
import {
  WIZARD_ACTIONS,
  defaultActionFor,
  type WizardAction,
} from "@/lib/wizard-action";
import {
  defaultSelectedIds,
  shouldShowMCPStep,
  autoMCPSelection,
} from "@/lib/mcp-tools-default";
import { cn } from "@/lib/utils";

// Action-first flow (#5336): the wizard now opens on an action choice (single /
// group / monorepo / add-to-group) — parity with the CLI wizard — before the
// folder picker. The chosen action tunes the Detect step's labels and defaults.
// "mcp" — choose which AI tools get the grafel MCP server (#5344). Create mode
// only; skipped when ≤1 MCP-capable tool is detected.
type WizardStep = "action" | "pick" | "detect" | "mcp" | "index";

export interface ScanWizardProps {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  /** "create" → ask for a group name + create it. "add-repo" → add into groupId. */
  mode: "create" | "add-repo";
  /** Required when mode==="add-repo". */
  groupId?: string;
  groupName?: string;
  /** Slugs/paths already taken so we can warn on duplicates. */
  takenNames?: string[];
  existingPaths?: string[];
  /** Fired once the index job reaches "done" (e.g. navigate into the group). */
  onIndexed?: (group: string) => void;
}

function slugify(s: string): string {
  return s.toLowerCase().trim().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}

/**
 * The per-repo list this index will register, as {@link WizardRepo}s — one per
 * selected child git repo, else one per selected monorepo package, else the
 * whole folder as a single repo. SINGLE source of truth shared by runIndex (the
 * from-scan payload) and the progress feed (seeding one row per repo, #5340), so
 * the seeded rows exactly match the repos the daemon will index.
 */
function buildRepos(
  scan: ScanInspectReply | null,
  selectedChildren: Set<string>,
  selectedPkgs: Set<string>,
): WizardRepo[] {
  if (!scan?.valid) return [];
  const hasChildGitRepos = (scan.childGitRepos?.length ?? 0) > 0;
  const isMonorepo = (scan.packages?.length ?? 0) > 0;
  if (hasChildGitRepos) {
    return [...selectedChildren].sort().map((child) => ({
      path: `${scan.absPath}/${child}`,
      slug: slugify(child),
    }));
  }
  if (isMonorepo) {
    return [...selectedPkgs].sort().map((pkg) => ({
      path: `${scan.absPath}/${pkg}`,
      slug: slugify(`${scan.suggestedSlug}-${pkg}`),
      modules: [pkg],
    }));
  }
  return [{ path: scan.absPath, slug: scan.suggestedSlug }];
}

export function ScanWizard(props: ScanWizardProps) {
  const { open, onOpenChange, mode, groupId, groupName, takenNames = [], existingPaths = [], onIndexed } = props;

  // In "create" mode the wizard opens on the action choice; in "add-repo" mode
  // the action is implicitly "add-group" so we skip straight to the picker.
  const [step, setStep] = useState<WizardStep>(mode === "create" ? "action" : "pick");
  const [action, setAction] = useState<WizardAction>(mode === "add-repo" ? "add-group" : "single");
  const [path, setPath] = useState("");
  const [scan, setScan] = useState<ScanInspectReply | null>(null);
  const [name, setName] = useState("");
  const [jobId, setJobId] = useState<string | null>(null);
  // Monorepo module selection (#1531). When Detect finds a monorepo, the user
  // picks WHICH package roots to index; each selected package is registered as
  // its own repo (its absolute sub-path), so indexing a subset works and the
  // Index step shows one row per chosen module. Default: all packages checked.
  const [selectedPkgs, setSelectedPkgs] = useState<Set<string>>(new Set());
  // Multi-repo-parent selection (#1531 follow-up). When Detect finds child git
  // repos, the user picks WHICH ones to index; each is registered as its own
  // repo (its absolute sub-path). Default: all children checked.
  const [selectedChildren, setSelectedChildren] = useState<Set<string>>(new Set());

  // MCP-tools selection (#5344). Which AI tools get the grafel MCP server. Only
  // relevant in create mode; defaulted from the detector's smart (B+C) defaults
  // once the detect query resolves. `mcpInitialized` ensures we seed the default
  // exactly once (so a user's unchecks aren't clobbered by a refetch).
  const [selectedMCPTools, setSelectedMCPTools] = useState<Set<string>>(new Set());
  const [mcpInitialized, setMcpInitialized] = useState(false);
  const mcpDetect = useMCPToolsDetect(open && mode === "create");
  const mcpTools = mcpDetect.data?.tools ?? [];

  // Seed the default MCP selection from the detector's B+C defaults, once.
  useEffect(() => {
    if (mcpInitialized || !mcpDetect.data) return;
    setSelectedMCPTools(new Set(defaultSelectedIds(mcpDetect.data.tools)));
    setMcpInitialized(true);
  }, [mcpDetect.data, mcpInitialized]);

  // Server-side folder browser (#1529). `browseDir` is the directory currently
  // being listed; null defaults to the daemon's home dir. `null` while the
  // dialog is closed avoids a wasted fetch.
  const [browseDir, setBrowseDir] = useState<string | null>(null);
  const fs = useFsList(browseDir, open && step === "pick");

  const inspect = useScanInspect();
  const createFromScan = useCreateGroupFromScan();
  const scanRepos = useScanReposIntoGroup(groupId ?? "");
  // The target group slug for both the job poller and the per-repo/per-module
  // progress stream. In create mode it is the slug we just created.
  const targetGroup = mode === "create" ? slugify(name || scan?.suggestedGroup || "") : groupId;
  const job = useWizardJob(jobId, targetGroup);

  // #1527 — subscribe to the per-repo / per-MODULE progress stream once we're
  // on the Index step and have a group. For a monorepo this yields one row per
  // package; for a single repo, one row per repo.
  //
  // progressActive gates the subscription on the JOB being active (Index step +
  // a group) — NOT on `terminal`/`feedTerminal` — so a premature feed-terminal
  // can't tear the stream down before every repo reports (#5326).
  //
  // SUBSCRIBE-BEFORE-INDEX (#5340): also activate while the from-scan/scan POST
  // is in flight (createFromScan/scanRepos pending) — BEFORE `setStep("index")`.
  // The POST is what triggers the daemon to start indexing; opening the SSE
  // stream as soon as the POST fires means the broker is already listening when
  // the first per-repo extraction events arrive, so a fast index doesn't drop
  // them (which collapsed the feed to just the late group terminal → one row).
  const indexStarting = createFromScan.isPending || scanRepos.isPending;
  const progressActive = (step === "index" || indexStarting) && !!targetGroup;
  // How many repos THIS index registered — the same selection the wizard uses
  // to start indexing (see runIndex): one per selected child git repo, else one
  // per selected monorepo package, else 1 for a single repo. Threading this into
  // the feed lets feed-terminal wait for ALL repos instead of firing after the
  // first one finishes while the rest are still streaming (#5326).
  const expectedRepos = scan?.valid
    ? (scan.childGitRepos?.length ?? 0) > 0
      ? selectedChildren.size
      : (scan.packages?.length ?? 0) > 0
        ? selectedPkgs.size
        : 1
    : undefined;
  // The per-repo slugs this index will register — the SAME list runIndex POSTs.
  // Seeds one pending row per repo so ALL repos (backend + frontend) show up
  // front and survive any dropped early SSE events, instead of collapsing to a
  // single late group-terminal row (#5340).
  const seedSlugs = buildRepos(scan, selectedChildren, selectedPkgs).map((r) => r.slug ?? "");
  // Finalize seeded rows to Done only on SUCCESSFUL completion. The job poller's
  // "done" is the success signal; a row frozen on its last intermediate phase
  // (final SSE events arrived after the rebuild RPC returned) is then advanced
  // to Done so the rows agree with "Done · 100%" (#5348/#5340). Failures keep
  // their state (handled by leaving complete false on "failed").
  const indexComplete = job.data?.status === "done";
  const indexProgress = useIndexProgress(targetGroup, progressActive, expectedRepos, {
    seedSlugs,
    complete: indexComplete,
  });

  // Reset everything when the dialog closes.
  function reset() {
    setStep(mode === "create" ? "action" : "pick");
    setAction(mode === "add-repo" ? "add-group" : "single");
    setPath("");
    setScan(null);
    setName("");
    setJobId(null);
    setSelectedPkgs(new Set());
    setSelectedChildren(new Set());
    setSelectedMCPTools(new Set());
    setMcpInitialized(false);
    setBrowseDir(null);
    inspect.reset();
    createFromScan.reset();
    scanRepos.reset();
  }

  // Drive completion toasts off the job poller.
  useEffect(() => {
    if (!job.data) return;
    if (job.data.status === "done") {
      toast.success(job.data.message ?? "Indexing complete.");
      onIndexed?.(job.data.group);
    } else if (job.data.status === "failed") {
      toast.error(job.data.error ?? "Indexing failed.");
    }
  }, [job.data, onIndexed]);

  const pathDuplicate = path.trim() !== "" && existingPaths.includes(path.trim());
  const nameSlug = slugify(name || scan?.suggestedGroup || "");
  const nameDuplicate = takenNames.includes(nameSlug);

  // --- Step 1: pick directory ---
  // The folder browser's "Select this folder" target — the absolute path the
  // user has navigated INTO (fs.data.path), which is what gets indexed. Falls
  // back to the manual-paste field when the user typed a path instead.
  const browsePath = fs.data?.path ?? "";

  // The drive root (e.g. "C:\\") that the current browse path lives on, so the
  // Windows drive <select> reflects the active drive. null when at the drives
  // level or off Windows (no drives reported). Compared on the leading
  // "<letter>:" segment so any depth under C:\ maps back to the C:\ option.
  const currentDrive =
    fs.data?.drives?.find((d) => {
      const letter = d.path.slice(0, 2).toLowerCase(); // "c:"
      return browsePath.slice(0, 2).toLowerCase() === letter;
    })?.path ?? null;

  // runDetect scans an explicit absolute path. The folder browser passes the
  // directory the user navigated into; the manual field passes its trimmed
  // text. Either way a single value drives the Detect step — no typing needed
  // when browsing.
  async function runDetect(target: string) {
    const p = target.trim();
    if (p === "" || existingPaths.includes(p)) return;
    try {
      const result = await inspect.mutateAsync(p);
      setScan(result);
      if (result.valid) {
        // Reconcile the chosen action with what the shared classifier detected
        // (#5336). In add-repo mode the action is pinned to "add-group". In
        // create mode, if the detected smart default disagrees with the picked
        // action, prefer the detection so the Detect step shows the right
        // selection list (e.g. a container folder → group/child repos).
        if (mode === "create") {
          setAction(defaultActionFor(result));
        }
        setName((prev) => prev || result.suggestedGroup);
        // Default to ALL detected packages checked (#1531). Empty for a single
        // repo — then runIndex registers the whole folder as one repo.
        setSelectedPkgs(new Set(result.packages ?? []));
        // Default all child git repos checked (#1531 follow-up).
        setSelectedChildren(new Set(result.childGitRepos ?? []));
        setStep("detect");
      } else {
        toast.error(result.error ?? "That path can't be indexed.");
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Failed to scan path.");
    }
  }

  // proceedFromDetect decides what comes after the Detect step (#5344). In
  // create mode, when more than one MCP-capable tool is detected, show the MCP
  // picker; otherwise (add-repo mode, or ≤1 tool) skip straight to indexing,
  // auto-using the single detected tool's selection.
  function proceedFromDetect() {
    if (mode === "create" && shouldShowMCPStep(mcpTools)) {
      setStep("mcp");
      return;
    }
    // ≤1 tool (or add-repo mode): auto-use. In create mode a single tool is
    // pre-checked; add-repo mode leaves the selection undefined (back-compat).
    const sel = mode === "create" ? autoMCPSelection(mcpTools) : undefined;
    void runIndex(sel);
  }

  // --- Step 3: create/register + index ---
  // mcpToolsSel (#5344): the chosen MCP tool IDs to register (create mode);
  // undefined preserves back-compat (register every detected tool / add-repo).
  async function runIndex(mcpToolsSel?: string[]) {
    if (!scan?.valid) return;
    // Build the repo list based on what was detected (child git repos →
    // monorepo packages → single repo). buildRepos is shared with the progress
    // feed's row seeding so the seeded rows match the registered repos (#5340).
    const repos = buildRepos(scan, selectedChildren, selectedPkgs);
    if (repos.length === 0) {
      toast.error("Select at least one repo to index.");
      return;
    }
    try {
      if (mode === "create") {
        if (!nameSlug || nameDuplicate) return;
        const ack = await createFromScan.mutateAsync({ name: nameSlug, repos, mcpTools: mcpToolsSel });
        setJobId(ack.job_id);
      } else {
        const ack = await scanRepos.mutateAsync(repos);
        setJobId(ack.job_id);
      }
      setStep("index");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Failed to start indexing.");
    }
  }

  const indexing = createFromScan.isPending || scanRepos.isPending;
  const jobStatus = job.data?.status;
  const jobProgress = job.data?.progress ?? 0;
  // Terminal state has TWO sources of truth (#5326 bug 1). The job poller
  // (/api/v2/jobs/{id}) is primary, but the live freeze showed the rebuild
  // finishing (`rebuild: done` in the daemon log) while the job status stayed
  // "running" — leaving the button stuck on "Indexing…". The per-repo SSE feed
  // carries its own terminal done/error event (now reliably replayed by the
  // #5327 broker fix), so we also treat "every repo row done/error" as terminal.
  // Either source flipping is enough to reach "Done".
  const jobTerminal = jobStatus === "done" || jobStatus === "failed";
  const feedTerminal = indexProgress.terminal;
  const feedFailed = indexProgress.rows.some((r) => r.phase === "error");
  const terminal = jobTerminal || feedTerminal;
  // Effective display status drives the icon, label and bar. The job poller is
  // primary; if it hasn't flipped yet but the per-repo feed has reached terminal
  // (#5326 bug 1), surface done/failed so the UI matches the real state instead
  // of an indefinite "Indexing…".
  const effectiveStatus = jobTerminal
    ? jobStatus
    : feedTerminal
      ? feedFailed
        ? "failed"
        : "done"
      : jobStatus;

  // Main progress bar (#5332). The job-poller value (jobProgress) barely moves
  // during indexing, so the bar looked frozen near 0% even when repos were at
  // "Materializing"/"Indexed". Derive a real aggregate from the per-repo feed:
  // each repo contributes a phase-weighted fraction (advancing as it crosses
  // phase boundaries, even through sub-progress-less phases like Materializing).
  // Use the feed value when it has data; otherwise fall back to jobProgress.
  // Once terminal, pin to 100%.
  const aggregate = aggregateProgress(indexProgress.rows, expectedRepos);
  const barProgress = terminal
    ? 100
    : indexProgress.hasData
      ? aggregate
      : jobProgress;
  // Overall phase label from the least-advanced active repo, so the header
  // reflects what the index is actually doing instead of a static string.
  const phaseLabel = overallPhaseLabel(indexProgress.rows, terminal, indexProgress.groupPhase);
  // Subtle "alive" pulse while non-terminal — especially during the
  // sub-progress-less phases that would otherwise look frozen.
  const barActive = !terminal;

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        // Don't allow closing mid-index unless terminal.
        if (!v && step === "index" && !terminal) return;
        if (!v) reset();
        onOpenChange(v);
      }}
    >
      {/* Slightly larger than the default modal (#5326 bug 3): the index feed
          was scrolling cramped. One step wider (max-w-lg) + a capped height with
          internal scroll, still responsive on small screens. */}
      <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogTitle>
          {mode === "create" ? "Index a new group" : <>Add a repo to <span className="font-mono">{groupName}</span></>}
        </DialogTitle>
        <DialogDescription>
          {step === "action" && "What do you want to index?"}
          {step === "pick" && "Point Grafel at a repository folder on this machine."}
          {step === "detect" && "Review what we detected, then start indexing."}
          {step === "mcp" && "Choose which AI tools get the grafel MCP server."}
          {step === "index" && "Indexing in progress — you can leave this open."}
        </DialogDescription>

        {/* Stepper — the "action" step is only present in create mode. */}
        {(() => {
          const steps: { id: WizardStep; label: string }[] = [
            ...(mode === "create" ? [{ id: "action" as WizardStep, label: "Action" }] : []),
            { id: "pick", label: "Pick" },
            { id: "detect", label: "Detect" },
            // The MCP step only appears in create mode (#5344).
            ...(mode === "create" ? [{ id: "mcp" as WizardStep, label: "MCP" }] : []),
            { id: "index", label: "Index" },
          ];
          return (
            <ol className="mt-3 flex items-center gap-2 text-xs text-text-3" data-testid="wizard-stepper">
              {steps.map((s, i) => (
                <li key={s.id} className="flex items-center gap-2">
                  <span
                    className={cn(
                      "inline-flex size-5 items-center justify-center rounded-full border font-mono",
                      step === s.id
                        ? "border-accent bg-accent-soft text-accent-strong"
                        : "border-border text-text-4",
                    )}
                  >
                    {i + 1}
                  </span>
                  <span className={cn(step === s.id ? "text-text-2" : "text-text-4")}>{s.label}</span>
                  {i < steps.length - 1 && <span className="text-border">/</span>}
                </li>
              ))}
            </ol>
          );
        })()}

        {/* Step 0 — action select (create mode only): the four-action first step,
            parity with the CLI wizard (#5336). The picked action tunes the
            Detect step; the smart default is reconciled against detection once a
            folder is scanned. */}
        {step === "action" && (
          <div className="mt-4 space-y-2" data-testid="wizard-action">
            <ul className="space-y-2">
              {WIZARD_ACTIONS.map((a) => {
                const selected = action === a.value;
                return (
                  <li key={a.value}>
                    <button
                      type="button"
                      role="radio"
                      aria-checked={selected}
                      className={cn(
                        "flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left text-sm",
                        selected
                          ? "border-accent bg-accent-soft text-text-1"
                          : "border-border bg-surface text-text-2 hover:bg-surface-2",
                      )}
                      onClick={() => setAction(a.value)}
                      data-testid="wizard-action-option"
                      data-action={a.value}
                      data-selected={selected}
                    >
                      <span
                        className={cn(
                          "inline-flex size-4 shrink-0 items-center justify-center rounded-full border",
                          selected ? "border-accent-strong" : "border-text-4",
                        )}
                      >
                        {selected && <span className="size-2 rounded-full bg-accent-strong" />}
                      </span>
                      <span className="min-w-0 flex-1">{a.label}</span>
                    </button>
                  </li>
                );
              })}
            </ul>
            <div className="flex justify-between gap-2 pt-2">
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="button" onClick={() => setStep("pick")} data-testid="wizard-action-next">
                Next
                <ArrowRight size={13} />
              </Button>
            </div>
          </div>
        )}

        {/* Step 1 — pick directory: a server-side folder browser (#1529). The
            daemon lists ITS OWN filesystem so navigating to a folder and
            selecting it yields a real absolute path. No typing required. */}
        {step === "pick" && (
          <div className="mt-4 space-y-3" data-testid="wizard-fsbrowser">
            {/* Current directory + Up control */}
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={!fs.data?.parent}
                onClick={() => setBrowseDir(fs.data?.parent ?? null)}
                data-testid="wizard-fs-up"
                title="Up one level"
              >
                <CornerLeftUp size={13} />
              </Button>
              <code
                className="min-w-0 flex-1 truncate rounded-md border border-border bg-surface px-2 py-1.5 font-mono text-xs text-text-2"
                title={browsePath}
                data-testid="wizard-fs-cwd"
              >
                {fs.data?.isDrives ? "Drives" : browsePath || "…"}
              </code>

              {/* Windows drive selector — lets the user switch C:→D: directly
                  without first navigating up to the drives level (#5595). Only
                  rendered when the backend reports drives (Windows hosts). */}
              {fs.data?.drives && fs.data.drives.length > 0 && (
                <select
                  className="shrink-0 rounded-md border border-border bg-surface px-2 py-1.5 font-mono text-xs text-text-2"
                  value={currentDrive ?? ""}
                  onChange={(e) => setBrowseDir(e.target.value)}
                  data-testid="wizard-fs-drive"
                  title="Switch drive"
                >
                  {currentDrive === null && <option value="">Drive…</option>}
                  {fs.data.drives.map((d) => (
                    <option key={d.path} value={d.path}>
                      {d.name}
                    </option>
                  ))}
                </select>
              )}
            </div>

            {/* Shortcuts (home view only) */}
            {fs.data?.shortcuts && fs.data.shortcuts.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {fs.data.shortcuts.map((sc) => (
                  <Button
                    key={sc.path}
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-7 text-xs"
                    onClick={() => setBrowseDir(sc.path)}
                  >
                    {sc.label}
                  </Button>
                ))}
              </div>
            )}

            {/* Subfolder list */}
            <div
              className="max-h-64 overflow-y-auto rounded-lg border border-border bg-surface"
              data-testid="wizard-fs-list"
            >
              {fs.isLoading ? (
                <div className="flex items-center gap-2 p-3 text-sm text-text-3">
                  <Loader2 size={14} className="animate-spin" /> Loading…
                </div>
              ) : fs.data?.error ? (
                <div className="flex items-start gap-2 p-3 text-sm text-danger">
                  <AlertTriangle size={14} className="mt-0.5 shrink-0" />
                  <span>{fs.data.error}</span>
                </div>
              ) : (fs.data?.entries.length ?? 0) === 0 ? (
                <div className="p-3 text-sm text-text-4">No subfolders here.</div>
              ) : (
                <ul className="divide-y divide-border/60">
                  {fs.data!.entries.map((e) => (
                    <li key={e.path}>
                      <button
                        type="button"
                        className={cn(
                          "flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-2",
                          e.hidden ? "text-text-4" : "text-text-2",
                        )}
                        onClick={() => setBrowseDir(e.path)}
                        data-testid="wizard-fs-entry"
                        title={e.path}
                      >
                        <Folder size={14} className="shrink-0 text-text-3" />
                        <span className="min-w-0 flex-1 truncate font-mono">{e.name}</span>
                        <ChevronRight size={14} className="shrink-0 text-text-4" />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </div>

            {/* Select-this-folder: picking is sufficient to proceed. */}
            <div className="flex items-center justify-between gap-2">
              <p className="text-xs text-text-4">
                Navigate to a repo folder, then select it — no typing needed.
              </p>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                // shrink-0 + whitespace-nowrap: the flex parent was squeezing the
                // button so its label wrapped to two lines, and the wrapped text
                // overflowed the fixed-height border box — reading as a double
                // border. Keep it a single-line pill with one clean border (#1531).
                className="shrink-0 whitespace-nowrap"
                // The virtual drives level (Windows) is not a real folder, so
                // it cannot be selected/indexed — only its drive entries can.
                disabled={!browsePath || fs.data?.isDrives || inspect.isPending}
                onClick={() => {
                  setPath(browsePath);
                  void runDetect(browsePath);
                }}
                data-testid="wizard-fs-select"
              >
                {inspect.isPending ? <Loader2 size={13} className="animate-spin" /> : <Check size={13} />}
                Select this folder
              </Button>
            </div>

            {/* Manual-paste fallback. */}
            <form
              className="border-t border-border/60 pt-3"
              onSubmit={(e) => {
                e.preventDefault();
                void runDetect(path);
              }}
            >
              <label className="block">
                <span className="text-xs text-text-3">Or paste an absolute path</span>
                <div className="mt-1 flex gap-2">
                  <Input
                    value={path}
                    onChange={(e) => setPath(e.target.value)}
                    placeholder="/Users/you/code/my-repo"
                    className="flex-1 font-mono text-sm"
                    data-testid="wizard-path"
                  />
                  <Button
                    type="submit"
                    variant="ghost"
                    size="sm"
                    disabled={path.trim() === "" || pathDuplicate || inspect.isPending}
                    data-testid="wizard-scan"
                  >
                    Scan
                    <ArrowRight size={13} />
                  </Button>
                </div>
                {pathDuplicate && (
                  <p className="mt-1 text-xs text-danger">This path is already in the group.</p>
                )}
              </label>
            </form>

            <div className="flex justify-between pt-1">
              {mode === "create" ? (
                <Button type="button" variant="ghost" onClick={() => setStep("action")}>
                  Back
                </Button>
              ) : (
                <span />
              )}
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
            </div>
          </div>
        )}

        {/* Step 2 — detect preview */}
        {step === "detect" && scan && (
          <div className="mt-4 space-y-4">
            {!scan.valid ? (
              <div className="flex items-start gap-2 rounded-lg border border-danger/40 bg-danger-soft/40 p-3 text-sm text-danger">
                <AlertTriangle size={15} className="mt-0.5 shrink-0" />
                <span>{scan.error ?? "That path can't be indexed."}</span>
              </div>
            ) : (
              <>
                <dl className="rounded-lg border border-border bg-surface p-3 text-sm" data-testid="wizard-detect">
                  <div className="flex items-center justify-between py-1">
                    <dt className="text-text-3">Path</dt>
                    <dd className="font-mono text-text-2 truncate max-w-[60%]" title={scan.absPath}>
                      {scan.absPath}
                    </dd>
                  </div>
                  <div className="flex items-center justify-between py-1">
                    <dt className="text-text-3">Stack</dt>
                    <dd>
                      <Badge tone="accent">{scan.stack}</Badge>
                    </dd>
                  </div>
                  <div className="flex items-center justify-between py-1">
                    <dt className="text-text-3">Layout</dt>
                    <dd>
                      {(scan.childGitRepos?.length ?? 0) > 0 ? (
                        <Badge tone="info">
                          {scan.childGitRepos.length} repositor{scan.childGitRepos.length === 1 ? "y" : "ies"} detected
                        </Badge>
                      ) : scan.monorepo ? (
                        <Badge tone="info">
                          {scan.monorepo} monorepo · {scan.packages.length} package
                          {scan.packages.length === 1 ? "" : "s"}
                        </Badge>
                      ) : (
                        <Badge tone="neutral">single repo</Badge>
                      )}
                    </dd>
                  </div>
                  {scan.alreadyRegistered && (
                    <div className="flex items-center justify-between py-1">
                      <dt className="text-text-3">Note</dt>
                      <dd className="text-warning text-xs">
                        already in group “{scan.alreadyRegistered}”
                      </dd>
                    </div>
                  )}
                </dl>

                {/* Child git repos selection (#1531 follow-up): multi-repo-parent
                    pattern — pick which child repos to index. Each selected child
                    dir is registered as its own repo. Only shown when childGitRepos
                    is non-empty (takes precedence over monorepo packages). */}
                {(scan.childGitRepos?.length ?? 0) > 0 && (
                  <div data-testid="wizard-child-repos">
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-text-2">
                        Repositories to index
                        <span className="ml-1.5 text-xs text-text-4">
                          ({selectedChildren.size}/{scan.childGitRepos.length})
                        </span>
                      </span>
                      <div className="flex gap-2">
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="h-6 text-xs"
                          onClick={() => setSelectedChildren(new Set(scan.childGitRepos))}
                          data-testid="wizard-child-repos-all"
                        >
                          Select all
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="h-6 text-xs"
                          onClick={() => setSelectedChildren(new Set())}
                          data-testid="wizard-child-repos-none"
                        >
                          None
                        </Button>
                      </div>
                    </div>
                    <ul className="mt-1.5 max-h-48 overflow-y-auto rounded-lg border border-border bg-surface divide-y divide-border/60">
                      {scan.childGitRepos.map((child) => {
                        const checked = selectedChildren.has(child);
                        return (
                          <li key={child}>
                            <button
                              type="button"
                              role="checkbox"
                              aria-checked={checked}
                              className={cn(
                                "flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-2",
                                checked ? "text-text-2" : "text-text-4",
                              )}
                              onClick={() =>
                                setSelectedChildren((prev) => {
                                  const next = new Set(prev);
                                  if (next.has(child)) next.delete(child);
                                  else next.add(child);
                                  return next;
                                })
                              }
                              data-testid="wizard-child-repo"
                              data-child={child}
                              data-checked={checked}
                            >
                              {checked ? (
                                <CheckSquare size={15} className="shrink-0 text-accent-strong" />
                              ) : (
                                <Square size={15} className="shrink-0 text-text-4" />
                              )}
                              <span className="min-w-0 flex-1 truncate font-mono">{child}</span>
                            </button>
                          </li>
                        );
                      })}
                    </ul>
                    {selectedChildren.size === 0 && (
                      <p className="mt-1 text-xs text-danger">Select at least one repository to index.</p>
                    )}
                  </div>
                )}

                {/* Monorepo module selection (#1531): pick which package roots
                    to index. Default all-checked; only the SELECTED packages are
                    registered + indexed. */}
                {scan.packages.length > 0 && (
                  <div data-testid="wizard-modules">
                    <div className="flex items-center justify-between">
                      <span className="text-sm text-text-2">
                        Packages to index
                        <span className="ml-1.5 text-xs text-text-4">
                          ({selectedPkgs.size}/{scan.packages.length})
                        </span>
                      </span>
                      <div className="flex gap-2">
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="h-6 text-xs"
                          onClick={() => setSelectedPkgs(new Set(scan.packages))}
                          data-testid="wizard-modules-all"
                        >
                          Select all
                        </Button>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="h-6 text-xs"
                          onClick={() => setSelectedPkgs(new Set())}
                          data-testid="wizard-modules-none"
                        >
                          None
                        </Button>
                      </div>
                    </div>
                    <ul className="mt-1.5 max-h-48 overflow-y-auto rounded-lg border border-border bg-surface divide-y divide-border/60">
                      {scan.packages.map((pkg) => {
                        const checked = selectedPkgs.has(pkg);
                        return (
                          <li key={pkg}>
                            <button
                              type="button"
                              role="checkbox"
                              aria-checked={checked}
                              className={cn(
                                "flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-2",
                                checked ? "text-text-2" : "text-text-4",
                              )}
                              onClick={() =>
                                setSelectedPkgs((prev) => {
                                  const next = new Set(prev);
                                  if (next.has(pkg)) next.delete(pkg);
                                  else next.add(pkg);
                                  return next;
                                })
                              }
                              data-testid="wizard-module"
                              data-pkg={pkg}
                              data-checked={checked}
                            >
                              {checked ? (
                                <CheckSquare size={15} className="shrink-0 text-accent-strong" />
                              ) : (
                                <Square size={15} className="shrink-0 text-text-4" />
                              )}
                              <span className="min-w-0 flex-1 truncate font-mono">{pkg}</span>
                            </button>
                          </li>
                        );
                      })}
                    </ul>
                    {selectedPkgs.size === 0 && (
                      <p className="mt-1 text-xs text-danger">Select at least one package to index.</p>
                    )}
                  </div>
                )}

                {mode === "create" && (
                  <label className="block">
                    <span className="text-sm text-text-2">Group name</span>
                    <Input
                      value={name}
                      onChange={(e) => setName(e.target.value)}
                      placeholder={scan.suggestedGroup}
                      className="mt-1 font-mono text-sm"
                      data-testid="wizard-group-name"
                    />
                    {nameDuplicate && (
                      <p className="mt-1 text-xs text-danger">A group “{nameSlug}” already exists.</p>
                    )}
                  </label>
                )}
              </>
            )}

            <div className="flex justify-between gap-2 pt-1">
              <Button type="button" variant="ghost" onClick={() => setStep("pick")}>
                Back
              </Button>
              <Button
                type="button"
                disabled={
                  !scan.valid ||
                  indexing ||
                  ((scan.childGitRepos?.length ?? 0) > 0 && selectedChildren.size === 0) ||
                  (scan.packages.length > 0 && selectedPkgs.size === 0) ||
                  (mode === "create" && (!nameSlug || nameDuplicate))
                }
                onClick={() => (mode === "create" ? proceedFromDetect() : void runIndex())}
                data-testid="wizard-index"
              >
                {indexing ? <Loader2 size={13} className="animate-spin" /> : null}
                {/* In create mode the next step may be the MCP picker, so the
                    label is action-neutral; add-repo goes straight to indexing. */}
                {mode === "create"
                  ? shouldShowMCPStep(mcpTools)
                    ? "Next"
                    : "Create & index"
                  : "Add & index"}
              </Button>
            </div>
          </div>
        )}

        {/* Step 2b — choose which AI tools get the grafel MCP server (#5344).
            Create mode only; reached when >1 MCP-capable tool is detected. The
            defaults are the detector's smart (recently-used / previously-
            configured / remembered) selection. */}
        {step === "mcp" && (
          <div className="mt-4 space-y-4" data-testid="wizard-mcp">
            <p className="text-sm text-text-3">Your AI agents that can query this graph.</p>
            {mcpDetect.isLoading ? (
              <div className="flex items-center gap-2 p-3 text-sm text-text-3">
                <Loader2 size={14} className="animate-spin" /> Detecting tools…
              </div>
            ) : mcpTools.length === 0 ? (
              <p className="text-sm text-text-4">No AI tools detected on this machine.</p>
            ) : (
              <ul className="max-h-64 overflow-y-auto rounded-lg border border-border bg-surface divide-y divide-border/60">
                {mcpTools.map((t) => {
                  const checked = selectedMCPTools.has(t.id);
                  return (
                    <li key={t.id}>
                      <button
                        type="button"
                        role="checkbox"
                        aria-checked={checked}
                        className={cn(
                          "flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-surface-2",
                          checked ? "text-text-2" : "text-text-4",
                        )}
                        onClick={() =>
                          setSelectedMCPTools((prev) => {
                            const next = new Set(prev);
                            if (next.has(t.id)) next.delete(t.id);
                            else next.add(t.id);
                            return next;
                          })
                        }
                        data-testid="wizard-mcp-tool"
                        data-tool={t.id}
                        data-checked={checked}
                      >
                        {checked ? (
                          <CheckSquare size={15} className="shrink-0 text-accent-strong" />
                        ) : (
                          <Square size={15} className="shrink-0 text-text-4" />
                        )}
                        <span className="min-w-0 flex-1 truncate">{t.displayName}</span>
                        {t.hasGrafel && (
                          <Badge tone="neutral" className="shrink-0 text-[10px]">
                            configured
                          </Badge>
                        )}
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
            <div className="flex justify-between gap-2 pt-1">
              <Button type="button" variant="ghost" onClick={() => setStep("detect")}>
                Back
              </Button>
              <Button
                type="button"
                disabled={indexing}
                onClick={() => void runIndex([...selectedMCPTools])}
                data-testid="wizard-mcp-next"
              >
                {indexing ? <Loader2 size={13} className="animate-spin" /> : null}
                Create &amp; index
              </Button>
            </div>
          </div>
        )}

        {/* Step 3 — index progress */}
        {step === "index" && (
          <div className="mt-4 space-y-4" data-testid="wizard-progress">
            <div className="flex items-center gap-2">
              {effectiveStatus === "done" ? (
                <CheckCircle2 size={16} className="text-success" />
              ) : effectiveStatus === "failed" ? (
                <AlertTriangle size={16} className="text-danger" />
              ) : (
                <Loader2 size={16} className="animate-spin text-accent-strong" />
              )}
              <span className="text-sm text-text-2" data-testid="wizard-status">
                {effectiveStatus === "queued" && "Queued…"}
                {/* While running, surface the current overall phase (derived from
                    the least-advanced active repo) instead of a static string,
                    so "Materializing graph…" no longer reads as stuck (#5332). */}
                {effectiveStatus === "running" &&
                  (indexProgress.hasData ? phaseLabel : job.data?.message || "Indexing…")}
                {effectiveStatus === "done" && (job.data?.message || "Indexing complete.")}
                {effectiveStatus === "failed" && (job.data?.error || "Indexing failed.")}
                {!effectiveStatus && "Starting…"}
              </span>
            </div>

            <div className="h-2 w-full overflow-hidden rounded-full bg-surface-2">
              <div
                className={cn(
                  "h-full rounded-full transition-all duration-500",
                  effectiveStatus === "failed"
                    ? "bg-danger"
                    : effectiveStatus === "done"
                      ? "bg-success"
                      : "bg-accent",
                  // A tasteful shimmer keeps the bar visibly alive during the
                  // sub-progress-less phases (resolving/algorithms/materializing)
                  // that advance only at phase boundaries (#5332). Respects
                  // prefers-reduced-motion via the keyframe utility.
                  barActive && "wizard-bar-pulse",
                )}
                style={{ width: `${barProgress}%` }}
                data-testid="wizard-progress-bar"
                data-progress={barProgress}
              />
            </div>

            {/* Per-repo / per-MODULE rows (#1527). For a monorepo this shows
                one row per package; for a single repo, one row per repo. */}
            <IndexProgressFeed
              rows={indexProgress.rows}
              loading={!indexProgress.hasData && !terminal}
              className="max-h-80 overflow-y-auto pr-0.5"
            />

            <div className="flex justify-end gap-2 pt-1">
              <Button
                type="button"
                disabled={!terminal}
                onClick={() => {
                  reset();
                  onOpenChange(false);
                }}
                data-testid="wizard-done"
              >
                {terminal ? "Done" : "Indexing…"}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
