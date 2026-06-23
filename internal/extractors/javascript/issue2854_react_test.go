// Package javascript — issue #2854 React Structure-group proving tests.
//
// Flips React component_extraction (partial→full) and hook_recognition
// (partial→full) by proving:
//   - PascalCase function/arrow components emit SCOPE.Component (via the file
//     entity) / SCOPE.Operation with RENDERS composition edges (#610),
//   - custom `useXxx` definitions are tagged subtype="react_hook",
//   - components and custom hooks emit USES_HOOK edges to the hooks they call.
package javascript_test

import (
	"context"
	"testing"

	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractReact(t *testing.T, path string, content []byte) []types.EntityRecord {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstsx.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  content,
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

func TestIssue2854_ReactHookRecognition(t *testing.T) {
	src := []byte(`import { useState, useEffect } from 'react';

export function useCounter(initial: number) {
  const [count, setCount] = useState(initial);
  useEffect(() => {}, [count]);
  const value = useWindowSize();
  return { count, setCount, value };
}

export const useWindowSize = () => {
  const [size, setSize] = useState(0);
  return size;
};

export function ProfileCard() {
  const { count } = useCounter(0);
  const size = useWindowSize();
  return <Avatar count={count} size={size} />;
}`)

	ents := extractReact(t, "components.tsx", src)

	// hook_recognition: custom hook definitions tagged react_hook.
	useCounter := findByName(ents, "useCounter")
	if useCounter == nil || useCounter.Subtype != "react_hook" {
		t.Fatalf("useCounter subtype = %v, want react_hook; %s", subtypeOf(useCounter), dumpKinds(ents))
	}
	useWindowSize := findByName(ents, "useWindowSize")
	if useWindowSize == nil || useWindowSize.Subtype != "react_hook" {
		t.Errorf("useWindowSize subtype = %v, want react_hook", subtypeOf(useWindowSize))
	}

	// USES_HOOK edges from the custom hook to the hooks it calls.
	if !hasRel(useCounter.Relationships, "USES_HOOK", "useState") {
		t.Errorf("useCounter missing USES_HOOK → useState; rels=%v", useCounter.Relationships)
	}
	if !hasRel(useCounter.Relationships, "USES_HOOK", "useEffect") {
		t.Errorf("useCounter missing USES_HOOK → useEffect")
	}
	if !hasRel(useCounter.Relationships, "USES_HOOK", "useWindowSize") {
		t.Errorf("useCounter missing USES_HOOK → useWindowSize (custom hook composition)")
	}

	// component_extraction: ProfileCard is a component (not a hook) and emits
	// USES_HOOK + RENDERS edges.
	profile := findByName(ents, "ProfileCard")
	if profile == nil {
		t.Fatalf("ProfileCard not extracted")
	}
	if profile.Subtype == "react_hook" {
		t.Errorf("ProfileCard must not be tagged react_hook")
	}
	if !hasRel(profile.Relationships, "USES_HOOK", "useCounter") {
		t.Errorf("ProfileCard missing USES_HOOK → useCounter; rels=%v", profile.Relationships)
	}
	if !hasRel(profile.Relationships, "RENDERS", "Avatar") {
		t.Errorf("ProfileCard missing RENDERS → Avatar")
	}
}

func subtypeOf(e *types.EntityRecord) string {
	if e == nil {
		return "<nil>"
	}
	return e.Subtype
}
