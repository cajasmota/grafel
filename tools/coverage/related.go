package main

import (
	"fmt"
	"sort"
	"strings"
)

// Cross-linking infra `databases`-category records to the per-language
// driver/ORM records that target the SAME datastore (#3628 child).
//
// The honesty problem this solves: an infra record like `db.mongodb`
// renders as a thin 2-cell page (resource_extraction + dependency_
// attribution) that looks almost empty, even though the SAME technology
// has rich code-level extraction spread across ~15 separate
// `lang.*.{driver,orm}.*` records (mongoose, mongoengine, mongoid,
// spring-data-mongo, the per-language native drivers, ...). The detail-
// page generator renders each record in ISOLATION, so the infra page
// never surfaces the real coverage and badly undersells it.
//
// We fix this in the GENERATOR (no extractor change): a curated alias
// map associates each datastore with the id-substrings of the records
// that genuinely target it, and the detail page grows a "Code-level
// coverage" section (on the infra page) plus a back-link (on each
// driver/ORM page).

// datastoreAliases maps a `databases`-category record ID to the set of
// id-substrings that identify the per-language driver/ORM records
// targeting that SAME datastore.
//
// Honesty rules baked into this map:
//
//   - ODMs/OGMs rarely name their datastore, so they are listed
//     explicitly: mongoose/mongoengine/mongoid/beanie/spring-data-mongo
//     are MongoDB; neomodel/neogma/grafeo/activegraph/bolt-sips are
//     Neo4j; spring-data-redis/ioredis/redix are Redis; xandra/scylla
//     are Cassandra; elastic4s/nest are Elasticsearch; scanamo is
//     DynamoDB; npgsql/postgrex/libpqxx/pgx are PostgreSQL; etc.
//   - GENERAL-PURPOSE SQL ORMs/query-builders (gorm, prisma, sqlalchemy,
//     hibernate, ecto, diesel, sequelize, knex, dapper, doctrine,
//     eloquent, slick, ...) target MULTIPLE SQL stores and do NOT name a
//     single one — linking them under postgres OR mysql OR sqlite would
//     mis-attribute their coverage, so they are deliberately OMITTED.
//     A db.postgres page only lists records that are unambiguously
//     PostgreSQL (the pg drivers + Postgres-specific libraries).
//   - Substrings match against the record ID's terminal token (the
//     library slug), never against arbitrary substrings of the dotted
//     path, so "redis" cannot accidentally match an unrelated id.
//
// A substring entry is matched against the record's terminal id segment
// AND against the whole id; either hit associates the record. Keep this
// map honest: only add a substring when the record genuinely targets the
// datastore.
var datastoreAliases = map[string][]string{
	"db.mongodb": {
		"mongodb", "mongo", "mongoose", "mongoengine", "mongoid",
		"beanie", "spring-data-mongo", "mongocxx",
	},
	"db.neo4j": {
		"neo4j", "neomodel", "neogma", "spring-data-neo4j", "grafeo",
		"activegraph", "bolt-sips", "bolt_sips", "neo4rs",
	},
	"db.redis": {
		"redis", "ioredis", "spring-data-redis", "redix", "predis",
	},
	"db.cassandra": {
		"cassandra", "spring-data-cassandra", "scylla", "xandra",
	},
	"db.elasticsearch": {
		"elasticsearch", "elastic", "elastic4s",
	},
	"db.dynamodb": {
		"dynamodb", "scanamo",
	},
	"db.postgres": {
		"postgres", "postgrex", "psycopg", "asyncpg", "npgsql",
		"libpqxx", "pgx", "node-postgres",
	},
	"db.mysql": {
		"mysql", "mariadb", "myxql", "mysql2", "mysql-connector",
		"mysqlconnector", "pymysql", "mysqlclient", "go-sql-driver",
	},
	"db.sqlite": {
		"sqlite", "rusqlite", "better-sqlite", "ecto-sqlite", "ecto_sqlite3",
	},
	"db.clickhouse": {
		"clickhouse",
	},
	"db.snowflake": {
		"snowflake",
	},
}

// relatedRecordDigest is one related driver/ORM record rendered under
// the infra page's "Code-level coverage" section, or the single infra
// record back-linked from a driver/ORM page.
type relatedRecordDigest struct {
	ID       string // e.g. lang.jsts.orm.mongoose
	Label    string // e.g. Mongoose
	Language string // e.g. jsts
	Kind     string // "driver" or "orm" (from the id), "" for infra
	Digest   string // compact status digest, e.g. "3 full, 2 partial"
}

// terminalSegment returns the last dotted segment of a record ID
// (the library slug), e.g. "mongoose" for "lang.jsts.orm.mongoose".
func terminalSegment(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[i+1:]
	}
	return id
}

// recordKind extracts the driver/orm/odm classifier from a record ID
// (the segment immediately before the terminal slug), e.g. "orm" for
// "lang.jsts.orm.mongoose". Returns "" when the id has no such segment.
func recordKind(id string) string {
	parts := strings.Split(id, ".")
	if len(parts) < 2 {
		return ""
	}
	switch parts[len(parts)-2] {
	case "driver", "orm", "odm":
		return parts[len(parts)-2]
	}
	return ""
}

// matchesDatastore reports whether a driver/ORM record (by its ID)
// targets the datastore identified by db (a `db.<tech>` infra id). The
// match is against the record's terminal slug so an alias substring
// never bleeds across unrelated dotted segments.
func matchesDatastore(db, recID string) bool {
	subs, ok := datastoreAliases[db]
	if !ok {
		return false
	}
	seg := terminalSegment(recID)
	for _, s := range subs {
		if strings.Contains(seg, s) {
			return true
		}
	}
	return false
}

// statusDigest renders a compact per-status digest for one record, e.g.
// "3 full, 2 partial" or "5 partial" or "all not applicable". Counts are
// taken over every capability cell on the record (canonical + framework-
// specific). Statuses are listed in severity order (full, partial,
// missing, not_applicable); zero-count statuses are omitted. An empty
// record (no cells) renders "no cells".
func statusDigest(rec Record) string {
	caps := rec.AllCapabilitiesIncludingFrameworkSpecific()
	if len(caps) == 0 {
		return "no cells"
	}
	var full, partial, missing, na int
	for _, c := range caps {
		switch c.Status {
		case StatusFull:
			full++
		case StatusPartial:
			partial++
		case StatusMissing:
			missing++
		case StatusNotApplicable:
			na++
		}
	}
	parts := make([]string, 0, 4)
	if full > 0 {
		parts = append(parts, fmt.Sprintf("%d full", full))
	}
	if partial > 0 {
		parts = append(parts, fmt.Sprintf("%d partial", partial))
	}
	if missing > 0 {
		parts = append(parts, fmt.Sprintf("%d missing", missing))
	}
	if na > 0 {
		parts = append(parts, fmt.Sprintf("%d n/a", na))
	}
	if len(parts) == 0 {
		return "no cells"
	}
	return strings.Join(parts, ", ")
}

// relatedDriverORMRecords returns the driver/ORM records that target the
// same datastore as the given infra record (a `databases`-category
// `db.<tech>` record), each with a compact status digest, sorted by
// (language, id) for deterministic output. Returns nil when the record
// is not a recognised infra record or has no related records — the
// caller omits the section entirely in that case (never fabricates).
func relatedDriverORMRecords(infra Record, all []Record) []relatedRecordDigest {
	if infra.Category != "databases" {
		return nil
	}
	if _, ok := datastoreAliases[infra.ID]; !ok {
		return nil
	}
	out := make([]relatedRecordDigest, 0, 16)
	for _, r := range all {
		if r.ID == infra.ID {
			continue
		}
		kind := recordKind(r.ID)
		if kind == "" {
			continue
		}
		if !matchesDatastore(infra.ID, r.ID) {
			continue
		}
		out = append(out, relatedRecordDigest{
			ID:       r.ID,
			Label:    r.Label,
			Language: r.Language,
			Kind:     kind,
			Digest:   statusDigest(r),
		})
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Language != out[j].Language {
			return out[i].Language < out[j].Language
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// infraRecordFor returns the `databases`-category infra record that the
// given driver/ORM record targets, or nil when there is none. A
// driver/ORM record matches an infra record when the infra's alias map
// associates it (the same association used by relatedDriverORMRecords,
// kept symmetric). Returns nil for non-driver/ORM records.
func infraRecordFor(rec Record, all []Record) *relatedRecordDigest {
	if recordKind(rec.ID) == "" {
		return nil
	}
	// Find the infra record whose alias set claims this record. There is
	// at most one per datastore; iterate the infra records deterministically.
	infraByID := map[string]Record{}
	infraIDs := make([]string, 0, len(datastoreAliases))
	for _, r := range all {
		if r.Category == "databases" {
			if _, ok := datastoreAliases[r.ID]; ok {
				infraByID[r.ID] = r
				infraIDs = append(infraIDs, r.ID)
			}
		}
	}
	sort.Strings(infraIDs)
	for _, id := range infraIDs {
		if matchesDatastore(id, rec.ID) {
			r := infraByID[id]
			return &relatedRecordDigest{
				ID:       r.ID,
				Label:    r.Label,
				Language: r.Language,
				Kind:     "",
				Digest:   statusDigest(r),
			}
		}
	}
	return nil
}
