package txscope

import "testing"

func TestDetectJava_TransactionalWithPropagation(t *testing.T) {
	src := `
    @Transactional(propagation = Propagation.REQUIRES_NEW, isolation = Isolation.SERIALIZABLE, readOnly = false)
    public void transfer(Account a, Account b, BigDecimal amt) {
        debit(a, amt);
        credit(b, amt);
    }`
	s := DetectJava(src)
	if !s.Transactional {
		t.Fatalf("expected transfer() transactional=true")
	}
	if s.Source != "spring_transactional" {
		t.Errorf("tx_source = %q, want spring_transactional", s.Source)
	}
	if s.Propagation != "REQUIRES_NEW" {
		t.Errorf("tx_propagation = %q, want REQUIRES_NEW", s.Propagation)
	}
	if s.Isolation != "SERIALIZABLE" {
		t.Errorf("tx_isolation = %q, want SERIALIZABLE", s.Isolation)
	}
	if s.ReadOnly != "false" {
		t.Errorf("tx_read_only = %q, want false", s.ReadOnly)
	}
}

func TestDetectJava_BareTransactional(t *testing.T) {
	s := DetectJava("    @Transactional\n    public void save() {}")
	if !s.Transactional || s.Source != "spring_transactional" {
		t.Fatalf("bare @Transactional should stamp, got %+v", s)
	}
	if s.Propagation != "" {
		t.Errorf("no propagation expected, got %q", s.Propagation)
	}
}

func TestDetectJava_JtaTxType(t *testing.T) {
	s := DetectJava("@Transactional(Transactional.TxType.MANDATORY)\nvoid run(){}")
	if !s.Transactional || s.Propagation != "MANDATORY" {
		t.Fatalf("JTA TxType propagation not captured: %+v", s)
	}
}

func TestDetectJava_NoTransaction(t *testing.T) {
	s := DetectJava("public void plain() { repo.save(x); }")
	if s.Transactional {
		t.Fatalf("plain method must not be stamped: %+v", s)
	}
}

func TestDetectPython_AtomicDecorator(t *testing.T) {
	src := `
@transaction.atomic
def checkout(request):
    order.save()`
	s := DetectPython(src)
	if !s.Transactional || s.Source != "django_atomic" {
		t.Fatalf("@transaction.atomic on checkout should stamp django_atomic, got %+v", s)
	}
}

func TestDetectPython_AtomicBlock(t *testing.T) {
	src := `
def process(items):
    with transaction.atomic():
        for i in items:
            i.save()`
	s := DetectPython(src)
	if !s.Transactional || s.Source != "django_atomic" {
		t.Fatalf("with transaction.atomic(): in process should stamp, got %+v", s)
	}
}

func TestDetectPython_SQLAlchemyBegin(t *testing.T) {
	src := `
def commit_all(session):
    with session.begin():
        session.add(obj)`
	s := DetectPython(src)
	if !s.Transactional || s.Source != "sqlalchemy_begin" {
		t.Fatalf("session.begin() should stamp sqlalchemy_begin, got %+v", s)
	}
}

func TestDetectPython_ReceivesSessionButNoBegin(t *testing.T) {
	// Honesty boundary: a fn that only RECEIVES a session/tx but does not open
	// a transaction must NOT be stamped.
	src := `
def add_row(session, row):
    session.add(row)
    session.flush()`
	s := DetectPython(src)
	if s.Transactional {
		t.Fatalf("add_row only receives session, must not be stamped: %+v", s)
	}
}

func TestDetectPython_NoTransaction(t *testing.T) {
	s := DetectPython("def f(x):\n    return x + 1")
	if s.Transactional {
		t.Fatalf("no tx expected: %+v", s)
	}
}

func TestDetectRuby_TransactionDo(t *testing.T) {
	src := `
  def pay(amount)
    ActiveRecord::Base.transaction do
      account.debit!(amount)
      ledger.record!(amount)
    end
  end`
	s := DetectRuby(src)
	if !s.Transactional || s.Source != "rails_transaction" {
		t.Fatalf("transaction do in pay should stamp rails_transaction, got %+v", s)
	}
}

func TestDetectRuby_ModelTransactionBlock(t *testing.T) {
	s := DetectRuby("def refund\n  Order.transaction { order.update!(state: :refunded) }\nend")
	if !s.Transactional {
		t.Fatalf("Model.transaction { } should stamp: %+v", s)
	}
}

func TestDetectRuby_NegativeNonDBTransaction(t *testing.T) {
	// A `transaction` method on a non-DB object (lowercase receiver, no block,
	// or an attribute read) must not be stamped.
	if s := DetectRuby("def total\n  payment.transaction.amount\nend"); s.Transactional {
		t.Fatalf("attribute read payment.transaction must not stamp: %+v", s)
	}
}

func TestDetectJSTS_SequelizeTransaction(t *testing.T) {
	src := `async function transfer() {
    await sequelize.transaction(async (t) => {
      await Account.update({}, { transaction: t });
    });
  }`
	s := DetectJSTS(src)
	if !s.Transactional || s.Source != "sequelize_transaction" {
		t.Fatalf("sequelize.transaction should stamp, got %+v", s)
	}
}

func TestDetectJSTS_TypeORMTransaction(t *testing.T) {
	src := `async save() {
    await this.dataSource.transaction(async (manager) => {
      await manager.save(user);
    });
  }`
	s := DetectJSTS(src)
	if !s.Transactional || s.Source != "typeorm_transaction" {
		t.Fatalf("dataSource.transaction should stamp typeorm_transaction, got %+v", s)
	}
}

func TestDetectJSTS_TransactionDecorator(t *testing.T) {
	s := DetectJSTS("@Transaction()\n  async create(@TransactionManager() m) {}")
	if !s.Transactional || s.Source != "typeorm_transaction" {
		t.Fatalf("@Transaction() decorator should stamp: %+v", s)
	}
}

func TestDetectJSTS_ReceivesManagerNoOpen(t *testing.T) {
	// Honesty: a callee that receives a manager but does not open a tx.
	s := DetectJSTS("async persist(manager) {\n  await manager.save(this.entity);\n}")
	if s.Transactional {
		t.Fatalf("persist only receives manager, must not stamp: %+v", s)
	}
}

func TestDetectGo_DBBeginCommit(t *testing.T) {
	src := `func (s *Store) save(ctx context.Context) error {
    tx, err := s.db.Begin()
    if err != nil { return err }
    defer tx.Rollback()
    if _, err := tx.Exec("..."); err != nil { return err }
    return tx.Commit()
  }`
	s := DetectGo(src)
	if !s.Transactional || s.Source != "go_sql_begin" {
		t.Fatalf("db.Begin()+Commit in save should stamp go_sql_begin, got %+v", s)
	}
}

func TestDetectGo_BeginTxWithIsolation(t *testing.T) {
	src := `func transfer(ctx context.Context, db *sql.DB) error {
    tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
    _ = tx
    return err
  }`
	s := DetectGo(src)
	if !s.Transactional || s.Source != "go_sql_begin_tx" {
		t.Fatalf("BeginTx should stamp go_sql_begin_tx, got %+v", s)
	}
	if s.Isolation != "Serializable" {
		t.Errorf("tx_isolation = %q, want Serializable", s.Isolation)
	}
}

func TestDetectGo_GormTransaction(t *testing.T) {
	src := `func run(db *gorm.DB) error {
    return db.Transaction(func(tx *gorm.DB) error {
      return tx.Create(&u).Error
    })
  }`
	s := DetectGo(src)
	if !s.Transactional || s.Source != "gorm_transaction" {
		t.Fatalf("gorm db.Transaction should stamp gorm_transaction, got %+v", s)
	}
}

func TestDetectGo_ReceivesTxNoOpen(t *testing.T) {
	// Honesty: a helper that receives a *sql.Tx and uses it but never opens one.
	src := `func insertRow(tx *sql.Tx, r Row) error {
    _, err := tx.Exec("INSERT ...", r.ID)
    return err
  }`
	s := DetectGo(src)
	if s.Transactional {
		t.Fatalf("insertRow receives tx but does not Begin, must not stamp: %+v", s)
	}
}

func TestDetectGo_NoTransaction(t *testing.T) {
	if s := DetectGo("func add(a, b int) int { return a + b }"); s.Transactional {
		t.Fatalf("no tx expected: %+v", s)
	}
}

func TestStamp_Apply(t *testing.T) {
	s := Stamp{Transactional: true, Source: "go_sql_begin_tx", Isolation: "Serializable"}
	props := s.Apply(nil)
	if props["transactional"] != "true" {
		t.Fatalf("transactional property not set")
	}
	if props["tx_source"] != "go_sql_begin_tx" || props["tx_isolation"] != "Serializable" {
		t.Fatalf("tx props not set correctly: %v", props)
	}
	if _, ok := props["tx_propagation"]; ok {
		t.Errorf("tx_propagation should be absent when empty")
	}
}

func TestStamp_ApplyNoop(t *testing.T) {
	// No transaction → property absent (nil map stays nil).
	if got := (Stamp{}).Apply(nil); got != nil {
		t.Fatalf("non-transactional Apply should leave map nil, got %v", got)
	}
}
