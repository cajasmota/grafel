package substrate

import "testing"

// dataflow_jsts_nestjs_bodyfield_test.go — issue #3935 (epic #3929): DTO
// field-level taint for NestJS `@Body() dto`. #3902 projects a single member
// access (`dto.email` → field=email); this completes the picture for the
// idiomatic destructuring form `const { name } = dto`, attributing the
// destructured property as the DATA_FLOWS_TO source field. As elsewhere these
// tests assert the RESOLVED field on the specific flow — not len(flows) > 0.

// const { name } = dto; create({ name }) → db_write, field=name.
func TestDataFlowJSTS_NestJS_BodyDestructure_SimpleField(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto) {
    const { name } = dto;
    return this.repo.create({ name });
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow create->repo.create, got %+v", flows)
	}
	if got.SinkName != "this.repo.create" {
		t.Errorf("sink = %q, want this.repo.create", got.SinkName)
	}
	if got.SourceField != "name" {
		t.Errorf("source field = %q, want name (destructured from dto)", got.SourceField)
	}
}

// const { email: e } = dto; save({ e }) → db_write, field=email
// (the SOURCE property name, not the rebound local).
func TestDataFlowJSTS_NestJS_BodyDestructure_RenamedField(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto) {
    const { email: e } = dto;
    return this.repo.save({ e });
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
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email (source prop, not rebound local e)", got.SourceField)
	}
}

// const { name, age } = dto; two sinks → two flows, each carrying the right
// destructured field.
func TestDataFlowJSTS_NestJS_BodyDestructure_MultiField(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto) {
    const { name, age } = dto;
    this.repo.create({ name });
    this.repo.update({ age });
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	byName := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "this.repo.create" })
	byAge := findFlow(flows, func(f DataFlow) bool { return f.SinkName == "this.repo.update" })
	if byName == nil || byName.SourceField != "name" {
		t.Errorf("create flow field = %v, want name; flows=%+v", byName, flows)
	}
	if byAge == nil || byAge.SourceField != "age" {
		t.Errorf("update flow field = %v, want age; flows=%+v", byAge, flows)
	}
}

// @Body('email') email → save({ email }) : the decorator key directly is the
// field (no member access / destructure needed).
func TestDataFlowJSTS_NestJS_BodyDecoratorKey_DirectField(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body('email') email: string) {
    return this.repo.save({ email });
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected db_write flow, got %+v", flows)
	}
	if got.SourceField != "email" {
		t.Errorf("source field = %q, want email (decorator key)", got.SourceField)
	}
}

// Negative (honest-partial): a rest element `...rest` is NOT a static property,
// so a sink fed only by `rest` produces no flow.
func TestDataFlowJSTS_NestJS_BodyDestructure_RestNoFlow(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto) {
    const { name, ...rest } = dto;
    return this.repo.save(rest);
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	if got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite }); got != nil {
		t.Fatalf("rest element is not a static field; expected NO db_write flow, got %+v", *got)
	}
}

// Negative (honest-partial): whole-object pass-through with no member access /
// destructure keeps field="" — unchanged from #3902.
func TestDataFlowJSTS_NestJS_BodyWholeObject_EmptyField(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto) {
    return this.repo.save(dto);
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool {
		return f.Function == "create" && f.SinkKind == DataFlowSinkDBWrite
	})
	if got == nil {
		t.Fatalf("expected whole-object db_write flow, got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("source field = %q, want empty (whole-object pass-through)", got.SourceField)
	}
}

// Negative (honest-partial): a dynamic member `dto[k]` is not a static
// property, so no specific field can be attributed. The taint still reaches
// the sink (the whole object is request-derived) but the field stays "" — we
// never invent a field name from a computed access.
func TestDataFlowJSTS_NestJS_BodyDynamicMember_EmptyField(t *testing.T) {
	src := `
@Controller('users')
export class UsersController {
  @Post()
  create(@Body() dto: CreateUserDto) {
    return this.repo.save(dto[k]);
  }
}
`
	flows := sniffDataFlowJSTSEx(src).Flows
	got := findFlow(flows, func(f DataFlow) bool { return f.SinkKind == DataFlowSinkDBWrite })
	if got == nil {
		t.Fatalf("expected db_write flow (whole object is request-derived), got %+v", flows)
	}
	if got.SourceField != "" {
		t.Errorf("dynamic member dto[k] must not attribute a field; got field=%q", got.SourceField)
	}
}
