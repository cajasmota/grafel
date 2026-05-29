package substrate

import "testing"

// taint_sites_jsts_nestjs_test.go — issue #3163: NestJS decorator-injected
// controller-method parameters (@Body/@Query/@Param/@Headers/@Req/@Request)
// must be recognised as taint sources by the JS/TS sniffer.

// TestTaintSniffer_JSTS_NestJS_DecoratorBodyToSink is the primary proving
// fixture for #3163.  It demonstrates the full @Body → dangerous-sink
// taint flow: the decorator-injected `dto` parameter is a taint source that
// eventually reaches a raw SQL concatenation sink, mirroring the req.body→
// db.query(concat) chain already tested for Express.
func TestTaintSniffer_JSTS_NestJS_DecoratorBodyToSink(t *testing.T) {
	src := `
import { Controller, Post, Body, Get, Query, Param, Headers, Req } from '@nestjs/common';

@Controller('users')
export class UsersController {
  @Post()
  async create(@Body() dto: CreateUserDto) {
    // unsafe: dto is tainted, flowing into a raw SQL concat → SQL injection
    const result = await db.query("INSERT INTO users SET name = " + dto.name);
    return result;
  }
}
`
	got := sniffTaintJSTS(src)
	var sources, sinks int
	for _, m := range got {
		if m.Kind == TaintKindSource && m.Primitive == "@Body/@Query/@Param/@Headers/@Req" {
			sources++
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			sinks++
		}
	}
	if sources == 0 {
		t.Errorf("expected @Body() dto to be flagged as source, got 0; all=%+v", got)
	}
	if sinks == 0 {
		t.Errorf("expected raw SQL concat to be flagged as SQL sink, got 0; all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_NestJS_AllDecorators asserts that all six NestJS
// param decorators are each recognised as taint sources.
func TestTaintSniffer_JSTS_NestJS_AllDecorators(t *testing.T) {
	src := `
@Controller('items')
export class ItemsController {
  @Get(':id')
  async findOne(
    @Param('id') id: string,
    @Query('filter') filter: string,
    @Headers('x-api-key') apiKey: string,
    @Req() req: Request,
    @Request() request: any,
    @Body() body: UpdateItemDto,
  ) {
    const raw = db.query("SELECT * FROM items WHERE id = " + id);
    return raw;
  }
}
`
	got := sniffTaintJSTS(src)
	var sources int
	for _, m := range got {
		if m.Kind == TaintKindSource && m.Primitive == "@Body/@Query/@Param/@Headers/@Req" {
			sources++
		}
	}
	// We have 6 decorated params; expect all 6 to fire.
	if sources < 6 {
		t.Errorf("expected >=6 NestJS decorator sources, got %d; all=%+v", sources, got)
	}
}

// TestTaintSniffer_JSTS_NestJS_QueryParamToExec asserts that a @Query-injected
// parameter reaching an exec sink is detected — the command-injection chain.
func TestTaintSniffer_JSTS_NestJS_QueryParamToExec(t *testing.T) {
	src := `
@Controller('run')
export class RunController {
  @Get()
  async run(@Query('cmd') cmd: string) {
    const result = child_process.exec(cmd);
    return result;
  }
}
`
	got := sniffTaintJSTS(src)
	var sources, sinks int
	for _, m := range got {
		if m.Kind == TaintKindSource && m.Primitive == "@Body/@Query/@Param/@Headers/@Req" {
			sources++
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategoryCommand {
			sinks++
		}
	}
	if sources == 0 {
		t.Errorf("expected @Query('cmd') cmd to be flagged as source; all=%+v", got)
	}
	if sinks == 0 {
		t.Errorf("expected child_process.exec to be flagged as command sink; all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_NestJS_NoRegressionExpressReqBody confirms that
// the existing Express req.body taint source still fires alongside NestJS
// sources in the same file — the NestJS regex must ADD, not replace.
func TestTaintSniffer_JSTS_NestJS_NoRegressionExpressReqBody(t *testing.T) {
	src := `
// Express-style handler in the same codebase.
function expressHandler(req, res) {
  const q = req.body.q;
  db.query("SELECT * FROM t WHERE x = " + q);
}

// NestJS controller in the same file.
@Controller('search')
export class SearchController {
  @Get()
  async search(@Query('q') q: string) {
    db.query("SELECT * FROM t WHERE x = " + q);
  }
}
`
	got := sniffTaintJSTS(src)
	var expressSources, nestjsSources int
	for _, m := range got {
		if m.Kind == TaintKindSource && m.Primitive == "req.body/query/headers" {
			expressSources++
		}
		if m.Kind == TaintKindSource && m.Primitive == "@Body/@Query/@Param/@Headers/@Req" {
			nestjsSources++
		}
	}
	if expressSources == 0 {
		t.Errorf("Express req.body source must still fire (no regression); all=%+v", got)
	}
	if nestjsSources == 0 {
		t.Errorf("expected NestJS @Query source to fire alongside Express req.body; all=%+v", got)
	}
}
