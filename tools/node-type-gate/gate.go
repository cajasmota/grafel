package main

import (
	"fmt"
	"sort"
	"strings"
)

// runtimeKinds are node kinds tree-sitter produces at parse time but does not
// list in any grammar's symbol table. A gate that does not whitelist these
// reports a false alarm on the first file that tests for a parse error.
var runtimeKinds = map[string]bool{
	"ERROR":   true,
	"MISSING": true,
}

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
}

// Evaluate applies the resolve rule to a derived surface.
func Evaluate(scan Scan, regs []Registration, grammars map[string]*Grammar, base *Baseline) Result {
	res := Result{
		Sites:          scan.Sites,
		Dynamic:        scan.Dynamic,
		Sinks:          scan.Sinks,
		Registrations:  regs,
		GrammarsForDir: map[string][]string{},
	}
	for _, dir := range pkgDirs(scan.Sites) {
		keys := grammarKeysFor(dir, regs, grammars)
		if len(keys) > 0 {
			res.GrammarsForDir[dir] = keys
			res.DirsWithGrammar = append(res.DirsWithGrammar, dir)
		}
	}
	sort.Strings(res.DirsWithGrammar)

	distinct := map[string]bool{}
	for _, s := range scan.Sites {
		keys := res.GrammarsForDir[s.Dir]
		if len(keys) == 0 {
			// The package never receives a tree-sitter tree, so its strings are
			// not node types by construction.
			continue
		}
		if s.Lit == "" || runtimeKinds[s.Lit] {
			// "" is a sentinel, never a node kind. ERROR/MISSING are runtime
			// kinds absent from every symbol table.
			continue
		}
		res.Resolved++
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
		if !m.Baselined {
			res.Failures = append(res.Failures, m)
		}
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
	return len(r.Failures) > 0 || len(r.StaleBaseline) > 0
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
