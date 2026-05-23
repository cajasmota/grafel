package mcp

// Tests for #1790: param-name standardization (query/entity_id) with compat aliases.
//
// Six cases:
//   (a) new name "query"     works for archigraph_find
//   (b) old name "question"  works for archigraph_find + deprecation log fires
//   (c) new name "entity_id" works for archigraph_get_source
//   (d) old name "node_id"   works for archigraph_get_source + deprecation log fires
//   (e) new name "entity_id" works for archigraph_inspect
//   (f) old name "label_or_id" works for archigraph_inspect + deprecation log fires

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr to a buffer for the duration of fn,
// then restores it and returns whatever was written.
func captureStderr(fn func()) string {
	r, w, _ := os.Pipe()
	orig := os.Stderr
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = orig

	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck
	return buf.String()
}

func newSmokeSrv(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "r1")
	_ = os.MkdirAll(repo, 0o755)
	writeGraph(t, repo, fixtureDoc("r1"))
	regPath := makeRegistry(t, dir, map[string]map[string]string{"g": {"r1": repo}})
	srv, err := NewServer(Config{RegistryPath: regPath})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// (a) new name "query" works for archigraph_find — no deprecation warning.
func TestFindNewParamQuery(t *testing.T) {
	srv := newSmokeSrv(t)

	var sawHardError bool
	stderr := captureStderr(func() {
		r := callTool(t, srv, "archigraph_find", map[string]any{
			"query": "rareUniqueWidget",
			"group": "g",
		})
		if r.IsError {
			// response may be an error (node not found etc.) but should not be a
			// "missing required argument" hard error.
			text := resultText(r)
			if strings.Contains(text, "missing required argument") {
				sawHardError = true
			}
		}
	})

	if sawHardError {
		t.Error("new param 'query' should be accepted, got missing-arg error")
	}
	if strings.Contains(stderr, "[archigraph deprecation]") {
		t.Errorf("new param 'query' should NOT emit a deprecation warning, got stderr: %s", stderr)
	}
}

// (b) old name "question" works for archigraph_find AND deprecation log fires.
func TestFindOldParamQuestion_DeprecationFires(t *testing.T) {
	srv := newSmokeSrv(t)

	var resultIsHardError bool
	stderr := captureStderr(func() {
		r := callTool(t, srv, "archigraph_find", map[string]any{
			"question": "rareUniqueWidget",
			"group":    "g",
		})
		if r.IsError {
			text := resultText(r)
			if strings.Contains(text, "missing required argument") {
				resultIsHardError = true
			}
		}
	})

	if resultIsHardError {
		t.Error("legacy param 'question' should still work (compat alias), got missing-arg error")
	}
	if !strings.Contains(stderr, "[archigraph deprecation]") {
		t.Errorf("expected deprecation warning on stderr for legacy param 'question', got: %q", stderr)
	}
	if !strings.Contains(stderr, "archigraph_find") {
		t.Errorf("deprecation message should name the tool, got: %q", stderr)
	}
}

// (c) new name "entity_id" works for archigraph_get_source — no deprecation warning.
func TestGetSourceNewParamEntityID(t *testing.T) {
	srv := newSmokeSrv(t)

	var resultIsHardError bool
	stderr := captureStderr(func() {
		r := callTool(t, srv, "archigraph_get_source", map[string]any{
			"entity_id": "DashboardScreen",
			"group":     "g",
		})
		if r.IsError {
			text := resultText(r)
			if strings.Contains(text, "missing required argument") {
				resultIsHardError = true
			}
		}
	})

	if resultIsHardError {
		t.Error("new param 'entity_id' should be accepted, got missing-arg error")
	}
	if strings.Contains(stderr, "[archigraph deprecation]") {
		t.Errorf("new param 'entity_id' should NOT emit a deprecation warning, got stderr: %s", stderr)
	}
}

// (d) old name "node_id" works for archigraph_get_source AND deprecation log fires.
func TestGetSourceOldParamNodeID_DeprecationFires(t *testing.T) {
	srv := newSmokeSrv(t)

	var resultIsHardError bool
	stderr := captureStderr(func() {
		r := callTool(t, srv, "archigraph_get_source", map[string]any{
			"node_id": "DashboardScreen",
			"group":   "g",
		})
		if r.IsError {
			text := resultText(r)
			if strings.Contains(text, "missing required argument") {
				resultIsHardError = true
			}
		}
	})

	if resultIsHardError {
		t.Error("legacy param 'node_id' should still work (compat alias), got missing-arg error")
	}
	if !strings.Contains(stderr, "[archigraph deprecation]") {
		t.Errorf("expected deprecation warning on stderr for legacy param 'node_id', got: %q", stderr)
	}
	if !strings.Contains(stderr, "archigraph_get_source") {
		t.Errorf("deprecation message should name the tool, got: %q", stderr)
	}
}

// (e) new name "entity_id" works for archigraph_inspect — no deprecation warning.
func TestInspectNewParamEntityID(t *testing.T) {
	srv := newSmokeSrv(t)

	var resultIsHardError bool
	stderr := captureStderr(func() {
		r := callTool(t, srv, "archigraph_inspect", map[string]any{
			"entity_id": "DashboardScreen",
			"group":     "g",
		})
		if r.IsError {
			text := resultText(r)
			if strings.Contains(text, "missing required argument") {
				resultIsHardError = true
			}
		}
	})

	if resultIsHardError {
		t.Error("new param 'entity_id' should be accepted, got missing-arg error")
	}
	if strings.Contains(stderr, "[archigraph deprecation]") {
		t.Errorf("new param 'entity_id' should NOT emit a deprecation warning, got stderr: %s", stderr)
	}
}

// (f) old name "label_or_id" works for archigraph_inspect AND deprecation log fires.
func TestInspectOldParamLabelOrID_DeprecationFires(t *testing.T) {
	srv := newSmokeSrv(t)

	var resultIsHardError bool
	stderr := captureStderr(func() {
		r := callTool(t, srv, "archigraph_inspect", map[string]any{
			"label_or_id": "DashboardScreen",
			"group":       "g",
		})
		if r.IsError {
			text := resultText(r)
			if strings.Contains(text, "missing required argument") {
				resultIsHardError = true
			}
		}
	})

	if resultIsHardError {
		t.Error("legacy param 'label_or_id' should still work (compat alias), got missing-arg error")
	}
	if !strings.Contains(stderr, "[archigraph deprecation]") {
		t.Errorf("expected deprecation warning on stderr for legacy param 'label_or_id', got: %q", stderr)
	}
	if !strings.Contains(stderr, "archigraph_inspect") {
		t.Errorf("deprecation message should name the tool, got: %q", stderr)
	}
}
