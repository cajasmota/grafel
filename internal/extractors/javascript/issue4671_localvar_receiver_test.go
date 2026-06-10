package javascript_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/extractors/javascript"
	"github.com/cajasmota/archigraph/internal/types"
)

// extractSpec runs the real extractor over a TypeScript spec file at the given
// path and returns the entities.
func extractSpec(t *testing.T, path, src string) []types.EntityRecord {
	t.Helper()
	tree := parseTS(t, []byte(src))
	e := javascript.New()
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "typescript",
		Tree:     tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// findTestOpCallingHandler returns the first SCOPE.Operation/test entity that
// carries a CALLS edge whose ToID is the handler structural-ref for `method`
// in `controllerFile`. Returns nil when none is found.
func findTestOpCallingHandler(ents []types.EntityRecord, controllerFile, method string) *types.EntityRecord {
	want := "scope:operation:method:typescript:" + controllerFile + ":" + method
	for i := range ents {
		e := &ents[i]
		if e.Kind != "SCOPE.Operation" || e.Properties["subtype"] != "test" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == "CALLS" && r.ToID == want {
				return e
			}
		}
	}
	return nil
}

func hasEdge(e *types.EntityRecord, kind, toID string) bool {
	if e == nil {
		return false
	}
	for _, r := range e.Relationships {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

// TestIssue4671_NewClassReceiver_UnitSpec is the EXACT-MIRROR fixture for the
// live root cause: a controller UNIT spec constructs the controller with
// `new XController(mockSvc)` and calls `controller.getCounts('2025')` inside an
// it() block. Before #4671 the spec produced ZERO call-bearing entities, so no
// test→handler CALLS edge existed. After #4671 a test Operation must own a
// CALLS edge (and a derived TESTS edge) to XController.getCounts.
func TestIssue4671_NewClassReceiver_UnitSpec(t *testing.T) {
	const src = `
import { XController } from './x.controller';
import { XService } from './x.service';

describe('XController', () => {
  let controller;
  let mockSvc;
  beforeEach(() => {
    mockSvc = new XService();
    controller = new XController(mockSvc);
  });
  it('counts', () => {
    const r = controller.getCounts('2025');
  });
});
`
	ents := extractSpec(t, "src/x.controller.spec.ts", src)
	const handlerRef = "scope:operation:method:typescript:src/x.controller.ts:getCounts"

	op := findTestOpCallingHandler(ents, "src/x.controller.ts", "getCounts")
	if op == nil {
		t.Fatalf("no test Operation with CALLS -> %s; got entities: %s", handlerRef, dumpEnts(ents))
	}
	if !hasEdge(op, "TESTS", handlerRef) {
		t.Errorf("test Operation %q missing TESTS -> %s", op.Name, handlerRef)
	}
}

// TestIssue4671_ConstructionAtModuleScope covers the variant where the
// controller is constructed at module scope (top of the file) rather than in a
// beforeEach hook.
func TestIssue4671_ConstructionAtModuleScope(t *testing.T) {
	const src = `
import { XController } from './x.controller';
import { XService } from './x.service';

const controller = new XController(new XService());

describe('XController', () => {
  it('counts', () => {
    controller.getCounts('2025');
  });
});
`
	ents := extractSpec(t, "src/x.controller.spec.ts", src)
	if findTestOpCallingHandler(ents, "src/x.controller.ts", "getCounts") == nil {
		t.Fatalf("module-scope construction: no test Operation with CALLS to handler; got: %s", dumpEnts(ents))
	}
}

// TestIssue4671_NestJSModuleGet covers the NestJS DI variant: the controller is
// obtained via `module.get(XController)` after compiling a TestingModule.
func TestIssue4671_NestJSModuleGet(t *testing.T) {
	const src = `
import { Test } from '@nestjs/testing';
import { XController } from './x.controller';
import { XService } from './x.service';

describe('XController', () => {
  let controller;
  beforeEach(async () => {
    const module = await Test.createTestingModule({
      controllers: [XController],
      providers: [XService],
    }).compile();
    controller = module.get(XController);
  });
  it('counts', () => {
    controller.getCounts('2025');
  });
});
`
	ents := extractSpec(t, "src/x.controller.spec.ts", src)
	if findTestOpCallingHandler(ents, "src/x.controller.ts", "getCounts") == nil {
		t.Fatalf("module.get(XController): no test Operation with CALLS to handler; got: %s", dumpEnts(ents))
	}
}

// TestIssue4671_LocalVarReceiverInProductionMethod is the GENERAL (non-test)
// benefit: a plain production function that constructs a class locally and
// calls a method on it must now bind the call to the class method. This proves
// the local-var typing is not test-file-specific.
func TestIssue4671_LocalVarReceiverInProductionMethod(t *testing.T) {
	const src = `
import { XService } from './x.service';

export function run() {
  const svc = new XService();
  return svc.doWork();
}
`
	tree := parseTS(t, []byte(src))
	e := javascript.New()
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path:     "src/runner.ts",
		Content:  []byte(src),
		Language: "typescript",
		Tree:     tree,
	})
	if err != nil {
		t.Fatal(err)
	}
	const ref = "scope:operation:method:typescript:src/x.service.ts:doWork"
	run := findByName(ents, "run")
	if run == nil {
		t.Fatalf("run() not extracted; got: %s", dumpEnts(ents))
	}
	if !hasEdge(run, "CALLS", ref) {
		t.Errorf("run() missing CALLS -> %s (local-var receiver typing); rels: %v", ref, run.Relationships)
	}
}

func dumpEnts(ents []types.EntityRecord) string {
	var b strings.Builder
	for _, e := range ents {
		b.WriteString("\n  ")
		b.WriteString(e.Kind)
		b.WriteString("/")
		b.WriteString(e.Properties["subtype"])
		b.WriteString(" ")
		b.WriteString(e.Name)
		for _, r := range e.Relationships {
			b.WriteString("\n      ")
			b.WriteString(r.Kind)
			b.WriteString(" -> ")
			b.WriteString(r.ToID)
		}
	}
	return b.String()
}
