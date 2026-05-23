// Debezium / Kafka-Connect CDC connector extraction — Issue #1708.
//
// Polyglot-platform iter3 calibration surfaced a stitch gap: the DB
// trigger entity, the source table, the audit table, and the downstream
// Kafka CDC topic all exist as nodes, but no edges connect them into a
// single "CDC chain" subgraph. The trigger half is handled by the SQL
// extractor (Issue #1414 — CREATE TRIGGER ... ON ... EXECUTE FUNCTION
// emits FIRES + DEFINED_ON; the trigger function body emits WRITES_TO
// for INSERT INTO order_status_audit). What's missing is the connector
// half: the JSON file that says "Debezium captures `public.orders` and
// `public.order_status_audit` and produces topics `cdc.public.orders`
// and `cdc.public.order_status_audit`."
//
// This pass parses Debezium / Kafka-Connect connector JSON configs and
// emits, for each connector it can statically recognise:
//
//   - a SCOPE.Component entity for the connector itself (Subtype="cdc_connector"),
//     keyed by the JSON `name` field (e.g. "orders-postgres-cdc").
//   - a SCOPE.Datastore "table" stub entity per element of
//     `table.include.list` (or the `x-shipfast-cdc.captured-tables`
//     escape hatch). Stub entities collapse onto the SQL extractor's
//     real table entities via the existing same-name dedup path: the
//     resolver's same-repo entity-name reconciliation (#1662 family)
//     pins these to the canonical SCOPE.Datastore/table node when the
//     migration SQL is in the same repo. Cross-repo (CDC in services/cdc/
//     pointing at services/orders/migrations/) still binds via the
//     cross-repo name linker that #534 uses for HTTP and #726 for Kafka.
//   - a SCOPE.MessageTopic entity per produced Kafka topic, broker=kafka,
//     using the canonical `kafka:<topic>` ID that the existing Kafka
//     synthesizer uses (kafka_edges.go). This lets the CDC connector
//     share topic nodes with downstream Kafka consumers in other repos.
//   - edges:
//       connector  CAPTURES       table              (per captured table)
//       connector  PUBLISHES_TO   kafka:<topic>      (per produced topic)
//
//   CAPTURES is a new typed edge for the CDC chain — consumers of the
//   topic produce SUBSCRIBES_TO edges from the regular Kafka synthesis
//   pass (e.g. a Python consumer doing `KafkaConsumer("cdc.public.orders")`)
//   so the resulting subgraph is:
//
//       trigger ─FIRES─▶ trigger_function
//                         │
//                         └─WRITES_TO─▶ audit_table
//                                        │
//                                        └◀─CAPTURES─ connector ─PUBLISHES_TO─▶ kafka:cdc.public.audit_table
//                                                                                        │
//                                                                                        └─SUBSCRIBES_TO─ consumer
//
// # Topic-name derivation
//
// Debezium PostgreSQL connector topic names follow the pattern
// `<topic.prefix>.<schema>.<table>` — e.g. with `topic.prefix=cdc` and
// `table.include.list=public.orders,public.order_status_audit` the
// produced topics are `cdc.public.orders` and `cdc.public.order_status_audit`.
// We derive both from the standard fields. As a calibration escape
// hatch (and to match #1596 IaC-side x-shipfast-* convention), we also
// honour an `x-*-cdc.produced-topics` array if present in the connector
// document, which lets fixtures pin exact topic names without us having
// to encode every variant of Debezium's topic-naming rules.
//
// # Scope guard
//
// Append-only — this pass never modifies or removes existing entities
// or edges, so it cannot regress the surrounding pipeline's bug-rate.
// Files that don't content-sniff as a connector are no-ops.
//
// Refs #1708.
package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// debeziumCDCSupportsLanguage gates applyDebeziumCDCEdges. The classifier
// only routes path-narrow JSON files (cdc/, debezium/, kafka-connect/,
// *-connector.json, …) to language="json" precisely so this pass receives
// likely-connector files and nothing else.
func debeziumCDCSupportsLanguage(lang string) bool {
	return lang == "json"
}

// debeziumCDCSniff returns true if the raw content looks like a Debezium /
// Kafka-Connect connector config. Cheap byte-substring checks first so
// we don't pay JSON-decode cost on every JSON file the classifier may
// route through.
func debeziumCDCSniff(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	s := string(content)
	// Debezium connectors have connector.class=io.debezium.* and Kafka-
	// Connect connectors carry connector.class. We sniff for the JSON-
	// quoted form to avoid matching prose / Dockerfile snippets.
	if strings.Contains(s, `"connector.class"`) {
		return true
	}
	if strings.Contains(s, "io.debezium") {
		return true
	}
	return false
}

// debeziumConnectorDoc is the subset of a Debezium / Kafka-Connect JSON
// config we care about. The actual schema is far richer; we ignore the
// rest. Both top-level shapes are supported:
//
//   - bare connector config: { "name": ..., "config": {...} }
//   - the "config" object inline at top level
//
// We also pick up the calibration escape-hatch fields
// `x-*-cdc.produced-topics` and `x-*-cdc.captured-tables` from the
// top-level document (any property whose key starts with "x-" and ends
// with "-cdc"), so fixture authors can pin exact topic / table names
// without us having to reverse Debezium's topic-naming rules.
type debeziumConnectorDoc struct {
	Name   string                 `json:"name"`
	Config map[string]interface{} `json:"config"`
}

// applyDebeziumCDCEdges parses one JSON file as a Debezium / Kafka-Connect
// connector and APPENDS connector / table / topic entities and CAPTURES /
// PUBLISHES_TO edges. Returns inputs untouched if the file is not a
// connector or fails to parse.
func applyDebeziumCDCEdges(
	lang string,
	path string,
	content []byte,
	entities []types.EntityRecord,
	relationships []types.RelationshipRecord,
) ([]types.EntityRecord, []types.RelationshipRecord) {
	if !debeziumCDCSupportsLanguage(lang) {
		return entities, relationships
	}
	if !debeziumCDCSniff(content) {
		return entities, relationships
	}

	// Use a raw map so we can pick up both the typed `name`/`config`
	// fields and any `x-*-cdc` calibration overrides without a second
	// pass.
	var raw map[string]interface{}
	if err := json.Unmarshal(content, &raw); err != nil {
		return entities, relationships
	}

	connectorName, _ := raw["name"].(string)
	cfg := flattenConfig(raw)

	connectorClass, _ := cfg["connector.class"].(string)
	tableList, _ := cfg["table.include.list"].(string)
	topicPrefix, _ := cfg["topic.prefix"].(string)
	dbName, _ := cfg["database.dbname"].(string)
	dbServer, _ := cfg["database.server.name"].(string)

	// Derive the connector name if missing.
	if connectorName == "" {
		if name, ok := cfg["name"].(string); ok {
			connectorName = name
		}
	}
	if connectorName == "" {
		// Last resort: derive from filename. Strip directory + ".json".
		connectorName = derefConnectorNameFromPath(path)
	}
	if connectorName == "" {
		return entities, relationships
	}

	// Calibration escape hatches.
	capturedFromExt := extractCDCExtList(raw, "captured-tables")
	producedFromExt := extractCDCExtList(raw, "produced-topics")

	tables := parseTableIncludeList(tableList)
	for _, t := range capturedFromExt {
		tables = appendUnique(tables, normaliseTableName(t))
	}

	topics := producedFromExt
	if len(topics) == 0 {
		topics = derivedDebeziumTopics(topicPrefix, dbServer, tables)
	}

	// Emit connector entity.
	connectorEntity := types.EntityRecord{
		Name:       connectorName,
		Kind:       "SCOPE.Component",
		Subtype:    "cdc_connector",
		SourceFile: path,
		Language:   lang,
		Signature:  fmt.Sprintf("Debezium connector %s (class=%s db=%s)", connectorName, connectorClass, dbName),
		Properties: map[string]string{
			"connector_class":     connectorClass,
			"database_dbname":     dbName,
			"database_server":     dbServer,
			"topic_prefix":        topicPrefix,
			"table_include_list":  tableList,
			"pattern_type":        "debezium_cdc",
			"enrichment_pipeline": "debezium_cdc",
		},
		EnrichmentRequired: false,
	}

	// Build relationships from the connector outward.
	for _, table := range tables {
		// Bare table name (drop schema prefix) so the SQL extractor's
		// SCOPE.Datastore/table entities (which are stored by bare name)
		// collapse onto this same node via same-name dedup.
		bare := bareTableName(table)
		connectorEntity.Relationships = append(connectorEntity.Relationships, types.RelationshipRecord{
			FromID:     connectorName,
			ToID:       bare,
			Kind:       "CAPTURES",
			Properties: map[string]string{"cdc_source_table": table, "pattern_type": "debezium_cdc"},
		})
		// Emit a stub table entity so even if the SQL migration is in
		// another repo (or not yet indexed) the captured table still
		// renders as a graph node. The resolver collapses it onto the
		// canonical table when both are present.
		entities = append(entities, types.EntityRecord{
			Name:               bare,
			Kind:               "SCOPE.Datastore",
			Subtype:            "table",
			SourceFile:         "",
			Language:           lang,
			Signature:          fmt.Sprintf("Debezium-captured table %s", table),
			Properties:         map[string]string{"pattern_type": "debezium_cdc", "schema_qualified": table},
			EnrichmentRequired: false,
		})
	}

	for _, topic := range topics {
		topicID := "kafka:" + topic
		connectorEntity.Relationships = append(connectorEntity.Relationships, types.RelationshipRecord{
			FromID: connectorName,
			ToID:   topicID,
			Kind:   "PUBLISHES_TO",
			Properties: map[string]string{
				"broker":       "kafka",
				"topic_name":   topic,
				"pattern_type": "debezium_cdc",
			},
		})
		// Emit the canonical MessageTopic node, matching the
		// kafka_edges.go convention (SourceFile="" so cross-file +
		// cross-repo emissions collapse onto the same stamped entity).
		entities = append(entities, types.EntityRecord{
			Name:       topicID,
			Kind:       messageTopicKind,
			SourceFile: "",
			Language:   lang,
			Properties: map[string]string{
				"broker":       "kafka",
				"topic_name":   topic,
				"pattern_type": "debezium_cdc",
			},
			EnrichmentRequired: false,
			EnrichmentStatus:   types.StatusPending,
			QualityScore:       0.8,
		})
	}

	entities = append(entities, connectorEntity)
	return entities, relationships
}

// flattenConfig returns the "config" sub-object if present, otherwise the
// raw doc itself (some Kafka-Connect bundles inline the config at the top
// level rather than under a "config" key).
func flattenConfig(raw map[string]interface{}) map[string]interface{} {
	if c, ok := raw["config"].(map[string]interface{}); ok {
		return c
	}
	return raw
}

// extractCDCExtList walks raw["x-*-cdc"] (any property whose key starts
// with "x-" and ends with "-cdc") and returns the named string list.
// Used for fixture-pinned captured-tables / produced-topics overrides.
func extractCDCExtList(raw map[string]interface{}, field string) []string {
	var out []string
	for k, v := range raw {
		if !strings.HasPrefix(k, "x-") || !strings.HasSuffix(k, "-cdc") {
			continue
		}
		obj, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		arr, ok := obj[field].([]interface{})
		if !ok {
			continue
		}
		for _, item := range arr {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// parseTableIncludeList splits a Debezium `table.include.list` value
// (comma-separated schema.table tokens) and returns the trimmed,
// non-empty entries.
func parseTableIncludeList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// derivedDebeziumTopics computes the canonical Debezium topic names from
// the prefix and table list. Modern Debezium uses `<topic.prefix>.<schema>.<table>`
// (topic.prefix replaced server.name in Debezium 2.x but the topic
// pattern is identical). Pre-2.x connectors that only set
// `database.server.name` fall back to that as the prefix.
func derivedDebeziumTopics(topicPrefix, dbServer string, tables []string) []string {
	prefix := topicPrefix
	if prefix == "" {
		prefix = dbServer
	}
	if prefix == "" || len(tables) == 0 {
		return nil
	}
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		out = append(out, prefix+"."+strings.TrimSpace(t))
	}
	return out
}

// bareTableName strips an optional schema prefix from "schema.table",
// returning just the table name so it matches the SQL extractor's
// canonical entity name.
func bareTableName(t string) string {
	if i := strings.LastIndex(t, "."); i >= 0 {
		return t[i+1:]
	}
	return t
}

// normaliseTableName trims whitespace from a captured table reference.
func normaliseTableName(t string) string { return strings.TrimSpace(t) }

// appendUnique appends s to xs only if not already present.
func appendUnique(xs []string, s string) []string {
	if s == "" {
		return xs
	}
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// derefConnectorNameFromPath returns the basename of path stripped of the
// .json extension. Used only when the connector JSON has no `name`
// field (some Kafka-Connect distributions ship the config without the
// outer envelope, expecting the filename to provide the name).
func derefConnectorNameFromPath(path string) string {
	base := path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ".json")
}
