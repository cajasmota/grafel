// Command coverage maintains the archigraph capabilities registry at
// docs/coverage/registry.json. It is a standalone dev tool with zero
// imports from internal/ packages; pure file I/O only.
//
// See issue #2720 for the full spec and docs/coverage/summary.md for
// the rendered registry.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run dispatches subcommands. It is split out from main so tests can
// drive it with custom argv and capture stdout/stderr.
func run(argv []string, stdout, stderr io.Writer) error {
	if len(argv) == 0 {
		printUsage(stderr)
		return fmt.Errorf("no subcommand")
	}
	sub, rest := argv[0], argv[1:]
	switch sub {
	case "list":
		return cmdList(rest, stdout)
	case "get":
		return cmdGet(rest, stdout)
	case "add":
		return cmdAdd(rest, stdout)
	case "update":
		return cmdUpdate(rest, stdout)
	case "backfill":
		return cmdBackfill(rest, stdout)
	case "remove":
		return cmdRemove(rest, stdout)
	case "gaps":
		return cmdGaps(rest, stdout)
	case "stats":
		return cmdStats(rest, stdout)
	case "validate":
		return cmdValidate(rest, stdout, stderr)
	case "gen":
		return cmdGen(rest, stdout)
	case "fmt":
		return cmdFmt(rest, stdout)
	case "discover":
		return cmdDiscover(rest, stdout)
	case "map-status":
		return cmdMapStatus(rest, stdout)
	case "delta":
		return cmdDelta(rest, stdout)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown subcommand %q", sub)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `usage: go run ./tools/coverage <subcommand> [flags]

subcommands:
  list      list records (filters: --status, --by-language, --by-category, --stale-days, --json)
  get       show one record by id (--json)
  add       insert a new record (--id, --category, [--subcategory], --language, --label)
  update    update one capability cell (flags must precede <record-id>)
            --capability, --status (required); --cites, --verified-now, --issue, --notes, --clear-issue
            when status=full or not_applicable the issue field is auto-cleared unless --issue is given
  backfill  seed missing lane cells declared by the group taxonomy (--file, --issue, --language, --subcategory, --dry-run, --check)
  remove    delete a record by id
  gaps      list missing/partial records (--language, --category, --json)
  stats     counters across the registry (--json)
  validate  schema + cite-exists + duplicate-id + stale checks
  gen       regenerate docs/coverage/*.md from docs/coverage/registry.json (--out, --file)
  fmt       rewrite registry.json in canonical form (--check verifies only; CI guard against recompaction)
  discover  catalog capabilities from repo signals; emit proposal + orphans + drift (--registry, --repo-root, --json, --include-orphans, --include-drift)
  map-status show the capability-map.yaml entry for one capability:
              map-status <record-id>/<capability>             (flat)
              map-status <record-id>/<group>/<capability>     (grouped)
            (--repo-root, --json)
  delta     diff registry.json before→after two git refs, emit markdown coverage-delta report
            (--base origin/main, --head HEAD, --lang <slug>, --post <PR#>, --file)`)
}

// registryFlag adds a shared --file flag for overriding the registry
// path (handy for tests). Returns a getter for the resolved path.
func registryFlag(fs *flag.FlagSet) *string {
	return fs.String("file", defaultRegistryPath, "path to the registry JSON (default docs/coverage/registry.json)")
}

func cmdList(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	path := registryFlag(fs)
	status := fs.String("status", "", "filter by capability status (full|partial|missing|not_applicable)")
	lang := fs.String("by-language", "", "filter by language")
	cat := fs.String("by-category", "", "filter by category")
	stale := fs.Int("stale-days", 0, "filter to records with any capability verified more than N days ago")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	recs := listRecords(reg, ListFilter{Status: *status, Language: *lang, Category: *cat, StaleDays: *stale})
	if *asJSON {
		return printJSON(out, recs)
	}
	printRecordsText(out, recs)
	return nil
}

func cmdGet(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	path := registryFlag(fs)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("get: expected exactly one ID argument")
	}
	id := fs.Arg(0)
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	rec := findRecord(reg, id)
	if rec == nil {
		return fmt.Errorf("get: id %q not found", id)
	}
	if *asJSON {
		return printJSON(out, rec)
	}
	printRecordsText(out, []Record{*rec})
	return nil
}

func cmdAdd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	path := registryFlag(fs)
	id := fs.String("id", "", "record ID (required)")
	cat := fs.String("category", "", "category (required)")
	sub := fs.String("subcategory", "", "subcategory (optional; must be declared for --category)")
	lang := fs.String("language", "", "language (required)")
	label := fs.String("label", "", "human-readable label (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *id == "" || *cat == "" || *lang == "" || *label == "" {
		return fmt.Errorf("add: --id, --category, --language, --label are all required")
	}
	if err := validateID(*id); err != nil {
		return err
	}
	if !categoryIsKnown(*cat) {
		return fmt.Errorf("add: unknown category %q (known: %v)", *cat, knownCategories())
	}
	if *sub != "" && !validSubcategory(*cat, *sub) {
		return fmt.Errorf("add: unknown subcategory %q for category %q (known: %v)", *sub, *cat, knownSubcategories(*cat))
	}
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	if findRecord(reg, *id) != nil {
		return fmt.Errorf("add: id %q already exists", *id)
	}
	reg.Records = append(reg.Records, Record{
		ID:           *id,
		Category:     *cat,
		Subcategory:  *sub,
		Language:     *lang,
		Label:        *label,
		Capabilities: map[string]Capability{},
	})
	if err := saveRegistry(*path, reg); err != nil {
		return err
	}
	fmt.Fprintf(out, "added %s\n", *id)
	return nil
}

func cmdUpdate(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	path := registryFlag(fs)
	cap := fs.String("capability", "", "capability key (required)")
	status := fs.String("status", "", "status: full|partial|missing|not_applicable (required)")
	cites := fs.String("cites", "", "comma-separated repo-relative paths")
	verifiedNow := fs.Bool("verified-now", false, "set verified_at to today")
	issue := fs.String("issue", "", "tracking issue URL (overrides auto-clear when status=full/not_applicable)")
	notes := fs.String("notes", "", "free-form scope clarification written to Capability.Notes")
	clearIssue := fs.Bool("clear-issue", false, "explicitly remove the issue field regardless of status")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("update: expected exactly one ID argument (flags must precede <record-id>)")
	}
	id := fs.Arg(0)
	if *cap == "" || *status == "" {
		return fmt.Errorf("update: --capability and --status are required")
	}
	if _, ok := validStatuses[*status]; !ok {
		return fmt.Errorf("update: invalid status %q", *status)
	}
	// Track whether --issue was explicitly supplied by the caller so
	// applyUpdateFlags can distinguish "not passed" from "passed as empty".
	issueExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "issue" {
			issueExplicit = true
		}
	})
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	rec := findRecord(reg, id)
	if rec == nil {
		return fmt.Errorf("update: id %q not found", id)
	}
	if rec.Subcategory != "" && validSubcategory(rec.Category, rec.Subcategory) {
		if !validCapabilityKeyForSubcategory(rec.Category, rec.Subcategory, *cap) {
			return fmt.Errorf("update: capability %q not valid for category %q subcategory %q", *cap, rec.Category, rec.Subcategory)
		}
	} else if !validCapabilityKey(rec.Category, *cap) {
		return fmt.Errorf("update: capability %q not valid for category %q", *cap, rec.Category)
	}
	// Route the write to the right shape. Grouped records auto-place
	// the cell into its canonical group (per subcategoryGroups); keys
	// outside the taxonomy fall into the synthetic Uncategorized
	// group so we never lose data.
	if rec.IsGrouped() {
		group := groupForCapability(rec.Subcategory, *cap)
		if group == "" {
			group = uncategorizedGroup
		}
		if rec.Groups[group] == nil {
			rec.Groups[group] = map[string]Capability{}
		}
		cell := rec.Groups[group][*cap]
		applyUpdateFlags(&cell, *status, *cites, *issue, *notes, *verifiedNow, issueExplicit, *clearIssue)
		rec.Groups[group][*cap] = cell
	} else {
		if rec.Capabilities == nil {
			rec.Capabilities = map[string]Capability{}
		}
		cell := rec.Capabilities[*cap]
		applyUpdateFlags(&cell, *status, *cites, *issue, *notes, *verifiedNow, issueExplicit, *clearIssue)
		rec.Capabilities[*cap] = cell
	}
	if err := saveRegistry(*path, reg); err != nil {
		return err
	}
	fmt.Fprintf(out, "updated %s.%s -> %s\n", id, *cap, *status)
	return nil
}

func cmdRemove(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	path := registryFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("remove: expected exactly one ID argument")
	}
	id := fs.Arg(0)
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	idx := -1
	for i, rec := range reg.Records {
		if rec.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("remove: id %q not found", id)
	}
	reg.Records = append(reg.Records[:idx], reg.Records[idx+1:]...)
	if err := saveRegistry(*path, reg); err != nil {
		return err
	}
	fmt.Fprintf(out, "removed %s\n", id)
	return nil
}

func cmdGaps(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("gaps", flag.ContinueOnError)
	path := registryFlag(fs)
	lang := fs.String("language", "", "filter by language")
	cat := fs.String("category", "", "filter by category")
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	recs := gapsRecords(reg, *lang, *cat)
	if *asJSON {
		return printJSON(out, recs)
	}
	printRecordsText(out, recs)
	return nil
}

func cmdStats(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	path := registryFlag(fs)
	asJSON := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	s := computeStats(reg)
	if *asJSON {
		return printJSON(out, s)
	}
	fmt.Fprintf(out, "total records:    %d\n", s.Total)
	fmt.Fprintf(out, "total capabilities: %d\n", s.Capabilities)
	fmt.Fprintln(out, "by status:")
	for _, k := range []string{StatusFull, StatusPartial, StatusMissing, StatusNotApplicable} {
		fmt.Fprintf(out, "  %-16s  %d\n", k, s.ByStatus[k])
	}
	fmt.Fprintln(out, "by language:")
	langs := make([]string, 0, len(s.ByLanguage))
	for k := range s.ByLanguage {
		langs = append(langs, k)
	}
	sortStrings(langs)
	for _, l := range langs {
		ls := s.ByLanguage[l]
		fmt.Fprintf(out, "  %-14s records=%d full=%d partial=%d missing=%d n/a=%d\n", l, ls.Records, ls.Full, ls.Partial, ls.Missing, ls.NotAppl)
	}
	return nil
}

func cmdValidate(args []string, out, errw io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	path := registryFlag(fs)
	repoRoot := fs.String("repo-root", ".", "repository root used to resolve cite paths")
	skipMap := fs.Bool("skip-map", false, "skip capability-map.yaml integrity checks")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	res := validateRegistry(reg, *repoRoot)
	var cm *CapabilityMap
	if !*skipMap {
		cm, err = LoadCapabilityMap(*repoRoot)
		if err != nil {
			return err
		}
	}
	mapRes := validateCapabilityMap(cm, reg, *repoRoot)
	for _, w := range res.Warnings {
		fmt.Fprintf(errw, "warning: %s\n", w)
	}
	for _, w := range mapRes.Warnings {
		fmt.Fprintf(errw, "warning: %s\n", w)
	}
	for _, e := range res.Errors {
		fmt.Fprintf(errw, "error: %s\n", e)
	}
	for _, e := range mapRes.Errors {
		fmt.Fprintf(errw, "error: %s\n", e)
	}
	totalErrors := len(res.Errors) + len(mapRes.Errors)
	totalWarnings := len(res.Warnings) + len(mapRes.Warnings)
	if totalErrors > 0 {
		return fmt.Errorf("validation failed: %d error(s)", totalErrors)
	}
	if cm == nil {
		fmt.Fprintf(out, "ok: %d record(s), %d warning(s) [no capability-map.yaml]\n", len(reg.Records), totalWarnings)
		return nil
	}
	fmt.Fprintf(out, "ok: %d record(s), %d warning(s); mapping: %d record(s), %d symbol(s), %d function(s), %d test(s) checked\n",
		len(reg.Records), totalWarnings, mapRes.RecordsChecked, mapRes.SymbolsChecked, mapRes.FunctionsChecked, mapRes.TestsChecked)
	return nil
}

// applyUpdateFlags mutates cell with the non-empty CLI flags from
// cmdUpdate. Shared between the flat and grouped write paths so cell
// semantics stay identical regardless of capability shape.
//
// Auto-clear semantics: when status is "full" or "not_applicable" the
// issue field is cleared UNLESS --issue was explicitly provided by the
// caller (issueExplicit=true). This prevents stale backfill issue tags
// from lingering after a cell is marked done. --clear-issue forces
// removal regardless of status.
func applyUpdateFlags(cell *Capability, status, cites, issue, notes string, verifiedNow, issueExplicit, clearIssue bool) {
	cell.Status = status
	if cites != "" {
		parts := strings.Split(cites, ",")
		clean := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				clean = append(clean, p)
			}
		}
		cell.Cites = clean
	}
	switch {
	case issueExplicit:
		// Caller explicitly passed --issue: honour the value (even if empty).
		cell.Issue = issue
	case clearIssue:
		// Caller explicitly requested removal.
		cell.Issue = ""
	case status == StatusFull || status == StatusNotApplicable:
		// Auto-clear: a resolved cell needs no tracking issue.
		cell.Issue = ""
	case issue != "":
		// Non-resolved status with a new issue value.
		cell.Issue = issue
	}
	if notes != "" {
		cell.Notes = notes
	}
	if verifiedNow {
		cell.VerifiedAt = time.Now().UTC().Format("2006-01-02")
	}
}

// sortStrings is a tiny local helper to avoid pulling sort into main.go
// at the top of the file (keeps the imports list minimal).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
