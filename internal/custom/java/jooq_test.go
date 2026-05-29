package java_test

// jooq_test.go — tests for the custom_java_jooq extractor (#3098).
//
// Coverage cells exercised:
//   schema_extraction        (Table class + TableField scanning)
//   association_extraction   (ForeignKey source→target pairs)
//   foreign_key_extraction   (ForeignKey constraint names)
//   relationship_extraction  (ForeignKey relation entities)

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/custom/java"
)

// ---------------------------------------------------------------------------
// Helpers (reuse runORM / ormFI from orm_extractors_test.go)
// ---------------------------------------------------------------------------

func jooqFI(path, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: "java", Content: []byte(src)}
}

func runJooq(t *testing.T, file extreg.FileInput) []ormEnt {
	t.Helper()
	return runORM(t, "custom_java_jooq", file)
}

// ---------------------------------------------------------------------------
// schema_extraction — Table class scanning
// ---------------------------------------------------------------------------

func TestJooqTableClassExtracted(t *testing.T) {
	src := `
import org.jooq.impl.TableImpl;
import org.jooq.TableField;

public class Customer extends TableImpl<CustomerRecord> {
    public static final Customer CUSTOMER = new Customer();
    public final TableField<CustomerRecord, Long> ID = createField(DSL.name("ID"), SQLDataType.BIGINT, this, "");
    public final TableField<CustomerRecord, String> FIRST_NAME = createField(DSL.name("FIRST_NAME"), SQLDataType.VARCHAR(50), this, "");
    public final TableField<CustomerRecord, String> EMAIL = createField(DSL.name("EMAIL"), SQLDataType.VARCHAR(255), this, "");
}
`
	ents := runJooq(t, jooqFI("Customer.java", src))

	if !hasEnt(ents, "SCOPE.Schema", "table", "Customer") {
		t.Errorf("expected Customer table entity, got %v", ents)
	}
	// Columns must be present.
	if !hasEnt(ents, "SCOPE.Schema", "column", "Customer.ID") {
		t.Errorf("expected Customer.ID column entity, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Schema", "column", "Customer.FIRST_NAME") {
		t.Errorf("expected Customer.FIRST_NAME column entity, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Schema", "column", "Customer.EMAIL") {
		t.Errorf("expected Customer.EMAIL column entity, got %v", ents)
	}
}

func TestJooqMultipleTableClasses(t *testing.T) {
	// A file that defines two Table classes.
	src := `
import org.jooq.impl.TableImpl;
import org.jooq.TableField;

public class CustomerTable extends TableImpl<CustomerRecord> {
    public final TableField<CustomerRecord, Long> ID = createField(DSL.name("ID"), SQLDataType.BIGINT, this, "");
}

public class OrderTable extends TableImpl<OrderRecord> {
    public final TableField<OrderRecord, Long> ID = createField(DSL.name("ID"), SQLDataType.BIGINT, this, "");
    public final TableField<OrderRecord, Long> CUSTOMER_ID = createField(DSL.name("CUSTOMER_ID"), SQLDataType.BIGINT, this, "");
}
`
	ents := runJooq(t, jooqFI("Tables.java", src))
	if !hasEnt(ents, "SCOPE.Schema", "table", "CustomerTable") {
		t.Errorf("expected CustomerTable table entity, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Schema", "table", "OrderTable") {
		t.Errorf("expected OrderTable table entity, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Schema", "column", "CustomerTable.ID") {
		t.Errorf("expected CustomerTable.ID column, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Schema", "column", "OrderTable.CUSTOMER_ID") {
		t.Errorf("expected OrderTable.CUSTOMER_ID column, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// foreign_key_extraction + association_extraction + relationship_extraction
// ---------------------------------------------------------------------------

func TestJooqForeignKeyInKeysFile(t *testing.T) {
	src := `
import org.jooq.ForeignKey;
import org.jooq.UniqueKey;
import org.jooq.impl.Internal;
import org.jooq.impl.DSL;

public class Keys {

    public static final UniqueKey<CustomerRecord> KEY_CUSTOMER_PRIMARY = Internal.createUniqueKey(Customer.CUSTOMER, DSL.name("KEY_CUSTOMER_PRIMARY"), new TableField[] { Customer.CUSTOMER.ID }, true);

    public static final ForeignKey<OrderRecord, CustomerRecord> FK_ORDER_CUSTOMER = Internal.createForeignKey(Order.ORDER, DSL.name("FK_ORDER_CUSTOMER"), new TableField[] { Order.ORDER.CUSTOMER_ID }, Keys.KEY_CUSTOMER_PRIMARY, new TableField[] { Customer.CUSTOMER.ID }, true);

    public static final ForeignKey<OrderRecord, CustomerRecord> FK_ORDER_SHIPPING = Internal.createForeignKey(Order.ORDER, DSL.name("FK_ORDER_SHIPPING"), new TableField[] { Order.ORDER.SHIPPING_ID }, Keys.KEY_CUSTOMER_PRIMARY, new TableField[] { Customer.CUSTOMER.ID }, true);
}
`
	ents := runJooq(t, jooqFI("Keys.java", src))

	// Foreign key entities.
	if !hasSub(ents, "foreign_key") {
		t.Errorf("expected foreign_key entities from ForeignKey declarations, got %v", ents)
	}
	// Check that FK_ORDER_CUSTOMER is captured.
	found := false
	for _, e := range ents {
		if e.Subtype == "foreign_key" && strings.Contains(e.Name, "FK_ORDER_CUSTOMER") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected FK_ORDER_CUSTOMER foreign_key entity, got %v", ents)
	}

	// Relation entities (association_extraction / relationship_extraction).
	if !hasSub(ents, "relation") {
		t.Errorf("expected relation entities from ForeignKey declarations, got %v", ents)
	}

	// UniqueKey entities.
	if !hasSub(ents, "unique_key") {
		t.Errorf("expected unique_key entity for KEY_CUSTOMER_PRIMARY, got %v", ents)
	}
}

func TestJooqForeignKeyRelationDirection(t *testing.T) {
	src := `
import org.jooq.ForeignKey;
import org.jooq.impl.Internal;
import org.jooq.impl.DSL;

public class Keys {
    public static final ForeignKey<LineItemRecord, OrderRecord> FK_LINEITEM_ORDER = Internal.createForeignKey(LineItem.LINE_ITEM, DSL.name("FK_LINEITEM_ORDER"), new TableField[] { LineItem.LINE_ITEM.ORDER_ID }, Keys.KEY_ORDER_PRIMARY, new TableField[] { Order.ORDER.ID }, true);
}
`
	ents := runJooq(t, jooqFI("Keys.java", src))

	// There should be a relation entity showing LineItemRecord->OrderRecord direction.
	if !hasEnt(ents, "SCOPE.Component", "relation", "LineItemRecord->OrderRecord") {
		t.Errorf("expected LineItemRecord->OrderRecord relation entity, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Gate — non-jOOQ source must produce no entities
// ---------------------------------------------------------------------------

func TestJooqSkipsNonJooqSource(t *testing.T) {
	src := `
@Entity
@Table(name = "users")
public class User {
    @Id
    private Long id;
    private String name;
}
`
	ents := runJooq(t, jooqFI("User.java", src))
	if len(ents) != 0 {
		t.Errorf("jooq extractor must not fire on plain JPA source, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Integration: fixture file
// ---------------------------------------------------------------------------

func TestJooqFixtureTablesFile(t *testing.T) {
	ctx := context.Background()
	e, ok := extreg.Get("custom_java_jooq")
	if !ok {
		t.Fatal("custom_java_jooq not registered")
	}

	src := `
import org.jooq.impl.TableImpl;
import org.jooq.TableField;
import org.jooq.impl.DSL;

public class Customer extends TableImpl<CustomerRecord> {
    public static final Customer CUSTOMER = new Customer();
    public final TableField<CustomerRecord, Long> ID = createField(DSL.name("ID"), SQLDataType.BIGINT.nullable(false), this, "");
    public final TableField<CustomerRecord, String> FIRST_NAME = createField(DSL.name("FIRST_NAME"), SQLDataType.VARCHAR(50).nullable(false), this, "");
    public final TableField<CustomerRecord, String> LAST_NAME = createField(DSL.name("LAST_NAME"), SQLDataType.VARCHAR(50).nullable(false), this, "");
    public final TableField<CustomerRecord, String> EMAIL = createField(DSL.name("EMAIL"), SQLDataType.VARCHAR(255).nullable(false), this, "");
}
`
	ents, err := e.Extract(ctx, extreg.FileInput{
		Path:     "testdata/fixtures/sources/java/jooq/Tables.java",
		Language: "java",
		Content:  []byte(src),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) == 0 {
		t.Fatal("expected entities from fixture, got none")
	}

	tableFound := false
	for _, ent := range ents {
		if ent.Subtype == "table" {
			tableFound = true
		}
	}
	if !tableFound {
		t.Errorf("expected at least one table entity from fixture, got %v", ents)
	}
}
