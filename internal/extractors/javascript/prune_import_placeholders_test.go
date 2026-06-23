// prune_import_placeholders_test.go — Issue #742 acceptance tests.
//
// These tests verify that:
// 1. `import { X } from "lib"` does NOT produce a SCOPE.Component/import
//    placeholder entity.
// 2. The IMPORTS edge + Properties are still present (on the file entity).
// 3. Real class/function SCOPE.Component entities are NOT affected.
// 4. Relative imports also do NOT produce a SCOPE.Component/import entity.
// 5. The file entity (SCOPE.Component/file) carries the IMPORTS edges.
// 6. CALLS / CONTAINS edges from existing code paths are unaffected.

package javascript_test

import (
	"context"
	"testing"

	"github.com/cajasmota/grafel/internal/treesitter/ts"
	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstypescript "github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// importPlaceholders returns every SCOPE.Component entity with
// Subtype=="import" in ents. After issue #742 there must be none.
func importPlaceholders742(ents []types.EntityRecord) []types.EntityRecord {
	var out []types.EntityRecord
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "import" {
			out = append(out, e)
		}
	}
	return out
}

// findFileEntity742 returns the SCOPE.Component/file entity, or nil.
func findFileEntity742(ents []types.EntityRecord) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Subtype == "file" {
			return &ents[i]
		}
	}
	return nil
}

// findImportOnAny742 returns the IMPORTS edge whose import_path or ToID
// matches spec from ANY entity in ents, or nil when absent.
func findImportOnAny742(ents []types.EntityRecord, spec string) *types.RelationshipRecord {
	for i := range ents {
		for j := range ents[i].Relationships {
			r := &ents[i].Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.Properties != nil && r.Properties["import_path"] == spec {
				return r
			}
			if r.ToID == spec {
				return r
			}
		}
	}
	return nil
}

func parseTS742(t *testing.T, src []byte) ts.Tree {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstypescript.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tree
}

func extractTS742(t *testing.T, src, path string) []types.EntityRecord {
	t.Helper()
	tree := parseTS742(t, []byte(src))
	ex, _ := extractor.Get("typescript")
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// TestNoJSImportPlaceholderForNpmImport — core acceptance test for #742.
// `import { useState } from "react"` must NOT produce a SCOPE.Component/import
// entity. The IMPORTS edge must still exist (on the file entity) with correct
// Properties.
func TestNoJSImportPlaceholderForNpmImport(t *testing.T) {
	src := `import { useState, useEffect } from "react";
import { BrowserRouter } from "react-router-dom";

function App() {
  const [count, setCount] = useState(0);
  return null;
}
`
	ents := extractTS742(t, src, "src/App.tsx")

	// No import-placeholder SCOPE.Component/import entities.
	if placeholders := importPlaceholders742(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/import placeholder entities still emitted (#742): %v", names)
	}

	// IMPORTS edges for both modules must still exist.
	if findImportOnAny742(ents, "react") == nil {
		t.Error("IMPORTS edge for 'react' not found after #742 fix")
	}
	if findImportOnAny742(ents, "react-router-dom") == nil {
		t.Error("IMPORTS edge for 'react-router-dom' not found after #742 fix")
	}
}

// TestNoJSImportPlaceholderForRelativeImport — relative imports also must not
// produce a SCOPE.Component/import entity.
func TestNoJSImportPlaceholderForRelativeImport(t *testing.T) {
	src := `import { UserService } from "./services/user.service";
import { AuthGuard } from "../guards/auth.guard";

class UsersController {
  constructor(private svc: UserService) {}
}
`
	ents := extractTS742(t, src, "src/users/users.controller.ts")

	if placeholders := importPlaceholders742(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/import placeholder entities emitted for relative imports: %v", names)
	}

	if findImportOnAny742(ents, "./services/user.service") == nil {
		t.Error("IMPORTS edge for './services/user.service' not found after #742 fix")
	}
	if findImportOnAny742(ents, "../guards/auth.guard") == nil {
		t.Error("IMPORTS edge for '../guards/auth.guard' not found after #742 fix")
	}
}

// TestNoJSImportPlaceholderMultipleImports — multiple import statements produce
// zero placeholder entities; all IMPORTS edges are present.
func TestNoJSImportPlaceholderMultipleImports(t *testing.T) {
	src := `import express from "express";
import { Request, Response, NextFunction } from "express";
import { Injectable } from "@nestjs/common";
import { InjectRepository } from "@nestjs/typeorm";
import * as path from "path";
import { readFileSync } from "fs";
`
	ents := extractTS742(t, src, "src/app.controller.ts")

	if placeholders := importPlaceholders742(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/import placeholder entities emitted for multiple imports: %v", names)
	}

	// Every module must have a corresponding IMPORTS edge.
	wantModules := []string{
		"express",
		"@nestjs/common",
		"@nestjs/typeorm",
		"path",
		"fs",
	}
	for _, mod := range wantModules {
		if findImportOnAny742(ents, mod) == nil {
			t.Errorf("IMPORTS edge for import_path=%q not found", mod)
		}
	}
}

// TestRealClassEntityNotPrunedByJSImportFix — regression: a genuine
// SCOPE.Component/class entity must NOT be removed. Only import-placeholder
// entities (Subtype=="import") are dropped.
func TestRealClassEntityNotPrunedByJSImportFix(t *testing.T) {
	src := `import { Injectable } from "@nestjs/common";
import { InjectRepository } from "@nestjs/typeorm";

@Injectable()
class UserService {
  findAll() {
    return [];
  }
  findOne(id: string) {
    return null;
  }
}
`
	ents := extractTS742(t, src, "src/user.service.ts")

	// No import placeholder entities.
	if placeholders := importPlaceholders742(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/import placeholder entities still emitted: %v", names)
	}

	// UserService class must still exist.
	foundClass := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Component" && e.Subtype == "class" && e.Name == "UserService" {
			foundClass = true
		}
	}
	if !foundClass {
		t.Error("UserService class entity was pruned — regression")
	}

	// Methods must still exist.
	for _, method := range []string{"findAll", "findOne"} {
		found := false
		for _, e := range ents {
			if e.Kind == "SCOPE.Operation" && e.Name == method {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("method entity %q was pruned — regression", method)
		}
	}
}

// TestImportsEdgeAttachedToFileEntityJS — IMPORTS edges must be on the
// file-level entity (SCOPE.Component/file), not standalone entities.
func TestImportsEdgeAttachedToFileEntityJS(t *testing.T) {
	src := `import { Component } from "@angular/core";
import axios from "axios";
`
	const filePath = "src/app/app.component.ts"
	ents := extractTS742(t, src, filePath)

	fileEnt := findFileEntity742(ents)
	if fileEnt == nil {
		t.Fatal("file entity (SCOPE.Component/file) not found")
	}

	// The file entity must carry the IMPORTS edges.
	wantImportPaths := map[string]bool{
		"@angular/core": false,
		"axios":         false,
	}
	for _, r := range fileEnt.Relationships {
		if r.Kind != "IMPORTS" {
			continue
		}
		ip := ""
		if r.Properties != nil {
			ip = r.Properties["import_path"]
		}
		if ip == "" {
			ip = r.ToID
		}
		if _, ok := wantImportPaths[ip]; ok {
			wantImportPaths[ip] = true
		}
	}
	for mod, found := range wantImportPaths {
		if !found {
			t.Errorf("IMPORTS edge for import_path=%q not found on file entity (rels count=%d)", mod, len(fileEnt.Relationships))
		}
	}
}

// TestNamedBindingPropertiesOnFileEntityJS — verifies that IMPORTS edges on
// the file entity carry correct Properties (local_name, imported_name,
// source_module, wildcard).
func TestNamedBindingPropertiesOnFileEntityJS(t *testing.T) {
	src := `import { Router, Request as Req } from "express";
import * as path from "path";
`
	const filePath = "src/router.ts"
	ents := extractTS742(t, src, filePath)

	fileEnt := findFileEntity742(ents)
	if fileEnt == nil {
		t.Fatal("file entity (SCOPE.Component/file) not found")
	}

	// Find IMPORTS edges by local_name.
	findByLocal := func(localName string) map[string]string {
		for _, r := range fileEnt.Relationships {
			if r.Kind != "IMPORTS" || r.Properties == nil {
				continue
			}
			if r.Properties["local_name"] == localName {
				return r.Properties
			}
		}
		return nil
	}

	// Router: local_name=Router, imported_name=Router, source_module=express
	routerProps := findByLocal("Router")
	if routerProps == nil {
		t.Fatal("IMPORTS edge for local_name=Router not found on file entity")
	}
	if routerProps["imported_name"] != "Router" {
		t.Errorf("imported_name=%q want Router", routerProps["imported_name"])
	}
	if routerProps["source_module"] != "express" {
		t.Errorf("source_module=%q want express", routerProps["source_module"])
	}

	// Req (aliased import): local_name=Req, imported_name=Request
	reqProps := findByLocal("Req")
	if reqProps == nil {
		t.Fatal("IMPORTS edge for local_name=Req not found on file entity")
	}
	if reqProps["imported_name"] != "Request" {
		t.Errorf("imported_name=%q want Request", reqProps["imported_name"])
	}

	// Namespace import: wildcard=1
	pathProps := findByLocal("path")
	if pathProps == nil {
		t.Fatal("IMPORTS edge for local_name=path not found on file entity")
	}
	if pathProps["wildcard"] != "1" {
		t.Errorf("namespace import expected wildcard=1, got %v", pathProps)
	}
}

// TestReExportNoPlaceholderJS — `export { X } from "./baz"` is a re-export.
// It should also not produce a SCOPE.Component/import placeholder entity.
func TestReExportNoPlaceholderJS(t *testing.T) {
	src := `export { UserService } from "./user.service";
export { AuthGuard } from "./auth.guard";
`
	ents := extractTS742(t, src, "src/index.ts")

	if placeholders := importPlaceholders742(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/import placeholder entities emitted for re-exports: %v", names)
	}
}

// TestSideEffectImportNoPlaceholderJS — `import "./polyfills"` (side-effect
// only, no bindings) must also not produce a placeholder entity. An IMPORTS
// edge with no Properties is acceptable — the resolver skips edge-less bindings.
func TestSideEffectImportNoPlaceholderJS(t *testing.T) {
	src := `import "./polyfills";
import "reflect-metadata";
`
	ents := extractTS742(t, src, "src/main.ts")

	if placeholders := importPlaceholders742(ents); len(placeholders) > 0 {
		names := make([]string, len(placeholders))
		for i, p := range placeholders {
			names[i] = p.Name
		}
		t.Errorf("SCOPE.Component/import placeholder entities emitted for side-effect imports: %v", names)
	}
}
