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

// relOwnersOf returns every entity carrying a relationship of the given kind to
// toID — i.e. the FROM endpoints. relOf deliberately searches all records and
// therefore cannot see which node an edge hangs off; #6144 is entirely about
// that endpoint, so it needs its own helper.
func relOwnersOf(recs []types.EntityRecord, kind, toID string) []types.EntityRecord {
	var out []types.EntityRecord
	for i := range recs {
		for j := range recs[i].Relationships {
			r := &recs[i].Relationships[j]
			if r.Kind == kind && r.ToID == toID {
				out = append(out, recs[i])
				break
			}
		}
	}
	return out
}

// recByKindName finds an entity by (Kind, Name).
func recByKindName(recs []types.EntityRecord, kind, name string) *types.EntityRecord {
	for i := range recs {
		if recs[i].Kind == kind && recs[i].Name == name {
			return &recs[i]
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

// ---------------------------------------------------------------------------
// #6144 — the DEPENDS_ON_SERVICE **FROM** endpoint.
// ---------------------------------------------------------------------------
//
// #6123 fixed this edge's TARGET. Its SOURCE was a separate defect: the
// relationship hung off the container node, so the edge ran
// `container:PostgreSqlContainer -> service PostgreSqlContainer` — both
// endpoints derived from one token, the test nowhere in it, and the whole
// payload (image, container_type) already duplicated as properties on the
// container node. It told an MCP caller nothing grafel_inspect on that node
// would not.
//
// The edge now hangs off the ENCLOSING TEST TYPE, emitted as a merge facet with
// the identity the base csharp extractor gives that class (SourceFile, Kind
// SCOPE.Component, Name) so #6104's Tier A combines the two rather than
// creating a second node.
//
// Asserted bidirectionally on CONTENT, not counts: the class must own the edge
// AND the container node must not, because "the class owns it" alone would also
// be satisfied by emitting the edge twice.

func TestTestDoubles_ContainerServiceEdgeHangsOffTheTestType6144(t *testing.T) {
	src := `
using Testcontainers.PostgreSql;
using DotNet.Testcontainers.Builders;
using Xunit;

public class OrderIntegrationTests
{
    public OrderIntegrationTests()
    {
        var pg = new PostgreSqlContainer();
        var cache = new ContainerBuilder()
            .WithImage("redis:7")
            .Build();
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("OrderIntegrationTests.cs", "csharp", src))

	pgRef := extractor.ExternalServiceTargetID("PostgreSqlContainer")
	redisRef := extractor.ExternalServiceTargetID("redis:7")

	for _, tc := range []struct{ what, ref, container string }{
		{"typed container", pgRef, "container:PostgreSqlContainer"},
		{"image binding", redisRef, "container:redis:7"},
	} {
		owners := relOwnersOf(recs, "DEPENDS_ON_SERVICE", tc.ref)
		if len(owners) != 1 {
			t.Fatalf("%s: expected exactly 1 entity to own the DEPENDS_ON_SERVICE edge to %s, got %d (%v)",
				tc.what, tc.ref, len(owners), ownerNames(owners))
		}
		owner := owners[0]

		// FORWARD: the owner is the test type, with the identity the base
		// extractor gives it — anything else creates a second node instead of
		// merging onto the class (#6104 Tier A keys on SourceFile+Kind+Name).
		if owner.Name != "OrderIntegrationTests" {
			t.Errorf("%s: DEPENDS_ON_SERVICE hangs off %q, want the enclosing test type "+
				"%q — the edge is tautological when it hangs off the container (#6144)",
				tc.what, owner.Name, "OrderIntegrationTests")
		}
		if owner.Kind != "SCOPE.Component" || owner.Subtype != "class" {
			t.Errorf("%s: owner is %s/%s, want SCOPE.Component/class so #6104 Tier A merges it "+
				"onto the base extractor's class node instead of adding a duplicate",
				tc.what, owner.Kind, owner.Subtype)
		}
		if owner.SourceFile != "OrderIntegrationTests.cs" {
			t.Errorf("%s: owner SourceFile = %q, want the file under extraction — identity must "+
				"match the base class node exactly", tc.what, owner.SourceFile)
		}

		// REVERSE: the container node must NOT also carry it.
		if c := recByKindName(recs, "SCOPE.Pattern", tc.container); c == nil {
			t.Errorf("%s: container node %q was not emitted at all", tc.what, tc.container)
		} else {
			for _, r := range c.Relationships {
				if r.Kind == "DEPENDS_ON_SERVICE" {
					t.Errorf("%s: the container node %q still carries a DEPENDS_ON_SERVICE edge to %q — "+
						"the tautological edge was duplicated, not moved (#6144)", tc.what, tc.container, r.ToID)
				}
			}
		}

		// The edge must still name the container it came from, or moving the
		// FROM endpoint would LOSE the association the old shape encoded.
		e := relOf(recs, "DEPENDS_ON_SERVICE", tc.ref)
		if got := e.Properties.Get("container"); got != tc.container {
			t.Errorf("%s: edge property container = %q, want %q — the class-level edge must still "+
				"say which container produced it", tc.what, got, tc.container)
		}
	}

	// The container nodes keep their own payload: moving the edge must not
	// strip the properties an MCP caller reads off the node.
	if c := recByKindName(recs, "SCOPE.Pattern", "container:PostgreSqlContainer"); c == nil {
		t.Fatal("container:PostgreSqlContainer node missing")
	} else if c.Properties["container_type"] != "PostgreSqlContainer" {
		t.Errorf("container node lost its container_type property: %v", c.Properties)
	}
	if c := recByKindName(recs, "SCOPE.Pattern", "container:redis:7"); c == nil {
		t.Fatal("container:redis:7 node missing")
	} else if c.Properties["image"] != "redis:7" {
		t.Errorf("container node lost its image property: %v", c.Properties)
	}

	// Exactly ONE facet for the test type, however many containers it holds —
	// otherwise `add`'s dedup silently drops all but the first edge.
	facets := 0
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" && r.Name == "OrderIntegrationTests" {
			facets++
		}
	}
	if facets != 1 {
		t.Errorf("emitted %d SCOPE.Component/OrderIntegrationTests records, want exactly 1 "+
			"(two containers in one class must share one merge facet)", facets)
	}
}

// TestTestDoubles_ContainerEdgeFallsBackToTheContainerNode6144 pins the
// fallback: with no enclosing type there is nothing to re-point at, and the
// edge must stay on the container node rather than being dropped — #6123's
// principle that a silently dropped relationship is worse than an honest
// dangle, and unmeasurable afterwards.
func TestTestDoubles_ContainerEdgeFallsBackToTheContainerNode6144(t *testing.T) {
	// Top-level statements: no class declaration anywhere in the file.
	src := `
using Testcontainers.PostgreSql;

var pg = new PostgreSqlContainer();
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("Program.cs", "csharp", src))

	ref := extractor.ExternalServiceTargetID("PostgreSqlContainer")
	owners := relOwnersOf(recs, "DEPENDS_ON_SERVICE", ref)
	if len(owners) != 1 {
		t.Fatalf("expected the edge to survive with no enclosing type, got %d owners (%v)",
			len(owners), ownerNames(owners))
	}
	if owners[0].Name != "container:PostgreSqlContainer" {
		t.Errorf("fallback owner = %q, want the container node — with no test type to attribute "+
			"the dependency to, the pre-#6144 shape is the honest one", owners[0].Name)
	}
	// And no phantom class facet may be invented for a file that has no class.
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" {
			t.Errorf("invented a SCOPE.Component %q for a file with no type declaration", r.Name)
		}
	}
}

func ownerNames(recs []types.EntityRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Kind+"/"+r.Name)
	}
	return out
}

// TestTestDoubles_RecordStructDoesNotFabricateAComponent6144 is the regression
// test for the node-fabrication defect adversarial review found in the #6144
// enclosing-class scan.
//
// The original regex alternated `(?:class|record|struct|interface)`, and Go's
// regexp is leftmost-first over an alternation, so `record struct Row(int Id)`
// matched `record` and captured the FOLLOWING word — "struct" — as the type
// name. A positional record beside a test class is ordinary modern C#, so when
// one was the nearest preceding declaration the extractor emitted
// SCOPE.Component/class/"struct": an entity matching nothing the base extractor
// produces, surviving the #6104 merge as a standalone Tier C node, and carrying
// the DEPENDS_ON_SERVICE edge. That is precisely the fabrication
// enclosingCSharpClass's contract says never happens, and which
// TestTestDoubles_ContainerEdgeFallsBackToTheContainerNode6144 asserts against
// in the no-class case.
//
// Restricting the scan to plain `class` fixes it at the root: the record is
// skipped entirely, so the nearest preceding class is the enclosing test class —
// which is also the correct attribution. Asserted on content in both directions:
// the bogus names must be absent AND the edge must land on the real class.
func TestTestDoubles_RecordStructDoesNotFabricateAComponent6144(t *testing.T) {
	src := `
using Testcontainers.PostgreSql;
using Xunit;

public class OrderIntegrationTests
{
    public record struct Row(int Id);
    public record class Boxed(int Id);
    public readonly struct Flag { }
    public interface IMarker { }

    public OrderIntegrationTests()
    {
        var pg = new PostgreSqlContainer();
    }
}
`
	recs := extractFull(t, "custom_csharp_test_doubles", fi("OrderIntegrationTests.cs", "csharp", src))

	// No component may be named after a KEYWORD — that is the fabrication.
	for _, r := range recs {
		for _, kw := range []string{"struct", "class", "record", "interface"} {
			if r.Name == kw {
				t.Errorf("fabricated an entity named after the keyword %q (%s/%s in %s) — the "+
					"declaration scan captured a keyword as a type name", kw, r.Kind, r.Subtype, r.SourceFile)
			}
		}
	}

	// The edge must land on the enclosing test class, not on Row/Boxed/Flag/
	// IMarker (whose base Subtypes are type/type/struct/interface, so a facet
	// claiming "class" would displace them) and not on a keyword.
	ref := extractor.ExternalServiceTargetID("PostgreSqlContainer")
	owners := relOwnersOf(recs, "DEPENDS_ON_SERVICE", ref)
	if len(owners) != 1 {
		t.Fatalf("expected exactly 1 owner for the DEPENDS_ON_SERVICE edge, got %d (%v)",
			len(owners), ownerNames(owners))
	}
	if owners[0].Name != "OrderIntegrationTests" || owners[0].Kind != "SCOPE.Component" {
		t.Errorf("edge owner = %s/%s, want SCOPE.Component/OrderIntegrationTests — a record or "+
			"struct declared inside the test class must not shadow it",
			owners[0].Kind, owners[0].Name)
	}
	if owners[0].Subtype != "class" {
		t.Errorf("edge owner Subtype = %q, want \"class\"; the facet must only ever claim the "+
			"subtype the base extractor gives a class_declaration, or the #6104 Tier A merge "+
			"lets it displace a record's \"type\" or a struct's \"struct\"", owners[0].Subtype)
	}

	// Exactly one component facet, and it is the test class.
	comps := 0
	for _, r := range recs {
		if r.Kind == "SCOPE.Component" {
			comps++
			if r.Name != "OrderIntegrationTests" {
				t.Errorf("unexpected SCOPE.Component %q — this extractor may only ever emit a "+
					"facet for the enclosing test class", r.Name)
			}
		}
	}
	if comps != 1 {
		t.Errorf("emitted %d SCOPE.Component records, want exactly 1", comps)
	}
}

// TestTestDoubles_RecordStructImmediatelyBeforeContainer6144 is the narrow
// shape: the `record struct` is the LAST declaration before the container
// construction, so under the old alternation it was the nearest preceding match
// and its bogus capture — the literal word "struct" — became the emitted entity
// name. The sibling test above happens to have an interface nearer the
// container, which catches the shadowing but not the capture itself.
func TestTestDoubles_RecordStructImmediatelyBeforeContainer6144(t *testing.T) {
	src := `
using Testcontainers.PostgreSql;
using Xunit;

public class DbFixtureTests
{
    public DbFixtureTests()
    {
        var pg = new PostgreSqlContainer();
    }

    public record struct Row(int Id);
}
`
	// NOTE the container is constructed BEFORE the record here, so the nearest
	// preceding declaration is the class. The inverse ordering is covered below.
	recs := extractFull(t, "custom_csharp_test_doubles", fi("DbFixtureTests.cs", "csharp", src))
	assertContainerOwnedByClass6144(t, recs, "DbFixtureTests")

	// Now the ordering that actually triggered the defect.
	src2 := `
using Testcontainers.PostgreSql;
using Xunit;

public class DbFixtureTests
{
    public record struct Row(int Id);

    public DbFixtureTests()
    {
        var pg = new PostgreSqlContainer();
    }
}
`
	recs2 := extractFull(t, "custom_csharp_test_doubles", fi("DbFixtureTests.cs", "csharp", src2))
	assertContainerOwnedByClass6144(t, recs2, "DbFixtureTests")
}

func assertContainerOwnedByClass6144(t *testing.T, recs []types.EntityRecord, class string) {
	t.Helper()
	for _, r := range recs {
		if r.Name == "struct" || r.Name == "class" || r.Name == "record" || r.Name == "interface" {
			t.Fatalf("emitted an entity named after the keyword %q (%s/%s) — the declaration scan "+
				"captured the word after `record` as the type name (#6144)", r.Name, r.Kind, r.Subtype)
		}
	}
	owners := relOwnersOf(recs, "DEPENDS_ON_SERVICE",
		extractor.ExternalServiceTargetID("PostgreSqlContainer"))
	if len(owners) != 1 {
		t.Fatalf("expected 1 DEPENDS_ON_SERVICE owner, got %d (%v)", len(owners), ownerNames(owners))
	}
	if owners[0].Name != class || owners[0].Kind != "SCOPE.Component" || owners[0].Subtype != "class" {
		t.Errorf("edge owner = %s/%s/%s, want SCOPE.Component/class/%s",
			owners[0].Kind, owners[0].Subtype, owners[0].Name, class)
	}
}
