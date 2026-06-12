package csharp_test

// ---------------------------------------------------------------------------
// Test-doubles — Moq / NSubstitute mock-binding, Testcontainers container
// topology, SpecFlow step definitions (#5005).
// ---------------------------------------------------------------------------

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// relOf returns the first relationship of the given kind whose ToID matches.
func relOf(recs []types.EntityRecord, kind, toID string) *types.RelationshipRecord {
	for i := range recs {
		for j := range recs[i].Relationships {
			r := &recs[i].Relationships[j]
			if r.Kind == kind && r.ToID == toID {
				return r
			}
		}
	}
	return nil
}

func TestTestDoubles_MoqMockBinding(t *testing.T) {
	src := `
using Moq;
using Xunit;

public class OrderServiceTests
{
    [Fact]
    public void PlacesOrder()
    {
        var repo = new Mock<IOrderRepository>();
        var clock = new Mock<Acme.Time.IClock>();
        var loose = Mock.Of<IMailer>();
        repo.Setup(r => r.Save(It.IsAny<Order>()));
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("OrderServiceTests.cs", "csharp", src))

	// mock node + USES edge for IOrderRepository
	if e := relOf(recs, "USES", "type:IOrderRepository"); e == nil {
		t.Error("expected Mock<IOrderRepository> -> USES type:IOrderRepository")
	} else if e.Properties["library"] != "moq" || e.Properties["role"] != "mock_binding" {
		t.Errorf("expected moq mock_binding props, got %v", e.Properties)
	}
	// dotted type leaf-normalised
	if relOf(recs, "USES", "type:IClock") == nil {
		t.Error("expected new Mock<Acme.Time.IClock> leaf-normalised to type:IClock")
	}
	// Mock.Of form
	if relOf(recs, "USES", "type:IMailer") == nil {
		t.Error("expected Mock.Of<IMailer> -> USES type:IMailer")
	}
}

func TestTestDoubles_NSubstituteMockBinding(t *testing.T) {
	src := `
using NSubstitute;
using Xunit;

public class PaymentTests
{
    [Fact]
    public void Charges()
    {
        var gateway = Substitute.For<IPaymentGateway>();
        gateway.Charge(100).Returns(true);
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("PaymentTests.cs", "csharp", src))
	if e := relOf(recs, "USES", "type:IPaymentGateway"); e == nil {
		t.Error("expected Substitute.For<IPaymentGateway> -> USES type:IPaymentGateway")
	} else if e.Properties["library"] != "nsubstitute" {
		t.Errorf("expected nsubstitute library, got %v", e.Properties)
	}
}

func TestTestDoubles_TestcontainersTopology(t *testing.T) {
	src := `
using Testcontainers.PostgreSql;
using DotNet.Testcontainers.Builders;

public class DbFixture
{
    public DbFixture()
    {
        var pg = new PostgreSqlContainer();
        var redis = new ContainerBuilder()
            .WithImage("redis:7")
            .Build();
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("DbFixture.cs", "csharp", src))

	// Typed container -> DEPENDS_ON_SERVICE service:PostgreSqlContainer
	if e := relOf(recs, "DEPENDS_ON_SERVICE", "service:PostgreSqlContainer"); e == nil {
		t.Error("expected new PostgreSqlContainer() -> DEPENDS_ON_SERVICE service:PostgreSqlContainer")
	} else if e.Properties["container_type"] != "PostgreSqlContainer" {
		t.Errorf("expected container_type prop, got %v", e.Properties)
	}
	// Image binding -> DEPENDS_ON_SERVICE service:redis:7
	if e := relOf(recs, "DEPENDS_ON_SERVICE", "service:redis:7"); e == nil {
		t.Error("expected .WithImage(\"redis:7\") -> DEPENDS_ON_SERVICE service:redis:7")
	} else if e.Properties["image"] != "redis:7" {
		t.Errorf("expected image=redis:7, got %v", e.Properties)
	}
	// ContainerBuilder itself must NOT emit a service node.
	if relOf(recs, "DEPENDS_ON_SERVICE", "service:ContainerBuilder") != nil {
		t.Error("ContainerBuilder should be excluded from container topology")
	}
}

func TestTestDoubles_SpecFlowStepDefinitions(t *testing.T) {
	src := `
using TechTalk.SpecFlow;

[Binding]
public class CheckoutSteps
{
    [Given(@"I have (\d+) items in my cart")]
    public void GivenItemsInCart(int count) { }

    [When("I place the order")]
    public void WhenIPlaceTheOrder() { }

    [Then("the order is confirmed")]
    public void ThenConfirmed() { }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("CheckoutSteps.cs", "csharp", src))

	kinds := map[string]string{}
	for _, e := range recs {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "step_definition" {
			kinds[e.Properties["keyword"]] = e.Properties["step_text"]
		}
	}
	if kinds["Given"] == "" {
		t.Error("expected a Given step_definition")
	}
	if kinds["When"] != "I place the order" {
		t.Errorf("expected When step text, got %q", kinds["When"])
	}
	if kinds["Then"] != "the order is confirmed" {
		t.Errorf("expected Then step text, got %q", kinds["Then"])
	}
}

func TestTestDoubles_NoFalsePositiveOnPlainSource(t *testing.T) {
	src := `
public class Order
{
    public int Id { get; set; }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("Order.cs", "csharp", src))
	if len(recs) != 0 {
		t.Errorf("expected no test-double entities for plain source, got %d", len(recs))
	}
}

// Step definitions must NOT fire outside a [Binding] class (avoid matching
// stray [Given] in non-SpecFlow code / comments).
func TestTestDoubles_StepRequiresBinding(t *testing.T) {
	src := `
public class NotSteps
{
    [Then("this should be ignored")]
    public void Whatever() { }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("NotSteps.cs", "csharp", src))
	for _, e := range recs {
		if e.Subtype == "step_definition" {
			t.Error("step_definition should not fire without [Binding]")
		}
	}
}
