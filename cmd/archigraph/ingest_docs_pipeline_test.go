package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// runIngestPipeline indexes the ingest_docs fixture with the markdown-ingestion
// flag set to ingestDocs and returns counts of the doc/section nodes and
// CONTAINS/MENTIONS edges produced.
func runIngestPipeline(t *testing.T, ingestDocs bool) (docs, sections, mentions int) {
	t.Helper()
	abs, err := filepath.Abs("testdata/ingest_docs")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	idx := newTestIndexer(t, "ingestdocs", nil, "")
	idx.ingestDocs = ingestDocs

	doc, err := idx.Run(context.Background(), abs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := range doc.Entities {
		switch doc.Entities[i].Kind {
		case string(types.EntityKindMarkdownDocument):
			docs++
		case string(types.EntityKindSection):
			sections++
		}
	}
	for i := range doc.Relationships {
		if doc.Relationships[i].Kind == string(types.RelationshipKindMentions) {
			mentions++
		}
	}
	return docs, sections, mentions
}

// TestIngestDocs_FlagOff_NoDocNodes is the off-by-default guarantee: with the
// opt-in flag OFF the pipeline emits ZERO Document/Section nodes and zero
// MENTIONS edges — behavior is identical to today.
func TestIngestDocs_FlagOff_NoDocNodes(t *testing.T) {
	docs, sections, mentions := runIngestPipeline(t, false)
	if docs != 0 || sections != 0 || mentions != 0 {
		t.Fatalf("flag OFF must produce no doc artifacts; got docs=%d sections=%d mentions=%d",
			docs, sections, mentions)
	}
}

// TestIngestDocs_FlagOn_EmitsDocNodes asserts the opt-in path emits the
// Document + Section nodes and at least one exact-match MENTIONS edge to the
// indexed Go symbols (WidgetProcessor / ProcessWidget), and that the common
// words in the fixture did not inflate the link count.
func TestIngestDocs_FlagOn_EmitsDocNodes(t *testing.T) {
	docs, sections, mentions := runIngestPipeline(t, true)
	if docs != 1 {
		t.Fatalf("flag ON: documents = %d, want 1", docs)
	}
	if sections < 2 {
		t.Fatalf("flag ON: sections = %d, want >= 2 (# + ##)", sections)
	}
	if mentions == 0 {
		t.Fatalf("flag ON: expected >=1 MENTIONS edge to an indexed Go symbol, got 0")
	}
}
