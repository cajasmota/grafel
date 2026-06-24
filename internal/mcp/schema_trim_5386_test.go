package mcp

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// newEmptyRegistryServer builds a Server against an empty registry — the same
// construction cmd/mcp-audit uses. It exercises registerTools() (all 68 tools)
// without needing any indexed group, so the registered tool surface can be
// asserted directly off s.MCP.ListTools().
func newEmptyRegistryServer(t *testing.T) *Server {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "reg-*.json")
	if err != nil {
		t.Fatalf("temp registry: %v", err)
	}
	if _, err := tmp.WriteString(`{"groups":{}}`); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	tmp.Close()
	srv, err := NewServer(Config{RegistryPath: tmp.Name()})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

// TestNoDefaultAnnotationHints guards the #5386 token trim: addTool strips the
// blanket annotation hints that mcp-go's NewTool stamps on every tool by
// default (readOnlyHint=false/destructiveHint=true/idempotentHint=false/
// openWorldHint=true). Those hints are inaccurate (read-only query tools were
// advertised destructive) and identical across all 68 tools, costing ~1.5k
// tokens in the per-connect tools/list payload. After the trim no registered
// tool should ship that default block — its Annotations must be empty (all
// pointer fields nil, no Title).
func TestNoDefaultAnnotationHints(t *testing.T) {
	srv := newEmptyRegistryServer(t)
	for name, st := range srv.MCP.ListTools() {
		a := st.Tool.Annotations
		if isDefaultAnnotation(a) {
			t.Errorf("tool %q still ships mcp-go's default annotation hint block — addTool should have stripped it", name)
		}
		// The expected post-trim state is a fully empty annotation: it
		// serializes to "{}" instead of the ~89-char default hint block.
		if a.Title != "" || a.ReadOnlyHint != nil || a.DestructiveHint != nil ||
			a.IdempotentHint != nil || a.OpenWorldHint != nil {
			b, _ := json.Marshal(a)
			t.Errorf("tool %q: expected empty annotations, got %s", name, string(b))
		}
	}
}

// TestToolParamNamesPreserved is a golden-snapshot guard that the SAFE schema
// trim changed only descriptive/boilerplate JSON — NOT the accepted parameter
// surface. It asserts the exact set of declared inputSchema property names per
// tool (plus the required-set). If a future edit adds/removes/renames a param,
// this test fails loudly so the change is deliberate. The trim itself must
// leave every entry below untouched.
func TestToolParamNamesPreserved(t *testing.T) {
	srv := newEmptyRegistryServer(t)

	got := map[string]string{} // tool -> "p1,p2|req:r1,r2"
	for name, st := range srv.MCP.ListTools() {
		raw, err := json.Marshal(st.Tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema for %q: %v", name, err)
		}
		var sch struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(raw, &sch); err != nil {
			t.Fatalf("unmarshal schema for %q: %v", name, err)
		}
		props := make([]string, 0, len(sch.Properties))
		for p := range sch.Properties {
			props = append(props, p)
		}
		sort.Strings(props)
		req := append([]string(nil), sch.Required...)
		sort.Strings(req)
		got[name] = strings.Join(props, ",") + "|req:" + strings.Join(req, ",")
	}

	for name, want := range wantToolParams {
		if g, ok := got[name]; !ok {
			t.Errorf("tool %q missing from registered set", name)
		} else if g != want {
			t.Errorf("tool %q param surface changed:\n  want %s\n  got  %s", name, want, g)
		}
	}
	for name := range got {
		if _, ok := wantToolParams[name]; !ok {
			t.Errorf("unexpected new tool %q (param surface %s) — add it to wantToolParams if intentional", name, got[name])
		}
	}
}

// wantToolParams is the golden snapshot of every registered tool's declared
// inputSchema property names (sorted, comma-joined) and required-set, captured
// at the time of the #5386 schema trim. The trim removed only annotation
// boilerplate, so this surface must remain identical. Regenerate deliberately
// if the param set legitimately changes.
var wantToolParams = map[string]string{
	"grafel_apply_doc_semantics":         "cwd,dry_run,group,repo_filter|req:",
	"grafel_apply_docgen_repairs":        "cwd,dry_run,group,repo_filter|req:",
	"grafel_auth_coverage":               "cwd,format,group,limit,only_missing,ref,repo_filter,token_budget,verbose|req:",
	"grafel_auth_posture_diff":           "group_oracle,group_v3|req:group_oracle,group_v3",
	"grafel_clusters":                    "cwd,group,min_size,ref,repo_filter,top_entities_limit|req:",
	"grafel_contract_test_effectiveness": "cwd,group|req:",
	"grafel_control_flow":                "cwd,detail,entity_id,group,repo_filter|req:entity_id",
	"grafel_coverage_effectiveness":      "cwd,group,ineffective_only,limit,ref,repo_filter|req:",
	"grafel_cross_links":                 "action,cwd,group|req:action",
	"grafel_data_flows":                  "cwd,entity_id,group,limit,repo_filter,sink_kind|req:",
	"grafel_dead_code":                   "cwd,from,group,kind_filter,limit,ref,repo_filter|req:",
	"grafel_def_use":                     "cwd,entity_id,group,limit,repo_filter|req:",
	"grafel_diff_refs":                   "cwd,group,ref_a,ref_b,repo|req:ref_a,ref_b,repo",
	"grafel_docgen_abort":                "cwd,group,run_id|req:run_id",
	"grafel_docgen_list":                 "cwd,group|req:group",
	"grafel_docgen_promote":              "cwd,force,group,run_id|req:run_id",
	"grafel_docgen_start_run":            "cwd,group,no_git,resume|req:group",
	"grafel_docgen_status":               "cwd,no_git,run_id|req:run_id",
	"grafel_docgen_validate":             "cwd,no_git,run_id|req:run_id",
	"grafel_effective_contract":          "cwd,entity_id,group,qualified_name,ref,repo_filter|req:entity_id",
	"grafel_effects":                     "cwd,entity_id,group,include,repo_filter|req:entity_id",
	"grafel_endpoint_posture":            "cwd,entity_id,facet,group,method,path_contains,repo_filter|req:",
	"grafel_endpoints":                   "action,cwd,detail,effect,entity_id,facet,format,group,include_navigation,kind,limit,method,offset,orphan_only,path_contains,qualified_name,ref,repo_filter,token_budget|req:",
	"grafel_enrichments":                 "action,candidate_id,confidence,cwd,group,kind,limit,reason,repo_filter,value|req:action",
	"grafel_expand":                      "cwd,depth,entity_id,fields,group,node,ref,repo_filter,token_budget|req:entity_id",
	"grafel_feedback_event":              "capability,group,library,note,outcome,phase|req:outcome",
	"grafel_find":                        "context_filter,cross_repo,cwd,depth,fields,full,group,include_noise,kind_filter,min_confidence,mode,query,ref,repo_filter,search,token_budget|req:query",
	"grafel_find_callees":                "cwd,depth,entity_id,group,ref,token_budget|req:entity_id",
	"grafel_find_callers":                "cwd,depth,entity_id,group,ref,token_budget|req:entity_id",
	"grafel_find_dead_code":              "cwd,group,kind_filter,limit,min_confidence,ref,repo_filter|req:",
	"grafel_find_paths":                  "cwd,from,group,max_hops,ref,to|req:from,to",
	"grafel_flows":                       "action,cwd,group,process_id,repo_filter|req:action",
	"grafel_get_source":                  "context_lines,cwd,entity_id,from_line,group,to_line|req:entity_id",
	"grafel_graph_patterns":              "action,confidence_min,cwd,group,limit,needs_attention,pattern_id,repo_filter,status|req:action",
	"grafel_impact_radius":               "base,cwd,entity_id,group,head,hops,ref,refs,repo,scope|req:",
	"grafel_import_cycles":               "cwd,group,limit,min_size,repo_filter|req:",
	"grafel_inspect":                     "cwd,entity_id,fields,group,include,include_unresolved,min_confidence,ref,repo_filter|req:entity_id",
	"grafel_license_audit":               "cwd,group|req:",
	"grafel_list_findings":               "cwd,group|req:",
	"grafel_literal_parity":              "group_oracle,group_v3,set|req:group_oracle,group_v3,set",
	"grafel_index_status":                "group,repo|req:",
	"grafel_mcp_metrics":                 "days|req:",
	"grafel_module_analysis":             "action,cwd,group,ref|req:",
	"grafel_navigates":                   "cwd,direction,entity_id,group,limit,max_depth,mode,repo_filter,route,with_param|req:",
	"grafel_neighbors":                   "cwd,depth,direction,entity_id,fields,group,ref,token_budget|req:entity_id",
	"grafel_orient":                      "cwd,group,max_questions,ref,repo_filter,top_edges,top_entities,topic_id,view|req:",
	"grafel_related":                     "cwd,depth,direction,entity_id,fields,group,ref,repo_filter,token_budget|req:entity_id",
	"grafel_patterns":                    "action,category,cwd,exemplars,group,kind,limit,repo_filter,steps,text|req:",
	"grafel_payload_drift":               "cwd,drift_class,group|req:",
	"grafel_persona_event":               "chain,depth,event_type,metadata,persona,target_persona|req:event_type,persona",
	"grafel_pr_impact":                   "base,cwd,group,head,hops,refs,repo|req:repo",
	"grafel_pure_functions":              "cwd,group,limit,repo_filter|req:",
	"grafel_quality_cycles":              "cwd,group,limit,ref,repo_filter|req:",
	"grafel_repairs":                     "action,cwd,group,limit,offset,repo_filter|req:action",
	"grafel_response_shape_diff":         "group_oracle,group_v3|req:group_oracle,group_v3",
	"grafel_save_finding":                "answer,cwd,group,question|req:answer,question",
	"grafel_search_entities":             "cwd,fields,format,group,include_noise,kind_filter,limit,min_confidence,query,repo_filter,token_budget|req:query",
	"grafel_secrets":                     "cwd,group,limit,severity|req:",
	"grafel_security_findings":           "category,cwd,group,limit,min_confidence,source_repo|req:",
	"grafel_stats":                       "breakdown,cwd,group,ref,repo_filter|req:",
	"grafel_status":                      "|req:",
	"grafel_stub_detector":               "group_oracle,group_v3|req:group_oracle,group_v3",
	"grafel_subgraph":                    "cwd,depth,entity_id,fields,format,group,max_nodes,mode,token_budget|req:entity_id",
	"grafel_template_patterns":           "cwd,group,kind,limit,repo_filter|req:",
	"grafel_test_coverage":               "cwd,entity_id,group,limit,ref,repo_filter,severity,top_directories|req:",
	"grafel_test_reachability":           "cwd,endpoints_only,entity_id,group,limit,ref,repo_filter,untested_only|req:",
	"grafel_topology":                    "action,cwd,group,repo_filter,topic_id|req:action",
	"grafel_trace":                       "action,cwd,detail,entity_id,group,include,kind,limit,ref,repo_filter,sink_kind,source,target|req:",
	"grafel_traces":                      "action,cwd,entry_point_id,group,limit,max_depth,process_id,ref,repo_filter,token_budget|req:",
	"grafel_whoami":                      "cwd,group,ref|req:",
	// #5546/#5550 ANALYSIS-cluster canonical tools.
	"grafel_debt":          "cwd,group,group_oracle,group_v3,kind,kind_filter,limit,ref,repo_filter|req:",
	"grafel_security":      "category,cwd,format,group,kind,limit,min_confidence,ref,repo_filter,severity,source_repo|req:",
	"grafel_test_analysis": "cwd,entity_id,group,kind,limit,ref,repo_filter,severity|req:",
	"grafel_findings":      "action,answer,cwd,group,question|req:",
	"grafel_diff":          "aspect,cwd,drift_class,group,group_oracle,group_v3,ref_a,ref_b,repo,set|req:",
}
