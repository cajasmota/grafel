package java_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func javaTplEdge(recs []types.EntityRecord, fromName, name string) bool {
	want := extractor.TemplateTargetID(name)
	for i := range recs {
		if recs[i].Name != fromName {
			continue
		}
		for _, r := range recs[i].Relationships {
			if r.Kind == "RENDERS" && r.ToID == want {
				return true
			}
		}
	}
	return false
}

func javaTplNode(recs []types.EntityRecord, name string) int {
	want := extractor.TemplateName(name)
	c := 0
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) && recs[i].Name == want {
			c++
		}
	}
	return c
}

func javaAnyTplNode(recs []types.EntityRecord) bool {
	for i := range recs {
		if recs[i].Kind == string(types.EntityKindTemplate) {
			return true
		}
	}
	return false
}

// TestJavaTemplate_SpringMVCViewName: a @Controller @GetMapping method returning
// a String literal yields RENDERS to that view template.
func TestJavaTemplate_SpringMVCViewName(t *testing.T) {
	src := `package demo;

@Controller
public class UserController {
    @GetMapping("/users")
    public String list() {
        return "users/list";
    }
}
`
	recs := extractJavaRaw(t, src)
	if !javaTplEdge(recs, "UserController.list", "users/list") {
		t.Error("missing RENDERS(UserController.list -> users/list)")
	}
	if javaTplNode(recs, "users/list") != 1 {
		t.Error("expected one SCOPE.Template node users/list")
	}
}

// TestJavaTemplate_ModelAndView: `new ModelAndView("users/show")` yields RENDERS.
func TestJavaTemplate_ModelAndView(t *testing.T) {
	src := `package demo;

@Controller
public class UserController {
    @RequestMapping("/show")
    public ModelAndView show() {
        return new ModelAndView("users/show");
    }
}
`
	recs := extractJavaRaw(t, src)
	if !javaTplEdge(recs, "UserController.show", "users/show") {
		t.Error("missing RENDERS(UserController.show -> users/show)")
	}
}

// TestJavaTemplate_RestControllerNoView: a @RestController method returning a
// String is a REST response BODY, not a view — NO template edge (the honesty
// boundary).
func TestJavaTemplate_RestControllerNoView(t *testing.T) {
	src := `package demo;

@RestController
public class ApiController {
    @GetMapping("/ping")
    public String ping() {
        return "pong";
    }
}
`
	recs := extractJavaRaw(t, src)
	if javaTplEdge(recs, "ApiController.ping", "pong") {
		t.Error("@RestController String return must NOT be a template (REST, not MVC)")
	}
	if javaAnyTplNode(recs) {
		t.Error("@RestController must not produce any template node")
	}
}

// TestJavaTemplate_ResponseBodyNoView: a @ResponseBody method inside a
// @Controller is also REST — NO template edge.
func TestJavaTemplate_ResponseBodyNoView(t *testing.T) {
	src := `package demo;

@Controller
public class MixedController {
    @GetMapping("/data")
    @ResponseBody
    public String data() {
        return "raw-json";
    }
}
`
	recs := extractJavaRaw(t, src)
	if javaTplEdge(recs, "MixedController.data", "raw-json") {
		t.Error("@ResponseBody String return must NOT be a template")
	}
}

// TestJavaTemplate_DynamicViewDropped: a computed/variable view name is dropped.
func TestJavaTemplate_DynamicViewDropped(t *testing.T) {
	src := `package demo;

@Controller
public class DynController {
    @GetMapping("/x")
    public String x(String which) {
        return which;
    }
}
`
	recs := extractJavaRaw(t, src)
	if javaAnyTplNode(recs) {
		t.Error("dynamic (variable) view name must not produce a template node")
	}
}

// TestJavaTemplate_Convergence: two MVC handlers returning the same view → one node.
func TestJavaTemplate_Convergence(t *testing.T) {
	src := `package demo;

@Controller
public class C {
    @GetMapping("/a")
    public String a() { return "shared/page"; }

    @GetMapping("/b")
    public String b() { return "shared/page"; }
}
`
	recs := extractJavaRaw(t, src)
	if !javaTplEdge(recs, "C.a", "shared/page") || !javaTplEdge(recs, "C.b", "shared/page") {
		t.Fatal("both handlers must RENDERS shared/page")
	}
	if n := javaTplNode(recs, "shared/page"); n != 1 {
		t.Fatalf("expected ONE converged template node, got %d", n)
	}
}
