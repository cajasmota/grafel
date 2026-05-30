package scala_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// ---------------------------------------------------------------------------
// Type System extractor tests — value-asserting (#3451).
//
// These assert *specific* extracted detail (case-class field names, enum-case
// names, trait-method names, type-alias targets) rather than mere existence,
// which is the bar required to flip the Type System capabilities to `full`.
// ---------------------------------------------------------------------------

// scalaExtractFull runs the type_system extractor and returns full records so
// tests can assert Properties, not just names.
func scalaExtractFull(t *testing.T, src string, path string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_scala_type_system")
	if !ok {
		t.Fatalf("extractor not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: path, Language: "scala", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

// scalaFind returns the first record matching kind+name, or fails.
func scalaFind(t *testing.T, ents []types.EntityRecord, kind, name string) types.EntityRecord {
	t.Helper()
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return e
		}
	}
	t.Fatalf("no %s entity named %q (have %d entities)", kind, name, len(ents))
	return types.EntityRecord{}
}

// scalaProp fetches a property value or fails.
func scalaProp(t *testing.T, e types.EntityRecord, key string) string {
	t.Helper()
	v, ok := e.Properties[key]
	if !ok {
		t.Fatalf("entity %q missing property %q (props: %v)", e.Name, key, e.Properties)
	}
	return v
}

// ---- type_extraction: case class fields + generics --------------------------

func TestTypeSystemCaseClassFields(t *testing.T) {
	src := `
case class User(id: Long, name: String, email: String)
final case class Address(street: String, city: String, zip: String = "00000")
case class Box[T](value: T, label: String)
`
	ents := scalaExtractFull(t, src, "Models.scala")

	user := scalaFind(t, ents, "SCOPE.Type", "User")
	if got := scalaProp(t, user, "fields"); got != "id,name,email" {
		t.Errorf("User fields = %q, want id,name,email", got)
	}
	if got := scalaProp(t, user, "type_kind"); got != "case_class" {
		t.Errorf("User type_kind = %q, want case_class", got)
	}

	addr := scalaFind(t, ents, "SCOPE.Type", "Address")
	if got := scalaProp(t, addr, "fields"); got != "street,city,zip" {
		t.Errorf("Address fields = %q, want street,city,zip (default value should be stripped)", got)
	}

	box := scalaFind(t, ents, "SCOPE.Type", "Box")
	if got := scalaProp(t, box, "fields"); got != "value,label" {
		t.Errorf("Box fields = %q, want value,label", got)
	}
	if got := scalaProp(t, box, "type_params"); got != "T" {
		t.Errorf("Box type_params = %q, want T", got)
	}
}

func TestTypeSystemPlainClassCtorFields(t *testing.T) {
	src := `
class Repository(val db: Database, val cache: Cache) {
  def find(id: Long): Option[User] = ???
}
`
	ents := scalaExtractFull(t, src, "Repo.scala")
	repo := scalaFind(t, ents, "SCOPE.Type", "Repository")
	if got := scalaProp(t, repo, "fields"); got != "db,cache" {
		t.Errorf("Repository fields = %q, want db,cache", got)
	}
}

// ---- enum_extraction: Scala 3 enum cases + Scala 2 ADT members --------------

func TestTypeSystemScala3EnumCases(t *testing.T) {
	src := `
enum Color {
  case Red, Green, Blue
}
enum Shape[A] extends SomeBase {
  case Circle(r: Double)
  case Rect(w: Double, h: Double)
  case Point
}
`
	ents := scalaExtractFull(t, src, "Enums.scala")

	color := scalaFind(t, ents, "SCOPE.Type", "Color")
	if got := scalaProp(t, color, "enum_cases"); got != "Red,Green,Blue" {
		t.Errorf("Color enum_cases = %q, want Red,Green,Blue", got)
	}
	if got := scalaProp(t, color, "type_kind"); got != "enum" {
		t.Errorf("Color type_kind = %q, want enum", got)
	}

	shape := scalaFind(t, ents, "SCOPE.Type", "Shape")
	cases := scalaProp(t, shape, "enum_cases")
	for _, want := range []string{"Circle", "Rect", "Point"} {
		if !strings.Contains(","+cases+",", ","+want+",") {
			t.Errorf("Shape enum_cases = %q, missing parameterized case %q", cases, want)
		}
	}
	if got := scalaProp(t, shape, "type_params"); got != "A" {
		t.Errorf("Shape type_params = %q, want A", got)
	}
	if got := scalaProp(t, shape, "extends"); !strings.Contains(got, "SomeBase") {
		t.Errorf("Shape extends = %q, want SomeBase", got)
	}
}

func TestTypeSystemScala2ADTMembers(t *testing.T) {
	src := `
sealed trait PaymentMethod
case class CreditCard(number: String) extends PaymentMethod
case object Cash extends PaymentMethod
case object Cheque extends PaymentMethod
`
	ents := scalaExtractFull(t, src, "Payment.scala")
	pm := scalaFind(t, ents, "SCOPE.Type", "PaymentMethod")
	if got := scalaProp(t, pm, "type_kind"); got != "sealed_trait" {
		t.Errorf("PaymentMethod type_kind = %q, want sealed_trait", got)
	}
	cases := scalaProp(t, pm, "enum_cases")
	for _, want := range []string{"CreditCard", "Cash", "Cheque"} {
		if !strings.Contains(","+cases+",", ","+want+",") {
			t.Errorf("PaymentMethod enum_cases = %q, missing member %q", cases, want)
		}
	}
	if got := scalaProp(t, pm, "is_adt"); got != "true" {
		t.Errorf("PaymentMethod is_adt = %q, want true", got)
	}
}

func TestTypeSystemSealedAbstractClassMembers(t *testing.T) {
	src := `
sealed abstract class ApiError(val code: Int)
case object NotFound extends ApiError(404)
case object ServerError extends ApiError(500)
`
	ents := scalaExtractFull(t, src, "Errors.scala")
	err := scalaFind(t, ents, "SCOPE.Type", "ApiError")
	cases := scalaProp(t, err, "enum_cases")
	for _, want := range []string{"NotFound", "ServerError"} {
		if !strings.Contains(","+cases+",", ","+want+",") {
			t.Errorf("ApiError enum_cases = %q, missing %q", cases, want)
		}
	}
}

// ---- interface_extraction: trait methods, supertraits, self-types -----------

func TestTypeSystemTraitMethods(t *testing.T) {
	src := `
trait UserRepository[F[_]] extends BaseRepository {
  def findById(id: Long): F[Option[User]]
  def save(user: User): F[User]
  def deleteById(id: Long): F[Unit]
}
`
	ents := scalaExtractFull(t, src, "Repos.scala")
	repo := scalaFind(t, ents, "SCOPE.Interface", "UserRepository")
	methods := scalaProp(t, repo, "methods")
	for _, want := range []string{"findById", "save", "deleteById"} {
		if !strings.Contains(","+methods+",", ","+want+",") {
			t.Errorf("UserRepository methods = %q, missing %q", methods, want)
		}
	}
	if got := scalaProp(t, repo, "type_params"); got != "F[_]" {
		t.Errorf("UserRepository type_params = %q, want F[_]", got)
	}
	if got := scalaProp(t, repo, "extends"); !strings.Contains(got, "BaseRepository") {
		t.Errorf("UserRepository extends = %q, want BaseRepository", got)
	}
}

func TestTypeSystemTraitSelfType(t *testing.T) {
	src := `
trait UserService {
  self: UserRepository =>
  def register(name: String): User
}
`
	ents := scalaExtractFull(t, src, "Service.scala")
	svc := scalaFind(t, ents, "SCOPE.Interface", "UserService")
	if got := scalaProp(t, svc, "self_type"); !strings.Contains(got, "UserRepository") {
		t.Errorf("UserService self_type = %q, want UserRepository", got)
	}
	if got := scalaProp(t, svc, "methods"); !strings.Contains(got, "register") {
		t.Errorf("UserService methods = %q, want register", got)
	}
}

func TestTypeSystemAbstractClassMethods(t *testing.T) {
	src := `
abstract class BaseService(val db: Database) extends Logging {
  def execute(): Unit
  def name: String
}
`
	ents := scalaExtractFull(t, src, "Base.scala")
	svc := scalaFind(t, ents, "SCOPE.Interface", "BaseService")
	methods := scalaProp(t, svc, "methods")
	for _, want := range []string{"execute", "name"} {
		if !strings.Contains(","+methods+",", ","+want+",") {
			t.Errorf("BaseService methods = %q, missing %q", methods, want)
		}
	}
	if got := scalaProp(t, svc, "extends"); !strings.Contains(got, "Logging") {
		t.Errorf("BaseService extends = %q, want Logging", got)
	}
}

// ---- type_alias_extraction: alias targets, generics, opaque -----------------

func TestTypeSystemTypeAliasTargets(t *testing.T) {
	src := `
type UserId = Long
type UserMap = Map[String, User]
type Pair[T] = (T, T)
opaque type Email = String
`
	ents := scalaExtractFull(t, src, "Types.scala")

	uid := scalaFind(t, ents, "SCOPE.Type", "UserId")
	if got := scalaProp(t, uid, "aliased_type"); got != "Long" {
		t.Errorf("UserId aliased_type = %q, want Long", got)
	}

	umap := scalaFind(t, ents, "SCOPE.Type", "UserMap")
	if got := scalaProp(t, umap, "aliased_type"); got != "Map[String, User]" {
		t.Errorf("UserMap aliased_type = %q, want Map[String, User]", got)
	}

	pair := scalaFind(t, ents, "SCOPE.Type", "Pair")
	if got := scalaProp(t, pair, "type_params"); got != "T" {
		t.Errorf("Pair type_params = %q, want T", got)
	}
	if got := scalaProp(t, pair, "aliased_type"); got != "(T, T)" {
		t.Errorf("Pair aliased_type = %q, want (T, T)", got)
	}

	email := scalaFind(t, ents, "SCOPE.Type", "Email")
	if got := scalaProp(t, email, "type_kind"); got != "opaque_type" {
		t.Errorf("Email type_kind = %q, want opaque_type", got)
	}
	if got := scalaProp(t, email, "aliased_type"); got != "String" {
		t.Errorf("Email aliased_type = %q, want String", got)
	}
}

// ---- guard: non-scala files produce nothing ---------------------------------

func TestTypeSystemNoMatchNonScala(t *testing.T) {
	ents := scalaExtractFull(t, `case class Foo(x: Int)`, "Foo.java")
	// language is forced to "scala" by helper; assert the real guard separately.
	e, _ := extreg.Get("custom_scala_type_system")
	out, _ := e.Extract(context.Background(), extreg.FileInput{
		Path: "Foo.java", Language: "java", Content: []byte(`case class Foo(x: Int)`),
	})
	if len(out) != 0 {
		t.Errorf("expected no entities for java file, got %d", len(out))
	}
	_ = ents
}
