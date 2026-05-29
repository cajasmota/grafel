package java_test

// dynamodb_java_test.go — tests for the custom_java_dynamodb extractor.
//
// Covers:
//   - @DynamoDbBean class → model entity
//   - @DynamoDbPartitionKey → partition_key component
//   - @DynamoDbSortKey → sort_key component
//   - @DynamoDbAttribute → attribute component
//   - @DynamoDbIgnore → ignored_field component
//   - Gate: non-DynamoDB source must produce no results
//   - Fixture file round-trip (testdata/fixtures/sources/java/dynamodb/CustomerEntity.java)

import (
	"context"
	"os"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/custom/java"
)

// ddbFI builds a FileInput for DynamoDB tests.
func ddbFI(path, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: "java", Content: []byte(src)}
}

type ddbEnt struct{ Kind, Subtype, Name string }

func runDynamoDB(t *testing.T, file extreg.FileInput) []ddbEnt {
	t.Helper()
	e, ok := extreg.Get("custom_java_dynamodb")
	if !ok {
		t.Fatal("extractor custom_java_dynamodb not registered")
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []ddbEnt
	for _, ent := range ents {
		out = append(out, ddbEnt{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

func hasDDB(ents []ddbEnt, kind, subtype, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Subtype == subtype && e.Name == name {
			return true
		}
	}
	return false
}

func hasDDBSub(ents []ddbEnt, subtype string) bool {
	for _, e := range ents {
		if e.Subtype == subtype {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Basic bean model detection
// ---------------------------------------------------------------------------

func TestDynamoDBBeanModelExtraction(t *testing.T) {
	src := `
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbBean;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbPartitionKey;

@DynamoDbBean
public class OrderEntity {
    private String orderId;

    @DynamoDbPartitionKey
    public String getOrderId() { return orderId; }
    public void setOrderId(String id) { this.orderId = id; }
}
`
	ents := runDynamoDB(t, ddbFI("OrderEntity.java", src))
	if !hasDDB(ents, "SCOPE.Schema", "model", "OrderEntity") {
		t.Errorf("expected SCOPE.Schema/model/OrderEntity, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Partition key detection
// ---------------------------------------------------------------------------

func TestDynamoDBPartitionKey(t *testing.T) {
	src := `
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbBean;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbPartitionKey;

@DynamoDbBean
public class ProductEntity {
    private String productId;

    @DynamoDbPartitionKey
    public String getProductId() { return productId; }
    public void setProductId(String id) { this.productId = id; }
}
`
	ents := runDynamoDB(t, ddbFI("ProductEntity.java", src))
	if !hasDDBSub(ents, "partition_key") {
		t.Errorf("expected partition_key subtype, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Sort key detection
// ---------------------------------------------------------------------------

func TestDynamoDBSortKey(t *testing.T) {
	src := `
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbBean;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbPartitionKey;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbSortKey;

@DynamoDbBean
public class EventEntity {
    private String eventId;
    private String timestamp;

    @DynamoDbPartitionKey
    public String getEventId() { return eventId; }
    public void setEventId(String id) { this.eventId = id; }

    @DynamoDbSortKey
    public String getTimestamp() { return timestamp; }
    public void setTimestamp(String ts) { this.timestamp = ts; }
}
`
	ents := runDynamoDB(t, ddbFI("EventEntity.java", src))
	if !hasDDBSub(ents, "sort_key") {
		t.Errorf("expected sort_key subtype, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Attribute alias detection
// ---------------------------------------------------------------------------

func TestDynamoDBAttribute(t *testing.T) {
	src := `
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbBean;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbPartitionKey;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbAttribute;

@DynamoDbBean
public class UserEntity {
    private String userId;
    private String emailAddress;

    @DynamoDbPartitionKey
    public String getUserId() { return userId; }
    public void setUserId(String id) { this.userId = id; }

    @DynamoDbAttribute("email_address")
    public String getEmailAddress() { return emailAddress; }
    public void setEmailAddress(String email) { this.emailAddress = email; }
}
`
	ents := runDynamoDB(t, ddbFI("UserEntity.java", src))
	if !hasDDB(ents, "SCOPE.Component", "attribute", "attr:email_address") {
		t.Errorf("expected attribute attr:email_address, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Ignored field detection
// ---------------------------------------------------------------------------

func TestDynamoDBIgnore(t *testing.T) {
	src := `
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbBean;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbPartitionKey;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbIgnore;

@DynamoDbBean
public class SessionEntity {
    private String sessionId;
    private transient String cachedToken;

    @DynamoDbPartitionKey
    public String getSessionId() { return sessionId; }
    public void setSessionId(String id) { this.sessionId = id; }

    @DynamoDbIgnore
    public String getCachedToken() { return cachedToken; }
    public void setCachedToken(String t) { this.cachedToken = t; }
}
`
	ents := runDynamoDB(t, ddbFI("SessionEntity.java", src))
	if !hasDDBSub(ents, "ignored_field") {
		t.Errorf("expected ignored_field subtype, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Gate: non-DynamoDB Java source yields no results
// ---------------------------------------------------------------------------

func TestDynamoDBGateNonDynamoDB(t *testing.T) {
	src := `
import javax.persistence.Entity;
import javax.persistence.Table;

@Entity
@Table(name = "products")
public class Product {
    private Long id;
    private String name;
}
`
	ents := runDynamoDB(t, ddbFI("Product.java", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-DynamoDB source, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Gate: empty content yields no results
// ---------------------------------------------------------------------------

func TestDynamoDBGateEmpty(t *testing.T) {
	ents := runDynamoDB(t, ddbFI("Empty.java", ""))
	if len(ents) != 0 {
		t.Errorf("expected no entities for empty file, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Gate: wrong language yields no results
// ---------------------------------------------------------------------------

func TestDynamoDBGateWrongLanguage(t *testing.T) {
	e, ok := extreg.Get("custom_java_dynamodb")
	if !ok {
		t.Fatal("extractor custom_java_dynamodb not registered")
	}
	file := extreg.FileInput{
		Path:     "Customer.kt",
		Language: "kotlin",
		Content:  []byte(`@DynamoDbBean class Foo`),
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected no entities for Kotlin file, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Fixture round-trip: CustomerEntity.java
// ---------------------------------------------------------------------------

func TestDynamoDBFixtureCustomerEntity(t *testing.T) {
	path := "../../../testdata/fixtures/sources/java/dynamodb/CustomerEntity.java"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	ents := runDynamoDB(t, extreg.FileInput{
		Path:     path,
		Language: "java",
		Content:  data,
	})

	// Expect @DynamoDbBean class → model entity
	if !hasDDB(ents, "SCOPE.Schema", "model", "CustomerEntity") {
		t.Errorf("expected SCOPE.Schema/model/CustomerEntity, got %v", ents)
	}
	// Expect @DynamoDbPartitionKey
	if !hasDDBSub(ents, "partition_key") {
		t.Errorf("expected partition_key subtype from fixture, got %v", ents)
	}
	// Expect @DynamoDbSortKey
	if !hasDDBSub(ents, "sort_key") {
		t.Errorf("expected sort_key subtype from fixture, got %v", ents)
	}
	// Expect @DynamoDbAttribute entries
	if !hasDDB(ents, "SCOPE.Component", "attribute", "attr:customer_name") {
		t.Errorf("expected attr:customer_name, got %v", ents)
	}
	if !hasDDB(ents, "SCOPE.Component", "attribute", "attr:customer_email") {
		t.Errorf("expected attr:customer_email, got %v", ents)
	}
	// Expect @DynamoDbIgnore
	if !hasDDBSub(ents, "ignored_field") {
		t.Errorf("expected ignored_field subtype from fixture, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// Multiple beans in one file
// ---------------------------------------------------------------------------

func TestDynamoDBMultipleBeans(t *testing.T) {
	src := `
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbBean;
import software.amazon.awssdk.enhanced.dynamodb.mapper.annotations.DynamoDbPartitionKey;

@DynamoDbBean
public class OrderEntity {
    @DynamoDbPartitionKey
    public String getOrderId() { return orderId; }
}

@DynamoDbBean
public class ShipmentEntity {
    @DynamoDbPartitionKey
    public String getShipmentId() { return shipmentId; }
}
`
	ents := runDynamoDB(t, ddbFI("Multi.java", src))
	if !hasDDB(ents, "SCOPE.Schema", "model", "OrderEntity") {
		t.Errorf("expected OrderEntity model, got %v", ents)
	}
	if !hasDDB(ents, "SCOPE.Schema", "model", "ShipmentEntity") {
		t.Errorf("expected ShipmentEntity model, got %v", ents)
	}
}
