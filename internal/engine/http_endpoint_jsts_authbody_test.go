package engine

import "testing"

// Home-rolled / custom auth-check recognition (#5499). A handler / server-action
// / route-handler whose body OPENS with an auth check — a require*/authorize/
// assertCan/checkPermission/getServerSession call, or an `if (!session)
// throw/redirect` guard — is an AUTHORIZES relation from the endpoint to the
// check. These never reach the route/decorator resolver (no middleware, no
// @UseGuards), so this pass recovers the posture from the body.

// TestAuthBody_NextRouteHandler_RequireUser — a Next.js App Router route handler
// whose body opens with `const user = await requireUser()` → protected,
// method="check", guard=requireUser.
func TestAuthBody_NextRouteHandler_RequireUser(t *testing.T) {
	src := `
export async function POST(request: Request) {
  const user = await requireUser()
  return Response.json({ ok: true })
}
export async function GET(request: Request) {
  return Response.json([])
}
`
	eps := authProps(t, "typescript", "app/api/orders/route.ts", src)
	// POST opens with requireUser() → protected via body check.
	post, ok := eps["POST /api/orders"]
	if !ok {
		t.Fatalf("POST /api/orders not synthesised (got %v)", keysOf(eps))
	}
	if post.Properties["auth_required"] != "true" {
		t.Errorf("POST: auth_required=%q want true (props %v)", post.Properties["auth_required"], post.Properties)
	}
	if post.Properties["auth_method"] != "check" {
		t.Errorf("POST: auth_method=%q want check", post.Properties["auth_method"])
	}
	if post.Properties["auth_guard"] != "requireUser" {
		t.Errorf("POST: auth_guard=%q want requireUser", post.Properties["auth_guard"])
	}
	// GET has no auth check → no edge (negative).
	requirePublic(t, eps, "GET /api/orders")
}

// TestAuthBody_NextRouteHandler_SessionRedirectGuard — a route handler whose
// body opens with `if (!session) redirect('/login')` → protected, guard=!session.
func TestAuthBody_NextRouteHandler_SessionRedirectGuard(t *testing.T) {
	src := `
import { redirect } from 'next/navigation'
export async function GET(request: Request) {
  const session = await getServerSession()
  if (!session) redirect('/login')
  return Response.json({ ok: true })
}
`
	eps := authProps(t, "typescript", "app/api/profile/route.ts", src)
	e, ok := eps["GET /api/profile"]
	if !ok {
		t.Fatalf("GET /api/profile not synthesised (got %v)", keysOf(eps))
	}
	if e.Properties["auth_required"] != "true" || e.Properties["auth_method"] != "check" {
		t.Errorf("GET: want protected method=check, got %v", e.Properties)
	}
	// getServerSession is in the idiom set and appears first → it is the guard.
	if g := e.Properties["auth_guard"]; g != "getServerSession" && g != "!session" {
		t.Errorf("GET: auth_guard=%q want getServerSession or !session", g)
	}
}

// TestAuthBody_PermissionCheck_CapturesGrant — `await checkPermission('orders:delete')`
// captures the literal permission.
func TestAuthBody_PermissionCheck_CapturesGrant(t *testing.T) {
	src := `
export async function DELETE(request: Request) {
  await checkPermission('orders:delete')
  return new Response(null, { status: 204 })
}
`
	eps := authProps(t, "typescript", "app/api/orders/[id]/route.ts", src)
	e, ok := eps["DELETE /api/orders/{id}"]
	if !ok {
		t.Fatalf("DELETE not synthesised (got %v)", keysOf(eps))
	}
	if e.Properties["auth_required"] != "true" {
		t.Errorf("DELETE: want protected, got %v", e.Properties)
	}
	if e.Properties["auth_permissions"] != "orders:delete" {
		t.Errorf("DELETE: auth_permissions=%q want orders:delete", e.Properties["auth_permissions"])
	}
}

// TestAuthBody_NoAuthHandler_NoEdge — a plain handler with no auth idiom in its
// body opener gets NO auth edge (negative; guards against false positives on
// ordinary calls like a `require('./db')` module import or a business call).
func TestAuthBody_NoAuthHandler_NoEdge(t *testing.T) {
	src := `
export async function GET(request: Request) {
  const rows = await db.query('SELECT * FROM items')
  return Response.json(rows)
}
`
	eps := authProps(t, "typescript", "app/api/items/route.ts", src)
	requirePublic(t, eps, "GET /api/items")
}

// TestAuthBody_NestHandlerBodyGate — a NestJS handler with NO @UseGuards but a
// body opening with `await this.authz.assertCan('read', dto)` → protected via
// the body check (the decorator resolver finds nothing here).
func TestAuthBody_NestHandlerBodyGate(t *testing.T) {
	src := `
import { Controller, Get } from '@nestjs/common'

@Controller('reports')
export class ReportsController {
  @Get()
  async findAll() {
    await this.authz.assertCan('reports:read')
    return []
  }
}
`
	eps := authProps(t, "typescript", "reports.controller.ts", src)
	e, ok := eps["GET /reports"]
	if !ok {
		t.Fatalf("GET /reports not synthesised (got %v)", keysOf(eps))
	}
	if e.Properties["auth_required"] != "true" || e.Properties["auth_method"] != "check" {
		t.Errorf("GET /reports: want protected method=check, got %v", e.Properties)
	}
	if e.Properties["auth_permissions"] != "reports:read" {
		t.Errorf("GET /reports: auth_permissions=%q want reports:read", e.Properties["auth_permissions"])
	}
}

// TestAuthBody_RequireRole_CapturesRole — `await requireRole('admin')` captures
// the role into auth_roles.
func TestAuthBody_RequireRole_CapturesRole(t *testing.T) {
	src := `
export async function POST(request: Request) {
  await requireRole('admin')
  return Response.json({})
}
`
	eps := authProps(t, "typescript", "app/api/admin/route.ts", src)
	e := eps["POST /api/admin"]
	if e.Properties["auth_required"] != "true" {
		t.Fatalf("POST /api/admin: want protected, got %v", e.Properties)
	}
	if e.Properties["auth_roles"] != "admin" {
		t.Errorf("POST /api/admin: auth_roles=%q want admin", e.Properties["auth_roles"])
	}
}
