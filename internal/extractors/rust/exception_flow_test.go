package rust_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/rust"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractErrFlow runs the rust extractor over src (reusing the shared
// extractRust helper) and returns the entities.
func extractErrFlow(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	return extractRust(t, "errs.rs", src)
}

// exceptionNode reports whether a SCOPE.ExceptionType convergence node exists
// for typeName, with the canonical synthetic SourceFile / QualifiedName.
func exceptionNode(ents []types.EntityRecord, typeName string) *types.EntityRecord {
	for i := range ents {
		e := &ents[i]
		if e.Kind == string(types.EntityKindExceptionType) &&
			e.Name == extractor.ExceptionTypeName(typeName) {
			return e
		}
	}
	return nil
}

// hasErrEdge reports whether some entity has a THROWS (catch=false) or CATCHES
// (catch=true) edge to typeName, optionally restricted to a given host Name.
func hasErrEdge(ents []types.EntityRecord, host, typeName string, catch bool) bool {
	want := string(types.RelationshipKindThrows)
	if catch {
		want = string(types.RelationshipKindCatches)
	}
	target := extractor.ExceptionTypeTargetID(typeName)
	for i := range ents {
		e := &ents[i]
		if host != "" && e.Name != host {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == want && r.ToID == target {
				return true
			}
		}
	}
	return false
}

func TestRustExceptionFlow_ThrowsTyped(t *testing.T) {
	src := `
fn find() -> Result<User, NotFoundError> {
    if x { return Err(NotFoundError::new()); }
    Err(IoError::from(e))
}

fn variant() -> Result<(), MyError> {
    return Err(MyError::NotFound);
}

fn tuple_ctor() -> Result<(), ValueErr> {
    Err(ValueErr(42))
}
`
	ents := extractErrFlow(t, src)

	for _, ty := range []string{"NotFoundError", "IoError", "MyError", "ValueErr"} {
		if exceptionNode(ents, ty) == nil {
			t.Errorf("missing exception node for %q", ty)
		}
	}
	if !hasErrEdge(ents, "find", "NotFoundError", false) {
		t.Error("find should THROWS NotFoundError (Err(Type::new()))")
	}
	if !hasErrEdge(ents, "find", "IoError", false) {
		t.Error("find should THROWS IoError (Err(Type::from()))")
	}
	if !hasErrEdge(ents, "variant", "MyError", false) {
		t.Error("variant should THROWS MyError (Err(MyError::NotFound) → ENUM type)")
	}
	if !hasErrEdge(ents, "tuple_ctor", "ValueErr", false) {
		t.Error("tuple_ctor should THROWS ValueErr (Err(ValueErr(..)))")
	}
	// Convergence node uses the synthetic SourceFile + QualifiedName.
	n := exceptionNode(ents, "NotFoundError")
	if n.SourceFile != extractor.ExceptionTypeSourceFile {
		t.Errorf("exception node SourceFile = %q, want synthetic %q", n.SourceFile, extractor.ExceptionTypeSourceFile)
	}
	if n.QualifiedName != extractor.ExceptionTypeTargetID("NotFoundError") {
		t.Errorf("exception node QualifiedName = %q, want %q", n.QualifiedName, extractor.ExceptionTypeTargetID("NotFoundError"))
	}
}

func TestRustExceptionFlow_ThrowsMacroAndOkOr(t *testing.T) {
	src := `
fn macros() -> Result<(), MyError> {
    bail!(MyError::Boom);
    ensure!(cond, OtherErr::Bad);
    let y = thing.ok_or(MissingErr::Gone)?;
    let z = thing.ok_or_else(|| LazyErr::Late)?;
    Ok(())
}
`
	ents := extractErrFlow(t, src)
	if !hasErrEdge(ents, "macros", "MyError", false) {
		t.Error("macros should THROWS MyError (bail!(MyError::Boom))")
	}
	if !hasErrEdge(ents, "macros", "OtherErr", false) {
		t.Error("macros should THROWS OtherErr (ensure!(cond, OtherErr::Bad))")
	}
	if !hasErrEdge(ents, "macros", "MissingErr", false) {
		t.Error("macros should THROWS MissingErr (ok_or(MissingErr::Gone))")
	}
	if !hasErrEdge(ents, "macros", "LazyErr", false) {
		t.Error("macros should THROWS LazyErr (ok_or_else(|| LazyErr::Late))")
	}
}

func TestRustExceptionFlow_CatchesTyped(t *testing.T) {
	src := `
fn handle() {
    match find() {
        Err(NotFoundError) => recover(),
        Ok(v) => use_it(v),
    }
    if let Err(ParseError) = parse() { log(); }
    let r = find().map_err(|e: IoError| wrap(e));
}

fn enum_arm() {
    match thing() {
        Err(MyError::Bad) => handle(),
        _ => {}
    }
}
`
	ents := extractErrFlow(t, src)
	if !hasErrEdge(ents, "handle", "NotFoundError", true) {
		t.Error("handle should CATCHES NotFoundError (typed match arm)")
	}
	if !hasErrEdge(ents, "handle", "ParseError", true) {
		t.Error("handle should CATCHES ParseError (if let Err)")
	}
	if !hasErrEdge(ents, "handle", "IoError", true) {
		t.Error("handle should CATCHES IoError (map_err typed closure)")
	}
	if !hasErrEdge(ents, "enum_arm", "MyError", true) {
		t.Error("enum_arm should CATCHES MyError (Err(MyError::Bad) → ENUM type)")
	}
}

func TestRustExceptionFlow_ConvergenceCrossFunction(t *testing.T) {
	// NotFoundError raised in one fn + caught in another → ONE node with both
	// the outbound THROWS and inbound CATCHES forming the error contract.
	src := `
fn raise() -> Result<(), NotFoundError> {
    Err(NotFoundError::new())
}
fn catch() {
    match raise() {
        Err(NotFoundError) => recover(),
        _ => {}
    }
}
`
	ents := extractErrFlow(t, src)
	count := 0
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindExceptionType) &&
			ents[i].Name == extractor.ExceptionTypeName("NotFoundError") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly ONE exception:NotFoundError node, got %d", count)
	}
	if !hasErrEdge(ents, "raise", "NotFoundError", false) {
		t.Error("raise should THROWS NotFoundError")
	}
	if !hasErrEdge(ents, "catch", "NotFoundError", true) {
		t.Error("catch should CATCHES NotFoundError")
	}
}

func TestRustExceptionFlow_ImplMethodHost(t *testing.T) {
	src := `
impl Repo {
    fn lookup(&self) -> Result<User, NotFoundError> {
        Err(NotFoundError::new())
    }
}
`
	ents := extractErrFlow(t, src)
	// rust.go qualifies impl methods as "Repo.lookup"; the edge must attach
	// to that host, not the bare "lookup".
	if !hasErrEdge(ents, "Repo.lookup", "NotFoundError", false) {
		t.Error("impl method Repo.lookup should THROWS NotFoundError")
	}
}

func TestRustExceptionFlow_Negatives(t *testing.T) {
	// None of these carry a specific static error type → NO exception nodes,
	// NO THROWS/CATCHES edges. Precision-first / honest-partial.
	src := `
fn bare_propagation() -> Result<User, Box<dyn Error>> {
    let u = lookup()?;            // bare ? — no type at this site
    Ok(u)
}

fn boxed() -> Result<(), Box<dyn Error>> {
    Err(Box::new(some_err))      // boxed trait object — no specific type
}

fn rethrow(e: MyError) -> Result<(), MyError> {
    Err(e)                       // re-raise of a variable — no NEW type
}

fn dynamic() -> Result<(), MyError> {
    Err(make_error())            // snake_case factory — no static type
}

fn panics() {
    panic!("plain string message"); // string payload, not a type
}

fn binding() {
    match thing() {
        Err(e) => log(e),        // value binding, not a type
        Ok(v) => use_it(v),      // success
        _ => {}
    }
}
`
	ents := extractErrFlow(t, src)
	for i := range ents {
		if ents[i].Kind == string(types.EntityKindExceptionType) {
			t.Errorf("unexpected exception node %q from negative-only source", ents[i].Name)
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == string(types.RelationshipKindThrows) ||
				r.Kind == string(types.RelationshipKindCatches) {
				t.Errorf("unexpected %s edge to %q from negative-only source", r.Kind, r.ToID)
			}
		}
	}
}
