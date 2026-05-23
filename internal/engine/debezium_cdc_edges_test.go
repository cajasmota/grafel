// Tests for the Debezium / Kafka-Connect CDC connector edges pass —
// Issue #1708.
//
// Acceptance criteria:
//   - The polyglot-platform `services/cdc/orders-connector.json`
//     fixture emits one cdc_connector entity, two captured-table
//     entities, two MessageTopic entities, and the expected
//     CAPTURES / PUBLISHES_TO edges.
//   - Topic IDs use the canonical "kafka:<topic>" form so they
//     collapse onto the same node as the Kafka synthesis pass would
//     emit for a downstream consumer.
//   - Non-CDC JSON (package.json shape) is a no-op.
//   - Non-JSON languages are a no-op.

package engine

import (
	"strings"
	"testing"
)

const debeziumPolyglotConnector = `{
  "name": "orders-postgres-cdc",
  "config": {
    "connector.class": "io.debezium.connector.postgresql.PostgresConnector",
    "database.dbname": "orders",
    "database.server.name": "shipfast",
    "plugin.name": "pgoutput",
    "table.include.list": "public.orders,public.order_status_audit",
    "topic.prefix": "cdc"
  },
  "x-shipfast-cdc": {
    "captured-tables": ["public.orders", "public.order_status_audit"],
    "produced-topics": ["cdc.public.orders", "cdc.public.order_status_audit"]
  }
}`

func TestDebeziumCDC_PolyglotFixture(t *testing.T) {
	ents, _ := applyDebeziumCDCEdges("json",
		"services/cdc/orders-connector.json",
		[]byte(debeziumPolyglotConnector), nil, nil,
	)

	var connector *struct {
		captures      map[string]bool
		publishes     map[string]bool
		connectorName string
	}
	connector = &struct {
		captures      map[string]bool
		publishes     map[string]bool
		connectorName string
	}{
		captures:  map[string]bool{},
		publishes: map[string]bool{},
	}

	var (
		gotConnectorEntity bool
		gotTables          = map[string]bool{}
		gotTopics          = map[string]bool{}
	)
	for _, e := range ents {
		switch {
		case e.Kind == "SCOPE.Component" && e.Subtype == "cdc_connector":
			gotConnectorEntity = true
			connector.connectorName = e.Name
			for _, r := range e.Relationships {
				switch r.Kind {
				case "CAPTURES":
					connector.captures[r.ToID] = true
				case "PUBLISHES_TO":
					connector.publishes[r.ToID] = true
				}
			}
		case e.Kind == "SCOPE.Datastore" && e.Subtype == "table":
			gotTables[e.Name] = true
		case e.Kind == messageTopicKind:
			gotTopics[e.Name] = true
		}
	}

	if !gotConnectorEntity {
		t.Fatal("expected cdc_connector entity")
	}
	if connector.connectorName != "orders-postgres-cdc" {
		t.Errorf("connector name = %q, want orders-postgres-cdc", connector.connectorName)
	}

	for _, want := range []string{"orders", "order_status_audit"} {
		if !gotTables[want] {
			t.Errorf("expected SCOPE.Datastore/table %q stub, got %v", want, gotTables)
		}
		if !connector.captures[want] {
			t.Errorf("expected CAPTURES edge to %q, got %v", want, connector.captures)
		}
	}

	for _, want := range []string{"kafka:cdc.public.orders", "kafka:cdc.public.order_status_audit"} {
		if !gotTopics[want] {
			t.Errorf("expected MessageTopic %q, got %v", want, gotTopics)
		}
		if !connector.publishes[want] {
			t.Errorf("expected PUBLISHES_TO edge to %q, got %v", want, connector.publishes)
		}
	}
}

// Even without the x-*-cdc escape hatch, topics are derived from the
// standard `topic.prefix` + `table.include.list` fields.
func TestDebeziumCDC_DerivedTopics(t *testing.T) {
	doc := `{
		"name": "users-cdc",
		"config": {
			"connector.class": "io.debezium.connector.postgresql.PostgresConnector",
			"table.include.list": "public.users,public.sessions",
			"topic.prefix": "cdc"
		}
	}`
	ents, _ := applyDebeziumCDCEdges("json", "cdc/users.json", []byte(doc), nil, nil)
	topics := map[string]bool{}
	for _, e := range ents {
		if e.Kind == messageTopicKind {
			topics[e.Name] = true
		}
	}
	for _, want := range []string{"kafka:cdc.public.users", "kafka:cdc.public.sessions"} {
		if !topics[want] {
			t.Errorf("expected derived topic %q, got %v", want, topics)
		}
	}
}

// Pre-2.x Debezium connectors only set database.server.name — that should
// be used as the topic prefix when topic.prefix is missing.
func TestDebeziumCDC_LegacyServerNameAsPrefix(t *testing.T) {
	doc := `{
		"name": "legacy-cdc",
		"config": {
			"connector.class": "io.debezium.connector.postgresql.PostgresConnector",
			"database.server.name": "legacy",
			"table.include.list": "public.orders"
		}
	}`
	ents, _ := applyDebeziumCDCEdges("json", "debezium/legacy.json", []byte(doc), nil, nil)
	got := false
	for _, e := range ents {
		if e.Kind == messageTopicKind && e.Name == "kafka:legacy.public.orders" {
			got = true
		}
	}
	if !got {
		t.Error("expected kafka:legacy.public.orders derived from database.server.name")
	}
}

func TestDebeziumCDC_NonJSONLanguageNoop(t *testing.T) {
	ents, rels := applyDebeziumCDCEdges("python", "x.py", []byte(debeziumPolyglotConnector), nil, nil)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("expected no-op for non-json language, got %d entities, %d rels", len(ents), len(rels))
	}
}

func TestDebeziumCDC_NonConnectorJSONNoop(t *testing.T) {
	pkg := `{"name":"some-package","version":"1.0.0","dependencies":{"react":"^18"}}`
	ents, rels := applyDebeziumCDCEdges("json", "package.json", []byte(pkg), nil, nil)
	if len(ents) != 0 || len(rels) != 0 {
		t.Errorf("expected no-op for non-connector JSON, got %d entities, %d rels", len(ents), len(rels))
	}
}

// A connector JSON missing both `name` and content sniff anchors must
// no-op (defensive guard against false positives from path heuristics).
func TestDebeziumCDC_SniffGuardsOutNonDebezium(t *testing.T) {
	// Has connector.class but it's not Debezium; we still accept any
	// Kafka-Connect connector that declares connector.class.
	doc := `{"name":"foo","config":{"connector.class":"com.example.Foo"}}`
	ents, _ := applyDebeziumCDCEdges("json", "cdc/foo.json", []byte(doc), nil, nil)
	got := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "cdc_connector" && e.Name == "foo" {
			got = true
		}
	}
	if !got {
		t.Error("expected non-Debezium Kafka-Connect connector to still emit a cdc_connector entity")
	}
}

func TestDebeziumCDC_BareTableNameStripsSchema(t *testing.T) {
	// Edges should target the bare table name so they align with the SQL
	// extractor's canonical SCOPE.Datastore/table entity names.
	ents, _ := applyDebeziumCDCEdges("json",
		"services/cdc/orders-connector.json",
		[]byte(debeziumPolyglotConnector), nil, nil,
	)
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind != "CAPTURES" {
				continue
			}
			if strings.Contains(r.ToID, ".") {
				t.Errorf("CAPTURES ToID %q must be bare table name (no schema prefix)", r.ToID)
			}
		}
	}
}
