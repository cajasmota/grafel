// Tests for the Phase 2A payload-shape sniffers (#2770). One per T1
// language. Each test verifies the canonical shape recognition the
// drift detector relies on; the goal is byte-stable output across
// runs and exhaustive coverage of the inline-literal cases.
package substrate

import (
	"reflect"
	"sort"
	"testing"
)

func TestPayloadShapeSnifferRegistry_T1AndT2AndT3(t *testing.T) {
	// T1 (#2770): go, java, jsts, python.
	// T2 (#2771): c-cpp, csharp, elixir, kotlin, php, ruby, rust, scala.
	// T3 (#2777): astro, clojure, crystal, fsharp, nim, solidity, svelte, swift, vue.
	want := []string{
		"astro", "c-cpp", "clojure", "crystal", "csharp", "elixir",
		"fsharp", "go", "java", "jsts", "kotlin", "nim", "php", "python",
		"ruby", "rust", "scala", "solidity", "svelte", "swift", "vue",
	}
	got := PayloadShapeLanguages()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registered languages mismatch: want %v got %v", want, got)
	}
}

func TestPayloadShapesPython_ProducerRequestReads(t *testing.T) {
	const src = `
def create_user(request):
    name = request.data["name"]
    email = request.data.get("email")
    age = request.json["age"]
    return Response({"id": 1, "name": name})
`
	shapes := sniffPayloadShapesPython(src)
	requestShape := findShape(shapes, "create_user", PayloadDirectionRequest, PayloadSideProducer)
	if requestShape == nil {
		t.Fatalf("expected producer request shape on create_user; got %+v", shapes)
	}
	wantFields := []string{"age", "email", "name"}
	if got := sortedNames(requestShape.Fields); !reflect.DeepEqual(got, wantFields) {
		t.Errorf("producer request fields: want %v got %v", wantFields, got)
	}
	responseShape := findShape(shapes, "create_user", PayloadDirectionResponse, PayloadSideProducer)
	if responseShape == nil {
		t.Fatalf("expected producer response shape on create_user; got %+v", shapes)
	}
	wantResp := []string{"id", "name"}
	if got := sortedNames(responseShape.Fields); !reflect.DeepEqual(got, wantResp) {
		t.Errorf("producer response fields: want %v got %v", wantResp, got)
	}
}

func TestPayloadShapesPython_ConsumerRequest(t *testing.T) {
	const src = `
def post_login():
    requests.post("/api/login", json={"username": "x", "password": "y"})
`
	shapes := sniffPayloadShapesPython(src)
	cs := findShape(shapes, "post_login", PayloadDirectionRequest, PayloadSideConsumer)
	if cs == nil {
		t.Fatalf("expected consumer request shape; got %+v", shapes)
	}
	if cs.EndpointHint != "/api/login" {
		t.Errorf("endpoint hint: want /api/login got %q", cs.EndpointHint)
	}
	if cs.VerbHint != "POST" {
		t.Errorf("verb hint: want POST got %q", cs.VerbHint)
	}
	want := []string{"password", "username"}
	if got := sortedNames(cs.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("consumer fields: want %v got %v", want, got)
	}
}

func TestPayloadShapesJSTS_Producer(t *testing.T) {
	const src = `
function createUser(req, res) {
  const name = req.body.name;
  const email = req.body["email"];
  res.json({ id: 1, name, email });
}
`
	shapes := sniffPayloadShapesJSTS(src)
	req := findShape(shapes, "createUser", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected jsts producer request shape; got %+v", shapes)
	}
	want := []string{"email", "name"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("jsts producer request fields: want %v got %v", want, got)
	}
	resp := findShape(shapes, "createUser", PayloadDirectionResponse, PayloadSideProducer)
	if resp == nil {
		t.Fatalf("expected jsts producer response shape; got %+v", shapes)
	}
	wantR := []string{"email", "id", "name"}
	if got := sortedNames(resp.Fields); !reflect.DeepEqual(got, wantR) {
		t.Errorf("jsts producer response fields: want %v got %v", wantR, got)
	}
}

func TestPayloadShapesJSTS_ConsumerAxiosAndFetch(t *testing.T) {
	const src = `
function submitForm() {
  axios.post("/api/users", { name: "x", email: "y" });
  fetch("/api/users", { method: "POST", body: JSON.stringify({ token: "z" }) });
}
`
	shapes := sniffPayloadShapesJSTS(src)
	consumerShapes := []PayloadShape{}
	for _, s := range shapes {
		if s.Side == PayloadSideConsumer {
			consumerShapes = append(consumerShapes, s)
		}
	}
	if len(consumerShapes) != 2 {
		t.Fatalf("expected 2 consumer shapes; got %d (%+v)", len(consumerShapes), consumerShapes)
	}
}

func TestPayloadShapesJSTS_UseForm(t *testing.T) {
	const src = `
const Comp = () => {
  const form = useForm({ defaultValues: { firstName: "", lastName: "" } });
  return null;
};
`
	shapes := sniffPayloadShapesJSTS(src)
	if len(shapes) == 0 {
		t.Fatalf("expected at least one useForm shape; got none")
	}
	var got *PayloadShape
	for i := range shapes {
		if shapes[i].Side == PayloadSideConsumer {
			got = &shapes[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("no consumer-side useForm shape; got %+v", shapes)
	}
	want := []string{"firstName", "lastName"}
	if !reflect.DeepEqual(sortedNames(got.Fields), want) {
		t.Errorf("useForm fields: want %v got %v", want, sortedNames(got.Fields))
	}
}

func TestPayloadShapesJava_RequestBodyDTO(t *testing.T) {
	const src = `
public class CreateUserDto {
  private String name;
  private String email;
  private Optional<String> phone;
}
public class UserController {
  @PostMapping("/users")
  public ResponseEntity<?> create(@RequestBody CreateUserDto dto) {
    return ResponseEntity.ok(Map.of("id", 1));
  }
}
`
	shapes := sniffPayloadShapesJava(src)
	req := findShape(shapes, "create", PayloadDirectionRequest, PayloadSideProducer)
	if req == nil {
		t.Fatalf("expected java producer request shape; got %+v", shapes)
	}
	want := []string{"email", "name", "phone"}
	if got := sortedNames(req.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("java DTO fields: want %v got %v", want, got)
	}
	// Confirm Optional<String> phone is flagged optional.
	for _, f := range req.Fields {
		if f.Name == "phone" && !f.Optional {
			t.Errorf("phone should be Optional=true; got %+v", f)
		}
	}
}

func TestPayloadShapesJava_MapOfResponse(t *testing.T) {
	const src = `
public class C {
  public Object f() {
    return Map.of("id", 1, "ok", true);
  }
}
`
	shapes := sniffPayloadShapesJava(src)
	r := findShape(shapes, "f", PayloadDirectionResponse, PayloadSideProducer)
	if r == nil {
		t.Fatalf("expected java Map.of response shape; got %+v", shapes)
	}
	want := []string{"id", "ok"}
	if got := sortedNames(r.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("java Map.of fields: want %v got %v", want, got)
	}
}

func TestPayloadShapesGo_StructDecode(t *testing.T) {
	const src = `
package h

type CreateUserReq struct {
	Name  string ` + "`json:\"name\"`" + `
	Email string ` + "`json:\"email,omitempty\"`" + `
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserReq
	json.NewDecoder(r.Body).Decode(&req)
	json.NewEncoder(w).Encode(&req)
}
`
	shapes := sniffPayloadShapesGo(src)
	reqShape := findShape(shapes, "createUser", PayloadDirectionRequest, PayloadSideProducer)
	if reqShape == nil {
		t.Fatalf("expected go producer request shape; got %+v", shapes)
	}
	want := []string{"email", "name"}
	if got := sortedNames(reqShape.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("go request fields: want %v got %v", want, got)
	}
	// email should be optional via omitempty.
	for _, f := range reqShape.Fields {
		if f.Name == "email" && !f.Optional {
			t.Errorf("email should be Optional=true via omitempty; got %+v", f)
		}
	}
}

func TestPayloadShapesGo_GinH(t *testing.T) {
	const src = `
package h

func handle(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"id": 1, "name": "x"})
}
`
	shapes := sniffPayloadShapesGo(src)
	r := findShape(shapes, "handle", PayloadDirectionResponse, PayloadSideProducer)
	if r == nil {
		t.Fatalf("expected go gin.H response shape; got %+v", shapes)
	}
	want := []string{"id", "name"}
	if got := sortedNames(r.Fields); !reflect.DeepEqual(got, want) {
		t.Errorf("gin.H fields: want %v got %v", want, got)
	}
}

func TestNormalizeFieldName(t *testing.T) {
	cases := map[string]string{
		"first_name": "firstname",
		"firstName":  "firstname",
		"FirstName":  "firstname",
		"first-name": "firstname",
		"":           "",
	}
	for in, want := range cases {
		if got := NormalizeFieldName(in); got != want {
			t.Errorf("NormalizeFieldName(%q): want %q got %q", in, want, got)
		}
	}
}

func TestDedupFields(t *testing.T) {
	in := []PayloadField{
		{Name: "a"},
		{Name: "b"},
		{Name: "a"},
		{Name: ""},
		{Name: "c"},
	}
	out := DedupFields(in)
	got := sortedNames(out)
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dedup: want %v got %v", want, got)
	}
}

// findShape returns the first PayloadShape matching the given
// (function, direction, side) tuple, or nil when none matches.
func findShape(shapes []PayloadShape, fn string, dir PayloadDirection, side PayloadSide) *PayloadShape {
	for i := range shapes {
		s := &shapes[i]
		if s.Function == fn && s.Direction == dir && s.Side == side {
			return s
		}
	}
	return nil
}

func sortedNames(fs []PayloadField) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}
