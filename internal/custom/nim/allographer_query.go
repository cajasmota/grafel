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
// #5116 deepening (follow-up to #5030):
//   - JOIN targets (`.join("other", ...)`/`.leftJoin(...)`/`.rightJoin(...)`/
//     `.innerJoin(...)`) inside a chain are attributed as additional QUERIES edges
//     (operation=select, join=true) against each joined table, so a multi-table
//     read converges on every accessed table, not just the primary `.table(...)`.
//   - raw SQL via `rdb().raw("SELECT ... FROM t")` is parsed for its FROM/JOIN/
//     INTO/UPDATE target table(s) and attributed (operation classified from the
//     leading SQL verb, raw=true) — even though there is no `.table()` anchor.
//   - dynamic table names (`.table(ident)` where ident is a const/let/var
//     string-literal binding) are resolved to their literal value; truly unbound
//     identifiers are still skipped (no fabricated query).
//   - a standalone SCOPE.Operation transaction-boundary entity is synthesised per
//     `rdb().transaction(...)` block (framework=allographer, subtype=transaction),
//     in addition to stamping transaction=true on the enclosed queries.
//
// Honest remainder: nested/named transactions are not distinguished (each
// rdb().transaction(...) head is one boundary entity); raw-SQL parsing is a
// best-effort FROM/JOIN/INTO/UPDATE table scan, not a full SQL parse.
//
// Registration key: "custom_nim_allographer_query".
package nim

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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

	// #5116(c): a `.table(ident)` whose argument is a bare identifier (not a string
	// literal). Resolved against const/let/var string-literal bindings; unbound
	// identifiers are skipped (no fabricated query). nimAlloStrBindingRe is the
	// shared binding regex declared in allographer_migrations.go.
	nimAlloRdbTableIdentRe = regexp.MustCompile(`\.\s*table\s*\(\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)

	// #5116(a): join targets in a chain — `.join("other", ...)` and the directional
	// variants. Group 1 is the joined table string literal. Each is a second
	// (select) access against the joined table.
	nimAlloRdbJoinRe = regexp.MustCompile(`\.\s*(?:join|leftJoin|rightJoin|innerJoin|outerJoin|crossJoin)\s*\(\s*"([^"]+)"`)

	// rdb().transaction( ... ) — the query-builder transaction boundary. Queries
	// whose chain head falls inside this block are stamped transaction=true, and a
	// standalone SCOPE.Operation transaction entity is synthesised per block (#5116(d)).
	nimAlloRdbTxnHeadRe = regexp.MustCompile(`\brdb\s*\(\s*\)\s*\.\s*transaction\s*\(`)

	// #5116(b): raw SQL via `rdb().raw("SELECT ... FROM t")`. Group 1 is the SQL
	// string literal; its target table(s) are scanned out below.
	nimAlloRdbRawRe = regexp.MustCompile(`\.\s*raw\s*\(\s*"([^"]*)"`)
	// FROM/JOIN/INTO/UPDATE <table> inside a raw SQL string (table = first bare
	// word, optionally quoted/backticked, ignoring a schema-qualified prefix).
	nimAlloRawTableRe = regexp.MustCompile(`(?i)\b(?:from|join|into|update)\s+["` + "`" + `]?([A-Za-z_][A-Za-z0-9_]*)`)

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
	// .table( anchors the fluent builder; .raw( anchors a raw-SQL query that has
	// no .table() anchor (#5116(b)).
	return strings.Contains(content, ".table(") || strings.Contains(content, ".raw(")
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

	// #5116(c): const/let/var string-literal bindings, so a `.table(ident)` whose
	// argument is a bare identifier can be resolved to its literal. Reuses the
	// shared binding regex declared in allographer_migrations.go.
	bindings := make(map[string]string)
	for _, m := range nimAlloStrBindingRe.FindAllStringSubmatch(src, -1) {
		bindings[m[1]] = m[2]
	}

	// Aggregate ops per table: table -> set of (operation, transaction, join, raw)
	// keys, and keep the first source line per table for the entity position.
	type opKey struct {
		op   string
		txn  bool
		join bool
		raw  bool
	}
	tableOps := make(map[string]map[opKey]bool)
	tableLine := make(map[string]int)
	var tableOrder []string

	record := func(table string, k opKey, pos int) {
		if table == "" {
			return
		}
		if tableOps[table] == nil {
			tableOps[table] = make(map[opKey]bool)
			tableLine[table] = nimLineOf(src, pos)
			tableOrder = append(tableOrder, table)
		}
		tableOps[table][k] = true
	}

	for i, h := range heads {
		start := h[0]
		end := len(src)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		chain := src[start:end]
		txn := inTxn(start)

		// #5116(b): raw SQL — `rdb().raw("SELECT ... FROM t ...")`. No .table()
		// anchor; classify the op from the leading SQL verb and scan out every
		// FROM/JOIN/INTO/UPDATE target table.
		if rm := nimAlloRdbRawRe.FindStringSubmatchIndex(chain); rm != nil {
			sql := chain[rm[2]:rm[3]]
			rawOp := classifyRawSQL(sql)
			for _, tm := range nimAlloRawTableRe.FindAllStringSubmatch(sql, -1) {
				record(tm[1], opKey{op: rawOp, txn: txn, raw: true}, start)
			}
			// A raw() chain has no fluent .table() anchor; nothing else to do.
			continue
		}

		// The `.table("name")` (or `.table(ident)`) anchoring this chain. Skip a
		// transaction-only head (rdb().transaction(...)) — it has no .table.
		var table string
		var anchorEnd int
		if tm := nimAlloRdbTableRe.FindStringSubmatchIndex(chain); tm != nil {
			table = chain[tm[2]:tm[3]]
			anchorEnd = tm[1]
		} else if tm := nimAlloRdbTableIdentRe.FindStringSubmatchIndex(chain); tm != nil {
			// #5116(c): dynamic table name — resolve the identifier binding, else skip.
			if v, ok := bindings[chain[tm[2]:tm[3]]]; ok {
				table = v
				anchorEnd = tm[1]
			} else {
				continue
			}
		} else {
			continue
		}

		// The chain body that classifies the op runs from the table() anchor to the
		// end of this rdb() chain.
		body := chain[anchorEnd:]

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

		record(table, opKey{op: op, txn: txn}, start)

		// #5116(a): JOIN targets — each joined table is a second (select) access.
		for _, jm := range nimAlloRdbJoinRe.FindAllStringSubmatch(body, -1) {
			record(jm[1], opKey{op: "select", txn: txn, join: true}, start)
		}
	}

	var out []types.EntityRecord

	// #5116(d): a standalone SCOPE.Operation transaction-boundary entity per
	// rdb().transaction(...) block, in addition to the transaction=true stamping.
	for _, r := range txnRanges {
		txnEnt := types.EntityRecord{
			Name:       "rdb().transaction",
			Kind:       "SCOPE.Operation",
			Subtype:    "transaction",
			SourceFile: file.Path,
			Language:   "nim",
			StartLine:  nimLineOf(src, r[0]),
			EndLine:    nimLineOf(src, r[1]-1),
			Properties: map[string]string{
				"framework":   "allographer",
				"provenance":  "INFERRED_FROM_ALLOGRAPHER_TRANSACTION",
				"transaction": "true",
			},
		}
		txnEnt.ID = txnEnt.ComputeID()
		out = append(out, txnEnt)
	}

	if len(tableOrder) == 0 {
		return out, nil
	}
	sort.Strings(tableOrder)

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
			if keys[i].txn != keys[j].txn {
				return !keys[i].txn && keys[j].txn
			}
			if keys[i].join != keys[j].join {
				return !keys[i].join && keys[j].join
			}
			return !keys[i].raw && keys[j].raw
		})
		var rels []types.RelationshipRecord
		for _, k := range keys {
			props := types.Props{
				{K: "operation", V: k.op},
				{K: "table", V: table},
			}
			if k.txn {
				props.Set("transaction", "true")
			}
			if k.join {
				props.Set("join", "true")
			}
			if k.raw {
				props.Set("raw", "true")
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

// classifyRawSQL maps the leading verb of a raw SQL string to the same operation
// vocabulary the fluent builder uses (#5116(b)). Unknown/empty -> select (a
// read-shaped default, matching the fluent .table() fallback).
func classifyRawSQL(sql string) string {
	s := strings.ToLower(strings.TrimSpace(sql))
	switch {
	case strings.HasPrefix(s, "insert"):
		return "insert"
	case strings.HasPrefix(s, "update"):
		return "update"
	case strings.HasPrefix(s, "delete"):
		return "delete"
	default:
		return "select"
	}
}
