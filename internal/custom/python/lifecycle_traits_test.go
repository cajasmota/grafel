// lifecycle_traits_test.go — value-asserting tests for ORM model
// data-lifecycle traits (#3628 child) stamped onto Django and SQLAlchemy model
// nodes: soft_delete / soft_delete_column / timestamps / audit_columns. Asserts
// the trait + column on the specific model entity, not just len>0.
package python_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/python"
)

func pyExtract(t *testing.T, name, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extractor.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), extractor.FileInput{
		Path: "models.py", Language: "python", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

func pyModel(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Schema" {
			return &ents[i]
		}
	}
	return nil
}

// --- Django ----------------------------------------------------------------

func TestDjangoLifecycle_DeletedAtAndTimestamps(t *testing.T) {
	src := `
class Order(models.Model):
    name = models.CharField(max_length=20)
    created_by = models.ForeignKey(User, on_delete=models.CASCADE)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    deleted_at = models.DateTimeField(null=True, blank=True)
`
	ents := pyExtract(t, "python_django", src)
	order := pyModel(ents, "Order")
	if order == nil {
		t.Fatal("Order Django model node not emitted")
	}
	if order.Properties["soft_delete"] != "true" {
		t.Errorf("Order.soft_delete: want true, got %q", order.Properties["soft_delete"])
	}
	if order.Properties["soft_delete_column"] != "deleted_at" {
		t.Errorf("Order.soft_delete_column: want deleted_at, got %q", order.Properties["soft_delete_column"])
	}
	if order.Properties["timestamps"] != "true" {
		t.Errorf("Order.timestamps: want true, got %q", order.Properties["timestamps"])
	}
	if order.Properties["audit_columns"] != "created_by" {
		t.Errorf("Order.audit_columns: want created_by, got %q", order.Properties["audit_columns"])
	}
}

func TestDjangoLifecycle_SafeDeleteBase(t *testing.T) {
	src := `
class Account(SafeDeleteModel):
    name = models.CharField(max_length=10)
`
	ents := pyExtract(t, "python_django", src)
	acct := pyModel(ents, "Account")
	if acct == nil {
		t.Fatal("Account SafeDeleteModel node not emitted")
	}
	if acct.Properties["soft_delete"] != "true" || acct.Properties["soft_delete_column"] != "deleted_at" {
		t.Errorf("Account soft-delete traits: want true/deleted_at, got %q/%q",
			acct.Properties["soft_delete"], acct.Properties["soft_delete_column"])
	}
}

func TestDjangoLifecycle_PlainDeletedBool_NoSoftDelete(t *testing.T) {
	src := `
class Item(models.Model):
    deleted = models.BooleanField(default=False)
    name = models.CharField(max_length=10)
`
	ents := pyExtract(t, "python_django", src)
	item := pyModel(ents, "Item")
	if item == nil {
		t.Fatal("Item model node not emitted")
	}
	if _, ok := item.Properties["soft_delete"]; ok {
		t.Error("plain `deleted` boolean must NOT stamp soft_delete")
	}
	if _, ok := item.Properties["timestamps"]; ok {
		t.Error("no timestamp fields → timestamps absent")
	}
}

// --- SQLAlchemy ------------------------------------------------------------

func TestSQLAlchemyLifecycle_SoftDeleteMixin(t *testing.T) {
	src := `
class User(Base, SoftDeleteMixin):
    __tablename__ = "users"
    id = Column(Integer, primary_key=True)
    created_by = Column(String)
`
	ents := pyExtract(t, "python_sqlalchemy", src)
	user := pyModel(ents, "User")
	if user == nil {
		t.Fatal("User SQLAlchemy model node not emitted")
	}
	if user.Properties["soft_delete"] != "true" || user.Properties["soft_delete_column"] != "deleted_at" {
		t.Errorf("User soft-delete: want true/deleted_at, got %q/%q",
			user.Properties["soft_delete"], user.Properties["soft_delete_column"])
	}
	if user.Properties["audit_columns"] != "created_by" {
		t.Errorf("User.audit_columns: want created_by, got %q", user.Properties["audit_columns"])
	}
}

func TestSQLAlchemyLifecycle_TimestampsAndDeletedAt(t *testing.T) {
	src := `
class Post(Base):
    __tablename__ = "posts"
    id = Column(Integer, primary_key=True)
    created_at = Column(DateTime, server_default=func.now())
    updated_at = Column(DateTime, onupdate=func.now())
    deleted_at = Column(DateTime, nullable=True)
`
	ents := pyExtract(t, "python_sqlalchemy", src)
	post := pyModel(ents, "Post")
	if post == nil {
		t.Fatal("Post SQLAlchemy model node not emitted")
	}
	if post.Properties["soft_delete"] != "true" || post.Properties["soft_delete_column"] != "deleted_at" {
		t.Errorf("Post soft-delete: want true/deleted_at, got %q/%q",
			post.Properties["soft_delete"], post.Properties["soft_delete_column"])
	}
	if post.Properties["timestamps"] != "true" {
		t.Errorf("Post.timestamps: want true, got %q", post.Properties["timestamps"])
	}
}

func TestSQLAlchemyLifecycle_PlainTimestamps_NotAsserted(t *testing.T) {
	src := `
class Plain(Base):
    __tablename__ = "plain"
    id = Column(Integer, primary_key=True)
    created_at = Column(DateTime)
    updated_at = Column(DateTime)
`
	ents := pyExtract(t, "python_sqlalchemy", src)
	plain := pyModel(ents, "Plain")
	if plain == nil {
		t.Fatal("Plain model node not emitted")
	}
	if _, ok := plain.Properties["timestamps"]; ok {
		t.Error("plain DateTime columns without default/onupdate must NOT assert timestamps")
	}
	if _, ok := plain.Properties["soft_delete"]; ok {
		t.Error("no deleted_at/mixin → soft_delete absent")
	}
}
