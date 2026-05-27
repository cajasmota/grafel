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
const doNotEditMarker = "<!-- DO NOT EDIT — generated from docs/coverage.json by 'go run ./tools/coverage gen' -->"

//go:embed templates/*.tmpl
var templateFS embed.FS

// loadTemplates parses the four embedded markdown templates. Templates
// are parsed once and reused per render; parsing here keeps generate.go
// free of init-time side effects.
func loadTemplates() (*template.Template, error) {
	root := template.New("coverage")
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
	CapList  []capEntry
}

// languageView is one row of the by-language summary.
type languageView struct {
	Name    string
	Stats   LanguageStats
	PctFull string
}

// categoryView is one row of the by-category summary.
type categoryView struct {
	Name  string
	Count int
}

// summaryData feeds summary.md.tmpl.
type summaryData struct {
	Marker     string
	Stats      Stats
	Languages  []languageView
	Categories []categoryView
	Records    []recordView
}

// languagePageData feeds by-language/<lang>.md.tmpl.
type languagePageData struct {
	Marker   string
	Language string
	Stats    LanguageStats
	Records  []recordView
}

// categoryPageData feeds by-category/<cat>.md.tmpl.
type categoryPageData struct {
	Marker         string
	Category       string
	Count          int
	CapabilityKeys []string
	Records        []recordView
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
	for _, k := range keys {
		list = append(list, capEntry{Key: k, Cap: rec.Capabilities[k]})
	}
	return recordView{
		ID:       rec.ID,
		Category: rec.Category,
		Language: rec.Language,
		Label:    rec.Label,
		CapList:  list,
	}
}

// pctFull renders the full-capability percentage as a string. Returns
// "0%" when the language has no capability cells (avoids divide-by-zero
// and keeps output deterministic across registries).
func pctFull(ls LanguageStats) string {
	total := ls.Full + ls.Partial + ls.Missing + ls.NotAppl
	if total == 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", (ls.Full*100)/total)
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
	stats := computeStats(reg)

	// Pre-build sorted language and category lists.
	langNames := make([]string, 0, len(stats.ByLanguage))
	for k := range stats.ByLanguage {
		langNames = append(langNames, k)
	}
	sort.Strings(langNames)
	catNames := make([]string, 0, len(stats.ByCategory))
	for k := range stats.ByCategory {
		catNames = append(catNames, k)
	}
	sort.Strings(catNames)

	// Sorted record views (registry already sorts records, but be defensive
	// so generate works on any *Registry, not just one fresh from saveRegistry).
	sortedRecs := make([]Record, len(reg.Records))
	copy(sortedRecs, reg.Records)
	sort.Slice(sortedRecs, func(i, j int) bool { return sortedRecs[i].ID < sortedRecs[j].ID })
	allViews := make([]recordView, len(sortedRecs))
	for i, r := range sortedRecs {
		allViews[i] = recordToView(r)
	}

	// Group views per language and per category for slice pages.
	byLang := map[string][]recordView{}
	byCat := map[string][]recordView{}
	for _, v := range allViews {
		byLang[v.Language] = append(byLang[v.Language], v)
		byCat[v.Category] = append(byCat[v.Category], v)
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

	// Render summary.
	languages := make([]languageView, 0, len(langNames))
	for _, n := range langNames {
		ls := stats.ByLanguage[n]
		languages = append(languages, languageView{Name: n, Stats: ls, PctFull: pctFull(ls)})
	}
	categories := make([]categoryView, 0, len(catNames))
	for _, n := range catNames {
		categories = append(categories, categoryView{Name: n, Count: stats.ByCategory[n]})
	}
	if err := renderToFile(tmpls, "summary.md.tmpl", filepath.Join(root, "summary.md"), summaryData{
		Marker:     doNotEditMarker,
		Stats:      stats,
		Languages:  languages,
		Categories: categories,
		Records:    allViews,
	}); err != nil {
		return err
	}

	// Per-language pages.
	for _, n := range langNames {
		recs := byLang[n]
		if err := renderToFile(tmpls, "by-language.md.tmpl",
			filepath.Join(root, "by-language", n+".md"),
			languagePageData{
				Marker:   doNotEditMarker,
				Language: n,
				Stats:    stats.ByLanguage[n],
				Records:  recs,
			}); err != nil {
			return err
		}
	}

	// Per-category pages.
	for _, n := range catNames {
		recs := byCat[n]
		keys := make([]string, len(categoryCapabilities[n]))
		copy(keys, categoryCapabilities[n])
		sort.Strings(keys)
		if err := renderToFile(tmpls, "by-category.md.tmpl",
			filepath.Join(root, "by-category", n+".md"),
			categoryPageData{
				Marker:         doNotEditMarker,
				Category:       n,
				Count:          stats.ByCategory[n],
				CapabilityKeys: keys,
				Records:        recs,
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
