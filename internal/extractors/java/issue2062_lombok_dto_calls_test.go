package java_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
	"github.com/cajasmota/grafel/internal/resolve"
	"github.com/cajasmota/grafel/internal/types"
)

// TestIssue2062_ObjectCreationEmitsConstructorTarget verifies that
// `new ClassName(args)` emits a CALLS edge whose ToID is
// "ClassName.ClassName" (the qualified constructor form), not the bare
// "ClassName" (which would bind to the class entity).
//
// Pre-fix: object_creation_expression returned the rightmost
// type_identifier verbatim ("ClassName"), so the CALLS edge bound to
// the class component instead of the constructor entity. Every
// Lombok-synthesized constructor (Name = "ClassName.ClassName") sat
// orphaned because no inbound CALLS ever pointed at it. Post-fix the
// extractor emits the qualified form so the resolver's byName /
// byMember indexes route the edge to the constructor.
func TestIssue2062_ObjectCreationEmitsConstructorTarget(t *testing.T) {
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	src := `package com.example.dto;

import lombok.Data;

@Data
public class ProductDTO {
    private Long id;
    private String name;
}

class Caller {
    public ProductDTO build() {
        return new ProductDTO(1L, "x");
    }
}
`
	out, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "com/example/dto/ProductDTO.java",
		Content:  []byte(src),
		Language: "java",
		TSTree:   parseForTest(t, src),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	var caller *types.EntityRecord
	for i := range out {
		if out[i].Name == "Caller.build" {
			caller = &out[i]
			break
		}
	}
	if caller == nil {
		t.Fatal("Caller.build method entity not found")
	}
	var found bool
	for _, r := range caller.Relationships {
		if r.Kind == "CALLS" && r.ToID == "ProductDTO.ProductDTO" {
			found = true
			break
		}
	}
	if !found {
		var got []string
		for _, r := range caller.Relationships {
			got = append(got, r.Kind+":"+r.ToID)
		}
		t.Fatalf("expected CALLS->ProductDTO.ProductDTO (qualified constructor form); got %v", got)
	}
}

// TestIssue2062_LombokDTOGetterCallResolves — regression for #2062.
//
// A Lombok @Data DTO synthesizes a getter entity ProductDTO.getId. A
// controller in another file declares `ProductDTO dto;` as a field and
// calls `dto.getId()`. The Java extractor emits a CALLS edge whose ToID
// is the dotted bare-string "ProductDTO.getId". After running the
// resolver, that edge must be rewritten to the synthesized method's
// entity ID instead of being left as a bare-string orphan (which then
// classifies as bug-resolver / ext:* stub on disposition).
//
// Pre-fix: byName[ProductDTO.getId] was indeed populated by the synth
// pipeline, BUT splitStub("ProductDTO.getId") returned kind="" only
// because the string contains no ':'. The kind-agnostic byName lookup
// fires on the full string. The latent bug surfaced when the resolver
// fell into rewriteOneWithCaller and the (callerFile, callerPkgDir)
// were empty — for embedded CALLS edges emitted from extractor pass-1,
// the FromID is set by buildDocument at apply time but the caller
// context is NOT threaded into the resolver. Combined with the
// `splitStub` interpretation of "ProductDTO.getId" producing kind=""
// + name=full-string, the test exists to document the contract.
func TestIssue2062_LombokDTOGetterCallResolves(t *testing.T) {
	ext, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}

	dtoSrc := `package com.example.dto;

import lombok.Data;

@Data
public class ProductDTO {
    private Long id;
    private String name;
}
`
	controllerSrc := `package com.example.api;

import com.example.dto.ProductDTO;

public class ProductController {
    private ProductDTO dto;

    public Long fetchId() {
        return dto.getId();
    }

    public String fetchName() {
        return dto.getName();
    }
}
`

	dtoOut, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "com/example/dto/ProductDTO.java",
		Content:  []byte(dtoSrc),
		Language: "java",
		TSTree:   parseForTest(t, dtoSrc),
	})
	if err != nil {
		t.Fatalf("Extract(dto): %v", err)
	}
	ctlOut, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "com/example/api/ProductController.java",
		Content:  []byte(controllerSrc),
		Language: "java",
		TSTree:   parseForTest(t, controllerSrc),
	})
	if err != nil {
		t.Fatalf("Extract(controller): %v", err)
	}

	all := append([]types.EntityRecord{}, dtoOut...)
	all = append(all, ctlOut...)

	// Assign deterministic IDs (real pipeline does this).
	for i := range all {
		if all[i].ID == "" {
			all[i].ID = all[i].ComputeID()
		}
	}

	// Find the synthesized ProductDTO.getId method ID.
	var getIDEntityID string
	for _, e := range all {
		if e.Name == "ProductDTO.getId" && e.Kind == "SCOPE.Operation" {
			getIDEntityID = e.ID
			break
		}
	}
	if getIDEntityID == "" {
		t.Fatal("expected synthesized ProductDTO.getId entity to be emitted by Lombok @Data synthesizer")
	}

	// Locate the caller method (fetchId) and confirm it carries a
	// CALLS relationship with ToID "ProductDTO.getId".
	var fetchID *types.EntityRecord
	for i := range all {
		if all[i].Name == "ProductController.fetchId" {
			fetchID = &all[i]
			break
		}
	}
	if fetchID == nil {
		t.Fatal("expected ProductController.fetchId method entity")
	}
	var foundCall bool
	for _, r := range fetchID.Relationships {
		if r.Kind == "CALLS" && r.ToID == "ProductDTO.getId" {
			foundCall = true
			break
		}
	}
	if !foundCall {
		var got []string
		for _, r := range fetchID.Relationships {
			got = append(got, r.Kind+":"+r.ToID)
		}
		t.Fatalf("ProductController.fetchId should emit CALLS->ProductDTO.getId; got %v", got)
	}

	// Build the index over ALL entities (DTO synthesized methods + controller).
	idx := resolve.BuildIndex(all)

	// Run the resolver on the embedded relationships — this mirrors the
	// real pipeline (ReferencesEmbedded). After resolution, the CALLS
	// edge to "ProductDTO.getId" must be rewritten to the synthesized
	// method's entity ID, not left as a bare-string stub (which would
	// flow to bug-resolver / classify as ext:).
	stats := resolve.ReferencesEmbedded(all, idx)
	_ = stats

	for i := range all {
		if all[i].Name != "ProductController.fetchId" {
			continue
		}
		for _, r := range all[i].Relationships {
			if r.Kind != "CALLS" {
				continue
			}
			// Specifically check the getId call.
			if r.ToID == "ProductDTO.getId" {
				t.Fatalf("ProductController.fetchId CALLS->ProductDTO.getId left unresolved (bare-string stub); expected rewrite to %s", getIDEntityID)
			}
			if r.ToID == getIDEntityID {
				return // success
			}
		}
	}
	t.Fatal("ProductController.fetchId CALLS edges did not include a rewrite to the synthesized ProductDTO.getId entity")
}
