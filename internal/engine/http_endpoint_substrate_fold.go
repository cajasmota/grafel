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
//
// INCREMENTAL CORRECTNESS (#6450 review, blocking finding 1)
//
// On an incremental re-index `merged` holds ONLY the re-extracted files. A
// first cut derived the symbol table's file set from `merged` alone, which
// meant an unrelated edit to the CALLER file left the DECLARING module
// invisible: the fold silently stopped firing and every previously-resolved
// FETCHES reverted to UNRESOLVED_FETCH, and stayed reverted until the next
// full index. Regressing a correct graph on an unrelated edit is worse than
// never folding at all, so the file set is now seeded from the
// carried-forward prior-graph entities as well as from `merged`. See
// TestFoldConsumerHTTPBaseURLs_IncrementalKeepsFolding_6450 and the
// end-to-end cmd/grafel incremental test.
//
// COST (#6450 review, blocking finding 2b)
//
// The first cut sniffed EVERY substrate-language file in the repo the
// moment a single candidate existed: measured at +12.3s on a 7,916-file
// repo for candidates=1 folded=0, against a 4.7s whole-repo index. Sniffing
// is now LAZY — a file is read only when a resolution chain actually
// reaches it — and capped at substrateMaxFileReads per run. Building the
// module→file lookup costs map inserts only, no I/O. Reads go through
// internal/safeio (non-blocking open, FIFO-behind-symlink safe, size
// capped), because this pass re-opens by path AFTER the walk's
// irregular-file filter has already run and must not reintroduce that hole.
package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cajasmota/grafel/internal/safeio"
	"github.com/cajasmota/grafel/internal/substrate"
	"github.com/cajasmota/grafel/internal/types"
)

// substrateMaxImportDepth bounds the IMPORTS-edge walk when resolving a
// cross-file binding. Mirrors internal/links/constant_propagation.go's
// maxImportDepth: the substrate targets shallow re-export chains.
const substrateMaxImportDepth = 3

// substrateMaxFileReads caps how many source files one index run may sniff
// for this pass. Each candidate costs one read for its caller file plus at
// most substrateMaxImportDepth reads to walk the re-export chain, and
// caller files are shared across candidates, so this is generous for any
// plausible repo while making the pass's worst case a constant rather than
// a function of repo size. At the ~1.5ms/file measured sniff cost the cap
// is worth roughly 0.4s, against the 12.3s the unbounded version cost.
const substrateMaxFileReads = 256

// substrateMaxFileBytes caps a single sniffed file. Base-URL constants live
// in small modules; a 1 MiB ceiling is far above any real one and stops a
// pathological or adversarial file from being read whole. It is also the
// bound safeio needs, since a character device never reaches EOF.
const substrateMaxFileBytes int64 = 1 << 20

// FoldConsumerHTTPBaseURLResult reports what the fold did, for the
// index-time stats line.
type FoldConsumerHTTPBaseURLResult struct {
	// Candidates is the number of http_endpoint_call records carrying
	// url_kind=dynamic_baseurl with a leading `/{ident}` segment.
	Candidates int
	// Folded is the number of those whose leading identifier the substrate
	// resolved to a literal, and whose `path` was therefore rewritten.
	Folded int
	// FilesSniffed is the number of source files actually read and sniffed.
	// Bounded by substrateMaxFileReads.
	FilesSniffed int
	// FilesIndexed is the number of files registered in the module→file
	// lookup. No I/O — this is the search space, not the read count.
	FilesIndexed int
	// ReadCapHit is true when the run stopped sniffing because it reached
	// substrateMaxFileReads. Surfaced so an unfolded call on a huge repo is
	// diagnosable rather than mysterious.
	ReadCapHit bool
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
// carriedForward carries the prior-graph entities an incremental run splices
// back in for unchanged files. It contributes SourceFile paths to the
// module→file lookup and nothing else; pass nil on a full index. Without it
// an incremental run cannot see the declaring module and un-folds paths a
// previous full index got right.
//
// MUST be called BEFORE resolveHTTPEndpointHandlers so the matcher's
// path-based tiers see the folded value. Mutates `merged` in place.
//
// One deliberate limit remains: the incremental ONE-FILE path
// (ResolveHTTPEndpointHandlersFileScoped, driven from
// internal/extractors/incremental.go) does not call this at all. That slice
// is a single re-extracted file with no carried-forward companion, so there
// is nothing to bind against. Those records keep their unfolded path until
// the enclosing full/incremental index runs — which, unlike the bug above,
// leaves a previously-correct graph alone rather than regressing it.
func FoldConsumerHTTPBaseURLs(merged []types.EntityRecord, srcRoot string, carriedForward []types.EntityRecord) FoldConsumerHTTPBaseURLResult {
	var res FoldConsumerHTTPBaseURLResult
	if srcRoot == "" || len(merged) == 0 {
		return res
	}

	// Cheap pre-scan: only pay for the lookup index when at least one record
	// could possibly be folded. On the overwhelming majority of repos (no
	// dynamic base URLs at all) this costs one pass over the records and
	// nothing else — no I/O, no map building.
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

	resolver := newRepoSubstrateResolver(srcRoot, merged, carriedForward)
	res.FilesIndexed = len(resolver.fileLookup)
	if len(resolver.fileLookup) == 0 {
		return res
	}

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
	res.FilesSniffed = resolver.reads
	res.ReadCapHit = resolver.capHit
	return res
}

// repoSubstrateResolver is the repo-scoped twin of
// internal/links.Resolver. Two differences, both deliberate:
//
//   - The repo dimension is dropped. One index run covers exactly one repo,
//     and the links resolver never crossed a repo boundary anyway.
//   - Sniffing is LAZY. fileLookup is built from paths alone (no I/O); a
//     file's bindings are lifted the first time a resolution reaches it,
//     under a hard read cap. The eager version's cost was linear in repo
//     size for every run that had a single candidate.
type repoSubstrateResolver struct {
	srcRoot string

	// fileLookup maps a module specifier to the repo-relative file that
	// declares it, under several canonical key forms. Paths only — built
	// without reading anything.
	fileLookup map[string]string

	// bindings[file][ident] is the symbol table, filled in on demand.
	bindings map[string]map[string]substrate.Binding
	// sniffed records that a file has been visited, so a file that yielded
	// no bindings is not re-read on every candidate.
	sniffed map[string]bool

	reads  int
	capHit bool
}

// substrateResolution is the reduced result shape the fold needs.
type substrateResolution struct {
	value      string
	confidence float64
	steps      []string
}

// newRepoSubstrateResolver registers every substrate-language source file
// referenced by `merged` or `carriedForward` in the module→file lookup.
// Reads nothing.
func newRepoSubstrateResolver(srcRoot string, merged, carriedForward []types.EntityRecord) *repoSubstrateResolver {
	r := &repoSubstrateResolver{
		srcRoot:    srcRoot,
		fileLookup: map[string]string{},
		bindings:   map[string]map[string]substrate.Binding{},
		sniffed:    map[string]bool{},
	}
	seen := make(map[string]bool, len(merged))
	add := func(recs []types.EntityRecord) {
		for i := range recs {
			file := recs[i].SourceFile
			if file == "" || seen[file] {
				continue
			}
			seen[file] = true
			if substrate.LanguageForPath(file) == "" {
				continue
			}
			indexFileForSubstrateLookup(r.fileLookup, file)
		}
	}
	add(merged)
	add(carriedForward)
	return r
}

// bindingsFor returns the symbol table for one file, sniffing it on first
// use. Returns nil once the read cap is reached.
func (r *repoSubstrateResolver) bindingsFor(file string) map[string]substrate.Binding {
	if b, ok := r.bindings[file]; ok {
		return b
	}
	if r.sniffed[file] {
		return nil
	}
	if r.reads >= substrateMaxFileReads {
		r.capHit = true
		return nil
	}
	r.sniffed[file] = true

	lang := substrate.LanguageForPath(file)
	if lang == "" {
		return nil
	}
	sniff := substrate.SnifferFor(lang)
	if sniff == nil {
		return nil
	}
	// safeio, not os.ReadFile: this pass re-opens by path after the walk's
	// irregular-file filter has already run, so a FIFO or device behind a
	// symlink named `config.js` would otherwise park the whole index in
	// open(2) forever (#6416).
	content, err := safeio.ReadFile(filepath.Join(r.srcRoot, file), safeio.FollowSymlinks, substrateMaxFileBytes)
	r.reads++
	if err != nil {
		// Best-effort: a file that vanished, is not a regular file, or would
		// block must not fail the index.
		return nil
	}
	lifted := sniff(string(content))
	if len(lifted) == 0 {
		return nil
	}
	fileBindings := make(map[string]substrate.Binding, len(lifted))
	for _, b := range lifted {
		// Last-wins on a duplicate ident: the sniffer emits in source order,
		// so the final declaration is the live one.
		fileBindings[b.Ident] = b
	}
	r.bindings[file] = fileBindings
	return fileBindings
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

	b, ok := r.bindingsFor(file)[ident]
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
