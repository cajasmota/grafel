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
		out = append(out, entitySummary{Kind: ent.Kind, Subtype: ent.Subtype, Name: ent.Name})
	}
	return out
}

type entitySummary struct{ Kind, Subtype, Name string }

func containsEntity(ents []entitySummary, kind, name string) bool {
	for _, e := range ents {
		if e.Kind == kind && e.Name == name {
			return true
		}
	}
	return false
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
