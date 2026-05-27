// Java effect-sink sniffer (#2764 Phase 1A T1).
//
// Recognises Java sink primitives:
//
//   - http_out  : RestTemplate.<verb>, WebClient.method, HttpClient.send,
//                 OkHttpClient.newCall, Feign / Retrofit interface calls
//                 (heuristic — caught by the method-on-receiver shapes),
//                 java.net.URL.openConnection / openStream
//   - db_read   : EntityManager.find / createQuery, JpaRepository.find*
//                 / get* / count* / exists*, JdbcTemplate.queryFor*,
//                 Statement.executeQuery, Spring Data Mongo .findBy*
//   - db_write  : EntityManager.persist / merge / remove,
//                 JpaRepository.save* / delete*, JdbcTemplate.update /
//                 batchUpdate, Statement.executeUpdate
//   - fs_read   : Files.readAllBytes / readAllLines / lines / newInputStream,
//                 FileInputStream(<...>), new BufferedReader(new FileReader),
//                 Paths.get(...).toFile().exists() style → modeled by Files.*
//   - fs_write  : Files.write / writeString / newOutputStream / createFile
//                 / createDirectories / delete / move / copy,
//                 FileOutputStream(<...>), FileWriter(<...>)
//   - mutation  : `this.<field> = ...` assignment inside a method body
//
// Function attribution uses the same nearest-header heuristic as the
// other T1 sniffers.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("java", sniffEffectsJava) }

// javaMethodHeaderRe matches a Java method signature header. Conservative
// matcher — requires a modifier (public/private/protected/static/...) or
// a return-type token, the method name, parens, optional `throws ...`,
// and the opening brace on the same logical line. Lambdas are not captured
// (Phase 4 will handle them via the real parser).
//
// Capture group 1 is the method name.
var javaMethodHeaderRe = regexp.MustCompile(
	`(?m)^\s*` +
		`(?:(?:public|private|protected|static|final|abstract|synchronized|native|default|@[A-Za-z_][\w]*(?:\([^)]*\))?)\s+)+` +
		`(?:<[^>]+>\s+)?` +
		`[A-Za-z_$][\w$<>\[\],?.\s]*\s+` +
		`([A-Za-z_$][\w$]*)\s*\(`,
)

// javaCtorHeaderRe captures constructor declarations: `public Foo(...)`.
var javaCtorHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:public|private|protected)\s+([A-Z][\w$]*)\s*\(`,
)

// javaHTTPRe matches outbound HTTP sites.
var javaHTTPRe = regexp.MustCompile(
	`\b(?:restTemplate|webClient|httpClient|okHttpClient|client)\s*\.\s*(?:getForObject|getForEntity|postForObject|postForEntity|put|patch|delete|exchange|execute|send|sendAsync|newCall|method|head|options|get|post)\s*\(` +
		`|\bnew\s+URL\s*\([^)]*\)\s*\.\s*(?:openConnection|openStream)\s*\(` +
		`|\bHttpRequest\s*\.\s*newBuilder\b`,
)

// javaDBReadRe matches JPA / Spring Data / JDBC read primitives.
var javaDBReadRe = regexp.MustCompile(
	`\b(?:entityManager|em)\s*\.\s*(?:find|getReference|createQuery|createNamedQuery|createNativeQuery)\s*\(` +
		`|\.\s*(?:findById|findAll|findBy[A-Za-z_]\w*|findOne|getOne|count(?:By[A-Za-z_]\w*)?|exists(?:ById|By[A-Za-z_]\w*)?)\s*\(` +
		`|\b(?:jdbcTemplate|namedJdbcTemplate)\s*\.\s*(?:queryFor[A-Za-z]+|query|queryForList|queryForMap|queryForObject)\s*\(` +
		`|\.\s*executeQuery\s*\(` +
		`|\bcriteriaBuilder\s*\.\s*createQuery\s*\(`,
)

// javaDBWriteRe matches JPA / Spring Data / JDBC write primitives.
var javaDBWriteRe = regexp.MustCompile(
	`\b(?:entityManager|em)\s*\.\s*(?:persist|merge|remove|refresh|flush)\s*\(` +
		`|\.\s*(?:save|saveAll|saveAndFlush|delete(?:All|ById|InBatch)?|deleteBy[A-Za-z_]\w*|updateBy[A-Za-z_]\w*|insert)\s*\(` +
		`|\b(?:jdbcTemplate|namedJdbcTemplate)\s*\.\s*(?:update|batchUpdate|execute)\s*\(` +
		`|\.\s*executeUpdate\s*\(`,
)

// javaFSReadRe matches NIO / classic-IO read primitives.
var javaFSReadRe = regexp.MustCompile(
	`\bFiles\s*\.\s*(?:readAllBytes|readAllLines|readString|lines|newInputStream|newBufferedReader|exists|isReadable|size|getAttribute|readAttributes|isDirectory|isRegularFile|list|walk|find)\s*\(` +
		`|\bnew\s+(?:FileInputStream|FileReader|BufferedReader\s*\(\s*new\s+FileReader|Scanner\s*\(\s*new\s+File)\s*\(`,
)

// javaFSWriteRe matches NIO / classic-IO write primitives.
var javaFSWriteRe = regexp.MustCompile(
	`\bFiles\s*\.\s*(?:write|writeString|newOutputStream|newBufferedWriter|createFile|createDirectory|createDirectories|delete|deleteIfExists|move|copy|setAttribute|setPosixFilePermissions)\s*\(` +
		`|\bnew\s+(?:FileOutputStream|FileWriter|PrintWriter\s*\(\s*new\s+File)\s*\(`,
)

// javaMutationRe matches `this.<field> = ...` inside a method body.
var javaMutationRe = regexp.MustCompile(
	`\bthis\s*\.\s*[A-Za-z_$][\w$]*\s*=(?:[^=])`,
)

func sniffEffectsJava(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanJavaFuncHeaders(content)
	var out []EffectMatch
	out = appendJavaMatches(out, content, headers, javaHTTPRe, EffectHTTPOut, "RestTemplate/WebClient/HttpClient", 1.0)
	out = appendJavaMatches(out, content, headers, javaDBReadRe, EffectDBRead, "jpa/jdbc.read", 0.85)
	out = appendJavaMatches(out, content, headers, javaDBWriteRe, EffectDBWrite, "jpa/jdbc.write", 0.85)
	out = appendJavaMatches(out, content, headers, javaFSReadRe, EffectFSRead, "Files.read/FileInputStream", 1.0)
	out = appendJavaMatches(out, content, headers, javaFSWriteRe, EffectFSWrite, "Files.write/FileOutputStream", 1.0)
	out = appendJavaMatches(out, content, headers, javaMutationRe, EffectMutation, "this.field=", 0.7)
	return out
}

func scanJavaFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	add := func(name string, off int) {
		if name == "" || javaControlKeyword(name) {
			return
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, off), Name: name})
	}
	for _, m := range javaMethodHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		add(content[m[2]:m[3]], m[0])
	}
	for _, m := range javaCtorHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		add(content[m[2]:m[3]], m[0])
	}
	return hs
}

// javaControlKeyword rejects control-flow tokens the conservative method
// regex can accidentally match (`if (...) {`, `while (...) {`, etc.).
func javaControlKeyword(s string) bool {
	switch s {
	case "if", "else", "for", "while", "switch", "catch", "try", "do", "return", "throw", "new", "synchronized", "super", "this":
		return true
	}
	return false
}

func appendJavaMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
