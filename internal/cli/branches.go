package cli

// branches.go implements the `grafel branches` command (PH6 of epic
// #2087 / issue #2094).
//
// The command surfaces per-ref graph lifecycle state: tier (HOT/WARM/COLD/
// EXPIRED), idle time, on-disk size, and pin status across all registered
// groups. It also drives lifecycle operations: prune EXPIRED graphs, pin/unpin
// refs, and enforce a keep-last-N policy per repo.
//
// Safety invariants:
//   - Pinned refs (isPinnedMain OR user-pinned) are NEVER deleted.
//   - --prune-stale requires interactive confirmation unless CI=true or --force.
//   - --dry-run shows what would be deleted without deleting.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cajasmota/grafel/internal/daemon"
	"github.com/cajasmota/grafel/internal/daemon/tier"
	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/registry"
)

func newBranchesCmd() *cobra.Command {
	var (
		pruneStale bool
		pruneTTL   string
		pinRepo    string
		pinRef     string
		unpinRepo  string
		unpinRef   string
		keepLast   int
		keepRepo   string
		jsonOut    bool
		dryRun     bool
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "branches [group]",
		Short: "List and manage per-ref graph lifecycle state",
		Long: `Show all known graph refs across registered groups with tier, idle time,
on-disk size, and pin status.

Examples:
  grafel branches                       list all refs across groups
  grafel branches mygroup               filter to one group
  grafel branches --prune-stale         delete EXPIRED graphs (7d default)
  grafel branches --prune-stale 14d     custom TTL override (one-time)
  grafel branches --pin myrepo main     mark a ref as pinned (never expires)
  grafel branches --unpin myrepo main   un-pin
  grafel branches --keep-last 3 myrepo  keep only 3 newest feature branches
  grafel branches --json                machine-readable output`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			groupFilter := ""
			if len(args) == 1 {
				groupFilter = args[0]
			}

			pins, err := daemon.DefaultPinStore()
			if err != nil {
				return fmt.Errorf("pin store: %w", err)
			}
			if err := pins.Load(); err != nil {
				return fmt.Errorf("loading pins: %w", err)
			}

			// Dispatch sub-operations.
			if pinRepo != "" {
				return runBranchesPin(cmd.OutOrStdout(), pins, groupFilter, pinRepo, pinRef)
			}
			if unpinRepo != "" {
				return runBranchesUnpin(cmd.OutOrStdout(), pins, groupFilter, unpinRepo, unpinRef)
			}
			if keepRepo != "" && keepLast > 0 {
				return runBranchesKeepLast(cmd.OutOrStdout(), pins, groupFilter, keepRepo, keepLast, dryRun, force)
			}
			if pruneStale {
				return runBranchesPrune(cmd, pins, groupFilter, pruneTTL, dryRun, force)
			}
			return runBranchesList(cmd.OutOrStdout(), pins, groupFilter, jsonOut)
		},
	}

	cmd.Flags().BoolVar(&pruneStale, "prune-stale", false, "delete EXPIRED graph artifacts from disk")
	cmd.Flags().StringVar(&pruneTTL, "ttl", "", "custom TTL override for --prune-stale (e.g. 14d)")

	cmd.Flags().StringVar(&pinRepo, "pin", "", "repo slug to pin (use with positional <group> arg for group, second arg for ref)")
	cmd.Flags().StringVar(&pinRef, "pin-ref", "", "ref to pin (used with --pin)")
	cmd.Flags().StringVar(&unpinRepo, "unpin", "", "repo slug to un-pin")
	cmd.Flags().StringVar(&unpinRef, "unpin-ref", "", "ref to un-pin (used with --unpin)")
	cmd.Flags().IntVar(&keepLast, "keep-last", 0, "keep only N most-recent feature branches per repo")
	cmd.Flags().StringVar(&keepRepo, "keep-last-repo", "", "repo slug for --keep-last")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted without deleting")
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")

	return cmd
}

// ---------------------------------------------------------------------------
// refInfo — row data for list / JSON output
// ---------------------------------------------------------------------------

type refInfo struct {
	Group     string    `json:"group"`
	Repo      string    `json:"repo"`
	Ref       string    `json:"ref"`
	Tier      string    `json:"tier"`
	Idle      string    `json:"idle"`
	SizeBytes int64     `json:"size_bytes"`
	SizeFmt   string    `json:"size"`
	Pinned    bool      `json:"pinned"`
	PinReason string    `json:"pin_reason,omitempty"` // "main" | "user"
	StateDir  string    `json:"state_dir"`
	LastSeen  time.Time `json:"last_seen"`
}

// ---------------------------------------------------------------------------
// collectRefs walks the store directory and builds refInfo rows.
// ---------------------------------------------------------------------------

func collectRefs(pins *daemon.PinStore, groupFilter string) ([]refInfo, error) {
	groups, err := registry.Groups()
	if err != nil {
		return nil, err
	}

	storeRoot := daemon.StoreDir()
	var rows []refInfo

	for _, g := range groups {
		if groupFilter != "" && g.Name != groupFilter {
			continue
		}
		cfg, err := registry.LoadGroupConfig(g.ConfigPath)
		if err != nil {
			continue // skip misconfigured groups
		}
		for _, repo := range cfg.Repos {
			// Walk refs/ sub-tree under the repo's store slot.
			repoBase := repoBaseForSlug(storeRoot, repo)
			refsDir := filepath.Join(repoBase, "refs")
			entries, err := os.ReadDir(refsDir)
			if err != nil {
				// No refs directory yet — repo indexed but no per-ref store.
				continue
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				ref := daemon.RefSafeDecode(e.Name())
				stateDir := filepath.Join(refsDir, e.Name())

				// Determine size.
				sizeBytes := dirSizeCLI(stateDir)

				// Determine tier from disk (best-effort without a live daemon).
				tierStr, lastSeen := inferTierFromDisk(stateDir)

				// Determine idle.
				idle := idleSince(lastSeen)

				// Determine pin state.
				isPinnedMain := tier.IsDefaultBranch(repo.Path, ref)
				isUserPinned := pins.IsPinned(g.Name, repo.Slug, ref)
				pinReason := ""
				if isPinnedMain {
					pinReason = "main"
				} else if isUserPinned {
					pinReason = "user"
				}

				rows = append(rows, refInfo{
					Group:     g.Name,
					Repo:      repo.Slug,
					Ref:       ref,
					Tier:      tierStr,
					Idle:      idle,
					SizeBytes: sizeBytes,
					SizeFmt:   fmtBytes(sizeBytes),
					Pinned:    isPinnedMain || isUserPinned,
					PinReason: pinReason,
					StateDir:  stateDir,
					LastSeen:  lastSeen,
				})
			}
		}
	}

	// Sort by group, repo, ref for stable output.
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Group != rows[j].Group {
			return rows[i].Group < rows[j].Group
		}
		if rows[i].Repo != rows[j].Repo {
			return rows[i].Repo < rows[j].Repo
		}
		return rows[i].Ref < rows[j].Ref
	})

	return rows, nil
}

// repoBaseForSlug derives the store base directory for a registry.Repo.
// Mirrors daemon.repoSlug logic (which is package-private).
func repoBaseForSlug(storeRoot string, repo registry.Repo) string {
	// We need to produce the same slug as daemon.repoSlug(absRepoPath).
	// Use StateDirForRepoRef with a known ref to extract the parent.
	// StateDirForRepoRef returns <store>/<slug>-<hash>/refs/<ref-safe>/
	// so filepath.Dir(filepath.Dir(path)) == <store>/<slug>-<hash>/.
	ref := "main" // any ref works — we just need the base
	d := daemon.StateDirForRepoRef(repo.Path, ref)
	if d == "" {
		return filepath.Join(storeRoot, repo.Slug)
	}
	// d = <store>/<slug>-<hash>/refs/main  → go up two levels
	return filepath.Dir(filepath.Dir(d))
}

// inferTierFromDisk makes a best-effort tier determination from file mtimes
// without requiring a live daemon. It reads the active graph's mtime and maps
// it to HOT/WARM/COLD/EXPIRED using the default TTL windows.
//
// #5915 J2 slice-3: graph.CurrentGraphMtime is segment-set aware — the prior
// os.Stat over [graph.CurrentGraphPath(stateDir), graph.json] only ever named
// a flat .fb path, which is ABSENT for a segment-set ref (graph.<gen>/ dir +
// manifest.json, no flat .fb). That silently inferred mtime-zero (tier "cold"
// with a zero lastSeen) for a freshly-indexed segment-set ref.
func inferTierFromDisk(stateDir string) (tierStr string, lastSeen time.Time) {
	jsonPath := filepath.Join(stateDir, "graph.json")

	var mtime time.Time
	if mt, ok := graph.CurrentGraphMtime(stateDir); ok {
		mtime = mt
	}
	if fi, err := os.Stat(jsonPath); err == nil && fi.ModTime().After(mtime) {
		mtime = fi.ModTime()
	}

	if mtime.IsZero() {
		return "cold", time.Time{}
	}

	ttl := tier.DefaultTTLConfig()
	idle := time.Since(mtime)
	switch {
	case idle < ttl.HotWindow:
		return "hot", mtime
	case idle < ttl.ColdWindow:
		return "warm", mtime
	case idle < ttl.ExpiredWindow:
		return "cold", mtime
	default:
		return "expired", mtime
	}
}

// idleSince formats duration since t as a human-readable string.
func idleSince(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%.0fh", d.Hours())
	default:
		return fmt.Sprintf("%.0fd", d.Hours()/24)
	}
}

// dirSizeCLI computes the total byte size of all files under dir.
func dirSizeCLI(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			total += fi.Size()
		}
		return nil
	})
	return total
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func runBranchesList(w io.Writer, pins *daemon.PinStore, groupFilter string, jsonOut bool) error {
	rows, err := collectRefs(pins, groupFilter)
	if err != nil {
		return err
	}

	if len(rows) == 0 {
		fmt.Fprintln(w, "No graph refs found. Run `grafel index` to build graphs.")
		return nil
	}

	if jsonOut {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "GROUP\tREPO\tREF\tTIER\tIDLE\tSIZE\tPINNED")
	for _, r := range rows {
		pinned := "no"
		if r.Pinned {
			pinned = "yes (" + r.PinReason + ")"
		}
		ref := r.Ref
		if len(ref) > 24 {
			ref = ref[:21] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Group, r.Repo, ref, r.Tier, r.Idle, r.SizeFmt, pinned)
	}
	return tw.Flush()
}

// ---------------------------------------------------------------------------
// Prune
// ---------------------------------------------------------------------------

func runBranchesPrune(cmd *cobra.Command, pins *daemon.PinStore, groupFilter, ttlOverride string, dryRun, force bool) error {
	w := cmd.OutOrStdout()

	// Parse optional TTL override.
	expiredWindow := tier.DefaultTTLConfig().ExpiredWindow
	if ttlOverride != "" {
		d, err := parseDuration(ttlOverride)
		if err != nil {
			return fmt.Errorf("invalid TTL %q: %w", ttlOverride, err)
		}
		expiredWindow = d
	}

	rows, err := collectRefs(pins, groupFilter)
	if err != nil {
		return err
	}

	// Find candidates: EXPIRED and not pinned.
	var candidates []refInfo
	var totalBytes int64
	for _, r := range rows {
		if r.Pinned {
			continue
		}
		// Use TTL override if provided.
		if ttlOverride != "" {
			if !r.LastSeen.IsZero() && time.Since(r.LastSeen) >= expiredWindow {
				candidates = append(candidates, r)
				totalBytes += r.SizeBytes
			}
		} else if r.Tier == "expired" {
			candidates = append(candidates, r)
			totalBytes += r.SizeBytes
		}
	}

	if len(candidates) == 0 {
		fmt.Fprintln(w, "Nothing to prune — no expired unpinned refs found.")
		return nil
	}

	fmt.Fprintf(w, "Would delete %d ref(s) totalling %s:\n", len(candidates), fmtBytes(totalBytes))
	for _, r := range candidates {
		fmt.Fprintf(w, "  %s/%s  %s  (idle %s, %s)\n", r.Group, r.Repo, r.Ref, r.Idle, r.SizeFmt)
	}

	if dryRun {
		fmt.Fprintln(w, "\nDry run — nothing deleted. Remove --dry-run to execute.")
		return nil
	}

	// Require confirmation unless CI=true or --force.
	if !force && os.Getenv("CI") != "true" {
		fmt.Fprintf(w, "\nDelete %d ref(s) (%s total)? [y/N] ", len(candidates), fmtBytes(totalBytes))
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(w, "aborted")
			return nil
		}
	}

	var freedTotal int64
	var deleted, failed int
	for _, r := range candidates {
		n := dirSizeCLI(r.StateDir)
		if err := os.RemoveAll(r.StateDir); err != nil {
			fmt.Fprintf(w, "  FAIL %s/%s %s: %v\n", r.Group, r.Repo, r.Ref, err)
			failed++
			continue
		}
		freedTotal += n
		deleted++
		fmt.Fprintf(w, "  deleted %s/%s %s (%s)\n", r.Group, r.Repo, r.Ref, fmtBytes(n))
	}
	fmt.Fprintf(w, "\nDeleted %d ref(s), freed %s", deleted, fmtBytes(freedTotal))
	if failed > 0 {
		fmt.Fprintf(w, " (%d failed)", failed)
	}
	fmt.Fprintln(w)
	return nil
}

// parseDuration parses "14d", "7d", "24h", "30m" style durations.
func parseDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n := strings.TrimSuffix(s, "d")
		days := 0
		if _, err := fmt.Sscanf(n, "%d", &days); err != nil || days <= 0 {
			return 0, fmt.Errorf("expected positive integer before 'd'")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// ---------------------------------------------------------------------------
// Pin / Unpin
// ---------------------------------------------------------------------------

func runBranchesPin(w io.Writer, pins *daemon.PinStore, group, repo, ref string) error {
	if group == "" {
		return fmt.Errorf("group required: pass as positional argument or first arg")
	}
	if ref == "" {
		return fmt.Errorf("--pin-ref required when using --pin")
	}
	if err := pins.Pin(group, repo, ref); err != nil {
		return fmt.Errorf("pin: %w", err)
	}
	fmt.Fprintf(w, "pinned %s/%s %s\n", group, repo, ref)
	return nil
}

func runBranchesUnpin(w io.Writer, pins *daemon.PinStore, group, repo, ref string) error {
	if group == "" {
		return fmt.Errorf("group required: pass as positional argument or first arg")
	}
	if ref == "" {
		return fmt.Errorf("--unpin-ref required when using --unpin")
	}
	if err := pins.Unpin(group, repo, ref); err != nil {
		return fmt.Errorf("unpin: %w", err)
	}
	fmt.Fprintf(w, "unpinned %s/%s %s\n", group, repo, ref)
	return nil
}

// ---------------------------------------------------------------------------
// Keep-last
// ---------------------------------------------------------------------------

func runBranchesKeepLast(w io.Writer, pins *daemon.PinStore, group, repo string, n int, dryRun, force bool) error {
	if group == "" {
		return fmt.Errorf("group required: pass as positional argument or first arg")
	}
	rows, err := collectRefs(pins, group)
	if err != nil {
		return err
	}

	// Filter to the target repo, non-pinned feature branches.
	var candidates []refInfo
	for _, r := range rows {
		if r.Repo != repo {
			continue
		}
		if r.Pinned {
			continue
		}
		candidates = append(candidates, r)
	}

	if len(candidates) <= n {
		fmt.Fprintf(w, "%s/%s has %d eligible refs (≤ keep-last %d) — nothing to do\n", group, repo, len(candidates), n)
		return nil
	}

	// Sort by LastSeen descending (newest first) then keep first n.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].LastSeen.After(candidates[j].LastSeen)
	})
	keep := candidates[:n]
	drop := candidates[n:]

	fmt.Fprintf(w, "Keeping %d most-recent refs for %s/%s, dropping %d:\n", len(keep), group, repo, len(drop))
	for _, r := range keep {
		fmt.Fprintf(w, "  KEEP  %s  (idle %s)\n", r.Ref, r.Idle)
	}
	for _, r := range drop {
		fmt.Fprintf(w, "  DROP  %s  (idle %s)\n", r.Ref, r.Idle)
	}

	if dryRun {
		fmt.Fprintln(w, "\nDry run — nothing deleted.")
		return nil
	}
	if !force && os.Getenv("CI") != "true" {
		fmt.Fprintf(w, "\nDrop %d ref(s)? [y/N] ", len(drop))
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(w, "aborted")
			return nil
		}
	}

	var freed int64
	for _, r := range drop {
		n := dirSizeCLI(r.StateDir)
		if err := os.RemoveAll(r.StateDir); err != nil {
			fmt.Fprintf(w, "  FAIL %s: %v\n", r.Ref, err)
			continue
		}
		freed += n
		fmt.Fprintf(w, "  deleted %s (%s)\n", r.Ref, fmtBytes(n))
	}
	fmt.Fprintf(w, "freed %s\n", fmtBytes(freed))
	return nil
}
