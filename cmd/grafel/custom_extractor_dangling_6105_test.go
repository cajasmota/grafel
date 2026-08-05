package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// Issue #6105 (refs #5989) — relationship endpoints emitted by the in-process
// custom extractors (GRAFEL_INPROC_CUSTOM_EXTRACTORS) that resolve to no
// entity.
//
// WHAT THE CORPUS MEASURED. Six edge kinds were 100% dangling under the gate:
// RETURNS (1,072), OWNS (466), ACCEPTS_INPUT (85), CACHES (18), INVALIDATES (4),
// DEPENDS_ON_SERVICE (4). Grounded here, they are THREE different defects, not
// one:
//
//  1. RETURNS / ACCEPTS_INPUT / OWNS — the target entity EXISTS and the ref is
//     malformed. Every structural ref minted in internal/custom/java has the
//     shape `scope:<kind>:<subtype>:<file>:<name>` (five segments; see
//     synthesizeCarrier's doc comment, which documents that shape as the
//     package's convention). The resolver's Format A is
//     `scope:<kind>:<subtype>:<lang>:<file>:<name>` — SIX segments, split with
//     stubScopeSegments = 6 at internal/resolve/refs.go:69 and rejected
//     outright when len(parts) != 6 at refs.go:2037-2039. The language slot was
//     never emitted. Refs whose NAME contains no colon were rejected on the
//     count; refs whose name DOES contain colons yielded six parts and did
//     parse, but with every field shifted one place left (file read as the
//     language), so they could only miss. Either way nothing bound. That is the
//     fix in this change; it is applied once, centrally, in
//     patternResultToRecords.
//
//     MEASURED SCOPE OF THE REPAIR ON THIS FIXTURE (fix neutered vs. applied):
//     13 distinct unresolved `scope:` endpoints before, 4 after. Recovered:
//     RETURNS (2), ACCEPTS_INPUT (1), OWNS via an `operation` scope-kind (1),
//     and the six #4367 `bean_validation_field` CONTAINS endpoints — which
//     #6105 never named and which were also 100% dangling.
//
//  2. CACHES / INVALIDATES — the target entity also EXISTS, but the ref is a
//     colon-bearing NAME (`cache:spring:orders`, `cache:django:orders`,
//     `Datastore:redis:orders`) and no index reaches it. LookupStatusHint
//     (refs.go:1629-1646) runs splitStub, which splits on the FIRST colon and
//     then probes byName with the REMAINDER — `django:orders`, never
//     `cache:django:orders`. Any convention where an entity Name contains a
//     colon and an edge addresses it by that Name is structurally unresolvable.
//     NOT fixed here: see TestCustomExtractorColonNameRefsRemainUnresolved6105.
//
//  3. DEPENDS_ON_SERVICE — the target does not exist AND the ref shape could
//     never have addressed it. FIXED in #6123, which corrected the #6105
//     description: passes DO create `service:` entities
//     (extractor.ExternalServiceName, internal/extractor/external_service.go:82-90)
//     — what no pass creates is one for a Testcontainers image. The ref was a
//     colon-bearing NAME, i.e. defect (2)'s class, so it could only dangle or
//     mis-bind; on this fixture it mis-bound. The producer now mints the
//     canonical `scope:externalservice:<name>` ref, colon-bounded so it stays
//     below the six-segment structural form where it WOULD mis-bind, and no
//     service node is fabricated. See
//     TestCustomExtractorDependsOnServiceDoesNotMisbind6123.
//
// WHY BEHAVIOURAL. A source scan ("every ref literal contains a language
// segment") is satisfied by any string that happens to contain one, and three
// source-scanning guards written in this area recently fell to trivial mutants.
// These tests index a real fixture with the gate off and on, round-trip both
// arms through graph.LoadGraphFromDir, and assert on the endpoints that land on
// disk.
//
// WHY THE DELTA AND NOT THE ABSOLUTE. grafel deliberately retains `ext:` and
// `scope:` placeholder endpoints on the base path, so an absolute dangling
// count is meaningless. Every dangling assertion below is scoped to endpoints
// the GATE introduces.

// writeDanglingFixture6105 lays down the smallest fixture that exercises each
// edge kind the corpus measurement reported as 100% dangling under the gate:
//
//	RETURNS / ACCEPTS_INPUT — Spring @RestController request/response DTOs
//	                          (internal/custom/java/spring_request_response.go)
//	OWNS                    — Spring AOP aspect owning its pointcut and advice
//	                          (internal/custom/java/spring_aop.go)
//	CACHES / INVALIDATES    — Spring @Cacheable/@CacheEvict cache regions
//	                          (internal/custom/java/caching.go) and the Django
//	                          low-level cache API (internal/custom/python/caching.go)
//	DEPENDS_ON_SERVICE      — Testcontainers container topology
//	                          (internal/custom/csharp/test_doubles.go)
func writeDanglingFixture6105(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"java/OrdersController.java": `package api;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class OrdersController {
    @GetMapping("/orders")
    public OrderResponse list() {
        return null;
    }

    @PostMapping("/orders")
    public OrderResponse create(@RequestBody OrderRequest body) {
        return null;
    }
}
`,
		"java/AuditAspect.java": `package api;

import org.aspectj.lang.annotation.Aspect;
import org.aspectj.lang.annotation.Before;
import org.aspectj.lang.annotation.Pointcut;
import org.springframework.stereotype.Component;

@Aspect
@Component
public class AuditAspect {
    @Pointcut("execution(* api..*(..))")
    public void anyApiCall() {}

    @Before("anyApiCall()")
    public void auditBefore() {}
}
`,
		"java/Order.java": `package api;

import javax.persistence.Entity;
import javax.persistence.NamedQuery;

@Entity
@NamedQuery(name = "findAllOrders", query = "select o from Order o")
public class Order {
    private Long id;
}
`,
		"java/OrderCache.java": `package api;

import org.springframework.cache.annotation.CacheEvict;
import org.springframework.cache.annotation.Cacheable;

public class OrderCache {
    @Cacheable("orders")
    public String load(String id) {
        return id;
    }

    @CacheEvict("orders")
    public void drop(String id) {
    }
}
`,
		// Three DTOs in ONE package directory, deliberately sharing the field
		// leaf names `id` and `reference`. This is the collision
		// lookupPackageMemberByLeafName would exploit if a
		// `scope:schema:bean_validation_field:` ref ever missed byLocation — see
		// TestCustomExtractorSchemaFieldRefsStayInTheirOwnFile6105.
		"dto/OrderRequest.java": `package dto;

import javax.validation.Valid;
import javax.validation.constraints.NotNull;
import javax.validation.constraints.Size;
import org.springframework.validation.annotation.Validated;

@Validated
public class OrderRequest {
    @NotNull
    private String id;

    @Size(max = 64)
    private String reference;

    @Valid
    private Address address;
}
`,
		"dto/Customer.java": `package dto;

import javax.validation.constraints.NotNull;
import javax.validation.constraints.Size;
import org.springframework.validation.annotation.Validated;

@Validated
public class Customer {
    @NotNull
    private String id;

    @Size(max = 32)
    private String reference;
}
`,
		"dto/Address.java": `package dto;

import javax.validation.constraints.NotNull;
import org.springframework.validation.annotation.Validated;

@Validated
public class Address {
    @NotNull
    private String id;
}
`,
		"cs/OrderTests.cs": `using Testcontainers.PostgreSql;

namespace Shop.Tests
{
    public class OrderTests
    {
        public void Setup()
        {
            var db = new PostgreSqlContainer();
        }
    }
}
`,
		"myapp/cacheviews.py": `
from django.core.cache import cache


def read_order(order_id):
    return cache.get("orders")


def drop_order(order_id):
    cache.delete("orders")
`,
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

// danglingEndpoint6105 is one unresolved relationship endpoint: the edge kind,
// which side dangles, and the raw endpoint string left on the edge.
type danglingEndpoint6105 struct {
	kind string
	side string
	id   string
}

func (d danglingEndpoint6105) String() string {
	return fmt.Sprintf("%s %s %s", d.kind, d.side, d.id)
}

// danglingEndpoints6105 returns every relationship endpoint in doc that names no
// entity in doc, as a multiset keyed by (kind, side, endpoint).
//
// BIDIRECTIONAL BY CONSTRUCTION (#6037): both FromID and ToID are checked, so an
// edge that is present but points at a name rather than an ID is visible
// whichever side it is on.
func danglingEndpoints6105(doc *graph.Document) map[danglingEndpoint6105]int {
	ids := make(map[string]struct{}, len(doc.Entities))
	for i := range doc.Entities {
		ids[doc.Entities[i].ID] = struct{}{}
	}
	out := map[danglingEndpoint6105]int{}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if _, ok := ids[r.FromID]; !ok {
			out[danglingEndpoint6105{r.Kind, "FROM", r.FromID}]++
		}
		if _, ok := ids[r.ToID]; !ok {
			out[danglingEndpoint6105{r.Kind, "TO", r.ToID}]++
		}
	}
	return out
}

// gateArms6105 indexes the fixture with the gate off and on and returns the
// dangling endpoints the gate ADDS (present, or more frequent, with the gate on)
// alongside the gate-on graph.
func gateArms6105(t *testing.T) (added map[danglingEndpoint6105]int, on *graph.Document) {
	t.Helper()
	fixture := writeDanglingFixture6105(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	offDoc := persistAndReload(t, runIndexerOn(t, fixture, "dangling6105", nil))
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	on = persistAndReload(t, runIndexerOn(t, fixture, "dangling6105", nil))

	off := danglingEndpoints6105(offDoc)
	all := danglingEndpoints6105(on)
	added = map[danglingEndpoint6105]int{}
	for k, n := range all {
		if d := n - off[k]; d > 0 {
			added[k] = d
		}
	}
	return added, on
}

// edgeSet6105 renders every relationship of the given kinds as
// "KIND: <fromKind>:<fromName> -> <toKind>:<toName>", with an unresolvable
// endpoint rendered as "DANGLING(<raw endpoint>)".
//
// CONTENT, NOT COUNTS. The assertions below name the exact entities each edge
// must join, so an edge that resolves to the WRONG node fails just as loudly as
// one that resolves to nothing — the failure mode a count-based or
// "dangling == 0" check is blind to.
func edgeSet6105(doc *graph.Document, kinds ...string) map[string]int {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	byID := make(map[string]string, len(doc.Entities))
	for i := range doc.Entities {
		e := &doc.Entities[i]
		byID[e.ID] = e.Kind + ":" + e.Name
	}
	show := func(id string) string {
		if s, ok := byID[id]; ok {
			return s
		}
		return "DANGLING(" + id + ")"
	}
	out := map[string]int{}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if !want[r.Kind] {
			continue
		}
		out[fmt.Sprintf("%s: %s -> %s", r.Kind, show(r.FromID), show(r.ToID))]++
	}
	return out
}

// TestCustomExtractorJavaStructuralRefEdgesBindToRealEntities6105 is the
// headline gate for defect (1). Each Java custom-extractor edge whose kind the
// corpus reported as 100% dangling must join the two entities it names.
//
// The DTO / pointcut / advice targets are all emitted by the same extractor run
// that emits the edge, in the same file — so this is purely a question of
// whether the ref the edge carries is addressable, and nothing here depends on
// another pass materialising a node.
func TestCustomExtractorJavaStructuralRefEdgesBindToRealEntities6105(t *testing.T) {
	_, on := gateArms6105(t)
	got := edgeSet6105(on, "RETURNS", "ACCEPTS_INPUT", "OWNS", "CONTAINS")

	want := []string{
		// Spring request/response DTOs (spring_request_response.go:130,150).
		"ACCEPTS_INPUT: SCOPE.Operation:OrdersController.create -> SCOPE.Schema:OrderRequest",
		"RETURNS: SCOPE.Operation:OrdersController.create -> SCOPE.Schema:OrderResponse",
		"RETURNS: SCOPE.Operation:OrdersController.list -> SCOPE.Schema:OrderResponse",
		// JPA entity owning its named query (hibernate.go:242). This is the
		// majority OWNS shape: an `operation` scope-kind, which
		// structuralKindFamilies maps onto the Operation family, so it resolves
		// even though the base extractor indexes the same file.
		"OWNS: SCOPE.Schema:Order -> SCOPE.Operation:Order.findAllOrders",
		// #4367 DTO field membership (bean_validation.go:316,338). Not one of the
		// six kinds #6105 named, but measured as 100% dangling on this fixture
		// before the fix (six `CONTAINS TO
		// scope:schema:bean_validation_field:…` endpoints) and resolving after
		// it — the largest single group the repair recovers here.
		//
		// Asserted per-field, and per-OWNER, because `id` and `reference`
		// deliberately exist in three classes in one package directory: a field
		// bound to the wrong class fails here rather than passing a count.
		//
		// The SCOPE.Class owner form appears only for OrderRequest: the #4367
		// edge's FromID is `Class:<Owner>` and only OrderRequest has a
		// SCOPE.Class node (synthesised as the nested-@Valid carrier,
		// bean_validation.go:356). Customer and Address have no SCOPE.Class
		// node, so their membership lands on the SCOPE.Component the Java AST
		// extractor emitted for the class. Both forms are the same repair — the
		// ToID resolving — so both are pinned.
		//
		// Until #6138 those last three named the FILE component instead
		// (`SCOPE.Component:dto/Customer.java`), because foldFileComponent-
		// Duplicates deleted the stem-named class component and re-pointed its
		// edges onto the file. A field belongs to its class, not to the file the
		// class happens to live in, so the owner asserted here is the class.
		"CONTAINS: SCOPE.Class:OrderRequest -> SCOPE.Schema:OrderRequest.id",
		"CONTAINS: SCOPE.Class:OrderRequest -> SCOPE.Schema:OrderRequest.reference",
		"CONTAINS: SCOPE.Class:OrderRequest -> SCOPE.Schema:OrderRequest.address",
		"CONTAINS: SCOPE.Component:Customer -> SCOPE.Schema:Customer.id",
		"CONTAINS: SCOPE.Component:Customer -> SCOPE.Schema:Customer.reference",
		"CONTAINS: SCOPE.Component:Address -> SCOPE.Schema:Address.id",
	}
	var missing []string
	for _, w := range want {
		if got[w] == 0 {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		have := make([]string, 0, len(got))
		for e, n := range got {
			have = append(have, fmt.Sprintf("%dx %s", n, e))
		}
		sort.Strings(have)
		t.Errorf("%d of %d Java custom-extractor edges do not join the entities they name:\nmissing:\n  %s\nactual:\n  %s",
			len(missing), len(want), strings.Join(missing, "\n  "), strings.Join(have, "\n  "))
	}
}

// TestCustomExtractorGateStructuralRefResidueIsRecorded6105 is the general form
// of the same defect, and the ratchet that stops the fix being special-cased to
// the edges named above: EVERY unresolved `scope:` endpoint the gate introduces
// must be recorded here by its exact ref, with a reason. A new five-segment ref
// escaping canonicalStructuralRef shows up as an unrecorded entry.
//
// WHY `scope:pattern:` SURVIVES THE FIX — AND WHY IT IS NOT FIXED HERE. With the
// segment count repaired, lookupStructural reaches Format A and calls
// structuralKindFamilies(scopeKind) (internal/resolve/refs.go:2380-2390), which
// knows only "component", "operation" and "schema". A `pattern` scope-kind gets
// a nil family, so the kind-aware tier is skipped and the lookup falls through
// to the unique-only byLocation index. Every AOP pointcut / advice node shares
// its (file, Name) with the base extractor's SCOPE.Operation for the same Java
// method by construction — `AuditAspect.anyApiCall` is both — so the pair is
// flagged in ambigLocation and the endpoint is left as statusAmbiguous.
//
// Fixing it means teaching structuralKindFamilies a Pattern family, which is in
// internal/resolve — under concurrent change by another work item, so out of
// scope for this one. It is a genuine second defect, not a consequence of this
// change: at 2f0175dfc these endpoints were unresolved too, for the earlier and
// more basic reason that the ref never parsed at all.
//
// Scoped to the gate DELTA: `scope:` placeholders retained on the base path are
// deliberate and are not this test's business.
func TestCustomExtractorGateStructuralRefResidueIsRecorded6105(t *testing.T) {
	added, _ := gateArms6105(t)
	got := map[string]int{}
	for d, n := range added {
		if strings.HasPrefix(d.id, "scope:") {
			got[d.String()] = n
		}
	}
	want := map[string]int{
		// `pattern` scope-kind: no family in structuralKindFamilies, and the
		// (file, Name) pair collides with the base extractor's SCOPE.Operation
		// for the same Java method. See the doc comment above.
		"OWNS TO scope:pattern:advice:java:java/AuditAspect.java:AuditAspect.auditBefore":        1,
		"OWNS TO scope:pattern:pointcut:java:java/AuditAspect.java:AuditAspect.anyApiCall":       1,
		"REFERENCES TO scope:pattern:pointcut:java:java/AuditAspect.java:AuditAspect.anyApiCall": 1,
		// #6123 — the Testcontainers container-topology edge. Now minted as the
		// canonical external-service ref (extractor.ExternalServiceTargetID)
		// instead of the unaddressable Name `service:PostgreSqlContainer`. It is
		// three segments, so lookupStructural rejects it at refs.go:2037-2039 and
		// returns statusUnmatched WITHOUT falling through to byName — which is the
		// point: the previous ref bound to the base extractor's SCOPE.Operation for
		// the `new PostgreSqlContainer()` call site. This entry is an INTENDED
		// dangle, not a defect: no pass creates a service node for a docker image,
		// and fabricating one would poison the corpus-wide external-service
		// convergence namespace. See TestCustomExtractorDependsOnServiceDoesNotMisbind6123.
		"DEPENDS_ON_SERVICE TO scope:externalservice:PostgreSqlContainer": 1,
		// findRefForType (types.go:120) fallback: `Address` is declared in
		// dto/Address.java, but the ref is minted with the REFERENCING file's
		// path, so byLocation[dto/OrderRequest.java]["Address"] can only miss.
		// `dependency` also has no family in structuralKindFamilies, so the
		// kind-aware tier is skipped too. Unresolved before this change as well
		// — then on the segment count, now on an honest cross-file miss. The
		// resolver declining to guess a type by bare name is correct; fixing it
		// properly means emitting the DECLARING file's path, which is a Java
		// extractor change out of scope here.
		"VALIDATES TO scope:dependency:bean_validation:java:dto/OrderRequest.java:Address": 1,
	}
	for k, n := range got {
		if want[k] != n {
			t.Errorf("unrecorded unresolved structural-ref endpoint %q (%d).\n"+
				"If this is a NEW five-segment ref, canonicalStructuralRef is not reaching it; "+
				"if it is a `scope:pattern:` one, add it here with its reason.", k, n)
		}
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("recorded structural-ref residue %q resolved (got %d, want %d) — "+
				"the resolver has presumably learned a Pattern/Dependency family, or the "+
				"emitting extractor now mints a resolvable ref; drop this entry.",
				k, got[k], n)
		}
	}
}

// TestCustomExtractorColonNameRefsRemainUnresolved6105 pins defect (2) as KNOWN
// AND UNFIXED rather than letting it rot unrecorded.
//
// #6123 UPDATE: defect (3) — DEPENDS_ON_SERVICE — used to be covered here too.
// It was the same colon-name class (an entity Name containing a colon can never
// be addressed by that Name, because splitStub cuts at the first colon), but it
// is now fixed at its producer: the ref is the canonical
// `scope:externalservice:<name>` and its residue is recorded in
// TestCustomExtractorGateStructuralRefResidueIsRecorded6105 instead. The kind
// switch below no longer collects it.
//
// WHY IT IS NOT FIXED HERE. It needs a resolver-side index — either byName
// keyed on the FULL colon-bearing stub, or Properties["ref"] indexed under
// byQualifiedName the way `scope:endpoint:` / `scope:testcoverage:` /
// `scope:component:interface:<lang>:` already are (internal/resolve/refs.go:992,
// :1006, :1029, :1045). internal/resolve is being changed concurrently by
// another work item, so this change stays out of it. It cannot be fixed from
// the extractor side without harm: setting the cache-region entity's
// QualifiedName to the ref would make it resolvable, but cache regions converge
// ACROSS files by design and duplicate QualifiedNames are blanked to a collision
// sentinel — the same region cached in two files would resolve to nothing again,
// only now "ambiguous" instead of "unmatched".
//
// Deleting the edges is not an option either: an edge pointing at nothing is
// bad, silently dropping a relationship the extractor meant to express is
// worse and unmeasurable.
//
// The test asserts the residue EXACTLY. If a future change resolves any of
// these, this fails and must be deleted — it is a ratchet, not an endorsement.
func TestCustomExtractorColonNameRefsRemainUnresolved6105(t *testing.T) {
	added, _ := gateArms6105(t)
	got := map[string]int{}
	for d, n := range added {
		switch d.kind {
		case "CACHES", "INVALIDATES", "READS_FROM", "WRITES_TO":
			got[d.String()] = n
		}
	}
	want := map[string]int{
		// internal/custom/java/caching.go:96 — entity Name is "orders",
		// Properties["ref"] is the stub; nothing indexes the stub.
		"CACHES TO cache:spring:orders":      1,
		"INVALIDATES TO cache:spring:orders": 1,
		// internal/custom/python/caching.go:118 — entity NAME is the stub, but
		// splitStub eats everything up to the first colon before probing byName.
		"CACHES TO cache:django:orders":      1,
		"INVALIDATES TO cache:django:orders": 1,
		// internal/custom/python/redis.go:176 — same colon-name shape.
		"READS_FROM TO Datastore:redis:orders": 1,
		"WRITES_TO TO Datastore:redis:orders":  1,
	}
	if len(got) != len(want) {
		t.Errorf("known-unresolved residue changed size: got %d, want %d\ngot:  %v\nwant: %v",
			len(got), len(want), got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("known-unresolved endpoint %q: got %d, want %d", k, got[k], n)
		}
	}
	for k, n := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("NEW unresolved colon-name endpoint %q (%d) — not in the recorded residue", k, n)
		}
	}
}

// TestCustomExtractorSchemaFieldRefsStayInTheirOwnFile6105 measures the WIDENING
// this fix introduces, which no dangling-based assertion can see.
//
// THE HAZARD. Making these refs parse also makes them eligible for
// lookupStructural tiers they could never reach before. The sharpest is the #667
// Java cross-file field tier (internal/resolve/refs.go:2250): predicate
// `scopeKind == "schema" && lang == "java" && tail contains a dot`. On a
// byLocation MISS it falls to lookupPackageMemberByLeafName(pkgDir, fieldName),
// which binds to any class in the package DIRECTORY declaring that leaf name,
// then to lookupUniqueSchemaFieldByName globally. For field names like `id` or
// `name` "unique in package dir" is a weak guarantee. Before the fix these were
// statusUnmatched; after it they could bind to an unrelated class's field — and
// a mis-bind is RESOLVED, so the residue ratchets above are blind to it by
// construction, exactly as they are for DEPENDS_ON_SERVICE.
//
// WHAT THE MEASUREMENT FOUND (re-run against c38c05f20, which landed #6098 and
// widened precisely these tiers). The tier is reachable in SHAPE but is not
// reached by any live emission, for two independent reasons:
//
//  1. It requires a DOTTED tail. Of the sixteen `scope:schema:` mint sites in
//     internal/custom/java, only spring_dto_fields.go:188 and
//     bean_validation.go:316 produce one (`<OwnerClass>.<field>`); every other
//     site interpolates a bare class name, so `LastIndexByte(tail, '.') > 0` is
//     false and the tier is never entered.
//  2. Both dotted sites emit the target entity in the SAME FILE and the same
//     extractor run as the ref, so byLocation always hits at refs.go:2091 —
//     before the schema fallbacks at :2225 are consulted.
//
// So the conclusion is "not reached", not "acceptable heuristic". This test is
// what keeps that true: the fixture puts three DTOs in one package directory
// sharing the leaf names `id` and `reference`, so if a future change ever lets
// one of these refs miss byLocation, the leaf-name tier has something wrong to
// bind to and this fails loudly instead of silently rewiring the graph.
func TestCustomExtractorSchemaFieldRefsStayInTheirOwnFile6105(t *testing.T) {
	_, on := gateArms6105(t)

	byID := make(map[string]*graph.Entity, len(on.Entities))
	for i := range on.Entities {
		byID[on.Entities[i].ID] = &on.Entities[i]
	}

	// Collect the bean-validation field entities and check each one is anchored
	// to the file its own ref names, and owned by the class its name declares.
	fields := map[string]*graph.Entity{}
	leafOwners := map[string]map[string]bool{} // leaf field name -> set of files
	for i := range on.Entities {
		e := &on.Entities[i]
		ref := e.PropGet("ref")
		if !strings.HasPrefix(ref, "scope:schema:bean_validation_field:java:") {
			continue
		}
		fields[e.ID] = e
		rest := strings.TrimPrefix(ref, "scope:schema:bean_validation_field:java:")
		colon := strings.IndexByte(rest, ':')
		if colon <= 0 {
			t.Errorf("field ref %q has no file segment", ref)
			continue
		}
		refFile, refTail := rest[:colon], rest[colon+1:]
		if e.SourceFile != refFile {
			t.Errorf("field entity %s lives in %s but its ref names %s",
				e.Name, e.SourceFile, refFile)
		}
		if e.Name != refTail {
			t.Errorf("field entity Name %q disagrees with its ref tail %q", e.Name, refTail)
		}
		if owner := e.PropGet("owner_class"); owner != "" &&
			!strings.HasPrefix(e.Name, owner+".") {
			t.Errorf("field entity %s claims owner_class=%s — the membership was rewired",
				e.Name, owner)
		}
		if dot := strings.LastIndexByte(e.Name, '.'); dot > 0 {
			leaf := e.Name[dot+1:]
			if leafOwners[leaf] == nil {
				leafOwners[leaf] = map[string]bool{}
			}
			leafOwners[leaf][e.SourceFile] = true
		}
	}

	// Non-vacuity: the fixture must actually present the leaf-name collision the
	// widened tier would exploit, in more than one file.
	collisions := 0
	for _, files := range leafOwners {
		if len(files) > 1 {
			collisions++
		}
	}
	if len(fields) < 6 || collisions < 2 {
		t.Fatalf("fixture no longer exercises the widened tier: %d bean-validation field "+
			"entities, %d field leaf names shared across files (want >= 6 and >= 2)",
			len(fields), collisions)
	}

	// Every membership edge touching a field entity must stay inside one file.
	// REFERENCES is excluded: the @Valid nested-DTO edge (bean_validation.go:356)
	// legitimately crosses files, and it addresses its target as `Class:<Name>`,
	// not as a schema structural ref, so it is not on the widened path.
	for i := range on.Relationships {
		r := &on.Relationships[i]
		if r.Kind == "REFERENCES" {
			continue
		}
		from, to := byID[r.FromID], byID[r.ToID]
		if from == nil || to == nil {
			continue
		}
		if fields[r.FromID] == nil && fields[r.ToID] == nil {
			continue
		}
		if from.SourceFile == "" || to.SourceFile == "" || from.SourceFile == to.SourceFile {
			continue
		}
		t.Errorf("%s edge crosses files onto a bean-validation field: %s (%s) -> %s (%s).\n"+
			"This is the #667 leaf-name tier (refs.go:2250) firing — a byLocation miss now "+
			"binds by field leaf name across the package directory.",
			r.Kind, from.Name, from.SourceFile, to.Name, to.SourceFile)
	}
}

// TestCustomExtractorDependsOnServiceDoesNotMisbind6123 replaces the #6105
// ratchet that PINNED the mis-binding. Issue #6123 is the fix.
//
// WHAT WAS ACTUALLY WRONG (#6105 defect (3) restated after grounding). The
// issue text says "no pass anywhere creates a `service:` entity". That premise
// is false: internal/extractor/external_service.go:82-90 (ExternalServiceName)
// mints entities whose Name is exactly `service:<svc>` — `service:stripe`,
// `service:aws-s3` — and they are the canonical DEPENDS_ON_SERVICE target
// (internal/types/kinds.go:1239). What no pass creates is a service node for a
// Testcontainers image/type.
//
// The defect is therefore the SAME CLASS as #6105 defect (2), the colon-name
// refs: an entity whose Name contains a colon can never be addressed by that
// Name. LookupStatusHint runs splitStub (internal/resolve/refs.go:2658), which
// cuts at the FIRST colon and probes byName with the REMAINDER — so a
// `service:X` ref probes byName["X"], never byName["service:X"]. Real service
// nodes are reached by QualifiedName (`scope:externalservice:<svc>`,
// ExternalServiceTargetID at external_service.go:102), not by Name. So
// `service:X` could never bind to a service node even where one existed, and
// on a leaf-name collision it binds to whatever else is called X — here the
// SCOPE.Operation the base C# extractor made for the `new PostgreSqlContainer()`
// call site, reached through the DEPENDS_ON_SERVICE operation-family hint
// (refs.go:1782).
//
// THE FIX. test_doubles.go now mints the canonical target ref rather than the
// unaddressable Name. Where a matching service node exists the edge binds to it
// via the byQualifiedName tier; where none exists — the Testcontainers case,
// always — the ref is `scope:`-prefixed and therefore handled by
// lookupStructural, which rejects any stub that is not six segments
// (refs.go:2037-2039) and returns statusUnmatched WITHOUT falling through to
// byName. The edge dangles honestly.
//
// "CANNOT MIS-BIND" IS CONDITIONAL, and an earlier revision of this comment
// stated it unconditionally, which was false. The guarantee holds only while the
// stub stays under six segments; a legal docker reference with a registry port,
// a tag AND a digest reaches six and is parsed as Format A. The producer bounds
// the colon count (containerServiceRef) and the boundary itself is pinned in
// internal/resolve.TestExternalServiceRefBindsOrDanglesWhenColonSafe6123.
//
// NOT FIXED, DELIBERATELY: no service entity is fabricated. Note that
// normalising the name would not have removed the dangle anyway —
// `PostgreSqlContainer` -> `postgres` still has no node, because the curated
// dictionary (external_service.go:52-79) is Stripe/Twilio/AWS/SaaS SDKs and
// contains no databases at all. The real choice was fabricate-or-dangle. See the
// argument in internal/custom/csharp/test_doubles.go's emitContainer comment.
//
// A dangling-count check cannot see any of this: the count IMPROVES when the
// edge silently mis-binds and WORSENS under this fix. That is why every
// assertion here is on content.
//
// #6144 EXTENDED THIS TEST'S REACH to the edge's SOURCE endpoint, which #6123
// deliberately left alone — see the note on the (c) expectation below.
func TestCustomExtractorDependsOnServiceDoesNotMisbind6123(t *testing.T) {
	_, on := gateArms6105(t)

	// (a) No service entity is fabricated for the container.
	for i := range on.Entities {
		e := &on.Entities[i]
		if strings.HasPrefix(e.Name, "service:") {
			t.Errorf("a `service:` entity now exists (%s %s in %s) — the fix must not "+
				"fabricate a service node for a Testcontainers image", e.Kind, e.Name, e.SourceFile)
		}
	}

	// (b) NON-VACUITY. The collision the old code fell into must still be
	// present in the graph, or this test could not detect a mis-bind at all.
	collision := false
	for i := range on.Entities {
		if on.Entities[i].Kind == "SCOPE.Operation" && on.Entities[i].Name == "PostgreSqlContainer" {
			collision = true
			break
		}
	}
	if !collision {
		t.Fatalf("fixture no longer contains the SCOPE.Operation:PostgreSqlContainer " +
			"collision target — a mis-bind would now be undetectable here")
	}

	// (c) The edge exists, joins THE TEST to the canonical external-service ref,
	// and binds to NOTHING (honest dangle).
	//
	// #6144 — THE FROM ENDPOINT MOVED, AND THIS IS THE END-TO-END PROOF. This
	// expectation used to be `SCOPE.Pattern:container:PostgreSqlContainer -> …`,
	// i.e. the edge hung off the container node, so it ran
	// container:PostgreSqlContainer -> service PostgreSqlContainer: both
	// endpoints derived from one token, with the test — the thing test_doubles.go
	// has always documented as the dependent — nowhere in it. That assertion was
	// pinning the tautology.
	//
	// The extractor attaches the edge to the enclosing test type (OrderTests) as
	// a #6104 Tier A merge facet, which merges onto the base C# extractor's
	// SCOPE.Component:OrderTests. Before #6138, foldFileComponentDuplicates then
	// folded that class component into the FILE component for cs/OrderTests.cs
	// unconditionally, so the edge was repointed one hop further than the merge
	// left it (`edges_repointed` in its log line) and the surviving shape was
	// file-granular. #6138 gated that fold on the file being the same entity as
	// its declaration — true for a React/Vue/Svelte module, false for a C# file,
	// which is a container holding a class, not a second name for it — so a C#
	// class no longer folds into its file and the edge stays on the class
	// component the merge actually produced. Still the test, still
	// non-tautological, just class-granular now.
	//
	// WHAT THIS TEST DOES NOT PROVE. An earlier revision of this comment claimed
	// the expectation also demonstrates the Tier A merge, on the grounds that an
	// unmerged duplicate would show up as an extra edge. That is false HERE: the
	// fixture's class (OrderTests) has the same name as its file stem
	// (OrderTests.cs), so a merged facet and an unmerged duplicate both fold into
	// the SAME file component and the edge set is byte-identical either way. The
	// merge is real, but it is pinned where it can actually be observed —
	// TestTestDoubles_ContainerServiceEdgeHangsOffTheTestType6144 asserts the
	// facet's identity (SourceFile, Kind, Name, Subtype) directly at the
	// extractor boundary, before any folding pass can hide a duplicate. What
	// THIS test proves is the end-to-end one: the edge no longer originates at
	// the container node.
	got := edgeSet6105(on, "DEPENDS_ON_SERVICE")
	want := map[string]int{
		"DEPENDS_ON_SERVICE: SCOPE.Component:OrderTests -> " +
			"DANGLING(scope:externalservice:PostgreSqlContainer)": 1,
	}
	if len(got) != len(want) {
		t.Errorf("DEPENDS_ON_SERVICE edge set changed size: got %v, want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("DEPENDS_ON_SERVICE %q: got %d, want %d (full set %v)", k, got[k], n, got)
		}
	}
	for k, n := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected DEPENDS_ON_SERVICE edge %q (%d) — full set %v", k, n, got)
		}
	}
}
