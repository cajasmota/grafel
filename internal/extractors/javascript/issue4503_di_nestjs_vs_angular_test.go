package javascript_test

import (
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #4503 LIVE-REPRO — NestJS @Injectable providers mis-classified as
// "angular" in the DI view.
//
// Angular (@angular/core) and NestJS (@nestjs/common) BOTH spell a DI provider
// `@Injectable()` with a constructor-injected providers list. The JS/TS
// extractor mapped the bare `Injectable` decorator to framework=angular
// unconditionally, so an entire NestJS codebase (core-backend-v3, 100% NestJS,
// zero Angular) surfaced on /di as "angular (121)". The DI view groups
// INJECTED_INTO edges by their `framework` property, and that property was
// stamped angular for every NestJS provider.
//
// ROOT FIX (import-origin disambiguation, NOT a hack): the framework label for
// an @Injectable class is resolved from where the decorator was imported from —
// `@nestjs/*` → nestjs, `@angular/*` → angular — falling back to the file's
// framework-import markers, and only then to the historical angular default.
// Genuine Angular providers (import @angular/core, providedIn:'root', NgModule)
// must still classify as angular.
//
// The test runs the REAL JSExtractor.Extract pipeline over BYTE-COPIES of real
// core-backend-v3 NestJS providers and a faithful Angular provider, then asserts
// the framework tag on both the provider entity and its INJECTED_INTO edges.

// injectedIntoFramework returns the framework property of the first
// INJECTED_INTO edge whose consumer (ToID) is the given class, or "".
func injectedIntoFramework(ents []types.EntityRecord, consumer string) string {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == string(types.RelationshipKindInjectedInto) && r.ToID == consumer {
				return r.Properties["framework"]
			}
		}
	}
	return ""
}

// entityFramework returns the `framework` property of the first entity whose
// Name matches and that carries an angular_decorator marker (the decorated
// provider class), or "".
func entityFramework(ents []types.EntityRecord, name string) string {
	for i := range ents {
		if ents[i].Name == name && ents[i].Properties["angular_decorator"] != "" {
			return ents[i].Properties["framework"]
		}
	}
	return ""
}

// TestIssue4503_NestJSInjectableClassifiesAsNestJS feeds byte-copies of real
// core-backend-v3 NestJS @Injectable providers through the real extractor and
// asserts framework=nestjs (NOT angular) on the entity and INJECTED_INTO edges.
func TestIssue4503_NestJSInjectableClassifiesAsNestJS(t *testing.T) {
	// Byte-copy of core-backend-v3 src/common/integrations/mailgun/services/
	// email.service.ts (trimmed body) — a NestJS @Injectable from @nestjs/common
	// with constructor injection. This is the exact shape the /di "angular" bug
	// mis-tagged.
	src := []byte(`import { Inject, Injectable, Logger } from '@nestjs/common';
import { ConfigService } from '@nestjs/config';

@Injectable()
export class EmailService {
  private readonly logger = new Logger(EmailService.name);
  constructor(
    @Inject('MAILGUN_TRANSPORT') private readonly transport: MailgunTransport,
    config: ConfigService,
  ) {}
  async send(): Promise<void> {}
}`)

	ents := extractAngular(t, src)

	if got := entityFramework(ents, "EmailService"); got != "nestjs" {
		t.Errorf("EmailService entity framework = %q, want nestjs (NestJS @Injectable mis-tagged as angular)", got)
	}
	if got := injectedIntoFramework(ents, "EmailService"); got != "nestjs" {
		t.Errorf("EmailService INJECTED_INTO framework = %q, want nestjs", got)
	}
	// No edge nor entity may be tagged angular for a pure-NestJS file.
	for i := range ents {
		if ents[i].Properties["angular_decorator"] != "" && ents[i].Properties["framework"] == "angular" {
			t.Errorf("entity %q tagged framework=angular in a pure-NestJS file", ents[i].Name)
		}
	}
}

// TestIssue4503_AppServiceNestJS — the simplest real NestJS provider
// (src/app.service.ts, no constructor injection) still classifies as nestjs.
func TestIssue4503_AppServiceNestJS(t *testing.T) {
	src := []byte(`import { Injectable } from '@nestjs/common';

@Injectable()
export class AppService {
  getHello(): string {
    return 'Hello World!';
  }
}`)
	ents := extractAngular(t, src)
	if got := entityFramework(ents, "AppService"); got != "nestjs" {
		t.Errorf("AppService framework = %q, want nestjs", got)
	}
}

// TestIssue4503_GenuineAngularStillAngular guards the no-regression direction:
// a genuine Angular provider (import @angular/core, providedIn:'root', an
// @NgModule sibling) must still classify as angular.
func TestIssue4503_GenuineAngularStillAngular(t *testing.T) {
	src := []byte(`import { Injectable, NgModule } from '@angular/core';
import { HttpClient } from '@angular/common/http';

@Injectable({ providedIn: 'root' })
export class UserService {
  constructor(private http: HttpClient) {}
  load() {}
}

@NgModule({ providers: [UserService] })
export class AppModule {}`)

	ents := extractAngular(t, src)

	if got := entityFramework(ents, "UserService"); got != "angular" {
		t.Errorf("genuine Angular UserService framework = %q, want angular (regressed real Angular detection)", got)
	}
	if got := injectedIntoFramework(ents, "UserService"); got != "angular" {
		t.Errorf("genuine Angular UserService INJECTED_INTO framework = %q, want angular", got)
	}
	for i := range ents {
		if ents[i].Properties["angular_decorator"] != "" && ents[i].Properties["framework"] == "nestjs" {
			t.Errorf("entity %q tagged framework=nestjs in a pure-Angular file", ents[i].Name)
		}
	}
}

// TestIssue4503_NestControllerUnaffected confirms the unambiguous NestJS
// decorators keep nestjs regardless of import disambiguation.
func TestIssue4503_NestControllerUnaffected(t *testing.T) {
	src := []byte(`import { Controller, Get } from '@nestjs/common';

@Controller('hello')
export class HelloController {
  constructor(private readonly svc: AppService) {}
  @Get()
  hello() {}
}`)
	ents := extractAngular(t, src)
	if got := entityFramework(ents, "HelloController"); got != "nestjs" {
		t.Errorf("HelloController framework = %q, want nestjs", got)
	}
}

// TestIssue4503_BarrelReexportedInjectableFallsBackToMarkers — when @Injectable
// is re-exported through a project barrel (so its import origin is NOT
// @nestjs/*), the file-level @nestjs marker still classifies it as nestjs.
func TestIssue4503_BarrelReexportedInjectableFallsBackToMarkers(t *testing.T) {
	src := []byte(`import { Injectable } from './common';
import { ConfigService } from '@nestjs/config';

@Injectable()
export class ReportService {
  constructor(private config: ConfigService) {}
}`)
	ents := extractAngular(t, src)
	if got := entityFramework(ents, "ReportService"); got != "nestjs" {
		t.Errorf("barrel-reexported @Injectable framework = %q, want nestjs (file imports @nestjs/config)", got)
	}
	if got := injectedIntoFramework(ents, "ReportService"); got != "nestjs" && got != "" {
		// INJECTED_INTO may be absent if ConfigService isn't a resolvable
		// provider; when present it must be nestjs.
		if !strings.EqualFold(got, "nestjs") {
			t.Errorf("ReportService INJECTED_INTO framework = %q, want nestjs", got)
		}
	}
}
