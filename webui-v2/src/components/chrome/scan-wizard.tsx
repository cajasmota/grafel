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
} from "@/hooks/use-wizard";
import { useIndexProgress } from "@/hooks/use-index-progress";
import { IndexProgressFeed } from "@/components/chrome/index-progress-feed";
import { ApiError } from "@/lib/api";
import type { ScanInspectReply, WizardRepo } from "@/data/types";
import { cn } from "@/lib/utils";

type WizardStep = "pick" | "detect" | "index";

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

export function ScanWizard(props: ScanWizardProps) {
  const { open, onOpenChange, mode, groupId, groupName, takenNames = [], existingPaths = [], onIndexed } = props;

  const [step, setStep] = useState<WizardStep>("pick");
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
  const progressActive = step === "index" && !!targetGroup;
  const indexProgress = useIndexProgress(targetGroup, progressActive);

  // Reset everything when the dialog closes.
  function reset() {
    setStep("pick");
    setPath("");
    setScan(null);
    setName("");
    setJobId(null);
    setSelectedPkgs(new Set());
    setSelectedChildren(new Set());
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

  // --- Step 3: create/register + index ---
  async function runIndex() {
    if (!scan?.valid) return;
    // Build the repo list based on what was detected:
    //   1. Child git repos (multi-repo-parent, #1531 follow-up): each selected
    //      child dir as its own repo (absolute sub-path).
    //   2. Monorepo packages (#1531): each selected package as its own repo.
    //   3. Single repo: the whole folder, as before.
    const hasChildGitRepos = (scan.childGitRepos?.length ?? 0) > 0;
    const isMonorepo = (scan.packages?.length ?? 0) > 0;
    let repos: WizardRepo[];
    if (hasChildGitRepos) {
      repos = [...selectedChildren].sort().map((child) => ({
        path: `${scan.absPath}/${child}`,
        slug: slugify(child),
      }));
    } else if (isMonorepo) {
      repos = [...selectedPkgs].sort().map((pkg) => ({
        path: `${scan.absPath}/${pkg}`,
        slug: slugify(`${scan.suggestedSlug}-${pkg}`),
        modules: [pkg],
      }));
    } else {
      repos = [{ path: scan.absPath, slug: scan.suggestedSlug }];
    }
    if (repos.length === 0) {
      toast.error("Select at least one repo to index.");
      return;
    }
    try {
      if (mode === "create") {
        if (!nameSlug || nameDuplicate) return;
        const ack = await createFromScan.mutateAsync({ name: nameSlug, repos });
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
          {step === "pick" && "Point Grafel at a repository folder on this machine."}
          {step === "detect" && "Review what we detected, then start indexing."}
          {step === "index" && "Indexing in progress — you can leave this open."}
        </DialogDescription>

        {/* Stepper */}
        <ol className="mt-3 flex items-center gap-2 text-xs text-text-3" data-testid="wizard-stepper">
          {(["pick", "detect", "index"] as WizardStep[]).map((s, i) => (
            <li key={s} className="flex items-center gap-2">
              <span
                className={cn(
                  "inline-flex size-5 items-center justify-center rounded-full border font-mono",
                  step === s
                    ? "border-accent bg-accent-soft text-accent-strong"
                    : "border-border text-text-4",
                )}
              >
                {i + 1}
              </span>
              <span className={cn(step === s ? "text-text-2" : "text-text-4")}>
                {s === "pick" ? "Pick" : s === "detect" ? "Detect" : "Index"}
              </span>
              {i < 2 && <span className="text-border">/</span>}
            </li>
          ))}
        </ol>

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
                {browsePath || "…"}
              </code>
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
                disabled={!browsePath || inspect.isPending}
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

            <div className="flex justify-end pt-1">
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
                onClick={() => void runIndex()}
                data-testid="wizard-index"
              >
                {indexing ? <Loader2 size={13} className="animate-spin" /> : null}
                {mode === "create" ? "Create & index" : "Add & index"}
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
                {effectiveStatus === "running" && (job.data?.message || "Indexing…")}
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
                )}
                style={{ width: `${effectiveStatus === "done" ? 100 : jobProgress}%` }}
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
