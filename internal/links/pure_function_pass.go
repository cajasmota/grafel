// Phase 3A pure-function tagging pass (#2774).
//
// Derivative of Phase 1A: any function-like entity that the effect
// propagation pass did NOT stamp with an effect set is marked as a
// pure-function candidate. The stamping uses two new property keys:
//
//   pure              "true" | "false"
//   pure_confidence   "0.30"   (matches Phase 1A's "pure" confidence floor;
//                                absence-of-detection does not prove absence)
//
// Why a separate pass instead of inlining into effect_propagation.go?
//   - Effect propagation runs in-memory and stamps only entities with a
//     non-empty effect set. The pure tagging needs to walk every
//     function-like entity (positive AND negative).
//   - Surfacing the tagged set on its own MCP tool keeps the contract
//     small: "list me memoization candidates".
//   - The pass has zero per-language code (it reads effect properties
//     that Phase 1A has already stamped).
//
// The sidecar <group>-links-pure-functions.json is written for the MCP
// archigraph_pure_functions tool.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// MethodPureFunctions identifies sidecar artefacts produced by this pass.
const MethodPureFunctions = "pure_functions"

// Pure-tagging confidence floor — mirrors Phase 1A's pure baseline so
// downstream consumers see one consistent number for "pure" judgements.
const purityConfidence = 0.30

// PurePropertyKey* are the stable property keys this pass stamps onto
// function-like entities. Tests assert their exact spelling.
const (
	PurePropertyKeyPure       = "pure"
	PurePropertyKeyConfidence = "pure_confidence"
)

// pureEntry is one persistent pure-function fact for the sidecar.
type pureEntry struct {
	Repo       string  `json:"repo"`
	EntityID   string  `json:"entity_id"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	SourceFile string  `json:"source_file,omitempty"`
	Confidence float64 `json:"confidence"`
}

// pureDocument is the on-disk shape of <group>-links-pure-functions.json.
type pureDocument struct {
	Version int         `json:"version"`
	Method  string      `json:"method"`
	Total   int         `json:"total"`
	Entries []pureEntry `json:"entries"`
}

// runPureFunctionPass walks every function-like entity in every repo
// and stamps the pure/pure_confidence properties. Entities the effect
// pass already marked with non-empty effects are explicitly stamped
// pure="false" so a downstream reader can distinguish "checked-pure"
// from "never checked" entities.
//
// Stamping happens in-memory on the graphs slice; a sidecar JSON is
// written to <paths.Links>-pure-functions.json for MCP consumption.
func runPureFunctionPass(graphs []repoGraph, paths Paths) (PassResult, error) {
	res := PassResult{Pass: "pure_functions"}
	var entries []pureEntry
	totalPure := 0

	for ri := range graphs {
		g := &graphs[ri]
		for ei := range g.Entities {
			e := &g.Entities[ei]
			if !isFunctionLikeKind(e.Kind) {
				continue
			}
			if e.Properties == nil {
				e.Properties = map[string]string{}
			}
			// If Phase 1A stamped effects, this entity is not pure.
			if eff := e.Properties[EffectPropertyKeyList]; eff != "" {
				e.Properties[PurePropertyKeyPure] = "false"
				continue
			}
			e.Properties[PurePropertyKeyPure] = "true"
			e.Properties[PurePropertyKeyConfidence] = fmt.Sprintf("%.2f", purityConfidence)
			totalPure++
			entries = append(entries, pureEntry{
				Repo:       g.Repo,
				EntityID:   e.ID,
				Name:       e.Name,
				Kind:       e.Kind,
				SourceFile: e.SourceFile,
				Confidence: purityConfidence,
			})
		}
	}

	res.LinksAdded = totalPure

	if paths.Links == "" {
		return res, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Repo != entries[j].Repo {
			return entries[i].Repo < entries[j].Repo
		}
		return entries[i].EntityID < entries[j].EntityID
	})
	sidecar := trimSuffix(paths.Links, ".json") + "-pure-functions.json"
	doc := pureDocument{
		Version: 1,
		Method:  MethodPureFunctions,
		Total:   totalPure,
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
		return res, fmt.Errorf("write pure-functions doc: %w", err)
	}
	return res, nil
}

// trimSuffix removes the trailing suf from s when present. Local helper
// to avoid importing strings just for this; mirrors the inline
// strings.TrimSuffix used by sibling passes.
func trimSuffix(s, suf string) string {
	if len(s) >= len(suf) && s[len(s)-len(suf):] == suf {
		return s[:len(s)-len(suf)]
	}
	return s
}
