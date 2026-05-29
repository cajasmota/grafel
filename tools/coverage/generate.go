package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
)

// docsDir is the canonical on-disk root for generated markdown.
const docsDir = "docs/coverage"

// doNotEditMarker is prepended to every generated file so reviewers and
// CI see immediately that hand-edits will be lost.
const doNotEditMarker = "<!-- DO NOT EDIT — generated from docs/coverage/registry.json by 'go run ./tools/coverage gen' -->"

//go:embed templates/*.tmpl
var templateFS embed.FS

// loadTemplates parses the four embedded markdown templates. Templates
// are parsed once and reused per render; parsing here keeps generate.go
// free of init-time side effects.
func loadTemplates() (*template.Template, error) {
	root := template.New("coverage").Funcs(templateFuncs)
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("read templates dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		data, err := templateFS.ReadFile("templates/" + n)
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", n, err)
		}
		if _, err := root.New(n).Parse(string(data)); err != nil {
			return nil, fmt.Errorf("parse template %s: %w", n, err)
		}
	}
	return root, nil
}

// capEntry is a key+capability pair flattened for deterministic
// template iteration (range over a map is unordered in Go).
type capEntry struct {
	Key string
	Cap Capability
}

// groupView is one capability group rendered on a detail page or as a
// digest cell on a pivot table.
type groupView struct {
	Name    string     // canonical group name (or "Uncategorized")
	CapList []capEntry // sorted capability cells in this group
	Digest  string     // "<glyph> <full>/<total>" or "—" when empty
}

// recordView wraps a Record with template-ready capability listings.
//
//   - CapList / CapByKey are the flat per-key listing used by pivot
//     tables and the legacy non-grouped detail rendering.
//   - GroupViews is populated only for grouped records and drives the
//     per-group sub-tables on the detail page plus the group-digest
//     cells on the per-language and per-category pivot tables.
//   - GroupDigestByName maps canonical group name → its digest for
//     templates that want O(1) cell lookup by column header.
type recordView struct {
	ID                string
	Category          string
	Subcategory       string
	Language          string
	Label             string
	Bucket            string
	CapList           []capEntry
	CapByKey          map[string]Capability
	GroupViews        []groupView
	GroupDigestByName map[string]string
	Digest            string
	Grouped           bool
}

// pivotRow is one row of the summary pivot table (rows = language,
// columns = bucket counts). Name is the slug used in filenames and
// links; Display is the human-facing label rendered in the table cell
// (they differ for slugs like "jsts" → "JS/TS").
type pivotRow struct {
	Name       string
	Display    string
	Frameworks int
	Tools      int
	ORMs       int
	Other      int
}

// crossCuttingRow is one row of the cross-cutting infrastructure pivot
// table (rows = category, columns = status counts). Counts are derived
// from registry records where language="multi" and category matches.
type crossCuttingRow struct {
	Slug    string // category slug, used in by-category/<slug>.md link
	Display string // human-facing label
	Records int
	Full    int
	Partial int
	Missing int
}

// placeholderLanguage is one entry in the "languages with extractor
// support but no records yet" section. Name is the file slug; Display
// is the human-facing label.
type placeholderLanguage struct {
	Name    string
	Display string
}

// bucketSection is one rendered section on a by-language page. When a
// bucket contains records with subcategories the section is split into
// Subsections (one per subcategory, ordered by subcategoryOrder) and a
// final Records list holds the un-subcategorized tail.
type bucketSection struct {
	Name           string
	CapabilityKeys []string
	Records        []recordView
	Subsections    []subSection
}

// subSection is one subcategory-scoped table inside a bucketSection.
// When the subcategory has a declared group taxonomy (subcategoryGroups)
// the table renders one column per group (group-digest cells) and
// CapabilityKeys is unused. Otherwise the legacy per-capability column
// set is used.
type subSection struct {
	Subcategory    string       // raw slug, used in IDs
	Heading        string       // display heading (e.g. "UI Frontend")
	CapabilityKeys []string     // legacy columns (when no group taxonomy)
	GroupNames     []string     // group-digest column headers
	Records        []recordView // pre-sorted (label, ID)
}

// summaryData feeds summary.md.tmpl.
//
// The summary is partitioned into three render sections:
//
//   - ActiveRows: languages with ≥1 ecosystem record, sorted by
//     Frameworks count desc then name (the main language pivot).
//   - CrossCutting: one row per cross-cutting category that has ≥1
//     record (language="multi"). Categories with zero records appear
//     in EmptyCrossCutting instead so the main table stays clean.
//   - PlaceholderLangs: extractor-supported languages with zero records,
//     sorted alphabetically by display name (the "not yet tracked"
//     table at the bottom).
//
// ActiveLanguages and PlaceholderLanguages totals power the headline
// banner ("16 active · 22 placeholder").
type summaryData struct {
	Marker             string
	TotalLanguages     int
	ActiveLanguages    int
	PlaceholderCount   int
	TotalFrameworks    int
	TotalTools         int
	TotalORMs          int
	TotalOther         int
	ActiveRows         []pivotRow
	CrossCutting       []crossCuttingRow
	CrossCuttingTotal  crossCuttingRow
	EmptyCrossCutting  []crossCuttingRow
	PlaceholderLangs   []placeholderLanguage
}

// languagePageData feeds by-language/<lang>.md.tmpl.
type languagePageData struct {
	Marker     string
	Language   string
	Frameworks int
	Tools      int
	ORMs       int
	Other      int
	Sections   []bucketSection // bucketOrder, empty sections omitted
}

// placeholderPageData feeds by-language-placeholder.md.tmpl. Rendered
// for each extractor-supported language that has zero ecosystem records;
// the page explains how to contribute records and cites the on-disk
// extractor directory.
type placeholderPageData struct {
	Marker       string
	Slug         string
	Language     string
	ExtractorDir string
}

// categoryLanguageCount is one element of the by-category banner.
type categoryLanguageCount struct {
	Language string
	Count    int
}

// categoryRow is one row in the by-category table (Language column +
// capability glyphs + Notes).
type categoryRow struct {
	Language          string
	Label             string
	ID                string
	Subcategory       string
	CapList           []capEntry
	CapByKey          map[string]Capability
	GroupViews        []groupView
	GroupDigestByName map[string]string
	Digest            string
	Grouped           bool
}

// categoryPageData feeds by-category/<cat>.md.tmpl.
type categoryPageData struct {
	Marker         string
	Category       string
	Bucket         string
	Total          int
	ByLanguage     []categoryLanguageCount
	CapabilityKeys []string
	Records        []categoryRow
	Subsections    []categorySubSection
}

// categorySubSection is one subcategory-scoped table on a by-category
// page. Mirrors subSection.
type categorySubSection struct {
	Subcategory    string
	Heading        string
	CapabilityKeys []string
	GroupNames     []string
	Records        []categoryRow
}

// detailPageData feeds detail/<id>.md.tmpl. When Grouped is true the
// template renders one sub-table per group (GroupViews); otherwise the
// legacy single capability table (CapList) is used. FrameworkSpecific
// renders as an additional top-level section below canonical capabilities
// when the record carries framework-unique capability groups (#2739).
//
// TotalCells is the grand total of capability cells rendered on the page:
// len(CapList) (canonical) + the sum of CapList lengths across all
// FrameworkSpecific groups (#2946). This is the number that the
// "Capability cells:" header must display so it stays consistent with the
// completeness metric that AllCapabilitiesIncludingFrameworkSpecific()
// powers.
type detailPageData struct {
	Marker            string
	Record            Record
	CapList           []capEntry
	GroupViews        []groupView
	Grouped           bool
	FrameworkSpecific []frameworkSpecificView
	TotalCells        int
}

// frameworkSpecificView is one free-form capability group rendered on a
// detail page. Name is emitted verbatim (no prettyKey transformation —
// authors choose the human-readable group name). CapList holds the
// sorted capability cells in this group.
type frameworkSpecificView struct {
	Name    string
	CapList []capEntry
}

// recordToView materialises a Record with sorted capability entries so
// templates iterate deterministically.
func recordToView(rec Record) recordView {
	flat := rec.AllCapabilities()
	keys := sortedCapKeys(flat)
	list := make([]capEntry, 0, len(keys))
	byKey := make(map[string]Capability, len(keys))
	for _, k := range keys {
		list = append(list, capEntry{Key: k, Cap: flat[k]})
		byKey[k] = flat[k]
	}
	view := recordView{
		ID:                rec.ID,
		Category:          rec.Category,
		Subcategory:       rec.Subcategory,
		Language:          rec.Language,
		Label:             rec.Label,
		Bucket:            bucketOf(rec.Category),
		CapList:           list,
		CapByKey:          byKey,
		Digest:            digestStatus(flat),
		Grouped:           rec.IsGrouped(),
		GroupDigestByName: map[string]string{},
	}
	if rec.IsGrouped() {
		view.GroupViews = buildGroupViews(rec)
		for _, g := range view.GroupViews {
			view.GroupDigestByName[g.Name] = g.Digest
		}
	}
	// "Other capabilities" merged digest (#2940): everything outside the
	// universal-core lanes — non-universal canonical group cells UNION all
	// framework_specific cells — rolled into a single digest column so a
	// framework's unique idioms render (and count) on the overview. The set
	// is built order-independently (map union) so the digest is
	// deterministic regardless of group/key iteration order.
	if other := otherCapabilitiesCells(rec); len(other) > 0 {
		view.GroupDigestByName[OtherCapabilitiesColumn] = groupDigest(other)
	} else {
		view.GroupDigestByName[OtherCapabilitiesColumn] = "—"
	}
	return view
}

// otherCapabilitiesCells returns the union of the record's non-universal-
// core canonical group cells and all of its framework_specific cells,
// keyed by capability slug (#2940). Universal-core group cells are
// excluded — they own their own pivot columns. For non-grouped records
// (no subcategory taxonomy) every canonical cell is "other" since none of
// them belongs to a universal-core lane. Keys are collision-free across
// tiers (validate.go), so the union never silently overwrites.
func otherCapabilitiesCells(rec Record) map[string]Capability {
	out := map[string]Capability{}
	if rec.IsGrouped() {
		for gname, caps := range rec.Groups {
			if isUniversalCore(gname) {
				continue
			}
			for k, c := range caps {
				out[k] = c
			}
		}
	} else {
		for k, c := range rec.Capabilities {
			out[k] = c
		}
	}
	for _, caps := range rec.FrameworkSpecific {
		for k, c := range caps {
			out[k] = c
		}
	}
	return out
}

// buildGroupViews materialises one groupView per declared group in the
// subcategory's taxonomy, in canonical order. Empty groups (records that
// have not yet populated every group) still render with a "—" digest so
// the pivot table column layout is stable across records.
func buildGroupViews(rec Record) []groupView {
	canon := knownGroupNames(rec.Subcategory)
	seen := map[string]bool{}
	views := make([]groupView, 0, len(canon)+len(rec.Groups))
	for _, name := range canon {
		caps := rec.Groups[name]
		views = append(views, makeGroupView(name, caps))
		seen[name] = true
	}
	// Any extras (Uncategorized or unknown group names that survived
	// validation only because of legacy data) get appended alphabetically.
	extras := make([]string, 0)
	for name := range rec.Groups {
		if !seen[name] {
			extras = append(extras, name)
		}
	}
	sort.Strings(extras)
	for _, name := range extras {
		views = append(views, makeGroupView(name, rec.Groups[name]))
	}
	return views
}

// buildFrameworkSpecificViews materialises rec.FrameworkSpecific as a
// slice of template-ready views. Group names sort alphabetically (no
// canonical taxonomy applies) and capability keys within each group
// follow sortedCapKeys. Returns nil when the record has no
// framework-specific entries so the template can guard with a simple
// length check.
func buildFrameworkSpecificViews(rec Record) []frameworkSpecificView {
	if !rec.HasFrameworkSpecific() {
		return nil
	}
	names := make([]string, 0, len(rec.FrameworkSpecific))
	for n := range rec.FrameworkSpecific {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]frameworkSpecificView, 0, len(names))
	for _, n := range names {
		caps := rec.FrameworkSpecific[n]
		keys := sortedCapKeys(caps)
		list := make([]capEntry, 0, len(keys))
		for _, k := range keys {
			list = append(list, capEntry{Key: k, Cap: caps[k]})
		}
		out = append(out, frameworkSpecificView{Name: n, CapList: list})
	}
	return out
}

// makeGroupView constructs a groupView from a group name + its cell
// map. The CapList is sorted alphabetically by key; the digest follows
// the "<glyph> <covered>/<applicable>" convention (see groupDigest).
func makeGroupView(name string, caps map[string]Capability) groupView {
	keys := sortedCapKeys(caps)
	list := make([]capEntry, 0, len(keys))
	for _, k := range keys {
		list = append(list, capEntry{Key: k, Cap: caps[k]})
	}
	return groupView{
		Name:    name,
		CapList: list,
		Digest:  groupDigest(caps),
	}
}

// groupDigest returns a support-tier glyph + covered/applicable summary
// for a group's capability cells. Empty groups (no cells declared) render
// as "—" so pivot rows for sparsely-populated records still align under
// every group column. The numerator counts COVERED cells (StatusFull +
// StatusPartial — any capability that has real extraction, comprehensive
// or heuristic); the denominator counts APPLICABLE cells (full + partial
// + missing), EXCLUDING not_applicable so capabilities that genuinely
// don't apply to the framework neither help nor hurt the fraction. The
// glyph is a SUPPORT LEVEL (see supportGlyph), not a worst-cell warning,
// so a fully-but-heuristically extracted column reads as "🟢 supported"
// rather than "⚠️ warning". A group with no applicable cells → "—".
func groupDigest(caps map[string]Capability) string {
	if len(caps) == 0 {
		return "—"
	}
	full, partial, missing := 0, 0, 0
	for _, c := range caps {
		switch c.Status {
		case StatusFull:
			full++
		case StatusPartial:
			partial++
		case StatusMissing:
			missing++
		}
	}
	covered := full + partial
	applicable := covered + missing
	if applicable == 0 {
		return "—"
	}
	return fmt.Sprintf("%s %d/%d", supportGlyph(full, partial, missing), covered, applicable)
}

// supportGlyph maps a group's full/partial/missing counts to a four-tier
// SUPPORT-LEVEL indicator. The point is that "extracted but heuristic" is
// a positive state, not a warning — so it gets a green circle, not ⚠️:
//
//	✅ comprehensive  — every applicable capability is `full` (fixture-proven)
//	🟢 supported      — every applicable capability is extracted; some only heuristically (partial)
//	🟡 partial        — some applicable capabilities are extracted, some still missing
//	🔴 not extracted  — nothing extracted yet (all applicable cells missing)
//
// Callers pass counts for a group whose applicable total (full+partial+
// missing) is already known to be > 0.
func supportGlyph(full, partial, missing int) string {
	switch {
	case missing == 0 && partial == 0:
		return "✅"
	case missing == 0:
		return "🟢"
	case full+partial > 0:
		return "🟡"
	default:
		return "🔴"
	}
}

// languageDisplay maps a language slug to its human-facing label. The
// underlying table is shared with the placeholder by-language pages —
// see languageDisplayName. Slugs that exist as records but lack an
// override render verbatim (preserves existing per-record labelling for
// languages like "python", "ruby", "go").
func languageDisplay(slug string) string {
	if v, ok := languageDisplayOverrides[slug]; ok {
		return v
	}
	return slug
}

// issueRefRe matches bare GitHub issue refs: #NNNN or owner/repo#NNNN.
var issueRefRe = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#[0-9]+$`)

// renderIssue formats a capability's issue field for markdown:
//   - empty string → "—"
//   - http(s) URL → "[link](<url>)"
//   - #NNNN or owner/repo#NNNN → "[link](<ref>)"
//   - anything else (e.g. "backfill:dictionary-completeness") → plain text
func renderIssue(issue string) string {
	if issue == "" {
		return "—"
	}
	if strings.HasPrefix(issue, "https://") || strings.HasPrefix(issue, "http://") {
		return "[link](" + issue + ")"
	}
	if issueRefRe.MatchString(issue) {
		return "[link](" + issue + ")"
	}
	return issue
}

// templateFuncs are the helpers exposed to templates.
var templateFuncs = template.FuncMap{
	"glyph":            statusGlyph,
	"langDsp":          languageDisplay,
	"prettyKey":        prettyKey,
	"humanizeCapKey":   humanizeCapKey,
	"subHeading":       subcategoryHeading,
	"groupCell":        groupCell,
	"renderIssue":      renderIssue,
}

// groupCell returns the digest string for groupName on a recordView or
// categoryRow-like value. It accepts a map[string]string lookup so the
// template can keep its expressions terse. Missing entries render as
// "—" so columns never leave blank cells.
func groupCell(byName map[string]string, groupName string) string {
	if byName == nil {
		return "—"
	}
	if v, ok := byName[groupName]; ok {
		return v
	}
	return "—"
}

// generate writes the full markdown tree under outRoot/docs/coverage.
// outRoot is normally the repo root; tests point it at a t.TempDir().
// Output is deterministic: sorted iteration everywhere, no time.Now,
// no environment-dependent state.
func generate(reg *Registry, outRoot string) error {
	tmpls, err := loadTemplates()
	if err != nil {
		return err
	}

	sortedRecs := make([]Record, len(reg.Records))
	copy(sortedRecs, reg.Records)
	sort.Slice(sortedRecs, func(i, j int) bool { return sortedRecs[i].ID < sortedRecs[j].ID })
	allViews := make([]recordView, len(sortedRecs))
	for i, r := range sortedRecs {
		allViews[i] = recordToView(r)
	}

	byLang := map[string][]recordView{}
	byCat := map[string][]recordView{}
	byLangBucket := map[string]map[string][]recordView{}
	langSet := map[string]struct{}{}
	for _, v := range allViews {
		byLang[v.Language] = append(byLang[v.Language], v)
		byCat[v.Category] = append(byCat[v.Category], v)
		if byLangBucket[v.Language] == nil {
			byLangBucket[v.Language] = map[string][]recordView{}
		}
		byLangBucket[v.Language][v.Bucket] = append(byLangBucket[v.Language][v.Bucket], v)
		langSet[v.Language] = struct{}{}
	}

	langNames := make([]string, 0, len(langSet))
	for n := range langSet {
		langNames = append(langNames, n)
	}
	sort.Strings(langNames)

	catNames := make([]string, 0, len(byCat))
	for n := range byCat {
		catNames = append(catNames, n)
	}
	sort.Strings(catNames)

	sortByLabel := func(rs []recordView) {
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].Label != rs[j].Label {
				return rs[i].Label < rs[j].Label
			}
			return rs[i].ID < rs[j].ID
		})
	}
	for _, n := range langNames {
		sortByLabel(byLang[n])
		for _, b := range bucketOrder {
			sortByLabel(byLangBucket[n][b])
		}
	}
	for _, n := range catNames {
		sortByLabel(byCat[n])
	}

	root := filepath.Join(outRoot, docsDir)
	if err := os.MkdirAll(filepath.Join(root, "by-language"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "by-category"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "detail"), 0o755); err != nil {
		return err
	}

	// Build the main language pivot from languages with ≥1 record. The
	// synthetic "multi" / unset-language slug is excluded — those records
	// surface in the cross-cutting pivot instead, where each cell lives
	// in exactly one place.
	totals := pivotRow{}
	activeRows := make([]pivotRow, 0, len(langNames))
	for _, n := range langNames {
		if n == "multi" || n == "" {
			continue
		}
		buckets := byLangBucket[n]
		row := pivotRow{
			Name:       n,
			Display:    languageDisplay(n),
			Frameworks: len(buckets[BucketFrameworks]),
			Tools:      len(buckets[BucketTools]),
			ORMs:       len(buckets[BucketORMs]),
			Other:      len(buckets[BucketOther]),
		}
		totals.Frameworks += row.Frameworks
		totals.Tools += row.Tools
		totals.ORMs += row.ORMs
		totals.Other += row.Other
		activeRows = append(activeRows, row)
	}
	// Cross-cutting records (language="multi") still contribute to the
	// global Frameworks/Tools/ORMs/Other counters in the headline banner
	// — the partition only governs which table renders them.
	if multiBuckets, ok := byLangBucket["multi"]; ok {
		totals.Frameworks += len(multiBuckets[BucketFrameworks])
		totals.Tools += len(multiBuckets[BucketTools])
		totals.ORMs += len(multiBuckets[BucketORMs])
		totals.Other += len(multiBuckets[BucketOther])
	}
	sort.SliceStable(activeRows, func(i, j int) bool {
		if activeRows[i].Frameworks != activeRows[j].Frameworks {
			return activeRows[i].Frameworks > activeRows[j].Frameworks
		}
		return activeRows[i].Name < activeRows[j].Name
	})

	// Cross-cutting pivot: one row per canonical category, populated from
	// records tagged language="multi" so every cell lives in exactly one
	// summary table. Categories with zero records render in the
	// "tracked but no records" section instead (omitted when empty).
	activeCC, emptyCC, ccTotal := buildCrossCuttingRows(byCat)

	// Extractor-supported but record-free languages form the placeholder
	// table at the bottom. Sorted by display name for human scanability.
	supported := SupportedLanguages(outRoot)
	placeholderLangs := make([]placeholderLanguage, 0, len(supported))
	for _, s := range supported {
		if _, present := langSet[s]; present {
			continue
		}
		placeholderLangs = append(placeholderLangs, placeholderLanguage{
			Name:    s,
			Display: languageDisplayName(s),
		})
	}
	sort.SliceStable(placeholderLangs, func(i, j int) bool {
		return placeholderLangs[i].Display < placeholderLangs[j].Display
	})

	activeLangCount := len(activeRows)
	if err := renderToFile(tmpls, "summary.md.tmpl", filepath.Join(root, "summary.md"), summaryData{
		Marker:            doNotEditMarker,
		TotalLanguages:    activeLangCount + len(placeholderLangs),
		ActiveLanguages:   activeLangCount,
		PlaceholderCount:  len(placeholderLangs),
		TotalFrameworks:   totals.Frameworks,
		TotalTools:        totals.Tools,
		TotalORMs:         totals.ORMs,
		TotalOther:        totals.Other,
		ActiveRows:        activeRows,
		CrossCutting:      activeCC,
		CrossCuttingTotal: ccTotal,
		EmptyCrossCutting: emptyCC,
		PlaceholderLangs:  placeholderLangs,
	}); err != nil {
		return err
	}

	// Emit placeholder by-language pages for each supported-but-untracked
	// language so the summary links resolve and contributors land on a
	// page that explains how to add records.
	for _, s := range supported {
		if _, present := langSet[s]; present {
			continue
		}
		if err := renderToFile(tmpls, "by-language-placeholder.md.tmpl",
			filepath.Join(root, "by-language", s+".md"),
			placeholderPageData{
				Marker:       doNotEditMarker,
				Slug:         s,
				Language:     languageDisplayName(s),
				ExtractorDir: extractorDirForSlug(s),
			}); err != nil {
			return err
		}
	}

	for _, n := range langNames {
		// "multi" / unset-language records are cross-cutting build/CI/infra
		// tooling, not a language — they render in the cross-cutting pivot on
		// the summary and in their by-category pages. Emitting a by-language
		// page for them would resurrect the "Uncategorized" pseudo-language
		// that #2821 removed, so skip it.
		if n == "multi" || n == "" {
			continue
		}
		buckets := byLangBucket[n]
		sections := make([]bucketSection, 0, len(bucketOrder))
		for _, b := range bucketOrder {
			recs := buckets[b]
			if len(recs) == 0 {
				continue
			}
			sections = append(sections, buildBucketSection(b, recs))
		}
		if err := renderToFile(tmpls, "by-language.md.tmpl",
			filepath.Join(root, "by-language", n+".md"),
			languagePageData{
				Marker:     doNotEditMarker,
				Language:   languageDisplay(n),
				Frameworks: len(buckets[BucketFrameworks]),
				Tools:      len(buckets[BucketTools]),
				ORMs:       len(buckets[BucketORMs]),
				Other:      len(buckets[BucketOther]),
				Sections:   sections,
			}); err != nil {
			return err
		}
	}

	for _, n := range catNames {
		recs := byCat[n]
		bucket := bucketOf(n)
		perLang := map[string]int{}
		for _, r := range recs {
			perLang[r.Language]++
		}
		langs := make([]string, 0, len(perLang))
		for l := range perLang {
			langs = append(langs, l)
		}
		sort.Strings(langs)
		banner := make([]categoryLanguageCount, 0, len(langs))
		for _, l := range langs {
			banner = append(banner, categoryLanguageCount{Language: l, Count: perLang[l]})
		}
		var keys []string
		if bucket == BucketOther {
			cats := dict().CategoryCapabilities(n)
			keys = make([]string, len(cats))
			copy(keys, cats)
			sort.Strings(keys)
		} else {
			keys = bucketCapabilityKeys(bucket)
		}
		rows := make([]categoryRow, 0, len(recs))
		for _, r := range recs {
			rows = append(rows, categoryRow{
				Language:          r.Language,
				Label:             r.Label,
				ID:                r.ID,
				Subcategory:       r.Subcategory,
				CapList:           r.CapList,
				CapByKey:          r.CapByKey,
				GroupViews:        r.GroupViews,
				GroupDigestByName: r.GroupDigestByName,
				Digest:            r.Digest,
				Grouped:           r.Grouped,
			})
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Language != rows[j].Language {
				return rows[i].Language < rows[j].Language
			}
			return rows[i].Label < rows[j].Label
		})
		subSecs, flatRows := splitCategoryRowsBySubcategory(n, rows)
		if err := renderToFile(tmpls, "by-category.md.tmpl",
			filepath.Join(root, "by-category", n+".md"),
			categoryPageData{
				Marker:         doNotEditMarker,
				Category:       n,
				Bucket:         bucket,
				Total:          len(recs),
				ByLanguage:     banner,
				CapabilityKeys: keys,
				Records:        flatRows,
				Subsections:    subSecs,
			}); err != nil {
			return err
		}
	}

	for _, rec := range sortedRecs {
		view := recordToView(rec)
		fsViews := buildFrameworkSpecificViews(rec)
		// TotalCells: canonical cells + all framework_specific cells (#2946).
		// This matches the completeness metric powered by
		// AllCapabilitiesIncludingFrameworkSpecific().
		totalCells := len(view.CapList)
		for _, fsv := range fsViews {
			totalCells += len(fsv.CapList)
		}
		if err := renderToFile(tmpls, "detail.md.tmpl",
			filepath.Join(root, "detail", rec.ID+".md"),
			detailPageData{
				Marker:            doNotEditMarker,
				Record:            rec,
				CapList:           view.CapList,
				GroupViews:        view.GroupViews,
				Grouped:           view.Grouped,
				FrameworkSpecific: fsViews,
				TotalCells:        totalCells,
			}); err != nil {
			return err
		}
	}
	return nil
}

// crossCuttingCategories is the canonical render order for the
// cross-cutting infrastructure pivot table. Each entry pairs the
// registry category slug (used in by-category links and as the lookup
// key into per-category record sets) with its human-facing display
// label. Adding a new cross-cutting category: append a row here and
// declare its capability keys in categoryCapabilities (schema.go).
var crossCuttingCategories = []struct {
	Slug    string
	Display string
}{
	{"databases", "Databases"},
	{"platform", "Platform / k8s"},
	{"message_broker", "Message Brokers"},
	{"ci_cd", "CI/CD"},
	{"security", "Security"},
	{"observability", "Observability"},
	{"protocol", "Protocols"},
	{"build_system", "Build Systems"},
}

// buildCrossCuttingRows partitions the canonical cross-cutting
// categories into rendered rows (≥1 record) and an "empty" tail
// (zero records) so the summary template can drop the second section
// entirely when nothing is tracked-but-empty. Counts come from records
// in byCat tagged language="multi"; each record contributes once to
// Records and once to whichever of Full/Partial/Missing its worst-cell
// status maps to (StatusNotApplicable rolls into Full so empty-but-
// declared records don't skew the missing column).
func buildCrossCuttingRows(byCat map[string][]recordView) ([]crossCuttingRow, []crossCuttingRow, crossCuttingRow) {
	active := make([]crossCuttingRow, 0, len(crossCuttingCategories))
	empty := make([]crossCuttingRow, 0)
	total := crossCuttingRow{Display: "Total"}
	for _, c := range crossCuttingCategories {
		row := crossCuttingRow{Slug: c.Slug, Display: c.Display}
		for _, rv := range byCat[c.Slug] {
			if rv.Language != "multi" {
				continue
			}
			row.Records++
			switch rv.Digest {
			case StatusFull, StatusNotApplicable, "":
				row.Full++
			case StatusPartial:
				row.Partial++
			case StatusMissing:
				row.Missing++
			}
		}
		if row.Records == 0 {
			empty = append(empty, row)
			continue
		}
		active = append(active, row)
		total.Records += row.Records
		total.Full += row.Full
		total.Partial += row.Partial
		total.Missing += row.Missing
	}
	return active, empty, total
}

// nonStrandedGroupNames is the don't-strand render guard (#2902/#2899).
// It filters a subcategory's canonical group-column headers down to those
// that at least one record in the table actually carries a cell for. A
// group whose digest is "—" (the empty-group glyph from groupDigest) for
// *every* record renders an all-"—" column that is pure visual noise — it
// is "truly N/A" for the records present, not "tracked-but-missing" (❌).
// Hiding it keeps the pivot honest while staging a partially-populated
// group never leaves an ugly all-"—" column.
//
// candidates is the canonical, already-ordered group-name list from
// knownGroupNames; digestFor returns a record's digest for a group name
// (caller passes a closure over recordView/categoryRow so this stays
// type-agnostic). The returned slice preserves candidates' order and is a
// fresh allocation (never aliases candidates). Determinism: iteration is
// over the ordered candidates and the explicitly-ordered record slice, so
// no map ranging leaks here.
func nonStrandedGroupNames(candidates []string, records int, digestFor func(rec int, group string) string) []string {
	if len(candidates) == 0 || records == 0 {
		return candidates
	}
	out := make([]string, 0, len(candidates))
	for _, name := range candidates {
		for r := 0; r < records; r++ {
			if digestFor(r, name) != "—" {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

// universalPivotColumns returns the per-subcategory pivot column headers
// under the #2940 model: the universal-core lanes the subcategory actually
// declares (universal_core.order ∩ knownGroupNames(sub), omit-if-absent,
// in universal-core render order) followed by the synthetic
// OtherCapabilitiesColumn, always last.
//
// The universal columns are subject to the don't-strand guard
// (nonStrandedGroupNames): a lane that is all-"—" across every record in
// the table is dropped. The OtherCapabilitiesColumn is EXEMPT from that
// over-suppression — it is kept whenever any record carries a non-"—"
// digest for it, and dropped only when it is "—" for every record (which
// the guard already does, applied to the single-element tail). Iteration
// is over the ordered universal_core list and the explicitly-ordered
// record slice, so no map ranging leaks here.
func universalPivotColumns(sub string, records int, digestFor func(rec int, group string) string) []string {
	declared := knownGroupNames(sub)
	declaredSet := map[string]bool{}
	for _, g := range declared {
		declaredSet[g] = true
	}
	universal := make([]string, 0, len(declared))
	for _, name := range universalCoreOrder() {
		if declaredSet[name] {
			universal = append(universal, name)
		}
	}
	cols := nonStrandedGroupNames(universal, records, digestFor)
	// The merged digest column: keep unless its digest is "—" for every
	// record (same all-"—" suppression, never the broader over-suppression).
	if len(nonStrandedGroupNames([]string{OtherCapabilitiesColumn}, records, digestFor)) > 0 {
		cols = append(cols, OtherCapabilitiesColumn)
	}
	return cols
}

// buildBucketSection produces a bucketSection for a per-language page.
// When any record in recs declares a subcategory, the section is split
// into one subSection per subcategory (ordered by subcategoryOrder)
// plus a final flat Records list for legacy un-subcategorised entries.
// Subsections whose subcategory has a declared group taxonomy switch
// from per-capability columns to per-group digest columns.
func buildBucketSection(bucket string, recs []recordView) bucketSection {
	bySub := map[string][]recordView{}
	var flat []recordView
	for _, r := range recs {
		if r.Subcategory == "" {
			flat = append(flat, r)
			continue
		}
		bySub[r.Subcategory] = append(bySub[r.Subcategory], r)
	}
	if len(bySub) == 0 {
		return bucketSection{
			Name:           bucket,
			CapabilityKeys: bucketCapabilityKeys(bucket),
			Records:        recs,
		}
	}
	cats := map[string]bool{}
	for _, r := range recs {
		if r.Subcategory != "" {
			cats[r.Category] = true
		}
	}
	catList := make([]string, 0, len(cats))
	for c := range cats {
		catList = append(catList, c)
	}
	sort.Strings(catList)
	merged := map[string]bool{}
	for s := range bySub {
		merged[s] = true
	}
	seen := map[string]bool{}
	ordered := make([]string, 0, len(merged))
	for _, c := range catList {
		for _, s := range dict().SubcategoriesByCategory(c) {
			if merged[s] && !seen[s] {
				ordered = append(ordered, s)
				seen[s] = true
			}
		}
	}
	extras := make([]string, 0)
	for s := range merged {
		if !seen[s] {
			extras = append(extras, s)
		}
	}
	sort.Strings(extras)
	ordered = append(ordered, extras...)

	subs := make([]subSection, 0, len(ordered))
	for _, s := range ordered {
		recsForSub := bySub[s]
		cat := recsForSub[0].Category
		groupNames := knownGroupNames(s)
		sec := subSection{
			Subcategory: s,
			Heading:     subcategoryHeading(s),
			Records:     recsForSub,
		}
		if len(groupNames) > 0 {
			sec.GroupNames = universalPivotColumns(s, len(recsForSub), func(r int, g string) string {
				return groupCell(recsForSub[r].GroupDigestByName, g)
			})
		} else {
			sec.CapabilityKeys = subcategoryRenderKeys(cat, s)
		}
		subs = append(subs, sec)
	}
	return bucketSection{
		Name:           bucket,
		CapabilityKeys: bucketCapabilityKeys(bucket),
		Records:        flat,
		Subsections:    subs,
	}
}

// splitCategoryRowsBySubcategory partitions by-category rows into
// per-subcategory subsections plus a tail of un-subcategorised rows.
// Subcategories with a declared group taxonomy switch from per-capability
// columns to per-group digest columns (parallel to buildBucketSection).
func splitCategoryRowsBySubcategory(category string, rows []categoryRow) ([]categorySubSection, []categoryRow) {
	bySub := map[string][]categoryRow{}
	var flat []categoryRow
	for _, r := range rows {
		if r.Subcategory == "" {
			flat = append(flat, r)
			continue
		}
		bySub[r.Subcategory] = append(bySub[r.Subcategory], r)
	}
	if len(bySub) == 0 {
		return nil, rows
	}
	present := map[string]bool{}
	for s := range bySub {
		present[s] = true
	}
	ordered := orderedSubcategories(category, present)
	subs := make([]categorySubSection, 0, len(ordered))
	for _, s := range ordered {
		groupNames := knownGroupNames(s)
		recsForSub := bySub[s]
		sec := categorySubSection{
			Subcategory: s,
			Heading:     subcategoryHeading(s),
			Records:     recsForSub,
		}
		if len(groupNames) > 0 {
			sec.GroupNames = universalPivotColumns(s, len(recsForSub), func(r int, g string) string {
				return groupCell(recsForSub[r].GroupDigestByName, g)
			})
		} else {
			sec.CapabilityKeys = subcategoryRenderKeys(category, s)
		}
		subs = append(subs, sec)
	}
	return subs, flat
}

// renderToFile executes the named template into a buffer and writes it
// atomically via temp+rename so partial writes never appear on disk.
func renderToFile(tmpls *template.Template, name, path string, data any) error {
	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("execute %s: %w", name, err)
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	out = append(out, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".coverage-gen.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// cmdGen wires the gen subcommand into main.go's dispatch.
func cmdGen(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	path := registryFlag(fs)
	outRoot := fs.String("out", ".", "output root (docs/coverage/* will be written under this)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	reg, err := loadRegistry(*path)
	if err != nil {
		return err
	}
	if err := generate(reg, *outRoot); err != nil {
		return err
	}
	fmt.Fprintf(out, "generated %d record(s) into %s\n", len(reg.Records), filepath.Join(*outRoot, docsDir))
	return nil
}
