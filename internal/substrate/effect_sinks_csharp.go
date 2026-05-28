// C# effect-sink sniffer (#2765 Phase 1A T2).
//
// Recognises C# sink primitives:
//
//   - http_out  : HttpClient.GetAsync / PostAsync / PutAsync / DeleteAsync
//                 / SendAsync / GetStringAsync / GetStreamAsync, WebClient
//                 .Download* / .Upload*, RestClient (RestSharp) .Execute*,
//                 HttpWebRequest.Create
//   - db_read   : Entity Framework DbSet `.Find / .FindAsync / .First /
//                 .FirstOrDefault / .Single / .SingleOrDefault / .Where /
//                 .ToList / .ToArray / .Count / .Any / .Include /
//                 .AsQueryable / .FromSqlRaw / .FromSqlInterpolated`,
//                 raw ADO.NET `SqlCommand.ExecuteReader / ExecuteScalar`,
//                 Dapper `Query / QueryAsync / QueryFirst / QuerySingle`
//   - db_write  : EF DbSet `.Add / .AddAsync / .AddRange / .Update /
//                 .UpdateRange / .Remove / .RemoveRange / .Attach`, EF
//                 `SaveChanges / SaveChangesAsync / ExecuteSqlRaw /
//                 ExecuteSqlInterpolated`, ADO.NET SqlCommand
//                 `.ExecuteNonQuery`, Dapper `Execute / ExecuteAsync`
//   - fs_read   : File.ReadAllText / .ReadAllBytes / .ReadAllLines /
//                 .ReadLines / .OpenRead / .OpenText, Directory.Get*,
//                 new FileStream(..., FileMode.Open, FileAccess.Read),
//                 StreamReader ctor
//   - fs_write  : File.WriteAllText / .WriteAllBytes / .WriteAllLines /
//                 .AppendAllText / .AppendAllLines / .Delete / .Move /
//                 .Copy / .Create / .CreateText, Directory.CreateDirectory
//                 / .Delete / .Move, FileStream with FileMode.Create or
//                 FileAccess.Write, StreamWriter ctor
//   - mutation  : `this.<Property|field> = ...` assignment inside a method
//
// Function attribution uses the nearest preceding method header — same
// shape as the Java sniffer with C#-specific keyword set.
package substrate

import "regexp"

func init() { RegisterEffectSniffer("csharp", sniffEffectsCSharp) }

// csharpMethodHeaderRe matches a C# method signature header. Conservative
// — requires a modifier (public/private/protected/internal/static/...) or
// async, the return type, the method name, and the opening parenthesis.
// Constructors are captured by csharpCtorHeaderRe.
var csharpMethodHeaderRe = regexp.MustCompile(
	`(?m)^\s*` +
		`(?:(?:public|private|protected|internal|static|virtual|override|abstract|sealed|async|unsafe|new|extern|partial|readonly|\[[^\]]+\])\s+)+` +
		`(?:<[^>]+>\s+)?` +
		`[A-Za-z_][\w<>\[\],?.\s]*\s+` +
		`([A-Za-z_][\w]*)\s*\(`,
)

// csharpCtorHeaderRe matches constructor declarations.
var csharpCtorHeaderRe = regexp.MustCompile(
	`(?m)^\s*(?:public|private|protected|internal)\s+([A-Z][\w]*)\s*\(`,
)

// csharpHTTPRe matches outbound HTTP primitives.
var csharpHTTPRe = regexp.MustCompile(
	`\b(?:httpClient|_httpClient|client|_client|http)\s*\.\s*(?:GetAsync|PostAsync|PutAsync|PatchAsync|DeleteAsync|HeadAsync|SendAsync|GetStringAsync|GetStreamAsync|GetByteArrayAsync|PostAsJsonAsync|PutAsJsonAsync|PatchAsJsonAsync)\s*\(` +
		`|\bnew\s+HttpClient\s*\(` +
		`|\b(?:HttpClient|HttpRequestMessage|HttpWebRequest|WebClient|RestClient)\s*\.\s*(?:Create|Default)\b` +
		`|\bnew\s+(?:RestClient|RestRequest|WebClient|HttpWebRequest)\s*\(` +
		`|\.\s*Execute(?:Async)?\s*<[^>]+>\s*\(` +
		`|\b(?:WebClient|HttpClient)\s*\.\s*(?:DownloadString|DownloadData|DownloadFile|UploadString|UploadData|UploadFile)\b`,
)

// csharpDBReadRe matches EF / Dapper / ADO.NET read primitives.
var csharpDBReadRe = regexp.MustCompile(
	`\.\s*(?:Find|FindAsync|First|FirstAsync|FirstOrDefault|FirstOrDefaultAsync|Single|SingleAsync|SingleOrDefault|SingleOrDefaultAsync|Where|Any|AnyAsync|All|Count|CountAsync|LongCount|LongCountAsync|ToList|ToListAsync|ToArray|ToArrayAsync|ToDictionary|Include|ThenInclude|AsQueryable|AsNoTracking|FromSqlRaw|FromSqlInterpolated|SqlQuery)\s*[\(<]` +
		`|\.\s*(?:ExecuteReader|ExecuteReaderAsync|ExecuteScalar|ExecuteScalarAsync)\s*\(` +
		`|\.\s*(?:Query|QueryAsync|QueryFirst|QueryFirstAsync|QueryFirstOrDefault|QueryFirstOrDefaultAsync|QuerySingle|QuerySingleAsync|QuerySingleOrDefault|QuerySingleOrDefaultAsync|QueryMultiple|QueryMultipleAsync)\s*[\(<]`,
)

// csharpDBWriteRe matches EF / Dapper / ADO.NET write primitives.
var csharpDBWriteRe = regexp.MustCompile(
	`\.\s*(?:Add|AddAsync|AddRange|AddRangeAsync|Update|UpdateRange|Remove|RemoveRange|Attach|AttachRange|Entry)\s*\(` +
		`|\.\s*(?:SaveChanges|SaveChangesAsync|ExecuteSqlRaw|ExecuteSqlRawAsync|ExecuteSqlInterpolated|ExecuteSqlInterpolatedAsync|ExecuteUpdate|ExecuteUpdateAsync|ExecuteDelete|ExecuteDeleteAsync)\s*\(` +
		`|\.\s*(?:ExecuteNonQuery|ExecuteNonQueryAsync)\s*\(` +
		`|\.\s*(?:Execute|ExecuteAsync|ExecuteScalar)\s*\(\s*["@$]`,
)

// csharpFSReadRe matches read-only filesystem primitives.
var csharpFSReadRe = regexp.MustCompile(
	`\bFile\s*\.\s*(?:ReadAllText|ReadAllTextAsync|ReadAllBytes|ReadAllBytesAsync|ReadAllLines|ReadAllLinesAsync|ReadLines|ReadLinesAsync|OpenRead|OpenText|Exists)\s*\(` +
		`|\bDirectory\s*\.\s*(?:GetFiles|GetDirectories|GetFileSystemEntries|EnumerateFiles|EnumerateDirectories|EnumerateFileSystemEntries|Exists)\s*\(` +
		`|\bnew\s+(?:StreamReader|FileStream\s*\([^)]*FileMode\.Open|BinaryReader)\s*\(`,
)

// csharpFSWriteRe matches write filesystem primitives.
var csharpFSWriteRe = regexp.MustCompile(
	`\bFile\s*\.\s*(?:WriteAllText|WriteAllTextAsync|WriteAllBytes|WriteAllBytesAsync|WriteAllLines|WriteAllLinesAsync|AppendAllText|AppendAllTextAsync|AppendAllLines|Delete|Move|Copy|Create|CreateText|OpenWrite|SetAttributes|SetCreationTime|SetLastWriteTime|Replace)\s*\(` +
		`|\bDirectory\s*\.\s*(?:CreateDirectory|Delete|Move)\s*\(` +
		`|\bnew\s+(?:StreamWriter|FileStream\s*\([^)]*FileMode\.(?:Create|CreateNew|Append)|BinaryWriter)\s*\(`,
)

// csharpProcessRe matches process-spawn primitives (modelled as fs_write).
var csharpProcessRe = regexp.MustCompile(
	`\bProcess\s*\.\s*Start\s*\(` +
		`|\bnew\s+Process\s*\(\s*\)\s*\.\s*Start\b` +
		`|\bProcessStartInfo\s*\(`,
)

// csharpMutationRe matches `this.<Member> = ...` assignment.
var csharpMutationRe = regexp.MustCompile(
	`\bthis\s*\.\s*[A-Za-z_][\w]*\s*=(?:[^=])`,
)

func sniffEffectsCSharp(content string) []EffectMatch {
	if content == "" {
		return nil
	}
	headers := scanCSharpFuncHeaders(content)
	var out []EffectMatch
	out = appendCSharpMatches(out, content, headers, csharpHTTPRe, EffectHTTPOut, "HttpClient/WebClient/RestSharp", 1.0)
	out = appendCSharpMatches(out, content, headers, csharpDBReadRe, EffectDBRead, "ef/dapper/ado.read", 0.85)
	out = appendCSharpMatches(out, content, headers, csharpDBWriteRe, EffectDBWrite, "ef/dapper/ado.write", 0.85)
	out = appendCSharpMatches(out, content, headers, csharpFSReadRe, EffectFSRead, "File.Read/Directory.Get", 1.0)
	out = appendCSharpMatches(out, content, headers, csharpFSWriteRe, EffectFSWrite, "File.Write/Directory.Create", 1.0)
	out = appendCSharpMatches(out, content, headers, csharpProcessRe, EffectFSWrite, "Process.Start", 0.9)
	out = appendCSharpMatches(out, content, headers, csharpMutationRe, EffectMutation, "this.field=", 0.7)
	return out
}

func scanCSharpFuncHeaders(content string) []funcHeader {
	var hs []funcHeader
	add := func(name string, off int) {
		if name == "" || csharpControlKeyword(name) {
			return
		}
		hs = append(hs, funcHeader{Line: lineOfOffset(content, off), Name: name})
	}
	for _, m := range csharpMethodHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		add(content[m[2]:m[3]], m[0])
	}
	for _, m := range csharpCtorHeaderRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		add(content[m[2]:m[3]], m[0])
	}
	return hs
}

// csharpControlKeyword rejects control-flow tokens that the conservative
// method regex can accidentally match.
func csharpControlKeyword(s string) bool {
	switch s {
	case "if", "else", "for", "foreach", "while", "switch", "catch", "try", "do", "return", "throw", "new", "lock", "using", "fixed", "checked", "unchecked", "yield", "await", "this", "base":
		return true
	}
	return false
}

func appendCSharpMatches(out []EffectMatch, content string, headers []funcHeader, re *regexp.Regexp, eff Effect, sink string, conf float64) []EffectMatch {
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
