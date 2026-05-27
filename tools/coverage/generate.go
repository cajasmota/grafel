package main

import (
	"bytes"
	"embed"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// recordView wraps a Record with a pre-sorted CapList for templates.
type recordView struct {
	ID       string
	Category string
	Language string
	Label    string
	Bucket   string
	CapList  []capEntry
	// CapByKey lets templates look up a capability cell by key without
	// re-ranging CapList. Empty (missing) keys return the zero Capability;
	// templates pair this with the Glyph helper to render "—".
	CapByKey map[string]Capability
	// Digest is the worst-status across this record's capabilities.
	// Populated for every record; templates use it for the Other bucket's
	// single Status column.
	Digest string
}

// pivotRow is one row of the summary pivot table (rows = language,
// columns = bucket counts).
type pivotRow struct {
	Name           string
	Frameworks     int
	Tools          int
	ORMs           int
	Other          int
	Uncategorized  bool // true for the language-neutral "Uncategorized" row
}

// bucketSection is one rendered section on a by-language page.
type bucketSection struct {
	Name           string   // bucket display name (Frameworks/Tools/ORMs/Other)
	CapabilityKeys []string // capability columns; empty for Other
	Records        []recordView
}

// summaryData feeds summary.md.tmpl.
type summaryData struct {
	Marker          string
	TotalLanguages  int
	TotalFrameworks int
	TotalTools      int
	TotalORMs       int
	TotalOther      int
	Rows            []pivotRow // sorted by language; Uncategorized last
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

// categoryLanguageCount is one element of the by-category banner.
type categoryLanguageCount struct {
	Language string
	Count    int
}

// categoryRow is one row in the by-category table (Language column +
// capability glyphs + Notes).
type categoryRow struct {
	Language string
	Label    string
	ID       string
	CapList  []capEntry
	CapByKey map[string]Capability
	Digest   string
}

// categoryPageData feeds by-category/<cat>.md.tmpl.
type categoryPageData struct {
	Marker         string
	Category       string
	Bucket         string
	Total          int
	ByLanguage     []categoryLanguageCount
	CapabilityKeys []string // capability columns for this category's bucket
	Records        []categoryRow
}

// detailPageData feeds detail/<id>.md.tmpl.
type detailPageData struct {
	Marker  string
	Record  Record
	CapList []capEntry
}

// recordToView materialises a Record with sorted capability entries so
// templates iterate deterministically.
func recordToView(rec Record) recordView {
	keys := sortedCapKeys(rec.Capabilities)
	list := make([]capEntry, 0, len(keys))
	byKey := make(map[string]Capability, len(keys))
	for _, k := range keys {
		list = append(list, capEntry{Key: k, Cap: rec.Capabilities[k]})
		byKey[k] = rec.Capabilities[k]
	}
	return recordView{
		ID:       rec.ID,
		Category: rec.Category,
		Language: rec.Language,
		Label:    rec.Label,
		Bucket:   bucketOf(rec.Category),
		CapList:  list,
		CapByKey: byKey,
		Digest:   digestStatus(rec.Capabilities),
	}
}

// templateFuncs are the helpers exposed to templates. Kept minimal so
// most rendering logic lives in Go where it can be tested.
var templateFuncs = template.FuncMap{
	"glyph": statusGlyph,
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

	// Sorted record views (registry already sorts records, but be defensive
	// so generate works on any *Registry, not just one fresh from saveRegistry).
	sortedRecs := make([]Record, len(reg.Records))
	copy(sortedRecs, reg.Records)
	sort.Slice(sortedRecs, func(i, j int) bool { return sortedRecs[i].ID < sortedRecs[j].ID })
	allViews := make([]recordView, len(sortedRecs))
	for i, r := range sortedRecs {
		allViews[i] = recordToView(r)
	}

	// Group views per language, per category, and per (language, bucket).
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

	// Sort language names for deterministic iteration. The pivot table
	// lists each language alphabetically; templates do not re-sort.
	langNames := make([]string, 0, len(langSet))
	for n := range langSet {
		langNames = append(langNames, n)
	}
	sort.Strings(langNames)

	// Sort category names.
	catNames := make([]string, 0, len(byCat))
	for n := range byCat {
		catNames = append(catNames, n)
	}
	sort.Strings(catNames)

	// Records within each slice are sorted by label (then ID for stability)
	// per #2725 rendering rules.
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

	// Build summary pivot rows.
	totals := pivotRow{}
	rows := make([]pivotRow, 0, len(langNames)+1)
	for _, n := range langNames {
		buckets := byLangBucket[n]
		row := pivotRow{
			Name:       n,
			Frameworks: len(buckets[BucketFrameworks]),
			Tools:      len(buckets[BucketTools]),
			ORMs:       len(buckets[BucketORMs]),
			Other:      len(buckets[BucketOther]),
		}
		totals.Frameworks += row.Frameworks
		totals.Tools += row.Tools
		totals.ORMs += row.ORMs
		totals.Other += row.Other
		// Language-neutral pseudo-language. Surface it as an explicit
		// "Uncategorized" row at the bottom rather than blending into the
		// alphabetical list (per #2725 spec).
		if n == "multi" || n == "" {
			row.Name = "Uncategorized"
			row.Uncategorized = true
		}
		rows = append(rows, row)
	}
	// Move any Uncategorized rows to the end.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Uncategorized != rows[j].Uncategorized {
			return !rows[i].Uncategorized
		}
		return rows[i].Name < rows[j].Name
	})

	if err := renderToFile(tmpls, "summary.md.tmpl", filepath.Join(root, "summary.md"), summaryData{
		Marker:          doNotEditMarker,
		TotalLanguages:  len(langNames),
		TotalFrameworks: totals.Frameworks,
		TotalTools:      totals.Tools,
		TotalORMs:       totals.ORMs,
		TotalOther:      totals.Other,
		Rows:            rows,
	}); err != nil {
		return err
	}

	// Per-language pages.
	for _, n := range langNames {
		buckets := byLangBucket[n]
		sections := make([]bucketSection, 0, len(bucketOrder))
		for _, b := range bucketOrder {
			recs := buckets[b]
			if len(recs) == 0 {
				continue
			}
			sections = append(sections, bucketSection{
				Name:           b,
				CapabilityKeys: bucketCapabilityKeys(b),
				Records:        recs,
			})
		}
		displayLang := n
		if n == "multi" || n == "" {
			displayLang = "Uncategorized"
		}
		if err := renderToFile(tmpls, "by-language.md.tmpl",
			filepath.Join(root, "by-language", n+".md"),
			languagePageData{
				Marker:     doNotEditMarker,
				Language:   displayLang,
				Frameworks: len(buckets[BucketFrameworks]),
				Tools:      len(buckets[BucketTools]),
				ORMs:       len(buckets[BucketORMs]),
				Other:      len(buckets[BucketOther]),
				Sections:   sections,
			}); err != nil {
			return err
		}
	}

	// Per-category pages.
	for _, n := range catNames {
		recs := byCat[n]
		bucket := bucketOf(n)
		// Per-language banner counts for this category.
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
		// Capability keys: prefer the bucket-wide union for framework/orm/tool
		// pages so the column set is consistent across categories in the
		// same bucket. For Other categories, use the category's own keys
		// (the bucket-wide nil signals "digest only", but per-category
		// pages still benefit from showing the real columns).
		var keys []string
		if bucket == BucketOther {
			keys = make([]string, len(categoryCapabilities[n]))
			copy(keys, categoryCapabilities[n])
			sort.Strings(keys)
		} else {
			keys = bucketCapabilityKeys(bucket)
		}
		// Rows sorted by language then label (already label-sorted; do a
		// stable resort by language).
		rows := make([]categoryRow, 0, len(recs))
		for _, r := range recs {
			rows = append(rows, categoryRow{
				Language: r.Language,
				Label:    r.Label,
				ID:       r.ID,
				CapList:  r.CapList,
				CapByKey: r.CapByKey,
				Digest:   r.Digest,
			})
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Language != rows[j].Language {
				return rows[i].Language < rows[j].Language
			}
			return rows[i].Label < rows[j].Label
		})
		if err := renderToFile(tmpls, "by-category.md.tmpl",
			filepath.Join(root, "by-category", n+".md"),
			categoryPageData{
				Marker:         doNotEditMarker,
				Category:       n,
				Bucket:         bucket,
				Total:          len(recs),
				ByLanguage:     banner,
				CapabilityKeys: keys,
				Records:        rows,
			}); err != nil {
			return err
		}
	}

	// Per-record detail pages.
	for _, rec := range sortedRecs {
		view := recordToView(rec)
		if err := renderToFile(tmpls, "detail.md.tmpl",
			filepath.Join(root, "detail", rec.ID+".md"),
			detailPageData{
				Marker:  doNotEditMarker,
				Record:  rec,
				CapList: view.CapList,
			}); err != nil {
			return err
		}
	}
	return nil
}

// renderToFile executes the named template into a buffer and writes it
// atomically via temp+rename so partial writes never appear on disk.
func renderToFile(tmpls *template.Template, name, path string, data any) error {
	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("execute %s: %w", name, err)
	}
	// Ensure file ends with exactly one trailing newline. Templates may
	// or may not include one; normalising here keeps output stable across
	// template edits.
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
