package substrate

import "testing"

// #4736 — Slick read-sink: ambiguous combinators (filter/map/sortBy/take/drop/
// groupBy/join) are credited db_read ONLY on a TableQuery/Query-typed receiver,
// never on a plain in-memory collection (the false-positive #4736 calls out).

// (A) TableQuery-typed receiver → ambiguous combinator credits db_read.
func TestScalaSlickTypedRead_4736(t *testing.T) {
	src := `object UserRepo {
  val users = TableQuery[Users]

  def active() = {
    users.filter(_.active).result
  }

  def names() = {
    val q = users.map(_.name)
    q.result
  }

  def inline() = {
    TableQuery[Users].filter(_.id === 1).result
  }
}`
	by := groupByEffect(sniffEffectsScala(src))
	// `users.filter` (typed local seeded from TableQuery), `.result` distinctive.
	mustHave(t, by, EffectDBRead, "active")
	// `users.map` typed-local credit plus `q.result`.
	mustHave(t, by, EffectDBRead, "names")
	// Inline `TableQuery[Users].filter(...)` literal-root credit + `.result`.
	mustHave(t, by, EffectDBRead, "inline")
}

// (B negative) Plain in-memory collection combinators stay PURE — no db_read.
func TestScalaCollectionNoFalsePositive_4736(t *testing.T) {
	src := `object Calc {
  def pick() = {
    List(1, 2, 3).filter(_ > 1).map(_ * 2)
  }

  def transform() = {
    val xs = Seq("a", "b", "c")
    xs.map(_.toUpperCase).take(2)
  }
}`
	by := groupByEffect(sniffEffectsScala(src))
	mustNotHave(t, by, EffectDBRead, "pick")
	mustNotHave(t, by, EffectDBRead, "transform")
}

// (C) A query-typed local propagated across reassignment stays typed; a read
// chain on it credits db_read.
func TestScalaSlickReadChain_4736(t *testing.T) {
	src := `object OrderRepo {
  def listRecent() = {
    val base = TableQuery[Orders]
    val recent = base.filter(_.recent)
    val sorted = recent.sortBy(_.createdAt)
    sorted.take(10).result
  }
}`
	by := groupByEffect(sniffEffectsScala(src))
	// recent.sortBy + sorted.take are credited via the propagated query typing;
	// `.result` is distinctive too.
	mustHave(t, by, EffectDBRead, "listRecent")
}
