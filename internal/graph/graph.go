// Package graph defines the public on-disk schema produced by `grafel index`.
// The schema is stable and versioned; downstream tools (graph loaders,
// MCP servers, viewers) consume graph.json files written by this package.
package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SchemaVersion is the integer version of the on-disk graph.json schema.
// Bump when making a backwards-incompatible change.
const SchemaVersion = 1

// Document is the top-level structure written to <repo>/.grafel/graph.json.
type Document struct {
	Version        int               `json:"version"`
	GeneratedAt    time.Time         `json:"generated_at"`
	Repo           string            `json:"repo"`
	IndexerVersion string            `json:"indexer_version"`
	Stats          Stats             `json:"stats"`
	Entities       []Entity          `json:"entities"`
	Relationships  []Relationship    `json:"relationships"`
	Communities    []CommunityResult `json:"communities,omitempty"`
	SurpriseEdges  []SurpriseEdge    `json:"surprise_edges,omitempty"`
	AlgorithmStats *AlgorithmStats   `json:"algorithm_stats,omitempty"`

	// Phase 0 git metadata (#2088). Populated at index time by
	// internal/gitmeta.Capture. Empty/false for non-git repos or when the
	// graph was loaded from an older graph.fb written before this field was
	// added (FlatBuffers defaults to "" / false for missing fields).
	IndexedRef string `json:"indexed_ref,omitempty"`
	IndexedSHA string `json:"indexed_sha,omitempty"`
	IsWorktree bool   `json:"is_worktree,omitempty"`

	// CoverageStatus indicates whether the indexed working tree is a full
	// or partial checkout (#2181 / M4 of monorepo epic #2175).
	//
	// Values (see internal/gitmeta constants):
	//   ""        — field absent / legacy graph (treated as "full" by readers).
	//   "full"    — normal full checkout; all tracked files are present.
	//   "partial" — git sparse-checkout is active; only a subset of paths
	//               was indexed. Readers should surface a badge in the UI.
	CoverageStatus string `json:"coverage_status,omitempty"`
}

// Stats summarises a Document.
type Stats struct {
	Files         int `json:"files"`
	Entities      int `json:"entities"`
	Relationships int `json:"relationships"`
}

// Entity is a single node in the graph.
type Entity struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	QualifiedName string                 `json:"qualified_name,omitempty"`
	Kind          string                 `json:"kind"`
	Subtype       string                 `json:"subtype,omitempty"`
	SourceFile    string                 `json:"source_file"`
	StartLine     int                    `json:"start_line"`
	EndLine       int                    `json:"end_line"`
	Language      string                 `json:"language"`
	Signature     string                 `json:"signature,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`

	// properties is intentionally unexported (#5851-class resident-memory
	// slice-vs-map refactor): all access — from every package, including
	// this one — MUST go through the Prop* accessors below (PropGet,
	// PropLookup, PropSet, PropDelete, PropRange, PropLen, PropsSnapshot,
	// WithProperties, PropsReplace) rather than the field directly. The wire
	// JSON key stays "properties" via MarshalJSON/UnmarshalJSON below.
	//
	// Backing representation (Phase B, #5850): a sorted-by-key []propKV
	// slice rather than a map[string]string. A Go map has ~48-80 bytes of
	// bucket/hmap overhead per entry regardless of key/value size; at
	// corpus scale (millions of small property sets, often 1-3 entries)
	// this overhead dominates the actual content. A sorted slice has none
	// of that — just N*(len(K)+len(V)+16) for the propKV headers — and
	// still supports O(log n) point lookups via binary search. nil when
	// empty (same zero-value-safe semantics as the old nil map).
	properties []propKV

	// PH8 (#2100): content-hash pointer into the shared embedding cache.
	// When non-empty, readers load the vector from Cache instead of
	// computing it inline. Old graphs have this absent; omitempty preserves
	// byte-identical output for graphs written before PH8.
	EmbeddingRef string `json:"embedding_ref,omitempty"`

	// Pass 4 (graph algorithm) attributes. Pointers + omitempty so that
	// documents written with --skip-pass=graph-algo stay byte-identical to
	// the pre-PORT-4 schema.
	CommunityID        *int     `json:"community_id,omitempty"`
	Centrality         *float64 `json:"centrality,omitempty"`
	PageRank           *float64 `json:"pagerank,omitempty"`
	IsGodNode          bool     `json:"is_god_node,omitempty"`
	IsSurpriseEndpoint bool     `json:"is_surprise_endpoint,omitempty"`
	IsArticulationPt   bool     `json:"is_articulation_point,omitempty"`

	// Confidence overlay (Phase 1C, #2769). Value in [0.0, 1.0]; zero/unset
	// reads as 1.0 (direct AST extraction). See internal/types/confidence.go
	// for the universal taxonomy and propagation rules.
	Confidence float64 `json:"confidence,omitempty"`
}

// Relationship is a directed edge between entities.
type Relationship struct {
	ID     string `json:"id"`
	FromID string `json:"from_id"`
	ToID   string `json:"to_id"`
	Kind   string `json:"kind"`
	// properties is intentionally unexported — see Entity.properties above
	// for the full rationale (including the Phase B []propKV backing).
	// Access only via the Prop* accessors.
	properties []propKV
	// Confidence overlay (Phase 1C, #2769). Value in [0.0, 1.0]; zero reads
	// as 1.0. See internal/types/confidence.go.
	Confidence float64 `json:"confidence,omitempty"`
}

// entityJSON is the wire shape for Entity, mirroring its field set exactly
// but with the properties map exported so encoding/json can see it. Used by
// Entity's MarshalJSON/UnmarshalJSON to keep the "properties" JSON key byte
// -identical to the pre-refactor map-backed field while the Go field itself
// stays unexported.
type entityJSON struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	QualifiedName      string                 `json:"qualified_name,omitempty"`
	Kind               string                 `json:"kind"`
	Subtype            string                 `json:"subtype,omitempty"`
	SourceFile         string                 `json:"source_file"`
	StartLine          int                    `json:"start_line"`
	EndLine            int                    `json:"end_line"`
	Language           string                 `json:"language"`
	Signature          string                 `json:"signature,omitempty"`
	Tags               []string               `json:"tags,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
	Properties         map[string]string      `json:"properties,omitempty"`
	EmbeddingRef       string                 `json:"embedding_ref,omitempty"`
	CommunityID        *int                   `json:"community_id,omitempty"`
	Centrality         *float64               `json:"centrality,omitempty"`
	PageRank           *float64               `json:"pagerank,omitempty"`
	IsGodNode          bool                   `json:"is_god_node,omitempty"`
	IsSurpriseEndpoint bool                   `json:"is_surprise_endpoint,omitempty"`
	IsArticulationPt   bool                   `json:"is_articulation_point,omitempty"`
	Confidence         float64                `json:"confidence,omitempty"`
}

// MarshalJSON emits the same wire shape as the original map-backed
// Properties field (key "properties", omitted when empty).
func (e Entity) MarshalJSON() ([]byte, error) {
	return json.Marshal(entityJSON{
		ID:                 e.ID,
		Name:               e.Name,
		QualifiedName:      e.QualifiedName,
		Kind:               e.Kind,
		Subtype:            e.Subtype,
		SourceFile:         e.SourceFile,
		StartLine:          e.StartLine,
		EndLine:            e.EndLine,
		Language:           e.Language,
		Signature:          e.Signature,
		Tags:               e.Tags,
		Metadata:           e.Metadata,
		Properties:         e.PropsSnapshot(),
		EmbeddingRef:       e.EmbeddingRef,
		CommunityID:        e.CommunityID,
		Centrality:         e.Centrality,
		PageRank:           e.PageRank,
		IsGodNode:          e.IsGodNode,
		IsSurpriseEndpoint: e.IsSurpriseEndpoint,
		IsArticulationPt:   e.IsArticulationPt,
		Confidence:         e.Confidence,
	})
}

// UnmarshalJSON decodes the wire shape written by MarshalJSON.
func (e *Entity) UnmarshalJSON(data []byte) error {
	var aux entityJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	e.ID = aux.ID
	e.Name = aux.Name
	e.QualifiedName = aux.QualifiedName
	e.Kind = aux.Kind
	e.Subtype = aux.Subtype
	e.SourceFile = aux.SourceFile
	e.StartLine = aux.StartLine
	e.EndLine = aux.EndLine
	e.Language = aux.Language
	e.Signature = aux.Signature
	e.Tags = aux.Tags
	e.Metadata = aux.Metadata
	e.PropsReplace(aux.Properties)
	e.EmbeddingRef = aux.EmbeddingRef
	e.CommunityID = aux.CommunityID
	e.Centrality = aux.Centrality
	e.PageRank = aux.PageRank
	e.IsGodNode = aux.IsGodNode
	e.IsSurpriseEndpoint = aux.IsSurpriseEndpoint
	e.IsArticulationPt = aux.IsArticulationPt
	e.Confidence = aux.Confidence
	return nil
}

// relationshipJSON is the wire shape for Relationship. See entityJSON.
type relationshipJSON struct {
	ID         string            `json:"id"`
	FromID     string            `json:"from_id"`
	ToID       string            `json:"to_id"`
	Kind       string            `json:"kind"`
	Properties map[string]string `json:"properties,omitempty"`
	Confidence float64           `json:"confidence,omitempty"`
}

// MarshalJSON emits the same wire shape as the original map-backed
// Properties field (key "properties", omitted when empty).
func (r Relationship) MarshalJSON() ([]byte, error) {
	return json.Marshal(relationshipJSON{
		ID:         r.ID,
		FromID:     r.FromID,
		ToID:       r.ToID,
		Kind:       r.Kind,
		Properties: r.PropsSnapshot(),
		Confidence: r.Confidence,
	})
}

// UnmarshalJSON decodes the wire shape written by MarshalJSON.
func (r *Relationship) UnmarshalJSON(data []byte) error {
	var aux relationshipJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.ID = aux.ID
	r.FromID = aux.FromID
	r.ToID = aux.ToID
	r.Kind = aux.Kind
	r.PropsReplace(aux.Properties)
	r.Confidence = aux.Confidence
	return nil
}

// EntityID computes a stable 16-char hex id from a repo tag and an entity's
// identity fields (kind + name + source file).
func EntityID(repo, kind, name, sourceFile string) string {
	h := sha256.New()
	h.Write([]byte(repo))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write([]byte(sourceFile))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// RelationshipID computes a stable 16-char hex id for an edge.
func RelationshipID(fromID, toID, kind string) string {
	h := sha256.New()
	h.Write([]byte(fromID))
	h.Write([]byte{0})
	h.Write([]byte(toID))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ---------------------------------------------------------------------------
// Entity/Relationship property accessors.
//
// Properties used to be a plain `map[string]string` field. It is now backed
// internally by a compact structure and only reachable through these
// accessors, so callers outside this package can no longer read/write the
// field directly — every access goes through PropGet / PropLookup / PropSet
// / PropDelete / PropRange / PropLen / PropsSnapshot / WithProperties /
// PropsReplace. This keeps the public behaviour (miss -> "", nil-safe
// range/len, JSON wire shape) identical to the old map while letting the
// backing representation change independently.
// ---------------------------------------------------------------------------

// propKV is one key/value pair in a sorted-by-key property slice. See the
// property-accessor doc comment above Entity.properties for the rationale
// (Phase B, #5850): this replaces a map[string]string backing to eliminate
// Go hmap/bucket overhead, which dominates resident memory at corpus scale
// for the common case of small (1-3 entry) property sets.
type propKV struct {
	K string
	V string
}

// propFind returns the insertion/match index of key in props (sorted by K)
// via binary search, and whether it was found. O(log n).
func propFind(props []propKV, key string) (int, bool) {
	i := sort.Search(len(props), func(i int) bool { return props[i].K >= key })
	if i < len(props) && props[i].K == key {
		return i, true
	}
	return i, false
}

// propsFromMap builds a sorted []propKV from a map[string]string, or nil if
// m is empty. Used by WithProperties/PropsReplace, which keep accepting a
// plain map at their call boundary so existing call sites are unaffected.
func propsFromMap(m map[string]string) []propKV {
	if len(m) == 0 {
		return nil
	}
	out := make([]propKV, 0, len(m))
	for k, v := range m {
		out = append(out, propKV{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}

// propsToMap converts a sorted []propKV back into an independent
// map[string]string, or nil if empty.
func propsToMap(props []propKV) map[string]string {
	if len(props) == 0 {
		return nil
	}
	out := make(map[string]string, len(props))
	for _, kv := range props {
		out[kv.K] = kv.V
	}
	return out
}

// propSet inserts or updates key=val in props (sorted by K), returning the
// (possibly reallocated) slice.
func propSet(props []propKV, key, val string) []propKV {
	i, ok := propFind(props, key)
	if ok {
		props[i].V = val
		return props
	}
	props = append(props, propKV{})
	copy(props[i+1:], props[i:])
	props[i] = propKV{K: key, V: val}
	return props
}

// propDelete removes key from props (sorted by K), if present, returning
// the (possibly shortened) slice.
func propDelete(props []propKV, key string) []propKV {
	i, ok := propFind(props, key)
	if !ok {
		return props
	}
	return append(props[:i], props[i+1:]...)
}

// EntityPtr returns a pointer to a copy of e. Used by call sites that build
// an Entity value (e.g. via a chained WithProperties call) but need a
// *Entity, since taking the address of a method-call result is not legal Go.
func EntityPtr(e Entity) *Entity { return &e }

// RelationshipPtr returns a pointer to a copy of r. See EntityPtr.
func RelationshipPtr(r Relationship) *Relationship { return &r }

// PropGet returns the value for key, or "" if key is absent.
func (e Entity) PropGet(key string) string {
	if i, ok := propFind(e.properties, key); ok {
		return e.properties[i].V
	}
	return ""
}

// PropLookup returns the value for key and whether it was present.
func (e Entity) PropLookup(key string) (string, bool) {
	if i, ok := propFind(e.properties, key); ok {
		return e.properties[i].V, true
	}
	return "", false
}

// PropSet sets key to val, lazily initializing the backing storage.
func (e *Entity) PropSet(key, val string) {
	e.properties = propSet(e.properties, key, val)
}

// PropDelete removes key, if present. No-op if absent or unset.
func (e *Entity) PropDelete(key string) {
	e.properties = propDelete(e.properties, key)
}

// PropRange calls f for every key/value pair in key-sorted order. Iteration
// stops early if f returns false. Safe to call on a zero-value Entity
// (no-op).
func (e Entity) PropRange(f func(k, v string) bool) {
	for _, kv := range e.properties {
		if !f(kv.K, kv.V) {
			return
		}
	}
}

// PropLen returns the number of properties.
func (e Entity) PropLen() int {
	return len(e.properties)
}

// PropsSnapshot returns an independent copy of the properties as a map, or
// nil if there are none. Callers must not assume the returned map aliases
// internal storage.
func (e Entity) PropsSnapshot() map[string]string {
	return propsToMap(e.properties)
}

// WithProperties returns a copy of e with its properties replaced by props,
// converted into the sorted []propKV backing (a fresh copy — the input map
// is not aliased, unlike the old field-assignment semantics).
func (e Entity) WithProperties(props map[string]string) Entity {
	e.properties = propsFromMap(props)
	return e
}

// PropsReplace replaces e's entire property set with props, mutating e in
// place. See WithProperties.
func (e *Entity) PropsReplace(props map[string]string) {
	e.properties = propsFromMap(props)
}

// PropGet returns the value for key, or "" if key is absent.
func (r Relationship) PropGet(key string) string {
	if i, ok := propFind(r.properties, key); ok {
		return r.properties[i].V
	}
	return ""
}

// PropLookup returns the value for key and whether it was present.
func (r Relationship) PropLookup(key string) (string, bool) {
	if i, ok := propFind(r.properties, key); ok {
		return r.properties[i].V, true
	}
	return "", false
}

// PropSet sets key to val, lazily initializing the backing storage.
func (r *Relationship) PropSet(key, val string) {
	r.properties = propSet(r.properties, key, val)
}

// PropDelete removes key, if present. No-op if absent or unset.
func (r *Relationship) PropDelete(key string) {
	r.properties = propDelete(r.properties, key)
}

// PropRange calls f for every key/value pair in key-sorted order. Iteration
// stops early if f returns false. Safe to call on a zero-value Relationship
// (no-op).
func (r Relationship) PropRange(f func(k, v string) bool) {
	for _, kv := range r.properties {
		if !f(kv.K, kv.V) {
			return
		}
	}
}

// PropLen returns the number of properties.
func (r Relationship) PropLen() int {
	return len(r.properties)
}

// PropsSnapshot returns an independent copy of the properties as a map, or
// nil if there are none. Callers must not assume the returned map aliases
// internal storage.
func (r Relationship) PropsSnapshot() map[string]string {
	return propsToMap(r.properties)
}

// WithProperties returns a copy of r with its properties replaced by props,
// converted into the sorted []propKV backing (a fresh copy — the input map
// is not aliased, unlike the old field-assignment semantics).
func (r Relationship) WithProperties(props map[string]string) Relationship {
	r.properties = propsFromMap(props)
	return r
}

// PropsReplace replaces r's entire property set with props, mutating r in
// place. See WithProperties.
func (r *Relationship) PropsReplace(props map[string]string) {
	r.properties = propsFromMap(props)
}

// GraphStatsSidecar is the corpus-level summary written to
// <repo>/.grafel/graph-stats.json. Consumed by `grafel doctor` and the
// future MCP `graph_stats` tool.
type GraphStatsSidecar struct {
	Version            int       `json:"version"`
	ComputedAt         time.Time `json:"computed_at"`
	TotalFiles         int       `json:"total_files,omitempty"`
	TotalEntities      int       `json:"total_entities"`
	TotalRelationships int       `json:"total_relationships"`
	Communities        int       `json:"communities"`
	Modularity         float64   `json:"modularity"`
	GodNodes           int       `json:"god_nodes"`
	ArticulationPoints int       `json:"articulation_points"`
	// RuntimeMS is the wall-clock duration of the graph-algorithm pass
	// (Pass 4: Louvain / PageRank / articulation). Its meaning is unchanged
	// since it was introduced; the extract/link phase timings below are
	// tracked separately.
	RuntimeMS int64 `json:"runtime_ms"`

	// ExtractMS is the wall-clock duration (milliseconds) of the extraction
	// phase for the index pass that produced this sidecar (#5692). Recorded so
	// `grafel feedback` and future tooling can report where indexing time goes.
	// A zero value means "unknown": the phase was not measured, or this sidecar
	// was written before the field existed (back-compat).
	//
	// The measured span differs slightly by write path: on the full-index path
	// it is scoped to the extraction pipeline (idx.Run) only; on the
	// incremental reindex path it is the whole incremental pass wall-clock
	// (manifest load + re-extract + scoped resolve + graph.fb write). Both are
	// written in-band by the same goroutine that writes this sidecar, so the
	// value is always consistent with the counts alongside it.
	//
	// It is written IN-BAND (by the reindex writer, sole owner of this file);
	// the cross-repo link timing lives in a SEPARATE link-stats.json (see
	// LinkStatsSidecar) so an unserialized link goroutine never races the
	// reindex writer on this file's count fields (#5692).
	ExtractMS int64 `json:"extract_ms,omitempty"`

	// ParseErrorCanary is the A4 per-language parse-error-node canary report
	// (#5414, epic #5359): per-language aggregate ERROR-node rates plus a
	// baseline comparison and a spike flag. The shape is the JSON marshalling
	// of treesitter.CanaryReport; kept as raw JSON here so the graph package
	// does not depend on the treesitter package. Omitted when no parse stats
	// were collected (e.g. a heuristic-only index).
	ParseErrorCanary json.RawMessage `json:"parse_error_canary,omitempty"`
	// ParseErrorSpike mirrors ParseErrorCanary.spiked at the top level so
	// dashboards / crons can read the alarm without decoding the full report.
	ParseErrorSpike bool `json:"parse_error_spike,omitempty"`
}

// WriteSidecar emits the graph-stats.json sidecar next to the main document.
// outPath is the same path passed to WriteAtomic; the sidecar is written to
// the sibling file `graph-stats.json`. When pretty is true, the JSON is
// indented for human readability; otherwise it is minified (default).
func WriteSidecar(outPath string, side *GraphStatsSidecar, pretty bool) error {
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("graph: mkdir %s: %w", dir, err)
	}
	return writeJSONAtomic(filepath.Join(dir, "graph-stats.json"), side, pretty)
}

// writeJSONAtomic encodes v to target via a sibling .tmp + rename.
func writeJSONAtomic(target string, v any, pretty bool) error {
	tmp := target + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("graph: create sidecar tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("graph: encode sidecar: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, target)
}

// SidecarPath returns the graph-stats.json path inside stateDir.
func SidecarPath(stateDir string) string {
	return filepath.Join(stateDir, "graph-stats.json")
}

// LoadSidecar reads and decodes the graph-stats.json sidecar in stateDir.
// Returns an error if the sidecar is absent or malformed.
func LoadSidecar(stateDir string) (*GraphStatsSidecar, error) {
	data, err := os.ReadFile(SidecarPath(stateDir))
	if err != nil {
		return nil, err
	}
	var side GraphStatsSidecar
	if err := json.Unmarshal(data, &side); err != nil {
		return nil, fmt.Errorf("graph: decode sidecar %s: %w", stateDir, err)
	}
	return &side, nil
}

// LinkStatsSidecar is the per-repo cross-repo-link timing sidecar written to
// <repo-state>/link-stats.json (#5692). It is kept SEPARATE from
// graph-stats.json on purpose: the cross-repo link pass runs on its own
// per-group goroutine (scheduler AfterFunc) which is NOT serialized against the
// reindex worker pool. Were link timing stamped into graph-stats.json, a
// read-modify-write from the link goroutine could land between a reindex's
// ReadFile and Rename and clobber the freshly written entity/relationship
// counts. Giving the link pass its own file makes it the SOLE writer here, so
// there is no cross-writer lost-update: link passes for a single group are
// themselves serialized by the scheduler's per-group debounce.
type LinkStatsSidecar struct {
	Version    int       `json:"version"`
	ComputedAt time.Time `json:"computed_at"`
	// LinkMS is the wall-clock duration (milliseconds) of the most recent
	// cross-repo link pass for the group this repo belongs to. Zero (or an
	// absent file) means "unknown".
	LinkMS int64 `json:"link_ms"`
}

// LinkStatsPath returns the link-stats.json path inside stateDir.
func LinkStatsPath(stateDir string) string {
	return filepath.Join(stateDir, "link-stats.json")
}

// WriteLinkStats atomically writes the link-stats.json sidecar into stateDir.
// The link pass is the sole writer of this file, so no read-modify-write /
// field-preservation is needed. Minified to match the other sidecars.
func WriteLinkStats(stateDir string, side *LinkStatsSidecar) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("graph: mkdir %s: %w", stateDir, err)
	}
	return writeJSONAtomic(LinkStatsPath(stateDir), side, false)
}

// LoadLinkStats reads and decodes the link-stats.json sidecar in stateDir.
// Returns an error if the file is absent (os.IsNotExist) or malformed;
// callers treating absence as "link timing unknown" should check IsNotExist.
func LoadLinkStats(stateDir string) (*LinkStatsSidecar, error) {
	data, err := os.ReadFile(LinkStatsPath(stateDir))
	if err != nil {
		return nil, err
	}
	var side LinkStatsSidecar
	if err := json.Unmarshal(data, &side); err != nil {
		return nil, fmt.Errorf("graph: decode link-stats %s: %w", stateDir, err)
	}
	return &side, nil
}

// WriteAtomic marshals doc to JSON and writes it to outPath atomically by
// writing to a sibling .tmp file and renaming on success. When pretty is
// true, the JSON is indented for human readability; otherwise it is minified
// (the default — minified output is materially smaller on real repos).
func WriteAtomic(outPath string, doc *Document, pretty bool) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("graph: mkdir %s: %w", filepath.Dir(outPath), err)
	}
	tmp := outPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("graph: create tmp: %w", err)
	}
	enc := json.NewEncoder(f)
	if pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(doc); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("graph: encode: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("graph: close tmp: %w", err)
	}
	if err := os.Rename(tmp, outPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("graph: rename: %w", err)
	}
	return nil
}
