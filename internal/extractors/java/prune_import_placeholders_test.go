// prune_import_placeholders_test.go — Issue #681 acceptance tests.
//
// These tests verify that:
// 1. External import names (List, Data, Inject, ...) do NOT produce
//    orphan SCOPE.Component placeholder entities.
// 2. The IMPORTS edge + Properties are still present (on the file entity).
// 3. Real in-file SCOPE.Component entities (classes, interfaces, enums) are
//    NOT affected.
// 4. A file with `import java.util.List; List<String> x = ...` produces NO
//    orphan SCOPE.Component for "List".

package java_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	_ "github.com/cajasmota/grafel/internal/extractors/java"
	"github.com/cajasmota/grafel/internal/types"
)

// noOrphanSCOPEComponents returns every SCOPE.Component entity in ents
// that has no Subtype (i.e. the old import-placeholder shape). These
// should not exist after issue #681.
func orphanImportPlaceholders(ents []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "" {
			out = append(out, e)
		}
	}
	return out
}

// extractJavaForTest parses src and runs the full Java extractor pipeline
// via the registered extractor. Uses parseForTest from java_test.go (same
// package java_test).
func extractJavaForTest(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	ex, ok := extractor.Get("java")
	if !ok {
		t.Fatal("java extractor not registered")
	}
	tree := parseForTest(t, src)
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "Test.java",
		Language: "java",
		Content:  []byte(src),
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// TestNoImportPlaceholderForExternalImport — core acceptance test for #681.
// `import java.util.List;` must NOT produce an orphan SCOPE.Component.
// The IMPORTS edge must still exist (on the file entity) with correct Properties.
func TestNoImportPlaceholderForExternalImport(t *testing.T) {
	src := `package com.example;

import java.util.List;

public class UserService {
    List<String> getNames() { return null; }
}
`
	ents := extractJavaForTest(t, src)

	// No import-placeholder SCOPE.Component entities.
	if orphans := orphanImportPlaceholders(ents); len(orphans) > 0 {
		names := make([]string, len(orphans))
		for i, o := range orphans {
			names[i] = o.Name
		}
		t.Errorf("import-placeholder entities still emitted (#681): %v", names)
	}

	// The IMPORTS edge for java.util.List must still exist.
	found := false
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" && r.Properties != nil &&
				r.Properties["source_module"] == "java.util" &&
				r.Properties["local_name"] == "List" {
				found = true
			}
		}
	}
	if !found {
		t.Error("IMPORTS edge for java.util.List not found after #681 fix")
	}
}

// TestNoImportPlaceholderMultipleImports — multiple external imports
// should produce zero placeholder entities and all IMPORTS edges.
func TestNoImportPlaceholderMultipleImports(t *testing.T) {
	src := `package com.example;

import java.util.List;
import java.util.Map;
import javax.inject.Inject;
import org.springframework.stereotype.Component;

@Component
public class DataService {
    @Inject
    DataRepository repo;
    List<String> getAll() { return null; }
}
`
	ents := extractJavaForTest(t, src)

	if orphans := orphanImportPlaceholders(ents); len(orphans) > 0 {
		names := make([]string, len(orphans))
		for i, o := range orphans {
			names[i] = o.Name
		}
		t.Errorf("import-placeholder entities still emitted for multiple imports: %v", names)
	}

	// All four IMPORTS edges must be present.
	want := map[string]bool{
		"java.util":                      false, // List
		"javax.inject":                   false, // Inject
		"org.springframework.stereotype": false, // Component
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" && r.Properties != nil {
				mod := r.Properties["source_module"]
				if _, ok := want[mod]; ok {
					want[mod] = true
				}
			}
		}
	}
	for mod, seen := range want {
		if !seen {
			t.Errorf("IMPORTS edge for source_module=%q not found", mod)
		}
	}
}

// TestRealClassEntityNotPruned — regression: a genuine SCOPE.Component
// class/interface/enum entity (with a non-empty Subtype) must NOT be
// removed. Only import-placeholder entities (Subtype="") are affected.
func TestRealClassEntityNotPruned(t *testing.T) {
	src := `package com.example;

import java.util.List;
import java.util.ArrayList;

public class UserService {
    public List<String> getNames() { return new ArrayList<>(); }
}
public interface UserRepository {
    String findById(int id);
}
`
	ents := extractJavaForTest(t, src)

	// import-placeholder SCOPE.Component entities must be absent.
	if orphans := orphanImportPlaceholders(ents); len(orphans) > 0 {
		names := make([]string, len(orphans))
		for i, o := range orphans {
			names[i] = o.Name
		}
		t.Errorf("import-placeholder entities still emitted: %v", names)
	}

	// Real class and interface entities must still exist.
	foundClass, foundIface := false, false
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Name == "UserService" && e.Subtype == "class" {
			foundClass = true
		}
		if e.Kind == "SCOPE.Component" && e.Name == "UserRepository" && e.Subtype == "interface" {
			foundIface = true
		}
	}
	if !foundClass {
		t.Error("UserService class entity was pruned — regression")
	}
	if !foundIface {
		t.Error("UserRepository interface entity was pruned — regression")
	}
}

// TestNoImportPlaceholderListUsedInMethodBody — the scenario that caused
// dangling placeholders: `import java.util.List;` + method body uses
// `List<String>`. After #681 the REFERENCES edge goes to the real external
// entity (ext:java:List) and no orphan placeholder for "List" is created.
func TestNoImportPlaceholderListUsedInMethodBody(t *testing.T) {
	src := `package com.example;

import java.util.List;
import java.util.ArrayList;

public class Svc {
    List<String> items = new ArrayList<>();
    List<String> getItems() {
        List<String> copy = new ArrayList<>(items);
        return copy;
    }
}
`
	ents := extractJavaForTest(t, src)

	// No import-placeholder entities.
	if orphans := orphanImportPlaceholders(ents); len(orphans) > 0 {
		names := make([]string, len(orphans))
		for i, o := range orphans {
			names[i] = o.Name
		}
		t.Errorf("import-placeholder entities emitted for List/ArrayList: %v", names)
	}

	// IMPORTS edges still present.
	foundList, foundArrayList := false, false
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" && r.Properties != nil {
				switch r.Properties["local_name"] {
				case "List":
					foundList = true
				case "ArrayList":
					foundArrayList = true
				}
			}
		}
	}
	if !foundList {
		t.Error("IMPORTS edge for List not found")
	}
	if !foundArrayList {
		t.Error("IMPORTS edge for ArrayList not found")
	}
}

// TestImportsEdgeAttachedToFileEntity — the IMPORTS edges must land on
// the file-level entity (Subtype="file", Name=file path), not a
// separate entity.
func TestImportsEdgeAttachedToFileEntity(t *testing.T) {
	src := `package com.example;

import java.util.List;

public class Demo {}
`
	ents := extractJavaForTest(t, src)

	// Find the file entity.
	var fileEnt *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "file" {
			fileEnt = &ents[i]
			break
		}
	}
	if fileEnt == nil {
		t.Fatal("file entity (SCOPE.Component subtype=file) not found")
	}

	// The file entity must carry the IMPORTS edge.
	found := false
	for _, r := range fileEnt.Relationships {
		if r.Kind == "IMPORTS" && r.Properties != nil &&
			r.Properties["local_name"] == "List" {
			found = true
		}
	}
	if !found {
		t.Errorf("file entity does not carry IMPORTS edge for List (rels=%+v)", fileEnt.Relationships)
	}
}

// TestWildcardImportNoPlaceholder — wildcard imports also must not create
// placeholder entities.
func TestWildcardImportNoPlaceholder(t *testing.T) {
	src := `package com.example;

import java.util.*;
import org.springframework.stereotype.*;

public class Demo {}
`
	ents := extractJavaForTest(t, src)

	if orphans := orphanImportPlaceholders(ents); len(orphans) > 0 {
		names := make([]string, len(orphans))
		for i, o := range orphans {
			names[i] = o.Name
		}
		t.Errorf("import-placeholder entities emitted for wildcard imports: %v", names)
	}

	// Wildcard IMPORTS edges must still be present.
	found := false
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "IMPORTS" && r.Properties != nil &&
				r.Properties["wildcard"] == "1" {
				found = true
			}
		}
	}
	if !found {
		t.Error("wildcard IMPORTS edge not found")
	}
}
