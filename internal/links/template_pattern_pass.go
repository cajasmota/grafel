// Phase 3D template-pattern catalog pass (#2774).
//
// Walks every T1 source file once, dispatches to the registered
// substrate.TemplatePatternSnifferFor sniffer, and persists every
// match in <group>-links-template-patterns.json. Powers:
//   - i18n key extraction
//   - log-format inconsistency surveys
//   - SQL-injection literal cataloguing (overlaps Phase 2B at the
//     literal level — Phase 2B tracks taint flow into the literal;
//     this pass catalogues the literal itself).
//
// In-memory: no entity property is stamped (the template literal is a
// per-call-site fact, not a per-entity fact). The MCP tool reads the
// sidecar directly.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cajasmota/archigraph/internal/substrate"
)

// MethodTemplatePatterns identifies sidecar artefacts from this pass.
const MethodTemplatePatterns = "template_patterns"

// templatePatternEntry is one persistent template-pattern fact.
type templatePatternEntry struct {
	Repo       string `json:"repo"`
	SourceFile string `json:"source_file"`
	Function   string `json:"function,omitempty"`
	Line       int    `json:"line"`
	Kind       string `json:"kind"`
	Tag        string `json:"tag"`
	Literal    string `json:"literal"`
}

type templatePatternDocument struct {
	Version int                    `json:"version"`
	Method  string                 `json:"method"`
	Total   int                    `json:"total"`
	ByKind  map[string]int         `json:"by_kind"`
	Entries []templatePatternEntry `json:"entries"`
}

// runTemplatePatternPass enumerates every template-pattern match in
// every T1 source file across every repo.
func runTemplatePatternPass(graphs []repoGraph, paths Paths) (PassResult, error) {
	res := PassResult{Pass: "template_patterns"}
	var entries []templatePatternEntry
	byKind := map[string]int{}
	scanned := 0

	for _, g := range graphs {
		fileSet := map[string]bool{}
		for _, e := range g.Entities {
			if e.SourceFile != "" {
				fileSet[e.SourceFile] = true
			}
		}
		for file := range fileSet {
			lang := substrate.LanguageForPath(file)
			if lang == "" {
				continue
			}
			sniff := substrate.TemplatePatternSnifferFor(lang)
			if sniff == nil {
				continue
			}
			srcRoot := repoSourcePathFor(g.Repo)
			if srcRoot == "" {
				srcRoot = g.FileRoot
			}
			abs := filepath.Join(srcRoot, file)
			content, err := os.ReadFile(abs)
			if err != nil {
				continue
			}
			scanned++
			for _, tp := range sniff(string(content)) {
				entries = append(entries, templatePatternEntry{
					Repo:       g.Repo,
					SourceFile: file,
					Function:   tp.Function,
					Line:       tp.Line,
					Kind:       string(tp.Kind),
					Tag:        tp.Tag,
					Literal:    tp.Literal,
				})
				byKind[string(tp.Kind)]++
			}
		}
	}

	res.LinksAdded = len(entries)
	res.Candidates = scanned

	if paths.Links == "" {
		return res, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Repo != entries[j].Repo {
			return entries[i].Repo < entries[j].Repo
		}
		if entries[i].SourceFile != entries[j].SourceFile {
			return entries[i].SourceFile < entries[j].SourceFile
		}
		if entries[i].Line != entries[j].Line {
			return entries[i].Line < entries[j].Line
		}
		return entries[i].Kind < entries[j].Kind
	})
	sidecar := trimSuffix(paths.Links, ".json") + "-template-patterns.json"
	doc := templatePatternDocument{
		Version: 1,
		Method:  MethodTemplatePatterns,
		Total:   len(entries),
		ByKind:  byKind,
		Entries: entries,
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return res, err
	}
	if err := os.MkdirAll(filepath.Dir(sidecar), 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(sidecar, buf, 0o644); err != nil {
		return res, fmt.Errorf("write template-patterns doc: %w", err)
	}
	return res, nil
}
