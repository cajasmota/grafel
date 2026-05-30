package scala_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Type System extractor tests
// ---------------------------------------------------------------------------

func TestTypeSystemCaseClass(t *testing.T) {
	src := `
case class User(id: Long, name: String, email: String)
case class OrderRequest(userId: Long, items: List[String])
final case class Address(street: String, city: String)
`
	ents := extract(t, "custom_scala_type_system", fi("Models.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Type", "User") {
		t.Error("expected User case class")
	}
	if !containsEntity(ents, "SCOPE.Type", "OrderRequest") {
		t.Error("expected OrderRequest case class")
	}
	if !containsEntity(ents, "SCOPE.Type", "Address") {
		t.Error("expected Address final case class")
	}
}

func TestTypeSystemSealedTrait(t *testing.T) {
	src := `
sealed trait PaymentMethod
sealed abstract class ApiError
case class CreditCard(number: String) extends PaymentMethod
case object Cash extends PaymentMethod
`
	ents := extract(t, "custom_scala_type_system", fi("Domain.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Type", "PaymentMethod") {
		t.Error("expected PaymentMethod sealed_trait as ADT enum")
	}
	if !containsEntity(ents, "SCOPE.Type", "ApiError") {
		t.Error("expected ApiError sealed_abstract_class")
	}
}

func TestTypeSystemScala3Enum(t *testing.T) {
	src := `
enum Color {
  case Red, Green, Blue
}
enum Status extends SomeBase {
  case Active, Inactive, Pending
}
`
	ents := extract(t, "custom_scala_type_system", fi("Enums.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Type", "Color") {
		t.Error("expected Color Scala3 enum")
	}
	if !containsEntity(ents, "SCOPE.Type", "Status") {
		t.Error("expected Status Scala3 enum")
	}
}

func TestTypeSystemTrait(t *testing.T) {
	src := `
trait UserRepository {
  def findById(id: Long): Option[User]
  def save(user: User): User
}
trait Logging {
  def log(msg: String): Unit
}
`
	ents := extract(t, "custom_scala_type_system", fi("Repos.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Interface", "UserRepository") {
		t.Error("expected UserRepository trait")
	}
	if !containsEntity(ents, "SCOPE.Interface", "Logging") {
		t.Error("expected Logging trait")
	}
}

func TestTypeSystemAbstractClass(t *testing.T) {
	src := `
abstract class BaseService(val db: Database) {
  def execute(): Unit
}
`
	ents := extract(t, "custom_scala_type_system", fi("Base.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Interface", "BaseService") {
		t.Error("expected BaseService abstract class as interface")
	}
}

func TestTypeSystemTypeAlias(t *testing.T) {
	src := `
type UserId = Long
type UserMap = Map[String, User]
opaque type Token = String
`
	ents := extract(t, "custom_scala_type_system", fi("Types.scala", "scala", src))
	if !containsEntity(ents, "SCOPE.Type", "UserId") {
		t.Error("expected UserId type alias")
	}
	if !containsEntity(ents, "SCOPE.Type", "UserMap") {
		t.Error("expected UserMap type alias")
	}
	if !containsEntity(ents, "SCOPE.Type", "Token") {
		t.Error("expected Token opaque type alias")
	}
}

func TestTypeSystemNoMatchNonScala(t *testing.T) {
	src := `case class Foo(x: Int)`
	ents := extract(t, "custom_scala_type_system", fi("Foo.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for java file, got %d", len(ents))
	}
}
