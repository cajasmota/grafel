// Index-time base-URL constant folding for consumer-side HTTP calls (#6450).
//
// WHY THIS LIVES IN internal/engine AND NOT internal/links
//
// The per-repo call→definition matcher (resolveHTTPEndpointHandlers, this
// package) runs at index/merge time and joins on the `path` property. The
// substrate constant fold used to run only in `grafel group-link`
// (internal/links/constant_propagation.go, applyResolverToConsumerHTTP),
// a different process phase entirely — so the engine matcher structurally
// could never see a folded path, and every `fetch(`${BASE}/things`)` whose
// BASE lives in another file was written to graph.json as an
// UNRESOLVED_FETCH even when the handler sat in the same tree. No gate was
// involved: the two passes simply never met.
//
// Folding is purely intra-repo — the links Resolver's symbol table,
// imports index and file lookup are all repo-keyed and its resolution walk
// never crosses a repo boundary — so nothing is lost by doing it per repo
// at index time, and internal/substrate imports no grafel/internal package,
// so engine → substrate introduces no cycle.
//
// IDENTITY CONTRACT (this is the part that is easy to get wrong)
//
// Only the `path` property is rewritten. `Name` and the entity ID derived
// from it stay frozen at the unfolded canonical form. The matcher's tier 0
// joins on Name and tiers 1-2 on `path`, so a folded path with a frozen
// name matches at tier 1 with ZERO entity-ID churn across incremental
// indexes. Folding at extraction time — i.e. changing Name — would churn
// the ID of every dynamic-base-URL call site on every run.
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/substrate"
	"github.com/cajasmota/grafel/internal/types"
)

// substrateMaxImportDepth bounds the IMPORTS-edge walk when resolving a
// cross-file binding. Mirrors internal/links/constant_propagation.go's
// maxImportDepth: the substrate targets shallow re-export chains.
const substrateMaxImportDepth = 3

// FoldConsumerHTTPBaseURLResult reports what the fold did, for the
// index-time stats line.
type FoldConsumerHTTPBaseURLResult struct {
	// Candidates is the number of http_endpoint_call records carrying
	// url_kind=dynamic_baseurl with a leading `/{ident}` segment.
	Candidates int
	// Folded is the number of those whose leading identifier the substrate
	// resolved to a literal, and whose `path` was therefore rewritten.
	Folded int
	// FilesSniffed is the number of source files the substrate lifted at
	// least one binding from.
	FilesSniffed int
}

// FoldConsumerHTTPBaseURLs rewrites the `path` property of every
// consumer-side http_endpoint_call record whose url_kind is
// "dynamic_baseurl" and whose canonical path starts with a `/{ident}`
// segment that the repo-scoped substrate symbol table can bind to a string
// literal.
//
// srcRoot is the on-disk root of the repo being indexed; source files are
// read relative to it. An empty srcRoot (or one whose files cannot be read)
// makes this a no-op — the fold is strictly best-effort and must never fail
// an index.
//
// MUST be called BEFORE resolveHTTPEndpointHandlers so the matcher's
// path-based tiers see the folded value. Mutates `merged` in place.
//
// Two deliberate limits, both inherited from internal/links.buildResolver
// rather than introduced here:
//
//   - The set of files sniffed is derived from the SourceFile of records in
//     `merged`, so a constants module that produced no entity at all is
//     invisible to the symbol table.
//   - The incremental one-file path (ResolveHTTPEndpointHandlersFileScoped,
//     driven from internal/extractors/incremental.go) does not call this:
//     a one-file slice cannot see the declaring module, so the fold would
//     have nothing to bind against. Those calls keep their unfolded path
//     until the next full index.
func FoldConsumerHTTPBaseURLs(merged []types.EntityRecord, srcRoot string) FoldConsumerHTTPBaseURLResult {
	var res FoldConsumerHTTPBaseURLResult
	if srcRoot == "" || len(merged) == 0 {
		return res
	}

	// Cheap pre-scan: only pay for the substrate sniff when at least one
	// record could possibly be folded. On the overwhelming majority of
	// repos (no dynamic base URLs at all) this costs one pass over the
	// records and nothing else.
	var candidates []int
	for i := range merged {
		r := &merged[i]
		if r.Kind != httpEndpointCallKind || r.Properties == nil {
			continue
		}
		if r.Properties["url_kind"] != "dynamic_baseurl" {
			continue
		}
		if leadingTemplateIdentEngine(r.Properties["path"]) == "" {
			continue
		}
		candidates = append(candidates, i)
	}
	res.Candidates = len(candidates)
	if len(candidates) == 0 {
		return res
	}

	resolver := buildRepoSubstrateResolver(merged, srcRoot)
	if resolver == nil {
		return res
	}
	res.FilesSniffed = len(resolver.bindings)

	for _, i := range candidates {
		r := &merged[i]
		callerFile := r.Properties["caller_file"]
		if callerFile == "" {
			callerFile = r.SourceFile
		}
		path := r.Properties["path"]
		ident := leadingTemplateIdentEngine(path)
		rr := resolver.resolve(callerFile, ident)
		if rr.value == "" {
			continue
		}
		// Substitute and re-classify. Strip any scheme + host so the result
		// lines up with the producer-side path index.
		replaced := stripURLPrefixEngine(rr.value) + path[len("/{"+ident+"}"):]
		if replaced == "" || replaced[0] != '/' {
			replaced = "/" + replaced
		}
		// IDENTITY CONTRACT: `path` only. Name / EntityID stay frozen.
		r.Properties["path"] = replaced
		r.Properties["url_kind"] = "literal"
		r.Properties["substrate_resolved_value"] = rr.value
		r.Properties["substrate_resolved_via"] = strings.Join(rr.steps, ",")
		r.Properties["substrate_confidence"] = fmt.Sprintf("%.2f", rr.confidence)
		// The `dynamic_baseurl` marker is what downstream orphan-caller
		// classification reads to bucket this call as data-flow-runtime.
		// It is no longer true once the base URL is bound.
		delete(r.Properties, "dynamic_baseurl")
		res.Folded++
	}
	return res
}

// repoSubstrateResolver is the repo-scoped twin of
// internal/links.Resolver. The repo dimension is dropped entirely: one
// index run covers exactly one repo, and the links resolver never crossed a
// repo boundary anyway.
type repoSubstrateResolver struct {
	// bindings[file][ident] = Binding — the classical symbol table.
	bindings map[string]map[string]substrate.Binding
	// fileLookup maps a module specifier to the repo-relative file that
	// declares it, under several canonical key forms.
	fileLookup map[string]string
}

// substrateResolution is the reduced result shape the fold needs.
type substrateResolution struct {
	value      string
	confidence float64
	steps      []string
}

// buildRepoSubstrateResolver sniffs every distinct source file referenced
// by `merged` and builds the symbol table. Returns nil when nothing was
// lifted.
func buildRepoSubstrateResolver(merged []types.EntityRecord, srcRoot string) *repoSubstrateResolver {
	fileSet := make(map[string]bool, len(merged))
	for i := range merged {
		if f := merged[i].SourceFile; f != "" {
			fileSet[f] = true
		}
	}
	r := &repoSubstrateResolver{
		bindings:   map[string]map[string]substrate.Binding{},
		fileLookup: map[string]string{},
	}
	for file := range fileSet {
		lang := substrate.LanguageForPath(file)
		if lang == "" {
			continue
		}
		sniff := substrate.SnifferFor(lang)
		if sniff == nil {
			continue
		}
		content, err := os.ReadFile(filepath.Join(srcRoot, file))
		if err != nil {
			// Best-effort: a file that vanished between extraction and
			// merge must not fail the index.
			continue
		}
		bindings := sniff(string(content))
		if len(bindings) == 0 {
			continue
		}
		fileBindings := make(map[string]substrate.Binding, len(bindings))
		for _, b := range bindings {
			// Last-wins on a duplicate ident: the sniffer emits in source
			// order, so the final declaration is the live one.
			fileBindings[b.Ident] = b
		}
		r.bindings[file] = fileBindings
		indexFileForSubstrateLookup(r.fileLookup, file)
	}
	if len(r.bindings) == 0 {
		return nil
	}
	return r
}

// resolve binds ident as seen from file, following at most
// substrateMaxImportDepth cross-file re-export hops.
func (r *repoSubstrateResolver) resolve(file, ident string) substrateResolution {
	return r.resolveDepth(file, ident, 0, map[string]bool{})
}

func (r *repoSubstrateResolver) resolveDepth(file, ident string, depth int, seen map[string]bool) substrateResolution {
	if depth > substrateMaxImportDepth {
		return substrateResolution{}
	}
	key := file + "::" + ident
	if seen[key] {
		return substrateResolution{}
	}
	seen[key] = true

	b, ok := r.bindings[file][ident]
	if !ok {
		return substrateResolution{}
	}
	switch b.Provenance {
	case substrate.ProvenanceLiteral:
		return substrateResolution{
			value:      b.Value,
			confidence: b.Confidence,
			steps:      []string{string(b.Provenance)},
		}
	case substrate.ProvenanceEnvFallback:
		step := string(b.Provenance)
		if b.EnvVar != "" {
			step += ":" + b.EnvVar
		}
		return substrateResolution{
			value:      b.Value,
			confidence: b.Confidence,
			steps:      []string{step},
		}
	case substrate.ProvenanceCrossFile:
		decl := r.fileLookup[b.ImportSource]
		if decl == "" {
			return substrateResolution{}
		}
		upstream := r.resolveDepth(decl, ident, depth+1, seen)
		if upstream.value == "" {
			return substrateResolution{}
		}
		conf := b.Confidence
		if upstream.confidence < conf {
			conf = upstream.confidence
		}
		return substrateResolution{
			value:      upstream.value,
			confidence: conf,
			steps:      append([]string{"import:" + b.ImportSource}, upstream.steps...),
		}
	}
	return substrateResolution{}
}

// indexFileForSubstrateLookup registers file under several canonical key
// forms so an import specifier can be matched back to its source file.
// Conservative: only forms that are still free are added, so a later file
// can never silently shadow an earlier one. Mirrors
// internal/links/constant_propagation.go's indexFileForLookup.
func indexFileForSubstrateLookup(idx map[string]string, file string) {
	ext := filepath.Ext(file)
	base := strings.TrimSuffix(filepath.Base(file), ext)
	dir := filepath.Dir(file)
	addIfFree := func(key string) {
		if key == "" || key == "." {
			return
		}
		if _, exists := idx[key]; exists {
			return
		}
		idx[key] = file
	}
	addIfFree(file)
	addIfFree(strings.TrimSuffix(file, ext))
	addIfFree(base)
	if dir != "." && dir != "" {
		addIfFree(filepath.Join(dir, base))
		addIfFree("./" + filepath.Join(dir, base))
		addIfFree("./" + base)
	} else {
		addIfFree("./" + base)
	}
	dotted := strings.ReplaceAll(strings.TrimSuffix(file, ext), "/", ".")
	addIfFree(dotted)
}

// leadingTemplateIdentEngine returns the bare identifier when path begins
// with `/{ident}/` or `/{ident}`; returns "" otherwise. Identifier
// characters only, so a generic route parameter like `/{id}/x` at the head
// of a path is never mistaken for a base-URL binding (it would simply fail
// to resolve, but rejecting early keeps the intent explicit). Mirrors
// internal/links/constant_propagation.go's leadingTemplateIdent.
func leadingTemplateIdentEngine(path string) string {
	if !strings.HasPrefix(path, "/{") {
		return ""
	}
	rest := path[2:]
	close := strings.IndexByte(rest, '}')
	if close <= 0 {
		return ""
	}
	ident := rest[:close]
	for _, r := range ident {
		if !(r == '_' || r == '$' ||
			(r >= '0' && r <= '9') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z')) {
			return ""
		}
	}
	return ident
}

// stripURLPrefixEngine removes a scheme + host prefix, leaving the path.
// A value with no scheme is returned unchanged. Mirrors
// internal/links/constant_propagation.go's stripURLPrefix.
func stripURLPrefixEngine(s string) string {
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(s, scheme) {
			rest := s[len(scheme):]
			if slash := strings.IndexByte(rest, '/'); slash >= 0 {
				return rest[slash:]
			}
			return ""
		}
	}
	return s
}
