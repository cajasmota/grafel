package csharp_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// AutoMapper / Mapster object-mapping topology coverage (#5074). Value-
// asserting: source→dest MAPS_TO edges must be recovered (resolvable to the
// real DTO/entity classes via Class:<T>), member-map counts captured, and the
// Mapster registrations + honest-partial inline projection surfaced.

const objectMappingSrc = `
using AutoMapper;
using Mapster;

public class UserProfile : Profile
{
    public UserProfile()
    {
        CreateMap<User, UserDto>()
            .ForMember(d => d.FullName, o => o.MapFrom(s => s.Name))
            .ForMember(d => d.Email, o => o.MapFrom(s => s.EmailAddress));
        CreateMap<Order, OrderDto>().ReverseMap();
    }
}

public class MapsterConfig
{
    public static void Register(TypeAdapterConfig config)
    {
        TypeAdapterConfig<Product, ProductDto>.NewConfig();
        config.NewConfig<Category, CategoryDto>();
    }

    public CustomerDto Map(Customer c) => c.Adapt<CustomerDto>();
}
`

func mapEdgeOf(ent *types.EntityRecord, kind, fromID, toID string) *types.RelationshipRecord {
	for i := range ent.Relationships {
		r := &ent.Relationships[i]
		if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
			return r
		}
	}
	return nil
}

func TestObjectMappingAutoMapperMapster(t *testing.T) {
	ents := extractFull(t, "custom_csharp_object_mapping",
		fi("Mapping.cs", "csharp", objectMappingSrc))

	// --- AutoMapper Profile recovered ---
	prof := findBy(ents, "mapping_profile", "UserProfile")
	if prof == nil {
		t.Fatal("expected mapping_profile 'UserProfile'")
	}
	if prof.Properties["framework"] != "automapper" {
		t.Errorf("profile framework = %q, want automapper", prof.Properties["framework"])
	}

	// --- CreateMap<User, UserDto> with two .ForMember member maps ---
	um := findBy(ents, "object_mapping", "User -> UserDto")
	if um == nil {
		t.Fatal("expected object_mapping 'User -> UserDto'")
	}
	if um.Properties["member_map_count"] != "2" {
		t.Errorf("User->UserDto member_map_count = %q, want 2", um.Properties["member_map_count"])
	}
	if um.Properties["profile"] != "UserProfile" {
		t.Errorf("User->UserDto profile = %q, want UserProfile", um.Properties["profile"])
	}
	if r := mapEdgeOf(um, "MAPS_TO", "Class:User", "Class:UserDto"); r == nil {
		t.Error("expected MAPS_TO Class:User -> Class:UserDto")
	} else if r.Properties.Get("framework") != "automapper" {
		t.Errorf("MAPS_TO framework = %q, want automapper", r.Properties.Get("framework"))
	}

	// --- CreateMap<Order, OrderDto>().ReverseMap() — both directions ---
	om := findBy(ents, "object_mapping", "Order -> OrderDto")
	if om == nil {
		t.Fatal("expected object_mapping 'Order -> OrderDto'")
	}
	if om.Properties["reverse"] != "true" {
		t.Errorf("Order->OrderDto reverse = %q, want true", om.Properties["reverse"])
	}
	if mapEdgeOf(om, "MAPS_TO", "Class:Order", "Class:OrderDto") == nil {
		t.Error("expected forward MAPS_TO Class:Order -> Class:OrderDto")
	}
	if mapEdgeOf(om, "MAPS_TO", "Class:OrderDto", "Class:Order") == nil {
		t.Error("expected reverse MAPS_TO Class:OrderDto -> Class:Order (.ReverseMap)")
	}

	// --- Mapster TypeAdapterConfig<Product, ProductDto> ---
	pm := findBy(ents, "object_mapping", "Product -> ProductDto")
	if pm == nil {
		t.Fatal("expected object_mapping 'Product -> ProductDto'")
	}
	if pm.Properties["framework"] != "mapster" {
		t.Errorf("Product->ProductDto framework = %q, want mapster", pm.Properties["framework"])
	}
	if pm.Properties["registration"] != "type_adapter_config" {
		t.Errorf("Product->ProductDto registration = %q, want type_adapter_config", pm.Properties["registration"])
	}
	if mapEdgeOf(pm, "MAPS_TO", "Class:Product", "Class:ProductDto") == nil {
		t.Error("expected MAPS_TO Class:Product -> Class:ProductDto")
	}

	// --- Mapster config.NewConfig<Category, CategoryDto>() ---
	cm := findBy(ents, "object_mapping", "Category -> CategoryDto")
	if cm == nil {
		t.Fatal("expected object_mapping 'Category -> CategoryDto'")
	}
	if cm.Properties["registration"] != "new_config" {
		t.Errorf("Category->CategoryDto registration = %q, want new_config", cm.Properties["registration"])
	}
	if mapEdgeOf(cm, "MAPS_TO", "Class:Category", "Class:CategoryDto") == nil {
		t.Error("expected MAPS_TO Class:Category -> Class:CategoryDto")
	}

	// --- Mapster inline `.Adapt<CustomerDto>()` projection (honest-partial) ---
	am := findBy(ents, "object_mapping", "Adapt -> CustomerDto")
	if am == nil {
		t.Fatal("expected object_mapping 'Adapt -> CustomerDto'")
	}
	if am.Properties["dynamic_source"] != "true" {
		t.Errorf("Adapt projection dynamic_source = %q, want true", am.Properties["dynamic_source"])
	}
	// honest-partial: no fabricated source edge
	if len(am.Relationships) != 0 {
		t.Errorf("Adapt projection should carry no MAPS_TO edge, got %d", len(am.Relationships))
	}
}

// Non-mapping C# must produce nothing (pre-filter guard).
func TestObjectMappingIgnoresUnrelated(t *testing.T) {
	src := `
public class PlainService
{
    public int Add(int a, int b) => a + b;
}
`
	ents := extractFull(t, "custom_csharp_object_mapping", fi("Plain.cs", "csharp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities from non-mapping source, got %d", len(ents))
	}
}
