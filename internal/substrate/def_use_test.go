package substrate

import "testing"

func TestDefUseSnifferRegistry_T1(t *testing.T) {
	for _, lang := range []string{"jsts", "python", "java", "go"} {
		if DefUseSnifferFor(lang) == nil {
			t.Errorf("def-use sniffer not registered for %q", lang)
		}
	}
}

func TestDefUseSniffer_JSTS_basic(t *testing.T) {
	src := `
function f(x) {
  let y = 1;
  let z = y + x;
  return z;
}`
	defs, uses := sniffDefUseJSTS(src)
	if len(defs) == 0 {
		t.Fatalf("no defs detected")
	}
	if len(uses) == 0 {
		t.Fatalf("no uses detected")
	}
	if !containsVarDef(defs, "f", "y") || !containsVarDef(defs, "f", "z") {
		t.Errorf("expected y and z defs in f, got %+v", defs)
	}
	if !containsVarUse(uses, "f", "y") || !containsVarUse(uses, "f", "z") {
		t.Errorf("expected y and z uses in f, got %+v", uses)
	}
}

func TestDefUseSniffer_Python_basic(t *testing.T) {
	src := "def f(x):\n    y = 1\n    z = y + x\n    return z\n"
	defs, uses := sniffDefUsePython(src)
	if !containsVarDef(defs, "f", "y") || !containsVarDef(defs, "f", "z") {
		t.Errorf("expected y/z defs, got %+v", defs)
	}
	if !containsVarUse(uses, "f", "z") {
		t.Errorf("expected z use, got %+v", uses)
	}
}

func TestDefUseSniffer_Java_basic(t *testing.T) {
	src := `class C {
  public void f(int x) {
    int y = 1;
    int z = y + x;
    System.out.println(z);
  }
}`
	defs, uses := sniffDefUseJava(src)
	if !containsVarDef(defs, "f", "y") || !containsVarDef(defs, "f", "z") {
		t.Errorf("expected y/z defs in f, got %+v", defs)
	}
	if !containsVarUse(uses, "f", "z") {
		t.Errorf("expected z use in f, got %+v", uses)
	}
}

func TestDefUseSniffer_Go_basic(t *testing.T) {
	src := `package p
func f(x int) int {
	y := 1
	z := y + x
	return z
}
`
	defs, uses := sniffDefUseGo(src)
	if !containsVarDef(defs, "f", "y") || !containsVarDef(defs, "f", "z") {
		t.Errorf("expected y/z defs in f, got %+v", defs)
	}
	if !containsVarUse(uses, "f", "z") {
		t.Errorf("expected z use in f, got %+v", uses)
	}
}

func containsVarDef(defs []VarDef, fn, v string) bool {
	for _, d := range defs {
		if d.Function == fn && d.Var == v {
			return true
		}
	}
	return false
}

func containsVarUse(uses []VarUse, fn, v string) bool {
	for _, u := range uses {
		if u.Function == fn && u.Var == v {
			return true
		}
	}
	return false
}
