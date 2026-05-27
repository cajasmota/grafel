package mcp

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

func TestEntityPassesConfidence(t *testing.T) {
	tests := []struct {
		name string
		e    *graph.Entity
		min  float64
		want bool
	}{
		{"nil entity passes when threshold is zero", nil, 0, true},
		{"nil entity passes when threshold > 0", nil, 0.5, true}, // helper short-circuits on nil
		{"zero confidence reads as 1.0 (default direct-AST)", &graph.Entity{}, 0.9, true},
		{"explicit 0.7 below 0.9 threshold", &graph.Entity{Confidence: 0.7}, 0.9, false},
		{"explicit 0.7 above 0.5 threshold", &graph.Entity{Confidence: 0.7}, 0.5, true},
		{"explicit 0.4 above 0.4 threshold", &graph.Entity{Confidence: 0.4}, 0.4, true},
		{"zero threshold always passes", &graph.Entity{Confidence: 0.01}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entityPassesConfidence(tt.e, tt.min); got != tt.want {
				t.Errorf("entityPassesConfidence(%v, %v) = %v, want %v", tt.e, tt.min, got, tt.want)
			}
		})
	}
}

func TestRelPassesConfidence(t *testing.T) {
	r := &graph.Relationship{Confidence: 0.5}
	if !relPassesConfidence(r, 0.4) {
		t.Error("0.5 should pass threshold 0.4")
	}
	if relPassesConfidence(r, 0.9) {
		t.Error("0.5 should NOT pass threshold 0.9")
	}
	// Zero confidence reads as 1.0.
	zero := &graph.Relationship{}
	if !relPassesConfidence(zero, 0.99) {
		t.Error("zero confidence should read as 1.0 and pass 0.99 threshold")
	}
}
