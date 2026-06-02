package lifecycle

import (
	"sort"
	"testing"
)

func stamped(t Traits) map[string]string {
	out := map[string]string{}
	t.Stamp(func(kv ...string) {
		for i := 0; i+1 < len(kv); i += 2 {
			out[kv[i]] = kv[i+1]
		}
	})
	return out
}

func TestGORM_Embed(t *testing.T) {
	got := GORM(GORMInput{EmbedsGormModel: true})
	if !got.SoftDelete || got.SoftDeleteColumn != "deleted_at" {
		t.Errorf("embed: want soft_delete deleted_at, got %+v", got)
	}
	if !got.Timestamps {
		t.Error("embed: want timestamps")
	}
}

func TestGORM_ExplicitDeletedAt(t *testing.T) {
	got := GORM(GORMInput{DeletedAtColumn: "archived_at", HasCreatedAt: true, HasUpdatedAt: true})
	if !got.SoftDelete || got.SoftDeleteColumn != "archived_at" {
		t.Errorf("want soft_delete archived_at, got %+v", got)
	}
	if !got.Timestamps {
		t.Error("want timestamps from CreatedAt+UpdatedAt")
	}
}

func TestGORM_CreatedAtOnly_NoTimestamps(t *testing.T) {
	got := GORM(GORMInput{HasCreatedAt: true})
	if got.Timestamps {
		t.Error("CreatedAt without UpdatedAt must NOT assert timestamps")
	}
	if got.SoftDelete {
		t.Error("no DeletedAt/embed must NOT assert soft_delete")
	}
}

func TestGORM_PlainDeletedBool_NotSoftDelete(t *testing.T) {
	got := GORM(GORMInput{Columns: []string{"id", "deleted", "note"}})
	if got.SoftDelete {
		t.Error("plain deleted column must NOT assert soft_delete")
	}
	if len(got.AuditColumns) != 0 {
		t.Error("no audit columns expected")
	}
}

func TestGORM_AuditColumns(t *testing.T) {
	got := GORM(GORMInput{Columns: []string{"id", "created_by", "updated_by", "name"}})
	want := []string{"created_by", "updated_by"}
	if len(got.AuditColumns) != 2 || got.AuditColumns[0] != want[0] || got.AuditColumns[1] != want[1] {
		t.Errorf("audit: want %v got %v", want, got.AuditColumns)
	}
}

func TestRails_ActsAsParanoid(t *testing.T) {
	got := RailsModelTraits("class U < ApplicationRecord\n acts_as_paranoid\nend", nil)
	if !got.SoftDelete || got.SoftDeleteColumn != "deleted_at" {
		t.Errorf("want soft_delete deleted_at, got %+v", got)
	}
	if got.Timestamps {
		t.Error("Rails timestamps must stay honest-partial (omitted)")
	}
}

func TestRails_DefaultScopeSoftDelete(t *testing.T) {
	got := RailsModelTraits("default_scope { where(deleted_at: nil) }", nil)
	if !got.SoftDelete {
		t.Error("default_scope deleted_at: nil must be soft_delete")
	}
}

func TestRails_OrderingScope_NotSoftDelete(t *testing.T) {
	got := RailsModelTraits("default_scope { order(created_at: :desc) }", nil)
	if got.SoftDelete {
		t.Error("ordering default_scope must NOT be soft_delete")
	}
}

// --- TypeORM ---------------------------------------------------------------

func TestTypeORM_DeleteDateColumn(t *testing.T) {
	body := `@Entity() class User {
	@PrimaryGeneratedColumn() id: number;
	@DeleteDateColumn() deletedAt: Date;
}`
	got := TypeORM(body)
	if !got.SoftDelete || got.SoftDeleteColumn != "deletedAt" {
		t.Errorf("want soft_delete deletedAt, got %+v", got)
	}
	if got.Timestamps {
		t.Error("no create/update date columns → timestamps must be absent")
	}
}

func TestTypeORM_CreateUpdateDateColumns_Timestamps(t *testing.T) {
	body := `@Entity() class Post {
	@CreateDateColumn() createdAt: Date;
	@UpdateDateColumn() updatedAt: Date;
}`
	got := TypeORM(body)
	if !got.Timestamps {
		t.Error("@CreateDateColumn + @UpdateDateColumn → timestamps")
	}
	if got.SoftDelete {
		t.Error("no @DeleteDateColumn → soft_delete must be absent")
	}
}

func TestTypeORM_CreateOnly_NoTimestamps(t *testing.T) {
	body := `@Entity() class Log { @CreateDateColumn() createdAt: Date; }`
	if TypeORM(body).Timestamps {
		t.Error("@CreateDateColumn alone must NOT assert timestamps")
	}
}

func TestTypeORM_AuditColumns(t *testing.T) {
	body := `@Entity() class Doc {
	@Column() createdBy: string;
	@Column() title: string;
}`
	got := TypeORM(body)
	if len(got.AuditColumns) != 1 || got.AuditColumns[0] != "created_by" {
		t.Errorf("want [created_by], got %v", got.AuditColumns)
	}
}

func TestTypeORM_PlainDeletedBool_NotSoftDelete(t *testing.T) {
	body := `@Entity() class Item { @Column() deleted: boolean; }`
	if TypeORM(body).SoftDelete {
		t.Error("plain @Column() deleted boolean must NOT be soft_delete")
	}
}

func TestTypeORM_NoTraits_Absent(t *testing.T) {
	got := TypeORM(`@Entity() class Plain { @Column() name: string; }`)
	if got.SoftDelete || got.Timestamps || len(got.AuditColumns) != 0 {
		t.Errorf("entity with no lifecycle conventions must have no traits, got %+v", got)
	}
}

// --- Sequelize -------------------------------------------------------------

func TestSequelize_Paranoid(t *testing.T) {
	got := Sequelize(SequelizeInput{OptionsBlob: `{ paranoid: true }`})
	if !got.SoftDelete || got.SoftDeleteColumn != "deletedAt" {
		t.Errorf("paranoid:true → soft_delete deletedAt, got %+v", got)
	}
	if !got.Timestamps {
		t.Error("paranoid mode forces timestamps on")
	}
}

func TestSequelize_ParanoidDeletedAtOverride(t *testing.T) {
	got := Sequelize(SequelizeInput{OptionsBlob: `{ paranoid: true, deletedAt: 'destroyed_on' }`})
	if got.SoftDeleteColumn != "destroyed_on" {
		t.Errorf("want override column destroyed_on, got %q", got.SoftDeleteColumn)
	}
}

func TestSequelize_TimestampsDefaultOn(t *testing.T) {
	got := Sequelize(SequelizeInput{OptionsBlob: `{ tableName: 'users' }`})
	if !got.Timestamps {
		t.Error("timestamps default ON unless timestamps:false")
	}
	if got.SoftDelete {
		t.Error("no paranoid → no soft_delete")
	}
}

func TestSequelize_TimestampsDisabled(t *testing.T) {
	got := Sequelize(SequelizeInput{OptionsBlob: `{ timestamps: false }`})
	if got.Timestamps {
		t.Error("timestamps:false must disable timestamps")
	}
}

func TestSequelize_AuditColumns(t *testing.T) {
	got := Sequelize(SequelizeInput{Columns: []string{"id", "createdBy", "name"}, OptionsBlob: `{ timestamps: false }`})
	if len(got.AuditColumns) != 1 || got.AuditColumns[0] != "created_by" {
		t.Errorf("want [created_by], got %v", got.AuditColumns)
	}
}

// --- Django ----------------------------------------------------------------

func TestDjango_DeletedAtField(t *testing.T) {
	body := `class Order(models.Model):
    name = models.CharField(max_length=20)
    deleted_at = models.DateTimeField(null=True)`
	got := Django("models.Model", body)
	if !got.SoftDelete || got.SoftDeleteColumn != "deleted_at" {
		t.Errorf("deleted_at field → soft_delete deleted_at, got %+v", got)
	}
}

func TestDjango_SafeDeleteBase(t *testing.T) {
	got := Django("SafeDeleteModel", `class A(SafeDeleteModel):
    pass`)
	if !got.SoftDelete || got.SoftDeleteColumn != "deleted_at" {
		t.Errorf("SafeDeleteModel base → soft_delete, got %+v", got)
	}
}

func TestDjango_IsDeletedField(t *testing.T) {
	body := `class B(models.Model):
    is_deleted = models.BooleanField(default=False)`
	got := Django("models.Model", body)
	if !got.SoftDelete || got.SoftDeleteColumn != "is_deleted" {
		t.Errorf("is_deleted field → soft_delete is_deleted, got %+v", got)
	}
}

func TestDjango_AutoNowTimestamps(t *testing.T) {
	body := `class C(models.Model):
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)`
	got := Django("models.Model", body)
	if !got.Timestamps {
		t.Error("auto_now_add + auto_now → timestamps")
	}
}

func TestDjango_AuditColumns(t *testing.T) {
	body := `class D(models.Model):
    created_by = models.ForeignKey(User, on_delete=models.CASCADE)
    title = models.CharField(max_length=10)`
	got := Django("models.Model", body)
	if len(got.AuditColumns) != 1 || got.AuditColumns[0] != "created_by" {
		t.Errorf("want [created_by], got %v", got.AuditColumns)
	}
}

func TestDjango_PlainDeletedBool_NotSoftDelete(t *testing.T) {
	body := `class E(models.Model):
    deleted = models.BooleanField(default=False)`
	got := Django("models.Model", body)
	if got.SoftDelete {
		t.Error("plain `deleted` boolean must NOT be soft_delete")
	}
}

func TestDjango_NoTraits_Absent(t *testing.T) {
	got := Django("models.Model", `class F(models.Model):
    name = models.CharField(max_length=5)`)
	if got.SoftDelete || got.Timestamps || len(got.AuditColumns) != 0 {
		t.Errorf("plain model must have no traits, got %+v", got)
	}
}

// --- SQLAlchemy ------------------------------------------------------------

func TestSQLAlchemy_SoftDeleteMixin(t *testing.T) {
	got := SQLAlchemy("Base, SoftDeleteMixin", `class User(Base, SoftDeleteMixin):
    __tablename__ = "users"`)
	if !got.SoftDelete || got.SoftDeleteColumn != "deleted_at" {
		t.Errorf("SoftDeleteMixin → soft_delete deleted_at, got %+v", got)
	}
}

func TestSQLAlchemy_DeletedAtColumn(t *testing.T) {
	body := `class Doc(Base):
    deleted_at = Column(DateTime, nullable=True)`
	got := SQLAlchemy("Base", body)
	if !got.SoftDelete || got.SoftDeleteColumn != "deleted_at" {
		t.Errorf("deleted_at Column → soft_delete, got %+v", got)
	}
}

func TestSQLAlchemy_Timestamps(t *testing.T) {
	body := `class Post(Base):
    created_at = Column(DateTime, server_default=func.now())
    updated_at = Column(DateTime, onupdate=func.now())`
	got := SQLAlchemy("Base", body)
	if !got.Timestamps {
		t.Error("created_at/updated_at with server_default/onupdate → timestamps")
	}
}

func TestSQLAlchemy_PlainTimestampCols_NotAsserted(t *testing.T) {
	body := `class Plain(Base):
    created_at = Column(DateTime)
    updated_at = Column(DateTime)`
	got := SQLAlchemy("Base", body)
	if got.Timestamps {
		t.Error("plain DateTime columns without default/onupdate must NOT assert timestamps")
	}
}

func TestSQLAlchemy_PlainDeletedBool_NotSoftDelete(t *testing.T) {
	body := `class Item(Base):
    deleted = Column(Boolean, default=False)`
	if SQLAlchemy("Base", body).SoftDelete {
		t.Error("plain `deleted` boolean Column must NOT be soft_delete")
	}
}

func TestSQLAlchemy_MappedColumn(t *testing.T) {
	body := `class M(Base):
    deleted_at: Mapped[datetime] = mapped_column(nullable=True)`
	got := SQLAlchemy("Base", body)
	if !got.SoftDelete || got.SoftDeleteColumn != "deleted_at" {
		t.Errorf("mapped_column deleted_at → soft_delete, got %+v", got)
	}
}

func TestStamp_OmitsAbsent(t *testing.T) {
	props := stamped(Traits{})
	if len(props) != 0 {
		t.Errorf("empty traits must stamp nothing, got %v", props)
	}
	props = stamped(Traits{SoftDelete: true, SoftDeleteColumn: "deleted_at", AuditColumns: []string{"created_by"}})
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if props["soft_delete"] != "true" || props["soft_delete_column"] != "deleted_at" || props["audit_columns"] != "created_by" {
		t.Errorf("unexpected stamp: %v", props)
	}
	if _, ok := props["timestamps"]; ok {
		t.Error("timestamps must be omitted when false")
	}
}
