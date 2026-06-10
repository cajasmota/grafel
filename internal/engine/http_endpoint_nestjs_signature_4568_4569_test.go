package engine

import (
	"testing"
)

// #4568 / #4569 — the synthesized NestJS http_endpoint (the entity the dashboard
// Paths panel reads) must carry the handler SIGNATURE: scalar @Query/@Param/
// @Headers params (Parameters table) and the unwrapped return-type DTO
// (Response shape). Before the fix synthesizeNestJS stamped only the route/verb,
// so the live upvate-v3 dashboard showed Parameters (0)/None and Response (none).

// findEndpointProps returns the Properties of the first synthesized
// http_endpoint whose route_path/verb match, or nil.
func findEndpointProps(res *DetectResult, verb, routeContains string) map[string]string {
	for _, e := range res.Entities {
		if e.Kind != httpEndpointKind && e.Kind != httpEndpointDefinitionKind {
			continue
		}
		p := e.Properties
		if p == nil {
			continue
		}
		if !containsFold(p["verb"]+p["http_method"], verb) {
			continue
		}
		if routeContains != "" && !containsFold(p["route_path"]+p["path"], routeContains) {
			continue
		}
		return p
	}
	return nil
}

func containsFold(hay, needle string) bool {
	if needle == "" {
		return true
	}
	return len(hay) >= len(needle) && indexFold(hay, needle) >= 0
}

func indexFold(s, sub string) int {
	ls, lsub := len(s), len(sub)
	for i := 0; i+lsub <= ls; i++ {
		match := true
		for j := 0; j < lsub; j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// #4568 — a handler with a scalar @Query('year') param surfaces a query
// parameter on the synthesized endpoint's `parameters` property.
func TestNestJS_ScalarQueryParam_Surfaced_4568(t *testing.T) {
	src := `import { Controller, Get, Query } from '@nestjs/common';
import { ProposalCountsResponse } from '../dto/response/proposal-counts.response.dto';

@Controller('proposals')
export class ProposalController {
  @Get('get_counts')
  getCounts(@Query('year') yearRaw: string): Promise<ProposalCountsResponse> {
    return this.svc.getCounts(yearRaw);
  }
}
`
	_, res := runDetect(t, "typescript", "src/modules/proposals/api/proposal.controller.ts", src)
	props := findEndpointProps(res, "GET", "get_counts")
	if props == nil {
		t.Fatal("no synthesized GET /proposals/get_counts endpoint")
	}
	pj := props["parameters"]
	if pj == "" {
		t.Fatalf("#4568: parameters property empty; want the scalar @Query('year') param. props=%v", props)
	}
	decoded := DecodeJavaParameters(pj)
	var found bool
	for _, p := range decoded {
		if p.Name == "year" && p.In == "query" {
			found = true
			if p.Type != "string" {
				t.Errorf("#4568: year param type = %q, want string", p.Type)
			}
		}
	}
	if !found {
		t.Errorf("#4568: scalar query param 'year' not in parameters: %v", decoded)
	}
}

// #4568 — scalar @Param('id', ParseIntPipe) path param surfaces as required path.
func TestNestJS_ScalarPathParam_Surfaced_4568(t *testing.T) {
	src := `import { Controller, Get, Param } from '@nestjs/common';

@Controller('buildings')
export class BuildingController {
  @Get(':id')
  findOne(@Param('id', ParseIntPipe) id: number): Promise<BuildingDto> {
    return this.svc.findOne(id);
  }
}
`
	_, res := runDetect(t, "typescript", "src/modules/buildings/api/building.controller.ts", src)
	props := findEndpointProps(res, "GET", "buildings")
	if props == nil {
		t.Fatal("no synthesized GET /buildings/:id endpoint")
	}
	decoded := DecodeJavaParameters(props["parameters"])
	var found bool
	for _, p := range decoded {
		if p.Name == "id" && p.In == "path" {
			found = true
			if !p.Required {
				t.Errorf("#4568: path param 'id' must be required")
			}
			if p.Type != "number" {
				t.Errorf("#4568: id param type = %q, want number", p.Type)
			}
		}
	}
	if !found {
		t.Errorf("#4568: scalar path param 'id' not surfaced: %v", decoded)
	}
}

// #4569 — a Promise<DTO> return type stamps response_type with the unwrapped DTO
// name on the synthesized endpoint so the Response row can resolve+render it.
func TestNestJS_PromiseDTOResponseType_Stamped_4569(t *testing.T) {
	src := `import { Controller, Get, Query } from '@nestjs/common';
import { ProposalCountsResponse } from '../dto/response/proposal-counts.response.dto';

@Controller('proposals')
export class ProposalController {
  @Get('get_counts')
  getCounts(@Query('year') yearRaw: string): Promise<ProposalCountsResponse> {
    return this.svc.getCounts(yearRaw);
  }
}
`
	_, res := runDetect(t, "typescript", "src/modules/proposals/api/proposal.controller.ts", src)
	props := findEndpointProps(res, "GET", "get_counts")
	if props == nil {
		t.Fatal("no synthesized GET /proposals/get_counts endpoint")
	}
	if got := props["response_type"]; got != "ProposalCountsResponse" {
		t.Errorf("#4569: response_type = %q, want ProposalCountsResponse. props=%v", got, props)
	}
}

// #4569 — Promise<DTO[]> flags the array payload while keeping the element DTO.
func TestNestJS_PromiseArrayDTOResponseType_4569(t *testing.T) {
	src := `import { Controller, Get } from '@nestjs/common';

@Controller('inspectors')
export class InspectorController {
  @Get()
  list(): Promise<InspectorDto[]> {
    return this.svc.list();
  }
}
`
	_, res := runDetect(t, "typescript", "src/modules/inspectors/api/inspector.controller.ts", src)
	props := findEndpointProps(res, "GET", "inspectors")
	if props == nil {
		t.Fatal("no synthesized GET /inspectors endpoint")
	}
	if got := props["response_type"]; got != "InspectorDto" {
		t.Errorf("#4569: response_type = %q, want InspectorDto", got)
	}
	if props["response_is_array"] != "true" {
		t.Errorf("#4569: response_is_array must be true for Promise<InspectorDto[]>")
	}
}
