package mcp

// mro_crosslang_3839_test.go — value-asserting coverage for cross-language
// inheritance / member-promotion resolution (#3839, epic #3829 MRO T7).
//
// These generalise the DRF MRO work (T1–T6) to the LANGUAGE level. They assert
// the promoted/inherited member resolves to its DEFINING BODY (not len>0):
//
//  1. Go struct embedding — `type Service struct { *BaseService }` and a
//     bodyless `Service.Process` stub resolves (method PROMOTION) to
//     BaseService.Process's real body via the EXTENDS(embedded_struct) walk,
//     CROSS-FILE (base lives in base.go). neighbors(out) reaches the base's
//     real callee.
//  2. Java interface default methods — `class Impl implements Iface` where the
//     interface declares `default void foo(){...}` (a real body): impl.foo
//     resolves to Iface.foo via the IMPLEMENTS walk, cross-file.
//  3. EXTENDS base_name resolves CROSS-FILE: a subclass in one file inherits a
//     base member whose body is in another file; the dotted base_name on the
//     EXTENDS edge drives the walk to the right defining class.
//  4. NEGATIVE: a Java class implementing an EXTERNAL interface with no indexed
//     body and no pack entry resolves honest-unresolved — never fabricated.

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// goEmbeddingDoc models `type Service struct { *BaseService }` where
// BaseService.Process has a real body in a DIFFERENT file (base.go) and calls a
// helper. Service.Process is a bodyless promoted stub the child never declares.
func goEmbeddingDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "base", Name: "BaseService", QualifiedName: "BaseService",
				Kind: "SCOPE.Component", Subtype: "struct", SourceFile: "base.go",
				StartLine: 1, EndLine: 6, Language: "go"},
			{ID: "base_process", Name: "BaseService.Process", QualifiedName: "BaseService.Process",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "base.go",
				StartLine: 3, EndLine: 6, Language: "go"},
			{ID: "base_helper", Name: "BaseService.validate", QualifiedName: "BaseService.validate",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "base.go",
				StartLine: 8, EndLine: 9, Language: "go"},
			// The embedding struct, in a different file.
			{ID: "svc", Name: "Service", QualifiedName: "Service",
				Kind: "SCOPE.Component", Subtype: "struct", SourceFile: "service.go",
				StartLine: 1, EndLine: 3, Language: "go"},
			// Bodyless PROMOTED method stub on Service (no body, no CALLS edges).
			{ID: "svc_process", Name: "Service.Process", QualifiedName: "Service.Process",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "service.go",
				StartLine: 0, EndLine: 0, Language: "go",
				Signature: "func (s *Service) Process()"},
		},
		Relationships: []graph.Relationship{
			// Go embedding -> EXTENDS(kind=embedded_struct) with base_name.
			graph.Relationship{ID: "e1", FromID: "svc", ToID: "base", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "go", "kind": "embedded_struct", "base_name": "BaseService"}),
			// The base method calls validate — the real promoted-through edge.
			graph.Relationship{ID: "e2", FromID: "base_process", ToID: "base_helper", Kind: "CALLS"}.WithProperties(map[string]string{"language": "go"}),
		},
	}
}

// TestGoEmbedding_PromotedMethod_ResolvesToBaseBody — #3839 case 1 (inspect).
// Service.Process is promoted from the embedded *BaseService; inspect must
// resolve it to BaseService.Process (the defining body), cross-file.
func TestGoEmbedding_PromotedMethod_ResolvesToBaseBody(t *testing.T) {
	srv := newTestServer(t, goEmbeddingDoc())
	out := callInspect(t, srv, "svc_process")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section for promoted Go method, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != true {
		t.Fatalf("expected resolved=true for Go method promotion, got %v (note=%v)", inh["resolved"], inh["note"])
	}
	if dc, _ := inh["defining_class"].(string); !strings.Contains(dc, "BaseService") {
		t.Errorf("expected defining_class BaseService, got %q", dc)
	}
	if rf, _ := inh["resolved_from"].(string); rf != "extends_in_repo" {
		t.Errorf("expected resolved_from extends_in_repo (in-repo embedded base), got %q", rf)
	}
	// Must point at the BASE file, proving cross-file resolution.
	if df, _ := inh["defining_file"].(string); df != "base.go" {
		t.Errorf("expected defining_file base.go (cross-file promotion), got %q", df)
	}
}

// TestGoEmbedding_PromotedMethod_TraversalReachesBaseCallee — #3839 case 1
// (neighbors). The promoted stub has no CALLS edges; neighbors(out) must hop
// via INHERITS to BaseService.Process and surface its real callee
// BaseService.validate.
func TestGoEmbedding_PromotedMethod_TraversalReachesBaseCallee(t *testing.T) {
	srv := newTestServer(t, goEmbeddingDoc())
	out := callNeighbors3834(t, srv, "svc_process", "out")
	names := calleeNames3834(t, out)
	if !containsString(names, "BaseService.Process") {
		t.Fatalf("expected promoted stub to reach defining BaseService.Process via INHERITS, got: %v", names)
	}
	if !containsString(names, "BaseService.validate") {
		t.Errorf("expected to reach base callee BaseService.validate THROUGH the promoted body, got: %v", names)
	}
}

// javaInterfaceDefaultDoc models `class Impl implements Iface` where Iface
// declares a `default void foo()` with a real body in Iface.java (a DIFFERENT
// file). Impl.foo is a bodyless inherited stub.
func javaInterfaceDefaultDoc() *graph.Document {
	return &graph.Document{
		Entities: []graph.Entity{
			{ID: "iface", Name: "Iface", QualifiedName: "Iface",
				Kind: "SCOPE.Component", Subtype: "interface", SourceFile: "Iface.java",
				StartLine: 1, EndLine: 6, Language: "java"},
			// The interface DEFAULT method — has a real body.
			{ID: "iface_foo", Name: "Iface.foo", QualifiedName: "Iface.foo",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "Iface.java",
				StartLine: 2, EndLine: 5, Language: "java"},
			{ID: "iface_log", Name: "Iface.log", QualifiedName: "Iface.log",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "Iface.java",
				StartLine: 7, EndLine: 8, Language: "java"},
			{ID: "impl", Name: "Impl", QualifiedName: "Impl",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "Impl.java",
				StartLine: 1, EndLine: 3, Language: "java"},
			// Bodyless inherited stub — Impl does not redeclare foo.
			{ID: "impl_foo", Name: "Impl.foo", QualifiedName: "Impl.foo",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "Impl.java",
				StartLine: 0, EndLine: 0, Language: "java",
				Signature: "void foo()"},
		},
		Relationships: []graph.Relationship{
			// class Impl implements Iface -> IMPLEMENTS, carrying base_name.
			graph.Relationship{ID: "e1", FromID: "impl", ToID: "iface", Kind: "IMPLEMENTS"}.WithProperties(map[string]string{"language": "java", "base_name": "Iface"}),
			// The default method calls log — the real edge to follow through.
			graph.Relationship{ID: "e2", FromID: "iface_foo", ToID: "iface_log", Kind: "CALLS"}.WithProperties(map[string]string{"language": "java"}),
		},
	}
}

// TestJavaInterfaceDefault_ResolvesToInterfaceBody — #3839 case 2 (inspect).
// impl.foo is inherited from the interface DEFAULT method; inspect must resolve
// it to Iface.foo via the IMPLEMENTS walk, cross-file.
func TestJavaInterfaceDefault_ResolvesToInterfaceBody(t *testing.T) {
	srv := newTestServer(t, javaInterfaceDefaultDoc())
	out := callInspect(t, srv, "impl_foo")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section for interface-default method, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != true {
		t.Fatalf("expected resolved=true for interface default method, got %v (note=%v)", inh["resolved"], inh["note"])
	}
	if dc, _ := inh["defining_class"].(string); !strings.Contains(dc, "Iface") {
		t.Errorf("expected defining_class Iface, got %q", dc)
	}
	if df, _ := inh["defining_file"].(string); df != "Iface.java" {
		t.Errorf("expected defining_file Iface.java (default method in interface file), got %q", df)
	}
}

// TestJavaInterfaceDefault_TraversalReachesInterfaceCallee — #3839 case 2
// (neighbors). The inherited stub hops via INHERITS to Iface.foo and surfaces
// its real callee Iface.log.
func TestJavaInterfaceDefault_TraversalReachesInterfaceCallee(t *testing.T) {
	srv := newTestServer(t, javaInterfaceDefaultDoc())
	out := callNeighbors3834(t, srv, "impl_foo", "out")
	names := calleeNames3834(t, out)
	if !containsString(names, "Iface.foo") {
		t.Fatalf("expected inherited stub to reach defining Iface.foo via INHERITS, got: %v", names)
	}
	if !containsString(names, "Iface.log") {
		t.Errorf("expected to reach interface callee Iface.log THROUGH the default body, got: %v", names)
	}
}

// TestExtendsBaseName_CrossFileResolution — #3839 case 3. A subclass inherits a
// base member whose body lives in another file; the dotted base_name on the
// EXTENDS edge drives the walk to the defining class cross-file.
func TestExtendsBaseName_CrossFileResolution(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "base", Name: "Repository", QualifiedName: "app.data.Repository",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "data/repository.py",
				StartLine: 1, EndLine: 4, Language: "python"},
			{ID: "base_save", Name: "Repository.save", QualifiedName: "app.data.Repository.save",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "data/repository.py",
				StartLine: 2, EndLine: 4, Language: "python"},
			{ID: "child", Name: "UserRepository", QualifiedName: "app.users.UserRepository",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "users/repo.py",
				StartLine: 1, EndLine: 2, Language: "python"},
			{ID: "child_save", Name: "UserRepository.save", QualifiedName: "app.users.UserRepository.save",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "users/repo.py",
				StartLine: 0, EndLine: 0, Language: "python",
				Signature: "def save(self, obj)"},
		},
		Relationships: []graph.Relationship{
			// base_name is the dotted FQN — must drive cross-file resolution.
			graph.Relationship{ID: "e1", FromID: "child", ToID: "base", Kind: "EXTENDS"}.WithProperties(map[string]string{"language": "python", "base_name": "app.data.Repository"}),
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "child_save")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != true {
		t.Fatalf("expected cross-file EXTENDS resolution, got resolved=%v note=%v", inh["resolved"], inh["note"])
	}
	if df, _ := inh["defining_file"].(string); df != "data/repository.py" {
		t.Errorf("expected defining_file data/repository.py (cross-file), got %q", df)
	}
}

// TestJavaInterfaceDefault_ExternalAbstract_HonestUnresolved — #3839 case 4
// (negative). A class implementing an EXTERNAL interface with no indexed body
// and no pack entry must surface honest-unresolved — no fabricated body.
func TestJavaInterfaceDefault_ExternalAbstract_HonestUnresolved(t *testing.T) {
	doc := &graph.Document{
		Entities: []graph.Entity{
			{ID: "impl", Name: "Handler", QualifiedName: "Handler",
				Kind: "SCOPE.Component", Subtype: "class", SourceFile: "Handler.java",
				StartLine: 1, EndLine: 3, Language: "java"},
			// Inherited stub whose interface is external/abstract (no indexed body).
			{ID: "impl_run", Name: "Handler.run", QualifiedName: "Handler.run",
				Kind: "SCOPE.Operation", Subtype: "method", SourceFile: "Handler.java",
				StartLine: 0, EndLine: 0, Language: "java", Signature: "void run()"},
			{ID: "ext", Name: "Runnable", Kind: "SCOPE.External", Language: "java"},
		},
		Relationships: []graph.Relationship{
			graph.Relationship{ID: "e1", FromID: "impl", ToID: "ext", Kind: "IMPLEMENTS"}.WithProperties(map[string]string{"language": "java", "base_name": "java.lang.Runnable"}),
		},
	}
	srv := newTestServer(t, doc)
	out := callInspect(t, srv, "impl_run")
	inh, ok := out["inheritance"].(map[string]any)
	if !ok {
		t.Fatalf("expected inheritance section for unresolved member, got keys: %v", mapKeys(out))
	}
	if inh["resolved"] != false {
		t.Errorf("expected resolved=false for external abstract interface, got %v", inh["resolved"])
	}
	if _, hasDC := inh["defining_class"]; hasDC {
		t.Errorf("unresolved member must NOT carry a defining_class, got %v", inh["defining_class"])
	}
	if note, _ := inh["note"].(string); note == "" {
		t.Errorf("expected an honest-unresolved note, got empty")
	}
	// No synthetic INHERITS edge for the unresolved member.
	if edges := mroOutboundEdges(srv.State.groups["test"].Repos["repo1"], "impl_run"); len(edges) != 0 {
		t.Errorf("expected no synthetic INHERITS edge for unresolved member, got: %+v", edges)
	}
}
