package engine

// Tests for finding #4 (upvate-v2 stress-test): endpoint declarations inside
// test/spec/e2e-spec files must NOT be extracted as production routes.
//
// Root cause: applyHTTPEndpointSynthesis had no guard against test files.
// NestJS e2e spec files (*.e2e-spec.ts) routinely import @nestjs/common,
// stand up a full TestingModule with real @Controller classes, and even call
// app.get('/buildings', ...) through Supertest — all of which match the
// synthesizeNestJS / synthesizeExpress patterns.
//
// Fix: isTestSourceFile() is consulted at the top of
// applyHTTPEndpointSynthesis; any file whose path matches a test/spec
// convention returns early without emitting http_endpoint_definition entities.

import (
	"testing"
)

// ---------------------------------------------------------------------------
// isTestSourceFile unit tests
// ---------------------------------------------------------------------------

func TestIsTestSourceFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
		desc string
	}{
		// JS/TS — spec files
		{"src/app.e2e-spec.ts", true, "NestJS e2e spec"},
		{"test/app.e2e-spec.ts", true, "NestJS e2e spec in test/"},
		{"src/buildings/buildings.e2e-spec.ts", true, "nested NestJS e2e spec"},
		{"src/foo.spec.ts", true, "plain .spec.ts"},
		{"src/foo.test.ts", true, "plain .test.ts"},
		{"src/foo.spec.js", true, "plain .spec.js"},
		{"src/foo.test.js", true, "plain .test.js"},
		{"src/foo.spec.tsx", true, "React spec tsx"},
		{"src/foo.test.tsx", true, "React test tsx"},
		{"src/foo.spec.mjs", true, "ESM spec"},
		{"src/foo.test.cjs", true, "CJS test"},
		// JS/TS — __tests__ directory
		{"src/__tests__/bar.ts", true, "__tests__ directory"},
		{"__tests__/bar.js", true, "__tests__ at root"},
		// JS/TS — e2e directory
		{"e2e/app.ts", true, "e2e directory"},
		{"src/e2e/flows.ts", true, "nested e2e directory"},
		// JS/TS — production files (must NOT be excluded)
		{"src/app.controller.ts", false, "NestJS controller (production)"},
		{"src/users/users.controller.ts", false, "production controller"},
		{"src/main.ts", false, "entry point"},
		{"src/routes/index.ts", false, "routes (but not spec)"},

		// Python
		{"tests/test_buildings.py", true, "Python test in /tests/"},
		{"test_buildings.py", true, "Python test_ prefix"},
		{"buildings_test.py", true, "Python _test suffix"},
		{"conftest.py", false, "conftest is not a test file by name"},
		{"buildings/views.py", false, "Django view (production)"},

		// Go
		{"internal/handler_test.go", true, "Go _test.go"},
		{"internal/handler.go", false, "Go production file"},

		// Ruby
		{"spec/models/user_spec.rb", true, "Ruby spec"},
		{"app/models/user.rb", false, "Ruby production model"},

		// Java
		{"src/test/java/UserTest.java", true, "Java Test suffix"},
		{"src/test/java/UserTests.java", true, "Java Tests suffix"},
		{"src/main/java/UserService.java", false, "Java production service"},

		// test/ directory catch-all
		{"test/utils.ts", true, "test/ directory"},
		{"tests/utils.ts", true, "tests/ directory"},
		{"spec/helpers.ts", true, "spec/ directory"},
	}

	for _, tc := range cases {
		got := isTestSourceFile(tc.path)
		if got != tc.want {
			t.Errorf("isTestSourceFile(%q) = %v, want %v (%s)", tc.path, got, tc.want, tc.desc)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration tests: synthesis pass must not emit endpoints from test files
// ---------------------------------------------------------------------------

// NestJS e2e-spec: the canonical upvate-v2 finding #4 case.
// A *.e2e-spec.ts file contains a full NestJS module setup with @Controller
// and @Get/@Post decorators — synthesis must produce NO http_endpoint entities.
func TestSynthesis_NestJSE2ESpec_NoEndpoints(t *testing.T) {
	// This mirrors the shape of a real NestJS e2e spec file (e.g.
	// src/buildings/buildings.e2e-spec.ts in upvate-v2).
	src := `import { INestApplication } from '@nestjs/common';
import { Test, TestingModule } from '@nestjs/testing';
import * as request from 'supertest';
import { AppModule } from '../src/app.module';

// Inline test controller that imitates the real controller for integration testing.
import { Controller, Get, Post, Body } from '@nestjs/common';

@Controller('buildings')
class TestBuildingsController {
  @Get()
  list() { return []; }

  @Post()
  create(@Body() body: any) { return body; }
}

describe('Buildings (e2e)', () => {
  let app: INestApplication;

  beforeEach(async () => {
    const moduleFixture: TestingModule = await Test.createTestingModule({
      imports: [AppModule],
      controllers: [TestBuildingsController],
    }).compile();
    app = moduleFixture.createNestApplication();
    await app.init();
  });

  it('/buildings (GET)', () => {
    return request(app.getHttpServer()).get('/buildings').expect(200);
  });

  it('/buildings (POST)', () => {
    return request(app.getHttpServer()).post('/buildings').send({}).expect(201);
  });
});
`
	got, _ := runDetect(t, "typescript", "src/buildings/buildings.e2e-spec.ts", src)
	if len(got) != 0 {
		t.Errorf("e2e-spec file should emit 0 http_endpoint entities, got %d: %v", len(got), got)
	}
}

// Counterpart: the same @Controller in a production file DOES produce endpoints.
func TestSynthesis_NestJSController_Production_HasEndpoints(t *testing.T) {
	src := `import { Controller, Get, Post, Body } from '@nestjs/common';

@Controller('buildings')
export class BuildingsController {
  @Get()
  list() { return []; }

  @Post()
  create(@Body() body: any) { return body; }
}
`
	got, _ := runDetect(t, "typescript", "src/buildings/buildings.controller.ts", src)
	want := []string{"http:GET:/buildings", "http:POST:/buildings"}
	requireContains(t, got, want, "NestJS production controller")
}

// Plain .spec.ts file (unit test, not e2e) — must also be excluded.
func TestSynthesis_NestJSSpecTs_NoEndpoints(t *testing.T) {
	src := `import { Test, TestingModule } from '@nestjs/testing';
import { Controller, Get } from '@nestjs/common';

@Controller('health')
class HealthController {
  @Get()
  check() { return { status: 'ok' }; }
}

describe('HealthController', () => {
  it('should be defined', () => {});
});
`
	got, _ := runDetect(t, "typescript", "src/health/health.controller.spec.ts", src)
	if len(got) != 0 {
		t.Errorf("spec.ts file should emit 0 http_endpoint entities, got %d: %v", len(got), got)
	}
}

// Express app.get() inside a test file must also be excluded.
func TestSynthesis_ExpressInTestFile_NoEndpoints(t *testing.T) {
	src := `const express = require('express');
const app = express();

app.get('/health', (req, res) => res.json({ ok: true }));
app.post('/users', (req, res) => res.json({}));

module.exports = app;
`
	// A Supertest fixture file in __tests__/
	got, _ := runDetect(t, "javascript", "__tests__/app.fixture.js", src)
	if len(got) != 0 {
		t.Errorf("__tests__ fixture should emit 0 http_endpoint entities, got %d: %v", len(got), got)
	}
}

// Python test file: FastAPI test client setup with route declarations
// inside conftest/test file must not produce endpoint entities.
func TestSynthesis_FastAPIInPyTestFile_NoEndpoints(t *testing.T) {
	src := `from fastapi import FastAPI
from fastapi.testclient import TestClient

app = FastAPI()

@app.get("/health")
def health():
    return {"status": "ok"}

@app.post("/items")
def create_item(item: dict):
    return item

client = TestClient(app)

def test_health():
    r = client.get("/health")
    assert r.status_code == 200
`
	got, _ := runDetect(t, "python", "tests/test_api.py", src)
	if len(got) != 0 {
		t.Errorf("Python test file should emit 0 http_endpoint entities, got %d: %v", len(got), got)
	}
}

// Go _test.go file with net/http handler setup: must not produce endpoint entities.
func TestSynthesis_GoTestFile_NoEndpoints(t *testing.T) {
	src := `package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
}
`
	got, _ := runDetect(t, "go", "internal/handler_test.go", src)
	if len(got) != 0 {
		t.Errorf("Go _test.go file should emit 0 http_endpoint entities, got %d: %v", len(got), got)
	}
}
