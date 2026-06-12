package scala_test

import "testing"

// Anorm extractor (#4915) — SQL("…") / SQL"…" statements, RowParser type
// mappings, and case-class row models in Anorm-flavoured Play files.

func TestAnormSQLCallStatement(t *testing.T) {
	src := `
import anorm._
import anorm.SqlParser._

def findById(id: Long): Option[User] =
  DB.withConnection { implicit c =>
    SQL("SELECT id, name FROM users WHERE id = {id}")
      .on("id" -> id)
      .as(User.parser.singleOpt)
  }
`
	ents := extract(t, "custom_scala_anorm", fi("UserRepo.scala", "scala", src))
	q := findEntity(ents, "SCOPE.Operation", "")
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" {
			found = true
			if e.Props["sql_verb"] != "select" {
				t.Errorf("sql_verb = %q, want select", e.Props["sql_verb"])
			}
			if e.Props["table_name"] != "users" {
				t.Errorf("table_name = %q, want users", e.Props["table_name"])
			}
			if e.Props["framework"] != "anorm" {
				t.Errorf("framework = %q, want anorm", e.Props["framework"])
			}
		}
	}
	if !found {
		t.Fatalf("expected SCOPE.Operation/query from SQL(...) call; got %v", q)
	}
}

func TestAnormSQLInterpolated(t *testing.T) {
	src := `
import anorm._

val q = SQL"""INSERT INTO orders (user_id, total) VALUES (${userId}, ${total})"""
`
	ents := extract(t, "custom_scala_anorm", fi("OrderRepo.scala", "scala", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query" && e.Props["sql_verb"] == "insert" {
			found = true
			if e.Props["table_name"] != "orders" {
				t.Errorf("table_name = %q, want orders", e.Props["table_name"])
			}
		}
	}
	if !found {
		t.Fatal("expected an insert SCOPE.Operation/query from interpolated SQL")
	}
}

func TestAnormMacroRowParser(t *testing.T) {
	src := `
import anorm._

case class User(id: Long, name: String)

object User {
  val parser = Macro.namedParser[User]
}
`
	ents := extract(t, "custom_scala_anorm", fi("User.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Schema", "row_parser:User") {
		t.Error("expected row_parser:User SCOPE.Schema from Macro.namedParser[User]")
	}
	if !containsEntity(ents, "SCOPE.Schema", "User") {
		t.Error("expected User case-class row model SCOPE.Schema")
	}
}

func TestAnormNoMatch(t *testing.T) {
	// No `anorm` token -> gate closes, nothing emitted (does not poach
	// doobie's lower-case sql"…" interpolator).
	src := `
import doobie._
val q = sql"SELECT 1".query[Int]
`
	ents := extract(t, "custom_scala_anorm", fi("Doobie.scala", "scala", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-anorm file, got %d", len(ents))
	}
}
