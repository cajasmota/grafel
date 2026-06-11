package javascript

import (
	"context"
	"sort"
	"strings"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
)

// Issue #4399 (extractor side). The browser-e2e route extractor must capture
// every Playwright/Cypress API-call route-by-string and stamp the `VERB route`
// pairs onto a one-per-file test_suite's e2e_route_calls property. Built /
// interpolated URLs are conservatively skipped.

const playwrightSpec4399 = "import { test, expect } from '@playwright/test';\n" +
	"\n" +
	"test.describe('users api', () => {\n" +
	"  test('creates a user', async ({ request }) => {\n" +
	"    const res = await request.post('/api/users', { data: { name: 'a' } });\n" +
	"    expect(res.status()).toBe(201);\n" +
	"  });\n" +
	"  test('lists users', async ({ page }) => {\n" +
	"    await page.request.get('/api/users');\n" +
	"  });\n" +
	"  test('updates via fetch', async ({ request: apiContext }) => {\n" +
	"    await apiContext.fetch('/api/users/1', { method: 'PUT', data: {} });\n" +
	"  });\n" +
	"  test('built url is skipped', async ({ request }) => {\n" +
	"    const base = process.env.BASE;\n" +
	"    await request.get(`${base}/api/users`);\n" +
	"  });\n" +
	"});\n"

const cypressSpec4399 = "describe('users api', () => {\n" +
	"  it('lists', () => {\n" +
	"    cy.request('GET', '/api/users').its('status').should('eq', 200);\n" +
	"  });\n" +
	"  it('creates', () => {\n" +
	"    cy.request({ method: 'POST', url: '/api/users', body: { name: 'a' } });\n" +
	"  });\n" +
	"  it('health single-arg', () => {\n" +
	"    cy.request('/api/health');\n" +
	"  });\n" +
	"  it('intercepts', () => {\n" +
	"    cy.intercept('DELETE', '/api/users/9').as('del');\n" +
	"  });\n" +
	"  it('built url skipped', () => {\n" +
	"    const u = apiUrl('users');\n" +
	"    cy.request(u);\n" +
	"  });\n" +
	"});\n"

func extractE2ESuiteCalls(t *testing.T, path, lang, src string) []string {
	t.Helper()
	ex := &jsTestRouteE2EExtractor{}
	ents, err := ex.Extract(context.Background(), extreg.FileInput{
		Path: path, Language: lang, Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	for _, e := range ents {
		if e.Subtype == "test_suite" {
			raw := e.Properties["e2e_route_calls"]
			if raw == "" {
				return nil
			}
			out := strings.Split(raw, "\n")
			sort.Strings(out)
			return out
		}
	}
	return nil
}

func TestPlaywrightE2ERouteCapture4399(t *testing.T) {
	got := extractE2ESuiteCalls(t, "e2e/users.spec.ts", "typescript", playwrightSpec4399)
	want := []string{"GET /api/users", "POST /api/users", "PUT /api/users/1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("playwright route calls = %v, want %v", got, want)
	}
}

func TestCypressE2ERouteCapture4399(t *testing.T) {
	got := extractE2ESuiteCalls(t, "cypress/e2e/users.cy.ts", "typescript", cypressSpec4399)
	want := []string{
		"DELETE /api/users/9",
		"GET /api/health",
		"GET /api/users",
		"POST /api/users",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("cypress route calls = %v, want %v", got, want)
	}
}

// A pure unit-test spec that hits no route emits no suite.
func TestBrowserE2E_NoRouteNoSuite4399(t *testing.T) {
	src := "import { test, expect } from '@playwright/test';\n" +
		"test('adds', () => { expect(1 + 1).toBe(2); });\n"
	got := extractE2ESuiteCalls(t, "e2e/math.spec.ts", "typescript", src)
	if got != nil {
		t.Fatalf("expected no suite for route-less spec, got %v", got)
	}
}

// Non-spec production code that calls an HTTP API is not a test file → no suite.
func TestBrowserE2E_NonSpecSkipped4399(t *testing.T) {
	got := extractE2ESuiteCalls(t, "src/api/client.ts", "typescript", cypressSpec4399)
	if got != nil {
		t.Fatalf("expected no suite for non-spec file, got %v", got)
	}
}
