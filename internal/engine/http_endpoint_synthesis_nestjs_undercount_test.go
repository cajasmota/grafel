package engine

// Regression tests for the NestJS HTTP-endpoint UNDERCOUNT + mis-attribution
// bug reported by the upvate-v2 Django→NestJS migration (the parity oracle).
//
// Report: core-backend-v2/.migration/plans/archigraph-endpoint-undercount-report.md
//
// Three defects in synthesizeNestJS / applyHTTPEndpointSynthesis:
//
//   1. A route whose PRECEDING decorator args contained a ')' inside a string
//      (e.g. an @ApiOperation description "...(parity with legacy)") was
//      silently dropped, because the old combined regex skipped intervening
//      decorators with `[^)]*` which stops at the first ')'. Fix: line-oriented
//      forward scan to bind the verb decorator to its handler.
//
//   2. A thin/aliasing controller (UsersLoginController) whose handler delegates
//      to another controller was invisible — the verb decorator's handler scan
//      was binding to the `constructor` line that sits between the class opening
//      and the first route, so the real handler was never found. Fix: the
//      forward scan skips `constructor`.
//
//   3. GET /health was mis-attributed to a build script (scripts/docs-check.mjs)
//      containing a "/health" string. Fix: isNonAppSourceFile excludes
//      build/tooling scripts from emitting endpoint DEFINITIONS.

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// sourceFileForID returns the SourceFile of the first endpoint-definition
// entity with the given synthetic ID, or "" if absent.
func nestjsSourceFileForID(res *DetectResult, id string) string {
	for _, e := range res.Entities {
		if e.ID == id &&
			(e.Kind == httpEndpointKind || e.Kind == httpEndpointDefinitionKind) {
			return e.SourceFile
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Defect 1 — the parens-in-string decorator no longer drops the sibling route.
// ---------------------------------------------------------------------------

// The minimal reproducer: a single @Post('reset_password') route whose
// preceding @ApiOperation description contains "(parity with legacy)". Before
// the fix this route was silently dropped.
func TestNestJS_Defect1_ParensInDecoratorString_RouteSurvives(t *testing.T) {
	src := `import { Controller, Post, HttpCode, HttpStatus, Body } from '@nestjs/common';
import { ApiOperation, ApiResponse } from '@nestjs/swagger';
import { Public } from '../shared/auth';

@Controller('api/v1/auth')
export class AuthController {
  @Public()
  @Post('reset_password')
  @HttpCode(HttpStatus.OK)
  @ApiOperation({
    summary: 'Request password reset',
    description:
      'Look up user by email, generate reset token + uid. No email sent (parity with legacy).',
  })
  @ApiResponse({ status: 200, description: 'Reset token generated' })
  @ApiResponse({ status: 400, description: 'Email required' })
  resetPassword(@Body() resetDto: any): Promise<any> {
    return this.resetPasswordService.resetPassword(resetDto.email);
  }
}
`
	got, _ := runDetect(t, "typescript", "src/modules/auth/api/auth.controller.ts", src)
	requireContains(t, got, []string{"http:POST:/api/v1/auth/reset_password"}, "defect1 parens-in-string")
}

// ---------------------------------------------------------------------------
// Defect 1 (full controller) — the real 6-route AuthController emits all 6.
// This is the EXACT decorator-stack shape from
// core-backend-v2/src/modules/auth/api/auth.controller.ts. Proves the
// undercount (6→? in the report; here 6/6) is gone.
// ---------------------------------------------------------------------------

func TestNestJS_FullAuthController_AllSixRoutes(t *testing.T) {
	src := `import {
  Controller, Post, Get, Body, Query, HttpCode, HttpStatus, Res, Inject,
} from '@nestjs/common';
import { ApiTags, ApiOperation, ApiResponse } from '@nestjs/swagger';
import type { Response } from 'express';
import { Public } from '../../../shared/auth';

@ApiTags('auth')
@Controller('api/v1/auth')
export class AuthController {
  constructor(
    private readonly authService: AuthService,
    @Inject(COGNITO_STATUS_SERVICE)
    private readonly cognitoStatusService: CognitoStatusService,
    private readonly jwtRefreshService: JwtRefreshService,
    @Inject(RESET_PASSWORD_SERVICE)
    private readonly resetPasswordService: ResetPasswordService,
    private readonly updatePasswordService: UpdatePasswordService,
    private readonly registerService: RegisterService,
  ) {}

  @Public()
  @Post('login')
  @HttpCode(HttpStatus.OK)
  @ApiOperation({
    summary: 'User login via Cognito',
    description: 'Authenticate user with Cognito and return tokens + user data',
  })
  @ApiResponse({ status: 200, description: 'Login successful', type: LoginResponseDto })
  @ApiResponse({ status: 400, description: 'Invalid credentials' })
  @ApiResponse({ status: 500, description: 'Cognito service error' })
  login(@Body() loginDto: LoginRequestDto): Promise<LoginResponseDto> {
    return this.authService.login(loginDto);
  }

  @Public()
  @Post('register')
  @HttpCode(HttpStatus.CREATED)
  @ApiOperation({
    summary: 'User registration',
    description: 'Register new user via Cognito and create local DB record',
  })
  @ApiResponse({ status: 201, description: 'Registration successful', type: RegisterResponseDto })
  @ApiResponse({ status: 400, description: 'Validation error' })
  register(@Body() registerDto: RegisterRequestDto): Promise<RegisterResponseDto> {
    return this.registerService.register(registerDto);
  }

  @Public()
  @Post('reset_password')
  @HttpCode(HttpStatus.OK)
  @ApiOperation({
    summary: 'Request password reset',
    description:
      'Look up user by email, generate reset token + uid. No email sent (parity with legacy).',
  })
  @ApiResponse({ status: 200, description: 'Reset token generated', type: ResetPasswordResponseDto })
  @ApiResponse({ status: 404, description: 'User not found' })
  resetPassword(@Body() resetDto: ResetPasswordRequestDto): Promise<ResetPasswordResponseDto> {
    return this.resetPasswordService.resetPassword(resetDto.email);
  }

  @Public()
  @Post('update_password')
  @ApiOperation({
    summary: 'Update password with reset token',
    description: 'Validate reset token and update user password',
  })
  @ApiResponse({ status: 200, description: 'Password updated' })
  @ApiResponse({ status: 404, description: 'User not found' })
  updatePassword(@Body() updateDto: UpdatePasswordRequestDto): Promise<any> {
    return this.updatePasswordService.updatePassword(updateDto.token_id, updateDto.password);
  }

  @Public()
  @Post('refresh')
  @ApiOperation({
    summary: 'Refresh access token',
    description: 'Get new access token from refresh token',
  })
  @ApiResponse({ status: 401, description: 'Invalid or expired refresh token' })
  refreshToken(@Body() refreshDto: RefreshRequestDto): RefreshResponseDto {
    return this.jwtRefreshService.refresh(refreshDto.refresh);
  }

  @Public()
  @Get('handle_cognito_status')
  @ApiOperation({
    summary: 'Check Cognito user status',
    description: 'Retrieve user status from Cognito',
  })
  @ApiResponse({ status: 200, description: 'User status retrieved' })
  async handleCognitoStatus(
    @Query('username') username: string,
    @Res() res: Response,
  ): Promise<void> {
    const result = await this.cognitoStatusService.handleCognitoStatus(username);
    res.status(result.statusCode).json(result.body);
  }
}
`
	got, _ := runDetect(t, "typescript", "src/modules/auth/api/auth.controller.ts", src)
	requireContains(t, got, []string{
		"http:POST:/api/v1/auth/login",
		"http:POST:/api/v1/auth/register",
		"http:POST:/api/v1/auth/reset_password",
		"http:POST:/api/v1/auth/update_password",
		"http:POST:/api/v1/auth/refresh",
		"http:GET:/api/v1/auth/handle_cognito_status",
	}, "full AuthController 6/6")
}

// ---------------------------------------------------------------------------
// Defect 2 — the thin/aliasing UsersLoginController emits its route. The
// constructor sitting before the first route must not be mis-bound as the
// handler. Fixture is the EXACT content of
// core-backend-v2/src/modules/auth/api/users-login.controller.ts.
// ---------------------------------------------------------------------------

func TestNestJS_Defect2_ThinAliasingController_EmitsRoute(t *testing.T) {
	src := `import { Controller, Post, Body, HttpCode, HttpStatus } from '@nestjs/common';
import { ApiTags, ApiOperation, ApiResponse } from '@nestjs/swagger';
import { Public } from '../../../shared/auth';
import { LoginRequestDto, LoginResponseDto } from './dto';
import { AuthController } from './auth.controller';

@ApiTags('auth')
@Controller('api/v1/users')
export class UsersLoginController {
  constructor(private readonly authController: AuthController) {}

  @Public()
  @Post('login')
  @HttpCode(HttpStatus.OK)
  @ApiOperation({
    summary: 'User login via Cognito (legacy alias route)',
    description:
      'Alias for /api/v1/auth/login — Django registered LoginViewSet at both paths.',
  })
  @ApiResponse({ status: 200, description: 'Login successful', type: LoginResponseDto })
  @ApiResponse({ status: 400, description: 'Invalid credentials' })
  login(@Body() loginDto: LoginRequestDto): Promise<LoginResponseDto> {
    return this.authController.login(loginDto);
  }
}
`
	got, res := runDetect(t, "typescript", "src/modules/auth/api/users-login.controller.ts", src)
	requireContains(t, got, []string{"http:POST:/api/v1/users/login"}, "defect2 aliasing controller")

	// The endpoint must be sourced from the real controller file.
	src2 := nestjsSourceFileForID(res, "http:POST:/api/v1/users/login")
	if src2 != "src/modules/auth/api/users-login.controller.ts" {
		t.Errorf("defect2: /api/v1/users/login should be sourced from the controller, got %q", src2)
	}
}

// ---------------------------------------------------------------------------
// Defect 3a — the bare @Get() HealthController emits GET /health, sourced from
// the controller file. Fixture is the EXACT content of
// core-backend-v2/src/modules/health/health.controller.ts.
// ---------------------------------------------------------------------------

func TestNestJS_Defect3a_BareGetHealthController_EmitsFromController(t *testing.T) {
	src := `import { Controller, Get } from '@nestjs/common';

import { Public } from '../../shared/auth';
import { HealthService } from './health.service';
import type { HealthStatus } from './health.service';

@Public()
@Controller('health')
export class HealthController {
  constructor(private readonly healthService: HealthService) {}

  @Get()
  check(): HealthStatus {
    return this.healthService.check();
  }
}
`
	path := "src/modules/health/health.controller.ts"
	got, res := runDetect(t, "typescript", path, src)
	requireContains(t, got, []string{"http:GET:/health"}, "defect3a HealthController")

	srcFile := nestjsSourceFileForID(res, "http:GET:/health")
	if srcFile != path {
		t.Errorf("defect3a: GET /health should be sourced from %q, got %q", path, srcFile)
	}
}

// ---------------------------------------------------------------------------
// Defect 3b — a scripts/docs-check.mjs-style build script containing a
// "/health" string must NOT emit an http_endpoint_definition.
// ---------------------------------------------------------------------------

func TestNestJS_Defect3b_BuildScript_NoEndpointDefinition(t *testing.T) {
	// A docs-build gate script. It references "/health" as data, never as a
	// declared route. It must not synthesize a phantom endpoint that would
	// collide with the real HealthController route.
	src := `#!/usr/bin/env node
// docs-check.mjs — CI docs-build gate. Pure structure checks, no routes.
import { readFileSync, existsSync } from 'node:fs';

const REQUIRED_PATHS = ['/health', '/api/v1/auth/login'];
const doc = JSON.parse(readFileSync('openapi.json', 'utf8'));
for (const p of REQUIRED_PATHS) {
  if (!doc.paths[p]) {
    process.stdout.write('FAIL missing ' + p + '\n');
    process.exit(1);
  }
}
`
	got, res := runDetect(t, "javascript", "scripts/docs-check.mjs", src)
	for _, e := range res.Entities {
		if e.Kind == httpEndpointDefinitionKind || e.Kind == httpEndpointKind {
			t.Errorf("build script must emit NO endpoint definitions, got %s (%s)", e.ID, e.Kind)
		}
	}
	// Specifically: no phantom /health definition.
	requireNotContains(t, got, []string{"http:GET:/health"}, "defect3b build script")
}

// isNonAppSourceFile unit table — conservative exclusion, real app code kept.
func TestIsNonAppSourceFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
		desc string
	}{
		{"scripts/docs-check.mjs", true, "scripts/ dir"},
		{"core-backend-v2/scripts/docs-check.mjs", true, "nested scripts/ dir"},
		{"tools/codegen.js", true, "tools/ dir"},
		{"bin/release.ts", true, "bin/ dir"},
		{"build.mjs", true, "top-level build script .mjs"},
		{"gen-openapi.cjs", true, "top-level gen .cjs"},
		// App code must NOT be excluded.
		{"src/modules/health/health.controller.ts", false, "real controller"},
		{"src/app.module.ts", false, "app module"},
		{"app/api/health/route.mjs", false, "Next.js .mjs app route"},
		{"src/server/index.mjs", false, ".mjs under src/server"},
		{"src/lib/helper.mjs", false, ".mjs under src/lib"},
		{"main.ts", false, "plain top-level ts"},
	}
	for _, tc := range cases {
		if got := isNonAppSourceFile(tc.path); got != tc.want {
			t.Errorf("isNonAppSourceFile(%q) = %v, want %v (%s)", tc.path, got, tc.want, tc.desc)
		}
	}
}

var _ = types.EntityRecord{}
