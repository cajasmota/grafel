// Package javascript — issue #4322 heritage-edge proving tests.
//
// These model the LIVE upvate-v3 orphan shapes (not synthetic primitives):
// NestJS framework-interface implementors and TypeORM/base-entity subclasses
// that were extracted as isolated SCOPE.Components because the JS/TS extractor
// dropped every class heritage clause except the narrow Angular guard path.
//
// After #4322 each such class emits an EXTENDS / IMPLEMENTS edge, so the node
// is no longer a graph island.
package javascript_test

import (
	"context"
	"testing"

	tssmacker "github.com/cajasmota/grafel/internal/treesitter/ts/smacker"
	tstsx "github.com/smacker/go-tree-sitter/typescript/tsx"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

func extractHeritageTS(t *testing.T, path string, content []byte) []types.EntityRecord {
	t.Helper()
	parser, err := tssmacker.New().NewParser(tssmacker.WrapLanguage(tstsx.GetLanguage()))
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ext, _ := extreg.Get("typescript")
	ents, err := ext.Extract(context.Background(), extreg.FileInput{
		Path:     path,
		Content:  content,
		Language: "typescript",
		TSTree:   tree,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return ents
}

// hasHeritageEdge reports whether the class named `from` carries a relationship
// of kind `kind` whose ToID leaf is `to`.
func hasHeritageEdge(ents []types.EntityRecord, from, kind, to string) bool {
	for _, e := range ents {
		if e.Name != from || e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == kind && r.ToID == to {
				return true
			}
		}
	}
	return false
}

// classRels returns the EXTENDS/IMPLEMENTS edges on the named class.
func classRels(ents []types.EntityRecord, from string) []types.RelationshipRecord {
	var out []types.RelationshipRecord
	for _, e := range ents {
		if e.Name != from || e.Kind != "SCOPE.Component" {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindExtends) ||
				r.Kind == string(types.RelationshipKindImplements) {
				out = append(out, r)
			}
		}
	}
	return out
}

// TestIssue4322_NestInterfaceImplements proves the clearest orphan win: a class
// implementing a NestJS framework interface emits an IMPLEMENTS edge so the
// implementer is connected (the interface itself is external/unresolved — the
// edge still de-islands the class).
func TestIssue4322_NestInterfaceImplements(t *testing.T) {
	src := []byte(`import { NestInterceptor, NestMiddleware, OnApplicationBootstrap } from '@nestjs/common';
import { EntitySubscriberInterface } from 'typeorm';

export class LoggingInterceptor implements NestInterceptor {
  intercept(ctx, next) { return next.handle(); }
}

export class AuthMiddleware implements NestMiddleware {
  use(req, res, next) { next(); }
}

export class StartupHook implements OnApplicationBootstrap {
  onApplicationBootstrap() {}
}

export class AuditSubscriber implements EntitySubscriberInterface<User> {
  listenTo() { return User; }
}
`)
	ents := extractHeritageTS(t, "interceptors.ts", src)

	cases := []struct{ from, to string }{
		{"LoggingInterceptor", "NestInterceptor"},
		{"AuthMiddleware", "NestMiddleware"},
		{"StartupHook", "OnApplicationBootstrap"},
		{"AuditSubscriber", "EntitySubscriberInterface"}, // generic stripped
	}
	for _, c := range cases {
		if !hasHeritageEdge(ents, c.from, string(types.RelationshipKindImplements), c.to) {
			t.Errorf("expected %s IMPLEMENTS %s edge (orphan not connected)", c.from, c.to)
		}
	}
}

// TestIssue4322_BaseEntityExtends proves the base-entity orphan win: entities
// that `extends AuditableEntity / SoftDeletableEntity / MinimalEntity` emit an
// EXTENDS edge. When the base is defined in-repo the bare-name ToID resolves
// via byName; here we assert the edge is emitted with the right leaf name.
func TestIssue4322_BaseEntityExtends(t *testing.T) {
	src := []byte(`import { Entity } from 'typeorm';
import { AuditableEntity } from './base/auditable.entity';

@Entity()
export class User extends AuditableEntity {
  id: number;
}

export class Product extends SoftDeletableEntity {}

export class Tag extends MinimalEntity {}
`)
	ents := extractHeritageTS(t, "user.entity.ts", src)

	cases := []struct{ from, to string }{
		{"User", "AuditableEntity"},
		{"Product", "SoftDeletableEntity"},
		{"Tag", "MinimalEntity"},
	}
	for _, c := range cases {
		if !hasHeritageEdge(ents, c.from, string(types.RelationshipKindExtends), c.to) {
			t.Errorf("expected %s EXTENDS %s edge (orphan not connected)", c.from, c.to)
		}
	}
}

// TestIssue4322_ExtendsAndImplements proves a class with BOTH clauses emits
// both edges, and that generic args / qualified prefixes are reduced to the
// leaf type name.
func TestIssue4322_ExtendsAndImplements(t *testing.T) {
	src := []byte(`export class OrderService extends ns.BaseService<Order> implements OnModuleInit, OnModuleDestroy {
  onModuleInit() {}
  onModuleDestroy() {}
}
`)
	ents := extractHeritageTS(t, "order.service.ts", src)

	if !hasHeritageEdge(ents, "OrderService", string(types.RelationshipKindExtends), "BaseService") {
		t.Errorf("expected OrderService EXTENDS BaseService (qualified+generic stripped)")
	}
	for _, iface := range []string{"OnModuleInit", "OnModuleDestroy"} {
		if !hasHeritageEdge(ents, "OrderService", string(types.RelationshipKindImplements), iface) {
			t.Errorf("expected OrderService IMPLEMENTS %s", iface)
		}
	}
}

// TestIssue4322_NoHeritageNoEdge is the guardrail: a class with no heritage
// clause emits no EXTENDS/IMPLEMENTS edge (no fabricated edges).
func TestIssue4322_NoHeritageNoEdge(t *testing.T) {
	src := []byte(`export class PlainThing {
  doStuff() {}
}
`)
	ents := extractHeritageTS(t, "plain.ts", src)
	if rels := classRels(ents, "PlainThing"); len(rels) != 0 {
		t.Errorf("expected no heritage edges on a class with no heritage clause, got %v", rels)
	}
}

// TestIssue4322_AngularLifecycleImplements proves the generalization to the
// Angular-decorated class path: an @Injectable that `implements OnInit` (a
// classic Angular lifecycle orphan) still emits the IMPLEMENTS edge, and does
// not duplicate the existing guard IMPLEMENTS when both apply.
func TestIssue4322_AngularLifecycleImplements(t *testing.T) {
	src := []byte(`import { Injectable, OnInit, OnDestroy } from '@angular/core';

@Injectable()
export class DataService implements OnInit, OnDestroy {
  ngOnInit() {}
  ngOnDestroy() {}
}
`)
	ents := extractHeritageTS(t, "data.service.ts", src)
	for _, iface := range []string{"OnInit", "OnDestroy"} {
		if !hasHeritageEdge(ents, "DataService", string(types.RelationshipKindImplements), iface) {
			t.Errorf("expected DataService IMPLEMENTS %s on the Angular path", iface)
		}
	}
	// No duplicate edges.
	seen := map[string]int{}
	for _, r := range classRels(ents, "DataService") {
		seen[r.Kind+"|"+r.ToID]++
	}
	for k, n := range seen {
		if n != 1 {
			t.Errorf("duplicate heritage edge %s emitted %d times", k, n)
		}
	}
}
