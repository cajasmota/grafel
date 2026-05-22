/* ============================================================
   ScanWizard — shared create-group / add-repo scan→detect→index wizard (#1517).

   Used by BOTH Landing (mode="create") and Settings (mode="add-repo").
   Three steps:

     1. Pick directory — a server-side PATH the daemon resolves + indexes.
        showDirectoryPicker() is offered only as a convenience to PREFILL the
        folder-name hint; the browser File System Access API yields an opaque
        FileSystemDirectoryHandle with no real on-disk path, so it cannot tell
        the daemon WHICH directory to index. The path text field is the source
        of truth. (See v2_wizard.go for the matching backend note.)

     2. Detect — POST /api/v2/scan/inspect previews the detected stack +
        monorepo layout + suggested group/slug. The user confirms.

     3. Index — POST /api/v2/groups/from-scan (create) or
        /api/v2/groups/{g}/repos/scan (add-repo) enqueues an async job; the
        wizard streams queued→running→done via the job poller (#1522 pattern).
   ============================================================ */

import { useEffect, useState } from "react";
import { CheckCircle2, FolderSearch, Loader2, AlertTriangle, ArrowRight } from "lucide-react";
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
} from "@/hooks/use-wizard";
import { useIndexProgress } from "@/hooks/use-index-progress";
import { IndexProgressFeed } from "@/components/chrome/index-progress-feed";
import { ApiError } from "@/lib/api";
import type { ScanInspectReply } from "@/data/types";
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

/** showDirectoryPicker is non-standard; narrow it without `any`. */
type DirPickerWindow = Window & {
  showDirectoryPicker?: () => Promise<{ name: string }>;
};

export function ScanWizard(props: ScanWizardProps) {
  const { open, onOpenChange, mode, groupId, groupName, takenNames = [], existingPaths = [], onIndexed } = props;

  const [step, setStep] = useState<WizardStep>("pick");
  const [path, setPath] = useState("");
  const [scan, setScan] = useState<ScanInspectReply | null>(null);
  const [name, setName] = useState("");
  const [jobId, setJobId] = useState<string | null>(null);

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
  async function browse() {
    const picker = (window as DirPickerWindow).showDirectoryPicker;
    if (!picker) {
      toast.info("Your browser can't open a folder picker — type or paste the path.");
      return;
    }
    try {
      const handle = await picker();
      // The handle has NO real on-disk path (browser sandbox). We can only use
      // its name as a hint; the user still confirms the absolute path below.
      if (handle?.name && path.trim() === "") {
        toast.info(`Picked “${handle.name}” — paste its full path to continue.`);
      }
    } catch {
      /* user cancelled — no-op */
    }
  }

  async function runDetect() {
    if (path.trim() === "" || pathDuplicate) return;
    try {
      const result = await inspect.mutateAsync(path.trim());
      setScan(result);
      if (result.valid) {
        setName((prev) => prev || result.suggestedGroup);
        setStep("detect");
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : "Failed to scan path.");
    }
  }

  // --- Step 3: create/register + index ---
  async function runIndex() {
    if (!scan?.valid) return;
    const repos = [{ path: scan.absPath, slug: scan.suggestedSlug }];
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
  const terminal = jobStatus === "done" || jobStatus === "failed";

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
      <DialogContent>
        <DialogTitle>
          {mode === "create" ? "Index a new group" : <>Add a repo to <span className="font-mono">{groupName}</span></>}
        </DialogTitle>
        <DialogDescription>
          {step === "pick" && "Point archigraph at a repository folder on this machine."}
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

        {/* Step 1 — pick directory */}
        {step === "pick" && (
          <form
            className="mt-4 space-y-3"
            onSubmit={(e) => {
              e.preventDefault();
              void runDetect();
            }}
          >
            <label className="block">
              <span className="text-sm text-text-2">Repository path</span>
              <div className="mt-1 flex gap-2">
                <Input
                  autoFocus
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                  placeholder="/Users/you/code/my-repo"
                  className="flex-1 font-mono text-sm"
                  data-testid="wizard-path"
                />
                <Button type="button" variant="secondary" size="sm" onClick={() => void browse()}>
                  <FolderSearch size={13} />
                  Browse…
                </Button>
              </div>
              {pathDuplicate && (
                <p className="mt-1 text-xs text-danger">This path is already in the group.</p>
              )}
            </label>
            <p className="text-xs text-text-4">
              archigraph indexes the folder on this machine — paste its absolute path. (Browsers
              can't hand a real path to the daemon, so the picker only prefills the name.)
            </p>
            <div className="flex justify-end gap-2 pt-1">
              <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={path.trim() === "" || pathDuplicate || inspect.isPending}
                data-testid="wizard-scan"
              >
                {inspect.isPending ? <Loader2 size={13} className="animate-spin" /> : null}
                Scan
                <ArrowRight size={13} />
              </Button>
            </div>
          </form>
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
                      {scan.monorepo ? (
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
                disabled={!scan.valid || indexing || (mode === "create" && (!nameSlug || nameDuplicate))}
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
              {jobStatus === "done" ? (
                <CheckCircle2 size={16} className="text-success" />
              ) : jobStatus === "failed" ? (
                <AlertTriangle size={16} className="text-danger" />
              ) : (
                <Loader2 size={16} className="animate-spin text-accent-strong" />
              )}
              <span className="text-sm text-text-2" data-testid="wizard-status">
                {jobStatus === "queued" && "Queued…"}
                {jobStatus === "running" && (job.data?.message || "Indexing…")}
                {jobStatus === "done" && (job.data?.message || "Indexing complete.")}
                {jobStatus === "failed" && (job.data?.error || "Indexing failed.")}
                {!jobStatus && "Starting…"}
              </span>
            </div>

            <div className="h-2 w-full overflow-hidden rounded-full bg-surface-2">
              <div
                className={cn(
                  "h-full rounded-full transition-all duration-500",
                  jobStatus === "failed" ? "bg-danger" : jobStatus === "done" ? "bg-success" : "bg-accent",
                )}
                style={{ width: `${jobStatus === "done" ? 100 : jobProgress}%` }}
              />
            </div>

            {/* Per-repo / per-MODULE rows (#1527). For a monorepo this shows
                one row per package; for a single repo, one row per repo. */}
            <IndexProgressFeed
              rows={indexProgress.rows}
              loading={!indexProgress.hasData && !terminal}
              className="max-h-64 overflow-y-auto pr-0.5"
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
