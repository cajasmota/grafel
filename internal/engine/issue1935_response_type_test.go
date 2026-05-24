package engine

import (
	"testing"
)

// Refs #1935 Phase 1 — handler method return types are extracted and
// surfaced via the `response_type` entity property so the dashboard can
// render an expandable ShapeTree response row.

func TestExtractJavaReturnType_PlainDTO(t *testing.T) {
	line := "    public LoginResponse login(LoginRequest req) {"
	got := extractJavaReturnType(line, "login")
	if got != "LoginResponse" {
		t.Errorf("want LoginResponse, got %q", got)
	}
}

func TestExtractJavaReturnType_UnwrapsResponseEntity(t *testing.T) {
	line := "public ResponseEntity<UserDTO> getUser(@PathVariable Long id) {"
	got := extractJavaReturnType(line, "getUser")
	if got != "UserDTO" {
		t.Errorf("want UserDTO, got %q", got)
	}
}

func TestExtractJavaReturnType_UnwrapsMono(t *testing.T) {
	if got := extractJavaReturnType("public Mono<Order> create(Order o) {", "create"); got != "Order" {
		t.Errorf("Mono unwrap: got %q", got)
	}
	if got := extractJavaReturnType("public Uni<Foo> bar() {", "bar"); got != "Foo" {
		t.Errorf("Uni unwrap: got %q", got)
	}
}

func TestExtractJavaReturnType_RejectsVoidAndNoise(t *testing.T) {
	if got := extractJavaReturnType("public void doIt() {", "doIt"); got != "" {
		t.Errorf("void: want empty, got %q", got)
	}
	if got := extractJavaReturnType("public Response handle() {", "handle"); got != "" {
		t.Errorf("bare Response: want empty (framework noise), got %q", got)
	}
}

func TestExtractJavaReturnType_EmittedOnEndpoint(t *testing.T) {
	// Full integration: a Spring controller method whose return type
	// must end up on the synthesized endpoint entity as response_type.
	src := `package x;
import org.springframework.web.bind.annotation.*;
@RestController
@RequestMapping("/api/v1")
public class AuthController {
    @PostMapping("/auth/login")
    public LoginResponse login(@RequestBody LoginRequest req) {
        return null;
    }
}
`
	reader := func(_ string) []byte { return []byte(src) }
	ents := ApplyJavaAnnotationRoutes([]string{"AuthController.java"}, reader)
	if len(ents) == 0 {
		t.Fatal("no endpoints emitted")
	}
	var got string
	for _, e := range ents {
		if e.Properties["verb"] == "POST" {
			got = e.Properties["response_type"]
		}
	}
	if got != "LoginResponse" {
		t.Errorf("POST /auth/login response_type: want LoginResponse, got %q", got)
	}
}
