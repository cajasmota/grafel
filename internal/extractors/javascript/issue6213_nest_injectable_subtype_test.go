// Package javascript — issue #6213 regression tests.
//
// The base TS AST extractor already knows which framework an @Injectable class
// belongs to: frameworkForClass (#4503) resolves the decorator's import origin
// and stamps framework="nestjs" or framework="angular". The SUBTYPE ignored that
// answer and was read straight out of angularClassDecorators, so a NestJS
// provider in a file with no Angular anywhere in the tree was stamped
// subtype="angular_service".
//
// That is not merely a cosmetic mislabel. The subtype is the fold vocabulary:
// engine.ClassLikeComponentSubtypes — which decides whether an entity is a class
// representation at all (engine.IsClassFoldSource, cmd/grafel's #1613 class
// shadow fold and foldFileComponentDuplicates) — carries the framework-neutral
// "service", and a NestJS provider never matched it. It also disagreed with the
// NestJS custom extractor, which emits subtype="service" for the SAME entity id
// (id = f(org, project, source_file, kind, name); both are
// SCOPE.Component/UsersService in users.service.ts), so one class symbol had two
// contradictory subtypes and which one reached the graph was decided by the
// same-id assembly dedup order.
//
// #3970 already applied this exact reasoning to the NestJS-only decorators:
// @Controller/@Resolver/@WebSocketGateway are stamped controller/resolver/
// gateway "so the node folds/dedups cleanly with the custom extractor's
// same-named entity". @Injectable was missed because it is the one decorator
// Angular and NestJS share, so it could not be resolved from the decorator name
// alone — which is precisely what frameworkForClass exists to do.
package javascript_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/grafel/internal/extractor"
	tstsx "github.com/cajasmota/grafel/internal/treesitter/ts/grammars/typescript"
	tsofficial "github.com/cajasmota/grafel/internal/treesitter/ts/official"
	"github.com/cajasmota/grafel/internal/types"
)

// extractTSAt runs the registered typescript extractor over real source at a
// real path — the same entry point the daemon's Pass 1 uses. Nothing here is a
// hand-built record: the subtype under test is whatever the extractor stamps.
func extractTSAt(t *testing.T, path string, content []byte) []types.EntityRecord {
	t.Helper()
	parser, err := tsofficial.New().NewParser(tstsx.LanguageTSX())
	if err != nil {
		t.Fatalf("parser init: %v", err)
	}
	defer parser.Close()
	tree, err := parser.Parse(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ext, ok := extreg.Get("typescript")
	if !ok {
		t.Fatal("typescript extractor not registered")
	}
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

func findComponent6213(ents []types.EntityRecord, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Component" && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

// nestServiceSrc6213 is NestJS-shaped source: the decorator is imported from
// @nestjs/common, the class is a constructor-injected provider, and there is no
// Angular import anywhere.
const nestServiceSrc6213 = `import { Injectable } from '@nestjs/common';
import { InjectRepository } from '@nestjs/typeorm';
import { Repository } from 'typeorm';

import { User } from './user.entity';
import { AuditService } from '../audit/audit.service';

@Injectable()
export class UsersService {
  constructor(
    @InjectRepository(User) private readonly users: Repository<User>,
    private readonly audit: AuditService,
  ) {}

  async findAll(): Promise<User[]> {
    return this.users.find();
  }
}
`

// angularServiceSrc6213 is the control: the same decorator, imported from
// @angular/core. This is the behaviour currently relying on the stamp, so it is
// pinned in both directions.
const angularServiceSrc6213 = `import { Injectable } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class UserService {
  constructor(private http: HttpClient) {}

  load() {
    return this.http.get('/api/users');
  }
}
`

// TestIssue6213_NestInjectableIsStampedService — a NestJS provider carries the
// framework-neutral "service" subtype, the one the fold vocabulary and the
// NestJS custom extractor already use.
func TestIssue6213_NestInjectableIsStampedService(t *testing.T) {
	ents := extractTSAt(t, "src/users/users.service.ts", []byte(nestServiceSrc6213))
	svc := findComponent6213(ents, "UsersService")
	if svc == nil {
		t.Fatalf("no SCOPE.Component for UsersService; extractor emitted %d entities", len(ents))
	}
	if got := svc.Properties["framework"]; got != "nestjs" {
		t.Fatalf("framework = %q, want %q (the #4503 disambiguation is the premise of this fix)", got, "nestjs")
	}
	if svc.Subtype != "service" {
		t.Errorf("subtype = %q, want %q — a NestJS @Injectable is not an Angular service", svc.Subtype, "service")
	}
	// The subtype is also mirrored onto the entity properties; a half-applied
	// fix that changes only the record field leaves the property contradicting it.
	if got := svc.Properties["angular_class_kind"]; got != "service" {
		t.Errorf("angular_class_kind property = %q, want %q", got, "service")
	}
	// Not vacuous: the DI edges the Angular path exists to emit must still be
	// there, so this cannot pass by the class falling through to the generic
	// class handler (which emits subtype="class" and no INJECTED_INTO edges).
	var injected int
	for _, rel := range svc.Relationships {
		if rel.Kind == string(types.RelationshipKindInjectedInto) {
			injected++
		}
	}
	if injected == 0 {
		t.Errorf("no INJECTED_INTO edges on UsersService: the class no longer goes through the DI-aware path")
	}
}

// TestIssue6213_AngularInjectableKeepsAngularService is the control in the other
// direction: a genuine Angular provider is unaffected. angular_service is the
// value the Angular coverage docs record (angular_service x6 on
// angular-realworld) and nothing in this fix renames it.
func TestIssue6213_AngularInjectableKeepsAngularService(t *testing.T) {
	ents := extractTSAt(t, "src/app/user.service.ts", []byte(angularServiceSrc6213))
	svc := findComponent6213(ents, "UserService")
	if svc == nil {
		t.Fatalf("no SCOPE.Component for UserService; extractor emitted %d entities", len(ents))
	}
	if got := svc.Properties["framework"]; got != "angular" {
		t.Fatalf("framework = %q, want %q", got, "angular")
	}
	if svc.Subtype != "angular_service" {
		t.Errorf("subtype = %q, want %q — the Angular stamp must not move", svc.Subtype, "angular_service")
	}
	if got := svc.Properties["angular_class_kind"]; got != "angular_service" {
		t.Errorf("angular_class_kind property = %q, want %q", got, "angular_service")
	}
}

// TestIssue6213_UnmarkedInjectableKeepsAngularDefault pins the third branch of
// frameworkForClass: with no framework markers at all the historical default
// (angular) still applies, so the fix cannot silently re-home decorator-only
// files onto the NestJS subtype.
func TestIssue6213_UnmarkedInjectableKeepsAngularDefault(t *testing.T) {
	const src = `@Injectable()
export class MysteryService {
  constructor(private dep: SomeDep) {}
}
`
	ents := extractTSAt(t, "src/mystery.service.ts", []byte(src))
	svc := findComponent6213(ents, "MysteryService")
	if svc == nil {
		t.Fatalf("no SCOPE.Component for MysteryService; extractor emitted %d entities", len(ents))
	}
	if got := svc.Properties["framework"]; got != "angular" {
		t.Fatalf("framework = %q, want %q", got, "angular")
	}
	if svc.Subtype != "angular_service" {
		t.Errorf("subtype = %q, want %q", svc.Subtype, "angular_service")
	}
}

// TestIssue6213_NestNonInjectableDecoratorsUnchanged pins the decorators #3970
// already made framework-neutral, so a regression that routes the subtype
// through a framework switch cannot disturb them.
func TestIssue6213_NestNonInjectableDecoratorsUnchanged(t *testing.T) {
	const src = `import { Controller, Get } from '@nestjs/common';
import { UsersService } from './users.service';

@Controller('users')
export class UsersController {
  constructor(private readonly users: UsersService) {}

  @Get()
  findAll() { return this.users.findAll(); }
}
`
	ents := extractTSAt(t, "src/users/users.controller.ts", []byte(src))
	ctrl := findComponent6213(ents, "UsersController")
	if ctrl == nil {
		t.Fatalf("no SCOPE.Component for UsersController; extractor emitted %d entities", len(ents))
	}
	if ctrl.Subtype != "controller" {
		t.Errorf("subtype = %q, want %q", ctrl.Subtype, "controller")
	}
	if got := ctrl.Properties["framework"]; got != "nestjs" {
		t.Errorf("framework = %q, want %q", got, "nestjs")
	}
}
