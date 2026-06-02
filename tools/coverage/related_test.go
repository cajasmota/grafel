package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRegistryWithDatastores builds a small registry exercising the
// cross-link logic: a MongoDB infra record plus mongoose/mongoengine/go
// driver records, a Neo4j infra record plus neomodel/neogma/grafeo
// records, and a ClickHouse infra record with NO related driver/ORM
// records (negative case).
func testRegistryWithDatastores() *Registry {
	return &Registry{
		SchemaVersion: SchemaVersion,
		Records: []Record{
			{ID: "db.mongodb", Category: "databases", Language: "multi", Label: "MongoDB (collections)",
				Capabilities: map[string]Capability{
					"resource_extraction":    {Status: StatusPartial},
					"dependency_attribution": {Status: StatusPartial},
				}},
			{ID: "lang.jsts.orm.mongoose", Category: "orm", Language: "jsts", Label: "Mongoose",
				Groups: map[string]map[string]Capability{
					"Models":        {"a": {Status: StatusFull}, "b": {Status: StatusFull}, "c": {Status: StatusFull}},
					"Relationships": {"d": {Status: StatusPartial}, "e": {Status: StatusPartial}},
				}},
			{ID: "lang.python.orm.mongoengine", Category: "orm", Language: "python", Label: "MongoEngine",
				Capabilities: map[string]Capability{
					"a": {Status: StatusPartial}, "b": {Status: StatusPartial},
				}},
			{ID: "lang.go.driver.mongodb", Category: "orm", Language: "go", Label: "mongo-go-driver",
				Capabilities: map[string]Capability{
					"a": {Status: StatusFull}, "b": {Status: StatusMissing},
				}},
			{ID: "db.neo4j", Category: "databases", Language: "multi", Label: "Neo4j",
				Capabilities: map[string]Capability{
					"resource_extraction":    {Status: StatusMissing},
					"dependency_attribution": {Status: StatusMissing},
				}},
			{ID: "lang.python.orm.neomodel", Category: "orm", Language: "python", Label: "neomodel",
				Capabilities: map[string]Capability{"a": {Status: StatusFull}}},
			{ID: "lang.jsts.orm.neogma", Category: "orm", Language: "jsts", Label: "neogma",
				Capabilities: map[string]Capability{"a": {Status: StatusPartial}}},
			{ID: "lang.java.orm.grafeo", Category: "orm", Language: "java", Label: "grafeo",
				Capabilities: map[string]Capability{"a": {Status: StatusMissing}}},
			// ClickHouse infra record with no related driver/ORM records.
			{ID: "db.clickhouse", Category: "databases", Language: "multi", Label: "ClickHouse",
				Capabilities: map[string]Capability{
					"resource_extraction": {Status: StatusMissing},
				}},
			// A general-SQL ORM that must NOT be linked to any datastore.
			{ID: "lang.go.orm.gorm", Category: "orm", Language: "go", Label: "GORM",
				Capabilities: map[string]Capability{"a": {Status: StatusFull}}},
		},
	}
}

func TestStatusDigest(t *testing.T) {
	rec := Record{Groups: map[string]map[string]Capability{
		"G": {
			"a": {Status: StatusFull}, "b": {Status: StatusFull}, "c": {Status: StatusFull},
			"d": {Status: StatusPartial}, "e": {Status: StatusPartial},
			"f": {Status: StatusMissing},
			"g": {Status: StatusNotApplicable},
		},
	}}
	got := statusDigest(rec)
	want := "3 full, 2 partial, 1 missing, 1 n/a"
	if got != want {
		t.Fatalf("statusDigest = %q, want %q", got, want)
	}
	if got := statusDigest(Record{}); got != "no cells" {
		t.Fatalf("empty statusDigest = %q, want %q", got, "no cells")
	}
}

func TestRelatedDriverORMRecords(t *testing.T) {
	reg := testRegistryWithDatastores()
	mongo := reg.Records[0] // db.mongodb
	related := relatedDriverORMRecords(mongo, reg.Records)
	ids := make([]string, len(related))
	for i, r := range related {
		ids[i] = r.ID
	}
	for _, want := range []string{"lang.jsts.orm.mongoose", "lang.python.orm.mongoengine", "lang.go.driver.mongodb"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Errorf("db.mongodb related missing %s; got %v", want, ids)
		}
	}
	// gorm (general SQL ORM) must never be linked under a datastore.
	for _, id := range ids {
		if strings.Contains(id, "gorm") {
			t.Errorf("gorm wrongly linked under db.mongodb: %v", ids)
		}
	}
	// Digest count assertion for mongoose (3 full + 2 partial).
	for _, r := range related {
		if r.ID == "lang.jsts.orm.mongoose" {
			if r.Digest != "3 full, 2 partial" {
				t.Errorf("mongoose digest = %q, want %q", r.Digest, "3 full, 2 partial")
			}
		}
	}

	// Neo4j: neomodel/neogma/grafeo all associate.
	neo := reg.Records[4] // db.neo4j
	neoRelated := relatedDriverORMRecords(neo, reg.Records)
	neoIDs := map[string]bool{}
	for _, r := range neoRelated {
		neoIDs[r.ID] = true
	}
	for _, want := range []string{"lang.python.orm.neomodel", "lang.jsts.orm.neogma", "lang.java.orm.grafeo"} {
		if !neoIDs[want] {
			t.Errorf("db.neo4j related missing %s; got %v", want, neoIDs)
		}
	}

	// ClickHouse: no related records → nil section.
	ch := reg.Records[8]
	if got := relatedDriverORMRecords(ch, reg.Records); got != nil {
		t.Errorf("db.clickhouse should have no related records, got %v", got)
	}
}

func TestInfraRecordFor(t *testing.T) {
	reg := testRegistryWithDatastores()
	mongoose := reg.Records[1]
	infra := infraRecordFor(mongoose, reg.Records)
	if infra == nil || infra.ID != "db.mongodb" {
		t.Fatalf("mongoose infra = %v, want db.mongodb", infra)
	}
	// gorm (general SQL) targets no recognised datastore → nil.
	gorm := reg.Records[9]
	if infra := infraRecordFor(gorm, reg.Records); infra != nil {
		t.Errorf("gorm should have no infra record, got %v", infra)
	}
	// An infra record itself is not a driver/ORM → nil.
	if infra := infraRecordFor(reg.Records[0], reg.Records); infra != nil {
		t.Errorf("db.mongodb should not back-link to an infra record, got %v", infra)
	}
}

// TestGenRelatedRecordsSection is the end-to-end gen-level assertion:
// db.mongodb's page lists the mongoose/mongoengine/go-driver records with
// links and a digest; mongoose backlinks to db.mongodb; db.clickhouse has
// no Code-level section (negative).
func TestGenRelatedRecordsSection(t *testing.T) {
	reg := testRegistryWithDatastores()
	root := t.TempDir()
	if err := generate(reg, root); err != nil {
		t.Fatalf("generate: %v", err)
	}
	read := func(id string) string {
		data, err := os.ReadFile(filepath.Join(root, "docs", "coverage", "detail", id+".md"))
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return string(data)
	}

	mongo := read("db.mongodb")
	if !strings.Contains(mongo, "## Code-level coverage") {
		t.Errorf("db.mongodb missing Code-level coverage section:\n%s", mongo)
	}
	for _, want := range []string{
		"[`lang.jsts.orm.mongoose`](./lang.jsts.orm.mongoose.md)",
		"[`lang.python.orm.mongoengine`](./lang.python.orm.mongoengine.md)",
		"[`lang.go.driver.mongodb`](./lang.go.driver.mongodb.md)",
		"3 full, 2 partial", // mongoose digest
	} {
		if !strings.Contains(mongo, want) {
			t.Errorf("db.mongodb missing %q:\n%s", want, mongo)
		}
	}
	if strings.Contains(mongo, "gorm") {
		t.Errorf("db.mongodb wrongly lists gorm:\n%s", mongo)
	}

	// neo4j lists neomodel/neogma/grafeo.
	neo := read("db.neo4j")
	for _, want := range []string{"neomodel", "neogma", "grafeo"} {
		if !strings.Contains(neo, want) {
			t.Errorf("db.neo4j missing %q:\n%s", want, neo)
		}
	}

	// mongoose backlinks to db.mongodb.
	mg := read("lang.jsts.orm.mongoose")
	if !strings.Contains(mg, "## Datastore") {
		t.Errorf("mongoose missing Datastore back-link section:\n%s", mg)
	}
	if !strings.Contains(mg, "[`db.mongodb`](./db.mongodb.md)") {
		t.Errorf("mongoose missing db.mongodb back-link:\n%s", mg)
	}

	// clickhouse: no Code-level section (negative).
	ch := read("db.clickhouse")
	if strings.Contains(ch, "## Code-level coverage") {
		t.Errorf("db.clickhouse should have no Code-level coverage section:\n%s", ch)
	}

	// gorm: no Datastore back-link (negative).
	gorm := read("lang.go.orm.gorm")
	if strings.Contains(gorm, "## Datastore") {
		t.Errorf("gorm should have no Datastore back-link:\n%s", gorm)
	}
}

// TestDatastoreAliasesNoCollision guards that no driver/ORM record in the
// LIVE registry associates with more than one datastore — a collision
// would mean an alias substring is too broad and mis-attributes coverage.
func TestDatastoreAliasesNoCollision(t *testing.T) {
	reg, err := loadRegistry(filepath.Join("..", "..", "docs", "coverage", "registry.json"))
	if err != nil {
		t.Skipf("live registry unavailable: %v", err)
	}
	for _, r := range reg.Records {
		if recordKind(r.ID) == "" {
			continue
		}
		var hits []string
		for db := range datastoreAliases {
			if matchesDatastore(db, r.ID) {
				hits = append(hits, db)
			}
		}
		if len(hits) > 1 {
			t.Errorf("record %s matches multiple datastores %v (alias too broad)", r.ID, hits)
		}
	}
}
