package vbnet

import (
	"fmt"
	"strings"
	"testing"
)

// S7b of #6327 — the parser half.
//
// `Handles` was already parsed onto Node.Handles by S3; `AddressOf` was not
// recorded at all, because the reference pass only sees `name(` use sites and
// an AddressOf operand carries no parentheses (the limit the S5 package doc
// states verbatim). These tests pin the operand scan.

// addrOf renders a node's AddressOf operands as "Qualifier.Name@line".
func addrOf(n *Node) []string {
	out := make([]string, 0, len(n.AddressOfs))
	for _, m := range n.AddressOfs {
		out = append(out, m.String())
	}
	return out
}

// allAddrOf renders every operand in the tree, in document order, prefixed by
// the owning node's path so ownership is asserted and not just presence.
func allAddrOf(root *Node) []string {
	var out []string
	root.Walk(func(n *Node) {
		for _, m := range n.AddressOfs {
			out = append(out, n.Path()+"|"+m.String())
		}
	})
	return out
}

func TestParse_AddressOfOperands(t *testing.T) {
	src := `Public Class Form1
    Private worker As System.Threading.Thread

    Private Sub Setup()
        AddHandler Button1.Click, AddressOf Button1_Click
        worker = New System.Threading.Thread(AddressOf Worker)
        RemoveHandler Button1.Click, AddressOf Me.Button1_Click
        Dim d As Action = AddressOf MyBase.OnLoad
    End Sub

    Private Sub Button1_Click()
    End Sub

    Private Sub Worker()
    End Sub
End Class
`
	res := Parse(src)
	setup := findNode(res.File, "Form1.Setup")
	if setup == nil {
		t.Fatalf("Form1.Setup not parsed\n%s", res.File.Dump())
	}
	got := addrOf(setup)
	want := []string{
		"Button1_Click@5",
		"Worker@6",
		"Me.Button1_Click@7",
		"MyBase.OnLoad@8",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("AddressOf operands\n got %v\nwant %v\n%s", got, want, res.File.Dump())
	}
}

// TestParse_AddressOfCaseAndBoundaries is the negative half: VB.NET is
// case-insensitive, so every spelling of the keyword must be recognised, and
// nothing that merely CONTAINS the letters may be.
func TestParse_AddressOfCaseAndBoundaries(t *testing.T) {
	cases := []struct {
		name string
		stmt string
		want []string
	}{
		{"lower", `RaiseIt(addressof Handler)`, []string{"Handler"}},
		{"upper", `RaiseIt(ADDRESSOF Handler)`, []string{"Handler"}},
		{"mixed", `RaiseIt(AdDrEsSoF Handler)`, []string{"Handler"}},
		// A member named AddressOf, reachable through a qualifier because its
		// declaration bracket-escaped the reserved word: not the keyword.
		// The operand-shaped word AFTER it is what makes this case bite — a
		// scan without a left boundary records `And` as a method reference.
		{"member", `x = obj.AddressOf And y`, nil},
		{"bracketed", `x = [AddressOf] And y`, nil},
		// The letters inside a longer identifier are not the keyword, and
		// again the trap is the word that follows.
		{"prefix", `x = AddressOfThing And y`, nil},
		{"suffix", `x = theAddressOf And y`, nil},
		// Inside a string literal.
		{"literal", `x = "AddressOf Handler"`, nil},
		// A comment is removed before the scan reaches it.
		{"comment", `x = 1 ' AddressOf Handler`, nil},
		// Keyword with no operand.
		{"dangling", `x = AddressOf`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "Public Class C\n    Sub M()\n        " + tc.stmt + "\n    End Sub\nEnd Class\n"
			res := Parse(src)
			m := findNode(res.File, "C.M")
			if m == nil {
				t.Fatalf("C.M not parsed\n%s", res.File.Dump())
			}
			var got []string
			for _, a := range m.AddressOfs {
				got = append(got, a.Qualifier+"|"+a.Name)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("operands: got %v want %v\n%s", got, tc.want, res.File.Dump())
			}
			for i := range got {
				if got[i] != "|"+tc.want[i] && got[i] != tc.want[i] {
					t.Fatalf("operand %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestParse_AddressOfOwnership pins WHICH node owns an operand, because S5
// projects per-node and a mis-owned operand becomes an edge on the wrong
// entity.
//
// A method body's operand belongs to the method. A FIELD initialiser's does
// NOT belong to the field — it belongs to the enclosing type, because
// walkFields opens the field node without pushing it, so the innermost OPEN
// node is still the type. That is inherited from the existing Refs pass, not
// introduced here: `Private cb As Action = Foo()` already puts its CALLS on
// the type for the same reason. Changing it would move CALLS too, which is
// not S7b's to do; it is asserted so the inheritance is deliberate.
func TestParse_AddressOfOwnership(t *testing.T) {
	src := `Module M
    Private cb As Action = AddressOf Target

    Sub Run()
        Dim local As Action = AddressOf Other
    End Sub

    Sub Target()
    End Sub

    Sub Other()
    End Sub
End Module
`
	res := Parse(src)
	got := allAddrOf(res.File)
	want := []string{"M|Target@2", "M.Run|Other@5"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ownership\n got %v\nwant %v\n%s", got, want, res.File.Dump())
	}
}

// TestParse_AddressOfContinuedLine pins the physical line through the
// continuation map: an operand on the third physical line of one logical
// statement must report THAT line, not the statement's first.
func TestParse_AddressOfContinuedLine(t *testing.T) {
	src := `Public Class C
    Sub M()
        AddHandler Btn.Click, _
            AddressOf _
            Handler
    End Sub
    Sub Handler()
    End Sub
End Class
`
	res := Parse(src)
	m := findNode(res.File, "C.M")
	if m == nil {
		t.Fatalf("C.M not parsed\n%s", res.File.Dump())
	}
	if len(m.AddressOfs) != 1 {
		t.Fatalf("want 1 operand, got %v\n%s", addrOf(m), res.File.Dump())
	}
	if m.AddressOfs[0].Name != "Handler" {
		t.Fatalf("name: got %q want %q", m.AddressOfs[0].Name, "Handler")
	}
	if m.AddressOfs[0].Line != 5 {
		t.Fatalf("line: got %d want 5 (the operand's own physical line)", m.AddressOfs[0].Line)
	}
}

// TestParse_HandlesMultipleTargets pins the clause S7b projects. Handles was
// parsed before this stage; the assertion is here because S7b is the first
// consumer and a silent change to splitNames would otherwise be invisible.
func TestParse_HandlesMultipleTargets(t *testing.T) {
	src := `Public Class Form1
    Private Sub Any(sender As Object, e As EventArgs) _
        Handles Button1.Click, Button2.Click, MyBase.Load
    End Sub

    Private Function F() As Integer Implements IFoo.F
        Return 0
    End Function

    Private Sub G() Implements IFoo.G Handles Me.Shown
    End Sub
End Class
`
	res := Parse(src)
	any := findNode(res.File, "Form1.Any")
	if any == nil {
		t.Fatalf("Form1.Any not parsed\n%s", res.File.Dump())
	}
	if fmt.Sprint(any.Handles) != fmt.Sprint([]string{"Button1.Click", "Button2.Click", "MyBase.Load"}) {
		t.Fatalf("Handles: got %v", any.Handles)
	}
	g := findNode(res.File, "Form1.G")
	if g == nil || fmt.Sprint(g.Handles) != fmt.Sprint([]string{"Me.Shown"}) {
		t.Fatalf("Handles after Implements: got %v", g.Handles)
	}
}
