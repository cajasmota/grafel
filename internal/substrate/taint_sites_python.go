// Python taint-sites sniffer (#2772 Phase 2B T1).
//
// Recognises Python source / sink / sanitizer primitives.
//
// Sources:
//   - Django: request.POST / GET / body / headers / COOKIES / FILES
//   - Flask: request.form / args / json / data / headers / cookies / files
//   - FastAPI / Starlette: request.json() / form() / body() / headers
//   - os.environ / os.getenv
//   - pickle.loads / yaml.load / yaml.unsafe_load / marshal.loads /
//     json.loads (latter low-confidence — not always taint)
//
// Sinks:
//   - SQL injection: cursor.execute("...") with % or .format / f-string,
//     raw Model.objects.raw, connection.execute
//   - Command injection: subprocess.call/run/Popen(..., shell=True),
//     os.system, os.popen, eval, exec, compile
//   - Path traversal: open(<non-literal>), pathlib.Path(<non-literal>)
//     followed by read/write
//   - XSS: Django |safe filter (template-side, not detectable here),
//     mark_safe(<non-literal>), HttpResponse(<non-literal>)
//   - ReDoS: re.compile(<non-literal>)
//
// Sanitizers:
//   - Parameterised SQL: cursor.execute(sql, params) — second arg
//     present
//   - HTML escape: html.escape, bleach.clean, django.utils.html.escape,
//     markupsafe.escape
//   - Validation libs (schema-declaration required): pydantic
//     BaseModel subclass declarations, marshmallow Schema subclass
//     declarations, attrs.define / dataclass with validators — we
//     detect the declaration form, not the .parse() call
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterTaintSniffer("python", sniffTaintPython) }

// pySourceReqRe matches Django/Flask/FastAPI/DRF request-object access.
// `request.data` is the DRF (Django REST Framework) convention for the
// parsed request body — distinct from Django's `request.POST` which is
// form-only. Included here because DRF is the dominant Django HTTP API
// framework in 2026.
var pySourceReqRe = regexp.MustCompile(
	`\brequest\s*\.\s*(?:POST|GET|body|headers|COOKIES|FILES|form|args|json|data|cookies|files|query_params|path_params|META)\b`,
)

// pySourceEnvRe matches os.environ / os.getenv reads.
var pySourceEnvRe = regexp.MustCompile(
	`\bos\s*\.\s*(?:environ\s*(?:\[\s*['"][A-Z_][A-Z0-9_]*['"]\s*\]|\.\s*get\s*\()|getenv\s*\()`,
)

// pySourceDeserializeRe matches deserialisation primitives. pickle and
// yaml.load (without SafeLoader) are RCE-capable; flagged at high
// confidence. yaml.safe_load is excluded from the source set.
var pySourceDeserializeRe = regexp.MustCompile(
	`\bpickle\s*\.\s*loads?\s*\(` +
		`|\byaml\s*\.\s*(?:load|unsafe_load)\s*\(` +
		`|\bmarshal\s*\.\s*loads?\s*\(`,
)

// pySinkSQLRe matches cursor.execute / connection.execute with a
// non-parameterised SQL argument. The parameterised form
// `cursor.execute(sql, (params,))` is caught by the sanitizer regex
// below; we exclude it here by requiring the open paren followed by a
// quoted string that contains a `%` formatter, an f-string, or
// concatenation, or by a bare identifier.
var pySinkSQLRe = regexp.MustCompile(
	`\b(?:cursor|conn|connection)\s*\.\s*execute\s*\(\s*` +
		`(?:` +
		`f['"]` + // f-string SQL
		`|['"][^'"]*['"]\s*[%+]` + // "..." % var  or  "..." + var
		`|[A-Za-z_][\w]*\s*\)` + // bare identifier as only arg
		`|['"][^'"]*['"]\s*\.format\s*\(` + // "... {} ...".format(...)
		`)`,
)

// pySinkRawORMRe matches Django's raw() escape hatch.
var pySinkRawORMRe = regexp.MustCompile(
	`\.\s*objects\s*\.\s*raw\s*\(`,
)

// pySinkExecRe matches command-injection sinks. subprocess.* with
// shell=True is the classic vector; os.system / os.popen are always
// shell-evaluated. eval / exec are dynamic-code sinks.
// Negative lookbehind isn't supported in Go's RE2, so we use the
// `(?:^|[^.\w])` prefix to reject builtins-like names preceded by a
// dot (re.compile, ast.compile, importlib.util.compile_*). This
// prevents `re.compile(<literal-regex>)` from misfiring as a sink.
var pySinkExecRe = regexp.MustCompile(
	`\bsubprocess\s*\.\s*(?:call|run|Popen|check_call|check_output)\s*\([^)]*shell\s*=\s*True` +
		`|\bos\s*\.\s*(?:system|popen)\s*\(` +
		`|(?:^|[^.\w])(?:eval|exec|compile)\s*\(`,
)

// pySinkFSRe matches DESTRUCTIVE filesystem operations with a non-
// literal first arg. We exclude `open(<ident>)` because Python codebases
// routinely pass module-level path constants (LOG_FILE, CONFIG_PATH)
// through opens — the intraprocedural dataflow needed to prove the
// path is request-derived lives in Phase 4. Destructive ops
// (os.remove, os.unlink, shutil.rmtree, os.rename) are unambiguously
// security-sensitive even when the variable is internal, so we keep
// them. Pathlib.Path itself is benign — it's the subsequent .write_*
// that matters, and those are captured by the matching write regex.
//
// Capture group 1 is the path-argument identifier. The generated-path
// recognizer (pyGeneratedPathLocals) uses it to suppress sinks whose
// argument is provably an internally-generated path (#2805).
var pySinkFSRe = regexp.MustCompile(
	`\bos\s*\.\s*(?:remove|unlink|rmdir|rename|replace)\s*\(\s*([A-Za-z_][\w]*)\s*[,)]` +
		`|\bshutil\s*\.\s*(?:rmtree|move|copy|copy2|copyfile|copytree)\s*\(\s*([A-Za-z_][\w]*)\s*[,)]`,
)

// pyGeneratedPathAssignRe matches assignments that bind a local to a
// value the runtime generates — never the request. Capture group 1 is
// the LHS target (single name or the second name of a `fd, path = ...`
// tuple unpack), group 2 the same for the simple-assign form. The RHS
// alternation covers:
//
//   - tempfile.mkstemp / mkdtemp                 → `fd, path = tempfile.mkstemp(...)`
//   - tempfile.NamedTemporaryFile / TemporaryDirectory
//   - uuid.uuid1/3/4/5 (often `str(uuid.uuid4())`)
//   - datetime.now()/utcnow()/strftime and time.time()/strftime
//     (timestamp-derived names)
//
// These are the producers behind the #2805 false positives
// (process_ecb_pdf_job's `fd, temp_path = tempfile.mkstemp(...)` and the
// download/generate helpers feeding send_proposals). A path bound here
// cannot carry request taint, so the destructive sink that consumes it
// is suppressed.
var pyGeneratedPathAssignRe = regexp.MustCompile(
	`(?m)^\s*(?:[A-Za-z_]\w*\s*,\s*)?([A-Za-z_]\w*)\s*=\s*` +
		`(?:[A-Za-z_][\w.]*\s*\(\s*)?` + // optional wrapping call e.g. str(...), Path(...)
		`(?:` +
		`tempfile\s*\.\s*(?:mkstemp|mkdtemp|NamedTemporaryFile|TemporaryDirectory|TemporaryFile)\b` +
		`|uuid\s*\.\s*uuid[1345]\s*\(` +
		`|datetime\s*\.\s*(?:datetime\s*\.\s*)?(?:now|utcnow)\s*\(` +
		`|(?:datetime|time)\s*\.\s*strftime\s*\(` +
		`|time\s*\.\s*time\s*\(` +
		`)`,
)

// pyGenStringAssignRe matches `name = f"..."` / `name = "..."` style
// string-literal assignments that build a name from generated material.
// Capture group 1 is the LHS, group 2 the full string-construction RHS.
// We do NOT trust the string blindly: pass 1 only promotes it to
// generated when pyStringRHSAllGenerated confirms every interpolated
// component is itself generated/attribute/literal and no bare request-
// local leaks in. This covers filename builders like
// `f"proposal_{proposal.id}_{uuid.uuid4().hex}.pdf"`.
var pyGenStringAssignRe = regexp.MustCompile(
	`(?m)^\s*([A-Za-z_]\w*)\s*=\s*(f?['"][^\n]*)$`,
)

// pyFStringInterpRe extracts `{ ... }` interpolation bodies from an
// f-string so each can be checked for request-local leakage.
var pyFStringInterpRe = regexp.MustCompile(`\{([^{}]*)\}`)

// pyGenProducerRe recognises a generated producer call anywhere in a
// fragment (uuid/datetime/time/tempfile). Used to require that a string
// assignment actually derives from generated material before trusting it.
var pyGenProducerRe = regexp.MustCompile(
	`uuid\s*\.\s*uuid[1345]\b` +
		`|datetime\s*\.\s*(?:datetime\s*\.\s*)?(?:now|utcnow)\b` +
		`|(?:datetime|time)\s*\.\s*strftime\b` +
		`|time\s*\.\s*time\b` +
		`|tempfile\s*\.\s*(?:gettempdir|mkstemp|mkdtemp)\b`,
)

// pyHelperReturnAssignRe matches `name = obj.method(args...)` and
// `name = func(args...)` — a local bound from a call's return value.
// Capture group 1 is the LHS local, group 2 the callee, group 3 the
// raw argument text (which may contain nested parens — the consumer
// re-balances it, since RE2 cannot). os.path.join is excluded here and
// handled by its own dedicated pass. This recognises send_proposals'
// real shape (#2805): the destructive path is a helper return —
// `temp_document_path = s3helper.download_file(f"{template.url}")` and
// `document_path = gen.generate_document(temp_document_path, ...)` — whose
// arguments are attributes / already-generated locals, never a bare
// request value. Such a return cannot carry request taint.
var pyHelperReturnAssignRe = regexp.MustCompile(
	`(?m)^\s*([A-Za-z_]\w*)\s*=\s*([A-Za-z_][\w.]*)\s*\(`,
)

// pyIdentRe extracts bare identifiers from an argument list so we can
// confirm each component is itself a generated local, a literal, or a
// benign attribute access.
var pyIdentRe = regexp.MustCompile(`[A-Za-z_][\w.]*`)

// pySinkXSSRe matches mark_safe / HttpResponse on a non-literal.
var pySinkXSSRe = regexp.MustCompile(
	`\bmark_safe\s*\(\s*[A-Za-z_][\w]*\s*\)` +
		`|\bHttpResponse\s*\(\s*[A-Za-z_][\w]*\s*[,)]`,
)

// pySinkReDoSRe matches re.compile of a non-literal.
var pySinkReDoSRe = regexp.MustCompile(
	`\bre\s*\.\s*compile\s*\(\s*[A-Za-z_][\w]*\s*[,)]`,
)

// pySanitizerSQLRe matches the parameterised cursor.execute form:
// cursor.execute(sql, params). Detection: open paren, quoted SQL,
// comma, then the params argument. We accept literal SQL or a bare
// identifier (named sql).
// Recognises the parameterised cursor.execute form. The SQL argument
// may be a quoted string, an f-string, a triple-quoted block, or a
// bare identifier (named sql); the second arg is what proves the call
// is safe. We accept the params arg starting with `[`, `(`, `{`,
// `tuple(`, `list(`, or a bare identifier — the universal pattern is
// "a second positional argument exists at all", which DB-API binds
// to the placeholders in the SQL.
var pySanitizerSQLRe = regexp.MustCompile(
	`\b(?:cursor|conn|connection)\s*\.\s*execute\s*\(\s*` +
		`(?:` +
		`f?['"]{3}[\s\S]*?['"]{3}` + // triple-quoted (possibly f-string) SQL
		`|f?['"][^'"]*['"]` + // single-quoted (possibly f-string) SQL
		`|[A-Za-z_][\w.]*` + // bare or dotted identifier (e.g. self.sql)
		`)` +
		`\s*,\s*` + // params separator
		`(?:[\[(\{]|tuple\b|list\b|dict\b|[A-Za-z_][\w]*)`,
)

// pySanitizerHTMLRe matches HTML-escape libraries.
var pySanitizerHTMLRe = regexp.MustCompile(
	`\bhtml\s*\.\s*escape\s*\(` +
		`|\bbleach\s*\.\s*(?:clean|linkify)\s*\(` +
		`|\bdjango\s*\.\s*utils\s*\.\s*html\s*\.\s*escape\s*\(` +
		`|\bmarkupsafe\s*\.\s*escape\s*\(` +
		`|\bescape\s*\(`,
)

// pySanitizerSchemaRe matches pydantic / marshmallow / attrs schema
// declarations. HARD RULE per #2772: the SCHEMA declaration is what
// counts, not the parse-call site. We match class declarations whose
// base is one of the known schema bases; this is conservative and
// only fires inside files that actually declare schemas.
var pySanitizerSchemaRe = regexp.MustCompile(
	`(?m)^\s*class\s+[A-Za-z_]\w*\s*\(\s*(?:BaseModel|Schema|marshmallow\s*\.\s*Schema|pydantic\s*\.\s*BaseModel)\s*[,)\s]`,
)

func sniffTaintPython(content string) []TaintMatch {
	if content == "" {
		return nil
	}
	headers := scanPyFuncHeaders(content)
	var out []TaintMatch
	out = appendTaintMatches(out, content, headers, pySourceReqRe, TaintKindSource, TaintCategoryGeneric, "request.body/POST/json", 1.0)
	out = appendTaintMatches(out, content, headers, pySourceEnvRe, TaintKindSource, TaintCategoryGeneric, "os.environ/getenv", 0.85)
	out = appendTaintMatches(out, content, headers, pySourceDeserializeRe, TaintKindSource, TaintCategoryDeserialization, "pickle.loads/yaml.load", 1.0)
	// Sanitizers first.
	out = appendTaintMatches(out, content, headers, pySanitizerSQLRe, TaintKindSanitizer, TaintCategorySQL, "cursor.execute(sql, params)", 1.0)
	out = appendTaintMatches(out, content, headers, pySanitizerHTMLRe, TaintKindSanitizer, TaintCategoryXSS, "html.escape/bleach", 1.0)
	out = appendTaintMatches(out, content, headers, pySanitizerSchemaRe, TaintKindSanitizer, TaintCategoryGeneric, "pydantic/marshmallow.Schema", 0.9)
	// Sinks.
	out = appendTaintMatches(out, content, headers, pySinkSQLRe, TaintKindSink, TaintCategorySQL, "cursor.execute(non-literal)", 0.9)
	out = appendTaintMatches(out, content, headers, pySinkRawORMRe, TaintKindSink, TaintCategorySQL, "Model.objects.raw", 0.85)
	out = appendTaintMatches(out, content, headers, pySinkExecRe, TaintKindSink, TaintCategoryCommand, "subprocess shell=True/os.system/eval", 1.0)
	out = appendPyFSSinkMatches(out, content, headers)
	out = appendTaintMatches(out, content, headers, pySinkXSSRe, TaintKindSink, TaintCategoryXSS, "mark_safe/HttpResponse(non-literal)", 0.85)
	out = appendTaintMatches(out, content, headers, pySinkReDoSRe, TaintKindSink, TaintCategoryReDoS, "re.compile(non-literal)", 0.9)
	return out
}

// appendPyFSSinkMatches appends destructive-filesystem sink matches but
// suppresses any whose path argument is provably an internally-generated
// local (#2805 generated-path sanitizer). The taint pass co-locates a
// request source and an FS sink in the same function; without proving
// the path argument actually carries the source value it produces false
// positives. We can't do full intra-procedural dataflow here (that's the
// IPDF primitive, tracked separately), but we CAN recognise the inverse:
// a path bound from tempfile.mkstemp / NamedTemporaryFile / uuid /
// timestamp / os.path.join-of-generated-components never carries request
// taint, so its sink is safe regardless of what else the function reads.
func appendPyFSSinkMatches(out []TaintMatch, content string, headers []funcHeader) []TaintMatch {
	generated := pyGeneratedPathLocals(content, headers)
	for _, m := range pySinkFSRe.FindAllStringSubmatchIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		// Group 1 = os.* form arg, group 2 = shutil.* form arg.
		arg := submatchAt(content, m, 1)
		if arg == "" {
			arg = submatchAt(content, m, 2)
		}
		if arg != "" && generated[fnLocal{fn: fn, name: arg}] {
			// Path argument is a known-generated local — not request
			// derived. Suppress the false-positive path-traversal sink.
			continue
		}
		out = append(out, TaintMatch{
			Function:   fn,
			Line:       line,
			Kind:       TaintKindSink,
			Category:   TaintCategoryPath,
			Primitive:  "os.remove/unlink/rename/shutil(non-literal)",
			Confidence: 0.85,
		})
	}
	return out
}

// fnLocal keys a local variable by its owning function name. Generated-
// path recognition is function-scoped: a local named `temp_path` bound
// from mkstemp in fn A does not whitelist a same-named local in fn B.
type fnLocal struct {
	fn   string
	name string
}

// submatchAt returns the text of capture group idx for a FindAllString-
// SubmatchIndex match tuple, or "" when the group did not participate.
func submatchAt(content string, m []int, idx int) string {
	lo, hi := m[2*idx], m[2*idx+1]
	if lo < 0 || hi < 0 {
		return ""
	}
	return content[lo:hi]
}

// pyGeneratedPathLocals returns the set of (function, local-name) pairs
// whose binding proves the value is an internally-generated path. It
// runs in layered passes so later derivations can reference locals that
// earlier passes already proved generated:
//
//  1. direct producers (mkstemp / NamedTemporaryFile / uuid / timestamp)
//     1b. string builders (f-strings / literals) whose interpolations are
//     all generated/attribute/literal — no bare request local leaks in.
//  2. call-return / os.path.join assignments where EVERY bare-identifier
//     argument is an already-generated local, and every other token is a
//     dotted/attribute access (settings.X, self.dir, template.url) or a
//     string literal. Request taint always arrives through a BARE local,
//     so a call whose bare args are all generated cannot return taint.
//     Iterated to a fixed point so chains propagate
//     (a = mkdtemp(); b = join(a, x); c = helper(b)).
//
// Pass 2 covers send_proposals' real shape (#2805): the os.remove target
// is `document_path = generator.generate_document(temp_document_path, ...)`
// where temp_document_path itself came from `s3helper.download_file(f"{tpl.url}")`.
func pyGeneratedPathLocals(content string, headers []funcHeader) map[fnLocal]bool {
	out := map[fnLocal]bool{}
	for _, m := range pyGeneratedPathAssignRe.FindAllStringSubmatchIndex(content, -1) {
		name := submatchAt(content, m, 1)
		if name == "" {
			continue
		}
		fn := nearestHeader(headers, lineOfOffset(content, m[0]))
		out[fnLocal{fn: fn, name: name}] = true
	}
	// Pass 1b: string-construction assignments (f-strings, literals) that
	// derive a name purely from generated producers + attribute/literal
	// material.
	for _, m := range pyGenStringAssignRe.FindAllStringSubmatchIndex(content, -1) {
		name := submatchAt(content, m, 1)
		rhs := submatchAt(content, m, 2)
		if name == "" {
			continue
		}
		if !pyStringRHSAllGenerated(rhs) {
			continue
		}
		fn := nearestHeader(headers, lineOfOffset(content, m[0]))
		out[fnLocal{fn: fn, name: name}] = true
	}
	// Pass 2: call-return assignments (incl. os.path.join) whose every
	// bare-identifier argument is already generated. Fixed-point iterate
	// so chained derivations propagate. Bounded by the number of call
	// assignments (each can flip a key at most once).
	for changed := true; changed; {
		changed = false
		for _, m := range pyHelperReturnAssignRe.FindAllStringSubmatchIndex(content, -1) {
			name := submatchAt(content, m, 1)
			if name == "" {
				continue
			}
			callee := submatchAt(content, m, 2)
			fn := nearestHeader(headers, lineOfOffset(content, m[0]))
			key := fnLocal{fn: fn, name: name}
			if out[key] {
				continue
			}
			// The callee itself must not be a request source. A bind from
			// `request.data.get(...)` / `request.GET.get(...)` is the
			// request VALUE, not a generated path.
			if pyRequestRootRe.MatchString(callee) {
				continue
			}
			// Extract the balanced argument list starting at the open
			// paren this match ended on (m[1]-1 is the '(').
			args := pyBalancedArgs(content, m[1]-1)
			// os.path.join COMBINES every component into the path, so all
			// args must be safe. Any other helper follows the path-in/
			// path-out convention: only the FIRST positional argument is
			// the path; trailing args are content/flags and don't taint
			// the returned path (e.g. generate_document(path, data, fmt)).
			joinAll := pyOsPathJoinCalleeRe.MatchString(callee)
			if pyCallArgsSafe(args, fn, out, joinAll) {
				out[key] = true
				changed = true
			}
		}
	}
	return out
}

// pyBalancedArgs returns the text between the open paren at openIdx and
// its matching close paren, ignoring parens inside string literals. RE2
// cannot balance nesting, so call/join argument lists with nested calls
// (os.path.join(tempfile.gettempdir(), name)) are extracted here instead.
// Returns "" if no matching close paren is found before EOF or newline-
// terminated statement boundaries are exceeded.
func pyBalancedArgs(content string, openIdx int) string {
	if openIdx < 0 || openIdx >= len(content) || content[openIdx] != '(' {
		return ""
	}
	depth := 0
	var quote byte
	for i := openIdx; i < len(content); i++ {
		c := content[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return content[openIdx+1 : i]
			}
		}
	}
	return ""
}

// pyCallArgsAllSafe reports whether every bare-identifier token in a
// call/join argument list is a known-generated local. Dotted accesses
// (settings.MEDIA_ROOT, template.url, self.workdir), nested generated
// producer calls (uuid.uuid4, tempfile.gettempdir), and string literals
// are treated as safe — they are config/attribute/generated values, not
// request data, which always arrives through a BARE local. Any bare,
// non-generated identifier fails the check, keeping genuine request-
// derived calls flagged. Empty args fail (a no-arg call returns an
// unknown value we will not vouch for).
func pyCallArgsSafe(args, fn string, generated map[fnLocal]bool, allArgs bool) bool {
	if strings.TrimSpace(args) == "" {
		return false
	}
	if !allArgs {
		// Path-in/path-out convention: only the first positional argument
		// is the path. Truncate at the first top-level comma.
		args = pyFirstArg(args)
	}
	// Reduce string literals to their interpolation bodies: plain literals
	// contribute nothing, f-strings contribute only their `{...}` bodies
	// (which CAN reference a bare request local and must be checked). This
	// also drops the `f`/`r`/`b` string prefixes so they are not mistaken
	// for bare identifiers.
	scan := pyStripStringLiterals(args)
	return pyFragmentAllSafe(scan, fn, generated)
}

// pyOsPathJoinCalleeRe matches an os.path.join callee so the all-args
// path-combining rule applies (every component must be safe).
var pyOsPathJoinCalleeRe = regexp.MustCompile(`(?:^|\.)path\.join$`)

// pyFirstArg returns the first top-level positional argument of a call's
// argument text, splitting on the first comma that is not nested inside
// parens/brackets/braces or a string literal. Used so an opaque helper's
// path argument is evaluated independently of trailing content args.
func pyFirstArg(args string) string {
	depth := 0
	var quote byte
	for i := 0; i < len(args); i++ {
		c := args[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				return args[:i]
			}
		}
	}
	return args
}

// pyFragmentAllSafe reports whether a code fragment (a call-argument list
// or an f-string interpolation body, string literals already removed) can
// be vouched for as carrying no request taint. It classifies each name
// reference by position:
//
//   - request-rooted (`request`, `request.X`, `self.request.X`) → UNSAFE.
//   - an attribute suffix (preceded by `.`) → safe (it names a member,
//     e.g. `.url`, `.hex`, `.id`, never a request local).
//   - a call name (followed by `(`) → safe (function/method name, not a
//     value; e.g. `download_file`, `uuid4`).
//   - a module/attribute ROOT immediately followed by `.` → safe
//     (settings, os, uuid, datetime, document_template, self).
//   - a bare value identifier that IS a known-generated local → safe.
//   - any other bare value identifier → UNSAFE (may carry request taint).
//
// Position is decided by scanning the byte before (`.`) and the first
// non-space byte after the token (`.` or `(`).
func pyFragmentAllSafe(scan, fn string, generated map[fnLocal]bool) bool {
	for _, loc := range pyIdentRe.FindAllStringIndex(scan, -1) {
		tok := scan[loc[0]:loc[1]]
		if pyRequestRootRe.MatchString(tok) {
			return false
		}
		// Dotted token (root.attr...): the root is a module/attribute,
		// never a bare request local. Safe.
		if strings.Contains(tok, ".") {
			continue
		}
		// Preceded by '.' → this token is an attribute member name.
		if loc[0] > 0 && scan[loc[0]-1] == '.' {
			continue
		}
		// Followed (after spaces) by '(' → a function/method call name.
		j := loc[1]
		for j < len(scan) && (scan[j] == ' ' || scan[j] == '\t') {
			j++
		}
		if j < len(scan) && scan[j] == '(' {
			continue
		}
		// Followed by '.' → a module/attribute root (settings.X) — the
		// dotted form was split by pyIdentRe only when a '(' intervened;
		// treat the root as safe.
		if j < len(scan) && scan[j] == '.' {
			continue
		}
		if generated[fnLocal{fn: fn, name: tok}] {
			continue
		}
		// A bare value identifier that is not generated — may be tainted.
		return false
	}
	return true
}

// pyRequestRootRe matches a dotted access rooted at the Django/DRF
// `request` object (request.* or self.request.*). Such a token in a
// call's arguments means a request source is flowing into the call, so
// the call's return must NOT be treated as a generated path.
var pyRequestRootRe = regexp.MustCompile(`^(?:self\.)?request\b`)

// pyStripStringLiterals removes string literals from a fragment, keeping
// only f-string interpolation bodies (the `{...}` content) so they remain
// subject to bare-local taint checks. Non-f literals are replaced with a
// space. Handles single/double quotes and the f/r/b prefixes; does not
// attempt to honour triple-quotes (argument lists never contain them in
// practice). Code outside string literals is preserved verbatim.
func pyStripStringLiterals(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' || c == '"' {
			// Determine if a string-prefix (f/r/b, any case/combo)
			// immediately precedes the quote; if so, this is a prefixed
			// literal and we must already have emitted those prefix
			// letters — strip them retroactively by trimming trailing
			// ident chars we just wrote.
			out := b.String()
			j := len(out)
			for j > 0 && isPyStrPrefixByte(out[j-1]) {
				j--
			}
			prefix := strings.ToLower(out[j:])
			isF := strings.Contains(prefix, "f")
			// Drop the prefix letters from the buffer.
			b.Reset()
			b.WriteString(out[:j])
			// Consume the literal body up to the matching quote.
			quote := c
			i++
			for i < len(s) && s[i] != quote {
				if isF && s[i] == '{' {
					// Emit interpolation body up to the matching '}'.
					i++
					for i < len(s) && s[i] != '}' {
						b.WriteByte(s[i])
						i++
					}
					if i < len(s) { // skip the '}'
						b.WriteByte(' ')
					}
					continue
				}
				i++
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// isPyStrPrefixByte reports whether b is a valid Python string-literal
// prefix letter (f/r/b in any case).
func isPyStrPrefixByte(b byte) bool {
	switch b {
	case 'f', 'F', 'r', 'R', 'b', 'B':
		return true
	}
	return false
}

// pyStringRHSAllGenerated reports whether a string-construction RHS
// (f-string or plain literal) can be trusted as a generated path
// component. Rules, deliberately conservative to avoid over-suppression:
//
//   - A plain string literal with no f-prefix and no interpolation is a
//     literal name — safe.
//   - An f-string is safe only when it contains at least one generated
//     producer (uuid/datetime/time/tempfile) AND none of its `{...}`
//     interpolations reference a bare local. Interpolations may freely
//     use dotted/attribute accesses (proposal.id, self.x) and generated
//     producer calls (uuid.uuid4().hex); a bare identifier inside `{...}`
//     is treated as possible request taint and fails the check.
//
// This trusts send_proposals' `f"proposal_{proposal.id}_{uuid.uuid4().hex}.pdf"`
// (attribute + generator only) while still rejecting
// `f"{request_name}.pdf"` (bare local → potential taint).
func pyStringRHSAllGenerated(rhs string) bool {
	isF := strings.HasPrefix(rhs, "f'") || strings.HasPrefix(rhs, `f"`)
	if !isF {
		// Plain literal name with no interpolation — safe. (A bare
		// identifier RHS never reaches here; the regex requires a quote.)
		return !strings.Contains(rhs, "{")
	}
	// Must actually derive from a generated producer to be trusted.
	if !pyGenProducerRe.MatchString(rhs) {
		return false
	}
	// Every interpolation body must be free of bare request locals. We
	// pass an empty generated-set: inside an f-string we trust attributes
	// and producer calls (handled by pyFragmentAllSafe's positional
	// classification), not arbitrary bare locals.
	for _, interp := range pyFStringInterpRe.FindAllStringSubmatch(rhs, -1) {
		if !pyFragmentAllSafe(interp[1], "", map[fnLocal]bool{}) {
			return false
		}
	}
	return true
}
