// imports_test.go — coverage for the IMPORTS ToID resolveImportToIDs
// pass (analog of #642 for Python).

package python

import (
	"context"
	"strings"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
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
			if r.Properties != nil && r.Properties["source_module"] == sourceModule {
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
