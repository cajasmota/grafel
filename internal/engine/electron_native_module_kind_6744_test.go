package engine

// electron_native_module_kind_6744_test.go — the BEHAVIOURAL half of #6744.
//
// internal/entkinds' guard pins what electron.yaml DECLARES. Nothing pinned
// what the engine EMITS: electron_detect_test.go collects `e.Name` only and
// never looks at `e.Kind`, so the three native-module patterns could have been
// declaring any kind at all — including the retired `ExternalAPI` they carried
// for the whole of #6451 — and the engine suite stayed green.
//
// electron.yaml's native_module_imports family is the only site in #6744 whose
// graph output actually changes: it is bound (source_patterns[].entity_type),
// unlike kubernetes/extras.yaml's three sites, which sit under keys schema.go
// binds nowhere. So this is the one place the change needs an end-to-end pin
// rather than a static one.
//
// It asserts the kind is VALID, not merely that it is the string "Module": a
// future rename to another declared kind is fine, a re-mint of an undeclared
// name is the bug.

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func TestElectronNativeModuleEmitsDeclaredKind(t *testing.T) {
	rules, err := LoadAllRules()
	if err != nil {
		t.Fatalf("LoadAllRules: %v", err)
	}
	const src = `
import { app, BrowserWindow } from 'electron';
const bindings = require('bindings');
const prebuild = require('node-gyp-build');
const addon = require('./build/Release/native.node');
`
	result, derr := New(rules).Detect(context.Background(), extractor.FileInput{
		Path:     "main.ts",
		Content:  []byte(src),
		Language: "javascript_typescript",
	})
	if derr != nil {
		t.Fatalf("Detect: %v", derr)
	}

	// The three native-addon loaders, by the name each pattern captures.
	want := map[string]bool{
		"require('bindings')":         false,
		"require('node-gyp-build')":   false,
		"./build/Release/native.node": false,
	}
	for _, e := range result.Entities {
		if _, ok := want[e.Name]; !ok {
			continue
		}
		want[e.Name] = true
		if !types.IsValidEntityKind(e.Kind) {
			t.Errorf("electron native-module entity %q was emitted with Kind %q, which "+
				"types.AllEntityKinds() does not declare (#6744).\n\n"+
				"internal/engine/detector.go copies source_patterns[].entity_type straight into "+
				"EntityRecord.Kind with no validation, so an undeclared name here reaches the "+
				"graph unexamined — that is how these three patterns kept emitting `ExternalAPI` "+
				"through #6451's retirement of it. Use a kind declared in "+
				"internal/types/kinds.go.", e.Name, e.Kind)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("the native_module_imports pattern for %q produced no entity at all; this "+
				"test is not observing the family it claims to pin", name)
		}
	}
}
