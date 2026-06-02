// Package txscope detects database transaction boundaries that are lexically
// present inside a function/method body and returns the properties to stamp on
// the enclosing function entity so the graph can answer "which operations run
// inside a DB transaction?" — valuable for transaction-parity during a rewrite.
//
// The detector is deliberately lexical and conservative. It stamps a function
// only when a transaction-opening construct (Spring @Transactional, Django
// transaction.atomic, SQLAlchemy session.begin, Rails Model.transaction do,
// Sequelize/TypeORM .transaction(...), Go db.Begin()/BeginTx) is present in the
// function's own source span.
//
// HONESTY BOUNDARY — no transitive propagation.
// A transaction opened in one function and passed (as a tx/session/manager
// handle) into a callee is NOT propagated to the callee. Only the function
// where the opening construct lexically appears is stamped. A function that
// merely RECEIVES a tx parameter but does not open one is not stamped. This
// avoids false positives at the cost of not chasing the dynamic call graph —
// the same safer-bias rule the rest of the extractors follow.
package txscope

import "regexp"

// Stamp is the set of properties the detector contributes to a function entity.
// Empty Source means "no transaction boundary found" — callers must check
// Transactional before stamping.
type Stamp struct {
	// Transactional is true when a transaction-opening construct is lexically
	// present in the function body.
	Transactional bool
	// Source identifies the signal that triggered the stamp, e.g.
	// "spring_transactional", "django_atomic", "sqlalchemy_begin",
	// "rails_transaction", "sequelize_transaction", "typeorm_transaction",
	// "go_sql_begin". Stamped as Properties["tx_source"].
	Source string
	// Propagation is the Spring propagation mode (REQUIRED, REQUIRES_NEW, …)
	// when available; empty otherwise. Stamped as Properties["tx_propagation"].
	Propagation string
	// Isolation is the isolation level when available (Spring isolation= or Go
	// sql.TxOptions Isolation). Stamped as Properties["tx_isolation"].
	Isolation string
	// ReadOnly is "true"/"false" when a readOnly attribute is present (Spring);
	// empty otherwise. Stamped as Properties["tx_read_only"].
	ReadOnly string
}

// Apply writes the stamp's properties onto props (allocating if nil) and
// returns the map. It is a no-op when the stamp is not transactional.
func (s Stamp) Apply(props map[string]string) map[string]string {
	if !s.Transactional {
		return props
	}
	if props == nil {
		props = map[string]string{}
	}
	props["transactional"] = "true"
	if s.Source != "" {
		props["tx_source"] = s.Source
	}
	if s.Propagation != "" {
		props["tx_propagation"] = s.Propagation
	}
	if s.Isolation != "" {
		props["tx_isolation"] = s.Isolation
	}
	if s.ReadOnly != "" {
		props["tx_read_only"] = s.ReadOnly
	}
	return props
}

// ---- Java / Kotlin (Spring & Jakarta/JTA @Transactional) -------------------

var (
	javaTransactionalRE = regexp.MustCompile(`@Transactional\b\s*(?:\(([^)]*)\))?`)
	// entityManager.getTransaction().begin() — programmatic JPA transaction.
	javaEMBeginRE   = regexp.MustCompile(`\bgetTransaction\s*\(\s*\)\s*\.\s*begin\s*\(`)
	javaPropagRE    = regexp.MustCompile(`propagation\s*=\s*(?:Propagation\.)?(\w+)`)
	javaTxTypeRE    = regexp.MustCompile(`TxType\.(\w+)`)
	javaIsolationRE = regexp.MustCompile(`isolation\s*=\s*(?:Isolation\.)?(\w+)`)
	javaReadOnlyRE  = regexp.MustCompile(`readOnly\s*=\s*(true|false)`)
)

// DetectJava returns the transaction stamp for a Java/Kotlin method body+annotation
// span. Pass the source covering the method's leading annotations through its
// closing brace (the @Transactional annotation sits just above the signature, so
// callers should include the annotation lines).
func DetectJava(src string) Stamp {
	if m := javaTransactionalRE.FindStringSubmatch(src); m != nil {
		s := Stamp{Transactional: true, Source: "spring_transactional"}
		body := m[1]
		if pm := javaPropagRE.FindStringSubmatch(body); pm != nil {
			s.Propagation = pm[1]
		} else if pm := javaTxTypeRE.FindStringSubmatch(body); pm != nil {
			s.Propagation = pm[1]
		}
		if im := javaIsolationRE.FindStringSubmatch(body); im != nil {
			s.Isolation = im[1]
		}
		if rm := javaReadOnlyRE.FindStringSubmatch(body); rm != nil {
			s.ReadOnly = rm[1]
		}
		return s
	}
	if javaEMBeginRE.MatchString(src) {
		return Stamp{Transactional: true, Source: "jpa_em_begin"}
	}
	return Stamp{}
}

// ---- Python (Django transaction.atomic / SQLAlchemy session.begin) ---------

var (
	// @transaction.atomic decorator (bare or with parens) OR a
	// `with transaction.atomic():` block lexically inside the function body.
	pyDjangoAtomicRE = regexp.MustCompile(`(?:@transaction\.atomic\b|\btransaction\.atomic\s*\()`)
	// session.begin() / session.begin_nested() / with session.begin(): /
	// with Session.begin() as ... ; matches the `.begin(` call site.
	pySQLAlchemyBeginRE = regexp.MustCompile(`\b\w*[Ss]ession\.begin(?:_nested)?\s*\(`)
	// engine.begin() — SQLAlchemy core connection-level transaction.
	pyEngineBeginRE = regexp.MustCompile(`\b\w*[Ee]ngine\.begin\s*\(`)
)

// DetectPython returns the transaction stamp for a Python function body. Pass
// the source covering the function's decorator lines through the end of its
// body so a `@transaction.atomic` decorator above the `def` is seen.
func DetectPython(src string) Stamp {
	if pyDjangoAtomicRE.MatchString(src) {
		return Stamp{Transactional: true, Source: "django_atomic"}
	}
	if pySQLAlchemyBeginRE.MatchString(src) {
		return Stamp{Transactional: true, Source: "sqlalchemy_begin"}
	}
	if pyEngineBeginRE.MatchString(src) {
		return Stamp{Transactional: true, Source: "sqlalchemy_begin"}
	}
	return Stamp{}
}

// ---- Ruby (Rails ActiveRecord::Base.transaction do / Model.transaction do) --

var (
	// `<Receiver>.transaction do` or `<Receiver>.transaction { ... }` where
	// Receiver is a constant/relation path (ActiveRecord::Base, User,
	// self.class, ApplicationRecord). The trailing `do`/`{` distinguishes a
	// transaction BLOCK from an unrelated `foo.transaction` attribute read.
	rubyTransactionRE = regexp.MustCompile(`(?:\b[A-Z]\w*(?:::\w+)*|\bself\b)\s*\.\s*transaction\b\s*(?:\(\s*[^)]*\)\s*)?(?:do\b|\{)`)
)

// DetectRuby returns the transaction stamp for a Ruby method body.
func DetectRuby(src string) Stamp {
	if rubyTransactionRE.MatchString(src) {
		return Stamp{Transactional: true, Source: "rails_transaction"}
	}
	return Stamp{}
}

// ---- JS / TS (Sequelize/TypeORM .transaction(...) / @Transaction decorator) -

var (
	// sequelize.transaction(...) / this.sequelize.transaction(async t => …)
	jsSequelizeTxRE = regexp.MustCompile(`\bsequelize\s*\.\s*transaction\s*\(`)
	// dataSource.transaction(...) / manager.transaction(...) /
	// queryRunner-style entityManager.transaction(...) — TypeORM.
	jsTypeORMTxRE = regexp.MustCompile(`\b(?:dataSource|manager|entityManager|getManager\s*\(\s*\)|connection)\s*\.\s*transaction\s*\(`)
	// @Transaction() decorator (TypeORM legacy declarative transactions).
	jsTransactionDecoratorRE = regexp.MustCompile(`@Transaction\s*\(`)
	// Prisma interactive transaction: prisma.$transaction(async tx => …).
	jsPrismaTxRE = regexp.MustCompile(`\.\$transaction\s*\(`)
	// knex.transaction(...) / trx pattern.
	jsKnexTxRE = regexp.MustCompile(`\bknex\s*\.\s*transaction\s*\(`)
)

// DetectJSTS returns the transaction stamp for a JavaScript/TypeScript function
// body (include leading decorator lines so @Transaction() is seen).
func DetectJSTS(src string) Stamp {
	switch {
	case jsTransactionDecoratorRE.MatchString(src):
		return Stamp{Transactional: true, Source: "typeorm_transaction"}
	case jsTypeORMTxRE.MatchString(src):
		return Stamp{Transactional: true, Source: "typeorm_transaction"}
	case jsSequelizeTxRE.MatchString(src):
		return Stamp{Transactional: true, Source: "sequelize_transaction"}
	case jsPrismaTxRE.MatchString(src):
		return Stamp{Transactional: true, Source: "prisma_transaction"}
	case jsKnexTxRE.MatchString(src):
		return Stamp{Transactional: true, Source: "knex_transaction"}
	}
	return Stamp{}
}

// ---- Go (database/sql db.Begin()/BeginTx, GORM db.Transaction) -------------

var (
	// tx, err := db.Begin()  /  db.Begin()  — the receiver may be any handle
	// name, so match `.Begin(` preceded by a selector. Excludes BeginTx which
	// is handled separately to capture isolation.
	goBeginRE = regexp.MustCompile(`\b\w+\s*\.\s*Begin\s*\(\s*\)`)
	// db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}) — the
	// isolation level is captured when present.
	goBeginTxRE = regexp.MustCompile(`\b\w+\s*\.\s*BeginTx\s*\(`)
	// GORM: db.Transaction(func(tx *gorm.DB) error { … }).
	goGormTxRE     = regexp.MustCompile(`\b\w+\s*\.\s*Transaction\s*\(`)
	goIsolationRE  = regexp.MustCompile(`Isolation\s*:\s*sql\.Level(\w+)`)
	goGormTxImport = regexp.MustCompile(`gorm`)
)

// DetectGo returns the transaction stamp for a Go function body. Pass the
// function body source. The GORM db.Transaction(func(tx *gorm.DB)…) signal is
// only honored when the body references gorm to avoid stamping unrelated
// `.Transaction(` calls.
func DetectGo(src string) Stamp {
	if goBeginTxRE.MatchString(src) {
		s := Stamp{Transactional: true, Source: "go_sql_begin_tx"}
		if im := goIsolationRE.FindStringSubmatch(src); im != nil {
			s.Isolation = im[1]
		}
		return s
	}
	if goBeginRE.MatchString(src) {
		return Stamp{Transactional: true, Source: "go_sql_begin"}
	}
	if goGormTxRE.MatchString(src) && goGormTxImport.MatchString(src) {
		return Stamp{Transactional: true, Source: "gorm_transaction"}
	}
	return Stamp{}
}
