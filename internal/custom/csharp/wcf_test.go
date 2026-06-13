package csharp_test

// ---------------------------------------------------------------------------
// WCF — ServiceContract / OperationContract / DataContract / Transport (#4968)
// ---------------------------------------------------------------------------

import (
	"strings"
	"testing"
)

func TestWCFServiceContract(t *testing.T) {
	src := `
using System.ServiceModel;
using System.Threading.Tasks;

[ServiceContract]
public interface IOrderService
{
    [OperationContract]
    Order GetOrder(int id);

    [OperationContract]
    Task<bool> CreateOrder(Order order);
}
`
	ents := extract(t, "custom_csharp_wcf", fi("IOrderService.cs", "csharp", src))

	foundService := false
	foundGet := false
	foundCreate := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "procedure_extraction" {
			switch e.Name {
			case "service:IOrderService":
				foundService = true
			case "operation:GetOrder":
				foundGet = true
			case "operation:CreateOrder":
				foundCreate = true
			}
		}
	}
	if !foundService {
		t.Error("expected procedure_extraction service:IOrderService from [ServiceContract]")
	}
	if !foundGet {
		t.Error("expected procedure_extraction operation:GetOrder from [OperationContract]")
	}
	if !foundCreate {
		t.Error("expected procedure_extraction operation:CreateOrder from [OperationContract]")
	}
}

func TestWCFDataContract(t *testing.T) {
	src := `
using System.Runtime.Serialization;

[DataContract]
public class Order
{
    [DataMember]
    public int Id { get; set; }

    [DataMember(Order = 2)]
    public string Customer { get; set; }
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("Order.cs", "csharp", src))

	foundClass := false
	memberCount := 0
	for _, e := range ents {
		if e.Kind == "SCOPE.Schema" && e.Subtype == "schema_extraction" {
			if e.Name == "datacontract:Order" {
				foundClass = true
			}
			if e.Properties["provenance"] == "INFERRED_FROM_DATA_MEMBER" {
				memberCount++
			}
		}
	}
	if !foundClass {
		t.Error("expected schema_extraction datacontract:Order from [DataContract]")
	}
	if memberCount != 2 {
		t.Errorf("expected 2 [DataMember] schema entities, got %d", memberCount)
	}
}

func TestWCFServiceHostBinding(t *testing.T) {
	src := `
using System.ServiceModel;

class Program
{
    static void Main()
    {
        var host = new ServiceHost(typeof(OrderService));
        host.Open();
    }
}
`
	ents := extract(t, "custom_csharp_wcf", fi("Program.cs", "csharp", src))

	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Subtype == "transport_binding" &&
			e.Name == "service_host:OrderService" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected transport_binding service_host:OrderService from new ServiceHost(typeof(...))")
	}
}

func TestWCFCoreWCFRegistration(t *testing.T) {
	src := `
var builder = WebApplication.CreateBuilder(args);
builder.Services.AddServiceModelServices();
var app = builder.Build();
app.UseServiceModel(b =>
{
    b.AddService<OrderService>();
    b.AddServiceEndpoint<OrderService, IOrderService>(new BasicHttpBinding(), "/order");
});
`
	ents := extractFull(t, "custom_csharp_wcf", fi("Program.cs", "csharp", src))

	foundAddModel := false
	foundEndpoint := false
	for _, e := range ents {
		if e.Subtype == "transport_binding" {
			if e.Properties["provenance"] == "INFERRED_FROM_ADD_SERVICE_MODEL" {
				foundAddModel = true
			}
			if e.Name == "service_endpoint:OrderService:IOrderService" {
				foundEndpoint = true
			}
		}
	}
	if !foundAddModel {
		t.Error("expected transport_binding from AddServiceModelServices()")
	}
	if !foundEndpoint {
		t.Error("expected transport_binding from AddServiceEndpoint<TService, TContract>()")
	}
}

// ---------------------------------------------------------------------------
// WCF — client proxy / client_codegen (#5004)
// ---------------------------------------------------------------------------

func TestWCFChannelFactoryClientProxy(t *testing.T) {
	src := `
using System.ServiceModel;

class OrderClient
{
    public void Run()
    {
        var factory = new ChannelFactory<IOrderService>(new BasicHttpBinding(), "http://host/order");
        var channel = factory.CreateChannel();
        channel.GetOrder(1);
    }
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("OrderClient.cs", "csharp", src))

	proxy := findEntity(ents, "channel_factory:IOrderService")
	if proxy == nil {
		t.Fatal("expected client_codegen channel_factory:IOrderService from new ChannelFactory<IOrderService>()")
	}
	if proxy.Kind != "SCOPE.Component" || proxy.Subtype != "client_codegen" {
		t.Errorf("expected SCOPE.Component/client_codegen, got %s/%s", proxy.Kind, proxy.Subtype)
	}
	if proxy.Properties["contract_type"] != "IOrderService" {
		t.Errorf("expected contract_type=IOrderService, got %q", proxy.Properties["contract_type"])
	}
	foundUses := false
	for _, r := range proxy.Relationships {
		if r.Kind == "USES" && r.ToID == "contract:IOrderService" {
			foundUses = true
		}
	}
	if !foundUses {
		t.Error("expected USES edge -> contract:IOrderService from ChannelFactory proxy")
	}
}

func TestWCFClientBaseProxy(t *testing.T) {
	src := `
using System.ServiceModel;

public partial class OrderServiceClient : ClientBase<IOrderService>, IOrderService
{
    public Order GetOrder(int id) => Channel.GetOrder(id);
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("OrderServiceClient.cs", "csharp", src))

	proxy := findEntity(ents, "client_base:OrderServiceClient")
	if proxy == nil {
		t.Fatal("expected client_codegen client_base:OrderServiceClient from class : ClientBase<IOrderService>")
	}
	if proxy.Kind != "SCOPE.Component" || proxy.Subtype != "client_codegen" {
		t.Errorf("expected SCOPE.Component/client_codegen, got %s/%s", proxy.Kind, proxy.Subtype)
	}
	if proxy.Properties["contract_type"] != "IOrderService" {
		t.Errorf("expected contract_type=IOrderService, got %q", proxy.Properties["contract_type"])
	}
	foundUses := false
	for _, r := range proxy.Relationships {
		if r.Kind == "USES" && r.ToID == "contract:IOrderService" {
			foundUses = true
		}
	}
	if !foundUses {
		t.Error("expected USES edge -> contract:IOrderService from ClientBase proxy class")
	}
}

func TestWCFClientCtorAndExclusions(t *testing.T) {
	src := `
using System.ServiceModel;
using System.Net.Http;

public partial class OrderServiceClient : ClientBase<IOrderService> {}

class Caller
{
    void Run()
    {
        var proxy = new OrderServiceClient();
        var http = new HttpClient();
    }
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("Caller.cs", "csharp", src))

	if findEntity(ents, "client:OrderServiceClient") == nil {
		t.Error("expected client:OrderServiceClient from new OrderServiceClient()")
	}
	if findEntity(ents, "client:HttpClient") != nil {
		t.Error("HttpClient must be excluded from WCF client_codegen proxies")
	}
}

func TestWCFNoMatch(t *testing.T) {
	src := `namespace MyApp { class Helper { public void Run() {} } }`
	ents := extract(t, "custom_csharp_wcf", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on non-WCF source, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// WCF deepening — binding props / CreateChannel / FaultContract / behaviors (#5091)
// ---------------------------------------------------------------------------

func TestWCFBindingConfigProps(t *testing.T) {
	src := `
using System.ServiceModel;

class Setup
{
    void Run()
    {
        var binding = new NetTcpBinding();
        binding.Security.Mode = SecurityMode.Transport;
        var address = new EndpointAddress("net.tcp://host:9000/order");
        var factory = new ChannelFactory<IOrderService>(binding, address);
    }
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("Setup.cs", "csharp", src))

	var binding *struct {
		bt, addr, mode string
	}
	for _, e := range ents {
		if e.Subtype == "transport_binding" && e.Properties["binding_type"] == "NetTcpBinding" {
			binding = &struct{ bt, addr, mode string }{
				e.Properties["binding_type"], e.Properties["endpoint_address"], e.Properties["security_mode"],
			}
		}
	}
	if binding == nil {
		t.Fatalf("expected transport_binding for NetTcpBinding; ents=%v", ents)
	}
	if binding.addr != "net.tcp://host:9000/order" {
		t.Errorf("endpoint_address=%q, want net.tcp://host:9000/order", binding.addr)
	}
	if binding.mode != "Transport" {
		t.Errorf("security_mode=%q, want Transport", binding.mode)
	}
}

func TestWCFCreateChannelAttribution(t *testing.T) {
	src := `
using System.ServiceModel;

class OrderClient
{
    public void Run()
    {
        var factory = new ChannelFactory<IOrderService>(new BasicHttpBinding(), "http://host/order");
        var channel = factory.CreateChannel();
        channel.GetOrder(1);
    }
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("OrderClient.cs", "csharp", src))

	cc := findEntity(ents, "create_channel:factory:IOrderService")
	if cc == nil {
		t.Fatalf("expected create_channel:factory:IOrderService; ents=%v", ents)
	}
	if cc.Kind != "SCOPE.Component" || cc.Subtype != "client_codegen" {
		t.Errorf("expected SCOPE.Component/client_codegen, got %s/%s", cc.Kind, cc.Subtype)
	}
	if cc.Properties["contract_type"] != "IOrderService" {
		t.Errorf("contract_type=%q, want IOrderService", cc.Properties["contract_type"])
	}
	foundUses := false
	for _, r := range cc.Relationships {
		if r.Kind == "USES" && r.ToID == "contract:IOrderService" {
			foundUses = true
		}
	}
	if !foundUses {
		t.Error("expected USES edge -> contract:IOrderService from CreateChannel attribution")
	}
}

func TestWCFCreateChannelUnknownReceiverSkipped(t *testing.T) {
	// A CreateChannel() on a receiver with no in-file ChannelFactory assignment
	// must NOT be attributed (honest-partial).
	src := `
using System.ServiceModel;

class C
{
    void Run(IChannelFactory mystery)
    {
        var ch = mystery.CreateChannel();
    }
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("C.cs", "csharp", src))
	for _, e := range ents {
		if e.Properties["provenance"] == "INFERRED_FROM_CREATE_CHANNEL" {
			t.Errorf("unknown-receiver CreateChannel must not be attributed; got %s", e.Name)
		}
	}
}

func TestWCFFaultContract(t *testing.T) {
	src := `
using System.ServiceModel;

[ServiceContract]
public interface IOrderService
{
    [FaultContract(typeof(OrderFault))]
    [OperationContract]
    Order GetOrder(int id);
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("IOrderService.cs", "csharp", src))

	var fault *string
	foundUsesOp := false
	for i := range ents {
		e := ents[i]
		if e.Properties["provenance"] == "INFERRED_FROM_FAULT_CONTRACT" && e.Properties["fault_type"] == "OrderFault" {
			f := e.Properties["operation_name"]
			fault = &f
			for _, r := range e.Relationships {
				if r.Kind == "USES" && r.ToID == "operation:GetOrder" {
					foundUsesOp = true
				}
			}
		}
	}
	if fault == nil {
		t.Fatalf("expected fault_contract for OrderFault; ents=%v", ents)
	}
	if *fault != "GetOrder" {
		t.Errorf("fault operation_name=%q, want GetOrder", *fault)
	}
	if !foundUsesOp {
		t.Error("expected USES edge fault -> operation:GetOrder")
	}
}

func TestWCFServiceAndOperationBehavior(t *testing.T) {
	src := `
using System.ServiceModel;

[ServiceBehavior(InstanceContextMode = InstanceContextMode.Single, ConcurrencyMode = ConcurrencyMode.Multiple)]
public class OrderService : IOrderService
{
    [OperationBehavior(TransactionScopeRequired = true)]
    public Order GetOrder(int id) => null;
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("OrderService.cs", "csharp", src))

	foundSvc := false
	foundOp := false
	for _, e := range ents {
		if e.Properties["provenance"] == "INFERRED_FROM_SERVICE_BEHAVIOR" {
			foundSvc = true
			if e.Properties["instance_context_mode"] != "Single" {
				t.Errorf("instance_context_mode=%q, want Single", e.Properties["instance_context_mode"])
			}
			if e.Properties["concurrency_mode"] != "Multiple" {
				t.Errorf("concurrency_mode=%q, want Multiple", e.Properties["concurrency_mode"])
			}
		}
		if e.Properties["provenance"] == "INFERRED_FROM_OPERATION_BEHAVIOR" && e.Properties["operation_name"] == "GetOrder" {
			foundOp = true
		}
	}
	if !foundSvc {
		t.Error("expected transport_binding from [ServiceBehavior]")
	}
	if !foundOp {
		t.Error("expected transport_binding from [OperationBehavior] on GetOrder")
	}
}

func TestWCFPrincipalPermission(t *testing.T) {
	src := `
using System.ServiceModel;
using System.Security.Permissions;

public class OrderService
{
    [PrincipalPermission(SecurityAction.Demand, Role = "Admin")]
    public void Delete(int id) {}
}
`
	ents := extractFull(t, "custom_csharp_wcf", fi("OrderService.cs", "csharp", src))

	found := false
	for _, e := range ents {
		if e.Subtype == "auth_coverage" && e.Properties["provenance"] == "INFERRED_FROM_PRINCIPAL_PERMISSION" {
			found = true
			if !strings.Contains(e.Properties["demand"], "Admin") {
				t.Errorf("expected demand to mention Admin, got %q", e.Properties["demand"])
			}
		}
	}
	if !found {
		t.Error("expected auth_coverage from [PrincipalPermission]")
	}
}

func TestWCFDeepeningNoMatchNoOp(t *testing.T) {
	// A csharp file with none of the WCF anchors emits nothing.
	src := `namespace App { class Plain { public void Go() { var x = 1; } } }`
	ents := extractFull(t, "custom_csharp_wcf", fi("Plain.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on non-WCF source, got %d", len(ents))
	}
}

