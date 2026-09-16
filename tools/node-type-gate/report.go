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
// Measured on the tree at the time of writing: 3081 resolved sites. The floor
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
	baselined := len(res.Misses) - len(res.Failures)
	fmt.Fprintf(w, "node-type-gate: %d misses, %d tolerated by the baseline, %d new\n",
		len(res.Misses), baselined, len(res.Failures))

	for _, m := range res.Failures {
		fmt.Fprintf(w, "::error file=%s,line=%d::%s\n", m.File, m.Line, m.String())
	}
	for _, e := range res.StaleBaseline {
		fmt.Fprintf(w, "::error::stale baseline row: %s %q in %s no longer matches anything. If the literal was fixed or moved, delete the row.\n",
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
	fmt.Fprintf(w, "  total      %6d sites / %d distinct literals / %d files / %d dynamic positions\n",
		len(res.Sites), len(distinct), len(files), len(res.Dynamic))

	fmt.Fprintln(w, "== grammar keys per package ==")
	for _, dir := range res.DirsWithGrammar {
		fmt.Fprintf(w, "  %-40s %s\n", dir, strings.Join(res.GrammarsForDir[dir], ","))
	}
	fmt.Fprintf(w, "  (%d packages, %d distinct grammar keys registered; %d grammars in the parser registry)\n",
		len(res.DirsWithGrammar), countKeys(res.GrammarsForDir), len(grammars))

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
# KNOWN GRANULARITY LIMIT: because the key ignores the line, one row tolerates
# every occurrence of that literal in that file. A SECOND dead occurrence of an
# already-listed literal in an already-listed file is therefore suppressed. Any
# other new dead literal — new name, or the same name in a different file or
# package — fails the gate. Per-line keying was rejected because an edit above
# an entry would turn CI red for no reason, and a gate that cries wolf gets
# switched off.
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
