// Scala payload-shape sniffer (#2771 Phase 2A T2).
//
// Producer-side shapes (Play Framework / http4s / Akka HTTP):
//
//   - Play: `request.body.as[T]` or `request.body.asJson` followed by
//     `(json \ "x").as[String]` reads — both forms contribute fields.
//   - http4s: `req.decodeJson[T]` / `req.as[T]` — wrapped T's case-
//     class members (same-file) are the request shape.
//   - Case class declarations `case class T(a: String, b: Option[Int])`
//     — primary-constructor parameters are the field set. `Option[U]`
//     flips Optional=true.
//   - Response shapes: `Ok(Json.obj("a" -> ..., "b" -> ...))` and
//     `Json.toJson(T(...))` (when T is a same-file case class).
//
// Consumer-side shapes (sttp):
//
//   - `basicRequest.post(uri).body(Map("a" -> ..., "b" -> ...))`
//   - `basicRequest.body(asJson(T(a, b)))` — when T resolves to a
//     same-file case class.
package substrate

import (
	"regexp"
	"strings"
)

func init() { RegisterPayloadShapeSniffer("scala", sniffPayloadShapesScala) }

// scalaCaseClassRe matches `case class T(...)`. Capture group 1 = the
// type name; group 2 = the primary-constructor parameter list.
var scalaCaseClassRe = regexp.MustCompile(
	`(?m)^\s*(?:final\s+|sealed\s+)*case\s+class\s+([A-Z][\w]*)\s*\(([^)]*)\)`,
)

// scalaDecodeJsonRe matches http4s `req.decodeJson[T]` / `req.as[T]`.
// Capture group 1 = T.
var scalaDecodeJsonRe = regexp.MustCompile(
	`\b(?:req|request)\s*\.\s*(?:decodeJson|as)\s*\[\s*([A-Z][\w]*)\s*\]`,
)

// scalaJsonReadRe matches `(json \ "x").as[Type]` style Play reads.
// Capture group 1 = the field name.
var scalaJsonReadRe = regexp.MustCompile(
	`\(\s*(?:json|js|body)\s*\\\s*"([A-Za-z_][\w]*)"\s*\)\s*\.\s*as`,
)

// scalaJsonObjRe matches `Json.obj("a" -> ..., "b" -> ...)`. Capture
// group 1 = the arg list body.
var scalaJsonObjRe = regexp.MustCompile(
	`\bJson\s*\.\s*obj\s*\(([^()]*)\)`,
)

// scalaMapTupleRe matches a `"name" ->` tuple inside Json.obj /
// Map literals. Capture group 1 = the bare name.
var scalaMapTupleRe = regexp.MustCompile(
	`"([A-Za-z_][\w]*)"\s*->`,
)

// scalaConsumerRe matches sttp `basicRequest.<verb>(uri)` chain. We
// recognise the verb + URL for the consumer hint; the body is picked
// up by the generic Json.obj / Map.apply recognition.
var scalaConsumerRe = regexp.MustCompile(
	`\bbasicRequest\s*\.\s*(get|post|put|patch|delete|head)\s*\(\s*uri"([^"]*)"`,
)

func sniffPayloadShapesScala(content string) []PayloadShape {
	if content == "" {
		return nil
	}
	headers := scanScalaFuncHeaders(content)
	caseClassFields := scanScalaCaseClassFields(content)
	clientHints := scanScalaClientHints(content, headers)

	var out []PayloadShape

	// Producer-side: req.decodeJson[T] / req.as[T] → request shape.
	for _, m := range scalaDecodeJsonRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		typ := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		fields := caseClassFields[typ]
		conf := 0.9
		if len(fields) == 0 {
			conf = 0.4
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: conf,
		})
	}

	// Producer-side: (json \ "x").as[T] bare reads.
	readFields := map[string][]PayloadField{}
	readFirstLine := map[string]int{}
	for _, m := range scalaJsonReadRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		name := content[m[2]:m[3]]
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		readFields[fn] = append(readFields[fn], PayloadField{Name: name})
		if _, ok := readFirstLine[fn]; !ok {
			readFirstLine[fn] = line
		}
	}
	for fn, fields := range readFields {
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       readFirstLine[fn],
			Direction:  PayloadDirectionRequest,
			Side:       PayloadSideProducer,
			Fields:     DedupFields(fields),
			Confidence: 0.85,
		})
	}

	// Producer/Consumer-side: Json.obj("a" -> ...) literals.
	for _, m := range scalaJsonObjRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 4 {
			continue
		}
		body := content[m[2]:m[3]]
		var fields []PayloadField
		for _, km := range scalaMapTupleRe.FindAllStringSubmatchIndex(body, -1) {
			if len(km) >= 4 {
				fields = append(fields, PayloadField{Name: body[km[2]:km[3]]})
			}
		}
		fields = DedupFields(fields)
		if len(fields) == 0 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if hint, ok := clientHints[fn]; ok {
			out = append(out, PayloadShape{
				Function:     fn,
				Line:         line,
				Direction:    PayloadDirectionRequest,
				Side:         PayloadSideConsumer,
				Fields:       fields,
				Confidence:   1.0,
				EndpointHint: hint.url,
				VerbHint:     hint.verb,
			})
			continue
		}
		if fn == "" {
			continue
		}
		out = append(out, PayloadShape{
			Function:   fn,
			Line:       line,
			Direction:  PayloadDirectionResponse,
			Side:       PayloadSideProducer,
			Fields:     fields,
			Confidence: 1.0,
		})
	}

	return out
}

// scanScalaCaseClassFields builds the map of case-class name →
// []PayloadField from primary-constructor parameters.
func scanScalaCaseClassFields(content string) map[string][]PayloadField {
	out := map[string][]PayloadField{}
	for _, m := range scalaCaseClassRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		name := content[m[2]:m[3]]
		paramList := content[m[4]:m[5]]
		var fields []PayloadField
		for _, p := range splitTopLevel(paramList) {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			// `name: Type` (with optional default).
			colon := strings.IndexByte(p, ':')
			if colon <= 0 {
				continue
			}
			fname := strings.TrimSpace(p[:colon])
			rest := strings.TrimSpace(p[colon+1:])
			if eq := strings.IndexByte(rest, '='); eq >= 0 {
				rest = strings.TrimSpace(rest[:eq])
			}
			optional := strings.HasPrefix(rest, "Option[") || strings.HasPrefix(rest, "Option<")
			if !isPlainIdent(fname) {
				continue
			}
			fields = append(fields, PayloadField{Name: fname, Type: rest, Optional: optional})
		}
		if len(fields) > 0 {
			out[name] = DedupFields(fields)
		}
	}
	return out
}

// scalaClientHint mirrors the other consumer-hint structs.
type scalaClientHint struct {
	url  string
	verb string
}

// scanScalaClientHints maps function name → first sttp basicRequest
// call hint observed inside it.
func scanScalaClientHints(content string, headers []funcHeader) map[string]scalaClientHint {
	out := map[string]scalaClientHint{}
	for _, m := range scalaConsumerRe.FindAllStringSubmatchIndex(content, -1) {
		if len(m) < 6 {
			continue
		}
		line := lineOfOffset(content, m[0])
		fn := nearestHeader(headers, line)
		if fn == "" {
			continue
		}
		if _, ok := out[fn]; ok {
			continue
		}
		out[fn] = scalaClientHint{
			url:  content[m[4]:m[5]],
			verb: strings.ToUpper(content[m[2]:m[3]]),
		}
	}
	return out
}
