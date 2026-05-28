// Kotlin effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises Kotlin sink primitives. Kotlin code typically runs on the
// JVM and pulls from the Java ecosystem, so we accept Java-shaped sinks
// (RestTemplate, Files, JPA) in addition to Kotlin-native libraries
// (Ktor HttpClient, Exposed DSL, kotlinx.coroutines IO):
//
//   - http_out  : Ktor `HttpClient { }.get|post|put|delete|request`,
//                 OkHttp `OkHttpClient().newCall(...).execute()`,
//                 RestTemplate / WebClient / HttpClient (JVM),
//                 Fuel `<verb>(...)`, Retrofit interface `@GET/@POST` calls
//   - db_read   : JPA / Hibernate (`em.find / em.createQuery`, JpaRepo
//                 `findBy*/get*/count*/exists*`), Exposed `select / .selectAll
//                 / Table.select`, R2DBC `databaseClient.sql("SELECT")`,
//                 Spring Data `findBy*`
//   - db_write  : JPA `em.persist / merge / remove`, JpaRepo `save*/delete*`,
//                 Exposed `Table.insert / .update / .deleteWhere`, R2DBC
//                 INSERT/UPDATE/DELETE shapes
//   - fs_read   : File("...").readText / readLines / readBytes / inputStream,
//                 Files.readAllBytes / readAllLines / lines, Paths.get,
//                 BufferedReader(FileReader(...))
//   - fs_write  : File("...").writeText / appendText / writeBytes /
//                 outputStream / delete / renameTo / mkdir(s), Files.write
//                 / writeString / newOutputStream / createFile /
//                 createDirectories / delete / move / copy
//   - mutation  : `this.<field> = ...` assignment inside a method
//
// Function attribution uses the nearest preceding `fun name(` header
// (with optional visibility, suspend, inline, infix, operator, override,
// open, final, abstract qualifiers).
package substrate

import "regexp"

func init() { RegisterEffectSniffer("kotlin", sniffEffectsKotlin) }

// kotlinFuncHeaderRe matches `fun name(` (with optional modifiers and
// generic parameters). Capture group 1 is the function name.
var kotlinFuncHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:(?:public|private|protected|internal|open|final|abstract|override|inline|infix|operator|tailrec|external|suspend|@[A-Za-z_][\w]*(?:\([^)]*\))?)\s+)*` +
		`fun\s+(?:<[^>]+>\s+)?(?:[A-Za-z_][\w<>?,.\[\]\s]*\.)?` +
		`([A-Za-z_][\w]*)\s*\(`,
)

// kotlinHTTPRe matches outbound HTTP primitives (Ktor + OkHttp + JVM
// HTTP clients + Retrofit shapes).
var kotlinHTTPRe = regexp.MustCompile(
	`\bHttpClient\s*\([^)]*\)\s*\.\s*(?:get|post|put|patch|delete|head|options|request|prepareGet|prepareRequest|use)\b` +
		`|\b(?:client|httpClient)\s*\.\s*(?:get|post|put|patch|delete|head|options|request|sendAsync|send)\s*[\({<]` +
		`|\bOkHttpClient\s*\(\s*\)\s*\.\s*newCall\s*\(` +
		`|\b(?:restTemplate|webClient)\s*\.\s*(?:getForObject|getForEntity|postForObject|postForEntity|put|patch|delete|exchange|execute|send)\s*\(` +
		`|\bFuel\s*\.\s*(?:get|post|put|patch|delete|head|request|upload|download)\s*\(` +
		`|\bnew\s+OkHttpClient\b` +
		`|\bRetrofit\s*\.\s*Builder\b`,
)

// kotlinDBReadRe matches JPA / Exposed / R2DBC read primitives.
var kotlinDBReadRe = regexp.MustCompile(
	`\b(?:entityManager|em)\s*\.\s*(?:find|getReference|createQuery|createNamedQuery|createNativeQuery)\s*\(` +
		`|\.\s*(?:findById|findAll|findBy[A-Z]\w*|findOne|getOne|count(?:By[A-Z]\w*)?|exists(?:ById|By[A-Z]\w*)?)\s*\(` +
		`|\b(?:[A-Z]\w*)\s*\.\s*(?:select|selectAll|selectBatched|join|innerJoin|leftJoin)\s*\(` +
		`|\bdatabaseClient\s*\.\s*sql\s*\(\s*"(?i:\s*(?:SELECT|WITH)\b)` +
		`|\.\s*executeQuery\s*\(`,
)

// kotlinDBWriteRe matches JPA / Exposed / R2DBC write primitives.
var kotlinDBWriteRe = regexp.MustCompile(
	`\b(?:entityManager|em)\s*\.\s*(?:persist|merge|remove|refresh|flush)\s*\(` +
		`|\.\s*(?:save|saveAll|saveAndFlush|delete(?:All|ById|InBatch)?|deleteBy[A-Z]\w*|updateBy[A-Z]\w*|insert)\s*\(` +
		`|\b(?:[A-Z]\w*)\s*\.\s*(?:insert|update|deleteWhere|deleteAll|batchInsert|replace|upsert)\s*[\({]` +
		`|\bdatabaseClient\s*\.\s*sql\s*\(\s*"(?i:\s*(?:INSERT|UPDATE|DELETE|REPLACE|MERGE|TRUNCATE)\b)` +
		`|\.\s*executeUpdate\s*\(`,
)

// kotlinFSReadRe matches read-only filesystem primitives.
var kotlinFSReadRe = regexp.MustCompile(
	`\bFile\s*\([^)]+\)\s*\.\s*(?:readText|readLines|readBytes|inputStream|bufferedReader|forEachLine|useLines|exists|isFile|isDirectory|length|listFiles)\b` +
		`|\bFiles\s*\.\s*(?:readAllBytes|readAllLines|readString|lines|newInputStream|newBufferedReader|exists|isReadable|size|getAttribute|readAttributes|isDirectory|isRegularFile|list|walk|find)\s*\(` +
		`|\bnew\s+(?:FileInputStream|FileReader|BufferedReader\s*\(\s*FileReader|Scanner\s*\(\s*File)\s*\(`,
)

// kotlinFSWriteRe matches write filesystem primitives.
var kotlinFSWriteRe = regexp.MustCompile(
	`\bFile\s*\([^)]+\)\s*\.\s*(?:writeText|appendText|writeBytes|appendBytes|outputStream|bufferedWriter|printWriter|delete|deleteRecursively|renameTo|mkdir|mkdirs|setReadable|setWritable|setExecutable|copyTo|copyRecursively)\b` +
		`|\bFiles\s*\.\s*(?:write|writeString|newOutputStream|newBufferedWriter|createFile|createDirectory|createDirectories|delete|deleteIfExists|move|copy|setAttribute|setPosixFilePermissions)\s*\(` +
		`|\bnew\s+(?:FileOutputStream|FileWriter|PrintWriter\s*\(\s*File)\s*\(`,
)

// kotlinProcessRe matches process-spawn primitives (modelled as fs_write).
var kotlinProcessRe = regexp.MustCompile(
	`\bProcessBuilder\s*\(` +
		`|\bRuntime\s*\.\s*getRuntime\s*\(\s*\)\s*\.\s*exec\s*\(`,
)

// kotlinMutationRe matches `this.<field> = ...` assignment.
var kotlinMutationRe = regexp.MustCompile(
	`\bthis\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsKotlin(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanKotlinFuncHeaders(content)
	var out []EffectMatch
	out = appendKotlinMatches(out, content, headers, kotlinHTTPRe, EffectHTTPOut, "Ktor/OkHttp/RestTemplate", 1.0)
	out = appendKotlinMatches(out, content, headers, kotlinDBReadRe, EffectDBRead, "jpa/exposed/r2dbc.read", 0.85)
	out = appendKotlinMatches(out, content, headers, kotlinDBWriteRe, EffectDBWrite, "jpa/exposed/r2dbc.write", 0.85)
	out = appendKotlinMatches(out, content, headers, kotlinFSReadRe, EffectFSRead, "File.read/Files.read", 1.0)
	out = appendKotlinMatches(out, content, headers, kotlinFSWriteRe, EffectFSWrite, "File.write/Files.write", 1.0)
	out = appendKotlinMatches(out, content, headers, kotlinProcessRe, EffectFSWrite, "ProcessBuilder", 0.9)
	out = appendKotlinMatches(out, content, headers, kotlinMutationRe, EffectMutation, "this.field=", 0.7)
	return out
}

func scanKotlinFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	for _, m := range kotlinFuncHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, m[0]), Name: content[m[2]:m[3]]})
	}
	return hs
}

func appendKotlinMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
