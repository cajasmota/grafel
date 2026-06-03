package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
)

// TestNestJSControllerDIPipeline_FullIndex is the baseline FULL-PIPELINE
// integration test: a NestJS controller's constructor-DI INJECTED_INTO edge
// (provider service -> consumer controller) must survive resolution + dedup
// into the persisted graph with RESOLVED hashed entity IDs (not bare names)
// on a from-scratch full index.
//
// This guards the extraction + resolve + buildDocument path (#3970/#3979). It
// passes on main; the regression that deploy-9 REFUTED lives on the INCREMENTAL
// path — see TestNestJSControllerDIPipeline_Incremental below.
func TestNestJSControllerDIPipeline_FullIndex(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	repo, ctrlPath := writeNestJSDIFixture(t)
	gitInitCommit(t, repo)

	out := filepath.Join(repo, ".ag", "graph.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Index(repo, out, "nestjs-di", nil, true, false); err != nil {
		t.Fatalf("index: %v", err)
	}
	_ = ctrlPath
	assertControllerDIResolved(t, filepath.Dir(out))
}

// TestNestJSControllerDIPipeline_Incremental is the regression test for the
// deploy-9 REFUTED item: the NestJS controller constructor-DI INJECTED_INTO
// edge is LOST on an incremental reindex that touches ONLY the controller file
// (the provider service file is unchanged, hence out of the changed-file
// resolver scope).
//
// Root cause: the edge is extractor-emitted on the CONTROLLER record with a
// bare-NAME FromID ("FooService"). On a full index the resolver index contains
// the provider entity so the name binds to its hashed ID; on an incremental
// index of the controller alone, the unchanged provider is absent from the
// resolver scope, the FromID stays a bare name, never matches survivingIDs in
// mergeIncrementalPrevDoc, and the controller's inbound DI edge silently
// disappears from the persisted graph — controller-specific because controller
// files are edited far more often than the services they consume.
//
// The fix seeds buildDocument's resolver index with carried-forward
// (unchanged-file) prev entities. This test MUST FAIL before that fix (the
// re-extracted edge keeps a bare-name FromID) and PASS after.
func TestNestJSControllerDIPipeline_Incremental(t *testing.T) {
	t.Setenv("ARCHIGRAPH_DAEMON_ROOT", t.TempDir())
	repo, ctrlPath := writeNestJSDIFixture(t)
	gitInitCommit(t, repo)

	out := filepath.Join(repo, ".ag", "graph.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Dir(out)

	// Initial full index (first incremental run has no manifest -> full).
	if err := Index(repo, out, "nestjs-di", nil, true, false, WithIncremental(stateDir)); err != nil {
		t.Fatalf("initial index: %v", err)
	}
	assertControllerDIResolved(t, stateDir) // sanity: baseline is correct.

	// Edit ONLY the controller; leave it UNCOMMITTED so `git diff HEAD`
	// reports it as the sole changed file (the editor-save scenario the
	// daemon reacts to). The provider service file stays unchanged and is
	// therefore excluded from this run's extraction scope.
	orig, err := os.ReadFile(ctrlPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ctrlPath, append(orig, []byte("\n// touched to trigger incremental reindex\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Index(repo, out, "nestjs-di", nil, true, false, WithIncremental(stateDir)); err != nil {
		t.Fatalf("incremental index: %v", err)
	}

	assertControllerDIResolved(t, stateDir)
}

// writeNestJSDIFixture lays down a tiny NestJS repo: an @Injectable() provider
// service and an @Controller consumer with constructor DI of that service, in
// sibling files. Returns the repo dir and the controller file path.
func writeNestJSDIFixture(t *testing.T) (repo, ctrlPath string) {
	t.Helper()
	repo = t.TempDir()
	srcDir := filepath.Join(repo, "src", "modules", "foo")
	if err := os.MkdirAll(filepath.Join(srcDir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "application"), 0o755); err != nil {
		t.Fatal(err)
	}

	serviceTS := `import { Injectable } from '@nestjs/common';

@Injectable()
export class FooService {
  listFoos(): string[] {
    return ['a', 'b'];
  }
}
`
	controllerTS := `import { Controller, Get } from '@nestjs/common';
import { FooService } from '../application/foo.service';

@Controller('api/v1/foos')
export class FooController {
  constructor(private readonly fooService: FooService) {}

  @Get()
  list(): string[] {
    return this.fooService.listFoos();
  }
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "application", "foo.service.ts"), []byte(serviceTS), 0o644); err != nil {
		t.Fatal(err)
	}
	ctrlPath = filepath.Join(srcDir, "api", "foo.controller.ts")
	if err := os.WriteFile(ctrlPath, []byte(controllerTS), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, ctrlPath
}

func gitInitCommit(t *testing.T, repo string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"-c", "user.email=t@t.t", "-c", "user.name=t", "add", "-A"},
		{"-c", "user.email=t@t.t", "-c", "user.name=t", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// assertControllerDIResolved loads the persisted graph from dir and asserts
// that an INJECTED_INTO edge exists between the RESOLVED FooService entity ID
// and the RESOLVED FooController entity ID — i.e. every INJECTED_INTO edge into
// the controller carries the hashed provider ID, never a bare name.
func assertControllerDIResolved(t *testing.T, dir string) {
	t.Helper()
	doc, err := graph.LoadGraphFromDir(dir)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}

	var controllerID, serviceID string
	for i := range doc.Entities {
		e := &doc.Entities[i]
		switch e.Name {
		case "FooController":
			if controllerID == "" || isComponentish(e.Kind) {
				controllerID = e.ID
			}
		case "FooService":
			if serviceID == "" || isComponentish(e.Kind) {
				serviceID = e.ID
			}
		}
	}
	if controllerID == "" {
		t.Fatalf("FooController entity not found in graph")
	}
	if serviceID == "" {
		t.Fatalf("FooService entity not found in graph")
	}

	resolved := false
	var sawBareName bool
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if r.Kind != "INJECTED_INTO" || r.ToID != controllerID {
			continue
		}
		if r.FromID == serviceID {
			resolved = true
		} else if !isHex16ID(r.FromID) {
			// A bare, unresolved provider name survived into the graph — the
			// exact deploy-9 REFUTED symptom.
			sawBareName = true
			t.Logf("unresolved INJECTED_INTO into FooController: FromID=%q (want resolved serviceID=%s)", r.FromID, serviceID)
		}
	}

	// A bare-name FromID surviving is itself the bug, even if a separate
	// resolved edge also happens to be present (e.g. a carried-forward copy
	// from a previous run): the unresolved edge is what persists once the
	// resolved copy ages out, and it is invisible to inspect / neighbors /
	// trace because buildAdjacency keys it under a phantom node.
	if sawBareName {
		t.Fatalf("controller constructor-DI INJECTED_INTO edge survived with a BARE-NAME FromID instead of the resolved FooService ID %s — provider name not resolved against the out-of-scope (unchanged) provider file", serviceID)
	}
	if !resolved {
		t.Fatalf("NO INJECTED_INTO edge from the resolved FooService ID %s to the resolved FooController ID %s survived the pipeline", serviceID, controllerID)
	}
}

func isComponentish(kind string) bool {
	switch kind {
	case "SCOPE.Component", "Class", "Component":
		return true
	}
	return false
}

func isHex16ID(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
