package java_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/java"
)

// Issue #4367 LIVE-REPRO — JPA/Hibernate + Bean Validation half.
//
// Runs the ACTUAL registered custom_java_patterns extractor (which dispatches
// ExtractHibernate + ExtractBeanValidation) + the ACTUAL resolve.BuildIndex
// symbol table over faithful JPA entity + Bean Validation DTO sources, and
// asserts that:
//   - @Column / @ManyToOne / @OneToMany / @Embedded entity fields are CLASS
//     MEMBERS (CONTAINS, source resolving to the owning @Entity class) — not
//     orphans;
//   - relation fields carry a REFERENCES edge to the target entity type (the
//     generic element type for collection relations, List<Item> -> Item) that
//     RESOLVES in the symbol table;
//   - @Valid nested DTO fields carry a REFERENCES edge to the nested DTO.
//
// Pre-fix: bean-validation `<Owner>.<field>` nodes had no CONTAINS membership,
// and Hibernate emitted no per-field entities at all (only class->class
// DEPENDS_ON), so every JPA column/relation field was a structural orphan.

const jpaOrderSrc = `package com.example;

import javax.persistence.*;
import java.util.List;

@Entity
@Table(name = "orders")
public class Order {
	@Id
	private Long id;

	@Column(name = "status", nullable = false)
	private String status;

	@ManyToOne
	private Customer customer;

	@OneToMany(mappedBy = "order")
	private List<Item> items;

	@Embedded
	private Address shippingAddress;
}
`

const jpaCustomerSrc = `package com.example;

import javax.persistence.*;

@Entity
public class Customer {
	@Id
	private Long id;

	@Column
	private String name;
}
`

const jpaItemSrc = `package com.example;

import javax.persistence.*;

@Entity
public class Item {
	@Id
	private Long id;
}
`

const jpaAddressSrc = `package com.example;

import javax.persistence.*;

@Embeddable
public class Address {
	@Column
	private String city;
}
`

const bvDtoSrc = `package com.example;

import javax.validation.Valid;
import javax.validation.constraints.NotNull;
import javax.validation.constraints.Size;

public class CreateOrderRequest {
	@NotNull
	@Size(min = 1, max = 64)
	private String status;

	@Valid
	@NotNull
	private AddressDto address;
}
`

func javaExtract4367(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	e, ok := extreg.Get("custom_java_patterns")
	if !ok {
		t.Fatal("custom_java_patterns not registered")
	}
	ents, err := e.Extract(context.Background(),
		extreg.FileInput{Path: path, Language: "java", Content: []byte(src)})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	return ents
}

func collectEdges4367(ents []types.EntityRecord) (contains int, refTargets map[string]int) {
	refTargets = map[string]int{}
	for _, e := range ents {
		for _, r := range e.Relationships {
			switch r.Kind {
			case string(types.RelationshipKindContains):
				if r.Properties["member"] == "field" {
					contains++
				}
			case string(types.RelationshipKindReferences):
				if r.Properties["ref_kind"] == "field_target_type" {
					refTargets[r.ToID]++
				}
			}
		}
	}
	return
}

func TestIssue4367_JPA_BeanValidation_FieldMembership_AndTargets(t *testing.T) {
	var all []types.EntityRecord
	all = append(all, javaExtract4367(t, "src/Order.java", jpaOrderSrc)...)
	all = append(all, javaExtract4367(t, "src/Customer.java", jpaCustomerSrc)...)
	all = append(all, javaExtract4367(t, "src/Item.java", jpaItemSrc)...)
	all = append(all, javaExtract4367(t, "src/Address.java", jpaAddressSrc)...)
	all = append(all, javaExtract4367(t, "src/CreateOrderRequest.java", bvDtoSrc)...)

	contains, refTargets := collectEdges4367(all)

	if contains == 0 {
		t.Fatalf("JVM field CONTAINS membership = 0 (orphan fields); want > 0 (#4367)")
	}
	t.Logf("JVM: CONTAINS field edges=%d, REFERENCES targets=%v", contains, refTargets)

	// JPA relation targets (collection element unwrapped) + @Embedded + @Valid.
	for _, want := range []string{"Class:Customer", "Class:Item", "Class:Address", "Class:AddressDto"} {
		if refTargets[want] == 0 {
			t.Errorf("expected REFERENCES target %q not emitted (#4367); got %v", want, refTargets)
		}
	}

	// The custom extractor itself emits a @Entity SCOPE.Schema node named after
	// each entity (Order/Customer/Item), and an @Embeddable Address node; these
	// are the resolution targets for the `Class:<Name>` CONTAINS/REFERENCES
	// stubs via the byName convention. The Bean Validation DTO class
	// (CreateOrderRequest) and the nested AddressDto are NOT emitted as standalone
	// nodes by the custom extractor (they come from the base tree-sitter Java
	// extractor in the real pipeline), so add faithful base class nodes for those
	// to exercise the @Valid REFERENCES / DTO-field CONTAINS resolution.
	for _, cls := range []string{"CreateOrderRequest", "AddressDto"} {
		c := types.EntityRecord{
			Name: cls, Kind: "SCOPE.Component", Subtype: "class",
			SourceFile: "src/" + cls + ".java", Language: "java",
			Properties: map[string]string{"kind": "SCOPE.Component", "subtype": "class"},
		}
		c.ID = c.ComputeID()
		all = append(all, c)
	}

	idx := resolve.BuildIndex(all)
	for _, target := range []string{"Class:Customer", "Class:Item", "Class:Order", "Class:CreateOrderRequest", "Class:AddressDto"} {
		if _, ok := idx.Lookup(target); !ok {
			t.Errorf("symbol table did NOT resolve %q — field/relation stays orphan (#4367)", target)
		}
	}
}
