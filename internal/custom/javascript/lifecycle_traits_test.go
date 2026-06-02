// lifecycle_traits_test.go — value-asserting tests for ORM model
// data-lifecycle traits (#3628 child) stamped onto TypeORM @Entity and
// Sequelize model nodes: soft_delete / soft_delete_column / timestamps /
// audit_columns. Asserts the trait + column on the specific model entity,
// not just len>0.
package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/javascript"
)

func extractRaw(t *testing.T, name string, file extreg.FileInput) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	return ents
}

func findModel(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == "SCOPE.Schema" {
			return &ents[i]
		}
	}
	return nil
}

// --- TypeORM ---------------------------------------------------------------

func TestTypeORMLifecycle_SoftDeleteAndTimestamps(t *testing.T) {
	src := `
@Entity()
export class User {
  @PrimaryGeneratedColumn() id: number;
  @Column() createdBy: string;
  @CreateDateColumn() createdAt: Date;
  @UpdateDateColumn() updatedAt: Date;
  @DeleteDateColumn() deletedAt: Date;
}
`
	ents := extractRaw(t, "custom_js_typeorm", fi("user.ts", "typescript", src))
	user := findModel(ents, "User")
	if user == nil {
		t.Fatal("User @Entity model node not emitted")
	}
	if user.Properties["soft_delete"] != "true" {
		t.Errorf("User.soft_delete: want true, got %q", user.Properties["soft_delete"])
	}
	if user.Properties["soft_delete_column"] != "deletedAt" {
		t.Errorf("User.soft_delete_column: want deletedAt, got %q", user.Properties["soft_delete_column"])
	}
	if user.Properties["timestamps"] != "true" {
		t.Errorf("User.timestamps: want true, got %q", user.Properties["timestamps"])
	}
	if user.Properties["audit_columns"] != "created_by" {
		t.Errorf("User.audit_columns: want created_by, got %q", user.Properties["audit_columns"])
	}
}

func TestTypeORMLifecycle_PlainEntity_NoTraits(t *testing.T) {
	src := `
@Entity()
export class Tag {
  @Column() deleted: boolean;
  @Column() name: string;
}
`
	ents := extractRaw(t, "custom_js_typeorm", fi("tag.ts", "typescript", src))
	tag := findModel(ents, "Tag")
	if tag == nil {
		t.Fatal("Tag @Entity model node not emitted")
	}
	if _, ok := tag.Properties["soft_delete"]; ok {
		t.Error("plain @Column() deleted boolean must NOT stamp soft_delete")
	}
	if _, ok := tag.Properties["timestamps"]; ok {
		t.Error("no date columns → timestamps must be absent")
	}
}

func TestTypeORMLifecycle_PerEntityBodyIsolation(t *testing.T) {
	src := `
@Entity()
export class Soft {
  @DeleteDateColumn() deletedAt: Date;
}
@Entity()
export class Hard {
  @Column() name: string;
}
`
	ents := extractRaw(t, "custom_js_typeorm", fi("two.ts", "typescript", src))
	soft := findModel(ents, "Soft")
	hard := findModel(ents, "Hard")
	if soft == nil || hard == nil {
		t.Fatal("both @Entity nodes must be emitted")
	}
	if soft.Properties["soft_delete"] != "true" {
		t.Error("Soft must be soft_delete")
	}
	if _, ok := hard.Properties["soft_delete"]; ok {
		t.Error("Hard must NOT inherit Soft's soft_delete (body isolation)")
	}
}

// --- Sequelize -------------------------------------------------------------

func TestSequelizeLifecycle_DefineParanoid(t *testing.T) {
	src := `
const User = sequelize.define("User", {
  name: DataTypes.STRING,
  createdBy: DataTypes.STRING,
}, {
  paranoid: true,
});
`
	ents := extractRaw(t, "custom_js_sequelize", fi("user.js", "javascript", src))
	user := findModel(ents, "User")
	if user == nil {
		t.Fatal("User define model node not emitted")
	}
	if user.Properties["soft_delete"] != "true" {
		t.Errorf("User.soft_delete: want true, got %q", user.Properties["soft_delete"])
	}
	if user.Properties["soft_delete_column"] != "deletedAt" {
		t.Errorf("User.soft_delete_column: want deletedAt, got %q", user.Properties["soft_delete_column"])
	}
	if user.Properties["timestamps"] != "true" {
		t.Errorf("User.timestamps: want true (paranoid forces it), got %q", user.Properties["timestamps"])
	}
	if user.Properties["audit_columns"] != "created_by" {
		t.Errorf("User.audit_columns: want created_by, got %q", user.Properties["audit_columns"])
	}
}

func TestSequelizeLifecycle_TimestampsDisabled(t *testing.T) {
	src := `
const Log = sequelize.define("Log", {
  message: DataTypes.STRING,
}, {
  timestamps: false,
});
`
	ents := extractRaw(t, "custom_js_sequelize", fi("log.js", "javascript", src))
	log := findModel(ents, "Log")
	if log == nil {
		t.Fatal("Log define model node not emitted")
	}
	if _, ok := log.Properties["timestamps"]; ok {
		t.Error("timestamps:false must omit timestamps")
	}
	if _, ok := log.Properties["soft_delete"]; ok {
		t.Error("no paranoid → no soft_delete")
	}
}

func TestSequelizeLifecycle_ClassInitParanoid(t *testing.T) {
	src := `
class Account extends Model {}
Account.init({
  email: DataTypes.STRING,
}, {
  sequelize,
  paranoid: true,
});
`
	ents := extractRaw(t, "custom_js_sequelize", fi("account.js", "javascript", src))
	acct := findModel(ents, "Account")
	if acct == nil {
		t.Fatal("Account class model node not emitted")
	}
	if acct.Properties["soft_delete"] != "true" {
		t.Errorf("Account.soft_delete: want true, got %q", acct.Properties["soft_delete"])
	}
}
