package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
)

// regroupMove is a single flat→grouped relocation the regroup pass would
// perform: capability key Key on record RecordID moves from the flat
// capabilities map into canonical group Group. Slices are sorted
// deterministically before any output or write so dry-run reports and
// registry writes are byte-stable across runs.
type regroupMove struct {
	RecordID string
	Language string
	Group    string
	Key      string
}

// planRegroup computes the set of flat capability cells that should be
// relocated into their canonical group. A record is in scope when:
//
//   - it carries a subcategory that is valid for its category, and
//   - that subcategory declares a group taxonomy, and
//   - the record still holds FLAT cells (len(Capabilities) > 0).
//
// Records already in the grouped shape are skipped (idempotency): a
// second run finds no flat cells and plans nothing. The cell payload
// (status/cites/verified_at/verified_sha/issue/notes) is preserved
// verbatim by applyRegroup — planRegroup only records placement. A key
// whose canonical owner is "" (not declared in any group) is parked in
// the synthetic Uncategorized group so no data is lost.
//
// subFilter, when non-empty, scopes the plan to a single subcategory slug.
func planRegroup(reg *Registry, subFilter string) []regroupMove {
	var moves []regroupMove
	for i := range reg.Records {
		rec := &reg.Records[i]
		if rec.Subcategory == "" || !validSubcategory(rec.Category, rec.Subcategory) {
			continue
		}
		if len(groupsForSubcategory(rec.Subcategory)) == 0 {
			continue
		}
		if subFilter != "" && rec.Subcategory != subFilter {
			continue
		}
		if len(rec.Capabilities) == 0 {
			// Already grouped (or empty): nothing flat to move.
			continue
		}
		for key := range rec.Capabilities {
			owner := groupForCapability(rec.Subcategory, key)
			if owner == "" {
				owner = uncategorizedGroup
			}
			moves = append(moves, regroupMove{
				RecordID: rec.ID,
				Language: rec.Language,
				Group:    owner,
				Key:      key,
			})
		}
	}
	sortRegroupMoves(moves)
	return moves
}

// sortRegroupMoves orders moves by (RecordID, Group, Key) so dry-run
// output and the resulting registry write are byte-stable across runs.
func sortRegroupMoves(moves []regroupMove) {
	sort.Slice(moves, func(i, j int) bool {
		if moves[i].RecordID != moves[j].RecordID {
			return moves[i].RecordID < moves[j].RecordID
		}
		if moves[i].Group != moves[j].Group {
			return moves[i].Group < moves[j].Group
		}
		return moves[i].Key < moves[j].Key
	})
}

// applyRegroup relocates each planned flat cell into its canonical group,
// preserving the cell payload verbatim, then clears the now-empty flat
// capabilities map so the record reads as grouped. Records that end up
// with at least one moved cell have Capabilities set to nil so the
// on-disk shape is unambiguously grouped (validate forbids carrying both
// shapes). Returns the number of cells moved.
func applyRegroup(reg *Registry, moves []regroupMove) int {
	byID := map[string]*Record{}
	for i := range reg.Records {
		byID[reg.Records[i].ID] = &reg.Records[i]
	}
	touched := map[string]bool{}
	moved := 0
	for _, m := range moves {
		rec := byID[m.RecordID]
		if rec == nil {
			continue
		}
		cell, ok := rec.Capabilities[m.Key]
		if !ok {
			continue
		}
		if rec.Groups == nil {
			rec.Groups = map[string]map[string]Capability{}
		}
		if rec.Groups[m.Group] == nil {
			rec.Groups[m.Group] = map[string]Capability{}
		}
		// No-clobber: never overwrite a cell that already exists in the
		// target group (e.g. a re-run after a partial write).
		if _, exists := rec.Groups[m.Group][m.Key]; !exists {
			rec.Groups[m.Group][m.Key] = cell
			moved++
		}
		delete(rec.Capabilities, m.Key)
		touched[m.RecordID] = true
	}
	// Drop emptied flat maps so the record serialises as grouped.
	for id := range touched {
		rec := byID[id]
		if len(rec.Capabilities) == 0 {
			rec.Capabilities = nil
		}
	}
	return moved
}

// cmdRegroup migrates records whose subcategory declares a group taxonomy
// but whose cells are still flat, moving each flat capability key into its
// canonical group (preserving status/cites/verified_at/issue/notes).
// Idempotent and deterministic; writes via saveRegistry.
func cmdRegroup(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("regroup", flag.ContinueOnError)
	path := registryFlag(fs)
	sub := fs.String("subcategory", "", "scope to a single subcategory slug")
	dryRun := fs.Bool("dry-run", false, "print (record, group, key) moves + per-language counts; write nothing")
	check := fs.Bool("check", false, "exit non-zero if any cell would be moved (CI guard); write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	moves := planRegroup(reg, *sub)

	if *dryRun || *check {
		printRegroupReport(out, moves)
	}
	if *check {
		if len(moves) > 0 {
			return fmt.Errorf("regroup --check: %d cell(s) would be moved", len(moves))
		}
		return nil
	}
	if *dryRun {
		return nil
	}

	moved := applyRegroup(reg, moves)
	if moved == 0 {
		fmt.Fprintln(out, "regroup: nothing to move (registry already grouped)")
		return nil
	}
	if err := saveRegistry(*path, reg); err != nil {
		return err
	}
	fmt.Fprintf(out, "regroup: moved %d cell(s)\n", moved)
	return nil
}

// printRegroupReport writes the (record, group, key) moves in sorted
// order followed by per-language counts and a grand total. Shared by
// --dry-run and --check.
func printRegroupReport(out io.Writer, moves []regroupMove) {
	for _, m := range moves {
		fmt.Fprintf(out, "%s\t%s\t%s\n", m.RecordID, m.Group, m.Key)
	}
	perLang := map[string]int{}
	for _, m := range moves {
		perLang[m.Language]++
	}
	langs := make([]string, 0, len(perLang))
	for l := range perLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	fmt.Fprintln(out, "per-language pending-move counts:")
	for _, l := range langs {
		fmt.Fprintf(out, "  %-14s %d\n", l, perLang[l])
	}
	fmt.Fprintf(out, "total: %d cell(s) would be moved\n", len(moves))
}
