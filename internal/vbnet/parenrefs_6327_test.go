package vbnet

import (
	"strings"
	"testing"
)

// refStrings renders every Ref under root in document order.
func refStrings(root *Node) []string {
	var out []string
	root.Walk(func(n *Node) {
		for _, r := range n.Refs {
			out = append(out, r.String())
		}
	})
	return out
}

func findRef(root *Node, name string) (Ref, bool) {
	var hit Ref
	found := false
	root.Walk(func(n *Node) {
		for _, r := range n.Refs {
			if !found && FoldName(r.Name) == FoldName(name) {
				hit, found = r, true
			}
		}
	})
	return hit, found
}

// TestParse_ParenClassification is the story this whole package exists for:
// `name(` is a call, an index, a generic list or genuinely undecidable, and
// the answer comes from the declaration table rather than from shape.
func TestParse_ParenClassification(t *testing.T) {
	src := strings.Join([]string{
		"Public Class Store",                                         // 1
		"    Private buf(10) As Integer",                             // 2
		"    Private names() As String",                              // 3
		"    Private count As Integer",                               // 4
		"    Public Function Compute(ByVal i As Integer) As Integer", // 5
		"        Return i",                                           // 6
		"    End Function",                                           // 7
		"    Public Sub Run()",                                       // 8
		"        Dim x As Integer = buf(1)",                          // 9
		"        Dim y As Integer = Compute(2)",                      // 10
		"        Dim z As Integer = names(0).Length",                 // 11
		"        Dim d = New Dictionary(Of String, Integer)",         // 12
		"        Dim w = count(3)",                                   // 13
		"        Compute(4)",                                         // 14
		"        Me.Compute(5)",                                      // 15
		"        Unknown(6)",                                         // 16
		"        Dim q = Other.Compute(7)",                           // 17
		"    End Sub",                                                // 18
		"End Class",                                                  // 19
	}, "\n")

	res := Parse(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	run := findNode(res.File, "Store.Run")
	if run == nil {
		t.Fatalf("no Run; tree:\n%s", res.File.Dump())
	}

	want := []string{
		"buf:index@9",
		"Compute:call@10",
		"names:index@11",
		"Dictionary:call@12+new",
		"count:unknown@13",
		"Compute:call@14+head",
		"Me.Compute:call@15",
		"Unknown:unknown@16+head",
		"Other.Compute:unknown@17",
	}
	got := refStrings(run)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("refs:\n got %v\nwant %v", got, want)
	}

	// The declaration itself must never look like a use site: `buf(10)` in
	// the field declaration is a bounds list, and reading it as a call is the
	// phantom-CALLS failure #6327 exists to avoid.
	store := findNode(res.File, "Store")
	if len(store.Refs) != 0 {
		t.Errorf("declaration headers produced use sites: %v", refStrings(store))
	}

	// IsCall composes the table's answer with what only syntax can say.
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"buf", false}, {"Compute", true}, {"names", false},
		{"Dictionary", true}, {"count", false}, {"Unknown", true},
	} {
		r, ok := findRef(run, tc.name)
		if !ok {
			t.Errorf("no ref for %s", tc.name)
			continue
		}
		if r.IsCall() != tc.want {
			t.Errorf("%s.IsCall() = %v, want %v (%s)", tc.name, r.IsCall(), tc.want, r)
		}
	}
}

// TestParse_QualifiedByAnUnnameableReceiver pins the distinction an S5 author
// depends on: a name qualified by something this per-file pass cannot NAME is
// not the same thing as an unqualified name it could not RESOLVE.
//
// `With` blocks and calls on expression results both produce Qualifier == "",
// and without the Qualified bit they are byte-identical to a bare unresolved
// name. An S5 author reading Qualifier == "" as "unqualified" would resolve
// `.Foo(` against file-local declarations and emit a confidently wrong CALLS
// edge — which is the failure #6327 exists to prevent. 1,600 of the 41,242
// use sites in the corpus are in this state.
func TestParse_QualifiedByAnUnnameableReceiver(t *testing.T) {
	src := strings.Join([]string{
		"Public Class C",                                    // 1
		"    Private key As RegistryKey",                    // 2
		"    Public Sub SetValue(ByVal n As String)",        // 3
		"    End Sub",                                       // 4
		"    Public Sub F()",                                // 5
		"        With key",                                  // 6
		"            .SetValue(\"a\", 1)",                   // 7
		"            Dim s = .OpenSubKey(\"b\")",            // 8
		"        End With",                                  // 9
		"        CType(Me, ISupportInitialize).BeginInit()", // 10
		"        Unresolved(1)",                             // 11
		"    End Sub",                                       // 12
		"End Class",                                         // 13
	}, "\n")

	res := Parse(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("diagnostics: %v", res.Diagnostics)
	}
	f := findNode(res.File, "C.F")

	// The load-bearing assertion, checked before anything cosmetic so that a
	// regression reports the consequence rather than a rendering diff.
	// `.SetValue` must NOT resolve against the same-named method declared on
	// this class: its receiver is the With target, which this pass cannot name.
	for _, name := range []string{"SetValue", "OpenSubKey", "BeginInit"} {
		r, ok := findRef(f, name)
		if !ok {
			t.Fatalf("no ref for %s", name)
		}
		if !r.Qualified {
			t.Errorf("%s: Qualified = false. S5 cannot tell this from an "+
				"unqualified unresolved name and will resolve it against "+
				"file-local declarations, emitting a wrong CALLS edge (#6327)", name)
		}
		if r.Qualifier != "" {
			t.Errorf("%s: Qualifier = %q, want \"\" (the receiver is unnameable here)", name, r.Qualifier)
		}
		// Recall limit, asserted so it is a recorded fact and not a surprise:
		// a With-block invocation is dropped rather than guessed at.
		if r.IsCall() {
			t.Errorf("%s: IsCall() = true; S5 would need a receiver this pass never resolved", name)
		}
	}

	want := []string{
		".SetValue:unknown@7", ".OpenSubKey:unknown@8",
		"CType:unknown@10+intrinsic+head", ".BeginInit:unknown@10",
		"Unresolved:unknown@11+head",
	}
	if got := refStrings(f); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("refs:\n got %v\nwant %v", got, want)
	}

	// The contrast case: a genuinely unqualified name the table could not
	// resolve. Same Kind, different Qualified, and IsCall differs because of it.
	u, _ := findRef(f, "Unresolved")
	if u.Qualified || !u.IsCall() {
		t.Errorf("unqualified unresolved ref = %+v, want Qualified=false and IsCall()=true", u)
	}
}

// TestParse_ParenNonCalls covers the shapes that are call-SHAPED but are not
// calls: language keywords, intrinsic conversions, grouping and literals.
func TestParse_ParenNonCalls(t *testing.T) {
	src := strings.Join([]string{
		"Public Module M",                   // 1
		"    Public Sub Go()",               // 2
		"        If (1 = 1) Then",           // 3
		"            Return",                // 4
		"        End If",                    // 5
		"        While (True)",              // 6
		"        End While",                 // 7
		"        Dim n = CInt(\"3\")",       // 8
		"        Dim o = CType(n, Object)",  // 9
		"        Dim s = \"Call Faked(1)\"", // 10
		"        Dim g = (n + 1) * 2",       // 11
		"        Dim f = Function(a) a + 1", // 12
		"    End Sub",                       // 13
		"End Module",                        // 14
	}, "\n")

	res := Parse(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	got := refStrings(res.File)
	want := []string{"CInt:unknown@8+intrinsic", "CType:unknown@9+intrinsic"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("refs:\n got %v\nwant %v\ntree:\n%s", got, want, res.File.Dump())
	}
	for _, r := range res.File.Children[0].Children[0].Refs {
		if r.IsCall() {
			t.Errorf("intrinsic %s reported as a call", r)
		}
	}
}

// TestParse_RefCaseAndContinuation pins two failure modes with a recorded
// history: VB.NET is case-insensitive (a case-sensitive compare killed 111 of
// 238 S3 entries), and a use site on a continued line must report ITS line,
// not the statement's first — 46 corpus files carry ' _' continuations.
func TestParse_RefCaseAndContinuation(t *testing.T) {
	src := strings.Join([]string{
		"Public Class C",                // 1
		"    Private buf(4) As Integer", // 2
		"    Public Sub Helper()",       // 3
		"    End Sub",                   // 4
		"    Public Sub Go()",           // 5
		"        Dim v = 1 + _",         // 6
		"            BUF(2) + _",        // 7
		"            helper()",          // 8
		"    End Sub",                   // 9
		"End Class",                     // 10
	}, "\n")

	res := Parse(src)
	got := refStrings(res.File)
	want := []string{"BUF:index@7", "helper:call@8"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("refs:\n got %v\nwant %v", got, want)
	}
}

// TestParse_Clauses pins that Inherits/Implements land on the TYPE and that
// Handles and member-level Implements land on the member.
//
// The anchoring is not cosmetic: #6295, #6298 and #6365 are the same defect in
// five languages — an EXTENDS edge hung off the file node instead of the type.
func TestParse_Clauses(t *testing.T) {
	src := strings.Join([]string{
		"Option Strict On",                        // 1
		"Imports System.Text",                     // 2
		"Imports WF = System.Windows.Forms",       // 3
		"",                                        // 4
		"Public Class Form1",                      // 5
		"    Inherits System.Windows.Forms.Form",  // 6
		"    Implements IDisposable, IComparable", // 7
		"", // 8
		"    Private Sub Button1_Click(sender As Object, e As EventArgs) Handles Button1.Click, Button2.Click", // 9
		"    End Sub", // 10
		"",            // 11
		"    Public Sub Dispose() Implements IDisposable.Dispose", // 12
		"    End Sub", // 13
		"End Class",   // 14
	}, "\n")

	res := Parse(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %v", res.Diagnostics)
	}
	form := findNode(res.File, "Form1")
	if form == nil {
		t.Fatalf("no class; tree:\n%s", res.File.Dump())
	}
	if strings.Join(form.Inherits, ",") != "System.Windows.Forms.Form" {
		t.Errorf("Inherits = %v", form.Inherits)
	}
	if strings.Join(form.Implements, ",") != "IDisposable,IComparable" {
		t.Errorf("Implements = %v", form.Implements)
	}
	// The clauses must not have been recorded on the file.
	if len(res.File.Inherits)+len(res.File.Implements) != 0 {
		t.Errorf("clauses landed on the file node: %v %v", res.File.Inherits, res.File.Implements)
	}
	click := findNode(res.File, "Form1.Button1_Click")
	if strings.Join(click.Handles, ",") != "Button1.Click,Button2.Click" {
		t.Errorf("Handles = %v", click.Handles)
	}
	if len(click.Params) != 2 {
		t.Errorf("Handles clause ate the parameters: %+v", click.Params)
	}
	disp := findNode(res.File, "Form1.Dispose")
	if strings.Join(disp.Implements, ",") != "IDisposable.Dispose" {
		t.Errorf("member Implements = %v", disp.Implements)
	}

	opt := findNode(res.File, "Strict")
	if opt == nil || opt.Kind != NodeOption || opt.Target != "On" {
		t.Errorf("Option node = %+v", opt)
	}
	if n := findNode(res.File, "System.Text"); n == nil || n.Kind != NodeImport || n.Target != "" {
		t.Errorf("plain Imports node = %+v", n)
	}
	if n := findNode(res.File, "WF"); n == nil || n.Target != "System.Windows.Forms" {
		t.Errorf("aliased Imports node = %+v", n)
	}
}

// TestParse_OptionStrictDoesNotSwallow is the regression S3 paid for: `On` is
// a continuation keyword for LINQ `Join ... On`, and `Option Strict On` as a
// first line joined the declaration below it, dropping the whole class.
func TestParse_OptionStrictDoesNotSwallow(t *testing.T) {
	for _, opt := range []string{"Option Strict On", "Option Explicit On", "Option Infer On"} {
		src := opt + "\nPublic Class C\n    Public Sub F()\n    End Sub\nEnd Class\n"
		res := Parse(src)
		if len(res.Diagnostics) != 0 {
			t.Errorf("%s: diagnostics %v", opt, res.Diagnostics)
		}
		if findNode(res.File, "C") == nil || findNode(res.File, "C.F") == nil {
			t.Errorf("%s swallowed the declaration; tree:\n%s", opt, res.File.Dump())
		}
	}
}

// TestParse_RealFileShapes covers shapes that only real source produced. Every
// one was found by running the parser over the 302-file corpus (#6363), not by
// writing a fixture: a constructed file has no byte-order mark, and nobody
// writes a multi-line lambda into a test by accident.
func TestParse_RealFileShapes(t *testing.T) {
	t.Run("utf8 bom before the first declaration", func(t *testing.T) {
		// 11 .Designer.vb files in the corpus start with a BOM and an
		// attribute group; without BOM handling the attribute is not
		// recognised as leading, the Partial Class is never declared, and its
		// Inherits clause — the whole point of a designer file — is orphaned.
		src := "\ufeff<Global.Microsoft.VisualBasic.CompilerServices.DesignerGenerated()> _\n" +
			"Partial Class Search\n    Inherits System.Windows.Forms.Form\nEnd Class\n"
		res := Parse(src)
		if len(res.Diagnostics) != 0 {
			t.Fatalf("diagnostics: %v", res.Diagnostics)
		}
		n := findNode(res.File, "Search")
		if n == nil {
			t.Fatalf("BOM swallowed the class; tree:\n%s", res.File.Dump())
		}
		if strings.Join(n.Inherits, ",") != "System.Windows.Forms.Form" {
			t.Errorf("Inherits = %v", n.Inherits)
		}
		if got := src[n.Span.StartByte:]; !strings.HasPrefix(got, "Partial Class Search") {
			t.Errorf("span starts inside the BOM: %q", got[:20])
		}
	})

	t.Run("bom before a bare class", func(t *testing.T) {
		res := Parse("\ufeffPublic Class Crash\nEnd Class\n")
		if len(res.Diagnostics) != 0 || findNode(res.File, "Crash") == nil {
			t.Fatalf("diagnostics %v; tree:\n%s", res.Diagnostics, res.File.Dump())
		}
	})

	t.Run("multi-line statement lambda", func(t *testing.T) {
		// `x = Sub(v) ... End Sub` closes the LAMBDA, not the method. Treating
		// its End Sub as the method's unwinds the container stack and
		// reparents the rest of the file — the largest failure class in the
		// corpus at 155 occurrences.
		src := strings.Join([]string{
			"Public Class C",                               // 1
			"    Public Sub Init()",                        // 2
			"        tbl.Action = Sub(value)",              // 3
			"                         Encoder.Show(value)", // 4
			"                     End Sub",                 // 5
			"        Dim f = Function(a)",                  // 6
			"                    Return a",                 // 7
			"                End Function",                 // 8
			"    End Sub",                                  // 9
			"    Public Sub After()",                       // 10
			"    End Sub",                                  // 11
			"End Class",                                    // 12
		}, "\n")
		res := Parse(src)
		if len(res.Diagnostics) != 0 {
			t.Fatalf("diagnostics: %v", res.Diagnostics)
		}
		init := findNode(res.File, "C.Init")
		if init == nil || init.Span.EndLine != 9 {
			t.Fatalf("Init span wrong; tree:\n%s", res.File.Dump())
		}
		after := findNode(res.File, "C.After")
		if after == nil || after.Scope != "C" {
			t.Fatalf("After was reparented; tree:\n%s", res.File.Dump())
		}
	})

	t.Run("multi-line lambda with a generic return type", func(t *testing.T) {
		// `Function() As List(Of Job)` is a multi-line lambda: a single-line
		// lambda has no As clause at all. Requiring the return type to be a
		// bare identifier missed this one and unwound the enclosing method.
		src := strings.Join([]string{
			"Public Class C",     // 1
			"    Public Sub F()", // 2
			"        Dim getJobs = Function() As List(Of Job)", // 3
			"                          Return Nothing",         // 4
			"                      End Function",               // 5
			"    End Sub",                                      // 6
			"    Public Sub After()",                           // 7
			"    End Sub",                                      // 8
			"End Class",                                        // 9
		}, "\n")
		res := Parse(src)
		if len(res.Diagnostics) != 0 {
			t.Fatalf("diagnostics: %v\ntree:\n%s", res.Diagnostics, res.File.Dump())
		}
		if n := findNode(res.File, "C.After"); n == nil || n.Scope != "C" {
			t.Fatalf("After was reparented; tree:\n%s", res.File.Dump())
		}
	})

	t.Run("single-line lambda opens no block", func(t *testing.T) {
		src := "Public Class C\n    Public Sub F()\n        Dim g = Sub(x) Log(x)\n    End Sub\nEnd Class\n"
		res := Parse(src)
		if len(res.Diagnostics) != 0 {
			t.Fatalf("diagnostics: %v", res.Diagnostics)
		}
		if n := findNode(res.File, "C.F"); n == nil || n.Span.EndLine != 4 {
			t.Fatalf("F span wrong; tree:\n%s", res.File.Dump())
		}
	})
}

// TestParse_Diagnostics pins what "does not parse" means, which is what the
// corpus measurement counts.
func TestParse_Diagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "unclosed class",
			src:  "Public Class C\n    Public Sub F()\n    End Sub\n",
			want: `unclosed class "C"`,
		},
		{
			name: "unmatched End",
			src:  "Public Class C\nEnd Class\nEnd Class\n",
			want: "End class with no matching block",
		},
		{
			name: "End Property with no property",
			src:  "Public Class C\n    End Property\nEnd Class\n",
			want: "End Property with no open property",
		},
		{
			name: "clean file",
			src:  "Public Class C\n    Public Sub F()\n    End Sub\nEnd Class\n",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := Parse(tc.src)
			joined := ""
			for _, d := range res.Diagnostics {
				joined += d.String() + ";"
			}
			if tc.want == "" {
				if joined != "" {
					t.Fatalf("wanted a clean parse, got %s", joined)
				}
				return
			}
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("diagnostics %q do not mention %q", joined, tc.want)
			}
		})
	}
}
