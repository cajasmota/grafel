package csharp_test

// ---------------------------------------------------------------------------
// Test-doubles — Moq / NSubstitute mock-binding, Testcontainers container
// topology, SpecFlow step definitions (#5005).
// ---------------------------------------------------------------------------

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
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
	} else if e.Properties.Get("library") != "moq" || e.Properties.Get("role") != "mock_binding" {
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
	} else if e.Properties.Get("library") != "nsubstitute" {
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

	// #6123 — the target is the CANONICAL external-service ref
	// (extractor.ExternalServiceTargetID), not the entity Name `service:<X>`.
	// A Name containing a colon is unaddressable: the resolver's splitStub cuts
	// at the first colon and probes byName with the remainder, so the old shape
	// could only dangle or bind to some unrelated entity called <X>. Asserted
	// through the helper so a change to the ref convention fails here rather
	// than silently rewiring the graph downstream.
	//
	// Typed container -> DEPENDS_ON_SERVICE scope:externalservice:PostgreSqlContainer
	if e := relOf(recs, "DEPENDS_ON_SERVICE", extractor.ExternalServiceTargetID("PostgreSqlContainer")); e == nil {
		t.Error("expected new PostgreSqlContainer() -> DEPENDS_ON_SERVICE scope:externalservice:PostgreSqlContainer")
	} else if e.Properties.Get("container_type") != "PostgreSqlContainer" {
		t.Errorf("expected container_type prop, got %v", e.Properties)
	}
	// Image binding -> DEPENDS_ON_SERVICE scope:externalservice:redis:7
	if e := relOf(recs, "DEPENDS_ON_SERVICE", extractor.ExternalServiceTargetID("redis:7")); e == nil {
		t.Error("expected .WithImage(\"redis:7\") -> DEPENDS_ON_SERVICE scope:externalservice:redis:7")
	} else if e.Properties.Get("image") != "redis:7" {
		t.Errorf("expected image=redis:7, got %v", e.Properties)
	}
	// The unaddressable old shape must be gone on BOTH containers.
	for _, stale := range []string{"service:PostgreSqlContainer", "service:redis:7"} {
		if relOf(recs, "DEPENDS_ON_SERVICE", stale) != nil {
			t.Errorf("DEPENDS_ON_SERVICE still emits the unaddressable Name ref %q (#6123)", stale)
		}
	}
	// ContainerBuilder itself must NOT emit a service edge, under either shape.
	for _, excluded := range []string{
		extractor.ExternalServiceTargetID("ContainerBuilder"),
		"service:ContainerBuilder",
	} {
		if relOf(recs, "DEPENDS_ON_SERVICE", excluded) != nil {
			t.Errorf("ContainerBuilder should be excluded from container topology (saw %q)", excluded)
		}
	}
	// No service ENTITY is fabricated for a container — the external-service
	// namespace is a corpus-wide convergence node gated on a curated SDK
	// dictionary, and a docker image is not a canonical service name (#6123).
	for _, r := range recs {
		if strings.HasPrefix(r.Name, "service:") || r.Kind == "SCOPE.ExternalService" {
			t.Errorf("test-doubles extractor fabricated a service entity: %s %s", r.Kind, r.Name)
		}
	}
}

// TestTestDoubles_ContainerServiceRefIsColonBounded6123 covers the defect that
// adversarial review found IN THE #6123 FIX ITSELF: the repaired ref shape could
// still mis-bind, on the very class of input the repair was about.
//
// ExternalServiceTargetID prefixes two colon-delimited segments, so
// `scope:externalservice:<name>` hits the resolver's six-segment structural-ref
// threshold (internal/resolve/refs.go:2037-2039) once <name> carries THREE
// colons. At six segments the stub stops being rejected and enters Format A,
// with parts[4] read as a FILE PATH and parts[5] as an entity NAME. Measured
// against a live index: `scope:externalservice:registry.io:5000/app/pg:15@sha256:deadbeef`
// resolves to an entity named "deadbeef" in a file named "15@sha256". A registry
// port + tag + digest is a legal docker reference, and reTcWithImage
// (test_doubles.go:96) captures an arbitrary unvalidated string literal.
//
// Both halves of the guard are asserted here, because they fail differently:
// the digest strip covers every LEGAL reference, and the colon ceiling covers
// input that is not a well-formed reference at all.
func TestTestDoubles_ContainerServiceRefIsColonBounded6123(t *testing.T) {
	src := `
using DotNet.Testcontainers.Builders;

public class RegistryFixture
{
    public RegistryFixture()
    {
        var pinned = new ContainerBuilder()
            .WithImage("registry.io:5000/app/pg:15@sha256:deadbeef")
            .Build();
        var junk = new ContainerBuilder()
            .WithImage("a:b:c:d")
            .Build();
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("RegistryFixture.cs", "csharp", src))

	// (1) Digest stripped: a build pin is not a service identity, and it is the
	// third colon in every legal reference. The surviving ref is five segments,
	// so lookupStructural still rejects it and the edge dangles honestly.
	const stripped = "scope:externalservice:registry.io:5000/app/pg:15"
	if e := relOf(recs, "DEPENDS_ON_SERVICE", stripped); e == nil {
		t.Errorf("expected the digest-stripped ref %q", stripped)
	} else if got := e.Properties.Get("image"); got != "registry.io:5000/app/pg:15@sha256:deadbeef" {
		// The RAW reference is still recorded on the edge/node, so stripping the
		// digest from the REF loses nothing a caller could act on.
		t.Errorf("raw image reference was lost from the edge properties: %q", got)
	}
	// The six-segment form must never be minted — this is the mis-bind.
	const hazard = "scope:externalservice:registry.io:5000/app/pg:15@sha256:deadbeef"
	if relOf(recs, "DEPENDS_ON_SERVICE", hazard) != nil {
		t.Errorf("minted the six-segment ref %q — it parses as Format A and binds to "+
			"entity parts[5] in file parts[4] (#6123 review)", hazard)
	}

	// (2) Colon ceiling: `a:b:c:d` is not a well-formed reference and survives the
	// digest strip at three colons, so NO ref can be minted for it. Refusing the
	// edge is deliberate and narrow — the alternative is a ref that parses, which
	// is the exact defect this file was repaired for.
	for _, r := range recs {
		for i := range r.Relationships {
			rel := &r.Relationships[i]
			if rel.Kind != "DEPENDS_ON_SERVICE" {
				continue
			}
			if strings.Count(rel.ToID, ":") >= 6 {
				t.Errorf("DEPENDS_ON_SERVICE ToID %q reaches the six-segment structural "+
					"threshold and can mis-bind", rel.ToID)
			}
			if strings.Contains(rel.ToID, "a:b:c:d") {
				t.Errorf("minted a ref for the malformed image `a:b:c:d`: %q", rel.ToID)
			}
		}
	}

	// NON-VACUITY: the malformed container node itself must still be emitted, so
	// "no edge" means "declined to name the service", not "dropped the container".
	found := false
	for _, r := range recs {
		if r.Name == "container:a:b:c:d" {
			found = true
		}
	}
	if !found {
		t.Error("the malformed-image container node was dropped entirely; only the " +
			"unnameable service REF should be refused")
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

// ---------------------------------------------------------------------------
// Bogus / AutoFixture test-data builders (#5071).
// ---------------------------------------------------------------------------

func TestTestDoubles_BogusFakerBuilder(t *testing.T) {
	src := `
using Bogus;
using Xunit;

public class CustomerFactoryTests
{
    [Fact]
    public void Builds()
    {
        var faker = new Faker<Customer>()
            .RuleFor(x => x.Name, f => f.Name.FullName())
            .RuleFor(x => x.Email, f => f.Internet.Email());
        var c = faker.Generate();
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("CustomerFactoryTests.cs", "csharp", src))

	if e := relOf(recs, "USES", "type:Customer"); e == nil {
		t.Error("expected new Faker<Customer>() -> USES type:Customer")
	} else if e.Properties.Get("role") != "test_data_builder" || e.Properties.Get("library") != "bogus" {
		t.Errorf("expected bogus test_data_builder props, got %v", e.Properties)
	}
	var fields string
	for _, e := range recs {
		if e.Subtype == "test_data_builder" && e.Properties["target"] == "Customer" {
			fields = e.Properties["fields"]
		}
	}
	if fields != "Name,Email" {
		t.Errorf("expected faked fields Name,Email, got %q", fields)
	}
}

func TestTestDoubles_AutoFixtureBuilder(t *testing.T) {
	src := `
using AutoFixture;
using Xunit;

public class OrderBuilderTests
{
    [Fact]
    public void Builds()
    {
        var fixture = new Fixture();
        var order = fixture.Create<Order>();
        var custom = fixture.Build<Customer>().With(c => c.Name, "Ann").Create();
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("OrderBuilderTests.cs", "csharp", src))

	if e := relOf(recs, "USES", "type:Order"); e == nil {
		t.Error("expected fixture.Create<Order>() -> USES type:Order")
	} else if e.Properties.Get("library") != "autofixture" || e.Properties.Get("role") != "test_data_builder" {
		t.Errorf("expected autofixture test_data_builder props, got %v", e.Properties)
	}
	if relOf(recs, "USES", "type:Customer") == nil {
		t.Error("expected fixture.Build<Customer>() -> USES type:Customer")
	}
}

// AutoFixture generic Create<T>/Build<T> must NOT fire without a Fixture in the
// file (avoid matching unrelated generic Create<T> / Build<T> calls).
func TestTestDoubles_AutoFixtureRequiresFixture(t *testing.T) {
	src := `
public class Factory
{
    public T Create<T>() => default;
    public void Go()
    {
        var x = Create<Order>();
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("Factory.cs", "csharp", src))
	for _, e := range recs {
		if e.Subtype == "test_data_builder" {
			t.Errorf("test_data_builder should not fire without a Fixture, got %v", e)
		}
	}
}

func TestTestDoubles_BuilderWrongLanguageNoOp(t *testing.T) {
	// A non-C# file that textually contains a Faker<T> must not extract.
	src := `const faker = new Faker<Customer>();`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("builder.ts", "typescript", src))
	if len(recs) != 0 {
		t.Errorf("expected no entities for non-csharp source, got %d", len(recs))
	}
}

// ---------------------------------------------------------------------------
// Mock-target -> DI-impl resolution (#5071).
// ---------------------------------------------------------------------------

func TestTestDoubles_MockResolvesToDIImpl_Registration(t *testing.T) {
	src := `
using Moq;
using Microsoft.Extensions.DependencyInjection;

public class SetupTests
{
    public void Configure(IServiceCollection services)
    {
        var repoMock = new Mock<IOrderRepository>();
        services.AddSingleton(repoMock.Object);
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("SetupTests.cs", "csharp", src))

	// USES edge still present.
	if relOf(recs, "USES", "type:IOrderRepository") == nil {
		t.Error("expected mock USES edge")
	}
	// RESOLVES_TO the by-name impl node the dotnet_di extractor binds.
	if e := relOf(recs, "RESOLVES_TO", "impl:OrderRepository"); e == nil {
		t.Error("expected repoMock.Object registration -> RESOLVES_TO impl:OrderRepository")
	} else if e.Properties.Get("interface") != "IOrderRepository" ||
		e.Properties.Get("role") != "mock_di_resolution" {
		t.Errorf("expected mock_di_resolution props, got %v", e.Properties)
	}
	// resolved_impl prop stamped on the mock node.
	for _, en := range recs {
		if en.Subtype == "test_double" && en.Properties["target"] == "IOrderRepository" {
			if en.Properties["resolved_impl"] != "OrderRepository" {
				t.Errorf("expected resolved_impl=OrderRepository, got %v", en.Properties)
			}
		}
	}
}

func TestTestDoubles_MockResolvesToDIImpl_SutCtor(t *testing.T) {
	src := `
using Moq;

public class HandlerTests
{
    public void Builds()
    {
        var gatewayMock = new Mock<IPaymentGateway>();
        var sut = new PaymentHandler(gatewayMock.Object);
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("HandlerTests.cs", "csharp", src))
	if relOf(recs, "RESOLVES_TO", "impl:PaymentGateway") == nil {
		t.Error("expected SUT-ctor mock.Object -> RESOLVES_TO impl:PaymentGateway")
	}
}

// A mock that is never wired into production (no .Object use) must NOT get a
// RESOLVES_TO edge — resolution is gated on actual DI/SUT wiring.
func TestTestDoubles_MockNoResolutionWithoutWiring(t *testing.T) {
	src := `
using Moq;

public class PlainMockTests
{
    public void Go()
    {
        var repo = new Mock<IOrderRepository>();
        repo.Setup(r => r.Save(It.IsAny<Order>()));
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("PlainMockTests.cs", "csharp", src))
	for _, e := range recs {
		for _, r := range e.Relationships {
			if r.Kind == "RESOLVES_TO" {
				t.Errorf("unwired mock should not RESOLVES_TO, got %v", r.ToID)
			}
		}
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
