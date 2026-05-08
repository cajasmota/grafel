package zig_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/zig"
)

func TestZigExtractor_Registered(t *testing.T) {
	_, ok := extractor.Get("zig")
	if !ok {
		t.Fatal("zig extractor not registered")
	}
}

func TestZigExtractor_Functions(t *testing.T) {
	src := `const std = @import("std");

pub fn main() !void {
    std.debug.print("Hello\n", .{});
}

fn helper(x: u32) u32 {
    return x * 2;
}

pub fn add(a: i32, b: i32) i32 {
    return a + b;
}
`
	ext, _ := extractor.Get("zig")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "main.zig",
		Content:  []byte(src),
		Language: "zig",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" {
			names[e.Name] = true
			if e.Language != "zig" {
				t.Errorf("entity %q: expected Language=zig, got %q", e.Name, e.Language)
			}
		}
	}
	for _, want := range []string{"main", "helper", "add"} {
		if !names[want] {
			t.Errorf("expected function %q to be extracted", want)
		}
	}
}

func TestZigExtractor_Structs(t *testing.T) {
	src := `const Point = struct {
    x: f64,
    y: f64,
};

pub const Circle = struct {
    center: Point,
    radius: f64,
};
`
	ext, _ := extractor.Get("zig")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "shapes.zig",
		Content:  []byte(src),
		Language: "zig",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	structs := make(map[string]bool)
	for _, e := range entities {
		if e.Kind == "SCOPE.Component" && e.Subtype == "struct" {
			structs[e.Name] = true
		}
	}
	for _, want := range []string{"Point", "Circle"} {
		if !structs[want] {
			t.Errorf("expected struct %q to be extracted", want)
		}
	}
}

func TestZigExtractor_EmptyInput(t *testing.T) {
	ext, _ := extractor.Get("zig")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "empty.zig",
		Content:  []byte{},
		Language: "zig",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities, got %d", len(entities))
	}
}

func TestZigExtractor_PubVsPrivate(t *testing.T) {
	src := `pub fn publicFn(x: i32) i32 {
    return x;
}

fn privateFn(x: i32) i32 {
    return x * 2;
}
`
	ext, _ := extractor.Get("zig")
	entities, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     "visibility.zig",
		Content:  []byte(src),
		Language: "zig",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sigs := make(map[string]string)
	for _, e := range entities {
		if e.Kind == "SCOPE.Operation" {
			sigs[e.Name] = e.Signature
		}
	}
	if sig := sigs["publicFn"]; sig == "" {
		t.Error("expected 'publicFn' to be extracted")
	} else if sig[:3] != "pub" {
		t.Errorf("expected pub fn signature, got %q", sig)
	}
	if _, ok := sigs["privateFn"]; !ok {
		t.Error("expected 'privateFn' to be extracted")
	}
}
