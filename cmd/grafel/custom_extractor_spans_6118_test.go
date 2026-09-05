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
//
// LANGUAGE IS PART OF THE TUPLE (#6862). Language is a dedicated top-level
// FlatBuffers slot (fbwriter/writer.go, #2370) — it round-trips into the
// shipped graph exactly like Kind and SourceFile do, and grafel_find
// --language reads it. Before #6862 the tuple hashed seven fields and Language
// was not among them, so an extractor that re-stamped the language token on
// EVERY entity it emits moved this digest by zero. That was measured, not
// assumed: mutating the Python extractor's token wholesale
// ("python" -> "zzlang", both TagEntitiesLanguage calls plus all eight direct
// `Language:` literals) left this digest byte-identical. The Go extractor's
// equivalent mutant did move it, but only via a structural side effect
// (141/286 instead of 142/287) — the field itself was still unobserved.
//
// Entity Properties are still NOT hashed. That is a deliberately separate
// decision with a much larger blast radius (#6862 declines to bundle it).
func entityTupleKey(e *graph.Entity) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%d",
		e.Kind, e.Name, e.QualifiedName, e.Subtype, e.SourceFile, e.Language,
		e.StartLine, e.EndLine)
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
//
// #6485 MOVED IT A FOURTH TIME, and again the delta is characterised item by
// item rather than absorbed. Route entities are no longer eligible targets in
// the phase-2 handler index, so an endpoint can no longer resolve to a route.
// This fixture held exactly one instance, in java/OrdersController.java, and it
// is worth stating what that instance was: the file declares
// `@GetMapping("/orders") list()` and `@PostMapping("/orders") create()`, whose
// two REAL endpoints (http:GET:/orders 9-12 and http:POST:/orders 14-17) are
// each properly IMPLEMENTS-linked to their handler method and are untouched
// here. The third endpoint, `http:ANY:/orders`, is the verb-less Spring
// placeholder for the same path, and it used to bind to `Route:/orders` — the
// very route the two real endpoints already belong to.
//
// Counts move 148/291 -> 147/286. The full delta, all of it downstream of that
// one binding:
//
//	DELETED  SCOPE.Process  http:ANY:/orders → /orders   java/OrdersController.java
//	DELETED  R Route:/orders -[IMPLEMENTS]-> http_endpoint_definition:http:ANY:/orders
//	DELETED  R SCOPE.Process -[STEP_IN_PROCESS]-> Route:/orders
//	DELETED  R SCOPE.Process -[STEP_IN_PROCESS]-> http_endpoint_definition:http:ANY:/orders
//	DELETED  R http_endpoint_definition:http:ANY:/orders -[ENTRY_POINT_OF]-> SCOPE.Process
//	DELETED  R Module:_external -[CONTAINS]-> SCOPE.Process
//	MOVED    E http_endpoint_definition http:ANY:/orders  9-0 -> 0-0
//	MOVED    R Module:span6118 -[CONTAINS]-> that endpoint, following it
//
// The one entity destroyed is a phantom. Every other SCOPE.Process in this
// fixture is named `<endpoint> → <handler function>`; this one was named
// `http:ANY:/orders → /orders`, a process flow from route /orders to route
// /orders, synthesised from the self-edge and carrying it one layer further
// into the process-flow graph. Its four edges go with it.
//
// NOTHING REAL IS LOST, which is the claim the re-pin rests on. Both genuine
// /orders endpoints survive with their spans; both genuine
// SCOPE.Operation -[IMPLEMENTS]-> endpoint edges survive; the Route entity and
// its Module CONTAINS survive; no file component, declaration or donated span
// moves. The endpoint that is kept goes positionless because
// bridgeEndpointToHandler used to rebind it onto its "handler" body — line 9,
// the `@GetMapping` annotation — and that rebind was derived from the binding
// #6485 declares invalid. It keeps its source_file and its source_handler
// property; it is now edgeless rather than wrongly linked, which is the
// NoHandlerProp keep-path decision that ships with #6485.
// #6601 RE-PIN — counts move 147/286 -> 142/281.
//
// The C# extractor's `using` carrier (`buildImport`) emitted
// Kind="SCOPE.Component" with Subtype UNSET, while every prune predicate in
// internal/resolve/imports.go selects on `Kind=="SCOPE.Component" &&
// Subtype=="import"`. The kind matched and the subtype never did, so the prune
// pass never even looked at them (considered=0) and they shipped as orphans.
// #6601 stamps the subtype at the producer. The full delta, all of it downstream
// of that one literal:
//
//	DELETED  E SCOPE.Component (subtype="")  FluentValidation   cs/OrderValidator.cs
//	DELETED  E SCOPE.Component (subtype="")  MassTransit        cs/OrderCreatedConsumer.cs
//	DELETED  E SCOPE.Component (subtype="")  MediatR            cs/CreateOrderHandler.cs
//	DELETED  E SCOPE.Component (subtype="")  Microsoft          cs/OrdersController.cs
//	DELETED  E SCOPE.Component (subtype="")  Microsoft          cs/ShopContext.cs
//	DELETED  E SCOPE.Component (subtype="")  Microsoft          cs/Startup.cs
//	DELETED  E SCOPE.Component (subtype="")  Quartz             cs/CleanupJob.cs
//	DELETED  R Module -[CONTAINS]-> each of the 7 above
//	ADDED    E SCOPE.External package  FluentValidation:FluentValidation
//	ADDED    E SCOPE.External package  MediatR:MediatR
//	ADDED    R Module -[CONTAINS]-> each of the 2 above
//	REWIRED  R 4 C# IMPORTS edges off the deleted carriers: 2 onto the new
//	         externals (ext:FluentValidation:FluentValidation, ext:MediatR:MediatR),
//	         2 onto bare module strings (Quartz, MassTransit) via the #6156
//	         module-string restore. Dangling endpoints 51 -> 53.
//
// NOTHING REAL IS LOST, which is the claim this re-pin rests on. All 7 deleted
// entities are subtype-less import carriers with 0/0 spans; the three `Microsoft`
// ones carried ONLY the Module CONTAINS edge, i.e. pure orphans; nothing
// legitimate referenced any of the 7. Each was already duplicated by the
// cross/imports placeholder for the same `using` — same Kind/Name/SourceFile,
// hence the same graph.EntityID, which does not hash Subtype — so every `using`
// was shipping twice and only the immortal copy is going away.
//
// The 2 bare-module ToIDs are the pre-existing #6156 restore path becoming
// reachable for C# for the first time, tracked separately in #6604. On the
// pre-#6601 graph those 2 edges pointed at a junk orphan carrier, so this is not
// a regression from a good state.
//
// 147/286 encoded the bug. Re-pin approved by the maintainer on PR #6603.

// #6742 RE-PIN — counts move 142/281 -> 142/287. Six added edges, nothing else.
//
// C# emitted no class-hierarchy edge for any base class or interface: the
// extractor only ever looked at base types declared in the SAME FILE, and this
// fixture declares every base type externally, so all seven C# classes shipped
// with no supertype at all while Java's structurally identical `implements`
// produced IMPLEMENTS. #6742 emits EXTENDS / IMPLEMENTS — the two kinds Java
// already uses — for every base-list entry. The full delta:
//
//	ADDED R cs/OrdersController.cs     OrdersController     -[EXTENDS]->    ControllerBase
//	ADDED R cs/ShopContext.cs          ShopContext          -[EXTENDS]->    DbContext
//	ADDED R cs/OrderValidator.cs       OrderValidator       -[EXTENDS]->    AbstractValidator
//	ADDED R cs/OrderCreatedConsumer.cs OrderCreatedConsumer -[IMPLEMENTS]-> IConsumer
//	ADDED R cs/CreateOrderHandler.cs   CreateOrderHandler   -[IMPLEMENTS]-> IRequestHandler
//	ADDED R cs/CleanupJob.cs           CleanupJob           -[IMPLEMENTS]-> IJob
//
// Six of the seven C# files declare a base type; cs/Startup.cs declares none and
// correctly gains nothing. Three of the six are framework base CLASSES and three
// are interfaces, and every kind here is decided by the ladder in
// internal/extractors/csharp/hierarchy.go WITHOUT reaching its
// naming-convention fallback: the generics are stripped to the bare leaf, and
// `I`+PascalCase is consulted only for the three that genuinely are interfaces.
// All six targets are external to the fixture, so all six are dangling
// bare-name endpoints — the same shape the C# CALLS and IMPORTS edges already
// use for out-of-repo types.
//
// NOTHING ELSE MOVED, which is the claim this re-pin rests on, and it was
// verified by diffing the whole gate-OFF graph against itself with
// attachCsharpHierarchy disabled: that variant reproduces gateOffDigest6601
// EXACTLY (142/281, d073c83f…), and the diff against it is six added edge lines
// and nothing else — no entity added, deleted, re-kinded or moved, no existing
// edge rewired, no dangling count change beyond the six new endpoints.

// #6862 MOVED IT A FIFTH TIME, AND THIS ONE IS NOT A GRAPH CHANGE. Every
// previous re-pin above moved the digest because the GRAPH moved. This one
// moves it because the TUPLE moved: entityTupleKey now hashes Language.
//
// The graph is byte-for-byte the same graph. Counts are unchanged at 142/287,
// no entity is added, deleted, re-kinded, re-positioned or renamed, and no edge
// is added, deleted or rewired — TestCustomExtractorGateOffDeltaIsExactlyThe-
// DocumentedSpanGain and TestCsharpHierarchyEdgesArePresentGateOff both pass
// unchanged across this re-pin. So the usual "enumerate the fixture members
// producing the delta" has no members to enumerate: the delta is one extra
// field appended to all 142 entity lines.
//
// What the tuple now observes, enumerated by the value it gained — the language
// census of the gate-OFF fixture graph, 142 entities:
//
//	python      39
//	csharp      32
//	(empty)     31
//	typescript  11
//	java        10
//	rust         9
//	javascript   6
//	go           4
//
// The 31 empty ones are not a hole in the extractors: every one of them is a
// SCOPE.External or a Module synthetic with NO source file (the `_external`
// and `span6118` modules, and 29 dangling import targets — Microsoft, axum,
// django, express, net/http, rest_framework, …). Nothing that came out of a
// file is language-less, which is the precondition that makes hashing the field
// safe rather than noisy.
//
// WHY: a wholesale language regression in an extractor was invisible here.
// Measured on this fixture at b0951cb0a, before the tuple changed:
//
//	python  "python"->"zzlang" (2 TagEntitiesLanguage calls + 8 `Language:`
//	        literals): this digest UNCHANGED. Caught only by
//	        TestLanguageTagPersistsAfterFBRoundtrip, which hardcodes
//	        `.py` -> "python" and therefore covers exactly one language.
//	go      "go"->"zzlang" (1 TagEntitiesLanguage call + 6 literals): digest
//	        moved, but at 141/286 — a STRUCTURAL side effect of the token, not
//	        the field being observed. A language change that does not perturb
//	        resolution would have been silent.
//	bicep   `const lang` -> "zzlang": nothing outside internal/extractors/bicep
//	        failed. (Still true after #6862 — this fixture has no .bicep file.)
//	engine  detector.go's three `Language: file.Language` rule-emit sites ->
//	        "zzlang": nothing outside internal/engine failed. This one IS closed
//	        by #6862 and is the mutant scored against the new tuple.
//
// NOTE ON THE OLDER CONSTANTS BELOW. With Language in the tuple, none of the
// pre-#6862 digests can be produced by a graph reversion alone any more — they
// require the tuple to have lost Language as well. Their reversion messages
// still describe the graph state each hash encodes, but a match on any of them
// now means "entityTupleKey no longer hashes Language, AND the graph is back at
// that baseline". The gateOffDigest6742 branch says so directly, because that
// is the one an accidental revert of #6862 alone would hit.
const (
	gateOffDigest6118Base = "a0a6ede910d8d0465d901153f483b27b8a57a565bdd79dd0104bef53786d9ca1"
	gateOffDigest6118     = "387a9c36cb585b4d73828afc997744dbe02e6df347e0860054adf69431996d46"
	gateOffDigest6138     = "8726464ce66a815e8b8a69bf8a9ee272368903f623c161fba81235237e235025"
	gateOffDigest6152     = "774eaec1d5ad5d9731d2c043a6207fdb7b3882b9fdf0f37e4a6b9dbdc555662f"
	gateOffDigest6485     = "3ed5d943639276cd687131418577904de2c0fdcb1de846bd87f3816046f43443"
	gateOffDigest6601     = "d073c83f287b2f6b3b93c0479b7dc55887ee292760b00217ebad1de0ecc0dcdf"
	gateOffDigest6742     = "cb82b3285e410ea566ccdb4a44260cd858c4d6df0c210120e9d00129535edafe"
	gateOffDigest6862     = "3a2e047ca1b7e4325e3b1fe625409be815afd47e80db8464836fee88c8670c4d"
)

func TestCustomExtractorGateOffGraphIsUnchanged(t *testing.T) {
	fixture := writeSpanFixture6118(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	off := persistAndReload(t, runIndexerOn(t, fixture, "span6118", nil))
	got := semanticDigest6118(off)
	if got == gateOffDigest6862 {
		return
	}
	if got == gateOffDigest6742 {
		t.Fatalf("gate-OFF graph hashes to the pre-#6862 tuple — entityTupleKey has " +
			"stopped hashing e.Language. The graph itself is unchanged (142/287); what " +
			"is gone is the digest's ability to see a wholesale language regression, " +
			"which is exactly the hole #6862 measured and closed")
	}
	if got == gateOffDigest6601 {
		t.Fatalf("gate-OFF graph reverted to the pre-#6742 baseline — C# class-hierarchy " +
			"edges have stopped being emitted, so all seven C# classes in this fixture " +
			"are back to having no supertype of any kind: `OrdersController : " +
			"ControllerBase`, `ShopContext : DbContext` and `OrderValidator : " +
			"AbstractValidator` emit no EXTENDS, and `OrderCreatedConsumer : IConsumer`, " +
			"`CreateOrderHandler : IRequestHandler` and `CleanupJob : IJob` emit no " +
			"IMPLEMENTS. Check that attachCsharpHierarchy is still wired into " +
			"internal/extractors/csharp.Extract and that walk() still stashes " +
			"hierarchy_bases on each declaration")
	}
	if got == gateOffDigest6485 {
		t.Fatalf("gate-OFF graph reverted to the pre-#6601 baseline — the C# `using` " +
			"carriers are shipping again as subtype-less SCOPE.Component orphans, so " +
			"`buildImport` has stopped stamping Subtype:\"import\" or the prune " +
			"predicates have stopped selecting on it")
	}
	if got == gateOffDigest6152 {
		t.Fatalf("gate-OFF graph reverted to the pre-#6485 baseline — a Route entity is " +
			"an eligible handler target again, so http:ANY:/orders is resolving to " +
			"Route:/orders and re-synthesising the `/orders → /orders` process")
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
	t.Fatalf("gate-OFF graph changed against ALL pinned digests\n got  %s\n post-#6862 %s\n post-#6742 %s\n post-#6601 %s\n post-#6485 %s\n post-#6152 %s\n post-#6138 %s\n post-#6118 %s\n 2f0175dfc %s\n"+
		"(entities=%d relationships=%d)", got, gateOffDigest6862, gateOffDigest6742,
		gateOffDigest6601, gateOffDigest6485, gateOffDigest6152, gateOffDigest6138,
		gateOffDigest6118, gateOffDigest6118Base, len(off.Entities), len(off.Relationships))
}

// TestSemanticDigestGradesEntityLanguage6862 pins, in behavioural terms rather
// than as a hash, the property #6862 bought: the semantic digest MOVES when an
// extractor re-stamps the language token on every entity it emits.
//
// This is deliberately not "the constant above changed". A pinned constant is a
// diagnostic signature — it moves for any reason and gets re-baselined. This
// test asserts the consequence directly, so it keeps holding across every future
// re-pin, and it fails if entityTupleKey ever drops e.Language again.
//
// It has three parts because there are three independent ways for the tuple to
// stop grading the field:
//
//  1. the format string drops it (unit-level: two entities differing ONLY in
//     Language must not share a tuple key);
//  2. the fixture stops carrying more than one language, which would make the
//     field's presence in the tuple vacuous over this corpus;
//  3. the end-to-end path: re-stamping every entity's language in the loaded
//     gate-OFF graph must change semanticDigest6118. Part 3 is the wholesale
//     regression from the issue, applied in memory so it needs no source mutant.
func TestSemanticDigestGradesEntityLanguage6862(t *testing.T) {
	// Part 1 — the tuple distinguishes on Language alone.
	a := graph.Entity{Kind: "SCOPE.Operation", Name: "greet", QualifiedName: "a.greet",
		Subtype: "func", SourceFile: "a.py", Language: "python", StartLine: 3, EndLine: 5}
	b := a
	b.Language = "zzlang"
	if entityTupleKey(&a) == entityTupleKey(&b) {
		t.Fatalf("entityTupleKey ignores Language: %q == %q (two entities identical "+
			"except for the language token hash to the same content tuple)",
			entityTupleKey(&a), entityTupleKey(&b))
	}

	fixture := writeSpanFixture6118(t)
	t.Setenv("GRAFEL_INPROC_CUSTOM_EXTRACTORS", "")
	off := persistAndReload(t, runIndexerOn(t, fixture, "span6118", nil))

	// Part 2 — the fixture is not language-degenerate.
	langs := map[string]int{}
	for i := range off.Entities {
		if l := off.Entities[i].Language; l != "" {
			langs[l]++
		}
	}
	if len(langs) < 5 {
		t.Fatalf("fixture no longer spans enough languages for the field to be graded "+
			"meaningfully: %d distinct non-empty languages %v (expected the documented "+
			"7: python, csharp, typescript, java, rust, javascript, go)", len(langs), langs)
	}
	tagged := 0
	for _, n := range langs {
		tagged += n
	}
	if tagged < 100 {
		t.Fatalf("only %d/%d entities carry a language at all — a wholesale language "+
			"mutation would barely register in the digest", tagged, len(off.Entities))
	}

	// Part 3 — the wholesale regression moves the digest.
	before := semanticDigest6118(off)
	mutated := *off
	mutated.Entities = append([]graph.Entity(nil), off.Entities...)
	restamped := 0
	for i := range mutated.Entities {
		if mutated.Entities[i].Language == "python" {
			mutated.Entities[i].Language = "zzlang"
			restamped++
		}
	}
	if restamped == 0 {
		t.Fatal("no Python entity in the fixture graph — the wholesale mutation is a no-op")
	}
	if after := semanticDigest6118(&mutated); after == before {
		t.Fatalf("re-stamping the language of all %d Python entities left the semantic "+
			"digest at %s — the digest does not observe entity Language, which is the "+
			"#6862 regression", restamped, before)
	}
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
	// Module->Meta CONTAINS edge that carried it (#6152, 148/291), minus the
	// phantom `SCOPE.Process http:ANY:/orders → /orders` and its five edges
	// (the Route self-IMPLEMENTS, two STEP_IN_PROCESS, one ENTRY_POINT_OF, one
	// Module:_external CONTAINS) once a Route stopped being an eligible handler
	// target (#6485, 147/286; then #6601 dropped the 7 C# import carriers →
	// 142/281; then #6742 added the six C# class-hierarchy edges → 142/287).
	// The six, one per C# file that declares a base type — cs/Startup.cs
	// declares none and adds nothing, which is why it is six and not seven:
	//
	//	OrdersController     -[EXTENDS]->    ControllerBase
	//	ShopContext          -[EXTENDS]->    DbContext
	//	OrderValidator       -[EXTENDS]->    AbstractValidator
	//	OrderCreatedConsumer -[IMPLEMENTS]-> IConsumer
	//	CreateOrderHandler   -[IMPLEMENTS]-> IRequestHandler
	//	CleanupJob           -[IMPLEMENTS]-> IJob
	//
	// Entities stay at 142: every supertype here is external to the fixture, so
	// the six edges dangle on bare names and mint no node. Both real /orders
	// endpoints and both real handler IMPLEMENTS edges are unaffected; see the
	// block comment above the digest constants for the item-by-item delta.
	const (
		wantEntities = 142
		wantRels     = 287
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
