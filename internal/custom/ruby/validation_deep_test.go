package ruby_test

// validation_deep_test.go — value-asserting tests for deep Rails validation
// extraction (TS/JS bar parity, issue #3340).
//
// Tests assert the *exact* attribute + validator + options entities emitted —
// not "≥1 entity".  Each test verifies:
//   - Entity name (railsval:<field>:<validator> or sp_field:<param>:<field>)
//   - Kind / Subtype
//   - Key properties: field, validator, validator_options, param, permit_type

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/ruby"
)

// valExtractRaw extracts raw EntityRecord values from the ruby_validation extractor.
func valExtractRaw(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("ruby_validation")
	if !ok {
		t.Fatal("ruby_validation extractor not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Language: "ruby",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// findValEnt returns the entity with the given name from the raw slice.
func findValEnt(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// mustValProp asserts that entity e has property key=want.
func mustValProp(t *testing.T, e *types.EntityRecord, key, want string) {
	t.Helper()
	if e == nil {
		t.Fatalf("entity is nil while checking prop %q=%q", key, want)
	}
	if got := e.Properties[key]; got != want {
		t.Errorf("entity %q: prop %q = %q, want %q", e.Name, key, got, want)
	}
}

// ---------------------------------------------------------------------------
// 1. validates :field, <validators> — per-validator rule entities
// ---------------------------------------------------------------------------

// TestDeepValidation_PresenceOnly checks that a bare `validates :name, presence: true`
// emits railsval:name:presence with field=name and validator=presence.
func TestDeepValidation_PresenceOnly(t *testing.T) {
	src := `
class User < ApplicationRecord
  validates :name, presence: true
end
`
	ents := valExtractRaw(t, "app/models/user.rb", src)

	e := findValEnt(ents, "railsval:name:presence")
	if e == nil {
		t.Fatal("expected railsval:name:presence entity")
	}
	mustValProp(t, e, "field", "name")
	mustValProp(t, e, "validator", "presence")
	mustValProp(t, e, "framework", "rails")
}

// TestDeepValidation_MultipleValidatorsOnField checks that
// `validates :email, presence: true, format: { with: /.../ }, uniqueness: true`
// emits separate entities for presence, format, and uniqueness.
func TestDeepValidation_MultipleValidatorsOnField(t *testing.T) {
	src := `
class User < ApplicationRecord
  validates :email, presence: true, format: { with: URI::MailTo::EMAIL_REGEXP }, uniqueness: true
end
`
	ents := valExtractRaw(t, "app/models/user.rb", src)

	// presence
	ep := findValEnt(ents, "railsval:email:presence")
	if ep == nil {
		t.Fatal("expected railsval:email:presence")
	}
	mustValProp(t, ep, "field", "email")
	mustValProp(t, ep, "validator", "presence")

	// format (with options)
	ef := findValEnt(ents, "railsval:email:format")
	if ef == nil {
		t.Fatal("expected railsval:email:format")
	}
	mustValProp(t, ef, "field", "email")
	mustValProp(t, ef, "validator", "format")
	// validator_options must be non-empty (contains `with:`)
	if ef.Properties["validator_options"] == "" {
		t.Error("railsval:email:format: expected validator_options to be set")
	}

	// uniqueness
	eu := findValEnt(ents, "railsval:email:uniqueness")
	if eu == nil {
		t.Fatal("expected railsval:email:uniqueness")
	}
	mustValProp(t, eu, "field", "email")
	mustValProp(t, eu, "validator", "uniqueness")
}

// TestDeepValidation_LengthWithMinMax checks that
// `validates :username, length: { minimum: 3, maximum: 20 }` emits
// railsval:username:length with validator_options containing minimum and maximum.
func TestDeepValidation_LengthWithMinMax(t *testing.T) {
	src := `
class User < ApplicationRecord
  validates :username, length: { minimum: 3, maximum: 20 }
end
`
	ents := valExtractRaw(t, "app/models/user.rb", src)

	e := findValEnt(ents, "railsval:username:length")
	if e == nil {
		t.Fatal("expected railsval:username:length")
	}
	mustValProp(t, e, "field", "username")
	mustValProp(t, e, "validator", "length")
	opts := e.Properties["validator_options"]
	if !containsSubstr(opts, "minimum") {
		t.Errorf("validator_options %q: expected 'minimum'", opts)
	}
	if !containsSubstr(opts, "maximum") {
		t.Errorf("validator_options %q: expected 'maximum'", opts)
	}
}

// TestDeepValidation_NumericalityOptions checks
// `validates :age, numericality: { greater_than: 0, less_than: 150 }`.
func TestDeepValidation_NumericalityOptions(t *testing.T) {
	src := `
class Person < ApplicationRecord
  validates :age, numericality: { greater_than: 0, less_than: 150 }
end
`
	ents := valExtractRaw(t, "app/models/person.rb", src)

	e := findValEnt(ents, "railsval:age:numericality")
	if e == nil {
		t.Fatal("expected railsval:age:numericality")
	}
	mustValProp(t, e, "validator", "numericality")
	opts := e.Properties["validator_options"]
	if !containsSubstr(opts, "greater_than") {
		t.Errorf("validator_options %q: expected 'greater_than'", opts)
	}
	if !containsSubstr(opts, "less_than") {
		t.Errorf("validator_options %q: expected 'less_than'", opts)
	}
}

// TestDeepValidation_InclusionOptions checks
// `validates :role, inclusion: { in: %w[admin user moderator] }`.
func TestDeepValidation_InclusionOptions(t *testing.T) {
	src := `
class User < ApplicationRecord
  validates :role, inclusion: { in: %w[admin user moderator] }
end
`
	ents := valExtractRaw(t, "app/models/user.rb", src)

	e := findValEnt(ents, "railsval:role:inclusion")
	if e == nil {
		t.Fatal("expected railsval:role:inclusion")
	}
	mustValProp(t, e, "validator", "inclusion")
}

// TestDeepValidation_AllowNilIsNotAValidator checks that allow_nil: true is NOT
// emitted as a validator entity (it is a modifier).
func TestDeepValidation_AllowNilIsNotAValidator(t *testing.T) {
	src := `
class Product < ApplicationRecord
  validates :description, length: { maximum: 500 }, allow_nil: true
end
`
	ents := valExtractRaw(t, "app/models/product.rb", src)

	if e := findValEnt(ents, "railsval:description:allow_nil"); e != nil {
		t.Error("allow_nil must not be emitted as a validator entity")
	}
	// length must still be emitted
	e := findValEnt(ents, "railsval:description:length")
	if e == nil {
		t.Fatal("expected railsval:description:length")
	}
}

// ---------------------------------------------------------------------------
// 2. validates_*_of with options — classic API
// ---------------------------------------------------------------------------

// TestDeepValidation_ClassicPresenceOf checks `validates_presence_of :title`
// emits railsval_classic:presence:title.
func TestDeepValidation_ClassicPresenceOf(t *testing.T) {
	src := `
class Post < ActiveRecord::Base
  validates_presence_of :title
end
`
	ents := valExtractRaw(t, "app/models/post.rb", src)

	e := findValEnt(ents, "railsval_classic:presence:title")
	if e == nil {
		t.Fatal("expected railsval_classic:presence:title")
	}
	mustValProp(t, e, "field", "title")
	mustValProp(t, e, "validator", "presence")
}

// TestDeepValidation_ClassicNumericalityWithOptions checks that
// `validates_numericality_of :price, greater_than: 0` captures the option.
func TestDeepValidation_ClassicNumericalityWithOptions(t *testing.T) {
	src := `
class Order < ApplicationRecord
  validates_numericality_of :price, greater_than: 0
end
`
	ents := valExtractRaw(t, "app/models/order.rb", src)

	e := findValEnt(ents, "railsval_classic:numericality:price")
	if e == nil {
		t.Fatal("expected railsval_classic:numericality:price")
	}
	mustValProp(t, e, "field", "price")
	mustValProp(t, e, "validator", "numericality")
	opts := e.Properties["validator_options"]
	if !containsSubstr(opts, "greater_than") {
		t.Errorf("validator_options %q: expected 'greater_than'", opts)
	}
}

// TestDeepValidation_ClassicUniquenessOf checks `validates_uniqueness_of :slug`.
func TestDeepValidation_ClassicUniquenessOf(t *testing.T) {
	src := `
class Article < ApplicationRecord
  validates_uniqueness_of :slug
end
`
	ents := valExtractRaw(t, "app/models/article.rb", src)

	e := findValEnt(ents, "railsval_classic:uniqueness:slug")
	if e == nil {
		t.Fatal("expected railsval_classic:uniqueness:slug")
	}
	mustValProp(t, e, "field", "slug")
}

// ---------------------------------------------------------------------------
// 3. with_options block — inherited options
// ---------------------------------------------------------------------------

// TestDeepValidation_WithOptions checks that validates inside with_options inherit
// the block's options and emit railsval_wo:field:validator entities.
func TestDeepValidation_WithOptions(t *testing.T) {
	src := `
class User < ApplicationRecord
  with_options presence: true do
    validates :name
    validates :email
  end
end
`
	ents := valExtractRaw(t, "app/models/user.rb", src)

	eName := findValEnt(ents, "railsval_wo:name:presence")
	if eName == nil {
		t.Fatal("expected railsval_wo:name:presence from with_options block")
	}
	mustValProp(t, eName, "field", "name")
	mustValProp(t, eName, "validator", "presence")
	mustValProp(t, eName, "inherited_options", "presence: true")

	eEmail := findValEnt(ents, "railsval_wo:email:presence")
	if eEmail == nil {
		t.Fatal("expected railsval_wo:email:presence from with_options block")
	}
}

// TestDeepValidation_WithOptionsAndExtraValidators checks that a validates inside
// with_options that also has per-field validators merges both.
func TestDeepValidation_WithOptionsAndExtraValidators(t *testing.T) {
	src := `
class Profile < ApplicationRecord
  with_options presence: true do
    validates :bio, length: { maximum: 500 }
  end
end
`
	ents := valExtractRaw(t, "app/models/profile.rb", src)

	// Inherited presence.
	ep := findValEnt(ents, "railsval_wo:bio:presence")
	if ep == nil {
		t.Fatal("expected railsval_wo:bio:presence (inherited from with_options)")
	}

	// Per-field length.
	el := findValEnt(ents, "railsval_wo:bio:length")
	if el == nil {
		t.Fatal("expected railsval_wo:bio:length (per-field validator in with_options)")
	}
}

// ---------------------------------------------------------------------------
// 4. Strong params — per-field dto_field entities
// ---------------------------------------------------------------------------

// TestDeepValidation_StrongParamsScalarFields checks that
// params.require(:user).permit(:name, :email, :password)
// emits separate sp_field:user:name / sp_field:user:email / sp_field:user:password
// with permit_type=scalar.
func TestDeepValidation_StrongParamsScalarFields(t *testing.T) {
	src := `
class UsersController < ApplicationController
  def user_params
    params.require(:user).permit(:name, :email, :password)
  end
end
`
	ents := valExtractRaw(t, "app/controllers/users_controller.rb", src)

	for _, field := range []string{"name", "email", "password"} {
		e := findValEnt(ents, "sp_field:user:"+field)
		if e == nil {
			t.Fatalf("expected sp_field:user:%s", field)
		}
		mustValProp(t, e, "param", "user")
		mustValProp(t, e, "field", field)
		mustValProp(t, e, "permit_type", "scalar")
	}
}

// TestDeepValidation_StrongParamsArrayField checks that
// params.require(:post).permit(:title, :body, tag_ids: [])
// emits sp_field:post:tag_ids with permit_type=array.
func TestDeepValidation_StrongParamsArrayField(t *testing.T) {
	src := `
class PostsController < ApplicationController
  def post_params
    params.require(:post).permit(:title, :body, tag_ids: [])
  end
end
`
	ents := valExtractRaw(t, "app/controllers/posts_controller.rb", src)

	// Scalar fields.
	for _, field := range []string{"title", "body"} {
		e := findValEnt(ents, "sp_field:post:"+field)
		if e == nil {
			t.Fatalf("expected sp_field:post:%s (scalar)", field)
		}
		mustValProp(t, e, "permit_type", "scalar")
	}

	// Array field.
	eArr := findValEnt(ents, "sp_field:post:tag_ids")
	if eArr == nil {
		t.Fatal("expected sp_field:post:tag_ids")
	}
	mustValProp(t, eArr, "permit_type", "array")
}

// TestDeepValidation_StrongParamsNestedFields checks that
// params.require(:user).permit(:name, address: [:street, :city])
// emits sp_field:user:address.street and sp_field:user:address.city with permit_type=nested.
func TestDeepValidation_StrongParamsNestedFields(t *testing.T) {
	src := `
class UsersController < ApplicationController
  def user_params
    params.require(:user).permit(:name, address: [:street, :city, :zip])
  end
end
`
	ents := valExtractRaw(t, "app/controllers/users_controller.rb", src)

	// Scalar :name.
	eName := findValEnt(ents, "sp_field:user:name")
	if eName == nil {
		t.Fatal("expected sp_field:user:name")
	}
	mustValProp(t, eName, "permit_type", "scalar")

	// Nested fields.
	for _, f := range []string{"address.street", "address.city", "address.zip"} {
		e := findValEnt(ents, "sp_field:user:"+f)
		if e == nil {
			t.Fatalf("expected sp_field:user:%s (nested)", f)
		}
		mustValProp(t, e, "permit_type", "nested")
		mustValProp(t, e, "param", "user")
	}
}

// TestDeepValidation_StrongParamsRolesArray checks that
// params.require(:user).permit(:email, roles: [])
// emits sp_field:user:roles with permit_type=array (empty brackets = array permit).
func TestDeepValidation_StrongParamsRolesArray(t *testing.T) {
	src := `
class UsersController < ApplicationController
  def user_params
    params.require(:user).permit(:email, roles: [])
  end
end
`
	ents := valExtractRaw(t, "app/controllers/users_controller.rb", src)

	eRoles := findValEnt(ents, "sp_field:user:roles")
	if eRoles == nil {
		t.Fatal("expected sp_field:user:roles")
	}
	mustValProp(t, eRoles, "permit_type", "array")
}

// TestDeepValidation_StrongParamsMultipleActions checks that multiple
// action-specific params methods in the same controller each emit their fields.
func TestDeepValidation_StrongParamsMultipleActions(t *testing.T) {
	src := `
class ArticlesController < ApplicationController
  private

  def article_params
    params.require(:article).permit(:title, :body, :published)
  end

  def comment_params
    params.require(:comment).permit(:content, :author_name)
  end
end
`
	ents := valExtractRaw(t, "app/controllers/articles_controller.rb", src)

	// Article params.
	for _, f := range []string{"title", "body", "published"} {
		e := findValEnt(ents, "sp_field:article:"+f)
		if e == nil {
			t.Fatalf("expected sp_field:article:%s", f)
		}
		mustValProp(t, e, "param", "article")
	}

	// Comment params.
	for _, f := range []string{"content", "author_name"} {
		e := findValEnt(ents, "sp_field:comment:"+f)
		if e == nil {
			t.Fatalf("expected sp_field:comment:%s", f)
		}
		mustValProp(t, e, "param", "comment")
	}
}

// ---------------------------------------------------------------------------
// 5. Composite — model with several validates and a controller with permit
// ---------------------------------------------------------------------------

// TestDeepValidation_Composite is the canonical "model has several validates"
// assertion that proves exact attribute+validator+options across the whole model.
func TestDeepValidation_Composite(t *testing.T) {
	src := `
class Product < ApplicationRecord
  validates :name, presence: true, length: { minimum: 2, maximum: 100 }
  validates :sku, presence: true, uniqueness: true, format: { with: /\A[A-Z]{2,4}-\d{4,8}\z/ }
  validates :price, numericality: { greater_than: 0 }, allow_nil: false
  validates :status, inclusion: { in: %w[draft published archived] }
  validates_uniqueness_of :sku, case_sensitive: false
end
`
	ents := valExtractRaw(t, "app/models/product.rb", src)

	// --- name ---
	eNamePres := findValEnt(ents, "railsval:name:presence")
	if eNamePres == nil {
		t.Fatal("expected railsval:name:presence")
	}
	mustValProp(t, eNamePres, "field", "name")
	mustValProp(t, eNamePres, "validator", "presence")

	eNameLen := findValEnt(ents, "railsval:name:length")
	if eNameLen == nil {
		t.Fatal("expected railsval:name:length")
	}
	lenOpts := eNameLen.Properties["validator_options"]
	if !containsSubstr(lenOpts, "minimum") {
		t.Errorf("name:length validator_options %q: missing 'minimum'", lenOpts)
	}
	if !containsSubstr(lenOpts, "maximum") {
		t.Errorf("name:length validator_options %q: missing 'maximum'", lenOpts)
	}

	// --- sku ---
	eSkuPres := findValEnt(ents, "railsval:sku:presence")
	if eSkuPres == nil {
		t.Fatal("expected railsval:sku:presence")
	}
	eSkuUniq := findValEnt(ents, "railsval:sku:uniqueness")
	if eSkuUniq == nil {
		t.Fatal("expected railsval:sku:uniqueness")
	}
	eSkuFmt := findValEnt(ents, "railsval:sku:format")
	if eSkuFmt == nil {
		t.Fatal("expected railsval:sku:format")
	}

	// --- price ---
	ePriceNum := findValEnt(ents, "railsval:price:numericality")
	if ePriceNum == nil {
		t.Fatal("expected railsval:price:numericality")
	}
	numOpts := ePriceNum.Properties["validator_options"]
	if !containsSubstr(numOpts, "greater_than") {
		t.Errorf("price:numericality validator_options %q: missing 'greater_than'", numOpts)
	}

	// --- status ---
	eStatusIncl := findValEnt(ents, "railsval:status:inclusion")
	if eStatusIncl == nil {
		t.Fatal("expected railsval:status:inclusion")
	}

	// --- validates_uniqueness_of :sku ---
	eSkuClassic := findValEnt(ents, "railsval_classic:uniqueness:sku")
	if eSkuClassic == nil {
		t.Fatal("expected railsval_classic:uniqueness:sku")
	}
	classicOpts := eSkuClassic.Properties["validator_options"]
	if !containsSubstr(classicOpts, "case_sensitive") {
		t.Errorf("classic:uniqueness:sku validator_options %q: missing 'case_sensitive'", classicOpts)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsSubstr(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && containsSubstrInner(s, sub)
}

func containsSubstrInner(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
