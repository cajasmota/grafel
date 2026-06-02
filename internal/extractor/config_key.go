package extractor

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// config_key.go — shared cross-language helpers for the config-consumption
// topology (issue #3641, epic #3625).
//
// The config-blast-radius capability answers "which code reads which config
// key" so a config change can be traced to every consumer. To make that a
// single graph node per key (rather than one-per-file), every language
// extractor emits:
//
//   - one SCOPE.Config / subtype="config_key" entity per distinct key, with a
//     SYNTHETIC, constant SourceFile (ConfigKeySourceFile) so EntityRecord.
//     ComputeID (SourceFile+Kind+Name) converges identical keys read from
//     different files into ONE node. The indexer's dedup-by-ID then collapses
//     them, leaving a single config:<key> node whose inbound DEPENDS_ON_CONFIG
//     edges are the blast radius.
//
//   - one DEPENDS_ON_CONFIG edge from the reading function / component to that
//     config-key entity, carried as a Name-keyed structural ref resolved at
//     link time (mirrors the Python config_consumer DEPENDS_ON_CONFIG shape).
//
// Dynamic keys (os.Getenv(varName), config.get(someVar)) are intentionally NOT
// emitted — honest-partial. We only record literal string keys.

// ConfigKeySourceFile is the synthetic, constant SourceFile assigned to every
// config-key entity so identical keys converge to a single graph node under
// EntityRecord.ComputeID (which hashes SourceFile+Kind+Name).
const ConfigKeySourceFile = "<config>"

// ConfigKeyName returns the canonical entity Name for a config key. We keep the
// raw key verbatim so the node reads "config:app.timeout", "config:API_URL",
// "config:db.host" — the literal a human grep's for. The "config:" prefix
// namespaces the node and prevents collision with same-named code symbols.
func ConfigKeyName(key string) string {
	return "config:" + key
}

// ConfigKeyTargetID returns the Name-keyed structural-ref ToID for a
// DEPENDS_ON_CONFIG edge pointing at a config-key entity. Shape:
//
//	scope:config:config_key:<key>
//
// The resolver's byName index for SCOPE.Config binds this to the config-key
// entity whose Name is ConfigKeyName(key). Constant across languages so a
// Java @Value and a Go viper.GetString of the same key resolve to the same
// node.
func ConfigKeyTargetID(key string) string {
	return "scope:config:config_key:" + key
}

// ConfigKeyEntity builds the SCOPE.Config / config_key entity for a single
// literal config key in the given language. The entity is deliberately
// file-agnostic (synthetic SourceFile) so it is the shared blast-radius node.
func ConfigKeyEntity(key, lang string) types.EntityRecord {
	e := types.EntityRecord{
		Name:          ConfigKeyName(key),
		QualifiedName: ConfigKeyName(key),
		Kind:          string(types.EntityKindConfig),
		Subtype:       "config_key",
		Language:      lang,
		SourceFile:    ConfigKeySourceFile,
		StartLine:     1,
		EndLine:       1,
		Signature:     ConfigKeyName(key),
		Properties: map[string]string{
			"config_key": key,
		},
	}
	// Pre-compute the deterministic ID so extractors that ID their entities at
	// emit time (e.g. JS/TS) stay consistent; the value matches what the
	// indexer would compute (SourceFile+Kind+Name), and the synthetic constant
	// SourceFile makes identical keys across files converge to ONE node.
	e.ID = e.ComputeID()
	return e
}

// ConfigRead is one resolved config-key read detected by a language extractor:
// the literal key plus the Name of the enclosing entity that read it ("" for
// file/module scope, which attaches to the file entity).
type ConfigRead struct {
	Key      string // literal config key, e.g. "app.timeout"
	FromName string // enclosing entity Name; "" => file entity
	Pattern  string // detector label, e.g. "spring_value", "viper", "process_env"
}

// EmitConfigReads appends, to *entities, the config-key entities and
// DEPENDS_ON_CONFIG edges for the given reads. entities[0] MUST be the file
// entity (every language extractor appends it first). hostByName maps an
// enclosing entity Name to its index in *entities; reads whose FromName is ""
// attach to the file entity (index 0), and reads whose FromName has no host
// attach to the file entity as a conservative fallback so the edge is never
// dropped.
//
// Returns the number of DEPENDS_ON_CONFIG edges emitted. Safe with nil/empty
// input.
func EmitConfigReads(entities *[]types.EntityRecord, lang string, reads []ConfigRead) int {
	if entities == nil || len(*entities) == 0 || len(reads) == 0 {
		return 0
	}

	// Build a Name → index map for hosts (last-writer-wins is fine; per-file
	// names are unique for the entity kinds we attach to).
	hostByName := map[string]int{}
	for i := range *entities {
		hostByName[(*entities)[i].Name] = i
	}

	// Dedup (FromName, Key) so a function reading the same key twice yields one
	// edge, and dedup Key so we append one config-key entity per distinct key.
	seenEdge := map[string]bool{}
	seenKey := map[string]bool{}
	var newEntities []types.EntityRecord
	edges := 0

	for _, r := range reads {
		key := strings.TrimSpace(r.Key)
		if key == "" {
			continue
		}

		hostIdx := 0 // file entity by default (module/file scope)
		if r.FromName != "" {
			if idx, ok := hostByName[r.FromName]; ok {
				hostIdx = idx
			}
		}

		edgeKey := r.FromName + "\x00" + key
		if !seenEdge[edgeKey] {
			seenEdge[edgeKey] = true
			props := map[string]string{"config_key": key}
			if r.Pattern != "" {
				props["pattern"] = r.Pattern
			}
			(*entities)[hostIdx].Relationships = append((*entities)[hostIdx].Relationships,
				types.RelationshipRecord{
					ToID:       ConfigKeyTargetID(key),
					Kind:       string(types.RelationshipKindDependsOnConfig),
					Properties: props,
				})
			edges++
		}

		if !seenKey[key] {
			seenKey[key] = true
			newEntities = append(newEntities, ConfigKeyEntity(key, lang))
		}
	}

	*entities = append(*entities, newEntities...)
	return edges
}
