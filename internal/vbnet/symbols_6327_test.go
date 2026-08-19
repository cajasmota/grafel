package vbnet

import (
	"testing"
)

// #6327 S3 — the per-file declaration table.
//
// The table exists for one reason: `(` is two-way overloaded in VB.NET.
// InvocationExpression and IndexExpression are the same production in
// Microsoft's grammar, so `Foo(1)` cannot be classified without knowing what
// Foo was declared as. Every kind below therefore earns its place by changing
// a ClassifyParen answer, and TestClassifyParen is the test that proves it.
//
// UNVERIFIED against real VB.NET source. There is no .vb file on this machine
// (checked in #6327 S2). winFormsFixture is written from the VB.NET language
// reference and from the shapes @dcastro-imp reported in #6321: a WinForms
// form with Inherits, WithEvents fields, Handles wiring, file-top Imports and
// an aliased Imports.
const winFormsFixture = `
Imports System
Imports WinForms = System.Windows.Forms

Namespace Acme.App

    ''' <summary>The main form.</summary>
    <Serializable()>
    Public Class MainForm
        Inherits WinForms.Form
        Implements IDisposable

        Private WithEvents btnSave As WinForms.Button
        Private buffer(255) As Byte
        Private names() As String
        Private first, second As Integer
        Public Const MaxItems As Integer = 10

        Public Event Saved(sender As Object, e As EventArgs)

        Public Property Title As String

        Public Property Items(index As Integer) As String
            Get
                Return names(index)
            End Get
            Set(value As String)
                names(index) = value
            End Set
        End Property

        Public Sub New()
            InitializeComponent()
        End Sub

        Private Sub btnSave_Click(sender As Object, e As EventArgs) Handles btnSave.Click
            Dim total As Integer = 0
            Dim rows(10) As Integer
            For i As Integer = 0 To 10
                total += rows(i)
            Next
            Save(total)
        End Sub

        Public Sub Fill(target() As Byte, ByRef count As Integer)
            target(0) = 1
        End Sub

        Public Function Save(count As Integer) As Boolean
            Return count > 0
        End Function

        Public Function Wrap(Of T)(item As T) As List(Of T)
            Return New List(Of T) From {item}
        End Function
    End Class

    Public Enum Status
        Idle
        Running = 2
    End Enum

    Public Interface ILogger
        Sub Write(msg As String)
    End Interface

    Public Module Helpers
        Public Sub Log(msg As String)
        End Sub
    End Module

End Namespace
`

// lookupIn finds the symbol declared as name in exactly scope.
func lookupIn(t *testing.T, tbl *Table, name, scope string) *Symbol {
	t.Helper()
	for _, s := range tbl.Lookup(name) {
		if s.Scope == scope {
			return s
		}
	}
	var got []string
	for _, s := range tbl.Lookup(name) {
		got = append(got, s.Scope)
	}
	t.Fatalf("no symbol %q in scope %q (found in scopes %q)", name, scope, got)
	return nil
}

func TestBuildTable_Declarations(t *testing.T) {
	tbl := BuildTable(winFormsFixture)

	cases := []struct {
		name     string
		rule     string
		symbol   string
		scope    string
		kind     SymbolKind
		typeName string
		isArray  bool
		generic  bool
	}{
		{
			name: "namespace", rule: "Namespace declares a scope",
			symbol: "Acme.App", scope: "", kind: KindNamespace,
		},
		{
			name: "import-alias/positive", rule: "Imports X = A.B binds the local name X",
			symbol: "WinForms", scope: "", kind: KindImportAlias,
		},
		{
			name: "class", rule: "Class declares a type",
			symbol: "MainForm", scope: "Acme.App", kind: KindType, typeName: "class",
		},
		{
			name: "generic-method-type-parameter", rule: "an (Of T) list declares T",
			symbol: "Wrap", scope: "Acme.App.MainForm", kind: KindMethod,
			typeName: "List(Of T)", generic: true,
		},
		{
			name: "withevents-field", rule: "a WithEvents field is a field with its declared type",
			symbol: "btnSave", scope: "Acme.App.MainForm", kind: KindField,
			typeName: "WinForms.Button",
		},
		{
			name: "array-field-bounds", rule: "Dim a(255) declares an array",
			symbol: "buffer", scope: "Acme.App.MainForm", kind: KindField,
			typeName: "Byte", isArray: true,
		},
		{
			name: "array-field-empty-parens", rule: "Dim a() declares an array",
			symbol: "names", scope: "Acme.App.MainForm", kind: KindField,
			typeName: "String", isArray: true,
		},
		{
			name: "shared-as-clause", rule: "one As governs every declarator before it",
			symbol: "first", scope: "Acme.App.MainForm", kind: KindField, typeName: "Integer",
		},
		{
			name: "shared-as-clause-second", rule: "the declarator carrying the As keeps its type",
			symbol: "second", scope: "Acme.App.MainForm", kind: KindField, typeName: "Integer",
		},
		{
			name: "const", rule: "Const is not a field",
			symbol: "MaxItems", scope: "Acme.App.MainForm", kind: KindConst, typeName: "Integer",
		},
		{
			name: "event", rule: "Event declares an event",
			symbol: "Saved", scope: "Acme.App.MainForm", kind: KindEvent,
		},
		{
			name: "auto-property", rule: "an auto-property is a property",
			symbol: "Title", scope: "Acme.App.MainForm", kind: KindProperty, typeName: "String",
		},
		{
			name: "indexed-property", rule: "a parameterised property is a property",
			symbol: "Items", scope: "Acme.App.MainForm", kind: KindProperty, typeName: "String",
		},
		{
			name: "indexed-property-parameter", rule: "an indexed property's parameter is scoped to it",
			symbol: "index", scope: "Acme.App.MainForm.Items", kind: KindParameter, typeName: "Integer",
		},
		{
			// The accessor container is named by the folded keyword, because a
			// property opens no container to hang it under (see the get/set
			// case in walkStatement).
			name: "setter-parameter", rule: "the Set accessor's value parameter is declared",
			symbol: "value", scope: "Acme.App.MainForm.set", kind: KindParameter, typeName: "String",
		},
		{
			name: "constructor", rule: "Sub New is a method",
			symbol: "New", scope: "Acme.App.MainForm", kind: KindMethod,
		},
		{
			name: "handles-method", rule: "a Handles clause does not disturb the method declaration",
			symbol: "btnSave_Click", scope: "Acme.App.MainForm", kind: KindMethod,
		},
		{
			name: "method-parameter", rule: "a parameter is scoped to its method",
			symbol: "sender", scope: "Acme.App.MainForm.btnSave_Click", kind: KindParameter, typeName: "Object",
		},
		{
			name: "array-parameter", rule: "target() As Byte is an array parameter",
			symbol: "target", scope: "Acme.App.MainForm.Fill", kind: KindParameter,
			typeName: "Byte", isArray: true,
		},
		{
			name: "byref-parameter", rule: "ByRef is peeled before the parameter name is read",
			symbol: "count", scope: "Acme.App.MainForm.Fill", kind: KindParameter, typeName: "Integer",
		},
		{
			name: "function-return-type", rule: "a Function records its return type",
			symbol: "Save", scope: "Acme.App.MainForm", kind: KindMethod, typeName: "Boolean",
		},
		{
			name: "local", rule: "Dim inside a method is a local, not a field",
			symbol: "total", scope: "Acme.App.MainForm.btnSave_Click", kind: KindLocal, typeName: "Integer",
		},
		{
			name: "local-array", rule: "Dim rows(10) inside a method is an array local",
			symbol: "rows", scope: "Acme.App.MainForm.btnSave_Click", kind: KindLocal,
			typeName: "Integer", isArray: true,
		},
		{
			name: "for-loop-variable", rule: "For i As Integer declares a local",
			symbol: "i", scope: "Acme.App.MainForm.btnSave_Click", kind: KindLocal, typeName: "Integer",
		},
		{
			name: "enum", rule: "Enum declares a type",
			symbol: "Status", scope: "Acme.App", kind: KindType, typeName: "enum",
		},
		{
			name: "enum-member", rule: "a bare name in an Enum body is an enum member",
			symbol: "Idle", scope: "Acme.App.Status", kind: KindEnumMember,
		},
		{
			name: "enum-member-with-value", rule: "an explicitly valued enum member is still a member",
			symbol: "Running", scope: "Acme.App.Status", kind: KindEnumMember,
		},
		{
			name: "interface", rule: "Interface declares a type",
			symbol: "ILogger", scope: "Acme.App", kind: KindType, typeName: "interface",
		},
		{
			name: "interface-method", rule: "a bodyless interface member is a method",
			symbol: "Write", scope: "Acme.App.ILogger", kind: KindMethod,
		},
		{
			name: "module", rule: "Module declares a type",
			symbol: "Helpers", scope: "Acme.App", kind: KindType, typeName: "module",
		},
		{
			name: "module-method", rule: "a module member lands in the module, not the interface above it",
			symbol: "Log", scope: "Acme.App.Helpers", kind: KindMethod,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := lookupIn(t, tbl, tc.symbol, tc.scope)
			if s.Kind != tc.kind {
				t.Errorf("rule %q: Kind = %v, want %v", tc.rule, s.Kind, tc.kind)
			}
			if s.TypeName != tc.typeName {
				t.Errorf("rule %q: TypeName = %q, want %q", tc.rule, s.TypeName, tc.typeName)
			}
			if s.IsArray != tc.isArray {
				t.Errorf("rule %q: IsArray = %v, want %v", tc.rule, s.IsArray, tc.isArray)
			}
			if s.Generic != tc.generic {
				t.Errorf("rule %q: Generic = %v, want %v", tc.rule, s.Generic, tc.generic)
			}
		})
	}
}

// TestBuildTable_NotDeclarations is the negative half: statements that look
// declaration-shaped and must record nothing. A table that over-declares is
// worse than one that under-declares, because a phantom symbol turns an honest
// ParenUnknown into a confidently wrong answer.
func TestBuildTable_NotDeclarations(t *testing.T) {
	cases := []struct {
		name   string
		rule   string
		src    string
		absent string
	}{
		{
			name: "call-statement", rule: "a bare call statement declares nothing",
			src: "Public Class C\nPublic Sub M()\nSave(total)\nEnd Sub\nEnd Class", absent: "total",
		},
		{
			name: "assignment", rule: "an assignment declares nothing",
			src: "Public Class C\nPublic Sub M()\nx = 1\nEnd Sub\nEnd Class", absent: "x",
		},
		{
			name: "plain-imports", rule: "Imports System binds no new local name",
			src: "Imports System.Text", absent: "System.Text",
		},
		{
			name: "inherits", rule: "Inherits names a base type, it does not declare one here",
			src: "Public Class C\nInherits Form\nEnd Class", absent: "Form",
		},
		{
			name: "declaration-in-a-comment", rule: "a declaration inside a comment is not a declaration",
			src: "' Dim ghost As Integer", absent: "ghost",
		},
		{
			name: "declaration-in-a-literal", rule: "a declaration inside a string literal is not a declaration",
			src: "Public Class C\nPublic Sub M()\nDim s = \"Dim ghost As Integer\"\nEnd Sub\nEnd Class", absent: "ghost",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl := BuildTable(tc.src)
			if got := tbl.Lookup(tc.absent); len(got) != 0 {
				t.Errorf("rule %q: %q was declared as %v in scope %q", tc.rule, tc.absent, got[0].Kind, got[0].Scope)
			}
		})
	}
}

func TestClassifyParen(t *testing.T) {
	tbl := BuildTable(winFormsFixture)
	const method = "Acme.App.MainForm.btnSave_Click"
	const class = "Acme.App.MainForm"

	cases := []struct {
		name      string
		rule      string
		symbol    string
		scope     string
		ofKeyword bool
		want      ParenKind
	}{
		{
			name: "method/call", rule: "a known method makes Foo(1) a call",
			symbol: "Save", scope: method, want: ParenCall,
		},
		{
			name: "array-local/index", rule: "a known array local makes rows(i) an index, not a call",
			symbol: "rows", scope: method, want: ParenIndex,
		},
		{
			name: "array-field/index", rule: "a known array field makes buffer(0) an index",
			symbol: "buffer", scope: class, want: ParenIndex,
		},
		{
			name: "scalar-local/unknown", rule: "a non-array value could be a Default property or a delegate; say so",
			symbol: "total", scope: method, want: ParenUnknown,
		},
		{
			name: "property/call", rule: "an indexed property is a call-shaped access",
			symbol: "Items", scope: class, want: ParenCall,
		},
		{
			name: "event/call", rule: "RaiseEvent Saved(...) is a call",
			symbol: "Saved", scope: class, want: ParenCall,
		},
		{
			name: "type/call", rule: "a type name with a bare paren is a conversion or a constructor",
			symbol: "MainForm", scope: class, want: ParenCall,
		},
		{
			name: "array-parameter/index", rule: "an array parameter makes target(0) an index, not a call",
			symbol: "target", scope: "Acme.App.MainForm.Fill", want: ParenIndex,
		},
		{
			name: "of-keyword/generic", rule: "List(Of T) is a generic instantiation whatever the table says",
			symbol: "List", scope: method, ofKeyword: true, want: ParenGeneric,
		},
		{
			name: "undeclared/unknown", rule: "an undeclared name is honestly unknown, not guessed",
			symbol: "SomethingExternal", scope: method, want: ParenUnknown,
		},
		{
			name: "const/unknown", rule: "a const is neither callable nor indexable",
			symbol: "MaxItems", scope: class, want: ParenUnknown,
		},
		{
			name: "enum-member/unknown", rule: "an enum member never takes parentheses",
			symbol: "Idle", scope: "Acme.App.Status", want: ParenUnknown,
		},

		// Case-insensitivity: VB.NET folds identifiers, so every answer above
		// must be reachable through any spelling.
		{
			name: "case/method-upper", rule: "SAVE and Save are one identifier",
			symbol: "SAVE", scope: method, want: ParenCall,
		},
		{
			name: "case/array-lower", rule: "ROWS and rows are one identifier",
			symbol: "ROWS", scope: method, want: ParenIndex,
		},
		{
			name: "case/field-mixed", rule: "BuFFeR and buffer are one identifier",
			symbol: "BuFFeR", scope: class, want: ParenIndex,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tbl.ClassifyParen(tc.symbol, tc.scope, tc.ofKeyword)
			if got != tc.want {
				t.Errorf("rule %q: ClassifyParen(%q, %q, of=%v) = %v, want %v",
					tc.rule, tc.symbol, tc.scope, tc.ofKeyword, got, tc.want)
			}
		})
	}
}

// TestClassifyParen_InnermostScopeWins pins the shadowing rule. The same name
// is an array field and a scalar local, and the right answer differs by scope:
// getting this wrong emits an index where a call belongs, or the reverse.
func TestClassifyParen_InnermostScopeWins(t *testing.T) {
	src := `
Public Class C
    Private buf(10) As Integer
    Public Sub M()
        Dim buf As Integer = 0
    End Sub
End Class
`
	tbl := BuildTable(src)
	if got := tbl.ClassifyParen("buf", "C", false); got != ParenIndex {
		t.Errorf("at class scope buf( is an array index, got %v", got)
	}
	if got := tbl.ClassifyParen("buf", "C.M", false); got != ParenUnknown {
		t.Errorf("inside M the local shadows the field, got %v", got)
	}
}

// TestBuildTable_CaseInsensitiveKeywords pins the other half of VB.NET's
// case-insensitivity: the keywords themselves.
func TestBuildTable_CaseInsensitiveKeywords(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{name: "upper", src: "PUBLIC CLASS C\nPRIVATE BUF(10) AS INTEGER\nPUBLIC SUB M()\nEND SUB\nEND CLASS"},
		{name: "lower", src: "public class C\nprivate buf(10) as integer\npublic sub M()\nend sub\nend class"},
		{name: "mixed", src: "Public Class C\nPrivate Buf(10) As Integer\nPublic Sub M()\nEnd Sub\nEnd Class"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tbl := BuildTable(tc.src)
			if got := tbl.ClassifyParen("buf", "C", false); got != ParenIndex {
				t.Errorf("buf( should be an index, got %v", got)
			}
			if got := tbl.ClassifyParen("m", "C", false); got != ParenCall {
				t.Errorf("M( should be a call, got %v", got)
			}
			if s := tbl.Resolve("C", ""); s == nil || s.Kind != KindType {
				t.Errorf("class C should be a type, got %v", s)
			}
		})
	}
}

// TestBuildTable_OptionHeaderIsNotAContinuation is the scope-level half of the
// joiner's Option/On bug. `Option Strict On` ends a line with `On`, which the
// LINQ `Join … On` position otherwise treats as an implicit continuation — so
// the header swallowed the first declaration of the file and every member
// below it landed at file scope. A blank line after the header masks it, which
// is why a hand-written fixture never caught it and a real legacy file would.
func TestBuildTable_OptionHeaderIsNotAContinuation(t *testing.T) {
	headers := []string{"Option Strict On", "Option Explicit On", "Option Infer On"}
	for _, header := range headers {
		t.Run(header, func(t *testing.T) {
			src := header + "\nPublic Class C\nPublic Sub M()\nEnd Sub\nEnd Class"
			tbl := BuildTable(src)
			if s := lookupIn(t, tbl, "C", ""); s.Kind != KindType {
				t.Errorf("C should be a type, got %v", s.Kind)
			}
			if s := lookupIn(t, tbl, "M", "C"); s.Kind != KindMethod {
				t.Errorf("M should be a method in C, got %v", s.Kind)
			}
		})
	}
	t.Run("Option header above an Imports alias", func(t *testing.T) {
		src := "Option Strict On\nImports SB = System.Text.StringBuilder\nPublic Class C\nEnd Class"
		tbl := BuildTable(src)
		if s := lookupIn(t, tbl, "SB", ""); s.Kind != KindImportAlias {
			t.Errorf("SB should be an import alias, got %v", s.Kind)
		}
		lookupIn(t, tbl, "C", "")
	})
}

// TestBuildTable_EndWithDoesNotDesynchroniseScopes pins the `End With`
// analogue of the `End Select` negative. `With` is an implicit-continuation
// keyword, so `End With` joined the statement below it: the `End Sub` that
// followed was consumed and the container stack stayed open for the rest of
// the file, nesting every later member inside the previous method.
func TestBuildTable_EndWithDoesNotDesynchroniseScopes(t *testing.T) {
	t.Run("End With directly above End Sub", func(t *testing.T) {
		src := `
Public Class Form1
    Public Sub Save()
        With obj
            .A = 1
        End With
    End Sub
    Public Sub Load2()
        Dim n As Integer
    End Sub
End Class
`
		tbl := BuildTable(src)
		if s := lookupIn(t, tbl, "Save", "Form1"); s.Kind != KindMethod {
			t.Errorf("Save should be a method on Form1, got %v", s.Kind)
		}
		if s := lookupIn(t, tbl, "Load2", "Form1"); s.Kind != KindMethod {
			t.Errorf("Load2 belongs to Form1, not to Form1.Save; got %v", s.Kind)
		}
		lookupIn(t, tbl, "n", "Form1.Load2")
	})

	t.Run("declaration on the line after End With", func(t *testing.T) {
		src := `
Public Class Form1
    Private data(9) As Integer
    Public Sub Save()
        With obj
            .A = 1
        End With
        Dim data As Integer = 0
    End Sub
End Class
`
		tbl := BuildTable(src)
		lookupIn(t, tbl, "data", "Form1.Save")
		if got := tbl.ClassifyParen("data", "Form1.Save", false); got != ParenUnknown {
			t.Errorf("the local shadows the array field, so data( is undecidable; got %v", got)
		}
		if got := tbl.ClassifyParen("data", "Form1", false); got != ParenIndex {
			t.Errorf("at class scope data( is an array index; got %v", got)
		}
	})
}

// TestBuildTable_LongTypeCharacter is the declaration-level half of the '&'
// fix: `32&` and `&HFFFF&` are Long literals, and treating the suffix as a
// dangling concatenation operator deletes the declaration below them. These
// shapes are pervasive in the Declare/Win32-interop code this package targets.
func TestBuildTable_LongTypeCharacter(t *testing.T) {
	src := `
Public Module Win32
    Public Const MAX_PATH = 260&
    Public Const GENERIC_READ = &H80000000&
    Public Const FILE_SHARE_READ = 1
End Module
`
	tbl := BuildTable(src)
	for _, name := range []string{"MAX_PATH", "GENERIC_READ", "FILE_SHARE_READ"} {
		if s := lookupIn(t, tbl, name, "Win32"); s.Kind != KindConst {
			t.Errorf("%s should be a const, got %v", name, s.Kind)
		}
	}
}

// TestClassifyParen_OutOfScopeIsUnknown pins the honesty rule ParenUnknown was
// invented for. Resolve falls back to any same-named declaration in the file
// when none is in scope; acting on that fallback classifies an assignment
// target in one class as a call to an unrelated method in another. S4 acts on
// a ParenCall confidently, so a wrong answer costs more than an honest unknown.
func TestClassifyParen_OutOfScopeIsUnknown(t *testing.T) {
	src := `
Public Class A
    Public Sub Items()
    End Sub
    Private cache(9) As Integer
    Public Sub Deep()
        Dim rows(4) As Integer
    End Sub
End Class

Public Class B
    Public Sub M()
        Items(3) = 1
    End Sub
End Class
`
	tbl := BuildTable(src)

	cases := []struct {
		name, rule, symbol, scope string
		want                      ParenKind
	}{
		{
			name: "method-in-another-class", rule: "Items is declared only in A; from B.M the table cannot decide",
			symbol: "Items", scope: "B.M", want: ParenUnknown,
		},
		{
			name: "array-field-in-another-class", rule: "A's array field must not make cache( an index inside B",
			symbol: "cache", scope: "B.M", want: ParenUnknown,
		},
		{
			name: "local-of-another-method", rule: "a local of A.Deep is invisible from B.M",
			symbol: "rows", scope: "B.M", want: ParenUnknown,
		},
		{
			name: "sibling-method-scope", rule: "A.Deep cannot see A.Deep's siblings' locals either",
			symbol: "rows", scope: "A", want: ParenUnknown,
		},

		// Controls: the same names, asked from a scope that does see them.
		{
			name: "control/method-in-scope", rule: "from A's own scope Items( is a call",
			symbol: "Items", scope: "A", want: ParenCall,
		},
		{
			name: "control/array-field-in-scope", rule: "from A's own scope cache( is an index",
			symbol: "cache", scope: "A", want: ParenIndex,
		},
		{
			name: "control/local-in-scope", rule: "inside A.Deep rows( is an index",
			symbol: "rows", scope: "A.Deep", want: ParenIndex,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tbl.ClassifyParen(tc.symbol, tc.scope, false); got != tc.want {
				t.Errorf("rule %q: ClassifyParen(%q, %q) = %v, want %v",
					tc.rule, tc.symbol, tc.scope, got, tc.want)
			}
		})
	}
}

// TestResolve_ScopeVisibility exercises scope resolution directly rather than
// only through ClassifyParen: both the visibility filter and the
// innermost-wins tiebreak decide answers that a single subtest cannot pin.
func TestResolve_ScopeVisibility(t *testing.T) {
	src := `
Public Class A
    Public Sub Deep()
        Dim v(3) As Integer
    End Sub
End Class

Public Class B
    Private v As Integer
    Public Sub N()
        Dim w As Integer = 0
    End Sub
    Public Sub P()
        Dim v As String = ""
    End Sub
End Class
`
	tbl := BuildTable(src)

	cases := []struct {
		name, rule, symbol, scope, wantScope string
	}{
		{
			name:   "unrelated-deeper-scope-is-not-visible",
			rule:   "A.Deep's local is deeper than B's field but invisible from B.N",
			symbol: "v", scope: "B.N", wantScope: "B",
		},
		{
			name:   "innermost-enclosing-wins",
			rule:   "inside P the local shadows B's field",
			symbol: "v", scope: "B.P", wantScope: "B.P",
		},
		{
			name:   "declaring-scope-resolves-to-itself",
			rule:   "a use site in A.Deep sees A.Deep's local",
			symbol: "v", scope: "A.Deep", wantScope: "A.Deep",
		},
		{
			name:   "file-level-is-visible-everywhere",
			rule:   "a file-scope type is in scope from any method",
			symbol: "A", scope: "B.N", wantScope: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tbl.Resolve(tc.symbol, tc.scope)
			if s == nil {
				t.Fatalf("rule %q: Resolve(%q, %q) = nil", tc.rule, tc.symbol, tc.scope)
			}
			if s.Scope != tc.wantScope {
				t.Errorf("rule %q: Resolve(%q, %q) resolved to scope %q, want %q",
					tc.rule, tc.symbol, tc.scope, s.Scope, tc.wantScope)
			}
		})
	}

	// The same two rules, observed through the answer S4 consumes.
	if got := tbl.ClassifyParen("v", "B.N", false); got != ParenUnknown {
		t.Errorf("B's scalar field makes v( undecidable from B.N, got %v", got)
	}
	if got := tbl.ClassifyParen("v", "A.Deep", false); got != ParenIndex {
		t.Errorf("A.Deep's array local makes v( an index, got %v", got)
	}
}

// TestBuildTable_BracketEscapedIdentifier pins the unwrapping of VB.NET's
// bracket escape. Without it `Dim [Class] As Integer` records nothing at all —
// the declaration disappears silently rather than being recorded under the
// wrong name, which no other assertion in the suite would notice.
func TestBuildTable_BracketEscapedIdentifier(t *testing.T) {
	src := `
Public Class C
    Private [Error] As String
    Public Sub M()
        Dim [Class] As Integer
        Dim [Select](4) As Integer
    End Sub
End Class
`
	tbl := BuildTable(src)
	if s := lookupIn(t, tbl, "Class", "C.M"); s.Kind != KindLocal || s.TypeName != "Integer" {
		t.Errorf("[Class] should be an Integer local, got kind %v type %q", s.Kind, s.TypeName)
	}
	if s := lookupIn(t, tbl, "Error", "C"); s.Kind != KindField || s.TypeName != "String" {
		t.Errorf("[Error] should be a String field, got kind %v type %q", s.Kind, s.TypeName)
	}
	if got := tbl.ClassifyParen("Select", "C.M", false); got != ParenIndex {
		t.Errorf("[Select](4) is an array local, so Select( is an index; got %v", got)
	}
}

// TestBuildTable_InterfaceMembersAreBodyless pins why a member declared in an
// Interface must not push a container: it has no `End Sub`, so pushing one
// leaves the stack open and every later member of the interface is scoped
// inside its predecessor. The single-member interface in winFormsFixture
// cannot see this — the tolerant pop at `End Interface` repairs it.
func TestBuildTable_InterfaceMembersAreBodyless(t *testing.T) {
	src := `
Public Interface ILogger
    Sub Write(msg As String)
    Function Read(id As Integer) As String
    Sub Flush()
End Interface
`
	tbl := BuildTable(src)
	for _, name := range []string{"Write", "Read", "Flush"} {
		if s := lookupIn(t, tbl, name, "ILogger"); s.Kind != KindMethod {
			t.Errorf("%s belongs directly to ILogger, got kind %v", name, s.Kind)
		}
	}
	lookupIn(t, tbl, "id", "ILogger.Read")
}

// TestBuildTable_MustOverrideMembersAreBodyless is the same rule for the other
// bodyless form, so the two halves of the condition are pinned separately.
func TestBuildTable_MustOverrideMembersAreBodyless(t *testing.T) {
	src := `
Public MustInherit Class Base
    Public MustOverride Sub Render()
    Public MustOverride Function Name() As String
    Public Sub Ready()
    End Sub
End Class
`
	tbl := BuildTable(src)
	for _, name := range []string{"Render", "Name", "Ready"} {
		if s := lookupIn(t, tbl, name, "Base"); s.Kind != KindMethod {
			t.Errorf("%s belongs directly to Base, got kind %v", name, s.Kind)
		}
	}
}

// TestBuildTable_UsingDeclaresEveryResource pins the multi-resource Using
// header. `Using a As New X, b As New Y` declares two locals; recording only
// the first, with the rest of the header swallowed into its type name, means
// the second is missing from the table and its '(' is undecidable.
func TestBuildTable_UsingDeclaresEveryResource(t *testing.T) {
	src := `
Public Class C
    Public Sub M()
        Using a As New StreamReader("x"), b As New StreamWriter("y")
        End Using
    End Sub
End Class
`
	tbl := BuildTable(src)
	if s := lookupIn(t, tbl, "a", "C.M"); s.TypeName != "StreamReader" {
		t.Errorf("a should be a StreamReader, got %q", s.TypeName)
	}
	if s := lookupIn(t, tbl, "b", "C.M"); s.TypeName != "StreamWriter" {
		t.Errorf("b should be a StreamWriter, got %q", s.TypeName)
	}
}

// TestBuildTable_AttributeWithAngleInParens is the declaration-level half of
// the attribute paren-depth rule: closing the attribute on the first '>'
// leaves the tail of the attribute body in front of the declaration, and the
// declaration it decorates is never recorded.
func TestBuildTable_AttributeWithAngleInParens(t *testing.T) {
	tbl := BuildTable("<MyAttr(2 > 1)> Public Class X\nEnd Class")
	if s := lookupIn(t, tbl, "X", ""); s.Kind != KindType {
		t.Errorf("X should be a type, got %v", s.Kind)
	}
}

// TestBuildTable_NewTypeNames pins what `As New T(...)` records. A
// constructor's argument list is not part of the type name, but a generic
// argument list is — and the difference decides whether the recorded type is
// ever matchable against a declared type in S5.
func TestBuildTable_NewTypeNames(t *testing.T) {
	src := `
Public Class C
    Public Sub M()
        Dim r As New StreamReader(path)
        Dim l As New List(Of String)
        Dim d As New Dictionary(Of String, Integer)(cmp)
        Dim p As New Point
    End Sub
End Class
`
	tbl := BuildTable(src)
	cases := []struct{ name, want string }{
		{"r", "StreamReader"},
		{"l", "List(Of String)"},
		{"d", "Dictionary(Of String, Integer)"},
		{"p", "Point"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if s := lookupIn(t, tbl, tc.name, "C.M"); s.TypeName != tc.want {
				t.Errorf("%s: TypeName = %q, want %q", tc.name, s.TypeName, tc.want)
			}
		})
	}
}

// TestBuildTable_BodylessMemberParameterScope pins where a bodyless member's
// parameters land. The member opens no container, so without an explicit scope
// its parameters would be recorded in the enclosing type and become visible to
// every other member of it.
func TestBuildTable_BodylessMemberParameterScope(t *testing.T) {
	src := `
Public Interface ILogger
    Sub Write(msg As String)
End Interface

Public MustInherit Class Base
    Public MustOverride Sub Render(target As Object)
End Class
`
	tbl := BuildTable(src)
	if s := lookupIn(t, tbl, "msg", "ILogger.Write"); s.Kind != KindParameter {
		t.Errorf("msg should be a parameter of ILogger.Write, got %v", s.Kind)
	}
	if s := lookupIn(t, tbl, "target", "Base.Render"); s.Kind != KindParameter {
		t.Errorf("target should be a parameter of Base.Render, got %v", s.Kind)
	}
	if got := tbl.ClassifyParen("msg", "ILogger", false); got != ParenUnknown {
		t.Errorf("a parameter of one member is not in scope for the type, got %v", got)
	}
}
