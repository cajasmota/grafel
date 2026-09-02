package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"
)

// Comparison bases. A grammar is compared release-to-release when both the pin
// and upstream name a release; a pseudo-version or branch pin has no release, so
// it falls back to commit dates. If neither comparison is possible the grammar is
// UNKNOWN — never assumed stale, never assumed current.
const (
	basisRelease = "release"
	basisDate    = "date"
)

// Result is one grammar's freshness verdict.
type Result struct {
	Language string
	Source   string
	// Bundled is the version this repo ACTUALLY pins, resolved per grammar from
	// go.mod or the vendored grammar's provenance header.
	Bundled Pin
	// UpstreamRelease / UpstreamCommitDate are the live upstream state.
	UpstreamRelease    string
	UpstreamCommitDate string
	UpstreamKind       string // "release" | "commit"
	// Basis records which comparison decided the verdict (basisRelease/basisDate).
	Basis string
	// Behind is the gap between the pinned commit and the upstream commit. It is
	// only meaningful on a date-basis comparison; a release-basis verdict reports
	// the version gap instead (see Gap).
	Behind time.Duration
	Stale  bool
	// Unknown marks a grammar whose bundled version could not be resolved, or
	// which cannot be compared to upstream. Reason says why.
	Unknown bool
	Reason  string
	// Err is set when the upstream lookup failed; the grammar is reported as
	// "unreachable" (skipped, not counted stale, not a hard failure on its own).
	Err error
}

// releaseGap scores how far a release-basis pin is behind upstream, so the
// report can order those rows by severity. It is 0 for any row without two
// comparable releases (a date-basis row is ordered by Behind instead).
func (r Result) releaseGap() int {
	if r.Basis != basisRelease || !r.Stale {
		return 0
	}
	a, aok := releaseParts(r.Bundled.Release)
	b, bok := releaseParts(r.UpstreamRelease)
	if !aok || !bok {
		return 0
	}
	weights := []int{1000000, 1000, 1}
	gap := 0
	for i, w := range weights {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		gap += (y - x) * w
	}
	if gap < 0 {
		return 0
	}
	return gap
}

// Gap renders the distance from the pin to upstream for humans.
func (r Result) Gap() string {
	if !r.Stale {
		return "-"
	}
	if r.Basis == basisRelease {
		return fmt.Sprintf("%s → %s", r.Bundled.Release, r.UpstreamRelease)
	}
	return fmt.Sprintf("~%d mo", months(r.Behind))
}

// Report is the full A2 result set.
type Report struct {
	LastVerified string
	CheckedAt    time.Time
	Grammars     []Result
	StaleCount   int
	Errored      int
	UnknownCount int
	// Unpinned lists lock languages with no resolvable bundled version, and
	// Unlocked lists grammar pins that no lock row claims. Both are drift the
	// tool exists to catch, so they are reported rather than reconciled away.
	Unpinned []string
	Unlocked []string
}

// CurrentCount is the number of grammars that are neither stale, unknown, nor
// unreachable. Every row falls in exactly one of the four buckets, so
// Stale+Current+Unknown+Errored == len(Grammars) — an invariant asserted by
// TestReport_SummaryCountsPartitionTheRows, because the summary line is the
// first number a maintainer reads and a constant-offset lie there is invisible
// to any before/after diff.
func (r Report) CurrentCount() int {
	return len(r.Grammars) - r.StaleCount - r.UnknownCount - r.Errored
}

// check fans out over every grammar in the lock, resolving each upstream and
// comparing it to that grammar's OWN pinned version. Errors on individual
// grammars are captured per-result (resilient) rather than aborting the run.
func check(ctx context.Context, lock *Lock, pins Pins, src UpstreamSource) Report {
	rep := Report{
		LastVerified: lock.LastVerified,
		CheckedAt:    time.Now().UTC(),
	}

	locked := map[string]bool{}
	for _, g := range lock.Grammars {
		locked[g.Source] = true
	}
	for _, s := range pins.Sources() {
		if !locked[s] {
			rep.Unlocked = append(rep.Unlocked, s)
		}
	}

	for _, g := range lock.Grammars {
		r := Result{Language: g.Language, Source: g.Source}

		pin, ok := pins.Get(g.Source)
		if !ok || !pin.resolved() {
			// The #6749 failure mode, refused: an unresolvable pin is UNKNOWN,
			// never silently defaulted to some constant date. The two cases are
			// distinguished because they need different fixes — one is a missing
			// dependency, the other a provenance header that records no version.
			r.Unknown = true
			switch {
			case ok && pin.RawRef != "":
				// Quote what the header SAYS. Claiming it "records no version"
				// when it records one we failed to read is the same class of
				// false assertion this whole issue is about.
				r.Bundled = pin
				r.Reason = fmt.Sprintf("%s records ref %q, in which no token parses as a release or a commit date",
					pin.Origin, pin.RawRef)
			case ok:
				r.Bundled = pin
				r.Reason = fmt.Sprintf("%s names %s but records no ref line", pin.Origin, g.Source)
			default:
				r.Reason = fmt.Sprintf("no pin for %s in go.mod or the vendored grammars", g.Source)
			}
			rep.UnknownCount++
			rep.Unpinned = append(rep.Unpinned, g.Language)
			rep.Grammars = append(rep.Grammars, r)
			continue
		}
		r.Bundled = pin

		up, err := src.Latest(ctx, g.Source)
		if err != nil {
			r.Err = err
			rep.Errored++
			rep.Grammars = append(rep.Grammars, r)
			continue
		}
		r.UpstreamRelease = up.Release
		r.UpstreamCommitDate = up.CommitDate
		r.UpstreamKind = up.Kind

		decide(&r)
		switch {
		case r.Unknown:
			rep.UnknownCount++
		case r.Stale:
			rep.StaleCount++
		}
		rep.Grammars = append(rep.Grammars, r)
	}

	// Stable order: stale first (biggest gap first), then current, then unknown,
	// then unreachable. "Biggest gap" spans both bases: a date-basis row is
	// ranked by elapsed time, a release-basis row by how many versions it is
	// behind. Without the latter every go.mod release pin would tie at zero
	// (they carry no date) and silently fall through to alphabetical order.
	sort.SliceStable(rep.Grammars, func(i, j int) bool {
		a, b := rep.Grammars[i], rep.Grammars[j]
		rank := func(r Result) int {
			switch {
			case r.Err != nil:
				return 3
			case r.Unknown:
				return 2
			case r.Stale:
				return 0
			default:
				return 1
			}
		}
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if a.Behind != b.Behind {
			return a.Behind > b.Behind
		}
		if ga, gb := a.releaseGap(), b.releaseGap(); ga != gb {
			return ga > gb
		}
		return a.Language < b.Language
	})
	sort.Strings(rep.Unpinned)
	sort.Strings(rep.Unlocked)
	return rep
}

// decide fills in Basis/Stale/Behind for one grammar whose pin and upstream are
// both known. Release-to-release is preferred when both sides name a release —
// a module version like v0.23.6 is a RELEASE, not a date, and comparing it to a
// commit date is what produced the wrong verdicts in #6749. Only a pin with no
// release (a pseudo-version or a branch) falls back to dates.
func decide(r *Result) {
	if r.Bundled.Release != "" && r.UpstreamRelease != "" {
		if cmp, ok := compareRelease(r.Bundled.Release, r.UpstreamRelease); ok {
			r.Basis = basisRelease
			r.Stale = cmp < 0
			if r.Stale {
				// A best-effort duration, only when both sides carry a date.
				bd, ud := parseDate(r.Bundled.Date), parseDate(r.UpstreamCommitDate)
				if !bd.IsZero() && !ud.IsZero() && ud.After(bd) {
					r.Behind = ud.Sub(bd)
				}
			}
			return
		}
	}

	bd, ud := parseDate(r.Bundled.Date), parseDate(r.UpstreamCommitDate)
	if !bd.IsZero() && !ud.IsZero() {
		r.Basis = basisDate
		if ud.After(bd) {
			r.Stale = true
			r.Behind = ud.Sub(bd)
		}
		return
	}

	r.Unknown = true
	r.Reason = fmt.Sprintf("cannot compare pin %q to upstream %q/%q",
		r.Bundled.String(), r.UpstreamRelease, r.UpstreamCommitDate)
}

func parseDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// months renders a duration as an approximate whole-month count for humans.
func months(d time.Duration) int {
	return int(d.Hours() / 24 / 30.4)
}

func upstreamOf(g Result) string {
	switch {
	case g.UpstreamRelease != "" && g.UpstreamCommitDate != "":
		return g.UpstreamRelease + " @ " + g.UpstreamCommitDate
	case g.UpstreamRelease != "":
		return g.UpstreamRelease
	case g.UpstreamCommitDate != "":
		return g.UpstreamCommitDate
	default:
		return "-"
	}
}

func writeTable(w io.Writer, r Report) {
	fmt.Fprintf(w, "grammar freshness (A2) — checked %s\n", r.CheckedAt.Format("2006-01-02"))
	fmt.Fprintf(w, "bundled versions read per grammar from go.mod + internal/treesitter/ts/grammars\n")
	fmt.Fprintf(w, "%d grammars: %d stale, %d current, %d unknown, %d unreachable\n\n",
		len(r.Grammars), r.StaleCount, r.CurrentCount(), r.UnknownCount, r.Errored)

	fmt.Fprintf(w, "%-12s %-10s %-22s %-24s %s\n", "LANGUAGE", "STATUS", "BUNDLED", "UPSTREAM", "DETAIL")
	for _, g := range r.Grammars {
		switch {
		case g.Err != nil:
			fmt.Fprintf(w, "%-12s %-10s %-22s %-24s %v\n",
				g.Language, "UNREACHABLE", g.Bundled.String(), "-", g.Err)
		case g.Unknown:
			fmt.Fprintf(w, "%-12s %-10s %-22s %-24s %s\n",
				g.Language, "UNKNOWN", g.Bundled.String(), upstreamOf(g), g.Reason)
		case g.Stale:
			fmt.Fprintf(w, "%-12s %-10s %-22s %-24s %s behind (by %s)\n",
				g.Language, "STALE", g.Bundled.String(), upstreamOf(g), g.Gap(), g.Basis)
		default:
			fmt.Fprintf(w, "%-12s %-10s %-22s %-24s up to date (by %s)\n",
				g.Language, "CURRENT", g.Bundled.String(), upstreamOf(g), g.Basis)
		}
	}

	if len(r.Unlocked) > 0 {
		fmt.Fprintf(w, "\npinned but absent from grammars.lock: %v\n", r.Unlocked)
	}
	if len(r.Unpinned) > 0 {
		fmt.Fprintf(w, "in grammars.lock with no resolvable bundled version: %v\n", r.Unpinned)
	}
}

// writeMarkdown renders the body for the tracking issue. It is deterministic
// (no timestamp churn beyond the checked date) so re-running on an unchanged
// repo produces a stable body — the workflow only edits the issue when content
// actually changes.
func writeMarkdown(w io.Writer, r Report) {
	fmt.Fprintf(w, "## Grammar freshness — %d of %d grammars stale\n\n", r.StaleCount, len(r.Grammars))
	fmt.Fprintf(w, "_Auto-generated by the `grammar-freshness` monthly cron (A2, epic #5359). Last checked **%s**._\n\n",
		r.CheckedAt.Format("2006-01-02"))
	fmt.Fprintf(w, "Each grammar's **bundled** version is read from where that grammar actually comes from — its `go.mod` require (applying `replace`), or the vendored provenance header in `internal/treesitter/ts/grammars/<lang>/`. Release pins are compared release-to-release; a pseudo-version or branch pin is compared by commit date. A grammar whose pin cannot be resolved is reported UNKNOWN, never assumed stale.\n\n")

	if r.StaleCount > 0 {
		fmt.Fprintf(w, "### Stale grammars\n\n")
		fmt.Fprintf(w, "| Language | Upstream repo | Bundled | Upstream latest | Behind | Basis |\n")
		fmt.Fprintf(w, "|---|---|---|---|---|---|\n")
		for _, g := range r.Grammars {
			if !g.Stale {
				continue
			}
			fmt.Fprintf(w, "| %s | [`%s`](https://github.com/%s) | `%s` | %s | %s | %s |\n",
				g.Language, g.Source, g.Source, g.Bundled.String(), upstreamOf(g), g.Gap(), g.Basis)
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintf(w, "All resolvable grammars are current against their upstream.\n\n")
	}

	if r.UnknownCount > 0 {
		fmt.Fprintf(w, "### Unknown (bundled version could not be resolved)\n\n")
		for _, g := range r.Grammars {
			if g.Unknown {
				fmt.Fprintf(w, "- `%s` (`%s`): %s\n", g.Language, g.Source, g.Reason)
			}
		}
		fmt.Fprintln(w)
	}

	if len(r.Unlocked) > 0 || len(r.Unpinned) > 0 {
		fmt.Fprintf(w, "### Lock / go.mod reconciliation\n\n")
		for _, s := range r.Unlocked {
			fmt.Fprintf(w, "- pinned but **absent from `grammars.lock`**: `%s`\n", s)
		}
		for _, l := range r.Unpinned {
			fmt.Fprintf(w, "- in `grammars.lock` with **no resolvable bundled version**: `%s`\n", l)
		}
		fmt.Fprintln(w)
	}

	if r.Errored > 0 {
		fmt.Fprintf(w, "### Unreachable (skipped, re-check next run)\n\n")
		for _, g := range r.Grammars {
			if g.Err != nil {
				fmt.Fprintf(w, "- `%s` (%s): %v\n", g.Language, g.Source, g.Err)
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "---\nPart of epic #5359 (milestone 0.1.4). See `docs/grammar-freshness-audit.md` and `grammars.lock`.\n")
}
