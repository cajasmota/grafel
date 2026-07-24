package sml_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/sml"
	"github.com/cajasmota/grafel/internal/types"
)

// runSML runs the SML extractor on raw source and returns entity records.
func runSML(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("sml")
	if !ok {
		t.Fatal("sml extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "sml",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func smlFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func smlHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
	for i := range ents {
		if ents[i].Name != name || ents[i].Kind != kind {
			continue
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == edgeKind && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

// TestSML_Registered verifies the extractor is in the registry.
func TestSML_Registered(t *testing.T) {
	_, ok := extractor.Get("sml")
	if !ok {
		t.Fatal("sml extractor not registered")
	}
}

// TestSML_EmptyInput returns zero entities for empty content.
func TestSML_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("sml")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.sml",
		Content:  []byte{},
		Language: "sml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestSML_StructureDeclaration — structure declarations → SCOPE.Component(structure).
func TestSML_StructureDeclaration(t *testing.T) {
	src := `structure Math = struct
  fun square x = x * x
  fun cube x = x * x * x
end

structure StringUtils : sig
  val upper : string -> string
end = struct
  fun upper s = String.map Char.toUpper s
end
`
	ents := runSML(t, src, "math.sml")

	math := smlFind(ents, "Math", "SCOPE.Component")
	if math == nil {
		t.Error("expected Math structure component")
	} else if math.Subtype != "structure" {
		t.Errorf("Math: expected subtype=structure, got %q", math.Subtype)
	}

	utils := smlFind(ents, "StringUtils", "SCOPE.Component")
	if utils == nil {
		t.Error("expected StringUtils structure component")
	} else if utils.Subtype != "structure" {
		t.Errorf("StringUtils: expected subtype=structure, got %q", utils.Subtype)
	}
}

// TestSML_SignatureDeclaration — signature declarations → SCOPE.Component(signature).
func TestSML_SignatureDeclaration(t *testing.T) {
	src := `signature ORDERED = sig
  type t
  val compare : t * t -> order
end

signature COLLECTION = sig
  type 'a t
  val empty : 'a t
  val insert : 'a -> 'a t -> 'a t
end
`
	ents := runSML(t, src, "sigs.sig")

	ordered := smlFind(ents, "ORDERED", "SCOPE.Component")
	if ordered == nil {
		t.Error("expected ORDERED signature component")
	} else if ordered.Subtype != "signature" {
		t.Errorf("ORDERED: expected subtype=signature, got %q", ordered.Subtype)
	}

	coll := smlFind(ents, "COLLECTION", "SCOPE.Component")
	if coll == nil {
		t.Error("expected COLLECTION signature component")
	} else if coll.Subtype != "signature" {
		t.Errorf("COLLECTION: expected subtype=signature, got %q", coll.Subtype)
	}
}

// TestSML_FunctorDeclaration — functor declarations → SCOPE.Component(functor).
func TestSML_FunctorDeclaration(t *testing.T) {
	src := `functor MakeSet (Elem : ORDERED) : SET = struct
  type t = Elem.t list
  val empty = []
  fun insert x s = x :: s
end

functor MakeMap (Key : ORDERED) = struct
  type 'a t = (Key.t * 'a) list
  val empty = []
end
`
	ents := runSML(t, src, "functors.fun")

	makeSet := smlFind(ents, "MakeSet", "SCOPE.Component")
	if makeSet == nil {
		t.Error("expected MakeSet functor component")
	} else if makeSet.Subtype != "functor" {
		t.Errorf("MakeSet: expected subtype=functor, got %q", makeSet.Subtype)
	}

	makeMap := smlFind(ents, "MakeMap", "SCOPE.Component")
	if makeMap == nil {
		t.Error("expected MakeMap functor component")
	} else if makeMap.Subtype != "functor" {
		t.Errorf("MakeMap: expected subtype=functor, got %q", makeMap.Subtype)
	}
}

// TestSML_FunctionDiscovery — fun declarations → SCOPE.Operation(function).
func TestSML_FunctionDiscovery(t *testing.T) {
	src := `fun add x y = x + y

fun factorial 0 = 1
  | factorial n = n * factorial (n - 1)

fun greet name =
  print ("Hello, " ^ name ^ "\n")
`
	ents := runSML(t, src, "funcs.sml")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "sml" {
				t.Errorf("entity %q: expected Language=sml, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"add", "factorial", "greet"} {
		if !ops[want] {
			t.Errorf("expected function %q to be extracted, got ops=%v", want, ops)
		}
	}
}

// TestSML_ValDiscovery — val declarations → SCOPE.Operation(val).
func TestSML_ValDiscovery(t *testing.T) {
	src := `val pi = 3.14159

val greeting = "Hello, SML!"

val double = fn x => x * 2
`
	ents := runSML(t, src, "vals.sml")

	ops := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = e.Subtype
		}
	}

	for _, want := range []string{"pi", "greeting", "double"} {
		if ops[want] == "" {
			t.Errorf("expected val %q to be extracted, got ops=%v", want, ops)
		} else if ops[want] != "val" {
			t.Errorf("val %q: expected subtype=val, got %q", want, ops[want])
		}
	}
}

// TestSML_DatatypeDeclarations — datatype declarations → SCOPE.Component(datatype).
func TestSML_DatatypeDeclarations(t *testing.T) {
	src := `datatype color = Red | Green | Blue

datatype 'a tree = Leaf | Node of 'a * 'a tree * 'a tree

datatype 'a option = NONE | SOME of 'a
`
	ents := runSML(t, src, "types.sml")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"color", "tree", "option"} {
		if subtype, ok := comps[name]; !ok {
			t.Errorf("expected datatype %q to be extracted; got comps=%v", name, comps)
		} else if subtype != "datatype" {
			t.Errorf("datatype %q: expected subtype=datatype, got %q", name, subtype)
		}
	}
}

// TestSML_OpenImports — open statements emit IMPORTS edges.
func TestSML_OpenImports(t *testing.T) {
	src := `open List
open String
open TextIO

fun main () =
  let
    val lst = [1, 2, 3]
  in
    app print (map Int.toString lst)
  end
`
	ents := runSML(t, src, "main.sml")

	wantImports := map[string]bool{
		"List":   false,
		"String": false,
		"TextIO": false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
					if r.FromID != "main.sml" {
						t.Errorf("IMPORTS %q: expected FromID=main.sml, got %q", r.ToID, r.FromID)
					}
				}
			}
		}
	}
	for mod, found := range wantImports {
		if !found {
			t.Errorf("expected IMPORTS edge for %q", mod)
		}
	}
}

// TestSML_CallsEdges — function calls in bodies emit CALLS edges.
func TestSML_CallsEdges(t *testing.T) {
	src := `fun helper x = x + 1

fun process lst =
  List.map helper (List.filter (fn x => x > 0) lst)
`
	ents := runSML(t, src, "calls.sml")

	if !smlHasRel(ents, "process", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS process→helper")
	}
	if !smlHasRel(ents, "process", "SCOPE.Operation", "CALLS", "List.map") {
		t.Error("expected CALLS process→List.map")
	}
	if !smlHasRel(ents, "process", "SCOPE.Operation", "CALLS", "List.filter") {
		t.Error("expected CALLS process→List.filter")
	}
}

// TestSML_CallsDeduped — duplicate call targets are emitted once.
func TestSML_CallsDeduped(t *testing.T) {
	src := `fun worker x = x + 1

fun runner () =
  let
    val a = worker 1
    val b = worker 2
    val c = worker 3
  in
    Int.toString (a + b + c)
  end
`
	ents := runSML(t, src, "dedup.sml")
	count := 0
	for _, e := range ents {
		if e.Name == "runner" && e.Kind == "SCOPE.Operation" {
			for _, r := range e.Relationships {
				if r.Kind == "CALLS" && r.ToID == "worker" {
					count++
				}
			}
		}
	}
	if count != 1 {
		t.Errorf("expected 1 CALLS runner→worker (deduped), got %d", count)
	}
}

// TestSML_LanguageTagged — all relationships carry language=sml.
func TestSML_LanguageTagged(t *testing.T) {
	src := `open List

datatype 'a tree = Leaf | Node of 'a * 'a tree * 'a tree

fun size Leaf = 0
  | size (Node (_, l, r)) = 1 + size l + size r
`
	ents := runSML(t, src, "tagged.sml")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties.Get("language") != "sml" {
				t.Errorf("rel %s→%s missing language=sml (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// TestSML_NoFalsePositives — keywords are not emitted as CALLS edges.
func TestSML_NoFalsePositives(t *testing.T) {
	src := `fun process lst =
  if List.null lst then
    []
  else
    let
      val filtered = List.filter (fn x => x > 0) lst
    in
      List.map (fn x => x * 2) filtered
    end
`
	ents := runSML(t, src, "nofp.sml")

	smlKWCalls := map[string]bool{
		"if": true, "then": true, "else": true, "let": true, "in": true,
		"fn": true, "fun": true, "val": true, "end": true, "of": true,
	}

	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && smlKWCalls[r.ToID] {
				t.Errorf("false positive CALLS edge: %s→%s (keyword)", e.Name, r.ToID)
			}
		}
	}
}

// TestSML_SigFile — .sig files are parsed (signature declarations).
func TestSML_SigFile(t *testing.T) {
	src := `(* Collection signature *)
signature STACK = sig
  type 'a stack
  val empty  : 'a stack
  val push   : 'a -> 'a stack -> 'a stack
  val pop    : 'a stack -> ('a * 'a stack) option
  val peek   : 'a stack -> 'a option
  val isEmpty : 'a stack -> bool
end
`
	ents := runSML(t, src, "stack.sig")

	stack := smlFind(ents, "STACK", "SCOPE.Component")
	if stack == nil {
		t.Error("expected STACK signature component from .sig file")
	} else if stack.Subtype != "signature" {
		t.Errorf("STACK: expected subtype=signature, got %q", stack.Subtype)
	}
}

// TestSML_FunFile — .fun files are parsed (functor declarations).
func TestSML_FunFile(t *testing.T) {
	src := `functor BinarySearch (Elem : ORDERED) = struct
  fun search cmp lst key =
    case lst of
      [] => NONE
    | x :: rest =>
        (case cmp (key, x) of
          EQUAL => SOME x
        | LESS  => NONE
        | GREATER => search cmp rest key)
end
`
	ents := runSML(t, src, "bsearch.fun")

	bs := smlFind(ents, "BinarySearch", "SCOPE.Component")
	if bs == nil {
		t.Error("expected BinarySearch functor from .fun file")
	} else if bs.Subtype != "functor" {
		t.Errorf("BinarySearch: expected subtype=functor, got %q", bs.Subtype)
	}
}

// TestSML_FinanceFixture — synthetic financial-domain SML fixture for recall.
// Must achieve ≥80% entity recall (acceptance criterion).
func TestSML_FinanceFixture(t *testing.T) {
	src := `(* SML financial calculations — synthetic fixture *)
open List
open Real

(* ─── Domain types ─── *)
datatype currency = USD | EUR | GBP | JPY

datatype transaction =
    Deposit of real * currency
  | Withdrawal of real * currency
  | Transfer of real * currency * string

(* ─── Signatures ─── *)
signature ACCOUNT = sig
  type t
  val create     : string -> real -> t
  val balance    : t -> real
  val apply      : transaction -> t -> t
end

(* ─── Functor ─── *)
functor MakeAccount (Cur : sig val base : currency end) : ACCOUNT = struct
  type t = { id : string, balance : real }
  fun create id bal = { id = id, balance = bal }
  fun balance acct = #balance acct
  fun apply txn acct =
    case txn of
      Deposit (amt, _)    => { id = #id acct, balance = #balance acct + amt }
    | Withdrawal (amt, _) => { id = #id acct, balance = #balance acct - amt }
    | Transfer (amt, _, _) => { id = #id acct, balance = #balance acct - amt }
end

(* ─── Utility functions ─── *)
fun fxRate (USD, EUR) = 0.92
  | fxRate (USD, GBP) = 0.79
  | fxRate (USD, JPY) = 149.5
  | fxRate (EUR, GBP) = 0.86
  | fxRate (c1, c2)   = if c1 = c2 then 1.0 else 1.0 / fxRate (c2, c1)

fun convertAmount amt fromC toC =
  amt * fxRate (fromC, toC)

fun applyAll txns acct =
  foldl (fn (txn, a) => a) acct txns

fun totalDeposits txns =
  foldl (fn (Deposit (amt, _), acc) => acc + amt | (_, acc) => acc) 0.0 txns

fun totalWithdrawals txns =
  foldl (fn (Withdrawal (amt, _), acc) => acc + amt | (_, acc) => acc) 0.0 txns

fun netFlow txns =
  totalDeposits txns - totalWithdrawals txns

val zeroBalance = 0.0

val defaultCurrency = USD
`
	ents := runSML(t, src, "finance.sml")

	wantOps := []string{
		"fxRate", "convertAmount", "applyAll",
		"totalDeposits", "totalWithdrawals", "netFlow",
		"zeroBalance", "defaultCurrency",
	}
	wantComps := []string{
		"currency", "transaction", "ACCOUNT", "MakeAccount",
	}
	wantImports := []string{"List", "Real"}

	foundOps := make(map[string]bool)
	foundComps := make(map[string]bool)
	foundImports := make(map[string]bool)

	for _, e := range ents {
		switch e.Kind {
		case "SCOPE.Operation":
			foundOps[e.Name] = true
		case "SCOPE.Component":
			foundComps[e.Name] = true
			for _, r := range e.Relationships {
				if r.Kind == "IMPORTS" {
					foundImports[r.ToID] = true
				}
			}
		}
	}

	opHits := 0
	for _, name := range wantOps {
		if foundOps[name] {
			opHits++
		} else {
			t.Logf("missing operation: %s", name)
		}
	}
	compHits := 0
	for _, name := range wantComps {
		if foundComps[name] {
			compHits++
		} else {
			t.Logf("missing component: %s", name)
		}
	}
	importHits := 0
	for _, mod := range wantImports {
		if foundImports[mod] {
			importHits++
		} else {
			t.Logf("missing import: %s", mod)
		}
	}

	totalWant := len(wantOps) + len(wantComps) + len(wantImports)
	totalFound := opHits + compHits + importHits
	recall := float64(totalFound) / float64(totalWant) * 100

	t.Logf("Finance fixture recall: %d/%d (%.0f%%): ops=%d/%d comps=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		opHits, len(wantOps),
		compHits, len(wantComps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}
