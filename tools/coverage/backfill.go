package main

import (
	"flag"
	"fmt"
	"io"
	"sort"
)

// defaultBackfillIssue is the tracking tag stamped on every cell seeded
// by `coverage backfill` when --issue is not supplied. It marks the cell
// as a dictionary-completeness placeholder rather than a hand-triaged
// gap so later sweeps can distinguish auto-seeded rows from real
// known-limitation tickets.
const defaultBackfillIssue = "backfill:dictionary-completeness"

// seedPlan is a single (record, group, key) cell that backfill would
// seed. Slices of seedPlan are sorted deterministically before any
// output or write so dry-run reports and registry writes are stable.
type seedPlan struct {
	RecordID string
	Language string
	Group    string
	Key      string
}

// planBackfill computes the set of lane cells that are declared by each
// grouped record's subcategory group taxonomy but absent from the
// record's cells. It mutates nothing — callers decide whether to seed.
//
// Only GROUPED records whose subcategory carries a group taxonomy are
// considered. A lane key is "declared" if it appears in the subcategory's
// group taxonomy; its canonical owning group is groupForCapability (the
// first group in render order that lists the key), mirroring the
// auto-placement in cmdUpdate. A key already present anywhere on the
// record (any group) is never re-seeded — this is the no-clobber
// guarantee. framework_specific cells are deliberately ignored: the
// completeness denominator here is the canonical lane set only.
//
// langFilter / subFilter, when non-empty, scope the plan to a single
// language slug / subcategory slug.
func planBackfill(reg *Registry, langFilter, subFilter string) []seedPlan {
	var plans []seedPlan
	for i := range reg.Records {
		rec := &reg.Records[i]
		if !rec.IsGrouped() {
			continue
		}
		if rec.Subcategory == "" || !validSubcategory(rec.Category, rec.Subcategory) {
			continue
		}
		if langFilter != "" && rec.Language != langFilter {
			continue
		}
		if subFilter != "" && rec.Subcategory != subFilter {
			continue
		}
		groups := groupsForSubcategory(rec.Subcategory)
		if len(groups) == 0 {
			continue
		}
		existing := rec.AllCapabilities()
		// Walk the taxonomy in canonical render order and seed each
		// declared key exactly once, into its canonical owning group.
		// A key declared under multiple groups (e.g. db_effect) is owned
		// by the first group in render order — groupForCapability returns
		// precisely that group, so seeding stays deterministic.
		seeded := map[string]struct{}{}
		for _, g := range groups {
			for _, key := range g.Keys {
				if _, ok := existing[key]; ok {
					continue
				}
				if _, done := seeded[key]; done {
					continue
				}
				owner := groupForCapability(rec.Subcategory, key)
				if owner == "" {
					owner = uncategorizedGroup
				}
				if owner != g.Name {
					// Defer to the canonical owning group; we'll reach it
					// later in the render-order walk.
					continue
				}
				seeded[key] = struct{}{}
				plans = append(plans, seedPlan{
					RecordID: rec.ID,
					Language: rec.Language,
					Group:    owner,
					Key:      key,
				})
			}
		}
	}
	sortSeedPlans(plans)
	return plans
}

// sortSeedPlans orders plans by (RecordID, Group, Key) so dry-run output
// and the resulting registry write are byte-stable across runs.
func sortSeedPlans(plans []seedPlan) {
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].RecordID != plans[j].RecordID {
			return plans[i].RecordID < plans[j].RecordID
		}
		if plans[i].Group != plans[j].Group {
			return plans[i].Group < plans[j].Group
		}
		return plans[i].Key < plans[j].Key
	})
}

// applyBackfill inserts every planned cell into reg as a {missing, issue}
// placeholder. It assumes plans were produced by planBackfill against the
// same reg (no-clobber already enforced there) but re-checks presence
// defensively so an existing cell is never overwritten.
func applyBackfill(reg *Registry, plans []seedPlan, issue string) int {
	byID := map[string]*Record{}
	for i := range reg.Records {
		byID[reg.Records[i].ID] = &reg.Records[i]
	}
	seeded := 0
	for _, p := range plans {
		rec := byID[p.RecordID]
		if rec == nil {
			continue
		}
		if rec.Groups == nil {
			rec.Groups = map[string]map[string]Capability{}
		}
		if rec.Groups[p.Group] == nil {
			rec.Groups[p.Group] = map[string]Capability{}
		}
		if _, exists := rec.Groups[p.Group][p.Key]; exists {
			continue
		}
		// Defensive cross-group no-clobber: a key present in any other
		// group must never be duplicated here.
		if _, anywhere := rec.AllCapabilities()[p.Key]; anywhere {
			continue
		}
		rec.Groups[p.Group][p.Key] = Capability{Status: StatusMissing, Issue: issue}
		seeded++
	}
	return seeded
}

// cmdBackfill seeds missing lane cells declared by each grouped record's
// subcategory group taxonomy but absent from the record. See planBackfill
// for the placement / no-clobber rules.
func cmdBackfill(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	path := registryFlag(fs)
	issue := fs.String("issue", defaultBackfillIssue, "tracking tag stamped on each seeded cell")
	lang := fs.String("language", "", "scope to a single language slug")
	sub := fs.String("subcategory", "", "scope to a single subcategory slug")
	dryRun := fs.Bool("dry-run", false, "print (record, group, key) tuples + per-language counts; write nothing")
	check := fs.Bool("check", false, "exit non-zero if any cell would be seeded (CI guard); write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	plans := planBackfill(reg, *lang, *sub)

	if *dryRun || *check {
		printBackfillReport(out, plans)
	}
	if *check {
		if len(plans) > 0 {
			return fmt.Errorf("backfill --check: %d cell(s) would be seeded", len(plans))
		}
		return nil
	}
	if *dryRun {
		return nil
	}

	seeded := applyBackfill(reg, plans, *issue)
	if seeded == 0 {
		fmt.Fprintln(out, "backfill: nothing to seed (registry already complete)")
		return nil
	}
	if err := saveRegistry(*path, reg); err != nil {
		return err
	}
	fmt.Fprintf(out, "backfill: seeded %d cell(s)\n", seeded)
	return nil
}

// printBackfillReport writes the (record, group, key) tuples in sorted
// order followed by per-language counts and a grand total. Shared by
// --dry-run and --check.
func printBackfillReport(out io.Writer, plans []seedPlan) {
	for _, p := range plans {
		fmt.Fprintf(out, "%s\t%s\t%s\n", p.RecordID, p.Group, p.Key)
	}
	perLang := map[string]int{}
	for _, p := range plans {
		perLang[p.Language]++
	}
	langs := make([]string, 0, len(perLang))
	for l := range perLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	fmt.Fprintln(out, "per-language pending-seed counts:")
	for _, l := range langs {
		fmt.Fprintf(out, "  %-14s %d\n", l, perLang[l])
	}
	fmt.Fprintf(out, "total: %d cell(s) would be seeded\n", len(plans))
}
