// Package javascript — unit tests for issue #610: JSX function-component
// composition emitted as RENDERS relationships.
package javascript_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// parseTSX parses source with the TypeScript TSX grammar (JSX-enabled).
func parseTSX(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstsx.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parseTSX: %v", err)
	}
	return tree
}

// extractTSX runs the extractor on TSX source.
func extractTSX(t *testing.T, content []byte, tree ts.Tree) []types.EntityRecord {
	t.Helper()
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     "test.tsx",
		Content:  content,
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func findByNameTSX(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// TestJSXRenders_FunctionComponent — `<UserCard />` inside `function UserList()`
// must emit a RENDERS edge from UserList to UserCard.
func TestJSXRenders_FunctionComponent(t *testing.T) {
	src := []byte(`
import React from 'react';
import { UserCard } from './UserCard';

function UserList({ users }) {
  return (
    <div>
      {users.map(u => <UserCard key={u.id} user={u} />)}
    </div>
  );
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	ul := findByNameTSX(entities, "UserList")
	if ul == nil {
		t.Fatal("UserList entity not found")
	}
	found := false
	for _, r := range ul.Relationships {
		if r.Kind == "RENDERS" && r.ToID == "UserCard" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected RENDERS UserList→UserCard; relationships: %+v", ul.Relationships)
	}
}

// TestJSXRenders_ArrowComponent — arrow-function component also emits RENDERS.
func TestJSXRenders_ArrowComponent(t *testing.T) {
	src := []byte(`
import React from 'react';

const ProductCard = ({ product }) => (
  <div>{product.name}</div>
);

const ProductList = ({ products }) => (
  <section>
    {products.map(p => <ProductCard key={p.id} product={p} />)}
  </section>
);
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	pl := findByNameTSX(entities, "ProductList")
	if pl == nil {
		t.Fatal("ProductList entity not found")
	}
	found := false
	for _, r := range pl.Relationships {
		if r.Kind == "RENDERS" && r.ToID == "ProductCard" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected RENDERS ProductList→ProductCard; relationships: %+v", pl.Relationships)
	}
}

// TestJSXRenders_NoHTMLIntrinsics — lowercase HTML tags must NOT generate RENDERS.
func TestJSXRenders_NoHTMLIntrinsics(t *testing.T) {
	src := []byte(`
import React from 'react';

function Page() {
  return (
    <div>
      <span>text</span>
      <button onClick={() => {}}>click</button>
    </div>
  );
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	page := findByNameTSX(entities, "Page")
	if page == nil {
		t.Fatal("Page entity not found")
	}
	for _, r := range page.Relationships {
		if r.Kind != "RENDERS" {
			continue
		}
		tag := r.ToID
		if len(tag) > 0 && tag[0] >= 'a' && tag[0] <= 'z' {
			t.Errorf("HTML intrinsic %q should NOT generate RENDERS edge", tag)
		}
	}
}

// TestJSXRenders_NoSelfRender — self-reference must not produce a RENDERS edge.
func TestJSXRenders_NoSelfRender(t *testing.T) {
	src := []byte(`
import React from 'react';

function Tree({ node }) {
  if (!node.children) return null;
  return (
    <div>
      {node.children.map(c => <Tree key={c.id} node={c} />)}
    </div>
  );
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	treeEnt := findByNameTSX(entities, "Tree")
	if treeEnt == nil {
		t.Fatal("Tree entity not found")
	}
	for _, r := range treeEnt.Relationships {
		if r.Kind == "RENDERS" && r.ToID == "Tree" {
			t.Errorf("self-RENDERS should be filtered out")
		}
	}
}

// TestJSXRenders_DeduplicatesMultipleUses — same child used N times produces
// exactly ONE RENDERS edge.
func TestJSXRenders_DeduplicatesMultipleUses(t *testing.T) {
	src := []byte(`
import React from 'react';

function Dashboard() {
  return (
    <div>
      <Widget id="a" />
      <Widget id="b" />
      <Widget id="c" />
    </div>
  );
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	dash := findByNameTSX(entities, "Dashboard")
	if dash == nil {
		t.Fatal("Dashboard entity not found")
	}
	count := 0
	for _, r := range dash.Relationships {
		if r.Kind == "RENDERS" && r.ToID == "Widget" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 deduplicated RENDERS Dashboard→Widget, got %d", count)
	}
}

// TestJSXRenders_LowercaseFunctionNotEmitted — a lowercase function name
// (non-component) should NOT emit RENDERS even if it contains JSX.
func TestJSXRenders_LowercaseFunctionNotEmitted(t *testing.T) {
	src := []byte(`
import React from 'react';

function renderItem(item) {
  return <ItemCard item={item} />;
}
`)
	tree := parseTSX(t, src)
	entities := extractTSX(t, src, tree)

	ri := findByNameTSX(entities, "renderItem")
	if ri == nil {
		t.Fatal("renderItem entity not found")
	}
	for _, r := range ri.Relationships {
		if r.Kind == "RENDERS" {
			t.Errorf("lowercase function renderItem should NOT emit RENDERS; got %+v", r)
		}
	}
}
