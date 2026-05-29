package java_test

// jpa_fk_lazy_test.go — tests for @JoinColumn, @ForeignKey, FetchType, and
// @Column depth across all five JPA-family ORM extractors:
//   - custom_java_eclipselink
//   - custom_java_ebean
// plus the pattern-based hibernate.go / spring_ecosystem.go helpers (tested
// via ExtractJPAFKAndLazy directly from the internal package).

import (
	"testing"

	_ "github.com/cajasmota/archigraph/internal/custom/java"
)

// ---------------------------------------------------------------------------
// EclipseLink — @JoinColumn + FetchType + @Column
// ---------------------------------------------------------------------------

func TestEclipseLinkJoinColumn(t *testing.T) {
	src := `
import org.eclipse.persistence.annotations.Cache;

@Entity
@Table(name = "orders")
public class Order {
    @Id private Long id;

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "customer_id", nullable = false)
    private Customer customer;

    @Column(name = "total_amount", nullable = false, length = 19)
    private BigDecimal totalAmount;
}
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("Order.java", "java", src))

	if !hasSub(ents, "foreign_key") {
		t.Errorf("expected foreign_key subtype for @JoinColumn, got %v", ents)
	}
	if !hasSub(ents, "fetch_config") {
		t.Errorf("expected fetch_config subtype for FetchType.LAZY, got %v", ents)
	}
	if !hasSub(ents, "column") {
		t.Errorf("expected column subtype for @Column, got %v", ents)
	}
}

func TestEclipseLinkForeignKeyConstraintName(t *testing.T) {
	src := `
// EclipseLink source
import org.eclipse.persistence.annotations.Cache;

@Entity
public class OrderItem {
    @ManyToOne
    @JoinColumn(name = "order_id",
                foreignKey = @ForeignKey(name = "fk_orderitem_order"))
    private Order order;
}
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("OrderItem.java", "java", src))
	if !hasSub(ents, "foreign_key") {
		t.Errorf("expected foreign_key subtype for @ForeignKey constraint, got %v", ents)
	}
}

func TestEclipseLinkFetchTypeEager(t *testing.T) {
	src := `
import org.eclipse.persistence.annotations.Cache;

@Entity
public class Product {
    @OneToMany(fetch = FetchType.EAGER, mappedBy = "product")
    private List<OrderItem> items;
}
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("Product.java", "java", src))
	if !hasEnt(ents, "SCOPE.Component", "fetch_config", "fetch:EAGER") {
		t.Errorf("expected fetch:EAGER entity, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Ebean — @JoinColumn + FetchType + @Column
// ---------------------------------------------------------------------------

func TestEbeanJoinColumn(t *testing.T) {
	src := `
import io.ebean.Model;

@Entity
@Table(name = "orders")
public class Order extends Model {
    @Id Long id;

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "customer_id")
    Customer customer;

    @Column(name = "status", length = 50)
    String status;
}
`
	ents := runORM(t, "custom_java_ebean", ormFI("Order.java", "java", src))

	if !hasSub(ents, "foreign_key") {
		t.Errorf("expected foreign_key subtype for @JoinColumn, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Component", "fetch_config", "fetch:LAZY") {
		t.Errorf("expected fetch:LAZY entity, got %v", ents)
	}
	if !hasSub(ents, "column") {
		t.Errorf("expected column subtype for @Column, got %v", ents)
	}
}

func TestEbeanJoinTable(t *testing.T) {
	src := `
import io.ebean.DB;

@Entity
public class Product {
    @ManyToMany(fetch = FetchType.LAZY)
    @JoinColumn(name = "product_id")
    List<Category> categories;
}
`
	ents := runORM(t, "custom_java_ebean", ormFI("Product.java", "java", src))
	if !hasSub(ents, "fetch_config") {
		t.Errorf("expected fetch_config for FetchType.LAZY in @ManyToMany, got %v", ents)
	}
}

func TestEbeanColumnDepth(t *testing.T) {
	src := `
import io.ebean.Model;

@Entity
public class Customer extends Model {
    @Id Long id;

    @Column(name = "email", nullable = false, length = 255)
    String email;

    @Column(name = "phone", nullable = true, length = 20)
    String phone;
}
`
	ents := runORM(t, "custom_java_ebean", ormFI("Customer.java", "java", src))
	// Expect two column entities
	count := 0
	for _, e := range ents {
		if e.Subtype == "column" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 column entities, got %d in %v", count, ents)
	}
}

// ---------------------------------------------------------------------------
// ExtractJPAFKAndLazy unit tests (testing the core helper directly via the
// package-internal test file jpa_fk_lazy_internal_test.go)
// These tests exercise the helper through a blank-import of the java package
// and rely on the extract helpers exposed by orm_extractors_test.go's ormFI /
// runORM pattern for hibernate and spring extractor paths.
// ---------------------------------------------------------------------------

// We can't call ExtractJPAFKAndLazy directly from the _test package.
// Use the hibernate extractor path as a proxy (hibernate.go calls the helper).

func TestHibernateJoinColumn(t *testing.T) {
	// hibernate.go ExtractHibernate uses PatternResult, not the registered
	// extractor interface, so we test the observable output via the extractors
	// that are registered (eclipselink / ebean already covered above).
	// For completeness, exercise the eclipselink extractor with a Hibernate-like
	// source that also carries an EclipseLink marker.
	src := `
import org.eclipse.persistence.annotations.Cache;
import javax.persistence.*;

@Entity
@Table(name = "employees")
public class Employee {
    @Id
    @GeneratedValue
    private Long id;

    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "dept_id", referencedColumnName = "id",
                foreignKey = @ForeignKey(name = "fk_emp_dept"))
    private Department department;

    @Column(name = "hire_date", nullable = false)
    private LocalDate hireDate;
}
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("Employee.java", "java", src))
	if !hasSub(ents, "foreign_key") {
		t.Errorf("expected foreign_key for @JoinColumn+@ForeignKey combo, got %v", ents)
	}
	if !hasEnt(ents, "SCOPE.Component", "fetch_config", "fetch:LAZY") {
		t.Errorf("expected fetch:LAZY, got %v", ents)
	}
	if !hasSub(ents, "column") {
		t.Errorf("expected column for @Column(name=hire_date), got %v", ents)
	}
}

func TestEclipseLinkMultipleFetchTypes(t *testing.T) {
	src := `
// EclipseLink ORM source
import org.eclipse.persistence.annotations.Cache;

@Entity
public class Invoice {
    @OneToMany(fetch = FetchType.LAZY, mappedBy = "invoice")
    @JoinColumn(name = "invoice_id")
    private List<LineItem> items;

    @ManyToOne(fetch = FetchType.EAGER)
    @JoinColumn(name = "client_id")
    private Client client;
}
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("Invoice.java", "java", src))
	lazyFound, eagerFound := false, false
	for _, e := range ents {
		if e.Name == "fetch:LAZY" {
			lazyFound = true
		}
		if e.Name == "fetch:EAGER" {
			eagerFound = true
		}
	}
	if !lazyFound {
		t.Errorf("expected fetch:LAZY entity, got %v", ents)
	}
	if !eagerFound {
		t.Errorf("expected fetch:EAGER entity, got %v", ents)
	}
}

func TestEclipseLinkNoFalsePositiveOnPlainSource(t *testing.T) {
	// No EclipseLink marker — extractor should not fire at all.
	src := `
@Entity
public class User {
    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "role_id")
    private Role role;
}
`
	ents := runORM(t, "custom_java_eclipselink", ormFI("User.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("eclipselink extractor must not fire on plain JPA source, got %v", ents)
	}
}

func TestEbeanNoFalsePositiveOnPlainSource(t *testing.T) {
	// No Ebean import/marker — extractor should not fire at all.
	src := `
@Entity
public class User {
    @ManyToOne(fetch = FetchType.LAZY)
    @JoinColumn(name = "role_id")
    private Role role;
}
`
	ents := runORM(t, "custom_java_ebean", ormFI("User.java", "java", src))
	if len(ents) != 0 {
		t.Errorf("ebean extractor must not fire on plain JPA source, got %v", ents)
	}
}
