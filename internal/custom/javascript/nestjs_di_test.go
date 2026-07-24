package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"

	_ "github.com/cajasmota/grafel/internal/custom/javascript"
)

// extractNest runs the NestJS extractor and returns the full entity records
// (with Relationships) so the DI-edge tests can assert specific edges.
func extractNest(t *testing.T, src string) []types.EntityRecord {
	t.Helper()
	return extractFull(t, "custom_js_nestjs", fi("file.ts", "typescript", src))
}

// hasEdge returns true when some entity named `ownerName` carries a relationship
// of the given Kind with the given FromID and ToID. ownerName may be "" to skip
// the owner check.
func hasEdge(ents []types.EntityRecord, ownerName, kind, fromID, toID string) bool {
	for _, e := range ents {
		if ownerName != "" && e.Name != ownerName {
			continue
		}
		for _, r := range e.Relationships {
			if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
				return true
			}
		}
	}
	return false
}

// edgeProp returns the value of property `key` on the first matching edge, or "".
func edgeProp(ents []types.EntityRecord, kind, fromID, toID, key string) string {
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == kind && r.FromID == fromID && r.ToID == toID {
				return r.Properties.Get(key)
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Constructor injection → INJECTED_INTO
// ---------------------------------------------------------------------------

func TestNestDIConstructorInjection(t *testing.T) {
	src := `
import { Controller, Get } from '@nestjs/common';

@Controller('users')
export class UsersController {
  constructor(
    private readonly usersService: UsersService,
    private readonly logger: LoggerService,
  ) {}

  @Get()
  list() { return this.usersService.findAll(); }
}
`
	ents := extractNest(t, src)
	// UsersService INJECTED_INTO UsersController
	if !hasEdge(ents, "UsersController", "INJECTED_INTO", "UsersService", "UsersController") {
		t.Error("expected UsersService INJECTED_INTO UsersController")
	}
	// LoggerService INJECTED_INTO UsersController
	if !hasEdge(ents, "UsersController", "INJECTED_INTO", "LoggerService", "UsersController") {
		t.Error("expected LoggerService INJECTED_INTO UsersController")
	}
	if v := edgeProp(ents, "INJECTED_INTO", "UsersService", "UsersController", "via"); v != "nestjs_constructor" {
		t.Errorf("expected via=nestjs_constructor, got %q", v)
	}
}

func TestNestDIInjectCustomToken(t *testing.T) {
	src := `
import { Injectable, Inject } from '@nestjs/common';

@Injectable()
export class ConfigConsumer {
  constructor(
    @Inject('CONFIG_TOKEN') private readonly cfg: ConfigShape,
    @Inject(DATA_SOURCE) private readonly ds: DataSource,
  ) {}
}
`
	ents := extractNest(t, src)
	// String-literal token normalised to its bare value.
	if !hasEdge(ents, "ConfigConsumer", "INJECTED_INTO", "CONFIG_TOKEN", "ConfigConsumer") {
		t.Error("expected token CONFIG_TOKEN INJECTED_INTO ConfigConsumer")
	}
	// Identifier token preserved.
	if !hasEdge(ents, "ConfigConsumer", "INJECTED_INTO", "DATA_SOURCE", "ConfigConsumer") {
		t.Error("expected token DATA_SOURCE INJECTED_INTO ConfigConsumer")
	}
	if v := edgeProp(ents, "INJECTED_INTO", "CONFIG_TOKEN", "ConfigConsumer", "di_role"); v != "token" {
		t.Errorf("expected di_role=token, got %q", v)
	}
}

// Negative: a constructor in class A must NOT inject into unrelated class B.
func TestNestDINoCrossClassInjection(t *testing.T) {
	src := `
@Injectable()
export class ServiceA {
  constructor(private readonly repo: RepoA) {}
}

@Injectable()
export class ServiceB {
  constructor(private readonly other: RepoB) {}
}
`
	ents := extractNest(t, src)
	if !hasEdge(ents, "ServiceA", "INJECTED_INTO", "RepoA", "ServiceA") {
		t.Error("expected RepoA INJECTED_INTO ServiceA")
	}
	if !hasEdge(ents, "ServiceB", "INJECTED_INTO", "RepoB", "ServiceB") {
		t.Error("expected RepoB INJECTED_INTO ServiceB")
	}
	// RepoA must not be injected into ServiceB (no cross-class leakage).
	if hasEdge(ents, "", "INJECTED_INTO", "RepoA", "ServiceB") {
		t.Error("unexpected cross-class injection RepoA INTO ServiceB")
	}
	if hasEdge(ents, "", "INJECTED_INTO", "RepoB", "ServiceA") {
		t.Error("unexpected cross-class injection RepoB INTO ServiceA")
	}
}

// Primitive-typed constructor params must not produce an injection edge.
func TestNestDIRejectsPrimitiveParams(t *testing.T) {
	src := `
@Injectable()
export class TokenService {
  constructor(private readonly secret: string, private readonly ttl: number) {}
}
`
	ents := extractNest(t, src)
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "INJECTED_INTO" {
				t.Errorf("unexpected INJECTED_INTO edge for primitive params: %+v", r)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// @Module wiring → BINDS
// ---------------------------------------------------------------------------

func TestNestDIModuleBinds(t *testing.T) {
	src := `
import { Module } from '@nestjs/common';

@Module({
  imports: [SharedModule, AuthModule],
  controllers: [UsersController],
  providers: [UsersService, UsersRepository],
  exports: [UsersService],
})
export class UsersModule {}
`
	ents := extractNest(t, src)
	// @Module providers:[UsersService] → UsersModule BINDS UsersService
	if !hasEdge(ents, "UsersModule", "BINDS", "UsersModule", "UsersService") {
		t.Error("expected UsersModule BINDS UsersService (provider)")
	}
	if v := edgeProp(ents, "BINDS", "UsersModule", "UsersService", "binding_kind"); v != "provider" {
		t.Errorf("expected binding_kind=provider, got %q", v)
	}
	// controllers
	if !hasEdge(ents, "UsersModule", "BINDS", "UsersModule", "UsersController") {
		t.Error("expected UsersModule BINDS UsersController (controller)")
	}
	if v := edgeProp(ents, "BINDS", "UsersModule", "UsersController", "binding_kind"); v != "controller" {
		t.Errorf("expected binding_kind=controller, got %q", v)
	}
	// imports
	if !hasEdge(ents, "UsersModule", "BINDS", "UsersModule", "SharedModule") {
		t.Error("expected UsersModule BINDS SharedModule (import)")
	}
	if v := edgeProp(ents, "BINDS", "UsersModule", "SharedModule", "binding_kind"); v != "import" {
		t.Errorf("expected binding_kind=import, got %q", v)
	}
	// exports: UsersService is both a provider and exported → a single BINDS
	// edge tagged binding_kind=provider with exported=true (no duplicate edge).
	if v := edgeProp(ents, "BINDS", "UsersModule", "UsersService", "exported"); v != "true" {
		t.Errorf("expected exported=true on UsersService binding, got %q", v)
	}
	usersServiceBinds := 0
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind == "BINDS" && r.FromID == "UsersModule" && r.ToID == "UsersService" {
				usersServiceBinds++
			}
		}
	}
	if usersServiceBinds != 1 {
		t.Errorf("expected exactly 1 BINDS edge to UsersService, got %d", usersServiceBinds)
	}
}

func TestNestDIModuleProviderTokenBinds(t *testing.T) {
	src := `
@Module({
  providers: [
    { provide: 'CONFIG', useClass: ConfigService },
    { provide: CACHE_TOKEN, useExisting: RedisCache },
  ],
})
export class CoreModule {}
`
	ents := extractNest(t, src)
	// {provide:'CONFIG', useClass: ConfigService} → token CONFIG BINDS ConfigService
	if !hasEdge(ents, "CoreModule", "BINDS", "CONFIG", "ConfigService") {
		t.Error("expected token CONFIG BINDS ConfigService")
	}
	if v := edgeProp(ents, "BINDS", "CONFIG", "ConfigService", "binding_kind"); v != "useClass" {
		t.Errorf("expected binding_kind=useClass, got %q", v)
	}
	// useExisting alias
	if !hasEdge(ents, "CoreModule", "BINDS", "CACHE_TOKEN", "RedisCache") {
		t.Error("expected token CACHE_TOKEN BINDS RedisCache")
	}
}

// ---------------------------------------------------------------------------
// @UseGuards / @UseInterceptors / @UsePipes → USES
// ---------------------------------------------------------------------------

func TestNestDIClassLevelGuard(t *testing.T) {
	src := `
import { Controller, UseGuards } from '@nestjs/common';

@UseGuards(JwtAuthGuard)
@Controller('admin')
export class AdminController {}
`
	ents := extractNest(t, src)
	// @UseGuards(JwtAuthGuard) on controller → AdminController USES JwtAuthGuard
	if !hasEdge(ents, "AdminController", "USES", "AdminController", "JwtAuthGuard") {
		t.Error("expected AdminController USES JwtAuthGuard")
	}
	if v := edgeProp(ents, "USES", "AdminController", "JwtAuthGuard", "di_role"); v != "guard" {
		t.Errorf("expected di_role=guard, got %q", v)
	}
}

func TestNestDIHandlerLevelGuard(t *testing.T) {
	src := `
import { Controller, Get, UseGuards, UseInterceptors } from '@nestjs/common';

@Controller('users')
export class UsersController {
  @UseGuards(RolesGuard)
  @UseInterceptors(LoggingInterceptor)
  @Get('secret')
  getSecret() { return 42; }
}
`
	ents := extractNest(t, src)
	// @UseGuards(RolesGuard) on a route → endpoint "GET getSecret" USES RolesGuard
	if !hasEdge(ents, "GET getSecret", "USES", "GET getSecret", "RolesGuard") {
		t.Error("expected endpoint GET getSecret USES RolesGuard")
	}
	if v := edgeProp(ents, "USES", "GET getSecret", "RolesGuard", "di_scope"); v != "handler" {
		t.Errorf("expected di_scope=handler, got %q", v)
	}
	// Interceptor on the same handler.
	if !hasEdge(ents, "GET getSecret", "USES", "GET getSecret", "LoggingInterceptor") {
		t.Error("expected endpoint GET getSecret USES LoggingInterceptor")
	}
	if v := edgeProp(ents, "USES", "GET getSecret", "LoggingInterceptor", "di_role"); v != "interceptor" {
		t.Errorf("expected di_role=interceptor, got %q", v)
	}
}

func TestNestDIMultipleGuards(t *testing.T) {
	src := `
@UseGuards(AuthGuard, RolesGuard)
@Controller('x')
export class XController {}
`
	ents := extractNest(t, src)
	if !hasEdge(ents, "XController", "USES", "XController", "AuthGuard") {
		t.Error("expected XController USES AuthGuard")
	}
	if !hasEdge(ents, "XController", "USES", "XController", "RolesGuard") {
		t.Error("expected XController USES RolesGuard")
	}
}

// ---------------------------------------------------------------------------
// @Injectable scope
// ---------------------------------------------------------------------------

func TestNestDIInjectableScope(t *testing.T) {
	src := `
import { Injectable, Scope } from '@nestjs/common';

@Injectable({ scope: Scope.REQUEST })
export class RequestScopedService {}
`
	ents := extractNest(t, src)
	found := false
	for _, e := range ents {
		if e.Name == "RequestScopedService" {
			found = true
			if e.Properties["di_scope"] != "REQUEST" {
				t.Errorf("expected di_scope=REQUEST, got %q", e.Properties["di_scope"])
			}
			if e.Properties["di_provider"] != "true" {
				t.Errorf("expected di_provider=true, got %q", e.Properties["di_provider"])
			}
		}
	}
	if !found {
		t.Error("RequestScopedService entity not emitted")
	}
}

// No DI edges leak out of a plain non-NestJS file.
func TestNestDINoMatch(t *testing.T) {
	ents := extractNest(t, "const x = 1;")
	if len(ents) != 0 {
		t.Errorf("expected no entities, got %d", len(ents))
	}
}
