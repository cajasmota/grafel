package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// The baseline is the checked-in list of literals that are known to name no
// node in any of their package's grammars, and that the gate therefore tolerates.
//
// It is a text file, one entry per line, fields separated by " | ":
//
//	class | package dir | literal | file:line | note
//
// The format is deliberately flat and greppable: `grep unreachable-code`,
// `grep internal/extractors/lua`, `grep for_each_statement` all work, and a
// reviewer reads the whole justification on one line. A baseline nobody can
// read is a mute button.
//
// MATCHING KEY is (dir, literal, file) — NOT the line number. A line number
// goes stale on the next edit above it, and a gate that fails on unrelated
// churn gets suppressed. The line is documentation, refreshed by -update.
//
// A baseline entry that matches nothing is itself a failure. That is what makes
// the list shrink: fix the literal, and CI tells you to delete its row.

// baselineClasses are the only classes an entry may carry. A free-text class
// would let "misc" swallow the taxonomy.
var baselineClasses = map[string]string{
	// The name is dead AND nothing reaches the code holding it, so there is no
	// behaviour today. Still a defect: unreachability is a property of the
	// current dispatch, not a guarantee.
	"unreachable-code": "dead name in code nothing reaches",
	// A live name in the SAME matcher does the work, so deleting the dead
	// literal would change nothing. Cleanup, not a bug. (The dead name need not
	// be a misspelling of the live one — an ESTree name or a name borrowed from
	// another language's grammar sitting in a live multi-name case is the same
	// situation.)
	"paired-with-correct-name": "dead name alongside a live one in the same matcher",
	// Dead AND behaviour-visible: nothing else covers the path. Each of these
	// is a real bug with its own issue, deliberately not fixed here so it can
	// be graded on its own. The note MUST name the issue.
	"known-defect": "dead name that changes behaviour; tracked by its own issue",
	// The string reached a node-type position but is not a node type at all
	// (a sentinel, a field name, an attribute). A scanner false positive.
	"not-a-node-type": "not a grammar node name; the scanner reached it in error",
}

// BaselineEntry is one tolerated literal.
type BaselineEntry struct {
	Class string
	Dir   string
	Lit   string
	File  string
	Line  int
	Note  string

	matched bool
}

type baselineKey struct {
	Dir  string
	Lit  string
	File string
}

func (e BaselineEntry) key() baselineKey { return baselineKey{e.Dir, e.Lit, e.File} }

// Baseline is the parsed file, in file order.
type Baseline struct {
	Entries []BaselineEntry
	byKey   map[baselineKey]int
}

// ParseBaseline reads the flat format. Blank lines and lines whose first
// non-space character is '#' are comments.
func ParseBaseline(r io.Reader) (*Baseline, error) {
	b := &Baseline{byKey: map[baselineKey]int{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Split(raw, "|")
		if len(fields) < 4 {
			return nil, fmt.Errorf("baseline line %d: want at least 4 '|'-separated fields (class | dir | literal | file:line | note), got %d: %q", lineNo, len(fields), raw)
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		e := BaselineEntry{Class: fields[0], Dir: fields[1], Lit: fields[2]}
		if _, ok := baselineClasses[e.Class]; !ok {
			return nil, fmt.Errorf("baseline line %d: unknown class %q (allowed: %s)", lineNo, e.Class, strings.Join(sortedClasses(), ", "))
		}
		if e.Dir == "" || e.Lit == "" {
			return nil, fmt.Errorf("baseline line %d: dir and literal must be non-empty: %q", lineNo, raw)
		}
		loc := fields[3]
		if i := strings.LastIndex(loc, ":"); i >= 0 {
			e.File = loc[:i]
			n, err := strconv.Atoi(loc[i+1:])
			if err != nil {
				return nil, fmt.Errorf("baseline line %d: %q is not file:line", lineNo, loc)
			}
			e.Line = n
		} else {
			return nil, fmt.Errorf("baseline line %d: %q is not file:line", lineNo, loc)
		}
		if len(fields) > 4 {
			e.Note = strings.TrimSpace(strings.Join(fields[4:], "|"))
		}
		if e.Note == "" {
			return nil, fmt.Errorf("baseline line %d: every entry needs a note saying why it is tolerated: %q", lineNo, raw)
		}
		// known-defect is the only class that tolerates a REAL bug. It is
		// acceptable only while each row hands off to a tracked fix, so the
		// rule is enforced here rather than by an on-disk test — a check that
		// lives only in a test over the current file grades nothing on the day
		// the file has no such row, which is exactly when someone adds one.
		if e.Class == "known-defect" && !strings.Contains(e.Note, "#") {
			return nil, fmt.Errorf("baseline line %d: a known-defect row must name its issue (e.g. #7068) in the note: %q", lineNo, e.Note)
		}
		if _, dup := b.byKey[e.key()]; dup {
			return nil, fmt.Errorf("baseline line %d: duplicate entry for %s %q in %s", lineNo, e.Dir, e.Lit, e.File)
		}
		b.byKey[e.key()] = len(b.Entries)
		b.Entries = append(b.Entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return b, nil
}

func sortedClasses() []string {
	out := make([]string, 0, len(baselineClasses))
	for k := range baselineClasses {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Match marks the entry covering (dir, lit, file) as used and reports whether
// one exists.
func (b *Baseline) Match(dir, lit, file string) bool {
	if b == nil {
		return false
	}
	i, ok := b.byKey[baselineKey{dir, lit, file}]
	if !ok {
		return false
	}
	b.Entries[i].matched = true
	return true
}

// Unmatched returns the entries nothing in the current tree hit. A non-empty
// result is a gate failure: the literal was fixed or moved, and its row must go.
func (b *Baseline) Unmatched() []BaselineEntry {
	if b == nil {
		return nil
	}
	var out []BaselineEntry
	for _, e := range b.Entries {
		if !e.matched {
			out = append(out, e)
		}
	}
	return out
}

// Format renders the baseline back out, column-aligned, in a stable order.
func (b *Baseline) Format(w io.Writer, header string) error {
	entries := append([]BaselineEntry(nil), b.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		a, c := entries[i], entries[j]
		if a.Dir != c.Dir {
			return a.Dir < c.Dir
		}
		if a.File != c.File {
			return a.File < c.File
		}
		if a.Line != c.Line {
			return a.Line < c.Line
		}
		return a.Lit < c.Lit
	})
	var wClass, wDir, wLit, wLoc int
	locs := make([]string, len(entries))
	for i, e := range entries {
		locs[i] = fmt.Sprintf("%s:%d", e.File, e.Line)
		wClass = max(wClass, len(e.Class))
		wDir = max(wDir, len(e.Dir))
		wLit = max(wLit, len(e.Lit))
		wLoc = max(wLoc, len(locs[i]))
	}
	if _, err := io.WriteString(w, header); err != nil {
		return err
	}
	for i, e := range entries {
		line := fmt.Sprintf("%-*s | %-*s | %-*s | %-*s | %s\n",
			wClass, e.Class, wDir, e.Dir, wLit, e.Lit, wLoc, locs[i], e.Note)
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
