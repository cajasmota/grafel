package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/engine"
	"github.com/cajasmota/grafel/internal/extractor"
)

// ---------------------------------------------------------------------------
// Issue #6537, Arm B — the `vbnet` rule bucket itself.
//
// Arm A (51d09cc14) proved the gap: v0.3.0 shipped a VB.NET extractor that
// produced 45,663 entities on a real ~1,600-file WinForms codebase and
// annotated 0.0% of them with a framework, because `rules/vbnet/` did not
// exist and nothing aliased onto it.
//
// ALIAS-VS-OWN-BUCKET, MEASURED (not assumed). Before writing a rule, the
// realistic VB source below was run through Detect with Language="csharp" —
// i.e. exactly what an alias vbnet->csharp would produce. The csharp bucket
// compiles 46 rules (27 source_patterns, 6 relationship_rules, 13
// file_conventions) and emitted ZERO entities and ZERO relationships on it.
// C# regexes are written against braces, `void`, `[Attribute]` and `//`; VB
// has `Sub`/`End Sub`, `Inherits`, `Handles`, `WithEvents` and `'`. An alias
// would have turned the Arm A guard green while firing nothing — the exact
// "metric reports success over ground it does not cover" failure this
// milestone keeps finding. Hence a real bucket with VB-syntax patterns.
//
// TestVBNet6537_BucketIsNonEmpty observes that the bucket EXISTS; the two
// tests after it observe that it FIRES, which is the property the deleted
// allowlist entry was standing in for.
// ---------------------------------------------------------------------------

// vbWinFormsSource is hand-written legacy (pre-SDK, .NET Framework) VB.NET
// WinForms code, in the shape the reporter's stack uses: a Form subclass, a
// Designer-style WithEvents control field block, InitializeComponent, and
// `Handles`-bound event handlers. Written for this test; not copied from any
// corpus.
const vbWinFormsSource = `Imports System
Imports System.Windows.Forms

Public Class OrderEntryForm
    Inherits System.Windows.Forms.Form

    Friend WithEvents SaveButton As System.Windows.Forms.Button
    Friend WithEvents CustomerTextBox As System.Windows.Forms.TextBox
    Friend WithEvents OrdersGrid As System.Windows.Forms.DataGridView

    ' A display-only control: no WithEvents, so it is reachable ONLY through
    ' the Me.Controls.Add pattern. Designer code emits both shapes.
    Private StatusLabel As System.Windows.Forms.Label

    Private Sub InitializeComponent()
        Me.SaveButton = New System.Windows.Forms.Button()
        Me.StatusLabel = New System.Windows.Forms.Label()
        Me.Controls.Add(Me.StatusLabel)
    End Sub

    Private Sub SaveButton_Click(ByVal sender As System.Object, ByVal e As System.EventArgs) Handles SaveButton.Click
        MessageBox.Show("saved")
    End Sub

    Private Sub OrderEntryForm_Load(ByVal sender As System.Object, ByVal e As System.EventArgs) Handles MyBase.Load
        Me.Text = "Orders"
    End Sub

    ' Ordinary subroutines with NO Handles clause. A legacy WinForms codebase is
    ' mostly these; they are helpers, not event handlers, and must NOT become
    ' Operation entities. Without them, truncating the Operation pattern at the
    ' argument list — dropping the Handles requirement entirely — was a silent
    ' survivor that turned every subroutine in the codebase into an Operation.
    Private Sub NotAHandler(ByVal total As Decimal)
        Me.Text = total.ToString()
    End Sub

    Public Sub AlsoNotAHandler()
        Me.Close()
    End Sub
End Class
`

// vbUserControlSource covers the UserControl shape, a distinct pattern from
// the Form shape.
const vbUserControlSource = `Imports System.Windows.Forms

Public Class AddressEditor
    Inherits System.Windows.Forms.UserControl

    Friend WithEvents StreetTextBox As System.Windows.Forms.TextBox
End Class
`

// vbModuleSource covers the second VB.NET WinForms shape: the application
// entry-point Module with Application.Run.
const vbModuleSource = `Imports System.Windows.Forms

Module Program
    <STAThread()> _
    Public Sub Main()
        Application.EnableVisualStyles()
        Application.Run(New OrderEntryForm())
    End Sub
End Module
`

// csharpWinFormsSource is the C#-syntax equivalent. It is fed to Detect under
// Language="vbnet" on purpose: if a VB pattern is loose enough to match C#,
// the bucket is not language-specific and this goes red. This is the guard
// against "widen a pattern until it matches anything".
const csharpWinFormsSource = `using System;
using System.Windows.Forms;

namespace Orders {
    public partial class OrderEntryForm : Form {
        private System.Windows.Forms.Button saveButton;

        public OrderEntryForm() {
            InitializeComponent();
            this.Controls.Add(this.saveButton);
        }

        private void saveButton_Click(object sender, EventArgs e) {
            MessageBox.Show("saved");
        }
    }
}
`

func vbnetDetector(t *testing.T) *engine.Detector {
	t.Helper()
	rules, err := engine.LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	return engine.New(rules)
}

func detectVBNet(t *testing.T, path, content string) *engine.DetectResult {
	t.Helper()
	res, err := vbnetDetector(t).Detect(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(content),
		Language: "vbnet",
	})
	if err != nil {
		t.Fatalf("Detect(%s): %v", path, err)
	}
	return res
}

// TestVBNet6537_BucketIsNonEmpty is the existence half: `vbnet` must resolve to
// a compiled rule set with real source_patterns. Deleting rules/vbnet/ turns
// this red.
func TestVBNet6537_BucketIsNonEmpty(t *testing.T) {
	d := vbnetDetector(t)

	if got := d.CompiledRuleCount("vbnet"); got == 0 {
		t.Fatalf("vbnet compiles 0 rules; the rules/vbnet bucket is missing or unloadable")
	}
	sp, _, fc := d.CompiledRuleBreakdown("vbnet")
	if sp == 0 {
		t.Errorf("vbnet compiles 0 source_patterns; a bucket that emits nothing is worse than "+
			"no bucket, because it silences the #6537 gap guard (breakdown: sp=%d fc=%d)", sp, fc)
	}
}

// TestVBNet6537_FiresOnVBWinFormsSource is the firing half, and the one that
// matters. A rule file that compiles but matches nothing would leave the
// reported 0.0% framework annotation exactly where it was while removing the
// allowlist entry that told us so.
func TestVBNet6537_FiresOnVBWinFormsSource(t *testing.T) {
	res := detectVBNet(t, "src/Forms/OrderEntryForm.vb", vbWinFormsSource)

	if len(res.Entities) == 0 {
		t.Fatalf("vbnet rules emitted 0 entities on real VB.NET WinForms source; the bucket does not fire")
	}

	// Every want below is reachable through exactly ONE source_pattern, so each
	// assertion pins that pattern's firing on its own. Neutering any single
	// pattern turns exactly one line red — that separation is what makes this
	// an observation of firing rather than a headcount. (An earlier version
	// asserted only on SaveButton, which Me.Controls.Add ALSO emits; a mutant
	// that anchored the WithEvents pattern impossibly survived it.)
	wants := []struct{ kind, name, viaPattern string }{
		// Inherits System.Windows.Forms.Form
		{"View", "OrderEntryForm", "form class"},
		// Friend WithEvents … As … — CustomerTextBox and OrdersGrid are declared
		// WithEvents but never passed to Me.Controls.Add, so no other pattern
		// in the file can reach them.
		{"Component", "CustomerTextBox", "WithEvents field"},
		{"Component", "OrdersGrid", "WithEvents field"},
		// Me.Controls.Add(Me.X) — StatusLabel is NOT declared WithEvents.
		{"Component", "StatusLabel", "Me.Controls.Add"},
		// Private Sub … Handles <Control>.<Event>
		{"Operation", "SaveButton_Click", "Handles clause"},
		{"Operation", "OrderEntryForm_Load", "Handles clause"},
	}
	for _, w := range wants {
		if !hasVBEntity(res, w.kind, w.name) {
			t.Errorf("no %s entity named %q (the %s pattern did not fire); got %s",
				w.kind, w.name, w.viaPattern, formatVBEntities(res))
		}
	}

	// The Handles clause is not decoration on the Operation pattern — it is the
	// whole of it. Every Sub below has a signature identical in shape to an
	// event handler and differs only by having no Handles clause; a pattern
	// that merely matched `Sub <name>(` would turn every subroutine in a
	// ~1,600-file codebase into an Operation, and before these three lines
	// existed that mutant survived the full package suite.
	for _, notHandler := range []string{"InitializeComponent", "NotAHandler", "AlsoNotAHandler"} {
		if hasVBEntity(res, "Operation", notHandler) {
			t.Errorf("Sub %q has no Handles clause but was emitted as an Operation; the "+
				"Operation pattern is not requiring the Handles clause: %s",
				notHandler, formatVBEntities(res))
		}
	}

	// The Handles clause is what binds a handler to its control in VB; that
	// binding is the edge the graph could never draw before.
	if !hasVBRelationship(res, "HANDLES", "Component:SaveButton", "Operation:SaveButton_Click") {
		t.Errorf("no HANDLES relationship Component:SaveButton -> Operation:SaveButton_Click; got %s",
			formatVBRelationships(res))
	}

	// `Handles MyBase.<Event>` binds to the form itself. The rule file claims
	// this yields Component:MyBase; nothing asserted it until now, so the claim
	// was prose. Form-lifecycle handlers are most of the handlers in a legacy
	// WinForms app, so losing them would be a real recall loss.
	if !hasVBRelationship(res, "HANDLES", "Component:MyBase", "Operation:OrderEntryForm_Load") {
		t.Errorf("no HANDLES relationship Component:MyBase -> Operation:OrderEntryForm_Load; "+
			"the `Handles MyBase.Load` form-lifecycle binding was dropped; got %s",
			formatVBRelationships(res))
	}
}

// TestVBNet6537_FiresOnEntryPointModule covers the Module/Application.Run shape.
func TestVBNet6537_FiresOnEntryPointModule(t *testing.T) {
	res := detectVBNet(t, "src/Program.vb", vbModuleSource)

	if !hasVBEntity(res, "Module", "Program") {
		t.Errorf("no Module entity named %q; got %s", "Program", formatVBEntities(res))
	}
	// The startup form is reached as the TARGET of an edge, not as a fresh
	// entity. Detector entities carry no ID and relationship endpoints are
	// symbolic "<Kind>:<Name>", so View:OrderEntryForm resolves to the View the
	// form's own file emits.
	if !hasVBRelationship(res, "INSTANTIATES", "Module:Program", "View:OrderEntryForm") {
		t.Errorf("no INSTANTIATES relationship Module:Program -> View:OrderEntryForm; got %s",
			formatVBRelationships(res))
	}

	// Regression guard for the orphan this rule used to mint (review of
	// PR #6600). An `Application.Run` source_pattern emitted Config:<FormName>
	// while the edge targeted View:<FormName>, so the entity could never be any
	// edge's endpoint — a guaranteed orphan, once per entry-point file, on the
	// very language whose orphan rate #6535 reported.
	for _, e := range res.Entities {
		if string(e.Kind) == "Config" && e.Name == "OrderEntryForm" {
			t.Errorf("Config:OrderEntryForm was emitted; no relationship rule in this bucket "+
				"targets Config, so it is an orphan by construction: %s", formatVBEntities(res))
		}
	}
}

// TestVBNet6537_ModuleGateRequiresFrameworkMarker pins the requires_framework
// gate on the Module pattern in BOTH directions. Deleting `requires_framework:
// true` from the rule survived the full package suite until this test existed.
//
// It also pins the gate's real, weaker strength: import_markers are literal
// substrings matched over the whole file, so a mention inside a `'` comment is
// enough. That is documented in the rule file and asserted here rather than
// being quietly assumed away.
func TestVBNet6537_ModuleGateRequiresFrameworkMarker(t *testing.T) {
	const noMarker = `Module MathHelpers
    Public Function Add(ByVal a As Integer, ByVal b As Integer) As Integer
        Return a + b
    End Function
End Module
`
	res := detectVBNet(t, "src/MathHelpers.vb", noMarker)
	if hasVBEntity(res, "Module", "MathHelpers") {
		t.Errorf("a Module with no WinForms marker anywhere was emitted; the requires_framework "+
			"gate on the Module pattern is not in effect: %s", formatVBEntities(res))
	}

	// Positive control: the identical module WITH a marker does fire, so the
	// negative above is the gate talking and not a broken pattern.
	const withMarker = `Imports System.Windows.Forms

Module MathHelpers
    Public Function Add(ByVal a As Integer, ByVal b As Integer) As Integer
        Return a + b
    End Function
End Module
`
	res = detectVBNet(t, "src/MathHelpers.vb", withMarker)
	if !hasVBEntity(res, "Module", "MathHelpers") {
		t.Errorf("positive control failed: the same module WITH an Imports marker did not fire, "+
			"so the negative case above proves nothing: %s", formatVBEntities(res))
	}

	// Documented weakness, asserted so it cannot drift into being believed
	// stronger than it is: a bare mention in a comment satisfies the gate.
	const markerInComment = `' see System.Windows.Forms docs
Module MathHelpers
End Module
`
	res = detectVBNet(t, "src/MathHelpers.vb", markerInComment)
	if !hasVBEntity(res, "Module", "MathHelpers") {
		t.Errorf("import_markers appear to be more than a literal whole-file substring match; "+
			"the rule file's stated limitation is now wrong and should be tightened: %s",
			formatVBEntities(res))
	}
}

// TestVBNet6537_DoesNotFireOnCSharpSyntax is the anti-widening guard: the same
// WinForms application, written in C#, must produce nothing under the vbnet
// bucket. If a pattern is relaxed to `class\s+(\w+)` or similar, this goes red.
func TestVBNet6537_DoesNotFireOnCSharpSyntax(t *testing.T) {
	res := detectVBNet(t, "src/Forms/OrderEntryForm.cs", csharpWinFormsSource)

	if len(res.Entities) != 0 || len(res.Relationships) != 0 {
		t.Errorf("vbnet rules fired on C# WinForms source (%d entities, %d relationships): %s — "+
			"the patterns are not VB-specific",
			len(res.Entities), len(res.Relationships), formatVBEntities(res))
	}
}

// TestVBNet6537_DoesNotFireOnArbitraryText is the second anti-widening guard: a
// file with none of the VB WinForms markers must stay silent.
func TestVBNet6537_DoesNotFireOnArbitraryText(t *testing.T) {
	const prose = `This is a plain text file. It mentions a Button and a Form and a
Sub-heading, but it is not VB.NET source and declares nothing.
`
	res := detectVBNet(t, "docs/NOTES.txt", prose)
	if len(res.Entities) != 0 {
		t.Errorf("vbnet rules fired on prose: %s", formatVBEntities(res))
	}
}

// TestVBNet6537_FiresOnUserControl pins the UserControl pattern, which the
// Form fixture cannot reach.
func TestVBNet6537_FiresOnUserControl(t *testing.T) {
	res := detectVBNet(t, "src/Controls/AddressEditor.vb", vbUserControlSource)
	if !hasVBEntity(res, "Component", "AddressEditor") {
		t.Errorf("no Component entity named %q; the UserControl pattern did not fire; got %s",
			"AddressEditor", formatVBEntities(res))
	}
	// The Form pattern must not claim a UserControl.
	if hasVBEntity(res, "View", "AddressEditor") {
		t.Errorf("AddressEditor was emitted as a View; the Form pattern is matching UserControl too: %s",
			formatVBEntities(res))
	}
}

func hasVBEntity(res *engine.DetectResult, kind, name string) bool {
	for _, e := range res.Entities {
		if string(e.Kind) == kind && e.Name == name {
			return true
		}
	}
	return false
}

func hasVBRelationship(res *engine.DetectResult, kind, fromID, toID string) bool {
	for _, r := range res.Relationships {
		if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
			return true
		}
	}
	return false
}

func formatVBRelationships(res *engine.DetectResult) string {
	if len(res.Relationships) == 0 {
		return "(no relationships)"
	}
	var b strings.Builder
	for i, r := range res.Relationships {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(r.FromID + " -" + r.Kind + "-> " + r.ToID)
	}
	return b.String()
}

func formatVBEntities(res *engine.DetectResult) string {
	if len(res.Entities) == 0 {
		return "(no entities)"
	}
	var b strings.Builder
	for i, e := range res.Entities {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(string(e.Kind))
		b.WriteString(":")
		b.WriteString(e.Name)
	}
	return b.String()
}
