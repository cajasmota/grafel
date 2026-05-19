package engine

import (
	"sort"
	"strings"
	"testing"
)

// helper: build a reader from a path->src map.
func mapReader(m map[string]string) JavaAnnotationFileReader {
	return func(p string) []byte {
		s, ok := m[p]
		if !ok {
			return nil
		}
		return []byte(s)
	}
}

func endpointIDs(records []recordLike) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.ID)
	}
	sort.Strings(out)
	return out
}

type recordLike struct {
	ID         string
	Props      map[string]string
	SourceFile string
}

func collect(t *testing.T, files map[string]string) []recordLike {
	t.Helper()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	got := ApplyJavaAnnotationRoutes(paths, mapReader(files))
	out := make([]recordLike, 0, len(got))
	for _, e := range got {
		out = append(out, recordLike{ID: e.ID, Props: e.Properties, SourceFile: e.SourceFile})
	}
	return out
}

func TestApplyJavaAnnotationRoutes_JAXRSClassPlusMethodPath(t *testing.T) {
	src := `package com.example;
import jakarta.ws.rs.*;

@Path("/products")
public class ProductsController {
    @GET
    public Object list() { return null; }

    @GET
    @Path("/{id}")
    public Object get() { return null; }

    @POST
    @Path("/upload")
    public Object upload() { return null; }
}
`
	got := collect(t, map[string]string{"Products.java": src})
	ids := endpointIDs(got)
	want := []string{
		"http:GET:/products",
		"http:GET:/products/{id}",
		"http:POST:/products/upload",
	}
	sort.Strings(want)
	if strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Fatalf("ids = %v\nwant %v", ids, want)
	}
	for _, r := range got {
		if r.Props["framework"] != "jaxrs" {
			t.Errorf("expected framework=jaxrs, got %q for %s", r.Props["framework"], r.ID)
		}
		if !strings.HasPrefix(r.Props["source_handler"], "SCOPE.Operation:ProductsController.") {
			t.Errorf("bad source_handler %q on %s", r.Props["source_handler"], r.ID)
		}
	}
}

func TestApplyJavaAnnotationRoutes_JAXRSMethodOnlyNoClassPrefix(t *testing.T) {
	src := `package com.example;
import jakarta.ws.rs.*;

public class StatusResource {
    @GET
    @Path("/status")
    public Object status() { return null; }
}
`
	got := collect(t, map[string]string{"Status.java": src})
	ids := endpointIDs(got)
	if len(ids) != 1 || ids[0] != "http:GET:/status" {
		t.Fatalf("ids = %v, want [http:GET:/status]", ids)
	}
}

func TestApplyJavaAnnotationRoutes_SpringRequestMappingClassGetMappingMethod(t *testing.T) {
	src := `package com.example;
import org.springframework.web.bind.annotation.*;

@RequestMapping("/api/users")
@RestController
public class UserController {
    @GetMapping("/{id}")
    public Object get() { return null; }

    @PostMapping
    public Object create() { return null; }
}
`
	got := collect(t, map[string]string{"User.java": src})
	ids := endpointIDs(got)
	want := []string{
		"http:GET:/api/users/{id}",
		"http:POST:/api/users",
	}
	sort.Strings(want)
	if strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Fatalf("ids = %v\nwant %v", ids, want)
	}
	for _, r := range got {
		if r.Props["framework"] != "spring" {
			t.Errorf("expected framework=spring, got %q for %s", r.Props["framework"], r.ID)
		}
	}
}

func TestApplyJavaAnnotationRoutes_SpringRequestMappingWithMethodKeyword(t *testing.T) {
	src := `package com.example;
import org.springframework.web.bind.annotation.*;

@RequestMapping("/api/items")
@RestController
public class ItemController {
    @RequestMapping(value = "/{id}", method = RequestMethod.POST)
    public Object update() { return null; }
}
`
	got := collect(t, map[string]string{"Item.java": src})
	ids := endpointIDs(got)
	if len(ids) != 1 || ids[0] != "http:POST:/api/items/{id}" {
		t.Fatalf("ids = %v, want [http:POST:/api/items/{id}]", ids)
	}
}

func TestApplyJavaAnnotationRoutes_PathParamRegexStripped(t *testing.T) {
	// Spring + JAX-RS both allow regex constraints inside {name:regex}.
	// The canonicalizer should drop the constraint.
	src := `package com.example;
import jakarta.ws.rs.*;

@Path("/things")
public class ThingsResource {
    @GET
    @Path("/{name:[a-z]+}")
    public Object byName() { return null; }
}
`
	got := collect(t, map[string]string{"Things.java": src})
	ids := endpointIDs(got)
	if len(ids) != 1 || ids[0] != "http:GET:/things/{name}" {
		t.Fatalf("ids = %v, want [http:GET:/things/{name}]", ids)
	}
}

func TestApplyJavaAnnotationRoutes_MultipleVerbsOnSamePath(t *testing.T) {
	src := `package com.example;
import jakarta.ws.rs.*;

@Path("/widgets")
public class WidgetController {
    @GET
    @Path("/{id}")
    public Object get() { return null; }

    @DELETE
    @Path("/{id}")
    public Object remove() { return null; }
}
`
	got := collect(t, map[string]string{"Widget.java": src})
	ids := endpointIDs(got)
	want := []string{"http:DELETE:/widgets/{id}", "http:GET:/widgets/{id}"}
	if strings.Join(ids, "|") != strings.Join(want, "|") {
		t.Fatalf("ids = %v\nwant %v", ids, want)
	}
}

func TestApplyJavaAnnotationRoutes_ConsumesProducesSurfaced(t *testing.T) {
	src := `package com.example;
import jakarta.ws.rs.*;
import jakarta.ws.rs.core.MediaType;

@Path("/files")
@Consumes(MediaType.APPLICATION_JSON)
@Produces(MediaType.APPLICATION_JSON)
public class FilesController {
    @POST
    @Path("/upload")
    @Consumes(MediaType.MULTIPART_FORM_DATA)
    public Object upload() { return null; }
}
`
	got := collect(t, map[string]string{"Files.java": src})
	if len(got) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(got))
	}
	r := got[0]
	if !strings.Contains(r.Props["consumes"], "MULTIPART_FORM_DATA") {
		t.Errorf("expected method-level consumes override, got %q", r.Props["consumes"])
	}
	if !strings.Contains(r.Props["produces"], "APPLICATION_JSON") {
		t.Errorf("expected class-level produces inherited, got %q", r.Props["produces"])
	}
}

func TestApplyJavaAnnotationRoutes_NonRouteFileSkipped(t *testing.T) {
	src := `package com.example;
public class PojoBag {
    private int x;
    public int getX() { return x; }
}
`
	got := collect(t, map[string]string{"Pojo.java": src})
	if len(got) != 0 {
		t.Fatalf("expected 0 endpoints for non-route file, got %d", len(got))
	}
}

func TestApplyJavaAnnotationRoutes_SpringSpecialisedMappingInlinePath(t *testing.T) {
	// No class-level @RequestMapping — only specialised method mappings.
	src := `package com.example;
import org.springframework.web.bind.annotation.*;

@RestController
public class PingController {
    @GetMapping("/ping")
    public String ping() { return "pong"; }
}
`
	got := collect(t, map[string]string{"Ping.java": src})
	ids := endpointIDs(got)
	if len(ids) != 1 || ids[0] != "http:GET:/ping" {
		t.Fatalf("ids = %v, want [http:GET:/ping]", ids)
	}
}
