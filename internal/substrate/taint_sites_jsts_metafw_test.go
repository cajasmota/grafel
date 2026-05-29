package substrate

import "testing"

// taint_sites_jsts_metafw_test.go — issue #3186: prove the framework-blind
// JS/TS taint sniffer fires on the meta-framework idioms used by Remix, Nuxt,
// Next.js (Pages- and App-Router) and Angular. These are the proving fixtures
// that justify flipping the taint cells on the respective coverage records to
// `partial`. The detection is intentionally framework-blind: the same regexes
// fire on these primitives regardless of the surrounding framework, so each
// fixture documents WHICH primitive carries the framework's untrusted input.

// helper: count matches of a given kind/category in a sniff result.
func countTaint(ms []TaintMatch, kind TaintKind, cat TaintCategory) int {
	n := 0
	for _, m := range ms {
		if m.Kind == kind && (cat == "" || m.Category == cat) {
			n++
		}
	}
	return n
}

// TestTaintSniffer_JSTS_Remix_ActionFormDataToSQL proves the Remix data-API
// taint chain: an `action` reads untrusted input via `request.formData()` and
// route `params`, then concatenates it into a raw SQL query (SQL-injection
// sink). Both the formData source (#3186) and the params source (#2858) fire.
func TestTaintSniffer_JSTS_Remix_ActionFormDataToSQL(t *testing.T) {
	src := `
import type { ActionFunctionArgs } from "@remix-run/node";

export async function action({ request, params }: ActionFunctionArgs) {
  const form = await request.formData();
  const name = form.get("name");
  const id = params.id;
  // unsafe: tainted name + id flow into a raw SQL concat
  const row = await db.query("UPDATE users SET active = 1 WHERE id = " + id);
  return row;
}
`
	got := sniffTaintJSTS(src)
	if countTaint(got, TaintKindSource, "") < 2 {
		t.Errorf("expected >=2 sources (request.formData + params.id), got %d; all=%+v",
			countTaint(got, TaintKindSource, ""), got)
	}
	if countTaint(got, TaintKindSink, TaintCategorySQL) == 0 {
		t.Errorf("expected a SQL sink for the raw-concat query; all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_Remix_LoaderJSONToExec proves a Remix `loader` reading
// `request.json()` reaching a command-injection sink.
func TestTaintSniffer_JSTS_Remix_LoaderJSONToExec(t *testing.T) {
	src := `
export async function loader({ request }) {
  const payload = await request.json();
  const result = child_process.exec(payload.cmd);
  return result;
}
`
	got := sniffTaintJSTS(src)
	if countTaint(got, TaintKindSource, "") == 0 {
		t.Errorf("expected request.json() source; all=%+v", got)
	}
	if countTaint(got, TaintKindSink, TaintCategoryCommand) == 0 {
		t.Errorf("expected command-injection sink; all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_Nuxt_ReadBodyToSQL proves the Nuxt / Nitro (h3) taint
// chain: a `defineEventHandler` reads untrusted input via `readBody(event)` /
// `getQuery(event)` and concatenates it into a raw SQL query. Before #3186 the
// Nuxt source primitives were undetected (only the sink fired).
func TestTaintSniffer_JSTS_Nuxt_ReadBodyToSQL(t *testing.T) {
	src := `
export default defineEventHandler(async (event) => {
  const body = await readBody(event);
  const q = getQuery(event);
  // unsafe: tainted body.name flows into a raw SQL concat
  const rows = await db.query("SELECT * FROM users WHERE name = " + body.name);
  return { rows, q };
});
`
	got := sniffTaintJSTS(src)
	if countTaint(got, TaintKindSource, "") < 2 {
		t.Errorf("expected >=2 sources (readBody + getQuery), got %d; all=%+v",
			countTaint(got, TaintKindSource, ""), got)
	}
	if countTaint(got, TaintKindSink, TaintCategorySQL) == 0 {
		t.Errorf("expected a SQL sink; all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_Nuxt_RouterParamToFS proves a Nuxt handler reading a
// route param via `getRouterParam(event, ...)` reaching a path-traversal sink.
func TestTaintSniffer_JSTS_Nuxt_RouterParamToFS(t *testing.T) {
	src := `
export default defineEventHandler((event) => {
  const name = getRouterParam(event, "name");
  const data = fs.readFile(name);
  return data;
});
`
	got := sniffTaintJSTS(src)
	if countTaint(got, TaintKindSource, "") == 0 {
		t.Errorf("expected getRouterParam source; all=%+v", got)
	}
	if countTaint(got, TaintKindSink, TaintCategoryPath) == 0 {
		t.Errorf("expected path-traversal sink; all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_NextAPI_PagesRouterReqQueryToXSS proves the Next.js
// Pages-Router API-route chain: `req.query` is the source (already covered by
// jstsSourceReqRe) flowing into a `res.send` XSS sink.
func TestTaintSniffer_JSTS_NextAPI_PagesRouterReqQueryToXSS(t *testing.T) {
	src := `
export default function handler(req, res) {
  const name = req.query.name;
  // unsafe: tainted name echoed straight back without escaping → reflected XSS
  res.send(name);
}
`
	got := sniffTaintJSTS(src)
	if countTaint(got, TaintKindSource, "") == 0 {
		t.Errorf("expected req.query source; all=%+v", got)
	}
	if countTaint(got, TaintKindSink, TaintCategoryXSS) == 0 {
		t.Errorf("expected XSS sink (res.send non-literal); all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_NextAPI_AppRouterRequestJSONToSQL proves the Next.js
// App-Router route-handler chain: the Web Fetch `request.json()` is the source
// (#3186) flowing into a raw SQL sink.
func TestTaintSniffer_JSTS_NextAPI_AppRouterRequestJSONToSQL(t *testing.T) {
	src := `
export async function POST(request: Request) {
  const data = await request.json();
  const row = await db.query("INSERT INTO posts SET title = " + data.title);
  return Response.json(row);
}
`
	got := sniffTaintJSTS(src)
	if countTaint(got, TaintKindSource, "") == 0 {
		t.Errorf("expected request.json() source; all=%+v", got)
	}
	if countTaint(got, TaintKindSink, TaintCategorySQL) == 0 {
		t.Errorf("expected a SQL sink; all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_Angular_InnerHTMLSinkAndSanitizer proves the Angular
// client-side surface: the DOM XSS sink (`.innerHTML =`) and the recognised
// sanitizer (`DOMPurify.sanitize`) both fire. Angular is a browser framework so
// it has no server-side request source — `vulnerability_finding` is therefore
// sink/sanitizer-driven rather than a full source→sink HTTP chain.
func TestTaintSniffer_JSTS_Angular_InnerHTMLSinkAndSanitizer(t *testing.T) {
	src := `
@Component({ selector: "app-note" })
export class NoteComponent {
  constructor(private el: ElementRef) {}

  render(raw: string) {
    // unsafe: raw flows straight into innerHTML → DOM XSS
    this.el.nativeElement.innerHTML = raw;
  }

  renderSafe(raw: string) {
    const clean = DOMPurify.sanitize(raw);
    this.el.nativeElement.innerHTML = clean;
  }
}
`
	got := sniffTaintJSTS(src)
	if countTaint(got, TaintKindSink, TaintCategoryXSS) == 0 {
		t.Errorf("expected innerHTML XSS sink; all=%+v", got)
	}
	if countTaint(got, TaintKindSanitizer, TaintCategoryXSS) == 0 {
		t.Errorf("expected DOMPurify.sanitize sanitizer; all=%+v", got)
	}
}

// TestTaintSniffer_JSTS_MetaFW_NoRegressionExpress confirms the new
// meta-framework source regex ADDS to, and does not displace, the existing
// Express `req.body` source detection.
func TestTaintSniffer_JSTS_MetaFW_NoRegressionExpress(t *testing.T) {
	src := `
function expressHandler(req, res) {
  const q = req.body.q;
  db.query("SELECT * FROM t WHERE x = " + q);
}
`
	got := sniffTaintJSTS(src)
	sawReq := false
	for _, m := range got {
		if m.Kind == TaintKindSource && m.Primitive == "req.body/query/headers" {
			sawReq = true
		}
	}
	if !sawReq {
		t.Errorf("expected Express req.body source to still fire; all=%+v", got)
	}
	if countTaint(got, TaintKindSink, TaintCategorySQL) == 0 {
		t.Errorf("expected SQL sink; all=%+v", got)
	}
}
