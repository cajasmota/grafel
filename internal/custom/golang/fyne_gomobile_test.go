package golang_test

import (
	"testing"
)

// Tests for the fyne (desktop GUI) and gomobile (mobile bindings) extractors,
// issue #3218 cluster 7 (Desktop & Mobile).

// findByName returns the first entity with the given Kind and Name, or nil.
func findByName(ents []fullEntity, kind, name string) *fullEntity {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// countWhere counts entities matching a predicate on properties.
func countWhere(ents []fullEntity, pred func(fullEntity) bool) int {
	n := 0
	for _, e := range ents {
		if pred(e) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// fyne
// ---------------------------------------------------------------------------

func TestFyneNoMarkerNoEmit(t *testing.T) {
	src := `package main
func main() {
	a := app.New()
	w := a.NewWindow("x")
	_ = w
}`
	// No fyne.io import marker => the app./NewWindow tokens must NOT match.
	ents := extractFull(t, "custom_go_fyne", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities without fyne marker, got %d: %+v", len(ents), ents)
	}
}

func TestFyneNonGoNoEmit(t *testing.T) {
	ents := extractFull(t, "custom_go_fyne", fi("main.py", "python", `import "fyne.io/fyne/v2"`))
	if len(ents) != 0 {
		t.Fatalf("non-go file should yield nothing, got %d", len(ents))
	}
}

func TestFyneAppWindowWidgets(t *testing.T) {
	ents := extractFull(t, "custom_go_fyne", fixtureFile(t, "fyne_app.go"))

	if e := findByName(ents, "SCOPE.Component", "fyne:app:a"); e == nil {
		t.Fatalf("expected app component fyne:app:a; got %+v", ents)
	} else if e.Props["fyne_kind"] != "app" || e.Props["framework"] != "fyne" {
		t.Fatalf("app props wrong: %+v", e.Props)
	}

	w := findByName(ents, "SCOPE.UIComponent", "fyne:window:w")
	if w == nil {
		t.Fatalf("expected window fyne:window:w")
	}
	if w.Props["window_title"] != "Hello Fyne" {
		t.Fatalf("window title wrong: %q", w.Props["window_title"])
	}

	// widgets: Label, Entry, Button, and container VBox.
	for _, ctor := range []string{"NewLabel", "NewEntry", "NewButton", "NewVBox"} {
		if findByName(ents, "SCOPE.UIComponent", "fyne:widget:"+ctor) == nil {
			t.Errorf("expected widget fyne:widget:%s", ctor)
		}
	}
}

func TestFyneEventHandlers(t *testing.T) {
	ents := extractFull(t, "custom_go_fyne", fixtureFile(t, "fyne_app.go"))

	if findByName(ents, "SCOPE.Pattern", "fyne:event:btn.OnTapped") == nil {
		t.Errorf("expected event fyne:event:btn.OnTapped")
	}
	if findByName(ents, "SCOPE.Pattern", "fyne:event:entry.OnChanged") == nil {
		t.Errorf("expected event fyne:event:entry.OnChanged")
	}
	// setter-style handler
	if findByName(ents, "SCOPE.Pattern", "fyne:event:SetOnClosed") == nil {
		t.Errorf("expected event fyne:event:SetOnClosed")
	}
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && e.Props["pattern_kind"] != "event" {
			t.Errorf("fyne pattern entity has wrong pattern_kind: %+v", e.Props)
		}
	}
}

func TestFyneNativeImport(t *testing.T) {
	// The fyne_app.go fixture imports fyne.io/fyne/v2/app — a native/driver
	// package backing the GUI (the native_module_imports surface).
	ents := extractFull(t, "custom_go_fyne", fixtureFile(t, "fyne_app.go"))
	n := countWhere(ents, func(e fullEntity) bool {
		return e.Kind == "SCOPE.External" && e.Props["native_kind"] == "fyne_driver"
	})
	if n == 0 {
		t.Fatalf("expected at least one fyne native import entity; got %+v", ents)
	}
}

// ---------------------------------------------------------------------------
// gomobile
// ---------------------------------------------------------------------------

func TestGomobileNoMarkerNoEmit(t *testing.T) {
	src := `package main
func main() {
	app.Main(func(a app.App) {})
}`
	ents := extractFull(t, "custom_go_gomobile", fi("main.go", "go", src))
	if len(ents) != 0 {
		t.Fatalf("expected no entities without gomobile marker, got %d: %+v", len(ents), ents)
	}
}

func TestGomobileNonGoNoEmit(t *testing.T) {
	ents := extractFull(t, "custom_go_gomobile", fi("main.kt", "kotlin", `import golang.org/x/mobile`))
	if len(ents) != 0 {
		t.Fatalf("non-go file should yield nothing, got %d", len(ents))
	}
}

func TestGomobileAppRoot(t *testing.T) {
	ents := extractFull(t, "custom_go_gomobile", fixtureFile(t, "gomobile_app.go"))
	e := findByName(ents, "SCOPE.Component", "gomobile:app:Main")
	if e == nil {
		t.Fatalf("expected gomobile:app:Main component; got %+v", ents)
	}
	if e.Props["gomobile_kind"] != "app_root" {
		t.Fatalf("app root props wrong: %+v", e.Props)
	}
}

func TestGomobilePlatformBranching(t *testing.T) {
	ents := extractFull(t, "custom_go_gomobile", fixtureFile(t, "gomobile_app.go"))

	// modern //go:build android || ios constraint
	if e := findByName(ents, "SCOPE.Pattern", "gomobile:platform:go_build:android || ios"); e == nil {
		t.Errorf("expected go_build platform branch; got %+v", ents)
	} else if e.Props["pattern_kind"] != "platform_branch" {
		t.Errorf("platform branch props wrong: %+v", e.Props)
	}

	// runtime.GOOS == "android" guard
	if findByName(ents, "SCOPE.Pattern", "gomobile:platform:goos_guard:android") == nil {
		t.Errorf("expected goos_guard platform branch for android")
	}
}

func TestGomobilePlatformBranchIgnoresNonMobile(t *testing.T) {
	src := `//go:build linux

package x

import "golang.org/x/mobile/app"

func f() {
	if runtime.GOOS == "windows" {
	}
}`
	ents := extractFull(t, "custom_go_gomobile", fi("x.go", "go", src))
	for _, e := range ents {
		if e.Props["pattern_kind"] == "platform_branch" {
			t.Errorf("non-mobile constraint should not emit platform branch: %+v", e)
		}
	}
}

func TestGomobileNativeImports(t *testing.T) {
	ents := extractFull(t, "custom_go_gomobile", fixtureFile(t, "gomobile_app.go"))
	n := countWhere(ents, func(e fullEntity) bool {
		return e.Kind == "SCOPE.External" && e.Props["native_kind"] == "gomobile"
	})
	if n < 2 { // app, gl, event/lifecycle
		t.Fatalf("expected >=2 gomobile native imports, got %d: %+v", n, ents)
	}
}

func TestGomobileBoundFuncs(t *testing.T) {
	ents := extractFull(t, "custom_go_gomobile", fixtureFile(t, "gomobile_bind.go"))

	for _, fn := range []string{"Greet", "Add"} {
		e := findByName(ents, "SCOPE.External", "gomobile:bound:"+fn)
		if e == nil {
			t.Errorf("expected bound func gomobile:bound:%s", fn)
			continue
		}
		if e.Props["native_kind"] != "bound_func" {
			t.Errorf("bound func props wrong: %+v", e.Props)
		}
	}
	// unexported helper must NOT be a bound func
	if findByName(ents, "SCOPE.External", "gomobile:bound:helper") != nil {
		t.Errorf("unexported helper must not be emitted as bound func")
	}
}

func TestGomobileBoundFuncsRequireBindImport(t *testing.T) {
	// gomobile_app.go imports app/gl but NOT bind — exported funcs there must
	// not be treated as bound FFI symbols.
	ents := extractFull(t, "custom_go_gomobile", fixtureFile(t, "gomobile_app.go"))
	if countWhere(ents, func(e fullEntity) bool { return e.Props["native_kind"] == "bound_func" }) != 0 {
		t.Errorf("non-bind package should not emit bound funcs: %+v", ents)
	}
}
