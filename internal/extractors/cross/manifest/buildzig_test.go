package manifest

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// realBuildZigZon is a representative build.zig.zon package manifest (shape from
// `zig init` + a couple of fetched deps). It exercises the .dependencies map,
// the content-addressed .hash pin (which IS the lockfile), and a .url fallback.
const realBuildZigZon = `.{
    .name = "myapp",
    .version = "0.1.0",
    .minimum_zig_version = "0.13.0",
    .dependencies = .{
        .zap = .{
            .url = "https://github.com/zigzap/zap/archive/v0.8.0.tar.gz",
            .hash = "1220abcdef0123456789abcdef0123456789abcdef0123456789abcdef012345",
        },
        .known_folders = .{
            .url = "https://github.com/ziglibs/known-folders/archive/main.tar.gz",
        },
    },
    .paths = .{
        "build.zig",
        "build.zig.zon",
        "src",
    },
}`

// realBuildZig is a representative build.zig build script. It declares an
// executable + a static library target and pulls in a build-graph dependency
// via b.dependency(...).
const realBuildZig = `const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const exe = b.addExecutable(.{
        .name = "myapp",
        .root_source_file = b.path("src/main.zig"),
        .target = target,
        .optimize = optimize,
    });

    const lib = b.addStaticLibrary(.{
        .name = "mylib",
        .root_source_file = b.path("src/lib.zig"),
        .target = target,
        .optimize = optimize,
    });

    const utils = b.addModule("utils", .{
        .root_source_file = b.path("src/utils.zig"),
    });
    _ = utils;

    const zap = b.dependency("zap", .{
        .target = target,
        .optimize = optimize,
    });
    exe.root_module.addImport("zap", zap.module("zap"));

    b.installArtifact(exe);
    b.installArtifact(lib);
}
`

func anchorRecord(records []types.EntityRecord) *types.EntityRecord {
	for i := range records {
		if records[i].Kind == "SCOPE.Component" && records[i].Subtype == "project" {
			return &records[i]
		}
	}
	return nil
}

// TestBuildZigZon_Dependencies proves the ZON manifest's .dependencies map is
// parsed into runtime deps under package_manager=zig, with the content hash as
// the pinned version (and a .url fallback when no .hash is present).
func TestBuildZigZon_Dependencies(t *testing.T) {
	deps := depEntities(runExtract(t, "build.zig.zon", realBuildZigZon))
	if len(deps) != 2 {
		t.Fatalf("expected 2 deps, got %d: %+v", len(deps), depNames(deps))
	}
	for _, d := range deps {
		if d.Properties["package_manager"] != "zig" {
			t.Errorf("%s: package_manager=%q want zig", d.Name, d.Properties["package_manager"])
		}
	}
	// zap pins to its content hash.
	if d := depByName(deps, "zap"); d == nil {
		t.Error("zap dep missing")
	} else if !strings.HasPrefix(d.Properties["version"], "1220") {
		t.Errorf("zap version=%q want the content hash", d.Properties["version"])
	}
	// known_folders has no .hash, so the version falls back to the archive URL.
	if d := depByName(deps, "known_folders"); d == nil {
		t.Error("known_folders dep missing")
	} else if !strings.Contains(d.Properties["version"], "known-folders") {
		t.Errorf("known_folders version=%q want the url fallback", d.Properties["version"])
	}
}

// TestBuildZig_DependencyGraph proves b.dependency("name", …) calls in the build
// script materialise build-graph dependencies under package_manager=zig.
func TestBuildZig_DependencyGraph(t *testing.T) {
	deps := depEntities(runExtract(t, "build.zig", realBuildZig))
	if len(deps) != 1 {
		t.Fatalf("expected 1 build-graph dep, got %d: %+v", len(deps), depNames(deps))
	}
	if d := depByName(deps, "zap"); d == nil {
		t.Error("zap build-graph dep missing")
	} else if d.Properties["package_manager"] != "zig" {
		t.Errorf("zap package_manager=%q want zig", d.Properties["package_manager"])
	}
}

// TestBuildZig_TargetExtraction proves the declared build targets (addExecutable
// / addStaticLibrary / addModule) are surfaced as the project anchor's
// "build_targets" property.
func TestBuildZig_TargetExtraction(t *testing.T) {
	records := runExtract(t, "build.zig", realBuildZig)
	anchor := anchorRecord(records)
	if anchor == nil {
		t.Fatal("no project anchor emitted for build.zig")
	}
	targets := anchor.Properties["build_targets"]
	if targets == "" {
		t.Fatal("build_targets property is empty")
	}
	for _, want := range []string{"myapp", "mylib", "utils"} {
		if !strings.Contains(targets, want) {
			t.Errorf("build_targets=%q missing target %q", targets, want)
		}
	}
}

// TestBuildZigZon_NoBuildTargets proves the build_targets property is NOT set on
// a build.zig.zon manifest (target extraction is a build.zig-only concern).
func TestBuildZigZon_NoBuildTargets(t *testing.T) {
	records := runExtract(t, "build.zig.zon", realBuildZigZon)
	anchor := anchorRecord(records)
	if anchor == nil {
		t.Fatal("no project anchor emitted for build.zig.zon")
	}
	if anchor.Properties["build_targets"] != "" {
		t.Errorf("build.zig.zon must not carry build_targets, got %q",
			anchor.Properties["build_targets"])
	}
}

// TestBuildZig_DependsOnEdges confirms the manifest emits DEPENDS_ON edges like
// every other ecosystem (the cross-manifest contract).
func TestBuildZig_DependsOnEdges(t *testing.T) {
	records := runExtract(t, "build.zig.zon", realBuildZigZon)
	var dependsOn int
	for _, r := range records {
		for _, rel := range r.Relationships {
			if rel.Kind == "DEPENDS_ON" {
				dependsOn++
				if rel.Properties.Get("package_manager") != "zig" {
					t.Errorf("DEPENDS_ON package_manager=%q want zig", rel.Properties.Get("package_manager"))
				}
			}
		}
	}
	if dependsOn != 2 {
		t.Errorf("expected 2 DEPENDS_ON edges, got %d", dependsOn)
	}
}

// TestBuildZig_IsManifest pins the dispatch wiring: build.zig and build.zig.zon
// are recognised and routed to the zig package manager.
func TestBuildZig_IsManifest(t *testing.T) {
	for _, p := range []string{"build.zig", "build.zig.zon", "sub/build.zig"} {
		if !IsManifest(p) {
			t.Errorf("%s should be recognised as a manifest", p)
		}
		if pm := detectPackageManager(p); pm != "zig" {
			t.Errorf("detectPackageManager(%s)=%q want zig", p, pm)
		}
	}
}
