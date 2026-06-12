// allographer_query.go — Nim Allographer rdb() query-builder attribution
// (#5030, follow-up to #4933).
//
// allographer_orm.go covers the CREATE-time schema (`schema().create(table(...))`)
// and allographer_migrations.go (#5029) covers alter()/drop() evolution. The third
// Allographer surface is the **rdb() query builder** — the runtime data-access API:
//
//	import allographer/query_builder
//
//	let users = rdb().table("users").select("id", "name").where("age", ">", 18).get()
//	rdb().table("users").insert(%*{"name": "Ada"})
//	rdb().table("users").where("id", "=", 1).update(%*{"name": "Grace"})
//	rdb().table("posts").where("draft", "=", true).delete()
//
//	rdb().transaction(proc() {.async.} =
//	  await rdb().table("accounts").where("id","=",1).update(%*{"bal": 0})
//	  await rdb().table("ledger").insert(%*{"acct": 1})
//	)
//
// Each `rdb().table("t")...<op>()` chain is a query against table `t`. The table
// identity is the literal string passed to `.table("...")` (the same key the
// schema-builder table uses, so query→table converges by name). The terminal
// builder method classifies the operation:
//
//	.get() / .first() / .find( / .pluck( / .count( ...   -> select
//	.insert( / .insertId( / .insertID(                   -> insert
//	.update(                                              -> update
//	.delete(                                              -> delete
//
// What this extractor emits (framework=allographer):
//   - one SCOPE.Schema/table per distinct table referenced by an rdb() chain
//     (identity = the .table("...") literal; converges with the schema-builder
//     table of the same name), carrying a QUERIES edge table -> table (bare table
//     name, resolved by the shared resolver) per distinct operation, with props:
//     operation, table, and transaction=true when the chain is inside an
//     rdb().transaction(...) block (query-builder transaction stamping).
//
// This reuses the SCOPE.Schema Kind + the QUERIES edge kind the Norm (#4904) and
// Debby (#5028) query-attribution extractors already use — no new kind.
//
// Honest exclusions / follow-ups (#5112):
//   - dynamic table names (a variable, not a string literal) are skipped — no
//     fabricated query.
//   - raw SQL via `rdb().raw("SELECT ...")` is not parsed for its target table
//     (no table() anchor); only the fluent .table("...") form is attributed.
//   - join targets (`.join("other", ...)`) inside a chain are not yet attributed
//     as a second QUERIES edge — only the primary .table("...") is captured.
//   - the transaction boundary is stamped on the queries inside it; a standalone
//     SCOPE.Operation transaction entity is not synthesised.
//
// Registration key: "custom_nim_allographer_query".
package nim

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_nim_allographer_query", &nimAllographerQueryExtractor{})
}

type nimAllographerQueryExtractor struct{}

func (e *nimAllographerQueryExtractor) Language() string {
	return "custom_nim_allographer_query"
}

var (
	// nimAlloRdbTableRe anchors an rdb() query chain on its `.table("name")`. We
	// require the `.table(` to be reachable from an `rdb()` head; the pre-filter
	// guarantees both tokens are present, and we bound each chain from the rdb()
	// head so a `.table("...")` that belongs to a schema().create block is not
	// misattributed. Group 1 is the table name string literal.
	nimAlloRdbHeadRe  = regexp.MustCompile(`\brdb\s*\(\s*\)`)
	nimAlloRdbTableRe = regexp.MustCompile(`\.\s*table\s*\(\s*"([^"]+)"`)

	// rdb().transaction( ... ) — the query-builder transaction boundary. Queries
	// whose chain head falls inside this block are stamped transaction=true.
	nimAlloRdbTxnHeadRe = regexp.MustCompile(`\brdb\s*\(\s*\)\s*\.\s*transaction\s*\(`)

	// Operation classifiers, applied to a single rdb() chain body.
	nimAlloOpSelectRe = regexp.MustCompile(`\.\s*(?:get|first|find|pluck|count|max|min|avg|sum)\s*\(`)
	nimAlloOpInsertRe = regexp.MustCompile(`\.\s*(?:insert|insertId|insertID)\s*\(`)
	nimAlloOpUpdateRe = regexp.MustCompile(`\.\s*update\s*\(`)
	nimAlloOpDeleteRe = regexp.MustCompile(`\.\s*delete\s*\(`)
)

// nimAllographerHasQuery is a fast pre-filter: the file must use the rdb() query
// builder against a table to be worth scanning.
func nimAllographerHasQuery(content string) bool {
	if !strings.Contains(content, "rdb(") {
		return false
	}
	return strings.Contains(content, ".table(")
}

func (e *nimAllographerQueryExtractor) Extract(
	ctx context.Context,
	file extractor.FileInput,
) ([]types.EntityRecord, error) {
	if len(file.Content) == 0 || file.Language != "nim" {
		return nil, nil
	}
	src := string(file.Content)
	if !nimAllographerHasQuery(src) {
		return nil, nil
	}

	// Transaction byte ranges: each rdb().transaction(...) balanced body. A query
	// chain head inside one of these ranges is stamped transaction=true.
	var txnRanges [][2]int
	for _, h := range nimAlloRdbTxnHeadRe.FindAllStringIndex(src, -1) {
		openIdx := h[1] - 1 // the '(' that opens transaction(
		if openIdx < 0 || openIdx >= len(src) || src[openIdx] != '(' {
			continue
		}
		end := txnBlockEnd(src, openIdx)
		txnRanges = append(txnRanges, [2]int{h[0], end})
	}
	inTxn := func(pos int) bool {
		for _, r := range txnRanges {
			if pos >= r[0] && pos < r[1] {
				return true
			}
		}
		return false
	}

	// Each rdb() head begins a query chain bounded by the next rdb() head (or EOF).
	heads := nimAlloRdbHeadRe.FindAllStringIndex(src, -1)
	if len(heads) == 0 {
		return nil, nil
	}

	// Aggregate ops per table: table -> set of (operation, transaction) keys, and
	// keep the first source line per table for the entity position.
	type opKey struct {
		op  string
		txn bool
	}
	tableOps := make(map[string]map[opKey]bool)
	tableLine := make(map[string]int)
	var tableOrder []string

	for i, h := range heads {
		start := h[0]
		end := len(src)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		chain := src[start:end]

		// The `.table("name")` anchoring this chain. Skip a transaction-only head
		// (rdb().transaction(...)) — it has no .table of its own.
		tm := nimAlloRdbTableRe.FindStringSubmatchIndex(chain)
		if tm == nil {
			continue
		}
		table := chain[tm[2]:tm[3]]
		// The chain body that classifies the op runs from the table() anchor to the
		// end of this rdb() chain.
		body := chain[tm[1]:]

		var op string
		switch {
		case nimAlloOpInsertRe.MatchString(body):
			op = "insert"
		case nimAlloOpUpdateRe.MatchString(body):
			op = "update"
		case nimAlloOpDeleteRe.MatchString(body):
			op = "delete"
		case nimAlloOpSelectRe.MatchString(body):
			op = "select"
		default:
			// A `.table("t")` with no terminal op in this chain segment is a
			// read builder whose terminal lands in the next segment; default to
			// select so the table access is still attributed (the common case is
			// `let x = rdb().table("t")...get()` on one logical chain).
			op = "select"
		}

		txn := inTxn(start)
		if tableOps[table] == nil {
			tableOps[table] = make(map[opKey]bool)
			tableLine[table] = nimLineOf(src, start)
			tableOrder = append(tableOrder, table)
		}
		tableOps[table][opKey{op: op, txn: txn}] = true
	}

	if len(tableOrder) == 0 {
		return nil, nil
	}
	sort.Strings(tableOrder)

	var out []types.EntityRecord
	for _, table := range tableOrder {
		tbl := newAllographerSchema(table, "table", file.Path, tableLine[table],
			"INFERRED_FROM_ALLOGRAPHER_QUERY")
		// Deterministic op edge order.
		keys := make([]opKey, 0, len(tableOps[table]))
		for k := range tableOps[table] {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].op != keys[j].op {
				return keys[i].op < keys[j].op
			}
			return !keys[i].txn && keys[j].txn
		})
		var rels []types.RelationshipRecord
		for _, k := range keys {
			props := map[string]string{
				"operation": k.op,
				"table":     table,
			}
			if k.txn {
				props["transaction"] = "true"
			}
			rels = append(rels, types.RelationshipRecord{
				ToID:       table,
				Kind:       "QUERIES",
				Properties: props,
			})
		}
		tbl.Relationships = rels
		tbl.ID = tbl.ComputeID()
		out = append(out, tbl)
	}
	return out, nil
}

// txnBlockEnd returns the byte offset just past the balanced () pair starting at
// the '(' at openIdx. If unbalanced, returns len(src).
func txnBlockEnd(src string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}
