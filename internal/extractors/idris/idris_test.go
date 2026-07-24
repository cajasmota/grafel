package idris_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/idris"
	"github.com/cajasmota/grafel/internal/types"
)

// runIdris runs the extractor on raw source and returns entity records.
func runIdris(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("idris")
	if !ok {
		t.Fatal("idris extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "idris",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func idrisFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func idrisHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
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

// TestIdris_Registered verifies the extractor is in the registry.
func TestIdris_Registered(t *testing.T) {
	_, ok := extractor.Get("idris")
	if !ok {
		t.Fatal("idris extractor not registered")
	}
}

// TestIdris_EmptyInput returns zero entities for empty content.
func TestIdris_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("idris")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.idr",
		Content:  []byte{},
		Language: "idris",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestIdris_ModuleDeclaration — module declaration extracted as SCOPE.Component(module).
func TestIdris_ModuleDeclaration(t *testing.T) {
	src := `module Main

import Data.List

main : IO ()
main = putStrLn "Hello, Idris!"
`
	ents := runIdris(t, src, "Main.idr")

	mod := idrisFind(ents, "Main", "SCOPE.Component")
	if mod == nil {
		t.Fatal("expected Main module component")
	}
	if mod.Subtype != "module" {
		t.Errorf("expected subtype=module, got %q", mod.Subtype)
	}
}

// TestIdris_FunctionDiscovery — top-level functions extracted as SCOPE.Operation.
func TestIdris_FunctionDiscovery(t *testing.T) {
	src := `module Lib

greet : String -> String
greet name = "Hello, " ++ name

add : Nat -> Nat -> Nat
add x y = x + y

factorial : Nat -> Nat
factorial Z = 1
factorial (S n) = (S n) * factorial n
`
	ents := runIdris(t, src, "Lib.idr")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "idris" {
				t.Errorf("entity %q: expected Language=idris, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"greet", "add", "factorial"} {
		if !ops[want] {
			t.Errorf("expected function %q to be extracted, got ops=%v", want, ops)
		}
	}
}

// TestIdris_DataTypeDiscovery — data declarations extracted as SCOPE.Component(data).
func TestIdris_DataTypeDiscovery(t *testing.T) {
	src := `module Types

data Shape = Circle Double | Rectangle Double Double

data Maybe a = Nothing | Just a

data Vect : Nat -> Type -> Type where
  Nil  : Vect 0 a
  (::) : a -> Vect n a -> Vect (S n) a
`
	ents := runIdris(t, src, "Types.idr")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	checks := map[string]string{
		"Shape": "data",
		"Maybe": "data",
		"Vect":  "data",
	}
	for name, wantSubtype := range checks {
		got, ok := comps[name]
		if !ok {
			t.Errorf("expected data type %q to be extracted; comps=%v", name, comps)
		} else if got != wantSubtype {
			t.Errorf("type %q: expected subtype=%q, got %q", name, wantSubtype, got)
		}
	}
}

// TestIdris_RecordDiscovery — record declarations extracted as SCOPE.Component(record).
func TestIdris_RecordDiscovery(t *testing.T) {
	src := `module Records

record Person where
  constructor MkPerson
  name : String
  age  : Nat

record Pair a b where
  constructor MkPair
  fst : a
  snd : b
`
	ents := runIdris(t, src, "Records.idr")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"Person", "Pair"} {
		if sub, ok := comps[name]; !ok {
			t.Errorf("expected record %q to be extracted", name)
		} else if sub != "record" {
			t.Errorf("record %q: expected subtype=record, got %q", name, sub)
		}
	}
}

// TestIdris_InterfaceDiscovery — interface declarations extracted as SCOPE.Component(interface).
func TestIdris_InterfaceDiscovery(t *testing.T) {
	src := `module Interfaces

interface Container f where
  empty : f a
  insert : a -> f a -> f a

interface (Eq a) => Hashable a where
  hash : a -> Int
`
	ents := runIdris(t, src, "Interfaces.idr")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"Container", "Hashable"} {
		if sub, ok := comps[name]; !ok {
			t.Errorf("expected interface %q to be extracted; comps=%v", name, comps)
		} else if sub != "interface" {
			t.Errorf("interface %q: expected subtype=interface, got %q", name, sub)
		}
	}
}

// TestIdris_ImplementationImplementsEdge — implementation declarations produce IMPLEMENTS edges.
func TestIdris_ImplementationImplementsEdge(t *testing.T) {
	src := `module Impls

data Color = Red | Green | Blue

implementation Show Color where
  show Red   = "Red"
  show Green = "Green"
  show Blue  = "Blue"

implementation Eq Color where
  (==) Red   Red   = True
  (==) Green Green = True
  (==) Blue  Blue  = True
  (==) _     _     = False
`
	ents := runIdris(t, src, "Impls.idr")

	var foundShow, foundEq bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "implementation" {
			for _, r := range e.Relationships {
				if r.Kind == "IMPLEMENTS" {
					if r.ToID == "Show" {
						foundShow = true
					}
					if r.ToID == "Eq" {
						foundEq = true
					}
				}
			}
		}
	}

	if !foundShow {
		t.Error("expected IMPLEMENTS edge for Show implementation")
	}
	if !foundEq {
		t.Error("expected IMPLEMENTS edge for Eq implementation")
	}
}

// TestIdris_ImportEdges — import statements emit IMPORTS edges.
func TestIdris_ImportEdges(t *testing.T) {
	src := `module Main

import Data.List
import Data.Vect
import Data.Maybe
import System.File

main : IO ()
main = putStrLn "done"
`
	ents := runIdris(t, src, "main.idr")

	wantImports := map[string]bool{
		"Data.List":   false,
		"Data.Vect":   false,
		"Data.Maybe":  false,
		"System.File": false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
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

// TestIdris_LanguageTagged — all relationships carry language=idris.
func TestIdris_LanguageTagged(t *testing.T) {
	src := `module Tagged

import Data.Vect

data Node = Node Nat

process : Node -> String
process (Node n) = show n
`
	ents := runIdris(t, src, "tagged.idr")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties.Get("language") != "idris" {
				t.Errorf("rel %s→%s missing language=idris (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// TestIdris_DependentTypeFixture — synthetic dependent-type fixture testing Idris-specific features.
func TestIdris_DependentTypeFixture(t *testing.T) {
	src := `module Server

import Data.Vect
import Data.List
import Data.Maybe
import System.File

-- Dependent-type sized vector operations
data BinTree : Type -> Type where
  Leaf : BinTree a
  Node : BinTree a -> a -> BinTree a -> BinTree a

record Config where
  constructor MkConfig
  host : String
  port : Nat
  maxConn : Nat

data Request = GET String | POST String String | DELETE String

data Response : Type where
  Ok     : String -> Response
  NotFound : Response
  Error  : String -> Response

-- Proof that insert preserves size
insertPreservesLen : (x : a) -> (xs : List a) -> length (x :: xs) = S (length xs)
insertPreservesLen x xs = Refl

-- Vector head (provably safe — no runtime check needed)
safeHead : Vect (S n) a -> a
safeHead (x :: _) = x

-- Routing function
route : Request -> Response
route (GET "/")     = Ok "index"
route (GET path)    = Ok path
route (POST path _) = Ok ("posted to " ++ path)
route (DELETE _)    = Ok "deleted"
route _             = NotFound

-- Server initialization
initServer : Config -> IO ()
initServer cfg = do
  putStrLn ("Starting on port " ++ show (port cfg))
  pure ()

-- Application entry point
main : IO ()
main = do
  let cfg = MkConfig "localhost" 8080 100
  initServer cfg
`
	ents := runIdris(t, src, "Server.idr")

	wantOps := []string{"insertPreservesLen", "safeHead", "route", "initServer", "main"}
	wantComps := []string{"Server", "BinTree", "Config", "Request", "Response"}
	wantImports := []string{"Data.Vect", "Data.List", "Data.Maybe", "System.File"}

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

	t.Logf("Idris dependent-type fixture recall: %d/%d (%.0f%%): ops=%d/%d comps=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		opHits, len(wantOps),
		compHits, len(wantComps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}
