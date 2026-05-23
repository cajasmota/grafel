package haskell_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/haskell"
	"github.com/cajasmota/archigraph/internal/types"
)

// runHaskell runs the extractor on raw source and returns entity records.
func runHaskell(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("haskell")
	if !ok {
		t.Fatal("haskell extractor not registered")
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "haskell",
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func hsFind(ents []types.EntityRecord, name, kind string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name && ents[i].Kind == kind {
			return &ents[i]
		}
	}
	return nil
}

func hsHasRel(ents []types.EntityRecord, name, kind, edgeKind, toID string) bool {
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

// TestHaskell_Registered verifies the extractor is in the registry.
func TestHaskell_Registered(t *testing.T) {
	_, ok := extractor.Get("haskell")
	if !ok {
		t.Fatal("haskell extractor not registered")
	}
}

// TestHaskell_EmptyInput returns zero entities for empty content.
func TestHaskell_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("haskell")
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.hs",
		Content:  []byte{},
		Language: "haskell",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("expected 0 entities, got %d", len(ents))
	}
}

// TestHaskell_ModuleDeclaration — module declaration extracted as SCOPE.Component(module).
func TestHaskell_ModuleDeclaration(t *testing.T) {
	src := `module Main where

main :: IO ()
main = putStrLn "Hello, World!"
`
	ents := runHaskell(t, src, "Main.hs")

	mod := hsFind(ents, "Main", "SCOPE.Component")
	if mod == nil {
		t.Fatal("expected Main module component")
	}
	if mod.Subtype != "module" {
		t.Errorf("expected subtype=module, got %q", mod.Subtype)
	}
}

// TestHaskell_FunctionDiscovery — top-level functions extracted as SCOPE.Operation.
func TestHaskell_FunctionDiscovery(t *testing.T) {
	src := `module Lib where

greet :: String -> String
greet name = "Hello, " ++ name

add :: Int -> Int -> Int
add x y = x + y

helper :: Bool -> String
helper True  = "yes"
helper False = "no"
`
	ents := runHaskell(t, src, "Lib.hs")

	ops := make(map[string]bool)
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			ops[e.Name] = true
			if e.Language != "haskell" {
				t.Errorf("entity %q: expected Language=haskell, got %q", e.Name, e.Language)
			}
		}
	}

	for _, want := range []string{"greet", "add", "helper"} {
		if !ops[want] {
			t.Errorf("expected function %q to be extracted, got ops=%v", want, ops)
		}
	}
}

// TestHaskell_DataTypeDiscovery — data declarations extracted as SCOPE.Component(data).
func TestHaskell_DataTypeDiscovery(t *testing.T) {
	src := `module Types where

data Shape
  = Circle Double
  | Rectangle Double Double
  | Triangle Double Double Double

data Maybe a = Nothing | Just a

data Person = Person
  { personName :: String
  , personAge  :: Int
  }
`
	ents := runHaskell(t, src, "Types.hs")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	checks := map[string]string{
		"Shape":  "data",
		"Maybe":  "data",
		"Person": "data",
	}
	for name, wantSubtype := range checks {
		got, ok := comps[name]
		if !ok {
			t.Errorf("expected data type %q to be extracted; got comps=%v", name, comps)
		} else if got != wantSubtype {
			t.Errorf("type %q: expected subtype=%q, got %q", name, wantSubtype, got)
		}
	}
}

// TestHaskell_NewtypeDiscovery — newtype declarations extracted as SCOPE.Component(newtype).
func TestHaskell_NewtypeDiscovery(t *testing.T) {
	src := `module Wrappers where

newtype UserId = UserId Int

newtype Name = Name { unName :: String }
`
	ents := runHaskell(t, src, "Wrappers.hs")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"UserId", "Name"} {
		if subtype, ok := comps[name]; !ok {
			t.Errorf("expected newtype %q to be extracted", name)
		} else if subtype != "newtype" {
			t.Errorf("newtype %q: expected subtype=newtype, got %q", name, subtype)
		}
	}
}

// TestHaskell_TypeclassDiscovery — class declarations extracted as SCOPE.Component(typeclass).
func TestHaskell_TypeclassDiscovery(t *testing.T) {
	src := `module Classes where

class Container f where
  empty :: f a
  insert :: a -> f a -> f a

class (Eq a) => Hashable a where
  hash :: a -> Int
`
	ents := runHaskell(t, src, "Classes.hs")

	comps := make(map[string]string)
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" {
			comps[e.Name] = e.Subtype
		}
	}

	for _, name := range []string{"Container", "Hashable"} {
		if subtype, ok := comps[name]; !ok {
			t.Errorf("expected typeclass %q to be extracted; comps=%v", name, comps)
		} else if subtype != "typeclass" {
			t.Errorf("typeclass %q: expected subtype=typeclass, got %q", name, subtype)
		}
	}
}

// TestHaskell_InstanceImplementsEdge — instance declarations produce IMPLEMENTS edges.
func TestHaskell_InstanceImplementsEdge(t *testing.T) {
	src := `module Instances where

data Color = Red | Green | Blue

instance Show Color where
  show Red   = "Red"
  show Green = "Green"
  show Blue  = "Blue"

instance Eq Color where
  Red   == Red   = True
  Green == Green = True
  Blue  == Blue  = True
  _     == _     = False
`
	ents := runHaskell(t, src, "Instances.hs")

	// Should have instances with IMPLEMENTS edges to Show and Eq.
	var foundShow, foundEq bool
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "instance" {
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
		t.Error("expected IMPLEMENTS edge for Show instance")
	}
	if !foundEq {
		t.Error("expected IMPLEMENTS edge for Eq instance")
	}
}

// TestHaskell_ImportEdges — import statements emit IMPORTS edges.
func TestHaskell_ImportEdges(t *testing.T) {
	src := `module Main where

import Data.Map (Map, lookup, insert)
import qualified Data.List as List
import Control.Monad (forM_, when)
import Text.Printf (printf)

main :: IO ()
main = putStrLn "done"
`
	ents := runHaskell(t, src, "main.hs")

	wantImports := map[string]bool{
		"Data.Map":      false,
		"Data.List":     false,
		"Control.Monad": false,
		"Text.Printf":   false,
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" {
				if _, ok := wantImports[r.ToID]; ok {
					wantImports[r.ToID] = true
					if r.FromID != "main.hs" {
						t.Errorf("IMPORTS %q: expected FromID=main.hs, got %q", r.ToID, r.FromID)
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

// TestHaskell_CallsEdges — function invocations emit CALLS edges.
func TestHaskell_CallsEdges(t *testing.T) {
	src := `module App where

helper :: Int -> Int
helper x = x * 2

process :: [Int] -> [Int]
process xs = map helper (filter even xs)
`
	ents := runHaskell(t, src, "app.hs")

	// process should call helper, map, filter, even
	if !hsHasRel(ents, "process", "SCOPE.Operation", "CALLS", "helper") {
		t.Error("expected CALLS process→helper")
	}
	if !hsHasRel(ents, "process", "SCOPE.Operation", "CALLS", "map") {
		t.Error("expected CALLS process→map")
	}
	if !hsHasRel(ents, "process", "SCOPE.Operation", "CALLS", "filter") {
		t.Error("expected CALLS process→filter")
	}
}

// TestHaskell_CallsDeduped — duplicate call targets are emitted once.
func TestHaskell_CallsDeduped(t *testing.T) {
	src := `module Dedup where

worker :: Int -> Int
worker x = x + 1

runner :: IO ()
runner = do
  let a = worker 1
  let b = worker 2
  let c = worker 3
  print a
  print b
  print c
`
	ents := runHaskell(t, src, "dedup.hs")
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

// TestHaskell_LanguageTagged — all relationships carry language=haskell.
func TestHaskell_LanguageTagged(t *testing.T) {
	src := `module Tagged where

import Data.Map (Map)

data Node = Node Int

process :: Node -> String
process (Node n) = show n
`
	ents := runHaskell(t, src, "tagged.hs")
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Properties == nil || r.Properties["language"] != "haskell" {
				t.Errorf("rel %s→%s missing language=haskell (got %v)", r.Kind, r.ToID, r.Properties)
			}
		}
	}
}

// TestHaskell_WarpMiniFixture — synthetic Warp/Servant web server fixture for recall.
func TestHaskell_WarpMiniFixture(t *testing.T) {
	src := `module Server where

import Network.Wai (Application, Request, Response, responseLBS)
import Network.Wai.Handler.Warp (run)
import Network.HTTP.Types (status200, status404)
import Data.Aeson (encode, decode, ToJSON, FromJSON)
import Control.Monad.IO.Class (liftIO)
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map

-- | User entity
data User = User
  { userId   :: Int
  , userName :: String
  , userEmail :: String
  } deriving (Show, Eq)

-- | In-memory store type alias
type UserStore = Map Int User

-- | Application configuration
data AppConfig = AppConfig
  { configPort :: Int
  , configHost :: String
  }

-- | Create a new user store
newUserStore :: UserStore
newUserStore = Map.empty

-- | Look up a user by ID
getUser :: UserStore -> Int -> Maybe User
getUser store uid = Map.lookup uid store

-- | Insert a user into the store
putUser :: UserStore -> User -> UserStore
putUser store user = Map.insert (userId user) user store

-- | Build the WAI application
mkApp :: UserStore -> Application
mkApp store req respond = do
  let response = handleRequest store req
  respond response

-- | Handle an incoming request
handleRequest :: UserStore -> Request -> Response
handleRequest store _req = responseLBS status200 [] (encode store)

-- | Start the HTTP server
startServer :: AppConfig -> UserStore -> IO ()
startServer cfg store = do
  let port = configPort cfg
  putStrLn ("Starting server on port " ++ show port)
  run port (mkApp store)

-- | Application entry point
main :: IO ()
main = do
  let cfg = AppConfig { configPort = 8080, configHost = "localhost" }
  let store = newUserStore
  startServer cfg store
`
	ents := runHaskell(t, src, "Server.hs")

	wantOps := []string{"newUserStore", "getUser", "putUser", "mkApp", "handleRequest", "startServer", "main"}
	wantComps := []string{"User", "AppConfig", "UserStore"}
	wantImports := []string{"Network.Wai", "Network.Wai.Handler.Warp", "Data.Aeson", "Data.Map.Strict"}

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

	t.Logf("Warp-mini fixture recall: %d/%d (%.0f%%): ops=%d/%d comps=%d/%d imports=%d/%d",
		totalFound, totalWant, recall,
		opHits, len(wantOps),
		compHits, len(wantComps),
		importHits, len(wantImports))

	if recall < 80.0 {
		t.Errorf("entity recall %.0f%% below 80%% threshold (%d/%d found)",
			recall, totalFound, totalWant)
	}
}
