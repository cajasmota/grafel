// langchain4j_tool_merge_6499_test.go — the contract that #6499 exposed.
//
// A custom framework extractor does NOT own a node of its own for a construct
// the base extractor already emits. It mints a record with the SAME
// (SourceFile, Kind, Name) and relies on buildDocument's dedupe (index.go,
// #4406) to collapse it onto the base entity and gap-fill its properties.
// types.EntityRecord.ComputeID and graph.EntityID both hash
// (SourceFile, Kind, Name) with SUBTYPE EXCLUDED, so the entity Name is the
// entire join key.
//
// That contract was undeclared and untested, which is how #6499 broke it
// silently: qualifying the base Kotlin extractor's operation names to
// `Class.method` left internal/custom/kotlin/langchain4j.go minting the bare
// leaf, so the @Tool annotation stopped landing on the operation AND a
// bare, name-colliding SCOPE.Operation was left behind to compete for the
// repo-wide byName slot (the collision class of #6369 / #6481).
//
// A test asserting only that `Tools.webSearch` exists would NOT catch a
// regression here — the base extractor emits that name on its own. This file
// asserts the MERGED OUTCOME instead: exactly one entity for the identity,
// carrying base-extractor identity fields AND custom-extractor properties.

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const lc4jToolKotlinSrc = `package com.example.tools

class Tools {
    @Tool("Search the web")
    fun webSearch(query: String): String = ""
}
`

// TestLangChain4jKotlinToolMergesOntoBaseOperation_6499 runs the real indexer
// over a single Kotlin file carrying a LangChain4j @Tool method and asserts the
// base and custom records became ONE entity.
func TestLangChain4jKotlinToolMergesOntoBaseOperation_6499(t *testing.T) {
	// The internal/custom/** extractors are default-OFF on the in-process
	// indexing path (#5989). They are NOT dormant: qualityIndexOptions passes
	// WithCustomExtractors(true), so the golden/quality path runs them, and
	// this gate is the per-call equivalent. Without it RunCustomExtractors is
	// never reached and the merge under test cannot occur at all — which the
	// positive control below reports as a failure rather than a pass.
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")

	repo := t.TempDir()
	src := filepath.Join(repo, "Tools.kt")
	if err := os.WriteFile(src, []byte(lc4jToolKotlinSrc), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	idx := newTestIndexer(t, "lc4j-tool-merge", nil, "")
	doc, err := idx.Run(context.Background(), repo)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if doc == nil {
		t.Fatal("Run returned nil document")
	}

	// Positive control — the custom LangChain4j extractor actually fired on
	// this fixture. Without it a silently-skipped extractor would satisfy
	// every "no duplicate" assertion below for the wrong reason.
	var sawFramework bool
	for k := range doc.Entities {
		if v, ok := doc.Entities[k].PropLookup("framework"); ok && v == "langchain4j" {
			sawFramework = true
			break
		}
	}
	if !sawFramework {
		t.Fatalf("positive control failed: no entity carries framework=langchain4j — " +
			"the custom extractor did not run, so this fixture proves nothing")
	}

	// Exactly ONE operation for the qualified identity.
	var matches []int
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind == "SCOPE.Operation" && e.Name == "Tools.webSearch" {
			matches = append(matches, k)
		}
	}
	if len(matches) != 1 {
		var got []string
		for k := range doc.Entities {
			if doc.Entities[k].Kind == "SCOPE.Operation" {
				got = append(got, doc.Entities[k].Name)
			}
		}
		t.Fatalf("want exactly 1 SCOPE.Operation named Tools.webSearch, got %d; operations: %v",
			len(matches), got)
	}
	op := &doc.Entities[matches[0]]

	// No bare-leaf twin. This is the name-colliding node the pre-fix custom
	// extractor left behind; it is what competes for the byName slot.
	for k := range doc.Entities {
		e := &doc.Entities[k]
		if e.Kind == "SCOPE.Operation" && e.Name == "webSearch" {
			t.Errorf("a bare `webSearch` SCOPE.Operation survives alongside Tools.webSearch — "+
				"the custom record minted a second node instead of annotating the base one "+
				"(subtype=%q, source_file=%q)", e.Subtype, e.SourceFile)
		}
	}

	// The BASE extractor's identity survived the merge. Signature is emitted
	// only by the base Kotlin extractor (buildFunSignature); the custom
	// makeEntity never sets it. Its presence proves the surviving entity is
	// the base one rather than the custom record having won outright.
	if op.Signature == "" {
		t.Errorf("merged entity carries no Signature — the base extractor's record did not " +
			"survive, so this is the custom record standing alone")
	}
	if op.Language != "kotlin" {
		t.Errorf("merged entity language = %q, want kotlin", op.Language)
	}

	// The CUSTOM extractor's annotation landed on that same entity. This is
	// the half that failed silently under #6499: the properties simply stopped
	// arriving, with no error and no test to notice.
	for _, tc := range []struct{ key, want string }{
		{"framework", "langchain4j"},
		{"provenance", "INFERRED_FROM_LANGCHAIN4J_TOOL"},
		{"tool_method", "webSearch"}, // names the METHOD, so bare
		{"owner_class", "Tools"},
	} {
		got, ok := op.PropLookup(tc.key)
		if !ok {
			t.Errorf("merged entity is missing property %q — the @Tool annotation did not "+
				"reach the operation; the custom record failed to collapse onto it", tc.key)
			continue
		}
		if got != tc.want {
			t.Errorf("merged property %s = %q, want %q", tc.key, got, tc.want)
		}
	}
}
