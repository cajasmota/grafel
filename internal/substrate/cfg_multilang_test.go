package substrate

import "testing"

// Control-flow extraction generalized from the JS/TS + Python validated set to
// the remaining brace-family languages plus Ruby and Swift (#4830, epic #4820).
//
// Part (b) (cfg_test.go) validated Python and JS/TS. The brace-family detectors
// (java/go/php/csharp/kotlin/scala/rust) and the non-brace families (ruby/swift)
// previously "ran but were only jsts-validated" — and several had real per-
// language quirks the C/Java baseline missed (Go's no-paren `if cond {`, Rust's
// `match`/`loop`, PHP's `elseif`/`foreach`, Ruby's `end`-blocks, Swift's
// `guard`/no-paren `switch`). This file flips each from "runs" to "validated"
// with a small real-shaped fixture: an if/else + loop + effect function whose
// CFG must contain a decision node carrying condition text, a loop node with a
// loop_back edge, a terminal wired to the exit, an effect-annotated node, and a
// non-trivial cyclomatic complexity.

// assertValidatedCFG runs the shared per-language validation contract.
func assertValidatedCFG(t *testing.T, lang, src string, wantLoopBack bool) {
	t.Helper()
	g := BuildControlFlowGraph(lang, src, 100)

	if !g.Supported {
		t.Fatalf("%s: CFG should be Supported (block detector wired); nodes=%+v", lang, g.Nodes)
	}
	if g.Cyclomatic < 2 {
		t.Errorf("%s: cyclomatic = %d; want >= 2 (non-trivial)", lang, g.Cyclomatic)
	}
	if g.BranchCount != g.Cyclomatic-1 {
		t.Errorf("%s: branch_count %d != cyclomatic-1 %d", lang, g.BranchCount, g.Cyclomatic-1)
	}

	// A decision node carrying condition text.
	decs := nodesByShape(g, ShapeDecision)
	if len(decs) == 0 {
		t.Fatalf("%s: no decision node; nodes=%+v", lang, g.Nodes)
	}
	hasCond := false
	for _, d := range decs {
		if d.Condition != "" {
			hasCond = true
		}
	}
	if !hasCond {
		t.Errorf("%s: no decision carried condition text; decisions=%+v", lang, decs)
	}

	// A loop node + (where the loop body has a tail) a back-edge.
	if len(nodesByShape(g, ShapeLoop)) == 0 {
		t.Errorf("%s: no loop node; nodes=%+v", lang, g.Nodes)
	}
	if wantLoopBack && !hasEdgeKind(g, EdgeBack) {
		t.Errorf("%s: no loop back-edge; edges=%+v", lang, g.Edges)
	}

	// An effect-annotated process node (db_read/db_write/http_out).
	gotEffect := false
	for _, n := range g.Nodes {
		if len(n.Effects) > 0 {
			gotEffect = true
		}
	}
	if !gotEffect {
		t.Errorf("%s: expected an effect-annotated node; nodes=%+v", lang, g.Nodes)
	}

	// Exactly one start + one end bookend.
	if len(nodesByShape(g, ShapeStart)) != 1 || len(nodesByShape(g, ShapeEnd)) != 1 {
		t.Errorf("%s: want exactly one start and one end; nodes=%+v", lang, g.Nodes)
	}
}

// TestCFGGoValidated — Go: no-paren `if force {`, `for rows.Next() {`, an
// explicit `return`, db/http effects.
func TestCFGGoValidated(t *testing.T) {
	assertValidatedCFG(t, "go", `func Sync(ctx context.Context, force bool) error {
	rows, _ := db.Query(ctx, "SELECT * FROM contacts")
	if force {
		db.Exec(ctx, "INSERT INTO log VALUES (1)")
	}
	for rows.Next() {
		http.Post("https://api.example.com/notify", "application/json", nil)
	}
	return nil
}
`, true)
}

// TestCFGRustValidated — Rust: no-paren `if force {`, `for row in rows {`, `?`
// early-exit (modeled as effect lines), db/http effects.
func TestCFGRustValidated(t *testing.T) {
	assertValidatedCFG(t, "rust", `fn sync(force: bool) -> Result<(), Error> {
    let rows = client.query("SELECT * FROM contacts", &[])?;
    if force {
        client.execute("INSERT INTO log VALUES (1)", &[])?;
    }
    for row in rows {
        reqwest::blocking::Client::new().post("https://api.example.com/notify").send()?;
    }
    Ok(())
}
`, true)
}

// TestCFGKotlinValidated — Kotlin: paren `if (force)`, `for (row in rows)`.
func TestCFGKotlinValidated(t *testing.T) {
	assertValidatedCFG(t, "kotlin", `fun sync(force: Boolean) {
    val rows = repository.findAll()
    if (force) {
        repository.save(Log(1))
    }
    for (row in rows) {
        httpClient.post("https://api.example.com/notify")
    }
    return
}
`, true)
}

// TestCFGScalaValidated — Scala: paren `if (force)`, `for (row <- rows)`.
func TestCFGScalaValidated(t *testing.T) {
	assertValidatedCFG(t, "scala", `def sync(force: Boolean): Unit = {
    val rows = sql"SELECT * FROM contacts".query[Contact].to[List]
    if (force) {
      em.persist(new Log(1))
    }
    for (row <- rows) {
      requests.post("https://api.example.com/notify")
    }
  }
`, true)
}

// TestCFGCSharpValidated — C#: paren `if (force)`, `foreach (var row in rows)`.
func TestCFGCSharpValidated(t *testing.T) {
	assertValidatedCFG(t, "csharp", `public async Task Sync(bool force) {
    var rows = await _db.Contacts.ToListAsync();
    if (force) {
        await _db.Logs.AddAsync(new Log());
    }
    foreach (var row in rows) {
        await _http.PostAsync("https://api.example.com/notify", null);
    }
    return;
}
`, true)
}

// TestCFGPHPValidated — PHP: `elseif` (second decision) and `foreach` (loop),
// which the C/Java baseline previously missed entirely.
func TestCFGPHPValidated(t *testing.T) {
	g := BuildControlFlowGraph("php", `function sync($force) {
    $rows = DB::table('contacts')->get();
    if ($force) {
        DB::table('log')->insert(['id' => 1]);
    } elseif ($other) {
        DB::table('log')->insert(['id' => 2]);
    }
    foreach ($rows as $row) {
        Http::post("https://api.example.com/notify");
    }
    return null;
}
`, 100)
	if !g.Supported {
		t.Fatalf("php CFG should be supported; nodes=%+v", g.Nodes)
	}
	// elseif must be its OWN decision (regression: previously eaten as bare else
	// without a condition).
	sawElseIf := false
	for _, d := range nodesByShape(g, ShapeDecision) {
		if d.Condition == "elseif ($other)" {
			sawElseIf = true
		}
	}
	if !sawElseIf {
		t.Errorf("php elseif not surfaced as its own conditioned decision; decisions=%+v", nodesByShape(g, ShapeDecision))
	}
	// foreach must be a loop with a back-edge.
	if len(nodesByShape(g, ShapeLoop)) == 0 {
		t.Errorf("php foreach not surfaced as a loop; nodes=%+v", g.Nodes)
	}
	if !hasEdgeKind(g, EdgeBack) {
		t.Errorf("php foreach has no loop back-edge; edges=%+v", g.Edges)
	}
	if !hasEdgeKind(g, EdgeExit) {
		t.Errorf("php no exit edge; edges=%+v", g.Edges)
	}
}

// TestCFGRubyValidated — Ruby (`end`-delimited, NOT brace): `if force … end`
// block + `.each do |row| … end` iterator block (a loop). Previously returned
// supported=false.
func TestCFGRubyValidated(t *testing.T) {
	assertValidatedCFG(t, "ruby", `def sync(force)
  rows = Contact.where(active: true)
  if force
    Contact.create(name: "x")
  end
  rows.each do |row|
    Net::HTTP.post(URI("https://api.example.com/notify"), "")
  end
  nil
end
`, true)
}

// TestCFGSwiftValidated — Swift: no-paren `if force {`, `for row in rows {`,
// explicit `return`. Previously returned supported=false.
func TestCFGSwiftValidated(t *testing.T) {
	assertValidatedCFG(t, "swift", `func sync(force: Bool) {
    let rows = try! context.fetch(request)
    if force {
        context.insert(Log())
    }
    for row in rows {
        URLSession.shared.dataTask(with: url).resume()
    }
    return
}
`, true)
}

// TestCFGGoSwitchSelect — Go-specific: `switch x {` and `select {` are decision
// nodes (no-paren), and `case` bodies carry effects.
func TestCFGGoSwitchSelect(t *testing.T) {
	g := BuildControlFlowGraph("go", `func route(kind string) error {
	switch kind {
	case "write":
		db.Exec(ctx, "INSERT INTO a VALUES (1)")
	default:
		return nil
	}
	return nil
}
`, 10)
	if !g.Supported {
		t.Fatalf("go switch CFG should be supported")
	}
	sawSwitch := false
	for _, d := range nodesByShape(g, ShapeDecision) {
		if d.Condition == "switch kind" {
			sawSwitch = true
		}
	}
	if !sawSwitch {
		t.Errorf("go `switch` not surfaced as a decision; decisions=%+v", nodesByShape(g, ShapeDecision))
	}
}

// TestCFGRustMatchLoop — Rust-specific: `match x {` decision + `loop {` loop.
func TestCFGRustMatchLoop(t *testing.T) {
	g := BuildControlFlowGraph("rust", `fn poll(kind: &str) {
    match kind {
        "a" => { client.execute("INSERT INTO a", &[]); }
        _ => {}
    }
    loop {
        reqwest::get("https://x").unwrap();
        break;
    }
}
`, 10)
	sawMatch, sawLoop := false, false
	for _, d := range nodesByShape(g, ShapeDecision) {
		if d.Condition == "match kind" {
			sawMatch = true
		}
	}
	for _, n := range nodesByShape(g, ShapeLoop) {
		if n.Condition == "loop" {
			sawLoop = true
		}
	}
	if !sawMatch {
		t.Errorf("rust `match` not surfaced as a decision; decisions=%+v", nodesByShape(g, ShapeDecision))
	}
	if !sawLoop {
		t.Errorf("rust `loop` not surfaced as a loop; loops=%+v", nodesByShape(g, ShapeLoop))
	}
}

// TestCFGKotlinWhen — Kotlin-specific: `when (x) {` is a decision.
func TestCFGKotlinWhen(t *testing.T) {
	g := BuildControlFlowGraph("kotlin", `fun route(kind: String) {
    when (kind) {
        "a" -> repository.save(A())
        else -> {}
    }
}
`, 10)
	saw := false
	for _, d := range nodesByShape(g, ShapeDecision) {
		if d.Condition == "when (kind)" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("kotlin `when` not surfaced as a decision; decisions=%+v", nodesByShape(g, ShapeDecision))
	}
}

// TestCFGScalaMatch — Scala-specific: infix `x match {` is a decision.
func TestCFGScalaMatch(t *testing.T) {
	g := BuildControlFlowGraph("scala", `def route(kind: String): Unit = {
    kind match {
      case "a" => repo.save(A())
      case _ =>
    }
  }
`, 10)
	saw := false
	for _, d := range nodesByShape(g, ShapeDecision) {
		if d.Condition == "kind match" {
			saw = true
		}
	}
	if !saw {
		t.Errorf("scala infix `match` not surfaced as a decision; decisions=%+v", nodesByShape(g, ShapeDecision))
	}
}

// TestCFGSwiftGuardSwitch — Swift-specific: `guard … else {` and no-paren
// `switch x {` are decisions.
func TestCFGSwiftGuardSwitch(t *testing.T) {
	g := BuildControlFlowGraph("swift", `func route(kind: String) {
    guard kind != "" else {
        return
    }
    switch kind {
    case "a":
        context.insert(A())
    default:
        break
    }
}
`, 10)
	sawGuard, sawSwitch := false, false
	for _, d := range nodesByShape(g, ShapeDecision) {
		if len(d.Condition) >= 5 && d.Condition[:5] == "guard" {
			sawGuard = true
		}
		if d.Condition == "switch kind" {
			sawSwitch = true
		}
	}
	if !sawGuard {
		t.Errorf("swift `guard` not surfaced as a decision; decisions=%+v", nodesByShape(g, ShapeDecision))
	}
	if !sawSwitch {
		t.Errorf("swift `switch` not surfaced as a decision; decisions=%+v", nodesByShape(g, ShapeDecision))
	}
}

// TestCFGUnsupportedLanguagesDocumented pins WHICH languages remain
// supported=false (no validated block detector) so the honest-partial contract
// is explicit and a future regression that silently flips them is caught.
func TestCFGUnsupportedLanguagesDocumented(t *testing.T) {
	for _, lang := range []string{"cobol", "elixir", "crystal", "lua", "dart", "solidity", "zig", "nim", "clojure"} {
		g := BuildControlFlowGraph(lang, "x = 1\n", 1)
		if g.Supported {
			t.Errorf("%s has no validated block detector; Supported should be false (update the deferred list + docs if intentionally added)", lang)
		}
		if len(nodesByShape(g, ShapeStart)) != 1 || len(nodesByShape(g, ShapeEnd)) != 1 {
			t.Errorf("%s: degenerate CFG must still have start+end; nodes=%+v", lang, g.Nodes)
		}
	}
}
