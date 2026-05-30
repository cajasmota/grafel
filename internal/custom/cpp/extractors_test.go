package cpp_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"

	_ "github.com/cajasmota/archigraph/internal/custom/cpp"
)

func fi(path, lang, src string) extreg.FileInput {
	return extreg.FileInput{Path: path, Language: lang, Content: []byte(src)}
}

func extract(t *testing.T, name string, file extreg.FileInput) []entitySummary {
	t.Helper()
	e, ok := extreg.Get(name)
	if !ok {
		t.Fatalf("extractor %q not registered", name)
	}
	ents, err := e.Extract(context.Background(), file)
	if err != nil {
		t.Fatalf("extract error: %v", err)
	}
	var out []entitySummary
	for _, ent := range ents {
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name, Props: ent.Properties})
	}
	return out
}

type entitySummary struct {
	Kind, Subtype, Name string
	Props               map[string]string
}

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
}

// findEndpoint returns the SCOPE.Operation entity whose Name equals the given
// "VERB /path" string, or nil if no such endpoint was extracted. Used by the
// value-asserting routing tests to inspect handler_name / route_path / etc.
func findEndpoint(ents []entitySummary, name string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// ormFindEntity returns the first entity matching kind/subtype/name, or nil.
// Namespaced helper for the deep ORM/driver value-asserting tests (#3493).
func ormFindEntity(ents []entitySummary, kind, subtype, name string) *entitySummary {
	for i := range ents {
		if ents[i].Kind == kind && ents[i].Subtype == subtype && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// assertEndpoint asserts that an endpoint with name "verb path" exists and that
// its route_path, http_method, and handler_name properties match exactly. This
// is the TS/JS bar for proving (verb, path, handler) attribution — not len>0.
func assertEndpoint(t *testing.T, ents []entitySummary, verb, path, handler string) {
	t.Helper()
	name := verb + " " + path
	ep := findEndpoint(ents, name)
	if ep == nil {
		t.Fatalf("expected endpoint %q, got %v", name, ents)
	}
	if got := ep.Props["http_method"]; got != verb {
		t.Errorf("endpoint %q: http_method = %q, want %q", name, got, verb)
	}
	if got := ep.Props["route_path"]; got != path {
		t.Errorf("endpoint %q: route_path = %q, want %q", name, got, path)
	}
	if handler != "" {
		if got := ep.Props["handler_name"]; got != handler {
			t.Errorf("endpoint %q: handler_name = %q, want %q", name, got, handler)
		}
	}
}

// ormProp asserts that the entity identified by kind/subtype/name carries
// property key=want; it fails the test otherwise.
func ormProp(t *testing.T, ents []entitySummary, kind, subtype, name, key, want string) {
	t.Helper()
	e := ormFindEntity(ents, kind, subtype, name)
	if e == nil {
		t.Fatalf("no %s/%s entity named %q (got %+v)", kind, subtype, name, ents)
	}
	if got := e.Props[key]; got != want {
		t.Errorf("entity %q prop %q = %q, want %q", name, key, got, want)
	}
}

// ---------------------------------------------------------------------------
// Qt
// ---------------------------------------------------------------------------

func TestQtQObject(t *testing.T) {
	src := `
class MainWindow : public QMainWindow {
    Q_OBJECT
public:
    explicit MainWindow(QWidget *parent = nullptr);
};
`
	ents := extract(t, "custom_cpp_qt", fi("mainwindow.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "MainWindow") {
		t.Error("expected MainWindow UIComponent")
	}
}

func TestQtSignals(t *testing.T) {
	src := `
class Button : public QPushButton {
    Q_OBJECT
signals:
    void clicked(bool checked);
    void pressed();
};
`
	ents := extract(t, "custom_cpp_qt", fi("button.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.UIComponent", "Button") {
		t.Error("expected Button UIComponent")
	}
	if !containsEntity(ents, "SCOPE.Pattern", "clicked") {
		t.Error("expected clicked signal pattern")
	}
}

func TestQtSlots(t *testing.T) {
	src := `
class Counter : public QObject {
    Q_OBJECT
public slots:
    void increment();
    void reset();
};
`
	ents := extract(t, "custom_cpp_qt", fi("counter.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Operation", "increment") {
		t.Error("expected increment slot operation")
	}
}

func TestQtConnectOldStyle(t *testing.T) {
	src := `connect(btn, SIGNAL(clicked(bool)), this, SLOT(onClicked()))`
	ents := extract(t, "custom_cpp_qt", fi("mainwindow.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "connect:btn::clicked->onClicked") {
		t.Error("expected old-style connect pattern")
	}
}

func TestQtConnectNewStyle(t *testing.T) {
	src := `connect(btn, &QPushButton::clicked, this, &MainWindow::onClicked)`
	ents := extract(t, "custom_cpp_qt", fi("mainwindow.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "connect:QPushButton::clicked->onClicked") {
		t.Error("expected new-style connect pattern")
	}
}

func TestQtProperty(t *testing.T) {
	src := `
class Slider : public QWidget {
    Q_OBJECT
    Q_PROPERTY(int value READ value WRITE setValue NOTIFY valueChanged)
};
`
	ents := extract(t, "custom_cpp_qt", fi("slider.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "property:value") {
		t.Error("expected property:value pattern")
	}
}

func TestQtNoMatch(t *testing.T) {
	src := `#include <iostream>\nint main() { return 0; }`
	ents := extract(t, "custom_cpp_qt", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}

func TestQtWrongLanguage(t *testing.T) {
	src := `class MainWindow : public QMainWindow { Q_OBJECT };`
	ents := extract(t, "custom_cpp_qt", fi("window.c", "c", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}
