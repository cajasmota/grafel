package links

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/cajasmota/archigraph/internal/external"
)

// lineNumberSuffix matches labels ending in `:<digits>` (e.g.
// `error_handling:try_catch:110`). Line numbers are pure positional
// coincidence across repos and produce noise-only matches in the
// cross-repo label channel — see issue #511.
var lineNumberSuffix = regexp.MustCompile(`:\d+$`)

// destructuredTupleRE matches React useState destructure patterns of
// the form `[varname, setvarname]` (already lowercased by the time we
// see them). These are structural noise — universal React patterns,
// not architectural concepts. See #565.
var destructuredTupleRE = regexp.MustCompile(`^\[[a-z][a-z0-9_]*,\s*set[a-z][a-z0-9_]*\]$`)

// destructuredObjectRE matches inline JS object destructure patterns
// like `{ data }`, `{ id }`, `{ url, fields }`. These are pseudo-names
// produced by extractors when an argument is an object literal
// destructure — they describe the call shape, not an entity. #565.
var destructuredObjectRE = regexp.MustCompile(`^\{\s*[a-z_][a-z0-9_, ]*\s*\}$`)

// destructuredArrayRE matches inline JS array destructure patterns
// like `[year, month, day]` (non-setX form). These are call-shape
// pseudo-names, not architectural identifiers. #565.
var destructuredArrayRE = regexp.MustCompile(`^\[\s*[a-z_][a-z0-9_, ]*\s*\]$`)

// Note: a blanket prefix filter (e.g. drop everything starting with
// `is`, `on`, `handle`, `use`, `get`, `set`) would over-filter — real
// architectural cross-stack identifiers like `createInspectionDeficiency`
// (backend DRF action ↔ frontend RTK Query hook) carry actual signal.
// Instead, the universal-pattern names below are listed literally.

// leadingNonAlpha matches any leading non-alphabetic characters used for
// the length-after-strip filter (#565).
var leadingNonAlpha = regexp.MustCompile(`^[^a-zA-Z]+`)

// builtinLabelStopList covers stdlib idioms, JS array/object/string
// methods, DOM/timer/React hook names, and Python builtins. Cross-repo
// matches on these are pure coincidence — every codebase has them. See
// issue #565.
var builtinLabelStopList = map[string]struct{}{
	// JS Array methods
	"filter": {}, "map": {}, "reduce": {}, "foreach": {}, "some": {}, "every": {},
	"sort": {}, "join": {}, "split": {}, "slice": {}, "splice": {}, "push": {},
	"pop": {}, "shift": {}, "unshift": {}, "concat": {}, "includes": {},
	"indexof": {}, "find": {}, "findindex": {}, "flat": {}, "flatmap": {},
	"fill": {}, "copywithin": {},
	// JS Object methods
	"keys": {}, "values": {}, "entries": {}, "assign": {}, "freeze": {},
	"create": {}, "fromentries": {},
	// JS String methods
	"replace": {}, "trim": {}, "padstart": {}, "padend": {}, "tolowercase": {},
	"touppercase": {}, "charat": {}, "charcodeat": {}, "codepointat": {},
	"startswith": {}, "endswith": {},
	// JS Promise
	"then": {}, "catch": {}, "finally": {}, "resolve": {}, "reject": {},
	"all": {}, "race": {}, "allsettled": {}, "any": {},
	// JS Math / Number
	"min": {}, "max": {}, "round": {}, "floor": {}, "ceil": {}, "abs": {},
	"pow": {}, "sqrt": {}, "log": {}, "sign": {},
	// DOM events
	"addeventlistener": {}, "removeeventlistener": {}, "dispatchevent": {},
	"queryselector": {}, "queryselectorall": {}, "getelementbyid": {},
	"createelement": {}, "appendchild": {}, "removechild": {},
	// Timer
	"cleartimeout": {}, "settimeout": {}, "setinterval": {}, "clearinterval": {},
	"requestanimationframe": {}, "cancelanimationframe": {},
	// React hooks
	"usestate": {}, "useeffect": {}, "usecallback": {}, "usememo": {},
	"useref": {}, "usecontext": {}, "usereducer": {}, "uselayouteffect": {},
	// Python builtins
	"len": {}, "str": {}, "int": {}, "float": {}, "bool": {}, "dict": {},
	"tuple": {}, "frozenset": {}, "range": {}, "enumerate": {}, "zip": {},
	"sorted": {}, "reversed": {}, "open": {}, "print": {}, "isinstance": {},
	"hasattr": {}, "getattr": {}, "setattr": {}, "delattr": {}, "repr": {},
	"vars": {}, "dir": {}, "callable": {},
	// Common stdlib idioms
	"encode": {}, "decode": {}, "parse": {}, "stringify": {}, "format": {},
	"tostring": {}, "valueof": {}, "hashcode": {},
	// Date/Number/JSON methods universally on every project.
	"getdate": {}, "getmonth": {}, "getfullyear": {}, "gettime": {},
	"getday": {}, "gethours": {}, "getminutes": {}, "getseconds": {},
	"tofixed": {}, "toisostring": {}, "tolocaledatestring": {},
	"tolocalestring": {}, "tolocaletimestring": {}, "tojson": {},
	"parseint": {}, "parsefloat": {}, "isnan": {}, "isarray": {},
	"isfinite": {}, "isinteger": {}, "isnull": {}, "isundefined": {},
	"localecompare": {}, "lastindexof": {}, "preventdefault": {},
	"stoppropagation": {}, "randomuuid": {}, "tojson_string": {},
	"reverse": {}, "match": {}, "matches": {}, "test": {}, "warn": {},
	"info": {}, "debug": {}, "trace": {}, "assert": {},
	// React/RTK Query/React-Query universal hook & lifecycle names.
	"usemutation": {}, "usequery": {}, "usequeryclient": {},
	"useinfinitequery": {}, "useprevious": {}, "useeffectdebugger": {},
	"createcontext": {}, "memo": {}, "forwardref": {}, "fragment": {},
	"mutate": {}, "mutateasync": {}, "invalidate": {}, "refetch": {},
	"setquerydata": {}, "getquerydata": {},
	// Event handler universal names.
	"oncreate": {}, "onerror": {}, "onprogress": {}, "onscroll": {},
	"onsettled": {}, "onsuccess": {}, "onclose": {}, "onopen": {},
	"onchange": {}, "onclick": {}, "onsubmit": {}, "onblur": {},
	"onfocus": {}, "onkeydown": {}, "onkeyup": {}, "onkeypress": {},
	"onmousedown": {}, "onmouseup": {}, "onmouseover": {}, "onmouseout": {},
	"onload": {}, "onunload": {}, "ondrop": {}, "ondrag": {},
	"handleblur": {}, "handlesubmit": {}, "handlechange": {},
	"handleclick": {}, "handleclose": {}, "handleopen": {},
	"handleerror": {}, "handlesuccess": {},
	// Single-letter
	"a": {}, "b": {}, "c": {}, "d": {}, "e": {}, "f": {}, "g": {}, "h": {},
	"i": {}, "j": {}, "k": {}, "l": {}, "m": {}, "n": {}, "o": {}, "p": {},
	"q": {}, "r": {}, "s": {}, "t": {}, "u": {}, "v": {}, "w": {}, "x": {},
	"y": {}, "z": {},
}

// genericFieldStopList covers universal field/var names that are
// reflexively reused in every codebase and carry no architectural
// signal in the cross-repo label channel. See #565.
//
// Borderline note: `auth` is intentionally KEPT (kept out of this list)
// because it is the architectural concept users care about. `role` is
// dropped because it is overloaded as a UI/permission field name in
// the user's corpus.
var genericFieldStopList = map[string]struct{}{
	"body": {}, "content": {}, "count": {}, "current": {}, "data": {},
	"date": {}, "day": {}, "description": {}, "email": {}, "enabled": {},
	"error": {}, "errors": {}, "field": {}, "fields": {}, "file": {},
	"files": {}, "form": {}, "formdata": {}, "header": {}, "headers": {},
	"height": {}, "html": {}, "id": {}, "image": {}, "images": {},
	"index": {}, "info": {}, "item": {}, "items": {}, "key": {}, "keys": {},
	"label": {}, "labels": {}, "length": {}, "level": {}, "line": {},
	"lines": {}, "list": {}, "loading": {}, "message": {}, "messages": {},
	"meta": {}, "mode": {}, "name": {}, "names": {}, "node": {}, "nodes": {},
	"options": {}, "page": {}, "pages": {}, "params": {}, "parent": {},
	"password": {}, "path": {}, "payload": {}, "position": {}, "query": {},
	"range": {}, "ref": {}, "refs": {}, "request": {}, "result": {},
	"results": {}, "role": {}, "root": {}, "route": {}, "routes": {},
	"row": {}, "rows": {}, "schema": {}, "schemas": {}, "score": {},
	"search": {}, "section": {}, "size": {}, "source": {}, "state": {},
	"states": {}, "status": {}, "step": {}, "style": {}, "styles": {},
	"success": {}, "tag": {}, "tags": {}, "target": {}, "text": {},
	"time": {}, "title": {}, "today": {}, "total": {}, "type": {},
	"types": {}, "url": {}, "urls": {}, "user": {}, "users": {},
	"value": {}, "values": {}, "version": {}, "view": {}, "views": {},
	"width": {},
	// Additional universal boolean/state vars + react conventions.
	"isactive": {}, "isdirty": {}, "isloading": {}, "isselected": {},
	"isvalid": {}, "isopen": {}, "isvisible": {}, "isdisabled": {},
	"isempty": {}, "isdark": {}, "isnyc": {}, "iscat5": {},
	"empty": {}, "emptymessage": {}, "allempty": {}, "available": {},
	"selected": {}, "existing": {}, "remaining": {}, "filtered": {},
	"normalized": {}, "found": {}, "saved": {}, "resolved": {},
	"mapped": {}, "mapping": {}, "merged": {}, "grouped": {}, "groupby": {},
	"actions": {}, "permission": {}, "permissions": {}, "navigation": {},
	"detail": {}, "extension": {}, "filename": {}, "from": {}, "store": {},
	"start": {}, "end": {}, "last": {}, "newid": {}, "initial": {},
	"theme": {}, "variant": {}, "color": {}, "palette": {}, "reason": {},
	"note": {}, "memo": {},
	// Additional pure-UI / non-architectural names.
	"badgestyle": {}, "buttonstyle": {}, "spinnerstyle": {}, "iconcolor": {},
	"tabs": {}, "tabitems": {}, "year": {}, "readonly": {}, "remove": {},
	"decodeuricomponent": {}, "encodeuricomponent": {}, "getstate": {},
	"createmutation": {}, "updatemutation": {}, "deletemutation": {},
	"changeddeps": {}, "previousdeps": {}, "initializedref": {},
	"keyname": {}, "rawname": {}, "lastsegment": {}, "typekey": {},
	"datelabel": {}, "datestr": {}, "dayofweek": {}, "formatted": {},
	"formatteddate": {}, "formatdate": {}, "formattime": {},
	"resultlabel": {}, "statuslabel": {}, "contextvalue": {},
	"activekey": {}, "destination": {}, "displayname": {}, "fullname": {},
	"enddate": {}, "startdate": {}, "totalcount": {}, "storage_key": {},
	"uploadedfiles": {}, "uploaddata": {}, "uploadresponse": {},
	// Universal can*/has*/should* booleans without architectural anchor.
	"caninteract": {}, "cansave": {}, "canedit": {}, "candelete": {},
	"canview": {}, "canread": {}, "canwrite": {}, "canupdate": {},
	"updateprogress": {}, "filteredgroups": {},
}

// isHardenedNoise centralises the #565 noise filters that run AFTER the
// existing #511 normalisation. Returns true if the (already-normalised)
// label should be suppressed.
func isHardenedNoise(label string) bool {
	if label == "" {
		return true
	}
	// 1. Stdlib / builtin / single-letter universal stop-list.
	if _, ok := builtinLabelStopList[label]; ok {
		return true
	}
	// 2. React useState destructure tuples: [name, setname] + generic
	// inline object/array destructure pseudo-names: `{ data }`,
	// `[year, month, day]`.
	if destructuredTupleRE.MatchString(label) {
		return true
	}
	if destructuredObjectRE.MatchString(label) {
		return true
	}
	if destructuredArrayRE.MatchString(label) {
		return true
	}
	// 3. Generic universal field/var name stop-list.
	if _, ok := genericFieldStopList[label]; ok {
		return true
	}
	// 4. Length-after-strip filter: <4 alpha chars carry no signal.
	stripped := leadingNonAlpha.ReplaceAllString(label, "")
	if len(stripped) < 4 {
		return true
	}
	// 5. Known npm/pip/maven package roots: redundant with the import
	// channel (#566) — both halves of a scoped path get checked.
	if external.IsKnownExternalPackage(label) {
		return true
	}
	if idx := strings.Index(label, "/"); idx > 0 {
		// Scoped: @scope/name — check both halves.
		head := strings.TrimPrefix(label[:idx], "@")
		tail := label[idx+1:]
		if head != "" && external.IsKnownExternalPackage(head) {
			return true
		}
		if tail != "" && external.IsKnownExternalPackage(tail) {
			return true
		}
	}
	return false
}

// labelStopList is the set of generic names that should never produce
// a shared-label match — they are too common across codebases to carry
// any signal.
var labelStopList = map[string]bool{
	"get": true, "set": true, "list": true, "create": true, "update": true, "delete": true,
	"index": true, "view": true, "show": true, "init": true, "main": true, "run": true,
	"process": true, "handle": true, "handler": true, "helper": true, "util": true, "utils": true,
	"config": true, "settings": true, "factory": true, "manager": true, "service": true,
	"module": true, "app": true, "client": true, "server": true, "request": true, "response": true,
	"error": true, "exception": true, "result": true, "data": true, "value": true, "item": true,
	"entry": true, "node": true, "field": true, "model": true, "schema": true, "base": true,
}

// suffixStrip is the ordered list of suffix tokens removed before
// normalisation. Order matters — longer entries first to avoid partial
// matches. Both snake-case (`_viewset`) and CamelCase (`Service`)
// variants are included.
var suffixStrip = []string{
	"_viewset", "_serializer", "_service", "_queries", "_dto", "_interface",
	"viewset", "serializer", "service", "queries", "dto", "interface",
	"Stub", "Service", "Client", "Manager", "Handler",
}

// thresholds for P2.
const (
	labelLinkThreshold      = 0.5
	labelCandidateThreshold = 0.2
	labelEmissionCap        = 6
)

// normalizeLabel returns the canonical lower-cased identifier used for
// cross-repo matching. Empty string means the label was filtered out
// (stop-listed or stripped to nothing).
func normalizeLabel(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return ""
	}
	// Drop line-number-keyed labels (see #511). Any label whose final
	// segment is a bare integer (`foo:bar:110`, `route:42`) is a
	// positional artefact, not a structural identifier, and cross-repo
	// matches on it are pure coincidence.
	if lineNumberSuffix.MatchString(s) {
		return ""
	}
	// Strip suffixes (case-sensitive for the CamelCase variants;
	// lowercase variants apply after we lowercase below). We do two
	// passes: one CamelCase, one lowercase.
	for _, suf := range suffixStrip {
		if isUpperSuffix(suf) {
			if strings.HasSuffix(s, suf) && len(s) > len(suf) {
				s = s[:len(s)-len(suf)]
			}
		}
	}
	s = strings.ToLower(s)
	for _, suf := range suffixStrip {
		ls := strings.ToLower(suf)
		if strings.HasSuffix(s, ls) && len(s) > len(ls) {
			s = s[:len(s)-len(ls)]
		}
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if labelStopList[s] {
		return ""
	}
	// #565: hardened stop-lists (stdlib/builtin, destructured tuples,
	// generic fields, short labels, npm packages).
	if isHardenedNoise(s) {
		return ""
	}
	return s
}

func isUpperSuffix(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= 'A' && c <= 'Z'
}

// kindCompat returns the kind-compatibility multiplier per spec.
func kindCompat(a, b string) float64 {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la == lb {
		return 1.0
	}
	// Class ↔ interface bridge (cross-stack: Java interface ↔ Python class).
	classy := map[string]bool{"class": true, "struct": true, "type": true}
	ifaceLike := map[string]bool{"interface": true, "trait": true, "protocol": true}
	if (classy[la] && ifaceLike[lb]) || (classy[lb] && ifaceLike[la]) {
		return 0.85
	}
	return 0.5
}

// runLabelPass implements P2.
func runLabelPass(graphs []repoGraph, paths Paths, rejects map[string]bool) (PassResult, error) {
	res := PassResult{Pass: "label"}
	if len(graphs) < 2 {
		// Still need to write empty output to keep idempotency clean.
		_, _, err := replaceByMethod(paths.Links, newMethodSet(MethodLabelMatch), nil, rejects)
		if err != nil {
			return res, err
		}
		_, _, err = replaceByMethod(paths.Candidates, newMethodSet(MethodLabelMatch), nil, rejects)
		return res, err
	}

	// Index: normalized → repo → []entityNode (with original index).
	type ent struct {
		repo string
		node entityNode
	}
	byLabel := map[string]map[string][]ent{}

	totalEntities := 0
	for _, g := range graphs {
		for _, e := range g.Entities {
			totalEntities++
			n := normalizeLabel(e.Name)
			if n == "" {
				continue
			}
			if _, ok := byLabel[n]; !ok {
				byLabel[n] = map[string][]ent{}
			}
			byLabel[n][g.Repo] = append(byLabel[n][g.Repo], ent{repo: g.Repo, node: e})
		}
	}
	corpusSize := totalEntities
	if corpusSize < 2 {
		corpusSize = 2
	}

	now := discoveredAt()
	var freshLinks, freshCands []Link

	// Stable iteration order over labels.
	labels := make([]string, 0, len(byLabel))
	for k := range byLabel {
		labels = append(labels, k)
	}
	sort.Strings(labels)

	// seenPair tracks (src,tgt) pairs already emitted by this pass run so
	// that a noisy label set cannot produce duplicate links for the same
	// repo pair. Keyed by ordered "src|tgt" — keeps the loop O(unique
	// pairs) instead of O(labels × repo_pairs).
	seenPair := map[string]bool{}

	for _, label := range labels {
		repos := byLabel[label]
		if len(repos) < 2 {
			continue
		}
		var totalOccur int
		for _, ents := range repos {
			totalOccur += len(ents)
		}
		idf := math.Log(float64(corpusSize+1)/float64(totalOccur+1)) / math.Log(float64(corpusSize+1))
		if idf < 0 {
			idf = 0
		}

		// Pairwise across repos. Stable order.
		repoNames := make([]string, 0, len(repos))
		for r := range repos {
			repoNames = append(repoNames, r)
		}
		sort.Strings(repoNames)

		emitted := 0
		for i := 0; i < len(repoNames) && emitted < labelEmissionCap; i++ {
			for j := i + 1; j < len(repoNames) && emitted < labelEmissionCap; j++ {
				ra, rb := repoNames[i], repoNames[j]
				if ra == rb {
					// Belt-and-suspenders self-pair guard.
					continue
				}
				// Pick best entity per repo: prefer non-stoplisted name length.
				ea := repos[ra][0].node
				eb := repos[rb][0].node
				kc := kindCompat(ea.Kind, eb.Kind)
				raw := idf * kc
				if raw < labelCandidateThreshold {
					continue
				}
				sa := entityKey(ra, ea.ID)
				sb := entityKey(rb, eb.ID)
				src, tgt := orderEndpoints(sa, sb)
				pairKey := src + "|" + tgt
				if seenPair[pairKey] {
					continue
				}
				seenPair[pairKey] = true
				conf := ScoreLabel(raw)
				link := Link{
					ID:           MakeID(src, tgt, MethodLabelMatch),
					Source:       src,
					Target:       tgt,
					Relation:     RelationSharedLabel,
					Method:       MethodLabelMatch,
					Confidence:   conf,
					Channel:      nil,
					Identifier:   strPtr(label),
					DiscoveredAt: now,
				}
				if raw >= labelLinkThreshold {
					freshLinks = append(freshLinks, link)
				} else {
					link.Reason = "label_match below threshold"
					freshCands = append(freshCands, link)
				}
				emitted++
			}
		}
	}

	added, skipped, err := replaceByMethod(paths.Links, newMethodSet(MethodLabelMatch), freshLinks, rejects)
	if err != nil {
		return res, err
	}
	cAdded, cSkipped, err := replaceByMethod(paths.Candidates, newMethodSet(MethodLabelMatch), freshCands, rejects)
	if err != nil {
		return res, err
	}
	res.LinksAdded = added
	res.Candidates = cAdded
	res.Skipped = skipped + cSkipped
	return res, nil
}
