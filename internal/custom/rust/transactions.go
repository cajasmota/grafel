package rust

// transactions.go — transaction_function_stamping for Rust data-access code.
//
// Stamps transaction-boundary posture on Rust functions/closures that open a
// database transaction, mirroring the cross-language transaction_function_stamping
// contract (Go db.Begin/BeginTx, Java/Kotlin @Transactional, Nim/Norm, Crystal/Granite).
//
// Detected transaction APIs:
//
//   - diesel:   conn.transaction(|c| ...)              → framework=diesel
//   - sqlx:     pool.begin() / conn.begin() (Acquire)  → framework=sqlx
//               tx.commit() / tx.rollback()
//   - sea_orm:  db.begin() / txn.commit()              → framework=sea_orm
//               db.transaction(|txn| ...) closure      → framework=sea_orm
//   - rusqlite: conn.transaction() / conn.unchecked_transaction()
//                                                       → framework=rusqlite
//
// For each match we emit one SCOPE.Pattern/transaction_boundary entity. When the
// match sits inside a `fn name(...)`, the entity name is `<fn>.transaction` and
// the enclosing function is recorded via the `function` property (the "function
// stamp"). Otherwise we fall back to the `<receiver>.transaction` boundary shape
// used by the Crystal/Nim extractors.
//
// Honesty:
//
//	partial — heuristic regex match on source text. Detects the transaction
//	opener and the nearest enclosing `fn`. Does NOT resolve isolation levels,
//	propagation, or perform transitive propagation (a fn receiving a tx handle
//	is not stamped). Fixtures prove the detection surface.
//
// Issue #5021 — lang.rust.orm.{diesel,sqlx,seaorm,rusqlite} transaction_function_stamping.

import (
	"context"
	"regexp"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_rust_transactions", &rustTransactionsExtractor{})
}

type rustTransactionsExtractor struct{}

func (e *rustTransactionsExtractor) Language() string { return "custom_rust_transactions" }

// ---------------------------------------------------------------------------
// Regex catalog
// ---------------------------------------------------------------------------

var (
	// `fn name(` — used to find the enclosing function for a transaction opener.
	reRustFnDecl = regexp.MustCompile(`\bfn\s+([A-Za-z_]\w*)\s*[<(]`)

	// diesel:   <recv>.transaction(|...| ...)  — closure-based tx.
	//           Also matches conn.transaction(|c| { ... }).
	reDieselTx = regexp.MustCompile(`([A-Za-z_]\w*)\s*\.\s*transaction\s*\(\s*\|`)

	// rusqlite: <recv>.transaction()  / <recv>.unchecked_transaction()
	//           (no closure — returns a Transaction guard).
	reRusqliteTx = regexp.MustCompile(`([A-Za-z_]\w*)\s*\.\s*(?:unchecked_transaction|transaction)\s*\(\s*\)`)

	// sqlx / sea_orm:  <recv>.begin()  — pool.begin() / conn.begin() / db.begin()
	reBeginTx = regexp.MustCompile(`([A-Za-z_]\w*)\s*\.\s*begin\s*\(\s*\)`)

	// sea_orm closure tx:  <recv>.transaction(|txn| async move { ... })
	// (covered structurally by reDieselTx; framework disambiguated by imports.)

	// commit / rollback on a tx handle — confirms an explicit tx lifecycle.
	reCommitTx = regexp.MustCompile(`([A-Za-z_]\w*)\s*\.\s*(commit|rollback)\s*\(\s*\)`)

	// Framework import / usage signals for disambiguation.
	reUseDiesel   = regexp.MustCompile(`\b(?:use\s+diesel\b|diesel::)`)
	reUseSqlx     = regexp.MustCompile(`\b(?:use\s+sqlx\b|sqlx::|PgPool|MySqlPool|SqlitePool)\b`)
	reUseSeaOrm   = regexp.MustCompile(`\b(?:use\s+sea_orm\b|sea_orm::|DatabaseConnection|DatabaseTransaction|TransactionTrait)\b`)
	reUseRusqlite = regexp.MustCompile(`\b(?:use\s+rusqlite\b|rusqlite::)`)
)

// ---------------------------------------------------------------------------
// Extract
// ---------------------------------------------------------------------------

func (e *rustTransactionsExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/rust")
	_, span := tracer.Start(ctx, "indexer.rust_transactions_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 || file.Language != "rust" {
		return nil, nil
	}

	src := string(file.Content)
	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Subtype + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	hasDiesel := reUseDiesel.MatchString(src)
	hasSqlx := reUseSqlx.MatchString(src)
	hasSeaOrm := reUseSeaOrm.MatchString(src)
	hasRusqlite := reUseRusqlite.MatchString(src)

	// Pre-compute function declaration offsets so we can resolve the enclosing fn
	// for any transaction opener via a backward scan.
	fnDecls := reRustFnDecl.FindAllStringSubmatchIndex(src, -1)
	enclosingFn := func(off int) string {
		name := ""
		for _, fd := range fnDecls {
			if fd[0] <= off {
				name = src[fd[2]:fd[3]]
			} else {
				break
			}
		}
		return name
	}

	emit := func(off int, recv, framework, api, provenance string) {
		fn := enclosingFn(off)
		name := recv + ".transaction"
		if fn != "" {
			name = fn + ".transaction"
		}
		ent := makeEntity(name, "SCOPE.Pattern", "transaction_boundary",
			file.Path, file.Language, lineOf(src, off))
		setProps(&ent,
			"framework", framework,
			"transactional", "true",
			"transaction_api", api,
			"db_handle", recv,
			"provenance", provenance,
		)
		if fn != "" {
			ent.Properties["function"] = fn
		}
		add(ent)
	}

	// 1. diesel / sea_orm closure transaction: <recv>.transaction(|...|)
	for _, m := range reDieselTx.FindAllStringSubmatchIndex(src, -1) {
		recv := src[m[2]:m[3]]
		framework, api, prov := "diesel", "diesel_transaction", "INFERRED_FROM_DIESEL_TRANSACTION"
		// sea_orm also exposes db.transaction(|txn| ...); disambiguate by imports
		// when diesel is absent but sea_orm is present.
		if hasSeaOrm && !hasDiesel {
			framework, api, prov = "sea_orm", "sea_orm_transaction_closure", "INFERRED_FROM_SEA_ORM_TRANSACTION"
		}
		emit(m[0], recv, framework, api, prov)
	}

	// 2. rusqlite: <recv>.transaction() / unchecked_transaction()
	//    Only when rusqlite is in scope — guards against false positives where a
	//    diesel/sea_orm closure form (handled above) would otherwise re-match.
	if hasRusqlite {
		for _, m := range reRusqliteTx.FindAllStringSubmatchIndex(src, -1) {
			recv := src[m[2]:m[3]]
			emit(m[0], recv, "rusqlite", "rusqlite_transaction", "INFERRED_FROM_RUSQLITE_TRANSACTION")
		}
	}

	// 3. sqlx / sea_orm explicit begin(): pool.begin() / db.begin()
	for _, m := range reBeginTx.FindAllStringSubmatchIndex(src, -1) {
		recv := src[m[2]:m[3]]
		framework, api, prov := "sqlx", "sqlx_begin", "INFERRED_FROM_SQLX_BEGIN"
		if hasSeaOrm && !hasSqlx {
			framework, api, prov = "sea_orm", "sea_orm_begin", "INFERRED_FROM_SEA_ORM_BEGIN"
		}
		emit(m[0], recv, framework, api, prov)
	}

	// 4. commit/rollback — stamp the enclosing fn as a tx boundary even when the
	//    opener lives in another statement form we don't catch. We pick the
	//    framework from the dominant import signal.
	for _, m := range reCommitTx.FindAllStringSubmatchIndex(src, -1) {
		recv := src[m[2]:m[3]]
		framework, api, prov := "", "", ""
		switch {
		case hasSeaOrm:
			framework, api, prov = "sea_orm", "sea_orm_commit", "INFERRED_FROM_SEA_ORM_COMMIT"
		case hasSqlx:
			framework, api, prov = "sqlx", "sqlx_commit", "INFERRED_FROM_SQLX_COMMIT"
		default:
			continue // commit() without a sqlx/sea_orm tx context — skip to stay honest.
		}
		emit(m[0], recv, framework, api, prov)
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
