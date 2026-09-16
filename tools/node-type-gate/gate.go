package main

import (
	"fmt"
	"sort"
	"strings"
)

// enforceAliasForms decides whether an alias-form dead literal FAILS the gate
// or is only reported.
//
// It is false, and that is a dated, conditional state, not a design. The alias
// shapes (`t := n.Type(); t == "…"` and a parameter fed `n.Type()`) were
// invisible to this scan until #7076 round 2, and invisible to the
// `.Type() == "…"` grep that had been offered as evidence the surface was
// complete — two methods sharing one blind spot are one confirmation, not two.
// On the shape's first honest look it found 11 un-baselined dead literals in 7
// mapped packages. Those are real findings; they are being filed as their own
// issues.
//
// Flipping this to true is the whole point and should happen as soon as those
// findings have issue numbers and `known-defect` baseline rows. It is NOT a
// permanent two-tier gate: TestAliasEnforcementWouldFailOnExactlyTheKnownSet
// pins the exact set that flipping it would red, so this constant cannot
// quietly become a way to keep new dead literals green.
const enforceAliasForms = false

// runtimeKinds are node kinds tree-sitter produces at parse time but does not
// list in any grammar's symbol table. A gate that does not whitelist these
// reports a false alarm on the first file that tests for a parse error.
var runtimeKinds = map[string]bool{
	"ERROR":   true,
	"MISSING": true,
}

// skipExemptions is the CLOSED list of package directories that produce
// node-type literals and cannot be resolved against a grammar. Every entry
// needs a reason that is true of that package today.
//
// This list exists because of the #7076 review's first finding, and it is the
// gate's own version of the defect it was built to catch: internal/custom/kotlin
// held 11 kotlin literals, got no grammar key, and was skipped in SILENCE — a
// brand-new dead literal there exited 0 — under a comment asserting that such
// packages "never receive a tree-sitter tree", which was false for it. The
// mapping is now derived (see parseLangArg) and the remaining skips are named.
//
// A directory with sites that is NOT in this list FAILS the gate. That is the
// whole point: a skipped package that is counted and named is a reviewed
// decision; an invisible one is the thing this tool exists to eliminate.
var skipExemptions = map[string]string{
	"internal/engine": "parses under a language computed at runtime " +
		"(http_endpoint_client_ast.go and http_endpoint_client_constscope_6552.go " +
		"pass a tsLang variable), so it can receive ANY grammar. Measured, not " +
		"argued: mapping it to the java/kotlin/python it names as constants " +
		"produces 15 FALSE failures over 14 distinct names — arrow_function, " +
		"lexical_declaration, member_expression and 11 more JS/TS names that " +
		"are live under the grammars the runtime path reaches. Unmappable by " +
		"construction.",
	"internal/treesitter": "is the parser itself, not a consumer of one. Its " +
		"13 sites are the #6736 kotlin annotation repair, which runs inside " +
		"the generic Parse under an `if language == \"kotlin\"` guard rather " +
		"than through a parse call of its own. That guard binds the surface to " +
		"kotlin as tightly as a parse call would, so this is NOT \"no single " +
		"grammar can be derived\" — it is a special case the derivation does " +
		"not model and that is not yet worth modelling for one package. Named " +
		"and counted rather than argued away; if a second package ever needs " +
		"it, teach parseLangArg about the guard instead of adding a row here.",
}

// SkippedDir is one package that produced node-type literals the gate did not
// check, with the count of what went unchecked.
type SkippedDir struct {
	Dir    string
	Sites  int
	Reason string // "" when the directory has no exemption — a failure
}

// Exempt reports whether the skip was a reviewed decision rather than an
// oversight.
func (s SkippedDir) Exempt() bool { return s.Reason != "" }

// Miss is one literal that names no node in ANY grammar its package can
// receive. Note the "ANY": a literal that is absent from one of a multi-grammar
// package's grammars but present in another is NOT a miss. That class is real
// and large (C++-only names in the shared c/cpp package, TS-only names in the
// shared js/ts/tsx package); firing on it would make a third of the output
// wrong by construction.
type Miss struct {
	Site
	Grammars  []string // every grammar the package can receive
	Baselined bool
}

// Alias reports whether the literal was reached through a value holding a node
// type rather than a syntactic x.Type() call.
func (m Miss) Alias() bool { return m.Site.Alias }

func (m Miss) String() string {
	return fmt.Sprintf("%s:%d: node type %q resolves in none of package %s's grammars [%s] (form=%s)",
		m.File, m.Line, m.Lit, m.Dir, strings.Join(m.Grammars, ","), m.Form)
}

// Result is one gate run.
type Result struct {
	Sites          []Site
	Dynamic        []Site
	Sinks          []string // the discovered helper parameter positions
	Registrations  []Registration
	GrammarsForDir map[string][]string

	// Resolved counts the sites that were actually checked (package has at
	// least one grammar, literal is not whitelisted). This is the count floor:
	// if it collapses, the gate read nothing, whatever its verdict says.
	Resolved int
	Distinct int

	Misses          []Miss // every miss, baselined or not
	Failures        []Miss // the subset that is not baselined — these fail CI
	StaleBaseline   []BaselineEntry
	DirsWithGrammar []string

	// Skipped is every directory that produced sites the gate did not check.
	// It is always reported, never merely counted: an unchecked surface that
	// nobody can see is indistinguishable from one that does not exist.
	Skipped []SkippedDir
	// SkippedSites is the total number of literal sites in Skipped.
	SkippedSites int
	// UnreviewedSkips is the subset of Skipped with no exemption. Non-empty
	// fails the gate.
	UnreviewedSkips []SkippedDir

	// AliasSites counts the literals reached through an alias — a local or a
	// parameter that HOLDS a node type rather than a syntactic x.Type() call.
	// Before #7076 round 2 this whole shape was invisible to the scan AND to
	// the `.Type() == "…"` grep that was offered as evidence of completeness.
	AliasSites int
	// AliasMisses is the subset of Misses reached through an alias. Reported
	// separately because the shape is newly visible and its findings have not
	// been triaged; see enforceAliasForms.
	AliasMisses []Miss
}

// Evaluate applies the resolve rule to a derived surface.
func Evaluate(scan Scan, regs []Registration, binds []ParseBinding, grammars map[string]*Grammar, base *Baseline) Result {
	res := Result{
		Sites:          scan.Sites,
		Dynamic:        scan.Dynamic,
		Sinks:          scan.Sinks,
		Registrations:  regs,
		GrammarsForDir: map[string][]string{},
	}
	sitesPerDir := map[string]int{}
	for _, s := range scan.Sites {
		sitesPerDir[s.Dir]++
	}
	for _, dir := range pkgDirs(scan.Sites) {
		keys := grammarKeysFor(dir, regs, binds, grammars)
		if len(keys) > 0 {
			res.GrammarsForDir[dir] = keys
			res.DirsWithGrammar = append(res.DirsWithGrammar, dir)
			continue
		}
		sk := SkippedDir{Dir: dir, Sites: sitesPerDir[dir], Reason: skipExemptions[dir]}
		res.Skipped = append(res.Skipped, sk)
		res.SkippedSites += sk.Sites
		if !sk.Exempt() {
			res.UnreviewedSkips = append(res.UnreviewedSkips, sk)
		}
	}
	sort.Strings(res.DirsWithGrammar)
	sort.Slice(res.Skipped, func(i, j int) bool { return res.Skipped[i].Dir < res.Skipped[j].Dir })
	sort.Slice(res.UnreviewedSkips, func(i, j int) bool { return res.UnreviewedSkips[i].Dir < res.UnreviewedSkips[j].Dir })

	distinct := map[string]bool{}
	for _, s := range scan.Sites {
		keys := res.GrammarsForDir[s.Dir]
		if len(keys) == 0 {
			// Unresolvable: no grammar could be derived for this package. It is
			// NOT safe to assume the strings are therefore not node types —
			// internal/custom/kotlin's are, and an earlier version of this
			// comment said otherwise (#7076 review, finding 1). The skip is
			// accounted for in res.Skipped and must carry an exemption.
			continue
		}
		if s.Lit == "" || runtimeKinds[s.Lit] {
			// "" is a sentinel, never a node kind. ERROR/MISSING are runtime
			// kinds absent from every symbol table.
			continue
		}
		res.Resolved++
		if s.Alias {
			res.AliasSites++
		}
		distinct[s.Dir+"\x00"+s.Lit] = true
		found := false
		for _, k := range keys {
			if grammars[k].Kinds[s.Lit] {
				found = true
				break
			}
		}
		if found {
			continue
		}
		m := Miss{Site: s, Grammars: keys}
		if base.Match(s.Dir, s.Lit, s.File) {
			m.Baselined = true
		}
		res.Misses = append(res.Misses, m)
		if m.Alias() {
			res.AliasMisses = append(res.AliasMisses, m)
		}
		if m.Baselined {
			continue
		}
		// An alias-form miss fails only once the shape's first-run findings
		// have been triaged — see enforceAliasForms. Until then it is reported
		// in full, never suppressed.
		if m.Alias() && !enforceAliasForms {
			continue
		}
		res.Failures = append(res.Failures, m)
	}
	res.Distinct = len(distinct)
	res.StaleBaseline = base.Unmatched()
	return res
}

// Failed reports whether the gate is red on its own terms: an un-baselined
// miss, or a baseline row that nothing hits any more.
//
// It deliberately does NOT include the count floor, because a single-package
// run (which is how the positive controls drive this) resolves far too few
// sites to clear a whole-tree floor. The floor is applied by ExitCode, at the
// only place it is meaningful: a whole-tree run.
func (r Result) Failed() bool {
	return len(r.Failures) > 0 || len(r.StaleBaseline) > 0 || len(r.UnreviewedSkips) > 0
}

// FloorBreached reports whether the scan read so few sites that its verdict is
// not evidence of anything. Whole-tree runs only.
func (r Result) FloorBreached() bool { return r.Resolved < minResolvedSites }

// ExitCode is the whole-tree verdict: 1 when the gate is red OR when the scan
// did not actually look at the tree. A green verdict from a scan that read
// nothing is the failure mode this tool exists to distrust, so it must not
// exit 0.
func ExitCode(r Result) int {
	if r.Failed() || r.FloorBreached() {
		return 1
	}
	return 0
}
