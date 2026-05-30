package csharp_test

// ---------------------------------------------------------------------------
// ORM Relationships — association, foreign key, lazy loading
// ---------------------------------------------------------------------------

import "testing"

func TestLinqToSQLAssociation(t *testing.T) {
	src := `
using System.Data.Linq;
using System.Data.Linq.Mapping;

[Table(Name = "orders")]
public class Order
{
    [Column(IsPrimaryKey = true)]
    public int OrderId { get; set; }

    [Association(ThisKey = "OrderId", OtherKey = "OrderId")]
    public EntitySet<OrderDetail> Details { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_relationships", fi("Order.cs", "csharp", src))

	foundAssoc := false
	foundFK := false
	for _, e := range ents {
		if e.Subtype == "association_extraction" {
			foundAssoc = true
		}
		if e.Subtype == "foreign_key_extraction" {
			foundFK = true
		}
	}
	if !foundAssoc {
		t.Error("expected association_extraction entity from [Association(...)]")
	}
	if !foundFK {
		t.Error("expected foreign_key_extraction entity from [Association(ThisKey/OtherKey)]")
	}
}

func TestLinqToSQLLoadWith(t *testing.T) {
	src := `
using System.Data.Linq;

var db = new NorthwindContext();
var opts = new DataLoadOptions();
opts.LoadWith<Customer>(c => c.Orders);
db.LoadOptions = opts;
`
	ents := extract(t, "custom_csharp_orm_relationships", fi("Query.cs", "csharp", src))

	foundLazy := false
	for _, e := range ents {
		if e.Subtype == "lazy_loading_recognition" {
			foundLazy = true
		}
	}
	if !foundLazy {
		t.Error("expected lazy_loading_recognition from LoadWith<T>() call")
	}
}

func TestLinqToDBAssociation(t *testing.T) {
	src := `
using LinqToDB;
using LinqToDB.Mapping;

[Table]
public class Customer
{
    [PrimaryKey, Identity]
    public int Id { get; set; }

    [Association(ThisKey = "Id", OtherKey = "CustomerId")]
    public IEnumerable<Order> Orders { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_relationships", fi("Customer.cs", "csharp", src))

	foundAssoc := false
	foundFK := false
	for _, e := range ents {
		if e.Subtype == "association_extraction" {
			foundAssoc = true
		}
		if e.Subtype == "foreign_key_extraction" {
			foundFK = true
		}
	}
	if !foundAssoc {
		t.Error("expected association_extraction from LinqToDB [Association(...)]")
	}
	if !foundFK {
		t.Error("expected foreign_key_extraction from LinqToDB [Association(ThisKey/OtherKey)]")
	}
}

func TestForeignKeyAttribute(t *testing.T) {
	src := `
using System.ComponentModel.DataAnnotations.Schema;

public class OrderDetail
{
    [ForeignKey("OrderId")]
    public Order Order { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_relationships", fi("OrderDetail.cs", "csharp", src))

	foundFK := false
	for _, e := range ents {
		if e.Subtype == "foreign_key_extraction" {
			foundFK = true
		}
	}
	if !foundFK {
		t.Error("expected foreign_key_extraction from [ForeignKey(\"OrderId\")] attribute")
	}
}

func TestNHibernateReferences(t *testing.T) {
	src := `
using FluentNHibernate.Mapping;

public class OrderMap : ClassMap<Order>
{
    public OrderMap()
    {
        Table("orders");
        Id(x => x.Id);
        References(x => x.Customer).Column("customer_id");
        HasMany(x => x.Items).LazyLoad();
    }
}
`
	ents := extract(t, "custom_csharp_orm_relationships", fi("OrderMap.cs", "csharp", src))

	foundAssocRef := false
	foundAssocMany := false
	foundFK := false
	foundLazy := false
	for _, e := range ents {
		switch {
		case e.Subtype == "association_extraction" && e.Name == "nhibernate:assoc:ref:Customer":
			foundAssocRef = true
		case e.Subtype == "association_extraction" && e.Name == "nhibernate:assoc:hasmany:Items":
			foundAssocMany = true
		case e.Subtype == "foreign_key_extraction":
			foundFK = true
		case e.Subtype == "lazy_loading_recognition":
			foundLazy = true
		}
	}
	if !foundAssocRef {
		t.Error("expected association_extraction for References(x => x.Customer)")
	}
	if !foundAssocMany {
		t.Error("expected association_extraction for HasMany(x => x.Items)")
	}
	if !foundFK {
		t.Error("expected foreign_key_extraction from .Column(\"customer_id\")")
	}
	if !foundLazy {
		t.Error("expected lazy_loading_recognition from .LazyLoad()")
	}
}

func TestNHibernateNotLazyLoad(t *testing.T) {
	src := `
using FluentNHibernate.Mapping;

public class ProductMap : ClassMap<Product>
{
    public ProductMap()
    {
        HasMany(x => x.Tags).Not.LazyLoad();
    }
}
`
	ents := extract(t, "custom_csharp_orm_relationships", fi("ProductMap.cs", "csharp", src))

	foundLazy := false
	for _, e := range ents {
		if e.Subtype == "lazy_loading_recognition" {
			foundLazy = true
		}
	}
	if !foundLazy {
		t.Error("expected lazy_loading_recognition from .Not.LazyLoad()")
	}
}

func TestOrmRelationshipsNoMatch(t *testing.T) {
	src := `namespace App { class Helper { } }`
	ents := extract(t, "custom_csharp_orm_relationships", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
