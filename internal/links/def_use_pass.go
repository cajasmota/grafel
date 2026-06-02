// Phase 3C reaching-definitions / def-use chain pass (#2774).
//
// For every (function, use) the pass finds the last preceding def of
// the same variable in the same function and emits a DefUseChain
// record. "Last write wins" — we deliberately skip SSA-phi precision
// per the issue spec; that lives in Phase 4.
//
// Intra-procedural ONLY. Cross-function chains require alias analysis
// and are out of Phase 3C scope.
//
// Per-language sniffing happens once per file via the registered
// substrate.DefUseSnifferFor sniffer. The pass binds each (file, fn)
// pair to its graph entity via the same (repo, file, name) lookup the
// effect propagation pass uses, then stamps a compact summary on the
// entity properties:
//
//	def_use_chains    "<var>@<defLine>->-<useLine>,..." (bounded length)
//	def_use_count     "<n>"
//
// The full chain list is also persisted in <group>-links-def-use.json
// for archigraph_def_use to surface unbounded.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/substrate"
)

// MethodDefUse identifies sidecar artefacts from this pass.
const MethodDefUse = "def_use"

// DefUsePropertyKey* are the stable property keys.
const (
	DefUsePropertyKeyChains = "def_use_chains"
	DefUsePropertyKeyCount  = "def_use_count"
)

// maxPropertyChains bounds the compact summary stamped onto entity
// properties (the full set is in the sidecar JSON). Keeps the per-entity
// property surface small — entities with hundreds of def-use pairs would
// otherwise blow up the graph file size.
const maxPropertyChains = 24

// defUseChain is one resolved (def, use) pair for a single variable.
type defUseChain struct {
	Var     string `json:"var"`
	DefLine int    `json:"def_line"`
	UseLine int    `json:"use_line"`
}

// defUseEntry is one persistent (function, [chain...]) record for the
// sidecar JSON surface.
type defUseEntry struct {
	Repo       string        `json:"repo"`
	EntityID   string        `json:"entity_id"`
	Name       string        `json:"name"`
	SourceFile string        `json:"source_file,omitempty"`
	Chains     []defUseChain `json:"chains"`
}

type defUseDocument struct {
	Version int           `json:"version"`
	Method  string        `json:"method"`
	Total   int           `json:"total_chains"`
	Entries []defUseEntry `json:"entries"`
}

// runDefUsePass walks every T1 source file and computes intra-procedural
// reaching definitions per the substrate.DefUseSnifferFor contract.
func runDefUsePass(graphs []repoGraph, paths Paths) (PassResult, error) {
	res := PassResult{Pass: "def_use"}
	binder := newEffectBinder(graphs) // re-uses the (repo, file, name) index.

	var allEntries []defUseEntry
	totalChains := 0
	scanned := 0

	for ri := range graphs {
		g := &graphs[ri]
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
			sniff := substrate.DefUseSnifferFor(lang)
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
			defs, uses := sniff(string(content))
			if len(uses) == 0 || len(defs) == 0 {
				continue
			}
			// Per-function reaching-defs: group defs by (fn, var) sorted
			// by line, then for each use binary-search the greatest def
			// line strictly ≤ use line.
			defsByFnVar := map[string][]int{}
			for _, d := range defs {
				if d.Function == "" {
					continue
				}
				key := d.Function + "::" + d.Var
				defsByFnVar[key] = append(defsByFnVar[key], d.Line)
			}
			for k := range defsByFnVar {
				sort.Ints(defsByFnVar[k])
			}

			perFn := map[string][]defUseChain{}
			for _, u := range uses {
				if u.Function == "" {
					continue
				}
				key := u.Function + "::" + u.Var
				lines, ok := defsByFnVar[key]
				if !ok {
					continue
				}
				idx := sort.SearchInts(lines, u.Line)
				// idx is the first def-line >= use line; we want strictly
				// less, so step back by one.
				if idx == 0 {
					continue
				}
				defLine := lines[idx-1]
				if defLine >= u.Line {
					continue
				}
				perFn[u.Function] = append(perFn[u.Function], defUseChain{
					Var:     u.Var,
					DefLine: defLine,
					UseLine: u.Line,
				})
			}

			for fn, chains := range perFn {
				if len(chains) == 0 {
					continue
				}
				totalChains += len(chains)
				eid := binder.lookup(g.Repo, file, fn)
				if eid == "" {
					continue
				}
				// Stamp the compact summary onto the entity properties.
				for ei := range g.Entities {
					if g.Entities[ei].ID != eid {
						continue
					}
					e := &g.Entities[ei]
					if e.Properties == nil {
						e.Properties = map[string]string{}
					}
					summary := formatDefUseSummary(chains, maxPropertyChains)
					e.Properties[DefUsePropertyKeyChains] = summary
					e.Properties[DefUsePropertyKeyCount] = fmt.Sprintf("%d", len(chains))
					allEntries = append(allEntries, defUseEntry{
						Repo:       g.Repo,
						EntityID:   eid,
						Name:       e.Name,
						SourceFile: file,
						Chains:     chains,
					})
					break
				}
			}
		}
	}

	res.LinksAdded = totalChains
	res.Candidates = scanned

	if paths.Links == "" {
		return res, nil
	}
	sort.Slice(allEntries, func(i, j int) bool {
		if allEntries[i].Repo != allEntries[j].Repo {
			return allEntries[i].Repo < allEntries[j].Repo
		}
		return allEntries[i].EntityID < allEntries[j].EntityID
	})
	sidecar := trimSuffix(paths.Links, ".json") + "-def-use.json"
	doc := defUseDocument{
		Version: 1,
		Method:  MethodDefUse,
		Total:   totalChains,
		Entries: allEntries,
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return res, err
	}
	if err := os.MkdirAll(filepath.Dir(sidecar), 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(sidecar, buf, 0o644); err != nil {
		return res, fmt.Errorf("write def-use doc: %w", err)
	}
	return res, nil
}

// formatDefUseSummary renders up to max chains as
// "<var>@<defLine>->-<useLine>" comma-joined. Stable iteration so
// graph output is byte-identical across runs.
func formatDefUseSummary(chains []defUseChain, max int) string {
	cp := append([]defUseChain(nil), chains...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].UseLine != cp[j].UseLine {
			return cp[i].UseLine < cp[j].UseLine
		}
		if cp[i].DefLine != cp[j].DefLine {
			return cp[i].DefLine < cp[j].DefLine
		}
		return cp[i].Var < cp[j].Var
	})
	if len(cp) > max {
		cp = cp[:max]
	}
	parts := make([]string, len(cp))
	for i, c := range cp {
		parts[i] = fmt.Sprintf("%s@%d->%d", c.Var, c.DefLine, c.UseLine)
	}
	return strings.Join(parts, ",")
}
