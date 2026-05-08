package react_props

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

// runExtract is a tiny harness wrapping Extract with an in-memory FileInput.
func runExtract(t *testing.T, path, lang, src string) []extractedResult {
	t.Helper()
	e := &Extractor{}
	out, err := e.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: lang,
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	var res []extractedResult
	for _, r := range out {
		res = append(res, extractedResult{
			name: r.Name, kind: r.Kind,
			subtype: r.Subtype,
			props:   r.Properties["props"],
			rels:    r.Relationships,
		})
	}
	return res
}

type extractedResult struct {
	name    string
	kind    string
	subtype string
	props   string
	rels    []types.RelationshipRecord
}

// relsByKind returns the count of relationships of the given kind across all
// entities in the result slice.
func relsByKind(res []extractedResult, kind string) int {
	n := 0
	for _, r := range res {
		for _, rel := range r.rels {
			if rel.Kind == kind {
				n++
			}
		}
	}
	return n
}

func findEntity(res []extractedResult, name, kind string) *extractedResult {
	for i := range res {
		if res[i].name == name && res[i].kind == kind {
			return &res[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Positive cases
// ---------------------------------------------------------------------------

func TestExtract_FunctionComponent_InterfaceProps_HasPropsEdge(t *testing.T) {
	src := `
import React from 'react';

export interface InspectorDeviceCardProps {
  status: string;
  date: Date;
  inspector: string;
  onPress?: () => void;
}

export function InspectorDeviceCard(props: InspectorDeviceCardProps) {
  return <div>{props.status}</div>;
}
`
	res := runExtract(t, "src/InspectorDeviceCard.tsx", "typescript", src)
	comp := findEntity(res, "InspectorDeviceCard", "SCOPE.Operation")
	if comp == nil {
		t.Fatalf("InspectorDeviceCard not extracted; got %+v", res)
	}
	if got := comp.props; !strings.Contains(got, "status") || !strings.Contains(got, "onPress") {
		t.Errorf("props property missing fields: %q", got)
	}
	// HAS_PROPS edge present.
	if n := relsByKind(res, RelHasProps); n != 1 {
		t.Errorf("HAS_PROPS count = %d want 1", n)
	}
	// PropsInterface entity emitted.
	if findEntity(res, "InspectorDeviceCardProps", "SCOPE.Schema") == nil {
		t.Errorf("PropsInterface entity not emitted")
	}
}

func TestExtract_FunctionComponent_TypeAliasProps(t *testing.T) {
	src := `
import React from 'react';

export type FooProps = {
  title: string;
  count: number;
};

export function Foo(p: FooProps) {
  return <span>{p.title}</span>;
}
`
	res := runExtract(t, "src/Foo.tsx", "typescript", src)
	if relsByKind(res, RelHasProps) != 1 {
		t.Errorf("expected HAS_PROPS edge from type alias")
	}
	comp := findEntity(res, "Foo", "SCOPE.Operation")
	if comp == nil || !strings.Contains(comp.props, "title") || !strings.Contains(comp.props, "count") {
		t.Errorf("type-alias prop fields not recovered: %+v", comp)
	}
}

func TestExtract_ArrowComponent_InlineDestructuringProps(t *testing.T) {
	src := `
import React from 'react';

export const Greeting = ({ name, count }) => {
  return <div>{name} {count}</div>;
};
`
	res := runExtract(t, "src/Greeting.jsx", "javascript", src)
	comp := findEntity(res, "Greeting", "SCOPE.Operation")
	if comp == nil {
		t.Fatalf("Greeting not extracted")
	}
	if !strings.Contains(comp.props, "name") || !strings.Contains(comp.props, "count") {
		t.Errorf("destructured props not captured: %q", comp.props)
	}
	// No interface → no HAS_PROPS edge expected.
	if relsByKind(res, RelHasProps) != 0 {
		t.Errorf("unexpected HAS_PROPS edge on inline destructuring")
	}
}

func TestExtract_ArrowComponent_DestructuredWithTypeAnnotation(t *testing.T) {
	src := `
import React from 'react';

interface CardProps {
  title: string;
  body: string;
}

export const Card = ({ title, body }: CardProps) => {
  return <section>{title}</section>;
};
`
	res := runExtract(t, "src/Card.tsx", "typescript", src)
	if relsByKind(res, RelHasProps) != 1 {
		t.Errorf("expected HAS_PROPS edge for annotated destructuring")
	}
	if findEntity(res, "CardProps", "SCOPE.Schema") == nil {
		t.Errorf("PropsInterface CardProps not emitted")
	}
}

func TestExtract_RendersChildComponents(t *testing.T) {
	src := `
import React from 'react';

interface InspectorDeviceCardProps { status: string; }
export function InspectorDeviceCard(props: InspectorDeviceCardProps) {
  return <div>{props.status}</div>;
}

export function DevicesTab() {
  return (
    <div>
      <InspectorDeviceCard status="ok" />
      <InspectorDeviceCard status="fail" />
      <SomeOther />
    </div>
  );
}
`
	res := runExtract(t, "src/DevicesTab.tsx", "typescript", src)
	if relsByKind(res, RelRenders) < 2 {
		t.Errorf("expected at least 2 RENDERS (InspectorDeviceCard, SomeOther), got %d", relsByKind(res, RelRenders))
	}
	// DevicesTab is the renderer — verify its entity carries both children.
	dev := findEntity(res, "DevicesTab", "SCOPE.Operation")
	if dev == nil {
		t.Fatalf("DevicesTab not extracted")
	}
	seen := map[string]bool{}
	for _, r := range dev.rels {
		if r.Kind == RelRenders {
			seen[r.Properties["child_name"]] = true
		}
	}
	if !seen["InspectorDeviceCard"] || !seen["SomeOther"] {
		t.Errorf("DevicesTab RENDERS edges missing: %v", seen)
	}
}

func TestExtract_RendersFiltersHTMLElements(t *testing.T) {
	src := `
import React from 'react';
export function Page() {
  return (
    <div>
      <span>hello</span>
      <button onClick={() => {}}>x</button>
      <CustomWidget />
    </div>
  );
}
`
	res := runExtract(t, "src/Page.tsx", "typescript", src)
	// Only CustomWidget should emit a RENDERS edge.
	page := findEntity(res, "Page", "SCOPE.Operation")
	if page == nil {
		t.Fatalf("Page not extracted")
	}
	n := 0
	for _, r := range page.rels {
		if r.Kind == RelRenders {
			n++
			if r.Properties["child_name"] != "CustomWidget" {
				t.Errorf("unexpected child %q — HTML tag leak", r.Properties["child_name"])
			}
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 RENDERS on Page, got %d", n)
	}
}

func TestExtract_UsesHookEdges(t *testing.T) {
	src := `
import React, { useState, useEffect } from 'react';

export function DevicesTab() {
  const [x, setX] = useState(0);
  useEffect(() => {}, []);
  useInspectorDevicesTab();
  return <div>{x}</div>;
}
`
	res := runExtract(t, "src/DevicesTab.tsx", "typescript", src)
	dev := findEntity(res, "DevicesTab", "SCOPE.Operation")
	if dev == nil {
		t.Fatalf("DevicesTab not extracted")
	}
	hooks := map[string]bool{}
	for _, r := range dev.rels {
		if r.Kind == RelUsesHook {
			hooks[r.Properties["hook_name"]] = true
		}
	}
	for _, want := range []string{"useState", "useEffect", "useInspectorDevicesTab"} {
		if !hooks[want] {
			t.Errorf("missing USES_HOOK edge for %s", want)
		}
	}
}

func TestExtract_CombinedScenario(t *testing.T) {
	// The canonical ticket example.
	src := `
import React from 'react';
import { useInspectorDevicesTab } from './hooks';

export interface InspectorDeviceCardProps {
  status: string;
  date: Date;
  inspector: string;
  onPress: () => void;
}

export function InspectorDeviceCard(props: InspectorDeviceCardProps) {
  return <div onClick={props.onPress}>{props.status}</div>;
}

export function DevicesTab() {
  const data = useInspectorDevicesTab();
  return (
    <div>
      <InspectorDeviceCard
        status={data.status}
        date={data.date}
        inspector={data.inspector}
        onPress={() => {}}
      />
    </div>
  );
}
`
	res := runExtract(t, "src/inspector/DevicesTab.tsx", "typescript", src)

	// Three entity kinds we expect: two components + one props interface.
	if findEntity(res, "InspectorDeviceCard", "SCOPE.Operation") == nil {
		t.Error("missing InspectorDeviceCard component entity")
	}
	if findEntity(res, "DevicesTab", "SCOPE.Operation") == nil {
		t.Error("missing DevicesTab component entity")
	}
	if findEntity(res, "InspectorDeviceCardProps", "SCOPE.Schema") == nil {
		t.Error("missing InspectorDeviceCardProps interface entity")
	}

	// Three relationship kinds we expect.
	if relsByKind(res, RelHasProps) != 1 {
		t.Errorf("HAS_PROPS count %d want 1", relsByKind(res, RelHasProps))
	}
	if relsByKind(res, RelRenders) != 1 {
		t.Errorf("RENDERS count %d want 1", relsByKind(res, RelRenders))
	}
	if relsByKind(res, RelUsesHook) != 1 {
		t.Errorf("USES_HOOK count %d want 1", relsByKind(res, RelUsesHook))
	}

	// Verify props CSV on the component.
	ic := findEntity(res, "InspectorDeviceCard", "SCOPE.Operation")
	for _, field := range []string{"status", "date", "inspector", "onPress"} {
		if !strings.Contains(ic.props, field) {
			t.Errorf("InspectorDeviceCard props missing field %q: %q", field, ic.props)
		}
	}
}

// ---------------------------------------------------------------------------
// Negative / short-circuit cases
// ---------------------------------------------------------------------------

func TestExtract_NonReactFile_IsSkipped(t *testing.T) {
	src := `function Handler() { return null; }`
	res := runExtract(t, "src/handler.ts", "typescript", src)
	if len(res) != 0 {
		t.Errorf("expected 0 entities from .ts file, got %d", len(res))
	}
}

func TestExtract_NoReactImport_IsSkipped(t *testing.T) {
	src := `
export function Foo(props: FooProps) {
  return <div/>;
}
`
	res := runExtract(t, "src/Foo.tsx", "typescript", src)
	if len(res) != 0 {
		t.Errorf("expected 0 entities without react import, got %d", len(res))
	}
}

func TestExtract_UtilityFunction_IsNotComponent(t *testing.T) {
	src := `
import React from 'react';

export function ParseJSON(input: string): any {
  return JSON.parse(input);
}
`
	res := runExtract(t, "src/util.tsx", "typescript", src)
	if len(res) != 0 {
		t.Errorf("utility function should not match, got %+v", res)
	}
}

func TestExtract_ClassComponent_IsNotEmitted(t *testing.T) {
	src := `
import React from 'react';

export class Legacy extends React.Component<{}> {
  render() { return <div/>; }
}
`
	res := runExtract(t, "src/Legacy.tsx", "typescript", src)
	// Class components are out of scope for MX-1062 (the story is explicitly
	// functional / props interface scoped). We must NOT emit anything.
	for _, r := range res {
		if r.name == "Legacy" {
			t.Errorf("class component was emitted: %+v", r)
		}
	}
}

func TestExtract_EmptyFile(t *testing.T) {
	res := runExtract(t, "src/Empty.tsx", "typescript", "")
	if len(res) != 0 {
		t.Errorf("empty file produced entities: %+v", res)
	}
}

func TestExtract_MalformedSource_DoesNotPanic(t *testing.T) {
	src := `
import React from 'react';
export function Broken(props: MyProps {  // missing paren
  return <div>
`
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on malformed source: %v", r)
		}
	}()
	_ = runExtract(t, "src/Broken.tsx", "typescript", src)
}

// ---------------------------------------------------------------------------
// Property-cap / truncation tests
// ---------------------------------------------------------------------------

func TestTruncateProps_UnderCap(t *testing.T) {
	got := truncateProps("a, b, c")
	if got != "a, b, c" {
		t.Errorf("truncateProps mutated short string: %q", got)
	}
}

func TestTruncateProps_OverCap(t *testing.T) {
	// Build a 600-char string of "x, ".
	var b strings.Builder
	for i := 0; i < 250; i++ {
		b.WriteString("abc")
	}
	got := truncateProps(b.String())
	if len(got) > propsMaxLen {
		t.Errorf("truncateProps exceeded cap: len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncateProps missing ellipsis: %q", got[len(got)-5:])
	}
}

// ---------------------------------------------------------------------------
// splitDestructured unit tests — covers default values, renames, rest.
// ---------------------------------------------------------------------------

func TestSplitDestructured_BasicList(t *testing.T) {
	got := splitDestructured("a, b, c")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("basic list: %+v", got)
	}
}

func TestSplitDestructured_DefaultValues(t *testing.T) {
	got := splitDestructured("a = 1, b = 'x', c")
	if len(got) != 3 {
		t.Errorf("default values dropped: %+v", got)
	}
}

func TestSplitDestructured_Renames(t *testing.T) {
	got := splitDestructured("foo: bar, baz: qux")
	// Renames: local binding is RHS.
	if len(got) != 2 || got[0] != "bar" || got[1] != "qux" {
		t.Errorf("rename handling wrong: %+v", got)
	}
}

func TestSplitDestructured_Rest(t *testing.T) {
	got := splitDestructured("a, ...rest")
	if len(got) != 2 || got[1] != "rest" {
		t.Errorf("rest handling wrong: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Registration sanity
// ---------------------------------------------------------------------------

func TestExtractorRegistration(t *testing.T) {
	e, ok := extractor.Get("_cross_react_props")
	if !ok {
		t.Fatalf("_cross_react_props not registered")
	}
	if e.Language() != "_cross_react_props" {
		t.Errorf("Language() = %q, want _cross_react_props", e.Language())
	}
}

func TestLanguage_ExactMatch(t *testing.T) {
	if (&Extractor{}).Language() != "_cross_react_props" {
		t.Error("Language() wrong")
	}
}
