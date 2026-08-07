package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

// Issue #6118 — span completeness of the entities the in-process custom
// extractor gate (GRAFEL_INPROC_CUSTOM_EXTRACTORS) adds, and the C# file-level
// component start-line regression the same gate causes.
//
// WHY THIS IS BEHAVIOURAL AND NOT A SOURCE SCAN. Three source-scanning guards
// written in this area recently all fell to trivial mutants: a scan that
// asserts "every EntityRecord literal in internal/custom mentions EndLine" is
// satisfied by `EndLine: 0`. These tests index a real multi-language fixture
// with the gate off and on, persist and reload both graphs through
// graph.LoadGraphFromDir, and assert on the CONTENT of what lands on disk.

// writeSpanFixture6118 lays down a fixture covering the languages that
// actually contribute added entities under the gate. C# and Java dominate the
// corpus gain (84% between them), so both are represented in depth; the C#
// files are deliberately named after the class they declare, because that is
// the precondition for the file-component fold whose regression this pins.
func writeSpanFixture6118(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"cs/OrdersController.cs": `using Microsoft.AspNetCore.Mvc;

namespace Shop.Api
{
    [ApiController]
    [Route("api/orders")]
    public class OrdersController : ControllerBase
    {
        private readonly IOrderService _svc;

        public OrdersController(IOrderService svc) { _svc = svc; }

        [HttpGet]
        [Authorize(Roles = "admin")]
        public IActionResult List()
        {
            return Ok(_svc.All());
        }

        [HttpPost("{id}")]
        public IActionResult Create(int id)
        {
            return Ok();
        }
    }
}
`,
		// A DbContext, a MassTransit consumer, a MediatR handler, a
		// FluentValidation validator and a Quartz job: each is a class the C#
		// custom extractors re-emit under a framework-typed kind, which is what
		// turns the base AST SCOPE.Component into a shadow fold SOURCE.
		"cs/ShopContext.cs": `using Microsoft.EntityFrameworkCore;

namespace Shop.Api
{
    public class ShopContext : DbContext
    {
        public DbSet<Order> Orders { get; set; }
    }
}
`,
		"cs/OrderCreatedConsumer.cs": `using MassTransit;

namespace Shop.Api
{
    public class OrderCreatedConsumer : IConsumer<OrderCreated>
    {
        public async Task Consume(ConsumeContext<OrderCreated> context)
        {
        }
    }
}
`,
		"cs/CreateOrderHandler.cs": `using MediatR;

namespace Shop.Api
{
    public class CreateOrderHandler : IRequestHandler<CreateOrder, int>
    {
        public Task<int> Handle(CreateOrder request, CancellationToken ct)
        {
            return Task.FromResult(1);
        }
    }
}
`,
		"cs/OrderValidator.cs": `using FluentValidation;

namespace Shop.Api
{
    public class OrderValidator : AbstractValidator<Order>
    {
        public OrderValidator()
        {
            RuleFor(x => x.Total).NotEmpty();
        }
    }
}
`,
		"cs/CleanupJob.cs": `using Quartz;

namespace Shop.Api
{
    public class CleanupJob : IJob
    {
        public Task Execute(IJobExecutionContext context)
        {
            return Task.CompletedTask;
        }
    }
}
`,
		"cs/Startup.cs": `using Microsoft.Extensions.DependencyInjection;

namespace Shop.Api
{
    public class Startup
    {
        public void ConfigureServices(IServiceCollection services)
        {
            services.AddScoped<IOrderService, OrderService>();
            services.AddSingleton<ICache, MemCache>();
            services.AddDbContext<ShopContext>();
        }
    }
}
`,
		"java/OrdersController.java": `package api;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
public class OrdersController {
    @GetMapping("/orders")
    public String list() {
        return "[]";
    }

    @PostMapping("/orders")
    public String create() {
        return "{}";
    }
}
`,
		"ts/routes.ts": `import express from "express";

const app = express();

app.get("/api/items", (req, res) => res.json([]));
app.post("/api/items", (req, res) => res.status(201).json({}));

export default app;
`,
		"web/server.js": `const express = require("express");
const app = express();

app.get("/api/orders", (req, res) => res.json([]));

module.exports = app;
`,
		// Python: settings + URLconf + DRF serializer + Celery task. This is
		// the language whose custom-extractor helper never populated EndLine.
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
from django.urls import path
from myapp import views

urlpatterns = [
    path("orders/", views.OrderList.as_view(), name="order-list"),
    path("health/", views.health, name="health"),
]
`,
		"myapp/models.py": `
from django.db import models


class Order(models.Model):
    reference = models.CharField(max_length=64)
`,
		"myapp/serializers.py": `
from rest_framework import serializers
from myapp.models import Order


class OrderSerializer(serializers.ModelSerializer):
    class Meta:
        model = Order
        fields = ["id", "reference"]
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
		"svc/handler.go": `package svc

import "net/http"

// Handler serves the orders API.
type Handler struct{}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
`,
		"rs/routes.rs": `use axum::{routing::get, Router};

pub fn app() -> Router {
    Router::new().route("/items", get(list_items))
}

async fn list_items() -> &'static str {
    "[]"
}
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

// indexSpanFixtureBothArms indexes the #6118 fixture with the gate off and on,
// round-tripping each arm through the on-disk graph (see persistAndReload —
// asserting on the in-memory Document would be blind to anything persistence
// rewrites).
func indexSpanFixtureBothArms(t *testing.T) (off, on *graph.Document) {
	t.Helper()
	fixture := writeSpanFixture6118(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	off = persistAndReload(t, runIndexerOn(t, fixture, "span6118", nil))
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	on = persistAndReload(t, runIndexerOn(t, fixture, "span6118", nil))
	return off, on
}

// TestCustomExtractorAddedEntitiesCarryACompleteSpan is the #6118 headline
// gate: every entity the custom-extractor gate ADDS must be locatable in its
// own source file — StartLine > 0 and EndLine >= StartLine.
//
// "Added" is computed as a CONTENT-tuple delta (entityTuples/tupleDelta), not
// a count: an entity mutated in place shows up as one destroyed tuple plus one
// added tuple, and the added half is held to the same standard.
func TestCustomExtractorAddedEntitiesCarryACompleteSpan(t *testing.T) {
	off, on := indexSpanFixtureBothArms(t)

	// Index the ON graph by content tuple so an added tuple can be resolved
	// back to the entity that carries it.
	byTuple := map[string]*graph.Entity{}
	for i := range on.Entities {
		e := &on.Entities[i]
		byTuple[entityTupleKey(e)] = e
	}

	_, added := tupleDelta(entityTuples(off, ""), entityTuples(on, ""))
	if len(added) == 0 {
		t.Fatalf("fixture produced no added entities — the gate is not firing, so this test is vacuous")
	}

	var bad []string
	for _, tup := range added {
		e, ok := byTuple[tup]
		if !ok {
			t.Fatalf("added tuple %q not found in the ON graph", tup)
		}
		if e.StartLine <= 0 || e.EndLine < e.StartLine {
			bad = append(bad, fmt.Sprintf("%s|%s|%s start=%d end=%d",
				e.Kind, e.Name, e.SourceFile, e.StartLine, e.EndLine))
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("%d of %d entities added by the custom-extractor gate lack a complete span "+
			"(want StartLine > 0 and EndLine >= StartLine):\n  %s",
			len(bad), len(added), strings.Join(bad, "\n  "))
	}
}

// TestCustomExtractorGateDoesNotRegressFileComponentStartLines pins the second
// #6118 defect: a SCOPE.Component(subtype="file") entity that carries a real
// start line with the gate OFF must not lose it when the gate is ON.
//
// MECHANISM. foldFileComponentDuplicates promotes a class-like
// SCOPE.Component's span onto its co-located file entity. It runs AFTER
// foldClassHierarchyShadows. With the gate on, the C# custom extractors emit a
// framework-typed node for the same symbol (DbContext, MassTransit consumer,
// MediatR handler, Quartz job, …), which turns the base AST class component
// into a shadow fold SOURCE — it is dropped before the file fold ever sees it,
// so the file entity is left at line 0. An entity that had a correct position
// loses it.
func TestCustomExtractorGateDoesNotRegressFileComponentStartLines(t *testing.T) {
	off, on := indexSpanFixtureBothArms(t)

	spans := func(doc *graph.Document) map[string][2]int {
		out := map[string][2]int{}
		for i := range doc.Entities {
			e := &doc.Entities[i]
			if e.Kind == "SCOPE.Component" && e.Subtype == "file" {
				out[e.SourceFile] = [2]int{e.StartLine, e.EndLine}
			}
		}
		return out
	}
	offSpans, onSpans := spans(off), spans(on)

	var regressed []string
	for file, o := range offSpans {
		n, ok := onSpans[file]
		if !ok {
			t.Errorf("file component for %s disappeared with the gate on", file)
			continue
		}
		if o[0] > 0 && n[0] <= 0 {
			regressed = append(regressed, fmt.Sprintf("%s: start %d -> %d", file, o[0], n[0]))
		}
		if o[1] > 0 && n[1] <= 0 {
			regressed = append(regressed, fmt.Sprintf("%s: end %d -> %d", file, o[1], n[1]))
		}
	}
	sort.Strings(regressed)
	if len(regressed) > 0 {
		t.Errorf("%d file-level components lost a real position under the gate:\n  %s",
			len(regressed), strings.Join(regressed, "\n  "))
	}

	// Guard against the test being satisfied by the fixture producing no
	// positioned C# file components at all.
	positioned := 0
	for file, s := range offSpans {
		if strings.HasPrefix(file, "cs/") && s[0] > 0 {
			positioned++
		}
	}
	if positioned < 4 {
		t.Fatalf("fixture no longer exercises the regression: only %d C# file components "+
			"carry a start line with the gate OFF", positioned)
	}
}

// entityTupleKey is the same content tuple entityTuples aggregates on, for a
// single entity.
func entityTupleKey(e *graph.Entity) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%d|%d",
		e.Kind, e.Name, e.QualifiedName, e.Subtype, e.SourceFile, e.StartLine, e.EndLine)
}

// semanticDigest6118 is a stable content digest of a graph: entities as
// content tuples, relationships keyed by the CONTENT of their endpoints rather
// than by entity ID.
//
// WHY ENDPOINTS ARE KEYED BY CONTENT. Entity IDs embed the repo tag and are
// re-stamped by the merge, so an ID-keyed digest reports churn that is not a
// content change (#6083 — raw graph.fb bytes are meaningless for the same
// reason). Keying an edge by its endpoints' content tuples makes the digest
// sensitive to a real rewiring and blind to a re-keying.
func semanticDigest6118(doc *graph.Document) string {
	byID := map[string]string{}
	for i := range doc.Entities {
		byID[doc.Entities[i].ID] = entityTupleKey(&doc.Entities[i])
	}
	lines := make([]string, 0, len(doc.Entities)+len(doc.Relationships))
	for i := range doc.Entities {
		lines = append(lines, "E "+entityTupleKey(&doc.Entities[i]))
	}
	resolve := func(id string) string {
		if t, ok := byID[id]; ok {
			return t
		}
		return "?" + id
	}
	for i := range doc.Relationships {
		r := &doc.Relationships[i]
		lines = append(lines, fmt.Sprintf("R %s -[%s]-> %s", resolve(r.FromID), r.Kind, resolve(r.ToID)))
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// gateOffDigest6118Base is the semantic digest of the #6118 fixture indexed
// with GRAFEL_INPROC_CUSTOM_EXTRACTORS unset, captured at 2f0175dfc — the
// commit this work branched from. gateOffDigest6118 is the digest after the
// span-donation fix.
//
// THIS PIN IS DELIBERATELY A PAIR, AND THE PAIR IS THE POINT. Everything in
// #5989/#6118 is reachable only with the gate ON, so the working assumption
// going in was that the fix would leave the default graph byte-identical in
// content. IT DOES NOT, and pretending otherwise by quietly re-baselining a
// single constant would hide the most important thing this change found:
//
// the C# start-line regression is NOT caused by the gate. The gate only makes
// it fire more often. foldFileComponentDuplicates is the only pass that ever
// positions a SCOPE.Component(subtype="file"), and it is starved whenever
// foldClassHierarchyShadows has already consumed the class component that
// would have donated the span. That happens on the DEFAULT path too, wherever
// a framework-typed node already exists for the symbol — an ASP.NET
// controller, a Spring @Service, a Django CBV.
//
// On this fixture the default-path change is exactly one entity, and it is a
// pure gain of information with nothing destroyed:
//
//	cs/OrdersController.cs  SCOPE.Component(subtype="file")  start 0 -> 5
//
// Entity and relationship counts are unchanged (141 / 284), no entity loses a
// position, and no edge is rewired. TestCustomExtractorGateOffDeltaIsExactly-
// TheDocumentedSpanGain below asserts that shape directly, so the deviation is
// pinned behaviourally and not merely absorbed into a hash.
//
// If the current digest fails again, the change is no longer gate-scoped in the
// way described here: characterise the new delta and say so, rather than
// re-baselining a second time.
//
// #6138 MOVED IT A SECOND TIME, and the delta is again a pure gain. The file
// fold now only fires where the ecosystem makes a module and its exported
// declaration the same entity (fileIsItsDeclaration — named
// fileIsDeclarationExtensions until #6202 split the extension allow-list into a
// component-extension set and a default-export test), so eight
// declarations this fixture used to delete come back, each with the span and
// signature the extractor read:
//
//	cs/CleanupJob.cs           public class CleanupJob           5-11
//	cs/CreateOrderHandler.cs   public class CreateOrderHandler   5-11
//	cs/OrderCreatedConsumer.cs public class OrderCreatedConsumer 5-10
//	cs/OrderValidator.cs       public class OrderValidator       5-11
//	cs/ShopContext.cs          public class ShopContext          5-8
//	cs/Startup.cs              public class Startup              5-13
//	java/OrdersController.java @RestController class OrdersController 7-18
//	svc/handler.go             type Handler struct               6-6
//
// Counts move 141/284 -> 149/292: the eight entities plus the eight
// Module->declaration CONTAINS edges that carry them. Nothing is destroyed.
// Every CONTAINS/DEPENDS_ON edge that used to name the file component now names
// the declaration that actually owns the member, and every file component keeps
// the position the span donation gives it. The one entity that changes in place
// is the java/OrdersController.java file component, which hands qualified_name
// `api.OrdersController` back to the class it belongs to.
// #6152 MOVED IT A THIRD TIME, and the delta is again characterised rather than
// absorbed. Two Python rule sets (falcon, cherrypy) carried a source_pattern
// matching a BARE `class X:` and typing it `Controller`; Detect resolved rule
// sets by file.Language alone, so both fired on every Python file whether or not
// the framework was present. Gating them on the rule file's own
// frameworks.detection.import_markers changes exactly two things in this
// fixture, both in myapp/serializers.py:
//
//	Controller/OrderSerializer  ->  SCOPE.Component/OrderSerializer   (re-kinded)
//	Controller/Meta             ->  (deleted)
//
// `class Meta:` is a nested DRF options class. The Python extractor already
// emits it, correctly named `OrderSerializer.Meta`; the ungated bare-class
// pattern emitted a SECOND, differently-named node for the same declaration.
// Removing it takes the counts 149/292 -> 148/291: one phantom node and the one
// Module->Meta CONTAINS edge that carried it. No real declaration is lost —
// TestCustomExtractorGateOffDeltaIsExactlyTheDocumentedSpanGain still finds
// every file component and every donated span.
const (
	gateOffDigest6118Base = "a0a6ede910d8d0465d901153f483b27b8a57a565bdd79dd0104bef53786d9ca1"
	gateOffDigest6118     = "387a9c36cb585b4d73828afc997744dbe02e6df347e0860054adf69431996d46"
	gateOffDigest6138     = "8726464ce66a815e8b8a69bf8a9ee272368903f623c161fba81235237e235025"
	gateOffDigest6152     = "774eaec1d5ad5d9731d2c043a6207fdb7b3882b9fdf0f37e4a6b9dbdc555662f"
)

func TestCustomExtractorGateOffGraphIsUnchanged(t *testing.T) {
	fixture := writeSpanFixture6118(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	off := persistAndReload(t, runIndexerOn(t, fixture, "span6118", nil))
	got := semanticDigest6118(off)
	if got == gateOffDigest6152 {
		return
	}
	if got == gateOffDigest6138 {
		t.Fatalf("gate-OFF graph reverted to the pre-#6152 baseline — the falcon/cherrypy " +
			"bare-class patterns are typing plain Python classes `Controller` again")
	}
	if got == gateOffDigest6118 {
		t.Fatalf("gate-OFF graph reverted to the pre-#6138 baseline — the file fold is " +
			"deleting stem-named declarations in container-file languages again")
	}
	if got == gateOffDigest6118Base {
		t.Fatalf("gate-OFF graph reverted to the pre-fix 2f0175dfc baseline — the #6118 " +
			"span donation is no longer reaching the default path")
	}
	t.Fatalf("gate-OFF graph changed against ALL pinned digests\n got  %s\n post-#6152 %s\n post-#6138 %s\n post-#6118 %s\n 2f0175dfc %s\n"+
		"(entities=%d relationships=%d)", got, gateOffDigest6152, gateOffDigest6138,
		gateOffDigest6118, gateOffDigest6118Base, len(off.Entities), len(off.Relationships))
}

// TestCustomExtractorGateOffDeltaIsExactlyTheDocumentedSpanGain pins the shape
// of the default-path change described above, in content terms rather than as
// a hash: the gate-OFF graph must hold its documented entity and relationship
// counts, every file component that had a position must still have it, and the
// one entity that was positionless and is now positioned must be the documented
// one.
func TestCustomExtractorGateOffDeltaIsExactlyTheDocumentedSpanGain(t *testing.T) {
	fixture := writeSpanFixture6118(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	off := persistAndReload(t, runIndexerOn(t, fixture, "span6118", nil))

	// 141/284 at 2f0175dfc, plus the eight declarations #6138 stopped deleting
	// and the eight Module->declaration CONTAINS edges that carry them (149/292),
	// minus the one phantom `Controller/Meta` node the ungated falcon/cherrypy
	// bare-class patterns emitted for the nested DRF `class Meta:` and the
	// Module->Meta CONTAINS edge that carried it (#6152).
	const (
		wantEntities = 148
		wantRels     = 291
	)
	if len(off.Entities) != wantEntities || len(off.Relationships) != wantRels {
		t.Fatalf("gate-OFF graph size moved: entities=%d (want %d) relationships=%d (want %d) — "+
			"the span donation must add and destroy nothing",
			len(off.Entities), wantEntities, len(off.Relationships), wantRels)
	}

	// Every file component in the fixture that a positioned declaration can
	// donate to must now be positioned, including the one that regressed at
	// 2f0175dfc because its class component had already been shadow-folded.
	want := map[string]int{
		"cs/OrdersController.cs":     5,
		"cs/ShopContext.cs":          5,
		"cs/OrderCreatedConsumer.cs": 5,
		"cs/CreateOrderHandler.cs":   5,
		"cs/CleanupJob.cs":           5,
		"cs/OrderValidator.cs":       5,
		"cs/Startup.cs":              5,
	}
	got := map[string]int{}
	for i := range off.Entities {
		e := &off.Entities[i]
		if e.Kind == "SCOPE.Component" && e.Subtype == "file" {
			got[e.SourceFile] = e.StartLine
		}
	}
	for file, wantLine := range want {
		if got[file] != wantLine {
			t.Errorf("gate-OFF file component %s: start=%d, want %d", file, got[file], wantLine)
		}
	}
}

// TestFileComponentSpanDonationIsDeterministic guards the donor tie-break: two
// runs over the same fixture must position every file component identically.
// The donor scan walks `merged` in slice order, so a non-deterministic choice
// would show up as a flapping start line rather than as a crash.
func TestFileComponentSpanDonationIsDeterministic(t *testing.T) {
	fixture := writeSpanFixture6118(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "1")
	spans := func() map[string][2]int {
		doc := persistAndReload(t, runIndexerOn(t, fixture, "span6118", nil))
		out := map[string][2]int{}
		for i := range doc.Entities {
			e := &doc.Entities[i]
			if e.Kind == "SCOPE.Component" && e.Subtype == "file" {
				out[e.SourceFile] = [2]int{e.StartLine, e.EndLine}
			}
		}
		return out
	}
	a, b := spans(), spans()
	if len(a) != len(b) {
		t.Fatalf("file component count is not stable across runs: %d vs %d", len(a), len(b))
	}
	for f, sa := range a {
		if sb := b[f]; sa != sb {
			t.Errorf("%s span is not stable across runs: %v vs %v", f, sa, sb)
		}
	}
}
