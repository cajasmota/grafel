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
//
// WHAT IT GRADES, AND WHAT IT KNOWINGLY DOES NOT. This file is two tests, and
// reading only the first one overstates the coverage:
//
//   - TestHierarchyEdgeKindParityCSharpJava6742 grades the shapes where C#
//     decides the kind from a LANGUAGE RULE or from an in-file declaration
//     keyword — an external interface and base class named by the .NET
//     convention, an in-file interface and base class, a generic base. On
//     those the two languages must agree, full stop.
//
//   - TestHierarchyEdgeKindParityKnownDivergence6742 records the one axis they
//     do NOT agree on: an out-of-file base type whose NAME breaks the .NET
//     `I`+PascalCase convention. Java reads the `implements`/`extends` keyword
//     and is always right; C# has only the name to go on and gets it wrong in
//     both directions. That case pins the CURRENT, WRONG behaviour on purpose,
//     so this file cannot be read as proof that the two languages agree
//     everywhere. They do not, and the divergence has a name and a fix.
//
// So: parity on the keyword-decidable and in-file-decidable axes is GRADED;
// parity on out-of-file naming-convention guesses is UNGRADED and known-broken.
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

// TestHierarchyEdgeKindParityKnownDivergence6742 records the ONE axis on which
// C# and Java do NOT agree, and pins the current — WRONG — C# answer so that
// nobody reads the parity test above as proof they agree everywhere.
//
// THE ASYMMETRY. Java writes `implements` and `extends` as KEYWORDS, so its
// extractor never has to guess: the kind is in the grammar. C# writes both in
// one undifferentiated base list, so when the base type is not declared in the
// file being parsed, the only signal left is its NAME, and the C# extractor
// falls back to the .NET `I`+PascalCase convention (csLooksLikeInterfaceName).
//
// The convention is right for the overwhelming majority of real .NET code and
// is deliberately kept. But it is a guess, and the two cases below are the two
// ways it loses. Both are legal, ordinary C#:
//
//	class RedisCache : Cacheable     an interface WITHOUT an I prefix, and a
//	                                 class may implement an interface with no
//	                                 base class, so it sits at index 0 where
//	                                 the convention is consulted.
//	                                 → C# says EXTENDS, Java says IMPLEMENTS.
//
//	class BufferedStream : IOStream  a base CLASS whose name happens to begin
//	                                 I + capital.
//	                                 → C# says IMPLEMENTS, Java says EXTENDS.
//
// WHY PIN THE WRONG ANSWER INSTEAD OF FIXING IT. The fix is cross-file type
// resolution: knowing what `Cacheable` was DECLARED as, which is exactly what
// rule 4 of the ladder already does for types declared in the same file. A
// per-file extractor pass cannot see other files, so the fix belongs in the
// resolver or in a whole-repo second pass, not here. Meanwhile the limitation
// was invisible — the five graded cases all use names the convention happens
// to get right (IJob, ControllerBase, IUserService, BaseEntity,
// BaseRepository) — and an invisible limitation reads as a denial.
//
// WHEN THIS TEST FAILS, IT IS PROBABLY GOOD NEWS. Read the failure message.
func TestHierarchyEdgeKindParityKnownDivergence6742(t *testing.T) {
	cases := []struct {
		shape      string
		java       string
		cs         string
		owner      string
		target     string
		wantJava   types.RelationshipKind
		csCurrent  types.RelationshipKind
		csCorrect  types.RelationshipKind
		whyCsWrong string
	}{
		{
			shape: "interface WITHOUT the .NET I prefix, at index 0",
			java: `
package cache;
public class RedisCache implements Cacheable {
    public void put() { }
}
`,
			cs: `
namespace Cache
{
    public class RedisCache : Cacheable
    {
        public void Put() { }
    }
}
`,
			owner: "RedisCache", target: "Cacheable",
			wantJava:  types.RelationshipKindImplements,
			csCurrent: types.RelationshipKindExtends,
			csCorrect: types.RelationshipKindImplements,
			whyCsWrong: "`Cacheable` is an interface but is not named I+PascalCase, " +
				"and a class may implement an interface with no base class, so it " +
				"occupies index 0 — the one position where the convention is asked",
		},
		{
			shape: "base CLASS whose name happens to start I + capital",
			java: `
package io;
public class BufferedStream extends IOStream {
    public void flush() { }
}
`,
			cs: `
namespace IO
{
    public class BufferedStream : IOStream
    {
        public void Flush() { }
    }
}
`,
			owner: "BufferedStream", target: "IOStream",
			wantJava:  types.RelationshipKindExtends,
			csCurrent: types.RelationshipKindImplements,
			csCorrect: types.RelationshipKindExtends,
			whyCsWrong: "`IOStream` is a base class, but I+O reads as the .NET " +
				"interface prefix, so the convention fires on a name that is not " +
				"an interface at all",
		},
	}

	for _, tc := range cases {
		t.Run(tc.shape, func(t *testing.T) {
			jav := parityHierarchyKinds(t, "java", "Shape.java", tc.java)
			csh := parityHierarchyKinds(t, "csharp", "Shape.cs", tc.cs)

			jk := kindFor(jav, tc.owner, tc.target)
			ck := kindFor(csh, tc.owner, tc.target)

			// Java is the reference: it reads a keyword and must be right.
			// If THIS drifts, the divergence recorded below is measured
			// against a moved baseline and means nothing.
			if jk != string(tc.wantJava) {
				t.Fatalf("java is the keyword-driven reference for %s→%s and must "+
					"emit %s; got %q (edges: %v). Fix Java first — every claim in "+
					"this file is relative to it.",
					tc.owner, tc.target, tc.wantJava, jk, jav)
			}

			// Positive control: C# must emit SOMETHING. A C# extractor that
			// stopped emitting hierarchy edges entirely would otherwise make
			// this test pass by producing no divergence to observe — which is
			// the exact failure mode (#6742's original gap) the parity file
			// exists to catch.
			if ck == "" {
				t.Fatalf("positive control failed: csharp emitted NO hierarchy edge "+
					"for %s→%s (edges: %v). This test grades a KIND divergence; with "+
					"no C# edge at all there is nothing to diverge, and the #6742 "+
					"regression would slip through here unnoticed.",
					tc.owner, tc.target, csh)
			}

			if ck == string(tc.csCorrect) {
				t.Fatalf("GOOD NEWS, UPDATE THIS TEST: csharp now emits the CORRECT "+
					"kind %s for %s→%s, matching java. This case existed only to "+
					"record that it did not. Someone has presumably added cross-file "+
					"type resolution so the base type's DECLARATION decides the kind "+
					"instead of csLooksLikeInterfaceName. Delete this sub-case and "+
					"move the shape into TestHierarchyEdgeKindParityCSharpJava6742, "+
					"where it will be graded as real parity.",
					tc.csCorrect, tc.owner, tc.target)
			}

			if ck != string(tc.csCurrent) {
				t.Fatalf("csharp emitted %q for %s→%s; this test recorded %q as the "+
					"current (known-wrong) answer and %q as the correct one, so %q is "+
					"a THIRD behaviour nobody has reasoned about. Re-derive the kind "+
					"ladder in internal/extractors/csharp/hierarchy.go before changing "+
					"this expectation.",
					ck, tc.owner, tc.target, tc.csCurrent, tc.csCorrect, ck)
			}

			// The divergence itself, stated out loud so the record is not a
			// silent equality that a reader could mistake for agreement.
			if ck == jk {
				t.Fatalf("internal inconsistency: this sub-case claims a divergence "+
					"but java and csharp both emitted %q for %s→%s", ck, tc.owner, tc.target)
			}
			t.Logf("KNOWN DIVERGENCE (%s): java=%s csharp=%s, correct=%s. %s. "+
				"Fixing it requires cross-file type resolution — resolving %s to its "+
				"DECLARATION so rule 4 of the kind ladder can decide instead of the "+
				"I+PascalCase guess — which a per-file extractor pass cannot do.",
				tc.shape, jk, ck, tc.csCorrect, tc.whyCsWrong, tc.target)
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
