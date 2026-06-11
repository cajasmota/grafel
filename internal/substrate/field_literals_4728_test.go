package substrate

import (
	"reflect"
	"sort"
	"testing"
)

// field_literals_4728_test.go — #4728 field-level partial-stub analyzers for
// Java/Spring, Go, Ruby/Rails, PHP/Laravel, C#/.NET.
//
// Each language is validated with the issue's two fixtures, mirroring the #4669
// flagship cases:
//   - A: {count: <derived>, tbd: 0, all: 5}  → flags tbd, all; NOT count.
//   - negative: {success: true, data: <derived>} → flags nothing (data derived,
//     success excluded as envelope flag).
// Plus a part_id:null case and conditional-derived guard where instructive.

func names4728(t *testing.T, facets []FieldFacet) []string {
	t.Helper()
	flagged := PartialStubFields(facets)
	out := make([]string, 0, len(flagged))
	for _, f := range flagged {
		out = append(out, f.Field)
	}
	sort.Strings(out)
	return out
}

// --- Go -----------------------------------------------------------------

func TestFieldLiteralsGo_CaseA_ginH(t *testing.T) {
	src := `func GetSummary(c *gin.Context) {
	n := repo.Count()
	c.JSON(200, gin.H{
		"count": n,
		"tbd":   0,
		"all":   5,
	})
}`
	facets := analyzeFieldLiteralsGo(src, 1)
	got := names4728(t, facets)
	want := []string{"all", "tbd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Go Case A: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsGo_Negative_envelope(t *testing.T) {
	src := `func List(c *gin.Context) {
	data := repo.All()
	c.JSON(200, gin.H{"success": true, "data": data})
}`
	facets := analyzeFieldLiteralsGo(src, 1)
	if got := names4728(t, facets); len(got) != 0 {
		t.Fatalf("Go negative: expected no flags, got %v (facets=%+v)", got, facets)
	}
}

func TestFieldLiteralsGo_structLiteral(t *testing.T) {
	src := `func GetItem(c *gin.Context) {
	item := svc.Get()
	c.JSON(200, ItemResp{PartID: nil, Name: item.Name})
}`
	facets := analyzeFieldLiteralsGo(src, 1)
	got := names4728(t, facets)
	want := []string{"PartID"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Go struct: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsGo_mapStringAny(t *testing.T) {
	src := `func H(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(map[string]any{"part_id": nil, "name": user.Name})
}`
	facets := analyzeFieldLiteralsGo(src, 1)
	got := names4728(t, facets)
	want := []string{"part_id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Go map[string]any: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

// --- Ruby ---------------------------------------------------------------

func TestFieldLiteralsRuby_CaseA_renderJson(t *testing.T) {
	src := `def summary
	n = Thing.count
	render json: {
		count: n,
		tbd: 0,
		all: 5,
	}
end`
	facets := analyzeFieldLiteralsRuby(src, 1)
	got := names4728(t, facets)
	want := []string{"all", "tbd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ruby Case A: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsRuby_Negative_envelope(t *testing.T) {
	src := `def list
	data = Thing.all
	render json: { success: true, data: data }
end`
	facets := analyzeFieldLiteralsRuby(src, 1)
	if got := names4728(t, facets); len(got) != 0 {
		t.Fatalf("Ruby negative: expected no flags, got %v (facets=%+v)", got, facets)
	}
}

func TestFieldLiteralsRuby_partIdNil(t *testing.T) {
	src := `def show
	item = find_item
	render json: { part_id: nil, name: item.name }
end`
	facets := analyzeFieldLiteralsRuby(src, 1)
	got := names4728(t, facets)
	want := []string{"part_id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Ruby part_id: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

// --- PHP ----------------------------------------------------------------

func TestFieldLiteralsPHP_CaseA_responseJson(t *testing.T) {
	src := `public function summary() {
	$n = Thing::count();
	return response()->json([
		'count' => $n,
		'tbd' => 0,
		'all' => 5,
	]);
}`
	facets := analyzeFieldLiteralsPHP(src, 1)
	got := names4728(t, facets)
	want := []string{"all", "tbd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PHP Case A: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsPHP_Negative_envelope(t *testing.T) {
	src := `public function list() {
	$data = Thing::all();
	return response()->json(['success' => true, 'data' => $data]);
}`
	facets := analyzeFieldLiteralsPHP(src, 1)
	if got := names4728(t, facets); len(got) != 0 {
		t.Fatalf("PHP negative: expected no flags, got %v (facets=%+v)", got, facets)
	}
}

func TestFieldLiteralsPHP_partIdNull(t *testing.T) {
	src := `public function show() {
	$item = $this->find();
	return ['part_id' => null, 'name' => $item->name];
}`
	facets := analyzeFieldLiteralsPHP(src, 1)
	got := names4728(t, facets)
	want := []string{"part_id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PHP part_id: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

// --- C# -----------------------------------------------------------------

func TestFieldLiteralsCSharp_CaseA_anonObject(t *testing.T) {
	src := `public IActionResult Summary() {
	var n = _repo.Count();
	return Ok(new {
		count = n,
		tbd = 0,
		all = 5,
	});
}`
	facets := analyzeFieldLiteralsCSharp(src, 1)
	got := names4728(t, facets)
	want := []string{"all", "tbd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("C# Case A: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsCSharp_Negative_envelope(t *testing.T) {
	src := `public IActionResult List() {
	var data = _repo.All();
	return Ok(new { success = true, data = data });
}`
	facets := analyzeFieldLiteralsCSharp(src, 1)
	if got := names4728(t, facets); len(got) != 0 {
		t.Fatalf("C# negative: expected no flags, got %v (facets=%+v)", got, facets)
	}
}

func TestFieldLiteralsCSharp_recordInit(t *testing.T) {
	src := `public IActionResult Show() {
	var item = _svc.Get();
	return Ok(new ItemResponse { PartId = null, Name = item.Name });
}`
	facets := analyzeFieldLiteralsCSharp(src, 1)
	got := names4728(t, facets)
	want := []string{"PartId"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("C# record init: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

// --- Java ---------------------------------------------------------------

func TestFieldLiteralsJava_CaseA_mapOf(t *testing.T) {
	src := `public ResponseEntity<?> summary() {
	long n = repo.count();
	return ResponseEntity.ok(Map.of(
		"count", n,
		"tbd", 0,
		"all", 5
	));
}`
	facets := analyzeFieldLiteralsJava(src, 1)
	got := names4728(t, facets)
	want := []string{"all", "tbd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Java Case A: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

func TestFieldLiteralsJava_Negative_envelope(t *testing.T) {
	src := `public ResponseEntity<?> list() {
	var data = repo.all();
	return ResponseEntity.ok(Map.of("success", true, "data", data));
}`
	facets := analyzeFieldLiteralsJava(src, 1)
	if got := names4728(t, facets); len(got) != 0 {
		t.Fatalf("Java negative: expected no flags, got %v (facets=%+v)", got, facets)
	}
}

func TestFieldLiteralsJava_objectNodePut(t *testing.T) {
	src := `public ObjectNode build() {
	ObjectNode node = mapper.createObjectNode();
	node.put("part_id", null);
	node.put("name", item.getName());
	return node;
}`
	facets := analyzeFieldLiteralsJava(src, 1)
	got := names4728(t, facets)
	want := []string{"part_id"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Java ObjectNode.put: flagged=%v want=%v (facets=%+v)", got, want, facets)
	}
}

// --- registry -----------------------------------------------------------

func TestFieldLiteralRegistry_4728_languagesRegistered(t *testing.T) {
	for _, lang := range []string{"java", "go", "ruby", "php", "csharp"} {
		if FieldLiteralAnalyzerFor(lang) == nil {
			t.Fatalf("expected field-literal analyzer registered for %q", lang)
		}
	}
}
