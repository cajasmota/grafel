package main

import (
	"bytes"
	"fmt"
	"go/constant"
	"go/types"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/atomicfile"
)

// minResolvedSites is a count floor. It does not prove the gate works — a scan
// can read the right number of the wrong things — but it does catch the first
// and cheapest vacuity mode: the scan silently read nothing. The positive
// controls cover the other four modes (wrong files, wrong content, no
// detection, no action).
//
// Measured on the tree at the time of writing: 3322 resolved sites. The floor
// is set well below that so ordinary deletions do not trip it, and far above
// zero so a broken loader does.
const minResolvedSites = 1500

func printVerdict(w io.Writer, res Result) {
	fmt.Fprintf(w, "node-type-gate: %d node-type literal sites resolved (%d distinct pkg+literal) across %d packages with a grammar\n",
		res.Resolved, res.Distinct, len(res.DirsWithGrammar))
	if res.FloorBreached() {
		fmt.Fprintf(w, "::error::node-type-gate read only %d sites (floor %d). The scan is not looking at the tree; the verdict below means nothing.\n",
			res.Resolved, minResolvedSites)
	}
	// The skipped surface is printed on EVERY run, not only under -report: an
	// unchecked package that nobody can see is indistinguishable from one that
	// does not exist (#7076 review, finding 1).
	if len(res.Skipped) > 0 {
		fmt.Fprintf(w, "node-type-gate: %d sites in %d package(s) were NOT checked (no grammar was derived for them):\n",
			res.SkippedSites, len(res.Skipped))
		for _, sk := range res.Skipped {
			why := sk.Reason
			if why == "" {
				why = "NO EXEMPTION — this package is not in skipExemptions"
			}
			fmt.Fprintf(w, "  %-28s %4d sites — %s\n", sk.Dir, sk.Sites, why)
		}
	}
	for _, sk := range res.UnreviewedSkips {
		fmt.Fprintf(w, "::error::%s produced %d node-type literal sites but no grammar could be derived for it, and it has no entry in skipExemptions. Either derive its grammar (a constant language passed to treesitter.Parse is picked up automatically) or add an exemption saying why it cannot be mapped. A silently skipped package is the defect this gate exists to catch.\n",
			sk.Dir, sk.Sites)
	}

	// The alias surface, reported on EVERY run. These literals were invisible
	// to this scan AND to the `.Type() == "…"` grep offered as evidence of
	// completeness, until #7076 round 2 — two methods sharing one blind spot
	// are one confirmation, not two. Counting them is the point: a shape
	// nobody can see is indistinguishable from one that does not exist.
	fmt.Fprintf(w, "node-type-gate: %d of the CHECKED sites were reached through an alias (a local or parameter holding a node type, not a syntactic x.Type() call)\n",
		res.AliasSites)
	if len(res.AliasMisses) > 0 {
		verb := "REPORTED, not enforced"
		if enforceAliasForms {
			verb = "enforced"
		}
		fmt.Fprintf(w, "node-type-gate: %d alias-form dead literal(s) — %s (see enforceAliasForms):\n",
			len(res.AliasMisses), verb)
		for _, m := range res.AliasMisses {
			tag := ""
			if m.Baselined {
				tag = " [baselined]"
			}
			fmt.Fprintf(w, "  %s:%d %q in %s%s\n", m.File, m.Line, m.Lit, m.Dir, tag)
		}
	}

	// Reconcile every miss explicitly. "tolerated by the baseline" must not
	// absorb the alias findings: they are NOT baselined, they are un-enforced
	// pending their issues, and conflating the two would make a suppression and
	// a deferral look alike.
	baselined, deferred := 0, 0
	for _, m := range res.Misses {
		switch {
		case m.Baselined:
			baselined++
		case m.Alias() && !enforceAliasForms:
			deferred++
		}
	}
	fmt.Fprintf(w, "node-type-gate: %d misses = %d tolerated by the baseline + %d alias-form, reported not enforced + %d new\n",
		len(res.Misses), baselined, deferred, len(res.Failures))

	for _, m := range res.Failures {
		fmt.Fprintf(w, "::error file=%s,line=%d::%s\n", m.File, m.Line, m.String())
	}
	for _, e := range res.StaleBaseline {
		fmt.Fprintf(w, "::error::stale baseline row: %s %q in %s no longer matches anything. "+
			"If the literal was FIXED, delete the row. If the file or package was RENAMED or the literal MOVED, "+
			"rewrite this row's dir/path to the new location — deleting it would leave the literal unbaselined and "+
			"still failing. -update does NOT rewrite paths: it only refreshes the line number of a row whose "+
			"(dir, literal, file) key already matches.\n",
			e.Dir, e.Lit, e.File)
	}
	switch {
	case res.FloorBreached():
		fmt.Fprintln(w, "node-type-gate: FAIL (count floor)")
	case res.Failed():
		fmt.Fprintln(w, "node-type-gate: FAIL")
	default:
		fmt.Fprintln(w, "node-type-gate: OK")
	}
}

func printReport(w io.Writer, res Result, grammars map[string]*Grammar) {
	fmt.Fprintln(w, "== derived surface ==")
	byForm := map[string]int{}
	files := map[string]bool{}
	distinct := map[string]bool{}
	for _, s := range res.Sites {
		byForm[s.Form]++
		files[s.File] = true
		distinct[s.Lit] = true
	}
	forms := make([]string, 0, len(byForm))
	for f := range byForm {
		forms = append(forms, f)
	}
	sort.Strings(forms)
	for _, f := range forms {
		fmt.Fprintf(w, "  form %-10s %6d sites\n", f, byForm[f])
	}
	whitelisted := 0
	for _, s := range res.Sites {
		if len(res.GrammarsForDir[s.Dir]) == 0 {
			continue
		}
		if s.Lit == "" || runtimeKinds[s.Lit] {
			whitelisted++
		}
	}
	fmt.Fprintf(w, "  total      %6d sites / %d distinct literals / %d files / %d dynamic positions\n",
		len(res.Sites), len(distinct), len(files), len(res.Dynamic))
	// Make the three headline numbers add up for a reader: every derived site
	// is resolved, skipped for want of a grammar, or whitelisted.
	fmt.Fprintf(w, "  accounting %6d resolved + %d in packages with no grammar + %d whitelisted (ERROR/MISSING/\"\") = %d\n",
		res.Resolved, res.SkippedSites, whitelisted, res.Resolved+res.SkippedSites+whitelisted)

	// The two fixpoints' output. Printed because "the fixpoint found nothing"
	// and "the fixpoint found the wrong thing" are different bugs that look
	// identical from a miss count alone.
	fmt.Fprintf(w, "  helper surface %d sink position(s) (argument is a node-type literal) / %d source position(s) (parameter receives a node type)\n",
		len(res.Sinks), len(res.Sources))

	fmt.Fprintln(w, "== grammar keys per package ==")
	for _, dir := range res.DirsWithGrammar {
		fmt.Fprintf(w, "  %-40s %s\n", dir, strings.Join(res.GrammarsForDir[dir], ","))
	}
	fmt.Fprintf(w, "  (%d packages, %d distinct grammar keys registered; %d grammars in the parser registry)\n",
		len(res.DirsWithGrammar), countKeys(res.GrammarsForDir), len(grammars))

	if len(res.Skipped) > 0 {
		fmt.Fprintln(w, "== packages with sites but no derivable grammar ==")
		for _, sk := range res.Skipped {
			fmt.Fprintf(w, "  %-28s %4d sites\n", sk.Dir, sk.Sites)
		}
	}

	var dynamicRegs []Registration
	for _, r := range res.Registrations {
		if r.Key == "" {
			dynamicRegs = append(dynamicRegs, r)
		}
	}
	if len(dynamicRegs) > 0 {
		fmt.Fprintln(w, "== registrations with a non-constant language key (mapping not derivable) ==")
		for _, r := range dynamicRegs {
			fmt.Fprintf(w, "  %s:%d\n", r.File, r.Line)
		}
	}

	fmt.Fprintln(w, "== misses ==")
	for _, m := range res.Misses {
		tag := "NEW"
		if m.Baselined {
			tag = "baselined"
		}
		fmt.Fprintf(w, "  [%-9s] %s\n", tag, m.String())
	}
}

func countKeys(m map[string][]string) int {
	seen := map[string]bool{}
	for _, ks := range m {
		for _, k := range ks {
			seen[k] = true
		}
	}
	return len(seen)
}

const baselineHeader = `# node-type-gate baseline — literals that name no node in ANY grammar their
# package can receive, and that the gate therefore tolerates. Generated from the
# #7065 audit; see tools/node-type-gate/main.go for what the gate does and does
# not catch.
#
# Format:  class | package dir | literal | file:line | why it is tolerated
# Classes: unreachable-code           the dead name makes a code path unreachable
#          paired-with-correct-name   a live name in the same matcher does the work
#          not-a-node-type            the string is not a grammar node name at all
#          known-defect               dead AND behaviour-visible; the note names its issue
#
# The matching key is (dir, literal, file); the line number is documentation,
# refreshed by 'go run ./tools/node-type-gate -update'. A row that matches
# nothing FAILS the gate — that is what makes this list shrink. Fix a literal,
# then delete its row.
#
# KNOWN GRANULARITY LIMIT, quantified: because the key ignores the line, one row
# tolerates every occurrence of that literal in that file. 48 rows currently
# cover 64 sites, so 16 tolerated sites have no row of their own, and a SECOND
# dead occurrence of an already-listed literal in an already-listed file is
# suppressed. Any other new dead literal — new name, or the same name in a
# different file or package — fails the gate. Per-line keying was rejected
# because an edit above an entry would turn CI red for no reason, and a gate that
# cries wolf gets switched off. The ratio is the cost of that choice; if it grows
# much past 64/48, revisit it.
#
# A RENAME is the case to be careful with. Moving a file or package makes every
# row naming it go stale AND makes its literals newly unbaselined, so you get
# both error kinds at once. Rewrite the dir/path in each affected row — do NOT
# just delete the stale rows, and do not expect -update to do it: -update only
# refreshes the LINE of a row whose (dir, literal, file) key already matches.
#
# THIS IS NOT A PLACE TO PUT A NEW DEAD LITERAL. Every row here is a defect
# someone chose not to fix yet, each with its own follow-up.
`

func updateBaseline(path string, base *Baseline, res Result) error {
	// Refresh each row's line number from where the literal actually sits now.
	loc := map[baselineKey]int{}
	for _, m := range res.Misses {
		k := baselineKey{m.Dir, m.Lit, m.File}
		if _, ok := loc[k]; !ok {
			loc[k] = m.Line
		}
	}
	for i := range base.Entries {
		if l, ok := loc[base.Entries[i].key()]; ok {
			base.Entries[i].Line = l
		}
	}
	var buf bytes.Buffer
	if err := base.Format(&buf, baselineHeader); err != nil {
		return err
	}
	// atomicfile rather than a hand-rolled "<dest>.tmp" + rename: a
	// deterministic temp name is shared by every concurrent writer aiming at
	// the same destination, and #6018's guard test rejects it outright.
	return atomicfile.WriteFile(filepath.Clean(path), buf.Bytes(), 0o644)
}

func stringVal(tv types.TypeAndValue) (string, error) {
	if tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", fmt.Errorf("not a string constant")
	}
	return constant.StringVal(tv.Value), nil
}
