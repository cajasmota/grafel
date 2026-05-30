package csharp_test

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Dapper POCO models
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Deep extraction: model_extraction from Query<T>() + POCO properties
// ---------------------------------------------------------------------------

func TestDapperDeepModelExtraction(t *testing.T) {
	src := `
using Dapper;

public class UserRepository
{
    public IEnumerable<User> GetAll(IDbConnection conn)
    {
        return conn.Query<User>("SELECT id, username, email FROM users");
    }
}

public class User
{
    public int Id { get; set; }
    public string Username { get; set; }
    public string Email { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("UserRepository.cs", "csharp", src))

	// model_extraction: the POCO type T = User from Query<User>()
	if !containsEntity(ents, "SCOPE.Component", "dapper:model:User") {
		t.Error("expected dapper:model:User POCO model entity from Query<User>() call")
	}

	// model_extraction: properties of User
	if !containsEntity(ents, "SCOPE.Pattern", "dapper:prop:User.Id") {
		t.Error("expected dapper:prop:User.Id from POCO class body scan")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "dapper:prop:User.Username") {
		t.Error("expected dapper:prop:User.Username from POCO class body scan")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "dapper:prop:User.Email") {
		t.Error("expected dapper:prop:User.Email from POCO class body scan")
	}
}

// ---------------------------------------------------------------------------
// Deep extraction: query_attribution — SQL verb + table attributed to T
// ---------------------------------------------------------------------------

func TestDapperDeepQueryAttrSelect(t *testing.T) {
	src := `
using Dapper;

public class OrderRepo
{
    public IEnumerable<Order> GetOrders(IDbConnection db)
    {
        return db.Query<Order>("SELECT order_id, total FROM orders WHERE status = @status");
    }
}

public class Order
{
    public int OrderId { get; set; }
    public decimal Total { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("OrderRepo.cs", "csharp", src))

	// query_attribution Operation with sql_verb=SELECT and sql_table=orders
	foundAttr := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query_attribution" {
			foundAttr = true
			break
		}
	}
	if !foundAttr {
		t.Error("expected query_attribution Operation entity from Query<Order>(SQL)")
	}
}

func TestDapperDeepQueryAttrInsert(t *testing.T) {
	src := `
using Dapper;

public class ProductRepo
{
    public void Insert(IDbConnection db, Product p)
    {
        db.Execute("INSERT INTO products (name, price) VALUES (@Name, @Price)", p);
    }
}

public class Product
{
    public string Name { get; set; }
    public decimal Price { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("ProductRepo.cs", "csharp", src))

	// query_attribution for Execute with INSERT
	foundAttr := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query_attribution" {
			foundAttr = true
			break
		}
	}
	if !foundAttr {
		t.Error("expected query_attribution Operation entity from Execute(INSERT SQL)")
	}
}

func TestDapperDeepQueryAttrAsync(t *testing.T) {
	src := `
using Dapper;

public class CustomerRepo
{
    public async Task<IEnumerable<Customer>> GetAsync(IDbConnection db)
    {
        return await db.QueryAsync<Customer>("SELECT customer_id, full_name FROM customers");
    }
}

public class Customer
{
    public int CustomerId { get; set; }
    public string FullName { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("CustomerRepo.cs", "csharp", src))

	// model_extraction: Customer from QueryAsync<Customer>()
	if !containsEntity(ents, "SCOPE.Component", "dapper:model:Customer") {
		t.Error("expected dapper:model:Customer from QueryAsync<Customer>()")
	}
	// property extraction
	if !containsEntity(ents, "SCOPE.Pattern", "dapper:prop:Customer.CustomerId") {
		t.Error("expected dapper:prop:Customer.CustomerId from POCO scan")
	}
}

// ---------------------------------------------------------------------------
// Deep extraction: schema_extraction — columns from SELECT column list
// ---------------------------------------------------------------------------

func TestDapperDeepSchemaSelectColumns(t *testing.T) {
	src := `
using Dapper;

public class ReportRepo
{
    public IEnumerable<Report> GetReports(IDbConnection db)
    {
        return db.Query<Report>("SELECT report_id, title, created_at FROM reports");
    }
}

public class Report
{
    public int ReportId { get; set; }
    public string Title { get; set; }
    public DateTime CreatedAt { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("ReportRepo.cs", "csharp", src))

	// schema_extraction: columns from SQL SELECT list
	cols := map[string]bool{}
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "schema_extraction" {
			cols[e.Name] = true
		}
	}

	// Each column from the SELECT list should appear as a schema entity
	for _, expected := range []string{"report_id", "title", "created_at"} {
		found := false
		for name := range cols {
			// name includes file:line suffix — check prefix
			if len(name) >= len("dapper:sql_col:"+expected) &&
				name[:len("dapper:sql_col:"+expected)] == "dapper:sql_col:"+expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected schema_extraction entity for SQL column %q", expected)
		}
	}
}

func TestDapperDeepSchemaInsertColumns(t *testing.T) {
	src := `
using Dapper;

public class EventRepo
{
    public void Save(IDbConnection db, Event ev)
    {
        db.Execute("INSERT INTO events (event_type, payload, occurred_at) VALUES (@Type, @Payload, @OccurredAt)", ev);
    }
}

public class Event
{
    public string EventType { get; set; }
    public string Payload { get; set; }
    public DateTime OccurredAt { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("EventRepo.cs", "csharp", src))

	// schema_extraction: columns from INSERT column list
	for _, expected := range []string{"event_type", "payload", "occurred_at"} {
		found := false
		for _, e := range ents {
			if e.Kind == "SCOPE.Pattern" && e.Subtype == "schema_extraction" &&
				len(e.Name) >= len("dapper:sql_col:"+expected) &&
				e.Name[:len("dapper:sql_col:"+expected)] == "dapper:sql_col:"+expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected schema_extraction entity for INSERT column %q", expected)
		}
	}
}

func TestDapperDeepSchemaSelectStarHonest(t *testing.T) {
	// SELECT * produces no column entities (honest-partial: table is known, columns are not)
	src := `
using Dapper;

public class AuditRepo
{
    public IEnumerable<Audit> GetAll(IDbConnection db)
    {
        return db.Query<Audit>("SELECT * FROM audit_log");
    }
}

public class Audit
{
    public int Id { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("AuditRepo.cs", "csharp", src))

	// model_extraction still fires for Audit
	if !containsEntity(ents, "SCOPE.Component", "dapper:model:Audit") {
		t.Error("expected dapper:model:Audit even for SELECT *")
	}

	// No sql_col schema entities from SELECT *
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "schema_extraction" &&
			len(e.Name) > len("dapper:sql_col:") &&
			e.Name[:len("dapper:sql_col:")] == "dapper:sql_col:" {
			t.Errorf("unexpected sql_col schema entity from SELECT *: %s", e.Name)
		}
	}
}

func TestDapperDeepQueryFirstSingle(t *testing.T) {
	// QueryFirst<T> and QuerySingle<T> should also trigger deep extraction
	src := `
using Dapper;

public class AccountRepo
{
    public Account Get(IDbConnection db, int id)
    {
        return db.QueryFirst<Account>("SELECT account_id, balance FROM accounts WHERE account_id = @id");
    }

    public Account GetSingle(IDbConnection db, string code)
    {
        return db.QuerySingle<Account>("SELECT account_id, balance FROM accounts WHERE code = @code");
    }
}

public class Account
{
    public int AccountId { get; set; }
    public decimal Balance { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("AccountRepo.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Component", "dapper:model:Account") {
		t.Error("expected dapper:model:Account from QueryFirst<Account>()")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "dapper:prop:Account.AccountId") {
		t.Error("expected dapper:prop:Account.AccountId from POCO scan")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "dapper:prop:Account.Balance") {
		t.Error("expected dapper:prop:Account.Balance from POCO scan")
	}
}

func TestDapperTableAttribute(t *testing.T) {
	src := `
using Dapper;
using System.ComponentModel.DataAnnotations.Schema;

[Table("products")]
public class Product
{
    [Column("product_id")]
    public int Id { get; set; }

    [Column("product_name")]
    public string Name { get; set; }

    [Column("price")]
    public decimal Price { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("Product.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Component", "dapper:table:products") {
		t.Error("expected dapper:table:products from [Table(\"products\")]")
	}
	if !containsEntity(ents, "SCOPE.Component", "dapper:poco:Product") {
		t.Error("expected dapper:poco:Product POCO class entity")
	}
	// At least one [Column] entity should appear
	foundColumn := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "schema_extraction" {
			foundColumn = true
			break
		}
	}
	if !foundColumn {
		t.Error("expected schema_extraction entity from [Column] attribute")
	}
}

func TestDapperQueryAttribution(t *testing.T) {
	src := `
using Dapper;

public class OrderRepository
{
    public IEnumerable<Order> GetAll(IDbConnection conn)
    {
        return conn.Query<Order>("SELECT * FROM orders");
    }

    public void Create(IDbConnection conn, Order order)
    {
        conn.Execute("INSERT INTO orders VALUES (@Id, @Total)", order);
    }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("OrderRepository.cs", "csharp", src))

	foundQuery := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query_attribution" {
			foundQuery = true
			break
		}
	}
	if !foundQuery {
		t.Error("expected query_attribution from Dapper Query<T> / Execute calls")
	}
}

// ---------------------------------------------------------------------------
// LINQ-to-SQL / LinqToDB
// ---------------------------------------------------------------------------

func TestLinqToDBTableAndColumn(t *testing.T) {
	src := `
using LinqToDB;
using LinqToDB.Mapping;

[Table(Name = "customers")]
public class Customer
{
    [Column(Name = "customer_id"), PrimaryKey, Identity]
    public int Id { get; set; }

    [Column(Name = "full_name")]
    public string Name { get; set; }

    [Association(ThisKey = "Id", OtherKey = "CustomerId")]
    public List<Order> Orders { get; set; }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("Customer.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Component", "linqtodb:table:customers") {
		t.Error("expected linqtodb:table:customers from [Table(Name=\"customers\")]")
	}
	foundSchema := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "schema_extraction" {
			foundSchema = true
			break
		}
	}
	if !foundSchema {
		t.Error("expected schema_extraction from [Column] attribute in linqtodb")
	}
	foundAssoc := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "relationship_extraction" {
			foundAssoc = true
			break
		}
	}
	if !foundAssoc {
		t.Error("expected relationship_extraction from [Association] attribute")
	}
}

func TestLinqToSQLContext(t *testing.T) {
	src := `
using System.Data.Linq;
using System.Data.Linq.Mapping;

[Table(Name="orders")]
public class Order
{
    [Column(IsPrimaryKey=true)]
    public int OrderId { get; set; }

    [Column]
    public string Description { get; set; }
}

public class ShopDataContext : DataContext
{
    public Table<Order> Orders;
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("ShopDataContext.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Component", "linq-to-sql:context:ShopDataContext") {
		t.Error("expected linq-to-sql:context:ShopDataContext")
	}
	if !containsEntity(ents, "SCOPE.Component", "linq-to-sql:table_prop:Order") {
		t.Error("expected linq-to-sql:table_prop:Order from Table<Order>")
	}
}

// ---------------------------------------------------------------------------
// NHibernate / FluentNHibernate
// ---------------------------------------------------------------------------

func TestNHibernateClassMap(t *testing.T) {
	src := `
using FluentNHibernate.Mapping;

public class ProductMap : ClassMap<Product>
{
    public ProductMap()
    {
        Table("products");
        Id(x => x.Id).Column("id").GeneratedBy.Native();
        Map(x => x.Name).Column("name").Not.Nullable();
        Map(x => x.Price).Column("price");
        References(x => x.Category).Column("category_id");
        HasMany(x => x.Reviews).KeyColumn("product_id");
    }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("ProductMap.cs", "csharp", src))

	if !containsEntity(ents, "SCOPE.Component", "nhibernate:classmap:ProductMap") {
		t.Error("expected nhibernate:classmap:ProductMap")
	}
	if !containsEntity(ents, "SCOPE.Component", "nhibernate:schema:Product") {
		t.Error("expected nhibernate:schema:Product")
	}
	foundRef := false
	foundHasMany := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "relationship_extraction" {
			if e.Name == "nhibernate:references:Category" {
				foundRef = true
			}
			if e.Name == "nhibernate:hasmany:Reviews" {
				foundHasMany = true
			}
		}
	}
	if !foundRef {
		t.Error("expected relationship_extraction for References(x => x.Category)")
	}
	if !foundHasMany {
		t.Error("expected relationship_extraction for HasMany(x => x.Reviews)")
	}
}

func TestNHibernateSessionQuery(t *testing.T) {
	src := `
using NHibernate;

public class ProductRepository
{
    private readonly ISession _session;

    public IList<Product> GetAll()
    {
        return _session.Query<Product>().ToList();
    }

    public Product GetById(int id)
    {
        return _session.Get<Product>(id);
    }
}
`
	ents := extract(t, "custom_csharp_orm_models", fi("ProductRepository.cs", "csharp", src))

	foundQuery := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && e.Subtype == "query_attribution" {
			foundQuery = true
			break
		}
	}
	if !foundQuery {
		t.Error("expected query_attribution from ISession.Query<T> / Get<T>")
	}
}

// ---------------------------------------------------------------------------
// Non-csharp files should produce no entities
// ---------------------------------------------------------------------------

func TestOrmModelsNonCsharpFile(t *testing.T) {
	src := `
using Dapper;
[Table("x")]
public class X {}
`
	ents := extract(t, "custom_csharp_orm_models", fi("model.go", "go", src))
	if len(ents) != 0 {
		t.Errorf("expected 0 entities for non-csharp file, got %d", len(ents))
	}
}
