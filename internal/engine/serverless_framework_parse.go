// Serverless Framework manifest parser — #3519.
//
// A small indentation-aware line parser for serverless.yml. It deliberately
// avoids a full YAML library (the package's other IaC passes — iac_sns_edges,
// event_bus_edges — are regex/line based for the same reason: the engine pass
// receives raw bytes and must stay dependency-light) and extracts exactly the
// subset the topology join needs: service name, provider runtime/region, and
// the functions block with each function's handler and events.
package engine

import (
	"strings"
)

// slsManifest is the parsed subset of a serverless.yml.
type slsManifest struct {
	service   string
	runtime   string
	region    string
	functions []slsFunction
}

// slsFunction is one entry under `functions:`.
type slsFunction struct {
	name    string
	handler string
	events  []slsEvent
}

// slsEvent is one entry under a function's `events:` list. `kind` is the event
// type (http, httpApi, sqs, sns, stream, kinesis, schedule). For http events
// method+path are set; for queue/stream/schedule events `source` carries the
// ARN / queue name / rate-or-cron expression.
type slsEvent struct {
	kind   string
	method string
	path   string
	source string
}

// indentOf returns the number of leading spaces on a line (tabs are invalid in
// YAML indentation, so we count spaces only).
func indentOf(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' {
			n++
		} else {
			break
		}
	}
	return n
}

// splitKeyValue splits a `key: value` line into (key, value), trimming quotes
// and inline comments from the value. Returns ok=false when there is no colon.
func splitKeyValue(line string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "- ")
	idx := strings.Index(trimmed, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(trimmed[:idx])
	value = strings.TrimSpace(trimmed[idx+1:])
	value = stripInlineComment(value)
	value = strings.Trim(value, `"'`)
	return key, value, true
}

// stripInlineComment removes a trailing ` # comment` from a scalar value while
// preserving `#` that appears inside a quoted string.
func stripInlineComment(v string) string {
	if v == "" {
		return v
	}
	inSingle, inDouble := false, false
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && i > 0 && (v[i-1] == ' ' || v[i-1] == '\t') {
				return strings.TrimSpace(v[:i])
			}
		}
	}
	return strings.TrimSpace(v)
}

// parseServerlessYML extracts the topology-relevant subset of a serverless.yml.
func parseServerlessYML(src string) slsManifest {
	lines := strings.Split(src, "\n")
	var m slsManifest

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if indentOf(line) != 0 {
			continue
		}
		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		switch key {
		case "service":
			if value != "" {
				m.service = value
			}
		case "provider":
			parseProviderBlock(lines, i+1, &m)
		case "functions":
			m.functions = parseFunctionsBlock(lines, i+1)
		}
	}
	return m
}

// parseProviderBlock reads runtime/region from the `provider:` mapping. `start`
// is the first line after the `provider:` header. Stops at the next top-level
// (indent 0) key.
func parseProviderBlock(lines []string, start int, m *slsManifest) {
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if indentOf(line) == 0 {
			return
		}
		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		switch key {
		case "runtime":
			if value != "" {
				m.runtime = value
			}
		case "region":
			if value != "" {
				m.region = value
			}
		}
	}
}

// parseFunctionsBlock reads each function entry under `functions:`. `start` is
// the first line after the `functions:` header. A function header is the first
// indentation level inside the block; its body (handler, events) is more deeply
// indented. Stops at the next top-level key.
func parseFunctionsBlock(lines []string, start int) []slsFunction {
	var fns []slsFunction
	// Determine the function-key indent from the first non-blank child line.
	fnIndent := -1
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		ind := indentOf(line)
		if ind == 0 {
			break // hit next top-level key before any function
		}
		fnIndent = ind
		break
	}
	if fnIndent < 0 {
		return fns
	}

	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		ind := indentOf(line)
		if ind == 0 {
			break // next top-level key — functions block done
		}
		if ind != fnIndent {
			continue // deeper line consumed by a function body scan below
		}
		key, _, ok := splitKeyValue(line)
		if !ok || key == "" {
			continue
		}
		fn := slsFunction{name: key}
		// Find the end of this function's body: up to the next line at <= fnIndent.
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if indentOf(lines[j]) <= fnIndent {
				end = j
				break
			}
		}
		parseFunctionBody(lines[i+1:end], &fn)
		fns = append(fns, fn)
	}
	return fns
}

// parseFunctionBody reads handler + events from a single function's body lines.
func parseFunctionBody(body []string, fn *slsFunction) {
	for i := 0; i < len(body); i++ {
		line := body[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		switch key {
		case "handler":
			if value != "" {
				fn.handler = value
			}
		case "events":
			eventsIndent := indentOf(line)
			// Events list lives below the `events:` line at deeper indent.
			end := len(body)
			for j := i + 1; j < len(body); j++ {
				if strings.TrimSpace(body[j]) == "" {
					continue
				}
				if indentOf(body[j]) <= eventsIndent {
					end = j
					break
				}
			}
			fn.events = parseEventsBlock(body[i+1 : end])
			i = end - 1
		}
	}
}

// parseEventsBlock reads the `- <type>:` list items of an events block.
func parseEventsBlock(lines []string) []slsEvent {
	var events []slsEvent
	// Each list item starts with a `-` at the shallowest indent in the block.
	itemIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "-") {
			itemIndent = indentOf(line)
			break
		}
	}
	if itemIndent < 0 {
		return events
	}

	// Split into per-item line groups.
	var itemStarts []int
	for idx, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		t := strings.TrimSpace(line)
		if indentOf(line) == itemIndent && strings.HasPrefix(t, "-") {
			itemStarts = append(itemStarts, idx)
		}
	}
	for k, s := range itemStarts {
		e := len(lines)
		if k+1 < len(itemStarts) {
			e = itemStarts[k+1]
		}
		if ev, ok := parseEventItem(lines[s:e], itemIndent); ok {
			events = append(events, ev)
		}
	}
	return events
}

// parseEventItem parses one `- <type>: ...` event list item.
func parseEventItem(lines []string, itemIndent int) (slsEvent, bool) {
	if len(lines) == 0 {
		return slsEvent{}, false
	}
	// First line: `- http:` or `- http: GET /users` or `- schedule: rate(...)`.
	first := strings.TrimSpace(lines[0])
	first = strings.TrimPrefix(first, "-")
	first = strings.TrimSpace(first)
	idx := strings.Index(first, ":")
	if idx < 0 {
		return slsEvent{}, false
	}
	kind := strings.TrimSpace(first[:idx])
	inlineVal := stripInlineComment(strings.TrimSpace(first[idx+1:]))
	inlineVal = strings.Trim(inlineVal, `"'`)

	ev := slsEvent{kind: kind}

	switch kind {
	case "http", "httpApi":
		// Inline short form: `http: GET /users` or `http: 'GET /users'`.
		if inlineVal != "" && inlineVal != "{" {
			parts := strings.Fields(inlineVal)
			if len(parts) >= 2 {
				ev.method = parts[0]
				ev.path = parts[1]
			} else if len(parts) == 1 {
				ev.path = parts[0]
			}
		}
		// Object form: method:/path: on following indented lines.
		for _, l := range lines[1:] {
			k, v, ok := splitKeyValue(l)
			if !ok {
				continue
			}
			switch k {
			case "method":
				ev.method = strings.ToUpper(v)
			case "path":
				ev.path = v
			}
		}
		return ev, ev.path != "" || ev.method != ""

	case "schedule":
		if inlineVal != "" {
			ev.source = inlineVal
		}
		for _, l := range lines[1:] {
			k, v, ok := splitKeyValue(l)
			if !ok {
				continue
			}
			switch k {
			case "rate", "expression":
				ev.source = v
			}
		}
		return ev, ev.source != ""

	case "sqs", "sns", "stream", "kinesis":
		if inlineVal != "" {
			ev.source = inlineVal
		}
		for _, l := range lines[1:] {
			k, v, ok := splitKeyValue(l)
			if !ok {
				continue
			}
			switch k {
			case "arn", "queueName", "topicName", "streamName":
				if v != "" {
					ev.source = v
				}
			}
		}
		return ev, ev.source != ""
	}

	return slsEvent{}, false
}
