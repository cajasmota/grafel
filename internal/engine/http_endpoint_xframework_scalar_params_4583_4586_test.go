package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Cross-framework scalar request-param extraction (#4583 Express, #4584 FastAPI,
// #4585 Spring, #4586 DRF). Generalises the NestJS scalar @Query/@Param work
// (#4568): each framework's synthesized http_endpoint — the entity the dashboard
// Paths panel reads — must carry one parameter record {name, in, type, required}
// per scalar request param (query / path / header) the handler declares.

// paramByNameIn returns the first decoded JavaParam matching name+in, or nil.
func paramByNameIn(ps []JavaParam, name, in string) *JavaParam {
	for i := range ps {
		if ps[i].Name == name && ps[i].In == in {
			return &ps[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// #4583 — Express / Koa
// ---------------------------------------------------------------------------

func TestExpress_ScalarParams_Surfaced_4583(t *testing.T) {
	src := `const express = require('express');
const router = express.Router();

router.get('/buildings/:id', (req, res) => {
  const id = req.params.id;
  const year = req.query.year;
  const trace = req.headers['x-trace-id'];
  res.json({ id, year, trace });
});

module.exports = router;
`
	_, res := runDetect(t, "javascript", "src/routes/buildings.js", src)
	props := findEndpointProps(res, "GET", "buildings")
	if props == nil {
		t.Fatal("no synthesized GET /buildings/:id endpoint")
	}
	decoded := DecodeJavaParameters(props["parameters"])
	if decoded == nil {
		t.Fatalf("#4583: parameters property empty; props=%v", props)
	}
	if p := paramByNameIn(decoded, "id", "path"); p == nil {
		t.Errorf("#4583: path param 'id' missing: %v", decoded)
	} else if !p.Required {
		t.Errorf("#4583: path param 'id' must be required")
	}
	if p := paramByNameIn(decoded, "year", "query"); p == nil {
		t.Errorf("#4583: query param 'year' missing: %v", decoded)
	}
	if p := paramByNameIn(decoded, "x-trace-id", "header"); p == nil {
		t.Errorf("#4583: header param 'x-trace-id' missing: %v", decoded)
	}
}

// ---------------------------------------------------------------------------
// #4584 — FastAPI
// ---------------------------------------------------------------------------

func TestFastAPI_ScalarParams_Surfaced_4584(t *testing.T) {
	src := `from fastapi import APIRouter, Query, Header

router = APIRouter()

@router.get("/buildings/{id}")
def get_building(id: int, year: str = Query(None), x_trace: str = Header(None)):
    return {"id": id}
`
	_, res := runDetect(t, "python", "app/routers/buildings.py", src)
	props := findEndpointProps(res, "GET", "buildings")
	if props == nil {
		t.Fatal("no synthesized GET /buildings/{id} endpoint")
	}
	decoded := DecodeJavaParameters(props["parameters"])
	if decoded == nil {
		t.Fatalf("#4584: parameters property empty; props=%v", props)
	}
	if p := paramByNameIn(decoded, "id", "path"); p == nil {
		t.Errorf("#4584: path param 'id' missing: %v", decoded)
	} else {
		if !p.Required {
			t.Errorf("#4584: path param 'id' must be required")
		}
		if p.Type != "int" {
			t.Errorf("#4584: id type = %q, want int", p.Type)
		}
	}
	if p := paramByNameIn(decoded, "year", "query"); p == nil {
		t.Errorf("#4584: query param 'year' missing: %v", decoded)
	} else if p.Required {
		t.Errorf("#4584: query param 'year' with Query(None) default must be optional")
	}
	if p := paramByNameIn(decoded, "x_trace", "header"); p == nil {
		t.Errorf("#4584: header param 'x_trace' missing: %v", decoded)
	}
}

// ---------------------------------------------------------------------------
// #4585 — Spring MVC (pre-existing extractor; this is a confirming guard)
// ---------------------------------------------------------------------------

func TestSpring_ScalarParams_Surfaced_4585(t *testing.T) {
	src := `package com.example.api;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/buildings")
public class BuildingController {

    @GetMapping("/{id}")
    public BuildingDto getBuilding(
            @PathVariable("id") Long id,
            @RequestParam("year") int year,
            @RequestHeader("X-Trace-Id") String trace) {
        return service.get(id);
    }
}
`
	_, res := runDetect(t, "java", "src/main/java/com/example/api/BuildingController.java", src)
	props := findEndpointProps(res, "GET", "buildings")
	if props == nil {
		t.Fatal("no synthesized GET /buildings/{id} endpoint")
	}
	decoded := DecodeJavaParameters(props["parameters"])
	if decoded == nil {
		t.Fatalf("#4585: parameters property empty; props=%v", props)
	}
	if p := paramByNameIn(decoded, "id", "path"); p == nil {
		t.Errorf("#4585: path param 'id' missing: %v", decoded)
	} else if !p.Required {
		t.Errorf("#4585: path param 'id' must be required")
	}
	if p := paramByNameIn(decoded, "year", "query"); p == nil {
		t.Errorf("#4585: query param 'year' missing: %v", decoded)
	}
	if p := paramByNameIn(decoded, "X-Trace-Id", "header"); p == nil {
		t.Errorf("#4585: header param 'X-Trace-Id' missing: %v", decoded)
	}
}

// ---------------------------------------------------------------------------
// #4586 — Django REST Framework
// ---------------------------------------------------------------------------

// DRF route synthesis (synthesizeDjangoFromComposed) consumes ast_driven Route
// entities produced by the Django AST composition pass — which the engine-only
// runDetect harness does not run. So we build the synthesized endpoint exactly
// as the live pipeline would (an ast_driven Route → ANY-verb http_endpoint) and
// then exercise applyScalarRequestParams against the view source, which is the
// unit under test for #4586.
func TestDRF_ScalarParams_Surfaced_4586(t *testing.T) {
	const path = "api/urls.py"
	content := `from rest_framework.views import APIView
from rest_framework.response import Response

urlpatterns = [
    path("buildings/<int:pk>/", BuildingDetail.as_view()),
]

class BuildingDetail(APIView):
    def get(self, request, pk):
        year = request.query_params.get('year')
        return Response({"pk": pk, "year": year})
`
	astRoute := []types.EntityRecord{{
		ID:         "ast:Route:/buildings/{pk}",
		Name:       "/buildings/{pk}",
		Kind:       "Route",
		SourceFile: path,
		Language:   "python",
		Properties: map[string]string{"framework": "python", "pattern_type": "ast_driven"},
	}}

	var endpoints []types.EntityRecord
	synthesizeDjangoFromComposed(astRoute, path,
		func(method, canonicalPath, framework, refKind, refName string) {
			props := map[string]string{
				"verb": method, "path": canonicalPath, "framework": framework,
				"pattern_type": "http_endpoint_synthesis",
			}
			endpoints = append(endpoints, types.EntityRecord{
				ID: "http:" + method + ":" + canonicalPath, Name: canonicalPath,
				Kind: httpEndpointDefinitionKind, SourceFile: path,
				Language: "python", Properties: props,
			})
		})
	if len(endpoints) == 0 {
		t.Fatal("no synthesized DRF endpoint")
	}

	applyScalarRequestParams("python", content, path, endpoints, 0)

	var props map[string]string
	for i := range endpoints {
		if containsFold(endpoints[i].Properties["path"], "buildings") {
			props = endpoints[i].Properties
			break
		}
	}
	if props == nil {
		t.Fatal("no /buildings endpoint to inspect")
	}
	decoded := DecodeJavaParameters(props["parameters"])
	if decoded == nil {
		t.Fatalf("#4586: parameters property empty; props=%v", props)
	}
	if p := paramByNameIn(decoded, "pk", "path"); p == nil {
		t.Errorf("#4586: path param 'pk' missing: %v", decoded)
	} else if !p.Required {
		t.Errorf("#4586: path param 'pk' must be required")
	}
	if p := paramByNameIn(decoded, "year", "query"); p == nil {
		t.Errorf("#4586: query param 'year' missing: %v", decoded)
	}
}
