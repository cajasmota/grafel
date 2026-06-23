package elixir_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/treesitter"
	"github.com/cajasmota/grafel/internal/types"
)

// TestLiveFireEx drives the FULL production path: ParserFactory.Parse for a
// real lib/*.ex file, then the registry-resolved elixir extractor's Extract,
// confirming error_flow fires live (THROWS + CATCHES converge on one node).
func TestLiveFireEx(t *testing.T) {
	src := []byte("defmodule Accounts do\n  def find(id) do\n    raise NotFoundError, \"x\"\n  end\n  def get(id) do\n    try do\n      find(id)\n    rescue\n      e in NotFoundError -> e\n    end\n  end\nend\n")
	pf := treesitter.NewParserFactory(noop.NewTracerProvider().Tracer("t"))
	res, err := pf.Parse(context.Background(), src, "elixir")
	if err != nil {
		t.Fatal(err)
	}
	ext, ok := extractor.Get("elixir")
	if !ok {
		t.Fatal("elixir extractor not registered")
	}
	recs, err := ext.Extract(context.Background(), extractor.FileInput{
		Path: "lib/accounts.ex", Content: src, Language: "elixir", TSTree: res.TSTree,
	})
	if err != nil {
		t.Fatal(err)
	}
	var throws, catches, node bool
	for _, r := range recs {
		if r.Kind == string(types.EntityKindExceptionType) && r.Name == "exception:NotFoundError" {
			node = true
		}
		for _, rel := range r.Relationships {
			if rel.Kind == "THROWS" && rel.ToID == extractor.ExceptionTypeTargetID("NotFoundError") {
				throws = true
			}
			if rel.Kind == "CATCHES" && rel.ToID == extractor.ExceptionTypeTargetID("NotFoundError") {
				catches = true
			}
		}
	}
	t.Logf("LIVE .ex fire: throws=%v catches=%v convergeNode=%v", throws, catches, node)
	if !(throws && catches && node) {
		t.Fatal("error_flow did NOT fire live on .ex through ParserFactory + registered extractor")
	}
}
