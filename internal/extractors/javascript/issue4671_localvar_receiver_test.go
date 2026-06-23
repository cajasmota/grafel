// Package javascript — unit tests for issue #4671 (local-variable receiver
// typing → test→handler CALLS edge → coverage crediting).
//
// Live root cause (proven on upvate-v3): the TS call resolver typed
// `this.<field>.method()` (DI fields) and bare typed params, but NOT a
// LOCAL variable bound from `new XController()` / NestJS `module.get(X)`.
// Controller-unit specs are dominated by that local form —
//
//	const controller = new ProposalController(mockSvc);
//	describe('ProposalController', () => {
//	  it('counts', () => { const r = controller.getCounts('2025'); });
//	});
//
// — so `controller.getCounts()` never bound to the handler, no test→handler
// CALLS edge was emitted, and ComputeCoverage undercounted ~4x (18% vs the
// real ~80%). These fixtures mirror the live shape EXACTLY and assert the
// structural-ref CALLS edge is now emitted (the prior four coverage fixes
// passed their fixtures but moved the live number by zero — these assert the
// actual missing edge).
package javascript_test

import (
	"context"
	"strings"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/extractors/javascript"
)

// hasStructuralCallTo reports whether ANY extracted entity carries a CALLS
// (or TESTS) relationship whose ToID is a Format A structural ref ending in
// `:<method>` and naming `<file>` in the resolved path. We match on the
// structural-ref shape rather than an exact file hash because the relative
// import resolver normalizes the path; the load-bearing facts are (a) the
// edge is a resolvable structural ref (contains ':'), not a bare leaf, and
// (b) it targets the right method on the right source file.
func hasStructuralCallTo(ents []entityRec4671, method, fileHint string) (string, bool) {
	want := "scope:operation:method:typescript:"
	for _, e := range ents {
		for _, r := range e.rels {
			if r.kind != "CALLS" && r.kind != "TESTS" {
				continue
			}
			if !strings.HasPrefix(r.toID, want) {
				continue
			}
			if !strings.HasSuffix(r.toID, ":"+method) {
				continue
			}
			if fileHint != "" && !strings.Contains(r.toID, fileHint) {
				continue
			}
			return r.toID, true
		}
	}
	return "", false
}

type relRec4671 struct {
	kind string
	toID string
}

type entityRec4671 struct {
	name string
	kind string
	rels []relRec4671
}

func extractTS4671(t *testing.T, src []byte, path string) []entityRec4671 {
	t.Helper()
	tree := parseTS(t, src)
	ext := javascript.New()
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  src,
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	out := make([]entityRec4671, 0, len(ents))
	for i := range ents {
		rr := make([]relRec4671, 0, len(ents[i].Relationships))
		for _, r := range ents[i].Relationships {
			rr = append(rr, relRec4671{kind: r.Kind, toID: r.ToID})
		}
		out = append(out, entityRec4671{name: ents[i].Name, kind: ents[i].Kind, rels: rr})
	}

	return out
}

// TestLocalVarReceiver_NewExpression is the exact-mirror fixture from the
// live root cause: a controller-unit spec that constructs the controller as
// a local `const` and calls a handler method on it. After the fix the call
// must resolve to a structural ref on the controller's source file (and be
// promoted into a TESTS edge by emitTestsEdgesForTestFile).
func TestLocalVarReceiver_NewExpression(t *testing.T) {
	src := []byte(`
import { ProposalController } from './proposal.controller';
import { ProposalService } from './proposal.service';

describe('ProposalController', () => {
  it('returns counts', () => {
    const mockSvc = {} as ProposalService;
    const controller = new ProposalController(mockSvc);
    const r = controller.getCounts('2025');
    expect(r).toBeDefined();
  });
});
`)
	ents := extractTS4671(t, src, "src/proposal/proposal.controller.spec.ts")

	toID, ok := hasStructuralCallTo(ents, "getCounts", "proposal.controller")
	if !ok {
		t.Fatalf("expected a structural-ref CALLS/TESTS edge to getCounts on proposal.controller; got entities:\n%s", dump4671(ents))
	}
	if !strings.HasSuffix(toID, ":getCounts") {
		t.Errorf("structural ref %q does not target getCounts", toID)
	}
}

// TestLocalVarReceiver_NestDIGet mirrors the NestJS Test.createTestingModule
// flow: the subject is resolved from the DI container via module.get(Class)
// rather than constructed directly. Same outcome required.
func TestLocalVarReceiver_NestDIGet(t *testing.T) {
	src := []byte(`
import { Test } from '@nestjs/testing';
import { ProposalController } from './proposal.controller';
import { ProposalService } from './proposal.service';

describe('ProposalController', () => {
  it('counts via DI', async () => {
    const moduleRef = await Test.createTestingModule({
      controllers: [ProposalController],
      providers: [ProposalService],
    }).compile();
    const controller = moduleRef.get(ProposalController);
    const r = controller.getCounts('2025');
    expect(r).toBeDefined();
  });
});
`)
	ents := extractTS4671(t, src, "src/proposal/proposal.controller.spec.ts")

	if _, ok := hasStructuralCallTo(ents, "getCounts", "proposal.controller"); !ok {
		t.Fatalf("expected a structural-ref CALLS/TESTS edge to getCounts via module.get(ProposalController); got entities:\n%s", dump4671(ents))
	}
}

// TestLocalVarReceiver_DIResolve covers request-scoped DI (`.resolve`) and an
// awaited resolution, which the binder unwraps.
func TestLocalVarReceiver_DIResolve(t *testing.T) {
	src := []byte(`
import { ProposalService } from './proposal.service';

describe('ProposalService', () => {
  it('lists', async () => {
    const svc = await moduleRef.resolve(ProposalService);
    const out = svc.findAll();
    expect(out).toBeDefined();
  });
});
`)
	ents := extractTS4671(t, src, "src/proposal/proposal.service.spec.ts")

	if _, ok := hasStructuralCallTo(ents, "findAll", "proposal.service"); !ok {
		t.Fatalf("expected a structural-ref CALLS/TESTS edge to findAll via moduleRef.resolve(ProposalService); got entities:\n%s", dump4671(ents))
	}
}

// TestLocalVarReceiver_StringTokenNotTyped is a NEGATIVE control: DI by a
// string token (`module.get('CACHE')`) names no class identifier, so the
// receiver must NOT be typed (no structural ref). The call falls back to the
// bare leaf, exactly as before — we must not invent a binding.
func TestLocalVarReceiver_StringTokenNotTyped(t *testing.T) {
	src := []byte(`
import { ProposalController } from './proposal.controller';

describe('tokens', () => {
  it('string DI token is opaque', () => {
    const cache = moduleRef.get('CACHE_MANAGER');
    const v = cache.getCounts('2025');
    expect(v).toBeDefined();
  });
});
`)
	ents := extractTS4671(t, src, "src/proposal/cache.spec.ts")

	if toID, ok := hasStructuralCallTo(ents, "getCounts", "proposal.controller"); ok {
		t.Errorf("string DI token must not type the receiver, but got structural ref %q", toID)
	}
}

// TestLocalVarReceiver_ExternalImportNotTyped is a NEGATIVE control: when the
// constructed class is imported from an EXTERNAL package (no relative file),
// the receiver binder finds no resolvedFile and must fall back to the bare
// leaf — never a structural ref to a non-existent local file.
func TestLocalVarReceiver_ExternalImportNotTyped(t *testing.T) {
	src := []byte(`
import { Repository } from 'typeorm';

describe('external', () => {
  it('does not bind external constructors', () => {
    const repo = new Repository();
    const x = repo.findOneBy({ id: 1 });
    expect(x).toBeDefined();
  });
});
`)
	ents := extractTS4671(t, src, "src/proposal/repo.spec.ts")

	if toID, ok := hasStructuralCallTo(ents, "findOneBy", ""); ok {
		t.Errorf("external (typeorm) constructor must not produce a local structural ref, but got %q", toID)
	}
}

func dump4671(ents []entityRec4671) string {
	var b strings.Builder
	for _, e := range ents {
		b.WriteString("- ")
		b.WriteString(e.kind)
		b.WriteString(" ")
		b.WriteString(e.name)
		b.WriteString("\n")
		for _, r := range e.rels {
			if r.kind == "CALLS" || r.kind == "TESTS" {
				b.WriteString("    ")
				b.WriteString(r.kind)
				b.WriteString(" -> ")
				b.WriteString(r.toID)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}
