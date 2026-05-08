package patterns

import (
	"fmt"
	"regexp"

	"github.com/cajasmota/archigraph/internal/types"
)

// transactionChangesetEnricher detects transaction and changeset patterns.
// Matches Python transaction_changeset_enricher.py.
type transactionChangesetEnricher struct{}

var (
	tcEctoChangesetRE     = regexp.MustCompile(`(?m)^[ \t]*def\s+changeset\b`)
	tcEctoMultiTriggerRE  = regexp.MustCompile(`\bEcto\.Multi\b`)
tcJavaTransactionalRE = regexp.MustCompile(`@Transactional\b`)
	tcJavaEMTxRE          = regexp.MustCompile(`entityManager\.(?:getTransaction|begin|commit|rollback)\s*\(`)
	tcPyDjangoTxRE        = regexp.MustCompile(`(?:transaction\.atomic|@transaction\.atomic)\b`)
	tcPySQLAlchemyTxRE    = regexp.MustCompile(`(?:session\.begin|with\s+session\.begin)`)
	tcGoSQLTxRE           = regexp.MustCompile(`db\.Begin(?:Tx)?\s*\(`)
	tcGoGORMTxRE          = regexp.MustCompile(`db\.Transaction\s*\(`)
	tcJSPrismaTxRE        = regexp.MustCompile(`\$transaction\s*\(`)
)

func (t *transactionChangesetEnricher) Category() string { return "transaction_changeset" }

func (t *transactionChangesetEnricher) AppliesTo(src string) bool {
	return tcEctoChangesetRE.MatchString(src) ||
		tcEctoMultiTriggerRE.MatchString(src) ||
		tcJavaTransactionalRE.MatchString(src) ||
		tcPyDjangoTxRE.MatchString(src) ||
		tcPySQLAlchemyTxRE.MatchString(src) ||
		tcGoSQLTxRE.MatchString(src) ||
		tcGoGORMTxRE.MatchString(src) ||
		tcJSPrismaTxRE.MatchString(src)
}

func (t *transactionChangesetEnricher) Detect(filePath, language, src string) []types.EntityRecord {
	var results []types.EntityRecord
	seen := map[string]bool{}

	emit := func(key, name, txPattern, framework string, line int) {
		if seen[key] {
			return
		}
		seen[key] = true
		results = append(results, makeEntity(filePath,
			name, "SCOPE.Pattern", "transaction_changeset", language, line,
			map[string]string{"kind": "transaction_changeset", "tx_pattern": txPattern, "framework": framework}))
	}

	if m := tcEctoMultiTriggerRE.FindStringIndex(src); m != nil {
		emit("elixir:ecto_multi", "transaction_ecto_multi", "multi_transaction", "ecto", lineOf(src, m[0]))
	}
	if m := tcJavaTransactionalRE.FindStringIndex(src); m != nil {
		emit("java:transactional", "transaction_spring", "declarative_transaction", "spring", lineOf(src, m[0]))
	}
	if m := tcJavaEMTxRE.FindStringIndex(src); m != nil {
		emit("java:em_tx", "transaction_java_em", "programmatic_transaction", "jpa", lineOf(src, m[0]))
	}
	if m := tcPyDjangoTxRE.FindStringIndex(src); m != nil {
		emit("py:django_atomic", "transaction_django_atomic", "atomic_transaction", "django", lineOf(src, m[0]))
	}
	if m := tcPySQLAlchemyTxRE.FindStringIndex(src); m != nil {
		emit("py:sqlalchemy_tx", "transaction_sqlalchemy", "session_transaction", "sqlalchemy", lineOf(src, m[0]))
	}
	if m := tcGoSQLTxRE.FindStringIndex(src); m != nil {
		emit("go:sql_tx", "transaction_go_sql", "begin_tx", "database/sql", lineOf(src, m[0]))
	}
	if m := tcGoGORMTxRE.FindStringIndex(src); m != nil {
		emit("go:gorm_tx", "transaction_gorm", "gorm_transaction", "gorm", lineOf(src, m[0]))
	}
	if m := tcJSPrismaTxRE.FindStringIndex(src); m != nil {
		emit("js:prisma_tx", "transaction_prisma", "prisma_transaction", "prisma", lineOf(src, m[0]))
	}

	// Ecto changeset validation — detect validate_required/validate_format etc.
	for _, m := range tcEctoChangesetRE.FindAllStringIndex(src, -1) {
		line := lineOf(src, m[0])
		key := fmt.Sprintf("elixir:changeset_validation:%d", line)
		name := fmt.Sprintf("changeset-validation@%s:%d", filePath, line)
		if !seen[key] {
			seen[key] = true
			results = append(results, makeEntity(filePath,
				name, "SCOPE.Pattern", "transaction_changeset", language, line,
				map[string]string{"kind": "changeset_validation", "framework": "ecto"}))
		}
	}

	return results
}

func init() {
	Register(&transactionChangesetEnricher{})
}
