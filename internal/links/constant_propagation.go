// Cross-file constant propagation pass (#2761 Phase 0 substrate).
//
// This pass implements the substrate foundation laid out in epic #2716:
// resolve constant bindings (string literals, env-var fallbacks,
// re-exported constants) across files via the IMPORTS edge graph so that
// downstream analyses (HTTP path canonicalisation, taint flow, payload
// shape) can read the resolved value off a single API.
//
// Pipeline:
//
//  1. Per repo, per file, run the registered substrate.SnifferFor(lang)
//     against the raw source content to lift Binding records.
//
//  2. Build an in-memory hash table keyed by (repo, file, ident) → Binding.
//     This is the classical compiler symbol table from #2716. It is
//     transient: rebuilt on every pass run, never persisted.
//
//  3. Resolve cross-file references by walking the IMPORTS edges from
//     each file (max depth 3). When a binding has ProvenanceCrossFile,
//     follow ImportSource through the file's outgoing IMPORTS to the
//     module's source file and look up the same ident there.
//
//  4. Emit one RESOLVES_TO link per resolved cross-file binding and a
//     <group>-resolves-to.json document for downstream consumers
//     (dashboard, MCP, follow-up phase passes). Intra-repo only — the
//     link is intra-graph by design.
//
// Storage model: the propagation pass deliberately does NOT introduce a
// ConstantBinding entity kind. Bindings are decoration on existing
// declaration entities; the cross-file resolution surface is a single
// RelationshipKindResolvesTo edge per resolved use-site. This keeps the
// graph schema and MCP query surface clean.
package links

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/substrate"
	"github.com/cajasmota/archigraph/internal/types"
)

// repoSourcePathOverrides lets the cli layer attach the on-disk source
// directory for each repo slug before runConstantPropagationPass runs.
// loadAllGraphs derives FileRoot from the staged graph directory, which
// resolves to the .archigraph store path — not the source repo. The
// substrate sniffer needs the source repo path to read .ts / .py / .java
// / .go files; this map is the bridge.
//
// Package-level (rather than threaded through RunAllPasses) so the
// public API stays untouched. The CLI sets it once per invocation
// before calling RunAllPasses; subsequent reads pick it up via
// repoSourcePathFor. Concurrent access during a single link run is
// safe because the map is mutated only at the start of the run and
// read-only thereafter.
var repoSourcePathOverrides = map[string]string{}

// SetRepoSourcePaths installs the mapping used by the substrate
// propagation pass to locate source files for each repo slug. The CLI
// calls this once with the group's fleet config before invoking
// RunAllPasses. Pass an empty map (or nil) to clear the table.
func SetRepoSourcePaths(m map[string]string) {
	if m == nil {
		repoSourcePathOverrides = map[string]string{}
		return
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	repoSourcePathOverrides = out
}

// repoSourcePathFor returns the on-disk source-repo path for the given
// repo slug, or "" when the cli has not registered one. Callers must
// fall back to repoGraph.FileRoot in that case.
func repoSourcePathFor(slug string) string {
	if slug == "" {
		return ""
	}
	return repoSourcePathOverrides[slug]
}

// MethodConstantPropagation identifies RESOLVES_TO entries produced by
// the substrate Phase 0 propagation pass. Method-segregated so re-runs
// rewrite only this pass's output and leave other passes' links alone.
const MethodConstantPropagation = "constant_propagation"

// maxImportDepth is the upper bound on the IMPORTS-edge walk depth when
// resolving a ProvenanceCrossFile binding. Per #2716 the substrate
// targets shallow re-export chains; deeper chains are a separate Phase 1+
// concern and exceed Phase 0 scope.
const maxImportDepth = 3

// ConstantResolution is the result of resolving a use-site identifier
// against the substrate symbol table.
//
// Returned by Resolver.Resolve. Empty Value + zero Confidence + nil
// Steps means the resolver could not bind the identifier.
type ConstantResolution struct {
	// Value is the final resolved literal string (after walking any
	// cross-file hops).
	Value string

	// Confidence is the min() confidence across the resolution chain.
	// 1.0 = direct literal, 0.85 = env-fallback, 0.6 baseline per hop
	// for cross-file chains.
	Confidence float64

	// Steps is the provenance chain — one entry per hop, ordered from
	// the use-site outwards. Each step is a short provenance tag
	// (e.g. "literal", "env_fallback:DB_URL", "import:./shared").
	Steps []string

	// DeclaringFile is the repo-relative path of the file that owns the
	// final terminating Binding.
	DeclaringFile string

	// DeclaringRepo is the repo slug that owns DeclaringFile. For
	// intra-repo resolutions this equals the calling repo.
	DeclaringRepo string
}

// Resolver is the runtime surface returned by RunConstantPropagation.
// It carries the per-(repo, file, ident) symbol table and the IMPORTS
// edge graph needed to walk re-exports.
//
// Resolver methods are read-only and safe for concurrent use after
// construction.
type Resolver struct {
	// bindings is the transient symbol table:
	//   bindings[repo][file][ident] = Binding
	bindings map[string]map[string]map[string]substrate.Binding

	// importsByFile is the file-keyed IMPORTS edge graph used to walk
	// cross-file references. Keys are (repo, file) and values are the
	// module specifiers imported by that file (the ImportSource field
	// from a substrate.Binding).
	importsByFile map[string]map[string][]importTarget

	// fileLookup maps (repo, moduleSpec) → repo-relative file path of the
	// module's source file. Populated by indexing existing graph entities
	// that own a SourceFile property whose path matches the spec (after
	// stripping the extension).
	fileLookup map[string]map[string]string
}

// importTarget is one re-export edge: a module specifier (e.g. "./shared",
// "com.example.config", "cmp", "package.mod") plus the local binding name
// it produces in the importing file.
type importTarget struct {
	ModuleSpec string
	LocalIdent string
}

// Resolve returns the ConstantResolution for ident as seen from
// (repo, file). When the binding is in-file it is returned directly;
// when it is cross-file via IMPORTS the resolver walks the import chain
// up to maxImportDepth.
func (r *Resolver) Resolve(repo, file, ident string) ConstantResolution {
	return r.resolveDepth(repo, file, ident, 0, map[string]bool{})
}

func (r *Resolver) resolveDepth(repo, file, ident string, depth int, seen map[string]bool) ConstantResolution {
	if depth > maxImportDepth {
		return ConstantResolution{}
	}
	key := repo + "::" + file + "::" + ident
	if seen[key] {
		return ConstantResolution{}
	}
	seen[key] = true

	fileBindings := r.bindings[repo][file]
	b, ok := fileBindings[ident]
	if !ok {
		return ConstantResolution{}
	}
	switch b.Provenance {
	case substrate.ProvenanceLiteral:
		return ConstantResolution{
			Value:         b.Value,
			Confidence:    b.Confidence,
			Steps:         []string{string(b.Provenance)},
			DeclaringFile: file,
			DeclaringRepo: repo,
		}
	case substrate.ProvenanceEnvFallback:
		step := string(b.Provenance)
		if b.EnvVar != "" {
			step = step + ":" + b.EnvVar
		}
		return ConstantResolution{
			Value:         b.Value,
			Confidence:    b.Confidence,
			Steps:         []string{step},
			DeclaringFile: file,
			DeclaringRepo: repo,
		}
	case substrate.ProvenanceCrossFile:
		// Resolve the imported module spec → declaring file.
		decl := r.fileLookup[repo][b.ImportSource]
		if decl == "" {
			return ConstantResolution{}
		}
		upstream := r.resolveDepth(repo, decl, ident, depth+1, seen)
		if upstream.Value == "" {
			return ConstantResolution{}
		}
		conf := b.Confidence
		if upstream.Confidence < conf {
			conf = upstream.Confidence
		}
		return ConstantResolution{
			Value:         upstream.Value,
			Confidence:    conf,
			Steps:         append([]string{"import:" + b.ImportSource}, upstream.Steps...),
			DeclaringFile: upstream.DeclaringFile,
			DeclaringRepo: upstream.DeclaringRepo,
		}
	}
	return ConstantResolution{}
}

// runConstantPropagationPass is the entry point invoked from RunAllPasses.
// It walks every repo graph, sniffs every supported-language file once,
// builds the symbol table + import index, and emits one RESOLVES_TO link
// per resolved use-site identifier. Returns a PassResult with the link
// count and a Resolver the caller can use to feed downstream passes.
func runConstantPropagationPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, *Resolver, error) {
	res := PassResult{Pass: "constant_propagation"}
	resolver := buildResolver(graphs)
	if resolver == nil {
		return res, nil, nil
	}

	// Emit one Link per file-scope binding that resolved via a cross-file
	// IMPORTS hop. Intra-file bindings need no link — they're already
	// directly resolvable. We dedupe by (repo, file, ident).
	type linkSeed struct {
		repo, file, ident string
		resolution        ConstantResolution
	}
	var seeds []linkSeed
	for repo, byFile := range resolver.bindings {
		for file, byIdent := range byFile {
			for ident, b := range byIdent {
				if b.Provenance != substrate.ProvenanceCrossFile {
					continue
				}
				rr := resolver.Resolve(repo, file, ident)
				if rr.Value == "" {
					continue
				}
				seeds = append(seeds, linkSeed{repo: repo, file: file, ident: ident, resolution: rr})
			}
		}
	}
	sort.Slice(seeds, func(i, j int) bool {
		if seeds[i].repo != seeds[j].repo {
			return seeds[i].repo < seeds[j].repo
		}
		if seeds[i].file != seeds[j].file {
			return seeds[i].file < seeds[j].file
		}
		return seeds[i].ident < seeds[j].ident
	})

	// Build a stable wire encoding. Use synthetic structural IDs of the
	// form `binding:<repo>::<file>::<ident>` for both endpoints; the
	// dashboard / MCP can resolve them back to the underlying entities
	// when one exists, or display them as substrate residues otherwise.
	links := make([]Link, 0, len(seeds))
	for _, s := range seeds {
		src := "binding:" + s.file + "::" + s.ident
		tgt := "binding:" + s.resolution.DeclaringFile + "::" + s.ident
		linkID := MakeID(entityKey(s.repo, src), entityKey(s.resolution.DeclaringRepo, tgt), MethodConstantPropagation)
		if rejects != nil && rejects[linkID] {
			res.Skipped++
			continue
		}
		l := Link{
			ID:           linkID,
			Source:       entityKey(s.repo, src),
			Target:       entityKey(s.resolution.DeclaringRepo, tgt),
			Relation:     string(types.RelationshipKindResolvesTo),
			Method:       MethodConstantPropagation,
			Confidence:   s.resolution.Confidence,
			DiscoveredAt: discoveredAt(),
			Properties: map[string]string{
				"resolved_value": s.resolution.Value,
				"resolved_via":   strings.Join(s.resolution.Steps, ","),
				"confidence":     fmt.Sprintf("%.2f", s.resolution.Confidence),
				"ident":          s.ident,
			},
		}
		links = append(links, l)
	}

	res.LinksAdded = len(links)
	// Persist the links for downstream consumers (dashboard / MCP).
	if paths.Links != "" {
		// Sidecar file beside the main links file so we don't disturb
		// the method-segregated rewrite logic in RunAllPasses.
		sidecar := strings.TrimSuffix(paths.Links, ".json") + "-resolves-to.json"
		if err := writeResolvesToDoc(sidecar, links); err != nil {
			return res, resolver, fmt.Errorf("write resolves-to doc: %w", err)
		}
	}
	return res, resolver, nil
}

// buildResolver constructs the in-memory symbol table + import index from
// the per-repo graphs. Returns nil when no T1-language entities are
// present (the pass is a no-op for unsupported groups).
func buildResolver(graphs []repoGraph) *Resolver {
	r := &Resolver{
		bindings:      map[string]map[string]map[string]substrate.Binding{},
		importsByFile: map[string]map[string][]importTarget{},
		fileLookup:    map[string]map[string]string{},
	}
	totalFiles := 0
	for _, g := range graphs {
		// Collect unique source files referenced by any entity. Use a
		// set so we sniff each file once even when many entities share it.
		fileSet := map[string]bool{}
		for _, e := range g.Entities {
			if e.SourceFile == "" {
				continue
			}
			fileSet[e.SourceFile] = true
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
			// Prefer the explicit source-path override registered by the
			// CLI. The fallback to g.FileRoot only succeeds when the
			// staged graphs dir was constructed with FileRoot pointing at
			// the source repo (e.g. tests that bypass stageGraphsDir).
			srcRoot := repoSourcePathFor(g.Repo)
			if srcRoot == "" {
				srcRoot = g.FileRoot
			}
			abs := filepath.Join(srcRoot, file)
			content, err := os.ReadFile(abs)
			if err != nil {
				// File missing on disk (graph indexed from a prior
				// snapshot, etc.) — skip; the pass is best-effort and
				// must never fail the whole link pipeline.
				continue
			}
			bindings := sniff(string(content))
			if len(bindings) == 0 {
				continue
			}
			totalFiles++
			if r.bindings[g.Repo] == nil {
				r.bindings[g.Repo] = map[string]map[string]substrate.Binding{}
			}
			if r.importsByFile[g.Repo] == nil {
				r.importsByFile[g.Repo] = map[string][]importTarget{}
			}
			if r.fileLookup[g.Repo] == nil {
				r.fileLookup[g.Repo] = map[string]string{}
			}
			fileBindings := map[string]substrate.Binding{}
			for _, b := range bindings {
				// Last-wins on duplicate idents — the sniffer is
				// expected to emit them in source order so the final
				// binding reflects the final declaration in the file.
				fileBindings[b.Ident] = b
				if b.Provenance == substrate.ProvenanceCrossFile {
					r.importsByFile[g.Repo][file] = append(r.importsByFile[g.Repo][file], importTarget{
						ModuleSpec: b.ImportSource,
						LocalIdent: b.Ident,
					})
				}
			}
			r.bindings[g.Repo][file] = fileBindings
			// Index the file in fileLookup under a handful of canonical
			// keys so cross-file resolution can find it by module spec
			// without requiring an exact match with the importer's
			// quoted string. Keys added: full path, basename (no ext),
			// trailing directory + basename.
			indexFileForLookup(r.fileLookup[g.Repo], file)
		}
	}
	if totalFiles == 0 {
		return nil
	}
	return r
}

// indexFileForLookup registers file under several canonical key forms so
// the resolver can match an import spec back to a source file. Conservative:
// we only add forms that are unique on insertion to avoid silently shadowing
// a previously indexed file.
func indexFileForLookup(idx map[string]string, file string) {
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
		// `./foo` and `foo` import-spec normalisation.
		addIfFree("./" + filepath.Join(dir, base))
		addIfFree("./" + base)
	} else {
		addIfFree("./" + base)
	}
	// Dotted module-path form (Python / Java).
	dotted := strings.ReplaceAll(strings.TrimSuffix(file, ext), "/", ".")
	addIfFree(dotted)
}

// applyResolverToConsumerHTTP rewrites the `path` property of every
// consumer-side http_endpoint_call entity whose url_kind is
// "dynamic_baseurl" by substituting the leading `/{ident}` placeholder
// with the substrate-resolved literal value (when one is available).
//
// Returns the number of entities mutated. Safe to call with a nil
// Resolver — the function is a no-op when the substrate produced no
// bindings.
//
// The rewrite mutates only the in-memory entityNode.Properties map; the
// per-repo graph.fb / graph.json files on disk are left untouched.
// Downstream link passes consume the in-memory state, so the substrate
// lift propagates without persisting the rewritten path back to disk.
func applyResolverToConsumerHTTP(graphs []repoGraph, resolver *Resolver) int {
	if resolver == nil {
		return 0
	}
	mutated := 0
	for ri := range graphs {
		g := &graphs[ri]
		for ei := range g.Entities {
			e := &g.Entities[ei]
			if !isHTTPEndpointLink(e.Kind) {
				continue
			}
			if e.Properties == nil {
				continue
			}
			if e.Properties["url_kind"] != "dynamic_baseurl" {
				continue
			}
			callerFile := e.Properties["caller_file"]
			if callerFile == "" {
				callerFile = e.SourceFile
			}
			path := e.Properties["path"]
			if path == "" {
				continue
			}
			ident := leadingTemplateIdent(path)
			if ident == "" {
				continue
			}
			rr := resolver.Resolve(g.Repo, callerFile, ident)
			if rr.Value == "" {
				continue
			}
			// Substitute and re-classify. Strip any URL scheme + host so
			// the result lines up with the producer-side path index.
			replaced := stripURLPrefix(rr.Value) + path[len("/{"+ident+"}"):]
			if replaced == "" || replaced[0] != '/' {
				replaced = "/" + replaced
			}
			e.Properties["path"] = replaced
			e.Properties["url_kind"] = "literal"
			e.Properties["substrate_resolved_value"] = rr.Value
			e.Properties["substrate_resolved_via"] = joinSteps(rr.Steps)
			e.Properties["substrate_confidence"] = fmt.Sprintf("%.2f", rr.Confidence)
			mutated++
		}
	}
	return mutated
}

// leadingTemplateIdent returns the bare identifier name when path begins
// with `/{ident}/` or `/{ident}` (no trailing slash); returns "" when the
// leading segment is not a single-identifier template placeholder.
func leadingTemplateIdent(path string) string {
	if !strings.HasPrefix(path, "/{") {
		return ""
	}
	rest := path[2:]
	close := strings.IndexByte(rest, '}')
	if close <= 0 {
		return ""
	}
	ident := rest[:close]
	// Conservative: identifier characters only (letters, digits, underscore,
	// dollar). Reject anything else so we never substitute on a generic
	// path-parameter placeholder like `{id}`.
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

// stripURLPrefix removes the protocol + host prefix from a fully-qualified
// URL, leaving the path component. URLs without a scheme are returned
// unchanged.
func stripURLPrefix(s string) string {
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

// joinSteps is a small helper used by the consumer-HTTP rewriter to
// serialise a Steps slice into a comma-joined provenance trace.
func joinSteps(steps []string) string { return strings.Join(steps, ",") }

// resolvesToDocument is the on-disk shape of <group>-links-resolves-to.json.
type resolvesToDocument struct {
	Version int    `json:"version"`
	Links   []Link `json:"links"`
}

func writeResolvesToDoc(path string, links []Link) error {
	doc := resolvesToDocument{Version: 1, Links: links}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}
