// Go effect-sink sniffer (#2764 Phase 1A T1).
//
// Recognises Go sink primitives:
//
//   - http_out  : http.Get/Post/Head/PostForm, (*http.Client).Do/Get/Post,
//                 http.NewRequest + a subsequent Do is captured by the
//                 receiver method form
//   - db_read   : db.Query / QueryContext / QueryRow / QueryRowContext,
//                 (*sql.Stmt).Query*, GORM Find / First / Take / Last /
//                 Count / Pluck / Scan, sqlx Get / Select
//   - db_write  : db.Exec / ExecContext, GORM Create / Save / Updates /
//                 Update / Delete / Insert, sqlx NamedExec / MustExec
//   - fs_read   : os.Open / os.ReadFile / ioutil.ReadFile / os.ReadDir,
//                 ioutil.ReadAll on a file reader (heuristic — not caught
//                 without taint; covered later by Phase 2)
//   - fs_write  : os.Create / os.WriteFile / os.MkdirAll / os.Mkdir /
//                 os.Remove / os.RemoveAll / os.Rename / os.Chmod,
//                 ioutil.WriteFile
//   - mutation  : `<receiver>.<field> = ...` assignment inside a method
//                 body. We attribute mutation by looking for any
//                 identifier followed by `.field = ` and rely on the
//                 nearest-header heuristic to bind the match to a method;
//                 false positives on package-level struct-field writes
//                 are tagged to the synthetic top-level scope (empty
//                 function name) and elided by the propagation pass.
//
// Function attribution uses the same nearest-header heuristic as the
// other T1 sniffers; Go's gofmt indentation makes the heuristic very
// reliable in practice.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("go", sniffEffectsGo) }

// goFuncHeaderRe matches `func name(` or `func (recv T) name(`. Capture
// group 1 is the bare name (method-receiver-stripped).
var goFuncHeaderRe = regexp.MustCompile(
	`(?m)^func\s+(?:\(\s*[A-Za-z_][\w]*\s+\*?[A-Za-z_][\w]*\s*\)\s+)?([A-Za-z_][\w]*)\s*\(`,
)

// goHTTPRe matches net/http client primitives.
var goHTTPRe = regexp.MustCompile(
	`\bhttp\s*\.\s*(?:Get|Post|Head|PostForm)\s*\(` +
		`|\.\s*(?:Do|Get|Post|Head|PostForm)\s*\(\s*(?:req|request|httpReq|r)\b` +
		`|\b(?:client|httpClient|c)\s*\.\s*(?:Do|Get|Post|Head|PostForm)\s*\(`,
)

// goDBReadRe matches database/sql + GORM + sqlx read primitives.
var goDBReadRe = regexp.MustCompile(
	`\.\s*(?:Query|QueryContext|QueryRow|QueryRowContext)\s*\(` +
		`|\.\s*(?:Find|First|Last|Take|Pluck|Count|Scan|FindInBatches|Distinct|Select)\s*\(` +
		`|\.\s*Get\s*\(\s*&` + // sqlx convention: db.Get(&dest, ...)
		`|\.\s*Select\s*\(\s*&`, // sqlx convention: db.Select(&dest, ...)
)

// goDBWriteRe matches database/sql + GORM + sqlx write primitives.
var goDBWriteRe = regexp.MustCompile(
	`\.\s*(?:Exec|ExecContext)\s*\(` +
		`|\.\s*(?:Create|Save|Updates|Update|UpdateColumn|UpdateColumns|Delete|Insert|FirstOrCreate|Assign|Attrs|Begin|Commit|Rollback)\s*\(` +
		`|\.\s*(?:NamedExec|MustExec|NamedQuery)\s*\(`,
)

// goFSReadRe matches os / ioutil read primitives.
var goFSReadRe = regexp.MustCompile(
	`\b(?:os|ioutil)\s*\.\s*(?:Open|OpenFile|ReadFile|ReadDir|Stat|Lstat|ReadAll|Readlink)\s*\(`,
)

// goFSWriteRe matches os / ioutil write primitives.
var goFSWriteRe = regexp.MustCompile(
	`\b(?:os|ioutil)\s*\.\s*(?:Create|WriteFile|Mkdir|MkdirAll|Remove|RemoveAll|Rename|Chmod|Chown|Symlink|Link|Truncate)\s*\(`,
)

// goMutationRe matches `<recv>.<field> = ...` style assignment. The
// nearest-header attribution binds this to the enclosing method; if
// no method precedes it, the match falls to the module scope (synthetic
// "" function name) and the propagation pass drops it.
//
// We require a single-identifier receiver to avoid matching qualified
// constants (e.g. `pkg.Const`) — a single bare identifier followed by
// `.field = ` (with a trailing non-`=`).
var goMutationRe = regexp.MustCompile(
	`(?m)^\s*[A-Za-z_][\w]*\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsGo(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanGoFuncHeaders(content)
	var out []EffectMatch
	out = appendGoMatches(out, content, headers, goHTTPRe, EffectHTTPOut, "http.Client.Do/Get/Post", 1.0)
	out = appendGoMatches(out, content, headers, goDBReadRe, EffectDBRead, "sql.Query/GORM.Find/sqlx.Get", 0.85)
	out = appendGoMatches(out, content, headers, goDBWriteRe, EffectDBWrite, "sql.Exec/GORM.Create/Save", 0.85)
	out = appendGoMatches(out, content, headers, goFSReadRe, EffectFSRead, "os.Open/ReadFile", 1.0)
	out = appendGoMatches(out, content, headers, goFSWriteRe, EffectFSWrite, "os.Create/WriteFile", 1.0)
	out = appendGoMatches(out, content, headers, goMutationRe, EffectMutation, "recv.field=", 0.6)
	return out
}

func scanGoFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range goFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendGoMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
	for _, m := range re.FindAllStringIndex(content, -1) {
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		out = append(out, EffectMatch{
			Function:   fn,
			Line:       line,
			Effect:     eff,
			Sink:       sink,
			Confidence: conf,
		})
	}
	return out
}
