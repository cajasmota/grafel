package main

// delta.go implements the "coverage delta" subcommand: computes the
// before→after diff of docs/coverage/registry.json between two git refs
// and emits a markdown report. Pure file I/O + os/exec git; no internal/*
// imports (standalone-tool rule).

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// deltaStatus is the rank used to decide whether a transition improves
// or regresses coverage. higher rank = more coverage.
var deltaRank = map[string]int{
	StatusMissing:       0,
	StatusNotApplicable: 0,
	StatusPartial:       1,
	StatusFull:          2,
}

// cellKey uniquely identifies a capability cell across all shapes.
type cellKey struct {
	RecordID string
	Group    string // "(flat)" for flat records, group name otherwise
	Key      string // capability key
}

// cellEntry pairs a key with its status and the language of its record.
type cellEntry struct {
	cellKey
	Status   string
	Language string
}

// cmdDelta is the "coverage delta" subcommand.
func cmdDelta(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("delta", flag.ContinueOnError)
	base := fs.String("base", "origin/main", "base git ref")
	head := fs.String("head", "", "head git ref (default: working tree)")
	lang := fs.String("lang", "", "restrict per-record table to this language slug")
	postPR := fs.String("post", "", "PR number: pipe report to `gh pr comment <PR#> --body-file -`")
	file := fs.String("file", defaultRegistryPath, "path to registry JSON (relative to repo root)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	baseReg, err := loadRegistryAtRef(*base, *file)
	if err != nil {
		return fmt.Errorf("delta: load base registry at %q: %w", *base, err)
	}

	var headReg *Registry
	if *head == "" {
		// working tree
		headReg, err = loadRegistry(*file)
		if err != nil {
			return fmt.Errorf("delta: load head registry (working tree): %w", err)
		}
	} else {
		headReg, err = loadRegistryAtRef(*head, *file)
		if err != nil {
			return fmt.Errorf("delta: load head registry at %q: %w", *head, err)
		}
	}

	headRef := "working tree"
	if *head != "" {
		headRef = *head
	}

	md := buildDeltaMarkdown(*base, headRef, *lang, baseReg, headReg)

	if _, err := fmt.Fprint(stdout, md); err != nil {
		return err
	}

	if *postPR != "" {
		if err := postPRComment(*postPR, md); err != nil {
			return fmt.Errorf("delta: post to PR #%s: %w", *postPR, err)
		}
		fmt.Fprintf(os.Stderr, "[posted to PR #%s]\n", *postPR)
	}
	return nil
}

// loadRegistryAtRef runs `git show <ref>:<file>` and decodes the result.
func loadRegistryAtRef(ref, filePath string) (*Registry, error) {
	arg := ref + ":" + filePath
	out, err := exec.Command("git", "show", arg).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			return nil, fmt.Errorf("git show %s: %s", arg, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("git show %s: %w", arg, err)
	}
	var reg Registry
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reg); err != nil {
		return nil, fmt.Errorf("decode registry at %s: %w", arg, err)
	}
	if reg.SchemaVersion == 0 {
		reg.SchemaVersion = SchemaVersion
	}
	return &reg, nil
}

// asExitError is a helper that avoids importing errors just for As.
func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// extractCells returns all capability cells from the registry as a map
// from cellKey → (status, language).
func extractCells(reg *Registry) map[cellKey]cellEntry {
	result := make(map[cellKey]cellEntry)
	for _, r := range reg.Records {
		rid := r.ID
		lang := r.Language
		// Flat capabilities
		for k, cap := range r.Capabilities {
			ck := cellKey{RecordID: rid, Group: "(flat)", Key: k}
			result[ck] = cellEntry{cellKey: ck, Status: cap.Status, Language: lang}
		}
		// Grouped capabilities
		for grp, inner := range r.Groups {
			for k, cap := range inner {
				ck := cellKey{RecordID: rid, Group: grp, Key: k}
				result[ck] = cellEntry{cellKey: ck, Status: cap.Status, Language: lang}
			}
		}
		// FrameworkSpecific capabilities
		for grp, inner := range r.FrameworkSpecific {
			for k, cap := range inner {
				ck := cellKey{RecordID: rid, Group: grp, Key: k}
				result[ck] = cellEntry{cellKey: ck, Status: cap.Status, Language: lang}
			}
		}
	}
	return result
}

// statusCounts tallies cells by status.
func statusCounts(cells map[cellKey]cellEntry) map[string]int {
	counts := map[string]int{}
	for _, e := range cells {
		counts[e.Status]++
	}
	return counts
}

// flipRecord describes a single cell that changed status between base and head.
type flipRecord struct {
	RecordID string
	Group    string
	Key      string
	Language string
	Before   string
	After    string
}

// computeFlips returns all cells whose status changed (including new cells
// not present in base, and cells removed from head).
func computeFlips(base, head map[cellKey]cellEntry) []flipRecord {
	var flips []flipRecord
	// Changed or added cells
	for ck, he := range head {
		be, inBase := base[ck]
		before := StatusMissing
		if inBase {
			before = be.Status
		}
		if before != he.Status {
			flips = append(flips, flipRecord{
				RecordID: ck.RecordID,
				Group:    ck.Group,
				Key:      ck.Key,
				Language: he.Language,
				Before:   before,
				After:    he.Status,
			})
		}
	}
	// Removed cells (exist in base but not head)
	for ck, be := range base {
		if _, inHead := head[ck]; !inHead {
			// Try to find language from base entry
			flips = append(flips, flipRecord{
				RecordID: ck.RecordID,
				Group:    ck.Group,
				Key:      ck.Key,
				Language: be.Language,
				Before:   be.Status,
				After:    "(removed)",
			})
		}
	}
	// Sort for deterministic output: by record, group, key
	sort.Slice(flips, func(i, j int) bool {
		if flips[i].RecordID != flips[j].RecordID {
			return flips[i].RecordID < flips[j].RecordID
		}
		if flips[i].Group != flips[j].Group {
			return flips[i].Group < flips[j].Group
		}
		return flips[i].Key < flips[j].Key
	})
	return flips
}

// transitionKey groups flips by (before, after) pair.
type transitionKey struct{ Before, After string }

// buildDeltaMarkdown constructs the markdown report string.
func buildDeltaMarkdown(baseRef, headRef, langFilter string, baseReg, headReg *Registry) string {
	baseCells := extractCells(baseReg)
	headCells := extractCells(headReg)

	baseCounts := statusCounts(baseCells)
	headCounts := statusCounts(headCells)

	flips := computeFlips(baseCells, headCells)

	// Tally improvements / regressions
	improved := 0
	regressed := 0
	transMap := map[transitionKey]int{}
	for _, f := range flips {
		tk := transitionKey{Before: f.Before, After: f.After}
		transMap[tk]++
		afterRank, afterKnown := deltaRank[f.After]
		beforeRank := deltaRank[f.Before]
		if afterKnown && afterRank > beforeRank {
			improved++
		} else if afterKnown && afterRank < beforeRank {
			regressed++
		}
	}

	// Sort transitions by count desc, then by key for stability
	type transEntry struct {
		Key   transitionKey
		Count int
	}
	var transitions []transEntry
	for k, n := range transMap {
		transitions = append(transitions, transEntry{Key: k, Count: n})
	}
	sort.Slice(transitions, func(i, j int) bool {
		if transitions[i].Count != transitions[j].Count {
			return transitions[i].Count > transitions[j].Count
		}
		if transitions[i].Key.Before != transitions[j].Key.Before {
			return transitions[i].Key.Before < transitions[j].Key.Before
		}
		return transitions[i].Key.After < transitions[j].Key.After
	})

	var sb strings.Builder
	wl := func(s string) { sb.WriteString(s); sb.WriteString("\n") }

	wl("## Coverage delta (this PR)")
	wl("")
	wl(fmt.Sprintf("_base_ `%s` → _head_ `%s`", baseRef, headRef))
	wl("")
	wl(fmt.Sprintf("**Cells changed: %d**  ·  improved (toward full): **%d**  ·  regressed: **%d**",
		len(flips), improved, regressed))
	wl("")
	wl("### Corpus totals")
	wl("| Status | Before | After | Δ |")
	wl("|---|---:|---:|---:|")
	for _, s := range []string{StatusFull, StatusPartial, StatusMissing, StatusNotApplicable} {
		b := baseCounts[s]
		h := headCounts[s]
		d := h - b
		var sign string
		switch {
		case d > 0:
			sign = fmt.Sprintf("+%d", d)
		case d < 0:
			sign = fmt.Sprintf("%d", d)
		default:
			sign = "±0"
		}
		wl(fmt.Sprintf("| %s | %d | %d | %s |", s, b, h, sign))
	}
	wl("")

	// Flipped-cell table (optionally filtered by language)
	visibleFlips := flips
	if langFilter != "" {
		filtered := flips[:0:0]
		for _, f := range flips {
			if f.Language == langFilter {
				filtered = append(filtered, f)
			}
		}
		visibleFlips = filtered
	}
	if len(visibleFlips) > 0 {
		wl("### Cells flipped by this PR")
		if langFilter != "" {
			wl(fmt.Sprintf("_(filtered to language: `%s`)_", langFilter))
			wl("")
		}
		wl("| Record | Group | Capability | Before → After |")
		wl("|---|---|---|---|")
		for _, f := range visibleFlips {
			wl(fmt.Sprintf("| `%s` | %s | %s | %s → **%s** |",
				f.RecordID, f.Group, f.Key, f.Before, f.After))
		}
		wl("")
	}

	if len(transitions) > 0 {
		wl("### Transitions")
		wl("| From → To | Count |")
		wl("|---|---:|")
		for _, te := range transitions {
			wl(fmt.Sprintf("| %s → %s | %d |", te.Key.Before, te.Key.After, te.Count))
		}
		wl("")
	}

	wl("### Assessment (agent fills — be honest, numbers above are computed)")
	wl("- **What changed / benefits:** <!-- TODO: what capability is now real; which queries/MCP tools improve -->")
	wl("- **How proven:** <!-- TODO: fixture(s) + test(s) that justify each flip to full -->")
	wl("- **Caveats:** <!-- TODO: partials left as partial and why; any heuristic limits -->")
	wl("- **Limits / not covered:** <!-- TODO: what's explicitly out of scope / deferred (+ follow-up issue #) -->")
	wl("- **Real-data check:** <!-- TODO: result on a real corpus if applicable, else 'unit-fixture only' -->")
	wl("")
	wl("<sub>generated by `coverage delta` — numbers computed from registry.json diff; prose by the PR author.</sub>")

	return sb.String()
}

// postPRComment pipes md to `gh pr comment <pr> --body-file -`.
func postPRComment(pr, md string) error {
	cmd := exec.Command("gh", "pr", "comment", pr, "--body-file", "-")
	cmd.Stdin = strings.NewReader(md)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
