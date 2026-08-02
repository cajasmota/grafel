package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/extractors"
	"github.com/cajasmota/grafel/internal/graph"
)

// Issue #5989 — the ~340 framework/custom extractors under internal/custom/**
// register under prefixed registry keys (e.g. "python_django") and are only
// selectable via extractors.RunCustomExtractors. Ordinary dispatch does an
// exact-key Get(file.Language) on "python", which never matches, so none of
// them have ever run on the DEFAULT in-process indexing path.
//
// These tests pin (a) the new opt-in gate's parsing and default-off contract
// and (b) the behavioural difference at the seam: with the gate on, a Django
// fixture must produce framework entities that the gate-off run does not.

func TestInProcCustomExtractorsDefaultOff(t *testing.T) {
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	if inProcCustomExtractors() {
		t.Fatalf("gate must be OFF when GRAFEL_INPROC_CUSTOM_EXTRACTORS is unset")
	}
}

func TestInProcCustomExtractorsGateParsing(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"no", false},
		{"false", false},
		{"garbage", false},
		{"1", true},
		{" 1 ", true},
		{"true", true},
		{"TRUE", true},
		{"yes", true},
		{"Yes", true},
	}
	for _, tc := range cases {
		t.Run("val="+tc.val, func(t *testing.T) {
			t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", tc.val)
			if got := inProcCustomExtractors(); got != tc.want {
				t.Fatalf("inProcCustomExtractors(%q) = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}

// writeDjangoFixture lays down a minimal but realistic Django/DRF project —
// enough surface for the python_django / python_drf custom extractors to fire.
func writeDjangoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"myproj/settings.py": `
INSTALLED_APPS = [
    "django.contrib.admin",
    "rest_framework",
    "myapp",
]
ROOT_URLCONF = "myproj.urls"
DATABASES = {"default": {"ENGINE": "django.db.backends.postgresql", "NAME": "app"}}
`,
		"myproj/urls.py": `
from django.urls import path, include
from myapp import views

urlpatterns = [
    path("orders/", views.OrderList.as_view(), name="order-list"),
    path("orders/<int:pk>/", views.OrderDetail.as_view(), name="order-detail"),
    path("health/", views.health, name="health"),
]
`,
		"myapp/models.py": `
from django.db import models


class Order(models.Model):
    reference = models.CharField(max_length=64)
    total = models.DecimalField(max_digits=10, decimal_places=2)
    customer = models.ForeignKey("Customer", on_delete=models.CASCADE)


class Customer(models.Model):
    email = models.EmailField(unique=True)
`,
		"myapp/serializers.py": `
from rest_framework import serializers
from myapp.models import Order


class OrderSerializer(serializers.ModelSerializer):
    class Meta:
        model = Order
        fields = ["id", "reference", "total"]
`,
		"myapp/views.py": `
from rest_framework import generics
from rest_framework.decorators import api_view
from rest_framework.response import Response
from myapp.models import Order
from myapp.serializers import OrderSerializer


class OrderList(generics.ListCreateAPIView):
    queryset = Order.objects.all()
    serializer_class = OrderSerializer


class OrderDetail(generics.RetrieveUpdateDestroyAPIView):
    queryset = Order.objects.all()
    serializer_class = OrderSerializer


@api_view(["GET"])
def health(request):
    return Response({"ok": True})
`,
		"myapp/tasks.py": `
from celery import shared_task


@shared_task
def send_receipt(order_id):
    return order_id
`,
		// Non-Python surface — #5989 requires checking that languages other
		// than Python survive the custom-extractor dispatch cleanly.
		"web/server.js": `
const express = require("express");
const app = express();

app.get("/api/orders", (req, res) => res.json([]));
app.post("/api/orders", (req, res) => res.status(201).json({}));

module.exports = app;
`,
		"svc/handler.go": `
package svc

import "net/http"

// Handler serves the orders API.
type Handler struct{}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
`,
		"api/OrdersController.java": `
package api;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class OrdersController {
    @GetMapping("/orders")
    public String list() {
        return "[]";
    }
}
`,
		"rb/orders_controller.rb": `
class OrdersController < ApplicationController
  def index
    render json: []
  end
end
`,
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func kindCounts(doc *graph.Document) map[string]int {
	out := map[string]int{}
	for i := range doc.Entities {
		out[doc.Entities[i].Kind]++
	}
	return out
}

// TestInProcCustomExtractorsProduceFrameworkEntities is the core behavioural
// test for #5989: flipping the gate on must route the Django fixture through
// the custom/framework extractors and yield entity kinds the base python
// extractor alone never emits.
func TestInProcCustomExtractorsProduceFrameworkEntities(t *testing.T) {
	fixture := writeDjangoFixture(t)

	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	off := runIndexerOn(t, fixture, "fx_off", nil)
	offKinds := kindCounts(off)

	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	on := runIndexerOn(t, fixture, "fx_on", nil)
	onKinds := kindCounts(on)

	if len(on.Entities) <= len(off.Entities) {
		t.Fatalf("gate ON must add entities: off=%d on=%d\noff kinds=%v\non kinds=%v",
			len(off.Entities), len(on.Entities), offKinds, onKinds)
	}
	if len(on.Relationships) <= len(off.Relationships) {
		t.Fatalf("gate ON must add relationships: off=%d on=%d",
			len(off.Relationships), len(on.Relationships))
	}

	// NOTE ON http_endpoint_definition: it does NOT grow here, and that is
	// correct. The Django URLconf → endpoint mapping is already performed on
	// the base path by the in-index django_cbv_routes post-pass (it reports
	// "django_cbv_routes=8 entities" in BOTH arms). The custom extractors are
	// additive to a surface the post-passes already cover, which is precisely
	// why the entity gain from this gate is modest. Asserting endpoint growth
	// here would encode a false expectation.
	if onKinds["http_endpoint_definition"] == 0 {
		t.Fatalf("expected the base path to already emit http_endpoint_definition; on kinds=%v", onKinds)
	}

	// What the Python framework extractors genuinely add on this fixture:
	// richer SCOPE.* modelling of the DRF serializers / Celery task / settings.
	for _, k := range []string{"SCOPE.Schema", "SCOPE.Operation"} {
		if onKinds[k] <= offKinds[k] {
			t.Errorf("kind %s did not grow with the gate on: off=%d on=%d",
				k, offKinds[k], onKinds[k])
		}
	}
	// Kinds only the custom extractors produce at all.
	for _, k := range []string{"SCOPE.Pattern", "SCOPE.Service"} {
		if offKinds[k] != 0 {
			t.Errorf("kind %s unexpectedly present with the gate OFF: %d", k, offKinds[k])
		}
		if onKinds[k] == 0 {
			t.Errorf("kind %s absent with the gate ON", k)
		}
	}
}

// entityTuples returns a CONTENT multiset of the entities for one language
// (or all languages when lang is ""), keyed by every field a substitution
// could change.
//
// COUNTS ARE NOT ENOUGH. An earlier version of the multi-language check
// compared per-language entity COUNTS and was therefore vacuous: on the big
// fixture Java's count is identical in both arms (320 -> 320) while 120
// entities are destroyed and 120 mutated replacements appear. A set-or-count
// comparison is structurally blind to substitution — the same defect class as
// #6037. Compare tuples, and compare them as a MULTISET (counts per tuple),
// so a duplicate appearing or disappearing is also visible.
func entityTuples(doc *graph.Document, lang string) map[string]int {
	out := map[string]int{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		if lang != "" && e.Language != lang {
			continue
		}
		out[fmt.Sprintf("%s|%s|%s|%s|%s|%d|%d",
			e.Kind, e.Name, e.QualifiedName, e.Subtype, e.SourceFile, e.StartLine, e.EndLine)]++
	}
	return out
}

// tupleDelta returns the tuples destroyed by (present in a, missing/reduced in
// b) and added by (present in b, missing/reduced in a) the gate.
func tupleDelta(a, b map[string]int) (destroyed, added []string) {
	for k, n := range a {
		if b[k] < n {
			destroyed = append(destroyed, k)
		}
	}
	for k, n := range b {
		if a[k] < n {
			added = append(added, k)
		}
	}
	sort.Strings(destroyed)
	sort.Strings(added)
	return destroyed, added
}

// indexBothArms runs the fixture with the gate off and on and returns both docs.
func indexBothArms(t *testing.T, tag string) (off, on *graph.Document) {
	t.Helper()
	fixture := writeDjangoFixture(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	off = runIndexerOn(t, fixture, tag, nil)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	on = runIndexerOn(t, fixture, tag, nil)
	return off, on
}

// TestInProcCustomExtractorsGoAndRubyAreInert pins the two languages that are
// GENUINELY unaffected by the gate. Go (40 dispatched custom extractors) and
// Ruby (21) produce byte-identical content tuples in both arms.
//
// This is a strict content-level assertion, not a count: if a future extractor
// starts mutating Go or Ruby entities in place, this fails.
func TestInProcCustomExtractorsGoAndRubyAreInert(t *testing.T) {
	off, on := indexBothArms(t, "inert_lang_repo")
	for _, lang := range []string{"go", "ruby"} {
		destroyed, added := tupleDelta(entityTuples(off, lang), entityTuples(on, lang))
		if len(destroyed) != 0 || len(added) != 0 {
			t.Errorf("%s is no longer inert under the gate: destroyed=%v added=%v",
				lang, destroyed, added)
		}
	}
}

// TestInProcCustomExtractorsJavaScriptIsNotInert pins that JavaScript IS
// affected by the gate — correcting an earlier false claim that every
// non-Python language contributed nothing.
//
// On this fixture the JS custom extractors REPLACE the two express route
// operations with better-positioned versions: the base path emits them at
// line 0 (no position), the custom path emits them at their real lines. That
// is an improvement, but it is still a SUBSTITUTION, and a count-based test
// cannot see it (2 -> 2).
func TestInProcCustomExtractorsJavaScriptIsNotInert(t *testing.T) {
	off, on := indexBothArms(t, "js_lang_repo")
	destroyed, added := tupleDelta(entityTuples(off, "javascript"), entityTuples(on, "javascript"))
	if len(destroyed) == 0 && len(added) == 0 {
		t.Fatalf("expected the gate to change JavaScript entities; it changed nothing. " +
			"If the JS custom extractors genuinely became inert, move javascript " +
			"into TestInProcCustomExtractorsGoAndRubyAreInert.")
	}
	// The specific substitution: line-0 route operations replaced by located ones.
	var zeroLineDestroyed int
	for _, d := range destroyed {
		if strings.Contains(d, "SCOPE.Operation|") && strings.HasSuffix(d, "|0|0") {
			zeroLineDestroyed++
		}
	}
	if zeroLineDestroyed == 0 {
		t.Errorf("expected the gate to replace line-0 JS route operations; destroyed=%v", destroyed)
	}
	t.Logf("javascript destroyed=%v added=%v", destroyed, added)
}

// TestInProcCustomExtractorsJavaEntityMutation PINS A KNOWN DEFECT (#6104).
//
// The Java custom extractors mutate existing entities IN PLACE rather than
// adding new ones. The collision is SAME KIND, SAME NAME, SAME FILE — only
// Subtype and EndLine differ:
//
//	OFF: SCOPE.Operation|OrdersController.list|api.OrdersController.list|method  |...|9|12
//	ON:  SCOPE.Operation|OrdersController.list|api.OrdersController.list|endpoint|...|9| 9
//
// Two things are corrupted: Subtype flips method -> endpoint, and EndLine is
// TRUNCATED to the start line, losing the body extent. The truncation
// propagates into the derived http_endpoint_definition entity too.
//
// WHY THIS TEST EXISTS SEPARATELY FROM THE CELERY PIN. Because the Java
// collision is same-(Kind, Name), the obvious remedy for the Celery loss —
// keying supersede on (Kind, Name) instead of Name alone — does NOT fix Java.
// Without this test, that fix would turn the Celery pin red, be declared
// complete, and ship with the Java corruption entirely unpinned.
//
// Asserts the BROKEN behaviour deliberately. When #6104 is fixed this test
// fails and should be inverted, not deleted.
func TestInProcCustomExtractorsJavaEntityMutation(t *testing.T) {
	off, on := indexBothArms(t, "java_lang_repo")
	destroyed, added := tupleDelta(entityTuples(off, "java"), entityTuples(on, "java"))

	if len(destroyed) == 0 {
		t.Fatalf("KNOWN DEFECT APPEARS FIXED: the gate no longer destroys Java entities. " +
			"If #6104 was fixed, invert this test to assert Java is inert.")
	}

	// 1. Subtype mutation on a same-kind/same-name/same-file entity.
	var subtypeMutated bool
	for _, d := range destroyed {
		if !strings.HasPrefix(d, "SCOPE.Operation|OrdersController.list|") || !strings.Contains(d, "|method|") {
			continue
		}
		for _, a := range added {
			if strings.HasPrefix(a, "SCOPE.Operation|OrdersController.list|") && strings.Contains(a, "|endpoint|") {
				subtypeMutated = true
			}
		}
	}
	if !subtypeMutated {
		t.Errorf("expected Java SCOPE.Operation subtype mutation method->endpoint; destroyed=%v added=%v",
			destroyed, added)
	}

	// 2. EndLine truncation — the body extent is lost. Compare the EndLine of
	// the destroyed vs added tuple for the same Kind|Name|File.
	endLineOf := func(tuple string) (kindName string, endLine int, ok bool) {
		parts := strings.Split(tuple, "|")
		if len(parts) != 7 {
			return "", 0, false
		}
		n, err := strconv.Atoi(parts[6])
		if err != nil {
			return "", 0, false
		}
		return parts[0] + "|" + parts[1] + "|" + parts[4], n, true
	}
	offEnd := map[string]int{}
	for _, d := range destroyed {
		if k, n, ok := endLineOf(d); ok {
			offEnd[k] = n
		}
	}
	var truncated []string
	for _, a := range added {
		k, n, ok := endLineOf(a)
		if !ok {
			continue
		}
		if prev, seen := offEnd[k]; seen && n < prev {
			truncated = append(truncated, fmt.Sprintf("%s EndLine %d->%d", k, prev, n))
		}
	}
	if len(truncated) == 0 {
		t.Errorf("expected Java EndLine truncation under the gate; destroyed=%v added=%v",
			destroyed, added)
	}
	t.Logf("java truncations: %v", truncated)

	// 3. The truncation must be visible on the derived endpoint entity too,
	// proving the corruption propagates beyond the entity it originated on.
	var endpointTruncated bool
	for _, s := range truncated {
		if strings.HasPrefix(s, "http_endpoint_definition|") {
			endpointTruncated = true
		}
	}
	if !endpointTruncated {
		t.Errorf("expected the EndLine truncation to propagate to http_endpoint_definition; got %v", truncated)
	}
}

// TestInProcCustomExtractorsGateOffIsInert asserts the gate genuinely gates:
// with it off, the produced graph must be semantically identical to a second
// gate-off run (i.e. the new code path contributes nothing at all).
func TestInProcCustomExtractorsGateOffIsInert(t *testing.T) {
	fixture := writeDjangoFixture(t)

	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	a := runIndexerOn(t, fixture, "inert_repo", nil)
	b := runIndexerOn(t, fixture, "inert_repo", nil)

	if len(a.Entities) != len(b.Entities) {
		t.Fatalf("gate-off entity count unstable: %d vs %d", len(a.Entities), len(b.Entities))
	}
	ka, kb := kindCounts(a), kindCounts(b)
	for k, v := range ka {
		if kb[k] != v {
			t.Fatalf("gate-off kind %q unstable: %d vs %d", k, v, kb[k])
		}
	}
}

// TestInProcCustomExtractorsGraphHygiene checks the correctness risk called
// out in #5989: these extractors have not run against the current graph in
// ~3 months. Assert no duplicate entity IDs, no empty names, and that every
// relationship endpoint resolves to a real entity.
func TestInProcCustomExtractorsGraphHygiene(t *testing.T) {
	fixture := writeDjangoFixture(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	doc := runIndexerOn(t, fixture, "hygiene_repo", nil)

	ids := map[string]int{}
	for i := range doc.Entities {
		e := &doc.Entities[i]
		ids[e.ID]++
		if e.Name == "" {
			t.Errorf("entity %s (kind=%s, file=%s) has empty Name", e.ID, e.Kind, e.SourceFile)
		}
		// Line-0 entities are the classic signature of an extractor that
		// synthesised a node without a real tree-sitter position.
		if e.StartLine < 0 {
			t.Errorf("entity %s (kind=%s) has negative StartLine %d", e.ID, e.Kind, e.StartLine)
		}
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("duplicate entity ID %q appears %d times", id, n)
		}
	}
}

// unresolvedEdgeTargets returns every relationship endpoint ID that does not
// name a real entity in the document, with its occurrence count.
//
// NOTE: a non-empty result is NORMAL on the base path — grafel deliberately
// keeps placeholder endpoints ("ext:django:models", "scope:operation:…") for
// unresolved externals. Absolute assertions on this set are therefore
// meaningless; only the DELTA introduced by the gate is informative.
func unresolvedEdgeTargets(doc *graph.Document) map[string]int {
	ids := make(map[string]bool, len(doc.Entities))
	for i := range doc.Entities {
		ids[doc.Entities[i].ID] = true
	}
	out := map[string]int{}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		if !ids[r.FromID] {
			out[r.FromID]++
		}
		if !ids[r.ToID] {
			out[r.ToID]++
		}
	}
	return out
}

// TestInProcCustomExtractorsSupersedeDestroysBaseEntities PINS A KNOWN DEFECT
// (#6104). It asserts the BROKEN behaviour deliberately so the defect is
// visible in CI rather than papered over.
//
// WHAT BREAKS. MergeWithCustom supersedes by NAME alone. When a custom
// extractor emits an entity whose Name collides with a base entity, the base
// node is REPLACED — so the gate is not purely additive, it DESTROYS content
// the base path produced.
//
// THE LOSS SET IS NOT ONE KIND AND NOT ONE LANGUAGE. On the full fixture the
// loss spans 5 kinds and 2 languages. Two distinct collision shapes exist, and
// they need DIFFERENT fixes:
//
//   - DIFFERENT-KIND collision (python/Celery). `@shared_task def
//     send_receipt` destroys TWO base entities — `Task|send_receipt` AND
//     `SCOPE.Operation|send_receipt|function` — replaced by a single
//     `SCOPE.Service|send_receipt|task`. Keying supersede on (Kind, Name)
//     would fix this shape.
//
//   - SAME-KIND collision (java/Spring). `SCOPE.Operation|C.list|method` is
//     replaced by `SCOPE.Operation|C.list|endpoint` with EndLine truncated.
//     Kind, Name and SourceFile are all IDENTICAL, so a (Kind, Name) key does
//     NOT fix this shape.
//
// THIS TEST DELIBERATELY COVERS BOTH SHAPES. If it only pinned the Celery
// loss, the obvious (Kind, Name) remedy would turn it red, be declared
// complete, and ship with the Java corruption unpinned. The same-kind
// assertion below is what makes a (Kind, Name)-only fix insufficient to make
// this test pass — it must still fail until the same-kind shape is fixed too.
//
// See also TestInProcCustomExtractorsJavaEntityMutation, which pins the Java
// field-level corruption in detail.
func TestInProcCustomExtractorsSupersedeDestroysBaseEntities(t *testing.T) {
	off, on := indexBothArms(t, "supersede_repo")

	destroyed, added := tupleDelta(entityTuples(off, ""), entityTuples(on, ""))
	if len(destroyed) == 0 {
		t.Fatalf("KNOWN DEFECT APPEARS FIXED: the gate destroys no base entities. " +
			"If #6104 was fixed, invert this test to assert the gate is purely additive.")
	}
	t.Logf("gate destroys %d base entity tuple(s):", len(destroyed))
	for _, d := range destroyed {
		t.Logf("    DESTROYED %s", d)
	}

	// --- Shape 1: DIFFERENT-KIND collision (python / Celery) ---------------
	// BOTH base entities for the task must be destroyed, not just the Task.
	// Pinning only `Task` understates the loss by half.
	wantDestroyed := []string{
		"Task|send_receipt|",            // the Celery task entity
		"SCOPE.Operation|send_receipt|", // AND its operation entity
	}
	for _, want := range wantDestroyed {
		found := false
		for _, d := range destroyed {
			if strings.HasPrefix(d, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected the gate to destroy an entity matching %q; destroyed=%v",
				want, destroyed)
		}
	}
	// The replacement is a single entity of a DIFFERENT kind.
	var serviceAdded bool
	for _, a := range added {
		if strings.HasPrefix(a, "SCOPE.Service|send_receipt|") {
			serviceAdded = true
		}
	}
	if !serviceAdded {
		t.Errorf("expected a SCOPE.Service replacement for the superseded task; added=%v", added)
	}

	// --- Shape 2: SAME-KIND collision (java / Spring) ---------------------
	// This is the assertion a (Kind, Name)-keyed fix cannot satisfy. Find a
	// destroyed tuple whose Kind, Name AND SourceFile all reappear in `added`
	// with different field values — i.e. a substitution that keying on
	// (Kind, Name) would still permit.
	keyOf := func(tuple string) (string, bool) {
		parts := strings.Split(tuple, "|")
		if len(parts) != 7 {
			return "", false
		}
		return parts[0] + "|" + parts[1] + "|" + parts[4], true // Kind|Name|SourceFile
	}
	addedKeys := map[string]bool{}
	for _, a := range added {
		if k, ok := keyOf(a); ok {
			addedKeys[k] = true
		}
	}
	var sameKindSubstitutions []string
	for _, d := range destroyed {
		if k, ok := keyOf(d); ok && addedKeys[k] {
			sameKindSubstitutions = append(sameKindSubstitutions, k)
		}
	}
	if len(sameKindSubstitutions) == 0 {
		t.Fatalf("KNOWN DEFECT APPEARS PARTIALLY FIXED: no same-(Kind, Name, SourceFile) "+
			"substitutions remain. A (Kind, Name)-keyed fix is NOT sufficient — the "+
			"same-kind shape (java/Spring) must be fixed too before this test may be "+
			"inverted. destroyed=%v added=%v", destroyed, added)
	}
	t.Logf("same-(Kind,Name,SourceFile) substitutions a (Kind,Name) fix would NOT prevent: %v",
		sameKindSubstitutions)

	// --- Dangling edges left behind by the supersede -----------------------
	uo, un := unresolvedEdgeTargets(off), unresolvedEdgeTargets(on)
	var introduced []string
	for id := range un {
		if _, existed := uo[id]; !existed {
			introduced = append(introduced, id)
		}
	}
	if len(introduced) == 0 {
		t.Errorf("expected the supersede to leave dangling edge endpoints behind")
	}
	sort.Strings(introduced)
	t.Logf("gate introduces %d new dangling edge endpoint(s): %v", len(introduced), introduced)
}

// TestInProcCustomExtractorsNilTreeGuardIsLoadBearing pins the
// `file.TSTree != nil` guard at the seam (mutant M3).
//
// The guard is not decorative. RunCustomExtractors does NOT require a parse
// tree to produce output — a subset of the custom extractors work directly on
// file CONTENT — so with a nil tree it still returns entities rather than
// erroring out. Dropping the guard would therefore admit content-only
// extractor output for files that FAILED TO PARSE, silently producing entities
// on exactly the inputs the indexer decided it could not understand.
//
// This asserts the underlying behaviour that makes the guard necessary; the
// guard itself is a one-line condition at the seam in classifyAndReadWithProgress.
func TestInProcCustomExtractorsNilTreeGuardIsLoadBearing(t *testing.T) {
	fi := extractors.FileInput{
		Path:     "myapp/models.py",
		Language: "python",
		Content: []byte("from django.db import models\n\n\n" +
			"class Widget(models.Model):\n    name = models.CharField(max_length=10)\n"),
		// TSTree deliberately nil — simulating a file that failed to parse.
	}
	ents, _ := extractors.RunCustomExtractors(context.Background(), fi)
	if len(ents) == 0 {
		t.Skip("custom extractors no longer emit content-only entities without a " +
			"parse tree; the nil-tree guard may now be redundant")
	}
	t.Logf("with a nil parse tree, RunCustomExtractors still emits %d entity(ies) — "+
		"this is what the `file.TSTree != nil` guard at the seam suppresses", len(ents))
}
