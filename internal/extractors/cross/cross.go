// Package cross hosts the cross-language extractors and exposes a registry
// of all such extractors so the indexer's Pass 3 (cross-language extraction)
// can iterate over them without hard-coding the list.
//
// Each cross-language extractor lives in a sub-package and registers itself
// against the global extractor registry under a "_cross_<name>" key in its
// init() function. AllExtractors() returns the canonical ordered slice of
// (name, extractor) pairs the indexer should run during Pass 3.
package cross

import (
	"github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/extractors/cross/abibridge"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/consumes_api"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/dbmap"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/deprecation"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/endpoint"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/hierarchy"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/httpclient"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/imports"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/manifest"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/ormlink"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/react_props"
	_ "github.com/cajasmota/archigraph/internal/extractors/cross/testmap"
)

// Entry pairs a stable short name with a registered cross-language extractor.
// The Name field is the suffix after "_cross_" used as the registry key
// (e.g. "imports" -> "_cross_imports") and is what the indexer logs and
// what users target with --skip-pass.
type Entry struct {
	Name      string
	Extractor extractor.Extractor
}

// names is the canonical ordering of cross-language extractors. Run order
// must remain deterministic: Pass 3 results in the graph.json should not
// fluctuate between invocations.
var names = []string{
	"imports",
	"hierarchy",
	"abibridge",
	"httpclient",
	"dbmap",
	"ormlink",
	"react_props",
	"endpoint",
	// consumes_api runs after httpclient + endpoint: it reuses their detection
	// to join same-file client calls → server endpoints into CONSUMES_API edges.
	"consumes_api",
	"manifest",
	"testmap",
	"deprecation",
}

// AllExtractors returns every registered cross-language extractor in a
// stable order. Extractors that fail to resolve in the registry (e.g. if a
// blank import was removed) are silently dropped — the registry is the
// source of truth.
func AllExtractors() []Entry {
	out := make([]Entry, 0, len(names))
	for _, n := range names {
		key := "_cross_" + n
		ext, ok := extractor.Get(key)
		if !ok {
			continue
		}
		out = append(out, Entry{Name: n, Extractor: ext})
	}
	return out
}

// Names returns the ordered list of cross-language extractor short names.
// Used by the CLI to validate --skip-pass entries.
func Names() []string {
	out := make([]string, len(names))
	copy(out, names)
	return out
}
