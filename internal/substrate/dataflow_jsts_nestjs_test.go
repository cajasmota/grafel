package substrate

import "testing"

// dataflow_jsts_nestjs_test.go — issue #3902 (epic #3872): the JS/TS dataflow
// sniffer must treat NestJS request-decorator parameters (@Body/@Query/@Param/
// @Headers/@Req/@Request) as request-input SOURCES, so DATA_FLOWS_TO fires on
// NestJS controllers (the rewrite target) exactly as it does on Express
// req.body. These tests assert the RESOLVED sink + the request-derived source
// field — not merely len(flows) > 0.

// @Body() dto → repo.save({email: dto.email}) : db_write, field=email.
func TestDataFlowJSTS_NestJS_BodyToDBWrite_MemberField(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto) {
    return this.repo.save({ email: dto.email });
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow create->repo.save, got %+v", flows)
	}
	if got.SinkName != "this.repo.save" {
		t.Errorf("sink = %q, want this.repo.save", got.SinkName)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email (lifted from dto.email)", got.SourceField)
	}
	if got.HopVia != "" {
		t.Errorf("expected intra-fn flow, got hop=%q", got.HopVia)
	}
}

// @Query('q') q → res.json(q) : response, field=q (from decorator key).
func TestDataFlowJSTS_NestJS_QueryToResponse_DecoratorKey(t *testing.T) {
	src := `
@Controller('search')
export class SearchController {
  @Get()
  find(@Query('q') q: string, @Res() res) {
    res.json(q);
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkResponse })
	if got == nil {
		t.Fatalf("expected response flow, got %+v", flows)
	}
	if got.SinkName != "res.json" {
		t.Errorf("sink = %q, want res.json", got.SinkName)
	}
	if got.SourceField != "q" {
		t.Errorf("source field = %q, want q", got.SourceField)
	}
	if got.Function != "find" {
		t.Errorf("function = %q, want find", got.Function)
	}
}

// @Param('id') id → repo.create({id}) read used in a DB call : db_write, field=id.
func TestDataFlowJSTS_NestJS_ParamToDBRead(t *testing.T) {
	src := `
@Controller('items')
export class ItemsController {
  @Get(':id')
  findOne(@Param('id') id: string) {
    return this.repo.create({ id });
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite })
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SinkName != "this.repo.create" {
		t.Errorf("sink = %q, want this.repo.create", got.SinkName)
	}
	if got.SourceField != "id" {
		t.Errorf("source field = %q, want id", got.SourceField)
	}
}

// Multi-line signature (idiomatic NestJS, one decorated param per line) — the
// param block spans lines; @Body() dto must still seed and flow.
func TestDataFlowJSTS_NestJS_MultiLineSignature(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Put(':id')
  update(
    @Param('id') id: string,
    @Body() dto: UpdateUserDto,
  ) {
    return this.repo.update({ name: dto.name });
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "this.repo.update" })
	if got == nil {
		t.Fatalf("expected db_write flow to this.repo.update, got %+v", flows)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name (from dto.name)", got.SourceField)
	}
}

// One-hop: @Body() dto passed bare to a local helper that writes to DB.
func TestDataFlowJSTS_NestJS_OneHop_BodyPassThrough(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto) {
    persist(dto);
  }
}
function persist(d) {
  Model.create(d);
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkName == "Model.create"
	})
	if got == nil {
		t.Fatalf("expected one-hop flow create->persist->Model.create, got %+v", flows)
	}
	if got.HopVia != "persist" {
		t.Errorf("hop_via = %q, want persist", got.HopVia)
	}
}

// Cross-file boundary: @Body('email') x escapes into an imported callee.
func TestDataFlowJSTS_NestJS_Boundary_DecoratorKeyField(t *testing.T) {
	src := `
import { save } from './svc';
@Controller('users')
export class UsersController {
  @Post()
  create(@Body('email') email: string) {
    save(email);
  }
}
`
	res := sniffDataFlowJSTSEx(src)
	if len(res.Boundaries) != 1 {
		t.Fatalf("expected 1 cross-file boundary, got %+v", res.Boundaries)
	}
	b := res.Boundaries[0]
	if b.Callee != "save" || b.ArgIndex != 0 || b.Function != "create" {
		t.Errorf("boundary = %+v, want callee=save arg=0 fn=create", b)
	}
	if b.SourceField != "email" {
		t.Errorf("boundary field = %q, want email (decorator key)", b.SourceField)
	}
}

// Negative: a constructor-injected service param (no request decorator) is NOT
// a source — its members reaching a sink must NOT produce a flow.
func TestDataFlowJSTS_NestJS_Negative_InjectedService(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  constructor(private readonly repo: UserRepo) {}
  @Get()
  list(@Query() q) {
    return this.repo.save({ all: true });
  }
}
`
	// repo is an injected service, not request-derived. The save arg
	// {all: true} carries no request taint → no flow. q is a source but is
	// never used, so still no flow.
	flows := sniffDataFlowJSTSEx(src).Flows
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected NO db_write flow (service not request-derived), got %+v", *got)
	}
}

// Negative: a static value into a NestJS sink → no flow.
func TestDataFlowJSTS_NestJS_Negative_StaticValue(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto) {
    const name = 'static';
    return this.repo.save({ name });
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("expected NO flow for static value, got %+v", *got)
	}
}

// No-regression: an Express req.body handler in the same file still flows
// alongside a NestJS controller.
func TestDataFlowJSTS_NestJS_NoRegressionExpress(t *testing.T) {
	src := `
function expressHandler(req, res) {
  res.json(req.query.q);
}
@Controller('s')
export class SearchController {
  @Get()
  find(@Query('q') q, @Res() res) {
    res.json(q);
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	express := findFlow(flows, func(f DataFlow) bool { return f.Function == "expressHandler" })
	nest := findFlow(flows, func(f DataFlow) bool { return f.Function == "find" })
	if express == nil {
		t.Errorf("Express req.query flow must still fire; got %+v", flows)
	}
	if nest == nil {
		t.Errorf("NestJS @Query flow must fire alongside Express; got %+v", flows)
	}
}
