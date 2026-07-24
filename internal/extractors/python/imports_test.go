// imports_test.go — coverage for the IMPORTS ToID resolveImportToIDs
// pass (analog of #642 for Python).

package python

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractor"
	"github.com/cajasmota/grafel/internal/types"
)

// findImportEdge returns the IMPORTS edge whose source_module matches
// the supplied dotted module path, or nil when no such edge exists.
//
// Issue #693: IMPORTS edges are now attached to the file entity
// (SCOPE.Component/file) rather than standalone SCOPE.Component/module
// placeholder entities. The helper searches all entities so tests are
// independent of the carrier entity kind/subtype.
func findImportEdge(ents []types.EntityRecord, sourceModule string) *types.RelationshipRecord {
	for i := range ents {
		e := &ents[i]
		for j := range e.Relationships {
			r := &e.Relationships[j]
			if r.Kind != "IMPORTS" {
				continue
			}
			if r.Properties != nil && r.Properties.Get("source_module") == sourceModule {
				return r
			}
		}
	}
	return nil
}

// Known external root package: `from django.db import models` →
// ToID="ext:django:models". The resolver's IsKnownExternalPackage
// allowlist will then classify this as ExternalKnown directly.
func TestImportsRewriteKnownExternal(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.py",
		Language: "python",
		Content:  []byte("from django.db import models\nimport requests\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findImportEdge(ents, "django.db")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for django.db")
	}
	if !strings.HasPrefix(r.ToID, "ext:django") {
		t.Fatalf("django.db import ToID = %q, want prefix ext:django", r.ToID)
	}
	r2 := findImportEdge(ents, "requests")
	if r2 == nil {
		t.Fatalf("missing IMPORTS edge for requests")
	}
	if r2.ToID != "ext:requests" {
		t.Fatalf("requests import ToID = %q, want ext:requests", r2.ToID)
	}
}

// Unknown external / in-tree imports are left untouched: the resolver's
// downstream ResolveDottedImportTarget path needs the original dotted
// shape to bind in-tree modules.
func TestImportsLeavesUnknownAlone(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.py",
		Language: "python",
		Content:  []byte("from myapp.users import models\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findImportEdge(ents, "myapp.users")
	if r == nil {
		t.Fatalf("missing IMPORTS edge for myapp.users")
	}
	if strings.HasPrefix(r.ToID, "ext:") {
		t.Fatalf("myapp.users import ToID = %q, must not be ext: form", r.ToID)
	}
}

// Polyglot-platform corpus additions: packages that were missing from
// pythonKnownExternalRoots and caused unresolved IMPORTS on the
// polyglot-platform group (bug-rate experiment 2026-05-23).
//
// Each sub-test checks that the top-level root is rewritten to ext:<root>
// form so the resolver's external-disposition gate classifies the edge
// as ExternalKnown rather than routing it to bug-extractor.
func TestImportsRewritePolyglotExternals(t *testing.T) {
	ex := &Extractor{}

	cases := []struct {
		name       string
		src        string
		sourcemod  string
		wantPrefix string
	}{
		{
			name:       "opentelemetry_trace",
			src:        "from opentelemetry import trace\n",
			sourcemod:  "opentelemetry",
			wantPrefix: "ext:opentelemetry",
		},
		{
			name:       "opentelemetry_sdk_submodule",
			src:        "from opentelemetry.sdk.trace import TracerProvider\n",
			sourcemod:  "opentelemetry.sdk.trace",
			wantPrefix: "ext:opentelemetry",
		},
		{
			name:       "airflow_dag",
			src:        "from airflow import DAG\n",
			sourcemod:  "airflow",
			wantPrefix: "ext:airflow",
		},
		{
			name:       "airflow_operators_python",
			src:        "from airflow.operators.python import PythonOperator\n",
			sourcemod:  "airflow.operators.python",
			wantPrefix: "ext:airflow",
		},
		{
			name:       "strawberry_import",
			src:        "import strawberry\n",
			sourcemod:  "strawberry",
			wantPrefix: "ext:strawberry",
		},
		{
			name:       "strawberry_fastapi",
			src:        "from strawberry.fastapi import GraphQLRouter\n",
			sourcemod:  "strawberry.fastapi",
			wantPrefix: "ext:strawberry",
		},
		{
			name:       "grpc_import",
			src:        "import grpc\n",
			sourcemod:  "grpc",
			wantPrefix: "ext:grpc",
		},
		{
			name:       "aio_pika",
			src:        "import aio_pika\n",
			sourcemod:  "aio_pika",
			wantPrefix: "ext:aio_pika",
		},
		{
			name:       "kafka",
			src:        "from kafka import KafkaProducer, KafkaConsumer\n",
			sourcemod:  "kafka",
			wantPrefix: "ext:kafka",
		},
		{
			name:       "hvac",
			src:        "import hvac\n",
			sourcemod:  "hvac",
			wantPrefix: "ext:hvac",
		},
		{
			name:       "pgvector",
			src:        "from pgvector.psycopg import register_vector\n",
			sourcemod:  "pgvector.psycopg",
			wantPrefix: "ext:pgvector",
		},
		{
			name:       "sentence_transformers",
			src:        "from sentence_transformers import SentenceTransformer\n",
			sourcemod:  "sentence_transformers",
			wantPrefix: "ext:sentence_transformers",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ents, err := ex.Extract(context.Background(), extractor.FileInput{
				Path:     "app/main.py",
				Language: "python",
				Content:  []byte(tc.src),
			})
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			r := findImportEdge(ents, tc.sourcemod)
			if r == nil {
				t.Fatalf("missing IMPORTS edge for source_module=%q", tc.sourcemod)
			}
			if !strings.HasPrefix(r.ToID, tc.wantPrefix) {
				t.Fatalf("ToID = %q, want prefix %q", r.ToID, tc.wantPrefix)
			}
		})
	}
}

// Relative imports are never rewritten — `from .foo import bar` carries
// a source_module starting with "." which is never an external package.
//
// Issue #693: IMPORTS edges now live on the file entity; the test checks
// all entities' IMPORTS edges (no longer filtering by SCOPE.Component/module).
func TestImportsSkipsRelative(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:     "demo.py",
		Language: "python",
		Content:  []byte("from .helpers import shape\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			if strings.HasPrefix(r.ToID, "ext:") {
				t.Fatalf("relative import got ext: ToID = %q", r.ToID)
			}
		}
	}
}

// Issue #2019 — Python relative-import resolution regression tests.
//
// `from .X import Y` was misresolved as external library X (leading dot
// stripped before package resolution). The root cause: for __init__.py
// files, filePathToModule already collapses the __init__ leaf, so
// resolvePythonImportModule was dropping one extra level, leaving the
// package root empty and causing celery/etc. to be mistaken for the
// well-known external package of the same name.

// TestRelativeImport_SingleDot verifies that `from .X import Y` inside a
// regular module resolves source_module to the sibling module path, NOT to
// the bare name X (which could collide with an external library).
//
// client-fixture-X: simple package layout simulating real project structure.
//
// `client_fixture_x/services/orders.py` + `from .helpers import format_id`
// → source_module = "client_fixture_x.services.helpers"
// → ToID = "client_fixture_x.services.helpers.format_id"  (NOT ext:helpers)
func TestRelativeImport_SingleDot(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:    "client_fixture_x/services/orders.py",
		Content: []byte("from .helpers import format_id\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findImportEdge(ents, "client_fixture_x.services.helpers")
	if r == nil {
		t.Fatalf("missing IMPORTS edge with source_module=client_fixture_x.services.helpers; got edges:")
	}
	if strings.HasPrefix(r.ToID, "ext:") {
		t.Errorf("relative import from regular module got ext: ToID = %q, want internal form", r.ToID)
	}
	want := "client_fixture_x.services.helpers.format_id"
	if r.ToID != want {
		t.Errorf("ToID = %q, want %q", r.ToID, want)
	}
}

// TestRelativeImport_SingleDot_InitPy is the W9R1 ground-truth regression:
// `from .celery import app` in `client_fixture_x/__init__.py` must resolve
// to source_module = "client_fixture_x.celery", NOT to "celery" (which
// would then be rewritten to ext:celery:app by resolveImportToIDs).
func TestRelativeImport_SingleDot_InitPy(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:    "client_fixture_x/__init__.py",
		Content: []byte("from .celery import app\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Must NOT have an ext:celery edge.
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			if strings.HasPrefix(r.ToID, "ext:celery") {
				t.Errorf("__init__.py relative import got ext:celery ToID = %q, want internal resolution", r.ToID)
			}
		}
	}
	// Must have the correctly resolved edge.
	r := findImportEdge(ents, "client_fixture_x.celery")
	if r == nil {
		t.Fatalf("missing IMPORTS edge with source_module=client_fixture_x.celery")
	}
	want := "client_fixture_x.celery.app"
	if r.ToID != want {
		t.Errorf("ToID = %q, want %q", r.ToID, want)
	}
}

// TestRelativeImport_DoubleDot verifies `from ..X import Y` climbs one level
// above the current package.
//
// `client_fixture_x/foo/bar.py` + `from ..models import BaseModel`
// → source_module = "client_fixture_x.models"
// → ToID = "client_fixture_x.models.BaseModel"
func TestRelativeImport_DoubleDot(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:    "client_fixture_x/foo/bar.py",
		Content: []byte("from ..models import BaseModel\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findImportEdge(ents, "client_fixture_x.models")
	if r == nil {
		t.Fatalf("missing IMPORTS edge with source_module=client_fixture_x.models")
	}
	want := "client_fixture_x.models.BaseModel"
	if r.ToID != want {
		t.Errorf("ToID = %q, want %q", r.ToID, want)
	}
}

// TestRelativeImport_FromDotImport covers `from . import Y` (no module name
// between dot and import keyword). The imported name Y is a sibling module.
//
// `client_fixture_x/services/dispatch.py` + `from . import helpers`
// → source_module = "client_fixture_x.services"
// → IMPORTS ToID should be internal (not ext:)
func TestRelativeImport_FromDotImport(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:    "client_fixture_x/services/dispatch.py",
		Content: []byte("from . import helpers\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Any IMPORTS edge produced must NOT be ext: prefixed.
	for _, e := range ents {
		for _, r := range e.Relationships {
			if r.Kind != "IMPORTS" {
				continue
			}
			if strings.HasPrefix(r.ToID, "ext:") {
				t.Errorf("from . import got ext: ToID = %q", r.ToID)
			}
		}
	}
}

// TestRelativeImport_AliasedImport verifies that `from .X import Y as Z`
// preserves the alias on the IMPORTS edge (local_name=Z, imported_name=Y).
//
// `client_fixture_x/api/views.py` + `from .serializers import OrderSerializer as OS`
// → local_name = "OS"
// → imported_name = "OrderSerializer"
// → source_module = "client_fixture_x.api.serializers"
func TestRelativeImport_AliasedImport(t *testing.T) {
	ex := &Extractor{}
	ents, err := ex.Extract(context.Background(), extractor.FileInput{
		Path:    "client_fixture_x/api/views.py",
		Content: []byte("from .serializers import OrderSerializer as OS\n"),
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	r := findImportEdge(ents, "client_fixture_x.api.serializers")
	if r == nil {
		t.Fatalf("missing IMPORTS edge with source_module=client_fixture_x.api.serializers")
	}
	if r.Properties == nil {
		t.Fatalf("IMPORTS edge has nil Properties")
	}
	if got := r.Properties.Get("local_name"); got != "OS" {
		t.Errorf("local_name = %q, want %q", got, "OS")
	}
	if got := r.Properties.Get("imported_name"); got != "OrderSerializer" {
		t.Errorf("imported_name = %q, want %q", got, "OrderSerializer")
	}
	if strings.HasPrefix(r.ToID, "ext:") {
		t.Errorf("aliased relative import got ext: ToID = %q", r.ToID)
	}
}
