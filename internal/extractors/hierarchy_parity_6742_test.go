// Issue #6742 — C#/Java class-hierarchy edge-KIND parity.
//
// WHY THIS TEST EXISTS. The C# extractor emitted no hierarchy edge of any kind
// while Java's structurally identical `implements` emitted IMPLEMENTS, and the
// gap survived for a long time because the golden fixtures AGREED VACUOUSLY:
// the C# rows were held `nice_to_have`, so "zero edges" and "the right edge"
// scored the same. The fix is only half the work; without a test that compares
// the two languages on the SAME structural shape, a later C#-specific kind
// (`INHERITS_CSHARP`, `DERIVES`, `BASE_TYPE`) or a silent reversal regresses it
// again and no fixture goes red.
//
// WHAT IT OBSERVES. The EMITTED ARTEFACT — it runs both production extractors
// over equivalent source and compares the relationship kinds that actually come
// out. It does not read expected.json, and it does not consult any counter the
// extractors keep about themselves: a pass that recorded "hierarchy edges
// emitted: 2" while emitting the wrong kind, or none, would not survive here.
package extractors_test

import (
	"context"
	"sort"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/csharp"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tscsharp "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/csharp"
	tsjava "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/java"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
	"github.com/cajasmota/grafel/internal/types"
)

func parityParse(t *testing.T, lang, src string) ts.Tree {
	t.Helper()
	var l ts.Language
	switch lang {
	case "csharp":
		l = tscsharp.Language()
	case "java":
		l = tsjava.Language()
	default:
		t.Fatalf("unknown language %q", lang)
	}
	p, err := tsofficial.New().NewParser(l)
	if err != nil {
		t.Fatalf("%s parser init: %v", lang, err)
	}
	defer p.Close()
	tree, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatalf("%s parse: %v", lang, err)
	}
	return tree
}

// parityHierarchyKinds returns the sorted "<owner> <KIND> <target>" triples for
// every EXTENDS / IMPLEMENTS edge the named extractor emits for src.
func parityHierarchyKinds(t *testing.T, lang, path, src string) []string {
	t.Helper()
	ext, ok := extractor.Get(lang)
	if !ok {
		t.Fatalf("%s extractor not registered", lang)
	}
	ents, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: lang,
		TSTree:   parityParse(t, lang, src),
	})
	if err != nil {
		t.Fatalf("%s extract: %v", lang, err)
	}
	var out []string
	for _, e := range ents {
		if e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			switch types.RelationshipKind(r.Kind) {
			case types.RelationshipKindExtends, types.RelationshipKindImplements:
				out = append(out, e.Name+" "+r.Kind+" "+r.ToID)
			}
		}
	}
	sort.Strings(out)
	return out
}

// TestHierarchyEdgeKindParityCSharpJava6742 is the parity guard. Each case is a
// structural shape expressed in both languages; the two extractors must agree
// on the edge kind, and the kind must be one of the two the repo already has.
func TestHierarchyEdgeKindParityCSharpJava6742(t *testing.T) {
	cases := []struct {
		shape string
		java  string
		cs    string
		// owner/target the shape declares, so the comparison is on kind
		// alone and not on the languages' differing naming.
		owner, target string
		wantKind      types.RelationshipKind
	}{
		{
			shape: "class implements an external framework interface " +
				"(java-quartz-mini's SendEmailJob implements Job vs " +
				"csharp-quartz-net-mini's ReportJob : IJob)",
			java: `
package jobs;
public class ReportJob implements IJob {
    public void execute() { }
}
`,
			cs: `
namespace Jobs
{
    public class ReportJob : IJob
    {
        public void Execute() { }
    }
}
`,
			owner: "ReportJob", target: "IJob",
			wantKind: types.RelationshipKindImplements,
		},
		{
			shape: "class derives from an external framework base class " +
				"(UsersController extends/: ControllerBase)",
			java: `
package api;
public class UsersController extends ControllerBase {
    public void get() { }
}
`,
			cs: `
namespace Api
{
    public class UsersController : ControllerBase
    {
        public void Get() { }
    }
}
`,
			owner: "UsersController", target: "ControllerBase",
			wantKind: types.RelationshipKindExtends,
		},
		{
			shape: "class implements an interface declared in the same file",
			java: `
package svc;
interface IUserService { void get(); }
public class UserService implements IUserService {
    public void get() { }
}
`,
			cs: `
namespace Svc
{
    public interface IUserService { void Get(); }
    public class UserService : IUserService { public void Get() { } }
}
`,
			owner: "UserService", target: "IUserService",
			wantKind: types.RelationshipKindImplements,
		},
		{
			shape: "class derives from a base class declared in the same file",
			java: `
package models;
class BaseEntity { }
public class Account extends BaseEntity { }
`,
			cs: `
namespace Models
{
    public class BaseEntity { }
    public class Account : BaseEntity { }
}
`,
			owner: "Account", target: "BaseEntity",
			wantKind: types.RelationshipKindExtends,
		},
		{
			shape: "class with a generic base type — the type arguments are " +
				"stripped in both languages",
			java: `
package repo;
public class Repo extends BaseRepository<User> { }
`,
			cs: `
namespace Repo
{
    public class Repo : BaseRepository<User> { }
}
`,
			owner: "Repo", target: "BaseRepository",
			wantKind: types.RelationshipKindExtends,
		},
	}

	for _, tc := range cases {
		t.Run(tc.shape, func(t *testing.T) {
			want := tc.owner + " " + string(tc.wantKind) + " " + tc.target
			jav := parityHierarchyKinds(t, "java", "Shape.java", tc.java)
			csh := parityHierarchyKinds(t, "csharp", "Shape.cs", tc.cs)

			if !containsStr(jav, want) {
				t.Errorf("java must emit %q for this shape; emitted %v", want, jav)
			}
			if !containsStr(csh, want) {
				t.Errorf("csharp must emit %q for this shape; emitted %v", want, csh)
			}

			// The parity assertion proper: whatever kind each language chose
			// for this owner→target pair, the two must be the same string.
			jk := kindFor(jav, tc.owner, tc.target)
			ck := kindFor(csh, tc.owner, tc.target)
			if jk != ck {
				t.Fatalf("edge-KIND parity broken for %s→%s: java=%q csharp=%q "+
					"(shape: %s)", tc.owner, tc.target, jk, ck, tc.shape)
			}
			if jk == "" {
				t.Fatalf("neither language emitted a hierarchy edge for %s→%s — "+
					"agreeing on nothing is how #6742 survived; "+
					"java=%v csharp=%v", tc.owner, tc.target, jav, csh)
			}
		})
	}
}

func containsStr(haystack []string, want string) bool {
	for _, h := range haystack {
		if h == want {
			return true
		}
	}
	return false
}

// kindFor returns the edge kind recorded for owner→target, or "" when the
// language emitted nothing for that pair.
func kindFor(edges []string, owner, target string) string {
	for _, e := range edges {
		// "<owner> <KIND> <target>"
		var o, k, tg string
		if n, _ := scan3(e, &o, &k, &tg); n != 3 {
			continue
		}
		if o == owner && tg == target {
			return k
		}
	}
	return ""
}

// scan3 splits a space-separated triple without pulling in fmt.Sscan's
// error-swallowing behaviour on a malformed line.
func scan3(s string, a, b, c *string) (int, error) {
	parts := splitSpaces(s)
	for i, p := range parts {
		switch i {
		case 0:
			*a = p
		case 1:
			*b = p
		case 2:
			*c = p
		}
	}
	return len(parts), nil
}

func splitSpaces(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
