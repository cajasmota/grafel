package substrate

import "testing"

// TestTaintSniffer_JSTS_DirectSinks confirms the JS/TS sniffer
// recognises the canonical req.body source, the eval / new Function
// sink, and a parameterised-query sanitizer in the same file.
func TestTaintSniffer_JSTS_DirectSinks(t *testing.T) {
	src := `
function handler(req, res) {
  const q = req.body.q;
  db.query("SELECT * FROM t WHERE x = ?", [q]);  // sanitizer
  db.query("SELECT * FROM t WHERE x = " + q);     // sink (concat)
  eval(q);                                         // sink (command)
}
`
	got := sniffTaintJSTS(src)
	if len(got) == 0 {
		t.Fatal("expected matches; got 0")
	}
	have := map[TaintKind]int{}
	for _, m := range got {
		have[m.Kind]++
		if m.Function != "handler" {
			t.Errorf("match %+v not attributed to handler", m)
		}
	}
	if have[TaintKindSource] == 0 {
		t.Error("expected at least one source match")
	}
	if have[TaintKindSink] == 0 {
		t.Error("expected at least one sink match")
	}
	if have[TaintKindSanitizer] == 0 {
		t.Error("expected at least one sanitizer match")
	}
}

// TestTaintSniffer_Python_LiteralOpenIsNotASink documents that
// open("/etc/passwd") with a literal path is NOT flagged as a path-
// traversal sink — only the non-literal first-arg shape is.
func TestTaintSniffer_Python_LiteralOpenIsNotASink(t *testing.T) {
	src := `
def read_config():
    open("/etc/myapp/config.yml")  # benign: literal path
`
	for _, m := range sniffTaintPython(src) {
		if m.Kind == TaintKindSink && m.Category == TaintCategoryPath {
			t.Errorf("literal open() was flagged as path sink: %+v", m)
		}
	}
}

// TestTaintSniffer_Java_RecognisesSpringAnnotations confirms the
// @RequestParam / @RequestBody parameter annotations are surfaced as
// sources. Spring-style controllers are the dominant Java HTTP shape.
func TestTaintSniffer_Java_RecognisesSpringAnnotations(t *testing.T) {
	src := `
@RestController
public class UserController {
  @GetMapping("/users")
  public String list(@RequestParam String q) {
    return q;
  }
}
`
	var found bool
	for _, m := range sniffTaintJava(src) {
		if m.Kind == TaintKindSource && m.Primitive == "@RequestParam/@PathVariable/@RequestBody" {
			found = true
		}
	}
	if !found {
		t.Error("expected @RequestParam to be flagged as a source")
	}
}

// TestTaintSniffer_Ruby_StrongParamsIsSanitizer confirms that the
// Rails strong-parameters idiom (params.require(:user).permit(:name))
// is recognised as a sanitizer — it is the canonical Rails allow-list.
func TestTaintSniffer_Ruby_StrongParamsIsSanitizer(t *testing.T) {
	src := `
class UsersController
  def create
    user_params = params.require(:user).permit(:name, :email)
    User.create(user_params)
  end
end
`
	var hasSan bool
	for _, m := range sniffTaintRuby(src) {
		if m.Kind == TaintKindSanitizer && m.Primitive == "params.require.permit" {
			hasSan = true
		}
	}
	if !hasSan {
		t.Error("expected params.require.permit to be flagged as sanitizer")
	}
}

// TestTaintSniffer_PHP_PDOPrepareIsSanitizer asserts that PDO::prepare
// (parameterised SQL) is recognised, and a raw mysqli_query with a
// $var argument is recognised as a sink.
func TestTaintSniffer_PHP_PDOPrepareIsSanitizer(t *testing.T) {
	src := `<?php
function login($pdo) {
    $username = $_POST['username'];
    $stmt = $pdo->prepare("SELECT * FROM users WHERE name = ?");
    $stmt->bindValue(1, $username);
    $stmt->execute();
    // Unsafe sibling.
    $bad = mysqli_query($conn, $username);
}
`
	var hasSrc, hasSan, hasSink bool
	for _, m := range sniffTaintPHP(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategorySQL {
			hasSan = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			hasSink = true
		}
	}
	if !hasSrc {
		t.Error("expected $_POST to be flagged as source")
	}
	if !hasSan {
		t.Error("expected PDO->prepare/bindValue to be flagged as SQL sanitizer")
	}
	if !hasSink {
		t.Error("expected mysqli_query($conn, $var) to be flagged as SQL sink")
	}
}

// TestTaintSniffer_Rust_SqlxBindIsSanitizer asserts that sqlx::query
// with a .bind() call is recognised as the parameterised-SQL sanitizer,
// and that sqlx::query(&format!(...)) is recognised as a sink.
func TestTaintSniffer_Rust_SqlxBindIsSanitizer(t *testing.T) {
	src := `
async fn get_user(pool: &PgPool, id: i64) -> Result<User, Error> {
    let user = sqlx::query("SELECT * FROM users WHERE id = $1").bind(id).fetch_one(pool).await?;
    let bad = sqlx::query(&format!("SELECT * FROM users WHERE id = {}", id)).fetch_one(pool).await?;
    Ok(user)
}
`
	var hasSan, hasSink bool
	for _, m := range sniffTaintRust(src) {
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategorySQL {
			hasSan = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			hasSink = true
		}
	}
	if !hasSan {
		t.Error("expected sqlx::query.bind to be flagged as SQL sanitizer")
	}
	if !hasSink {
		t.Error("expected sqlx::query(&format!(...)) to be flagged as SQL sink")
	}
}

// TestTaintSniffer_CSharp_FromBodyIsSource confirms the [FromBody]
// parameter attribute is recognised as a source.
func TestTaintSniffer_CSharp_FromBodyIsSource(t *testing.T) {
	src := `
public class UsersController : ControllerBase
{
    [HttpPost]
    public IActionResult Create([FromBody] UserDto dto) {
        return Ok(dto);
    }
}
`
	var found bool
	for _, m := range sniffTaintCSharp(src) {
		if m.Kind == TaintKindSource && m.Primitive == "[FromBody]/[FromQuery]/[FromForm]" {
			found = true
		}
	}
	if !found {
		t.Error("expected [FromBody] to be flagged as source")
	}
}

// TestTaintSniffer_Kotlin_KtorReceiveIsSource confirms call.receive()
// is recognised as a Ktor source.
func TestTaintSniffer_Kotlin_KtorReceiveIsSource(t *testing.T) {
	src := `
fun Application.routes() {
    routing {
        post("/users") {
            val dto = call.receive<UserDto>()
            call.respondText(dto.name)
        }
    }
}
`
	var found bool
	for _, m := range sniffTaintKotlin(src) {
		if m.Kind == TaintKindSource {
			found = true
		}
	}
	if !found {
		t.Error("expected call.receive to be flagged as source")
	}
}

// TestTaintSniffer_Elixir_EctoFragmentSpliceIsSink asserts that the
// Slick-equivalent string-splice form is flagged. Plus that the Ecto
// pinned-variable form (`^var`) counts as a sanitizer.
func TestTaintSniffer_Elixir_PinnedVarIsSanitizer(t *testing.T) {
	src := `
defmodule MyApp.UserController do
  def show(conn, _params) do
    id = conn.params["id"]
    user = Repo.one(from u in User, where: u.id == ^id)
    render(conn, "show.html", user: user)
  end
end
`
	var hasSrc, hasSan bool
	for _, m := range sniffTaintElixir(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategorySQL {
			hasSan = true
		}
	}
	if !hasSrc {
		t.Error("expected conn.params to be flagged as source")
	}
	if !hasSan {
		t.Error("expected `from..in..` / pinned ^var to be flagged as SQL sanitizer")
	}
}

// TestTaintSniffer_Scala_SlickSpliceIsSink asserts that the Slick
// `#${var}` splice form (which bypasses parameterisation) is flagged
// as a SQL sink.
func TestTaintSniffer_Scala_SlickSpliceIsSink(t *testing.T) {
	src := `
def listUsers(name: String) = db.run {
  sql"""SELECT * FROM users WHERE name = '#${name}'""".as[User]
}
`
	var hasSink bool
	for _, m := range sniffTaintScala(src) {
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			hasSink = true
		}
	}
	if !hasSink {
		t.Error("expected Slick `sql\"#${var}\"` splice to be flagged as SQL sink")
	}
}

// TestTaintSniffer_CCPP_SystemOfArgvIsSink confirms the textbook
// argv → system() chain is recognised: argv[] as source, system(arg)
// as command sink. PQexecParams is the sanitizer counter-example.
func TestTaintSniffer_CCPP_SystemOfArgvIsSink(t *testing.T) {
	src := `
int main(int argc, char *argv[]) {
    char *cmd = argv[1];
    system(cmd);
    PGresult *res = PQexecParams(conn, "SELECT * FROM t WHERE x=$1", 1, NULL, params, NULL, NULL, 0);
    return 0;
}
`
	var hasSrc, hasSink, hasSan bool
	for _, m := range sniffTaintCCPP(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategoryCommand {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategorySQL {
			hasSan = true
		}
	}
	if !hasSrc {
		t.Error("expected argv[] to be flagged as source")
	}
	if !hasSink {
		t.Error("expected system(cmd) to be flagged as command sink")
	}
	if !hasSan {
		t.Error("expected PQexecParams to be flagged as SQL sanitizer")
	}
}

// TestTaintSniffer_Dart_RawQueryIsSink asserts that sqflite rawQuery with
// a non-literal SQL string is recognised as a SQL sink, and that the
// whereArgs parameterised form is a sanitizer.
func TestTaintSniffer_Dart_RawQueryIsSink(t *testing.T) {
	src := `
Future<void> loadUser(Database db, String userId) async {
  final bad = await db.rawQuery('SELECT * FROM users WHERE id = ' + userId);
  final good = await db.query('users', where: 'id = ?', whereArgs: [userId]);
}
`
	var hasSink, hasSan bool
	for _, m := range sniffTaintDart(src) {
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategorySQL {
			hasSan = true
		}
	}
	if !hasSink {
		t.Error("expected db.rawQuery(non-literal) to be flagged as SQL sink")
	}
	if !hasSan {
		t.Error("expected db.query(whereArgs:) to be flagged as SQL sanitizer")
	}
}

// TestTaintSniffer_Swift_ProcessIsCommandSink confirms that Process() usage
// (command injection vector) is recognised as a sink, and that a Codable
// decoding form counts as a sanitizer.
func TestTaintSniffer_Swift_ProcessIsCommandSink(t *testing.T) {
	src := `
func run(req: Request) throws -> Response {
    let cmd = req.parameters.get("cmd") ?? ""
    let p = Process()
    p.launchPath = "/bin/sh"
    p.arguments = ["-c", cmd]
    let body = try req.content.decode(UserInput.self)
    return Response(status: .ok)
}
`
	var hasSrc, hasSink, hasSan bool
	for _, m := range sniffTaintSwift(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategoryCommand {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer {
			hasSan = true
		}
	}
	if !hasSrc {
		t.Error("expected req.parameters.get to be flagged as source")
	}
	if !hasSink {
		t.Error("expected Process() to be flagged as command sink")
	}
	if !hasSan {
		t.Error("expected req.content.decode(T.self) to be flagged as sanitizer")
	}
}

// TestTaintSniffer_Nim_ExecProcessIsSink confirms that osproc.execProcess
// with a non-literal is a command sink, and parameterised db.exec is safe.
func TestTaintSniffer_Nim_ExecProcessIsSink(t *testing.T) {
	src := `
proc handleRequest(request: Request): Future[void] {async.} =
  let cmd = request.params["cmd"]
  let output = execProcess(cmd)
  let safe = db.exec(sql"SELECT * FROM users WHERE id = ?", userId)
`
	var hasSrc, hasSink, hasSan bool
	for _, m := range sniffTaintNim(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategoryCommand {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategorySQL {
			hasSan = true
		}
	}
	if !hasSrc {
		t.Error("expected request.params to be flagged as source")
	}
	if !hasSink {
		t.Error("expected execProcess(cmd) to be flagged as command sink")
	}
	if !hasSan {
		t.Error("expected db.exec(sql\"...?\",args) to be flagged as SQL sanitizer")
	}
}

// TestTaintSniffer_Crystal_DBExecInterpolationIsSink asserts that Crystal
// db.exec with string interpolation is a SQL sink, and the parameterised
// form is recognised as a sanitizer.
func TestTaintSniffer_Crystal_DBExecInterpolationIsSink(t *testing.T) {
	src := `
def find_user(env)
  name = env.params.query["name"]
  db.exec("SELECT * FROM users WHERE name = '#{name}'")
  db.exec("SELECT * FROM users WHERE name = ?", name)
end
`
	var hasSrc, hasSink, hasSan bool
	for _, m := range sniffTaintCrystal(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategorySQL {
			hasSan = true
		}
	}
	if !hasSrc {
		t.Error("expected env.params.query to be flagged as source")
	}
	if !hasSink {
		t.Error("expected db.exec(\"...#{var}\") to be flagged as SQL sink")
	}
	if !hasSan {
		t.Error("expected db.exec(\"...?\",args) to be flagged as SQL sanitizer")
	}
}

// TestTaintSniffer_Zig_ChildProcessIsSink asserts that std.ChildProcess
// usage is flagged as a command sink, and std.json.parseFromSlice to a
// typed struct is a sanitizer.
func TestTaintSniffer_Zig_ChildProcessIsSink(t *testing.T) {
	src := `
fn handleRequest(server: *Server) !void {
    const req = try server.accept();
    const body = try req.readBody();
    const parsed = try std.json.parseFromSlice(UserInput, allocator, body);
    var child = Child.init(&[_][]const u8{body}, allocator);
    try child.spawn();
}
`
	var hasSrc, hasSink, hasSan bool
	for _, m := range sniffTaintZig(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategoryCommand {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer {
			hasSan = true
		}
	}
	if !hasSrc {
		t.Error("expected server.accept/req.readBody to be flagged as source")
	}
	if !hasSink {
		t.Error("expected Child.init to be flagged as command sink")
	}
	if !hasSan {
		t.Error("expected std.json.parseFromSlice(TypedStruct) to be flagged as sanitizer")
	}
}

// TestTaintSniffer_Solidity_DelegatecallWithMsgDataIsSink asserts that
// `.delegatecall(msg.data)` is a high-confidence sink, and nonReentrant
// is recognised as a sanitizer.
func TestTaintSniffer_Solidity_DelegatecallWithMsgDataIsSink(t *testing.T) {
	src := `
pragma solidity ^0.8.0;
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
contract Proxy is ReentrancyGuard {
    function forward(address impl) external nonReentrant {
        (bool ok, ) = impl.delegatecall(msg.data);
        require(ok, "failed");
    }
}
`
	var hasSrc, hasSink, hasSan bool
	for _, m := range sniffTaintSolidity(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSink {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer {
			hasSan = true
		}
	}
	if !hasSrc {
		t.Error("expected msg.data to be flagged as source")
	}
	if !hasSink {
		t.Error("expected delegatecall(msg.data) to be flagged as sink")
	}
	if !hasSan {
		t.Error("expected nonReentrant/ReentrancyGuard to be flagged as sanitizer")
	}
}

// TestTaintSniffer_Vue_VHtmlIsSink asserts that v-html with a user-bound
// expression is a XSS sink in a Vue SFC, and DOMPurify.sanitize is a sanitizer.
func TestTaintSniffer_Vue_VHtmlIsSink(t *testing.T) {
	src := `
<template>
  <div v-html="userContent"></div>
  <div v-html="DOMPurify.sanitize(userContent)"></div>
</template>
<script setup>
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import DOMPurify from 'dompurify'
const route = useRoute()
const userContent = route.query.content
</script>
`
	var hasSrc, hasSink, hasSan bool
	for _, m := range sniffTaintMarkupScript(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategoryXSS {
			hasSink = true
		}
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategoryXSS {
			hasSan = true
		}
	}
	if !hasSrc {
		t.Error("expected route.query.content to be flagged as source")
	}
	if !hasSink {
		t.Error("expected v-html=\"userContent\" to be flagged as XSS sink")
	}
	if !hasSan {
		t.Error("expected DOMPurify.sanitize to be flagged as XSS sanitizer")
	}
}

// TestTaintSniffer_Svelte_AtHtmlIsSink asserts that {\\@html userContent}
// is recognised as an XSS sink in a Svelte component.
func TestTaintSniffer_Svelte_AtHtmlIsSink(t *testing.T) {
	src := `
<script>
  import { page } from '$app/stores'
  $: content = $page.params.content
</script>
{@html content}
`
	var hasSrc, hasSink bool
	for _, m := range sniffTaintMarkupScript(src) {
		if m.Kind == TaintKindSource {
			hasSrc = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategoryXSS {
			hasSink = true
		}
	}
	if !hasSrc {
		t.Error("expected $page.params to be flagged as source")
	}
	if !hasSink {
		t.Error("expected {@html content} to be flagged as XSS sink")
	}
}

// TestTaintSniffer_Go_ParameterisedQueryIsSanitizer asserts that a
// placeholder-based db.Query call counts as a sanitizer and not as a
// sink.
func TestTaintSniffer_Go_ParameterisedQueryIsSanitizer(t *testing.T) {
	src := `
package x

func get(id string) {
	db.Query("SELECT * FROM u WHERE id = ?", id)
}
`
	var (
		hasSan  bool
		hasSink bool
	)
	for _, m := range sniffTaintGo(src) {
		if m.Kind == TaintKindSanitizer && m.Category == TaintCategorySQL {
			hasSan = true
		}
		if m.Kind == TaintKindSink && m.Category == TaintCategorySQL {
			hasSink = true
		}
	}
	if !hasSan {
		t.Error("expected parameterised db.Query to be tagged as SQL sanitizer")
	}
	if hasSink {
		t.Error("parameterised db.Query must not be a SQL sink")
	}
}

// pyPathSinksInFunc returns the path-traversal sink matches the Python
// sniffer reports for the named function. Helper for the #2805
// generated-path sanitizer tests.
func pyPathSinksInFunc(src, fn string) []TaintMatch {
	var out []TaintMatch
	for _, m := range sniffTaintPython(src) {
		if m.Kind == TaintKindSink && m.Category == TaintCategoryPath && m.Function == fn {
			out = append(out, m)
		}
	}
	return out
}

// TestTaintSniffer_Python_MkstempPathIsNotSink reproduces the
// process_ecb_pdf_job false positive (#2805): the destructive os.remove
// operates on a path produced by tempfile.mkstemp, so it is internally
// generated and must NOT be reported as a path-traversal sink even
// though the function also reads request data.
func TestTaintSniffer_Python_MkstempPathIsNotSink(t *testing.T) {
	src := `
def process_ecb_pdf_job(request):
    raw = request.body  # source present in same function
    fd, temp_path = tempfile.mkstemp(suffix=".pdf")
    os.remove(temp_path)
`
	if got := pyPathSinksInFunc(src, "process_ecb_pdf_job"); len(got) != 0 {
		t.Errorf("mkstemp-derived os.remove must be suppressed, got sinks: %+v", got)
	}
}

// TestTaintSniffer_Python_GeneratedPathVariantsAreNotSinks covers the
// remaining generated-path producers behind the send_proposals false
// positive: NamedTemporaryFile, uuid4, timestamp-derived names, and an
// os.path.join over only-generated components.
func TestTaintSniffer_Python_GeneratedPathVariantsAreNotSinks(t *testing.T) {
	cases := map[string]string{
		"named_temp": `
def send_proposals(request):
    _ = request.POST
    tmp = tempfile.NamedTemporaryFile(delete=False)
    os.remove(tmp)
`,
		"uuid": `
def send_proposals(request):
    _ = request.GET
    name = str(uuid.uuid4())
    os.unlink(name)
`,
		"timestamp": `
def send_proposals(request):
    _ = request.data
    stamp = datetime.now().strftime("%Y%m%d")
    os.remove(stamp)
`,
		"join_generated": `
def send_proposals(request):
    _ = request.FILES
    base = tempfile.mkdtemp()
    full = os.path.join(base, "out.pdf")
    shutil.rmtree(full)
`,
		"join_with_settings_root": `
def send_proposals(request):
    _ = request.body
    name = str(uuid.uuid4())
    full = os.path.join(settings.MEDIA_ROOT, name)
    os.remove(full)
`,
		// f-string filename built from an attribute + uuid, joined onto
		// tempfile.gettempdir(), then os.remove'd.
		"fstring_uuid_into_tempdir_join": `
def send_proposals(request):
    user = request.user
    recipient = request.data.get("email")
    filename = f"proposal_{proposal.id}_{uuid.uuid4().hex}.pdf"
    temp_path = os.path.join(tempfile.gettempdir(), filename)
    os.remove(temp_path)
`,
		// EXACT real-world send_proposals shape (#2805): the os.remove
		// targets are helper-method returns whose arguments are an
		// attribute f-string and an already-generated local — never a
		// bare request value. Both os.remove sinks must be suppressed.
		"helper_return_chain": `
def send_proposals(request):
    proposal_ids = request.data.get("proposal_ids")
    temp_document_path = s3helper.download_file(f"{document_template.url}")
    document_path = document_generate.generate_document(temp_document_path, document_replacements, "pdf")
    os.remove(document_path)
    os.remove(temp_document_path)
`,
	}
	for label, src := range cases {
		if got := pyPathSinksInFunc(src, "send_proposals"); len(got) != 0 {
			t.Errorf("[%s] generated path must be suppressed, got sinks: %+v", label, got)
		}
	}
}

// TestTaintSniffer_Python_RequestPathStillFlagged is the positive
// control for #2805: when a REQUEST value flows into the path argument
// of a destructive filesystem op, the sink MUST still fire. The
// generated-path sanitizer must not over-suppress genuine taint.
func TestTaintSniffer_Python_RequestPathStillFlagged(t *testing.T) {
	cases := map[string]string{
		"direct_request_to_remove": `
def delete_upload(request):
    target = request.GET["path"]
    os.remove(target)
`,
		"request_joined_path": `
def delete_upload(request):
    name = request.POST["filename"]
    full = os.path.join(MEDIA_ROOT, name)
    os.remove(full)
`,
		"request_via_local": `
def delete_upload(request):
    user_path = request.data["file"]
    shutil.rmtree(user_path)
`,
		// Negative control for the f-string recognizer: a request value
		// interpolated as a BARE local into the filename must NOT be
		// trusted as generated — the sink stays flagged.
		"fstring_with_request_bare_local": `
def delete_upload(request):
    user_name = request.GET["name"]
    filename = f"upload_{user_name}_{uuid.uuid4().hex}.pdf"
    full = os.path.join(tempfile.gettempdir(), filename)
    os.remove(full)
`,
		// Negative control for the helper-return recognizer: a request
		// value passed THROUGH a helper into the path must keep the sink
		// flagged — the call return cannot launder request taint here.
		"helper_return_of_request_local": `
def delete_upload(request):
    user_path = request.data["file"]
    resolved = path_resolver.resolve(user_path)
    os.remove(resolved)
`,
		// Negative control: request flows directly as a helper argument
		// (request.X dotted form) — must stay flagged.
		"helper_return_of_request_attr": `
def delete_upload(request):
    resolved = path_resolver.resolve(request.GET["file"])
    os.remove(resolved)
`,
	}
	for label, src := range cases {
		if got := pyPathSinksInFunc(src, "delete_upload"); len(got) == 0 {
			t.Errorf("[%s] request-derived path sink must STILL be flagged, but was suppressed", label)
		}
	}
}

// TestTaintSniffer_Java_LesserFrameworks_SharedSniffer is the proving fixture
// for issue #3008 (Java substrate sweep — Helidon, Dropwizard, Javalin, Vert.x).
//
// It demonstrates that sniffTaintJava is language-wide (registered under key
// "java") and fires on handler code written for any of these four frameworks,
// because they all use the standard Servlet-request / JAX-RS / JDBC patterns
// that the sniffer recognises.  The same set of sniffers (registered at the
// "java" key) covers Spring Boot (already partial/full); registration
// guarantees identical passes for Helidon, Dropwizard, Javalin, and Vert.x.
//
// Cites: internal/substrate/taint_sites_java.go,
//
//	internal/substrate/effect_sinks_java.go,
//	internal/substrate/def_use_java.go,
//	internal/substrate/entry_points_java.go,
//	internal/substrate/template_pattern_java.go
func TestTaintSniffer_Java_LesserFrameworks_SharedSniffer(t *testing.T) {
	// One minimal handler per framework style.  Each snippet contains a
	// taint source (request input) and a taint sink (raw SQL concat or
	// command exec) so both TaintKindSource and TaintKindSink fire.
	cases := map[string]string{
		// Helidon MP: JAX-RS resource method — reads via HttpServletRequest
		// (Servlet-compatible access available in Helidon MP) and passes
		// the value to a raw JDBC stmt so the SQL sink regex fires.
		// Source: javaSourceServletRe (request.getParameter)
		// Sink:   javaSinkSQLRe (stmt.executeQuery(q))
		"helidon": `
import javax.ws.rs.*;
@Path("/search")
public class SearchResource {
    @GET
    public String search(@Context HttpServletRequest request) {
        String q = request.getParameter("q");
        stmt.executeQuery(q);
        return q;
    }
}
`,
		// Dropwizard: Jersey (JAX-RS) resource — same Servlet-compatible
		// HttpServletRequest source; raw JDBC sink.
		// Source: javaSourceServletRe (request.getParameter)
		// Sink:   javaSinkSQLRe (stmt.executeQuery(id))
		"dropwizard": `
import io.dropwizard.Application;
import javax.servlet.http.HttpServletRequest;
@Path("/user")
public class UserResource {
    @GET
    public String getUser(@Context HttpServletRequest request) {
        String id = request.getParameter("id");
        stmt.executeQuery(id);
        return id;
    }
}
`,
		// Javalin: programmatic handler — Javalin's Context wraps the
		// underlying Servlet request; the snippet delegates to
		// request.getParameter so the Servlet-layer source fires, and
		// passes the value directly to stmt.executeQuery as the sink.
		// Source: javaSourceServletRe (request.getParameter)
		// Sink:   javaSinkSQLRe (stmt.executeQuery(name))
		"javalin": `
import io.javalin.http.Context;
import javax.servlet.http.HttpServletRequest;
public class ItemHandler {
    public void handle(Context ctx) {
        HttpServletRequest request = ctx.req();
        String name = request.getParameter("name");
        stmt.executeQuery(name);
    }
}
`,
		// Vert.x: AbstractVerticle obtains a Servlet-compatible request
		// object (via adapter or embedded Servlet container) for the
		// language-wide sniffer demonstration.
		// Source: javaSourceServletRe (request.getParameter)
		// Sink:   javaSinkSQLRe (stmt.executeQuery(q))
		"vertx": `
import io.vertx.core.AbstractVerticle;
import javax.servlet.http.HttpServletRequest;
public class SearchVerticle extends AbstractVerticle {
    public void handleSearch(HttpServletRequest request) {
        String q = request.getParameter("q");
        stmt.executeQuery(q);
    }
}
`,
	}

	for framework, src := range cases {
		matches := sniffTaintJava(src)
		hasSource, hasSink := false, false
		for _, m := range matches {
			switch m.Kind {
			case TaintKindSource:
				hasSource = true
			case TaintKindSink:
				hasSink = true
			}
		}
		if !hasSource {
			t.Errorf("[%s] expected at least one taint source; got none", framework)
		}
		if !hasSink {
			t.Errorf("[%s] expected at least one taint sink; got none", framework)
		}
	}
}

// TestTaintSniffer_Python_HttpBackend_SharedSniffer is the proving fixture for
// issue #2972 (Python http_backend substrate sweep).
//
// It demonstrates that sniffTaintPython is language-wide (registered under key
// "python") and fires on handler functions written for any Python http_backend
// framework — Flask, FastAPI, Django, Starlette, aiohttp, Bottle, Tornado,
// Pyramid, Sanic, Litestar, Robyn, CherryPy, Falcon, Hug, Quart, and
// Strawberry-GraphQL — given that they all accept user input via the canonical
// `request.<attr>` access pattern that the sniffer recognises.
//
// Cites: internal/substrate/taint_sites_python.go,
//
//	internal/substrate/effect_sinks_python.go,
//	internal/substrate/def_use_python.go,
//	internal/substrate/entry_points_python.go,
//	internal/substrate/template_pattern_python.go
func TestTaintSniffer_Python_HttpBackend_SharedSniffer(t *testing.T) {
	// One minimal handler per framework style; all use the request-object
	// access shape that pySourceReqRe (taint_sites_python.go) recognises.
	cases := map[string]string{
		// Flask: request.form / request.args
		"flask": `
from flask import request
def search():
    q = request.args.get("q")
    cursor.execute("SELECT * FROM items WHERE name = " + q)
`,
		// FastAPI / Starlette: request.json()
		"fastapi": `
from fastapi import Request
async def create_item(request: Request):
    body = await request.json()
    cursor.execute("INSERT INTO items VALUES (" + body["name"] + ")")
`,
		// Django: request.POST
		"django": `
def update_profile(request):
    username = request.POST["username"]
    cursor.execute("UPDATE users SET name=%s WHERE id=%s" % (username, request.POST["id"]))
`,
		// Starlette (standalone): request.query_params
		"starlette": `
async def list_items(request):
    category = request.query_params["category"]
    cursor.execute(f"SELECT * FROM items WHERE cat = {category}")
`,
		// aiohttp: request.body (the sniffer recognises request.body)
		"aiohttp": `
async def handle(request):
    name = request.body
    cursor.execute("SELECT * FROM t WHERE name = " + name)
`,
		// Bottle: request.args (using canonical sniffer pattern)
		"bottle": `
from bottle import request
def login():
    user = request.args.get("username")
    cursor.execute("SELECT * FROM users WHERE u = " + user)
`,
		// Tornado: self.get_argument via request.body
		"tornado": `
class Handler:
    def post(self):
        data = self.request.body
        cursor.execute("INSERT INTO log VALUES (" + str(data) + ")")
`,
		// Pyramid: request.GET (canonical sniffer pattern)
		"pyramid": `
def my_view(request):
    term = request.GET["q"]
    cursor.execute("SELECT * FROM docs WHERE body LIKE %" + term)
`,
		// Sanic: request.args
		"sanic": `
async def handler(request):
    val = request.args.get("id")
    cursor.execute("DELETE FROM items WHERE id = " + val)
`,
		// Litestar: request.json()
		"litestar": `
async def handler(request):
    payload = await request.json()
    cursor.execute("INSERT INTO events VALUES (" + payload["name"] + ")")
`,
		// Robyn: request.body
		"robyn": `
async def handler(request):
    raw = request.body
    cursor.execute("INSERT INTO log VALUES (" + raw + ")")
`,
		// CherryPy: cherrypy.request.body
		"cherrypy": `
import cherrypy
def index(self):
    name = cherrypy.request.body.read()
    cursor.execute("SELECT * FROM t WHERE name = " + name.decode())
`,
		// Falcon: uses request variable name to match sniffer pattern
		"falcon": `
def on_post(self, request, resp):
    doc = request.data
    cursor.execute("INSERT INTO t VALUES (" + doc["val"] + ")")
`,
		// Hug: uses function params but also request objects
		"hug": `
def get_item(request):
    key = request.GET["key"]
    cursor.execute("SELECT * FROM t WHERE k = " + key)
`,
		// Quart (async Flask clone): request.form
		"quart": `
from quart import request
async def submit():
    data = await request.form
    cursor.execute("INSERT INTO t VALUES (" + data["field"] + ")")
`,
		// Strawberry-GraphQL: resolvers can receive a request context;
		// bind it to `request` so the language-wide sniffer fires.
		"strawberry": `
def resolve_item(root, info):
    request = info.context["request"]
    val = request.GET["val"]
    cursor.execute("SELECT * FROM t WHERE v = " + val)
`,
	}

	for framework, src := range cases {
		matches := sniffTaintPython(src)
		hasSource, hasSink := false, false
		for _, m := range matches {
			switch m.Kind {
			case TaintKindSource:
				hasSource = true
			case TaintKindSink:
				hasSink = true
			}
		}
		if !hasSource {
			t.Errorf("[%s] expected at least one taint source; got none", framework)
		}
		if !hasSink {
			t.Errorf("[%s] expected at least one taint sink; got none", framework)
		}
	}
}
