package csharp_test

// ---------------------------------------------------------------------------
// WCF — ServiceContract / OperationContract / DataContract / Transport (#4968)
// ---------------------------------------------------------------------------

import "testing"

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

func TestWCFNoMatch(t *testing.T) {
	src := `namespace MyApp { class Helper { public void Run() {} } }`
	ents := extract(t, "custom_csharp_wcf", fi("Helper.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities on non-WCF source, got %d", len(ents))
	}
}
