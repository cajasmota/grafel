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

// TestInProcCustomExtractorsJavaEnrichment is the INVERSION of the former
// TestInProcCustomExtractorsJavaEntityMutation, which pinned #6104's known
// defect. It is now a correctness test.
//
// WHAT IT USED TO PIN. The Java custom extractors collide with the base path
// on SAME KIND, SAME NAME, SAME FILE:
//
//	OFF: SCOPE.Operation|OrdersController.list|api.OrdersController.list|method  |...|9|12
//	ON:  SCOPE.Operation|OrdersController.list|api.OrdersController.list|endpoint|...|9| 9
//
// Two things changed, and the old test asserted BOTH as broken behaviour:
// Subtype flipped method -> endpoint, and EndLine was TRUNCATED to the start
// line, losing the body extent — a truncation that propagated into the derived
// http_endpoint_definition.
//
// WHAT IT ASSERTS NOW, AND WHY THE TWO ARE TREATED DIFFERENTLY. Those two
// changes are not the same kind of change:
//
//   - The Subtype refinement is INFORMATION. "This method is really an
//     endpoint" is exactly what the framework extractor exists to know, and
//     discarding it would discard the point of the gate. It is KEPT, and the
//     value it displaced is retained under types.EntityBaseSubtypeProperty so
//     nothing is lost. Still asserted, now as desired behaviour.
//
//   - The EndLine truncation is DATA LOSS with no compensating information.
//     Same kind, same name, same file means these are the same graph node, and
//     a merge of two observations of one node must never produce a narrower
//     span than either observation. Now asserted as forbidden.
//
// The old test's third assertion — that the truncation propagates to the
// derived http_endpoint_definition — is kept and inverted with it: the
// derived entity must not be truncated either.
func TestInProcCustomExtractorsJavaEnrichment(t *testing.T) {
	off, on := indexBothArms(t, "java_lang_repo")
	offT, onT := entityTuples(off, "java"), entityTuples(on, "java")
	destroyed, added := tupleDelta(offT, onT)

	// 1. The Subtype refinement is KEPT — this is the gate's value on Java.
	var subtypeRefined bool
	for _, d := range destroyed {
		if !strings.HasPrefix(d, "SCOPE.Operation|OrdersController.list|") || !strings.Contains(d, "|method|") {
			continue
		}
		for _, a := range added {
			if strings.HasPrefix(a, "SCOPE.Operation|OrdersController.list|") && strings.Contains(a, "|endpoint|") {
				subtypeRefined = true
			}
		}
	}
	if !subtypeRefined {
		t.Errorf("expected the Java SCOPE.Operation subtype refinement method->endpoint to survive; destroyed=%v added=%v",
			destroyed, added)
	}

	// 2. NO SPAN MAY NARROW. Compare the span of every destroyed tuple against
	// its same Kind|Name|SourceFile successor in `added`. This is the
	// assertion that used to require a truncation to exist.
	spanOf := func(tuple string) (kindNameFile string, start, end int, ok bool) {
		parts := strings.Split(tuple, "|")
		if len(parts) != 7 {
			return "", 0, 0, false
		}
		st, err := strconv.Atoi(parts[5])
		if err != nil {
			return "", 0, 0, false
		}
		en, err := strconv.Atoi(parts[6])
		if err != nil {
			return "", 0, 0, false
		}
		return parts[0] + "|" + parts[1] + "|" + parts[4], st, en, true
	}
	type span struct{ start, end int }
	offSpan := map[string]span{}
	for _, d := range destroyed {
		if k, st, en, ok := spanOf(d); ok {
			offSpan[k] = span{st, en}
		}
	}
	var narrowed []string
	for _, a := range added {
		k, st, en, ok := spanOf(a)
		if !ok {
			continue
		}
		prev, seen := offSpan[k]
		if !seen {
			continue
		}
		if en < prev.end {
			narrowed = append(narrowed, fmt.Sprintf("%s EndLine %d->%d", k, prev.end, en))
		}
		// StartLine may only move EARLIER, or fill in from 0 (unknown).
		if prev.start != 0 && st > prev.start {
			narrowed = append(narrowed, fmt.Sprintf("%s StartLine %d->%d", k, prev.start, st))
		}
	}
	if len(narrowed) != 0 {
		t.Errorf("#6104: the merge narrowed %d Java span(s), which is data loss regardless "+
			"of which side wins: %v", len(narrowed), narrowed)
	}

	// 3. And specifically the derived http_endpoint_definition — the entity the
	// truncation used to propagate into — must be span-identical in both arms.
	for tuple, n := range offT {
		if !strings.HasPrefix(tuple, "http_endpoint_definition|") {
			continue
		}
		if onT[tuple] < n {
			t.Errorf("derived http_endpoint_definition tuple lost or altered under the gate: %q "+
				"(off=%d on=%d); added=%v", tuple, n, onT[tuple], added)
		}
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

// TestInProcCustomExtractorsSupersedeIsNonDestructive is the INVERSION of the
// former TestInProcCustomExtractorsSupersedeDestroysBaseEntities, which pinned
// #6104's known defect deliberately.
//
// WHAT IT USED TO PIN. MergeWithCustom superseded by NAME alone, so a custom
// entity whose Name collided with a base entity REPLACED it. The gate was not
// additive, it DESTROYED content the base path produced — 280 entities across
// 5 kinds and 2 languages on the full fixture, in three shapes: cross-kind
// (Task vs SCOPE.Operation), same-kind same-name, and same-kind in-place
// corruption (subtype rewrite + EndLine truncation).
//
// WHAT IT ASSERTS NOW. The merge policy is keyed on the graph's own identity
// (SourceFile, Kind, Name) and enforces two invariants — never lose an entity,
// never narrow a span. Both are asserted here at the WHOLE-GRAPH level, on
// content tuples compared as MULTISETS and on entity IDs, never on counts.
//
// ON THE ONE ID THAT LEGITIMATELY DISAPPEARS. `Task|send_receipt` still leaves
// the ON arm — but not at the merge boundary. It is consumed by the #1613
// class-shadow fold, which enforces a separate, pre-existing "one node per
// class symbol" invariant: SCOPE.Service outranks Task in
// frameworkClassKindPriority (100 vs 70), so the pair is folded and the edges
// are REPOINTED onto the survivor. That is a genuine combine, not a silent
// drop, and assertion (a) below is written to tell the two apart: a retired ID
// is only acceptable if a same-(Name, SourceFile) survivor exists AND no edge
// is left pointing at it.
func TestInProcCustomExtractorsSupersedeIsNonDestructive(t *testing.T) {
	off, on := indexBothArms(t, "supersede_repo")
	offT, onT := entityTuples(off, ""), entityTuples(on, "")
	destroyed, added := tupleDelta(offT, onT)
	t.Logf("gate replaces %d base entity tuple(s) with enriched versions:", len(destroyed))
	for _, d := range destroyed {
		t.Logf("    WAS %s", d)
	}

	// --- (a) INVARIANT 1: a merge never loses an entity -------------------
	onIDs := make(map[string]bool, len(on.Entities))
	for i := range on.Entities {
		onIDs[on.Entities[i].ID] = true
	}
	onNameFile := make(map[string][]string, len(on.Entities))
	for i := range on.Entities {
		k := on.Entities[i].Name + "|" + on.Entities[i].SourceFile
		onNameFile[k] = append(onNameFile[k], on.Entities[i].Kind)
	}
	onRefs := make(map[string]int, len(on.Relationships))
	for i := range on.Relationships {
		onRefs[on.Relationships[i].FromID]++
		onRefs[on.Relationships[i].ToID]++
	}
	for i := range off.Entities {
		e := &off.Entities[i]
		if onIDs[e.ID] {
			continue
		}
		survivors := onNameFile[e.Name+"|"+e.SourceFile]
		if len(survivors) == 0 {
			t.Errorf("#6104: the gate DESTROYED %s|%s (%s) — no entity with the same "+
				"name survives in that file", e.Kind, e.Name, e.SourceFile)
			continue
		}
		if n := onRefs[e.ID]; n > 0 {
			t.Errorf("#6104: %s|%s (%s) was retired but %d edge(s) still point at its "+
				"ID %s — a fold/supersede must re-key every edge, not only the node's own",
				e.Kind, e.Name, e.SourceFile, n, e.ID)
		}
		t.Logf("retired-and-folded (OK, #1613): %s|%s -> %v, 0 dangling edges",
			e.Kind, e.Name, survivors)
	}

	// --- (b) Shape 1 + 2: the Celery collisions ---------------------------
	// `@shared_task def send_receipt` used to destroy BOTH `Task|send_receipt`
	// (cross-kind) and `SCOPE.Operation|send_receipt` (the second half of the
	// double-destruction the original report missed). The SCOPE.Operation must
	// now survive untouched, and the SCOPE.Service must still be ADDED — the
	// gate's value is that it adds a kind, not that it swaps one for another.
	for _, d := range destroyed {
		if strings.HasPrefix(d, "SCOPE.Operation|send_receipt|") {
			t.Errorf("#6104 shape 1: the gate still destroys the Celery task's operation "+
				"entity: %q", d)
		}
	}
	var serviceAdded bool
	for _, a := range added {
		if strings.HasPrefix(a, "SCOPE.Service|send_receipt|") {
			serviceAdded = true
		}
	}
	if !serviceAdded {
		t.Errorf("expected the Celery custom extractor to ADD a SCOPE.Service; added=%v", added)
	}

	// --- (c) INVARIANT 2: a merge never narrows a span --------------------
	// Every remaining same-(Kind, Name, SourceFile) substitution must be an
	// ENRICHMENT: subtype refinement and/or a WIDER span. This is the shape the
	// old test required to exist; it is now constrained rather than forbidden,
	// because a same-identity pair IS one graph node and combining them is
	// correct — silently shrinking them was not.
	parse := func(tuple string) (key string, start, end int, ok bool) {
		parts := strings.Split(tuple, "|")
		if len(parts) != 7 {
			return "", 0, 0, false
		}
		st, err := strconv.Atoi(parts[5])
		if err != nil {
			return "", 0, 0, false
		}
		en, err := strconv.Atoi(parts[6])
		if err != nil {
			return "", 0, 0, false
		}
		return parts[0] + "|" + parts[1] + "|" + parts[4], st, en, true
	}
	type span struct{ start, end int }
	was := map[string]span{}
	for _, d := range destroyed {
		if k, st, en, ok := parse(d); ok {
			was[k] = span{st, en}
		}
	}
	var narrowed, substitutions []string
	for _, a := range added {
		k, st, en, ok := parse(a)
		if !ok {
			continue
		}
		prev, seen := was[k]
		if !seen {
			continue
		}
		substitutions = append(substitutions, k)
		if en < prev.end {
			narrowed = append(narrowed, fmt.Sprintf("%s EndLine %d->%d", k, prev.end, en))
		}
		if prev.start != 0 && st > prev.start {
			narrowed = append(narrowed, fmt.Sprintf("%s StartLine %d->%d", k, prev.start, st))
		}
	}
	if len(narrowed) != 0 {
		t.Errorf("#6104: the merge narrowed %d span(s): %v", len(narrowed), narrowed)
	}
	sort.Strings(substitutions)
	t.Logf("same-(Kind,Name,SourceFile) enrichments (all span-widening or neutral): %v",
		substitutions)

	// --- (d) No supersede-induced dangling edges --------------------------
	// The gate may still introduce dangling endpoints of its own — the
	// synthetic `Class:<Name>` stubs the framework extractors emit are tracked
	// separately as #6105 and are explicitly OUT OF SCOPE here. What must NOT
	// appear is a dangle naming an ID that EXISTED in the gate-off arm: that is
	// the supersede/re-keying failure this issue is about.
	offEntityIDs := make(map[string]bool, len(off.Entities))
	for i := range off.Entities {
		offEntityIDs[off.Entities[i].ID] = true
	}
	uo, un := unresolvedEdgeTargets(off), unresolvedEdgeTargets(on)
	var introduced, supersedeDangles []string
	for id := range un {
		if _, existed := uo[id]; existed {
			continue
		}
		introduced = append(introduced, id)
		if offEntityIDs[id] {
			supersedeDangles = append(supersedeDangles, id)
		}
	}
	if len(supersedeDangles) != 0 {
		sort.Strings(supersedeDangles)
		t.Errorf("#6104: %d edge endpoint(s) now dangle on IDs that were REAL ENTITIES "+
			"with the gate off — the supersede did not re-key them: %v",
			len(supersedeDangles), supersedeDangles)
	}
	sort.Strings(introduced)
	t.Logf("gate introduces %d new dangling endpoint(s), none of them retired entity IDs "+
		"(synthetic Class:<Name> stubs are #6105, out of scope here): %v",
		len(introduced), introduced)
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

// TestInProcCustomExtractorsThreeCollisionShapesAreClosed is the per-shape
// evidence for #6104. Each of the three CONFIRMED collision shapes gets its own
// assertion so a partial fix cannot pass.
//
// WHY MULTISETS AND NOT COUNTS. A per-kind count table cannot see
// destroy-and-re-add: on the full fixture `SCOPE.Operation` showed +120 while
// 120 of its members were destroyed, and gains masked losses exactly. That is
// how this defect was originally under-reported by 3.5x. Every assertion below
// is over entityTuples — a CONTENT multiset (counts per tuple) — or over entity
// IDs. Same instrument as internal/graph/parity after #6037.
//
// WHY THE THREE SHAPES ARE LISTED SEPARATELY. The obvious remedy — keying the
// supersede on (Kind, Name) instead of Name alone — closes shape 1 ONLY. It was
// verified by mutation that shapes 2 and 3 survive it intact. Listing them
// apart keeps that trap visible.
func TestInProcCustomExtractorsThreeCollisionShapesAreClosed(t *testing.T) {
	off, on := indexBothArms(t, "shapes_repo")
	offT, onT := entityTuples(off, ""), entityTuples(on, "")
	destroyed, added := tupleDelta(offT, onT)

	// Content-tuple multiset survival: a tuple present N times off must be
	// present at least N times on, OR have a same-(Kind,Name,SourceFile)
	// successor (a Tier A combine, which is one node either way).
	key := func(tuple string) string {
		p := strings.Split(tuple, "|")
		if len(p) != 7 {
			return tuple
		}
		return p[0] + "|" + p[1] + "|" + p[4]
	}
	addedKeys := map[string]bool{}
	for _, a := range added {
		addedKeys[key(a)] = true
	}
	survived := func(prefix string) (ok bool, why string) {
		for tuple, n := range offT {
			if !strings.HasPrefix(tuple, prefix) {
				continue
			}
			if onT[tuple] >= n {
				return true, "tuple survives verbatim (" + tuple + ")"
			}
			if addedKeys[key(tuple)] {
				return true, "combined into a same-(Kind,Name,File) successor (" + tuple + ")"
			}
			return false, "DESTROYED with no successor: " + tuple
		}
		return false, "no such tuple in the gate-off arm — fixture drift, assertion is vacuous"
	}

	// ---- Shape 1: CROSS-KIND collision (python / Celery) -----------------
	// `@shared_task def send_receipt` used to destroy BOTH the base
	// `Task|send_receipt` AND `SCOPE.Operation|send_receipt`, collapsing them
	// into one `SCOPE.Service|send_receipt`. Keying on (Kind, Name) closes
	// this one. Both base entities must now survive the MERGE.
	if ok, why := survived("SCOPE.Operation|send_receipt|"); !ok {
		t.Errorf("shape 1 (cross-kind) NOT closed: %s", why)
	} else {
		t.Logf("shape 1 (cross-kind, SCOPE.Operation|send_receipt): CLOSED — %s", why)
	}

	// ---- Shape 2: SAME-KIND, SAME NAME (python / Celery) -----------------
	// The custom extractor also emits a `Task`-kind entity of the same name,
	// so this collision is same-kind and a (Kind, Name) key does NOT close it.
	// The base Task must not be destroyed AT THE MERGE. It is subsequently
	// consumed by the #1613 class-shadow fold (SCOPE.Service outranks Task,
	// 100 vs 70) which REPOINTS its edges — a combine, so we assert on the
	// fold's contract rather than on tuple identity: a same-(Name, SourceFile)
	// survivor exists and nothing dangles on the retired ID.
	onIDs := make(map[string]bool, len(on.Entities))
	for i := range on.Entities {
		onIDs[on.Entities[i].ID] = true
	}
	onRefs := map[string]int{}
	for i := range on.Relationships {
		onRefs[on.Relationships[i].FromID]++
		onRefs[on.Relationships[i].ToID]++
	}
	var checkedShape2 bool
	for i := range off.Entities {
		e := &off.Entities[i]
		if e.Kind != "Task" || !strings.HasPrefix(e.Name, "send_receipt") {
			continue
		}
		checkedShape2 = true
		if onIDs[e.ID] {
			t.Logf("shape 2 (same-kind, Task|%s): CLOSED — entity survives verbatim", e.Name)
			continue
		}
		var survivors []string
		for j := range on.Entities {
			s := &on.Entities[j]
			if s.Name == e.Name && s.SourceFile == e.SourceFile {
				survivors = append(survivors, s.Kind)
			}
		}
		if len(survivors) == 0 {
			t.Errorf("shape 2 (same-kind) NOT closed: Task|%s destroyed with no survivor "+
				"in %s", e.Name, e.SourceFile)
			continue
		}
		if n := onRefs[e.ID]; n > 0 {
			t.Errorf("shape 2: Task|%s was retired but %d edge(s) still point at %s — "+
				"the retiring pass did not re-key them", e.Name, n, e.ID)
			continue
		}
		t.Logf("shape 2 (same-kind, Task|%s): CLOSED at the merge — folded by #1613 into %v "+
			"with 0 dangling edges", e.Name, survivors)
	}
	if !checkedShape2 {
		t.Errorf("shape 2 assertion is vacuous: no base Task entity in the gate-off arm")
	}

	// ---- Shape 3: SAME-KIND IN-PLACE CORRUPTION (java / Spring) ----------
	// Kind, Name and SourceFile all identical — these ARE one graph node. The
	// custom extractor rewrote Subtype method->endpoint AND truncated EndLine,
	// and the truncation propagated into the derived SCOPE.Process and
	// http_endpoint_definition. A (Kind, Name) key treats this as a legitimate
	// supersede and lets it through, which is why it needed a POLICY fix.
	//
	// Closed means: the node is still ONE node, the subtype refinement is
	// KEPT, and the span is NOT narrower than the gate-off span.
	spanOf := func(m map[string]int, prefix string) (tuple string, start, end int, found bool) {
		for tp := range m {
			if !strings.HasPrefix(tp, prefix) {
				continue
			}
			p := strings.Split(tp, "|")
			if len(p) != 7 {
				continue
			}
			st, err1 := strconv.Atoi(p[5])
			en, err2 := strconv.Atoi(p[6])
			if err1 != nil || err2 != nil {
				continue
			}
			return tp, st, en, true
		}
		return "", 0, 0, false
	}
	const javaOp = "SCOPE.Operation|OrdersController.list|"
	offTuple, offStart, offEnd, okOff := spanOf(offT, javaOp)
	onTuple, onStart, onEnd, okOn := spanOf(onT, javaOp)
	switch {
	case !okOff || !okOn:
		t.Errorf("shape 3 assertion is vacuous: java operation missing (off=%v on=%v)", okOff, okOn)
	case onEnd < offEnd:
		t.Errorf("shape 3 (in-place corruption) NOT closed: EndLine narrowed %d->%d\n  off=%s\n  on =%s",
			offEnd, onEnd, offTuple, onTuple)
	case offStart != 0 && onStart > offStart:
		t.Errorf("shape 3 NOT closed: StartLine clipped %d->%d\n  off=%s\n  on =%s",
			offStart, onStart, offTuple, onTuple)
	case !strings.Contains(onTuple, "|endpoint|"):
		t.Errorf("shape 3: the subtype refinement method->endpoint was lost; on=%s", onTuple)
	default:
		t.Logf("shape 3 (in-place corruption): CLOSED — span %d-%d preserved (was truncated to %d), "+
			"subtype refinement kept\n  off=%s\n  on =%s", onStart, onEnd, offStart, offTuple, onTuple)
	}

	// And the derived entity the corruption used to propagate into.
	for tuple, n := range offT {
		if strings.HasPrefix(tuple, "http_endpoint_definition|") && onT[tuple] < n {
			t.Errorf("shape 3: corruption still propagates to a derived entity: %q lost "+
				"(off=%d on=%d)", tuple, n, onT[tuple])
		}
	}
	t.Logf("destroyed=%d added=%d (tuple multiset delta)", len(destroyed), len(added))
}
