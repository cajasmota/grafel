(function () {
  "use strict";

  var LANGS = ["JS / TS","Python","Java","Go","C#","C / C++","Kotlin","Rust","PHP","Ruby","Swift","Scala","Elixir","Erlang","Clojure","Haskell","Dart","Lua","F#","OCaml","ReasonML","ReScript","Elm","Groovy","Nim","Crystal","Pony","Zig","Solidity","Verilog","VHDL","Assembly","COBOL","JCL","Idris","Lisp","Standard ML"];

  var FW_BARS = [["JS / TS",33],["C / C++",25],["Python",25],["Java",23],["Go",21],["C#",18],["Kotlin",18],["Rust",17],["PHP",16],["Elixir",14],["Scala",14],["Ruby",9]];
  var FW_MAX = 33;

  var INFRA = [["Platform / k8s",39],["Message brokers",22],["Observability",13],["Databases",12],["CI / CD",12],["Protocols",12],["Security",11],["Build systems",4]];

  // Full authoritative kind lists, sourced from internal/types/kinds.go
  // AllEntityKinds() (60) and AllRelationshipKinds() (106) — every kind grafel
  // extractors are permitted to emit, grouped by category. Display forms strip
  // the "SCOPE." prefix and lowercase to match the on-disk / MCP-rendered form.
  var ENT_GROUPS = [
    { label: "Code structure", items: ["operation","component","class","function","schema","variable","reference","constant","enum","constraint"] },
    { label: "Web & API", items: ["endpoint","route","http_endpoint_definition","http_endpoint_call","grpc_service","grpc_method","external_api","command","custom_validator"] },
    { label: "Frontend & views", items: ["view","ui_component","jsx","stylesheet","template"] },
    { label: "Data & persistence", items: ["datastore","table","model","data_access","data_loader","model_event"] },
    { label: "Messaging & async", items: ["queue","event","message_topic","channel","event_bus_event"] },
    { label: "Infra & config", items: ["infra_resource","config","feature_flag","plugin","serverless_function","project"] },
    { label: "Service & cross-cutting", items: ["service","external_service","exception_type","translation_key","package","module","state"] },
    { label: "Docs, patterns & analysis", items: ["document","heading","code_block","scope_unknown","external","pattern","evolution","agent_pattern","markdown_document","section","design_decision"] }
  ];
  var REL_GROUPS = [
    { label: "Structural", items: ["calls","imports","extends","implements","uses","uses_hook","contains","depends_on","references","has_props","has_type","typed_as","inherits"] },
    { label: "Web, routing & API", items: ["routes_to","serves","renders","returns","accepts_input","tagged_as","fetches","grpc_implements","grpc_handles","handles","handles_command","unresolved_fetch","navigates_to","validates","consumes_api"] },
    { label: "Data & persistence", items: ["accesses_table","reads_from","writes_to","queries","joins_collection","graph_relates","reads_field","writes_field","resolves_to","shares_data","shares_table_with","precedes","modifies_table","maps_to"] },
    { label: "Messaging & events", items: ["publishes_to","subscribes_to","transforms","batches","triggers","eventbridge_triggers","eventgrid_triggers","cloudevent_flows","captures"] },
    { label: "Real-time & streaming", items: ["ws_subscribes_to","ws_emits","ws_connects","streams_from","streams_to","graphql_subscribes","graphql_publishes","joins_channel","broadcasts_to"] },
    { label: "Data-flow & control analysis", items: ["injected_into","discriminates_on","branches_on","data_flows_to","gated_by","transitions_to","throws","catches"] },
    { label: "Infra, build & deployment", items: ["registers","depends_on_config","configures","bazel_depends_on","bazel_dep_status","mage_depends_on","task_depends_on","patches","binds","includes","overrides","instruments","caches","invalidates","registers_plugin","federates","depends_on_service","depends_on_package","uses_translation","instantiates","supervises","deploys"] },
    { label: "Docs, patterns & lifecycle", items: ["tests","exemplar","touches","anti_exemplar","supersedes","conflicts_with","co_applies_with","prerequisite","created_by","platform_variant_of","renamed_from","handles_signal","resolved_by","uses_schema","mentions","rationale_for"] }
  ];

  var $ = function (id) { return document.getElementById(id); };

  /* coverage matrix + legend fills */
  (function fillStatic() {
    var lc = $("langchips");
    LANGS.forEach(function (l) { var s = document.createElement("span"); s.className = "chip"; s.textContent = l; lc.appendChild(s); });

    var fb = $("fwbars");
    FW_BARS.forEach(function (r) {
      var row = document.createElement("div"); row.className = "barrow";
      row.innerHTML = '<span class="bl">' + r[0] + '</span><span class="bt"><span data-w="' + Math.round(r[1] / FW_MAX * 100) + '"></span></span><span class="bn">' + r[1] + '</span>';
      fb.appendChild(row);
    });
    setTimeout(function () { fb.querySelectorAll(".bt span").forEach(function (s) { s.style.width = s.dataset.w + "%"; }); }, 120);

    var inf = $("infra");
    INFRA.forEach(function (r) { var d = document.createElement("div"); d.className = "infra-chip"; d.innerHTML = '<span class="c">' + r[0] + '</span><span class="n2">' + r[1] + '</span>'; inf.appendChild(d); });

    function fillKindGroups(container, groups, cls) {
      groups.forEach(function (g) {
        var wrap = document.createElement("div"); wrap.className = "kind-group";
        var lab = document.createElement("div"); lab.className = "kind-group-label"; lab.textContent = g.label;
        wrap.appendChild(lab);
        var list = document.createElement("div"); list.className = "kind-list";
        g.items.forEach(function (k) { var s = document.createElement("span"); s.className = "kv " + cls; s.textContent = k; list.appendChild(s); });
        wrap.appendChild(list);
        container.appendChild(wrap);
      });
    }
    fillKindGroups($("entkinds"), ENT_GROUPS, "ent");
    fillKindGroups($("relkinds"), REL_GROUPS, "rel");
  })();
})();
