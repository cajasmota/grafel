package extractor

import (
	"strings"

	"github.com/cajasmota/archigraph/internal/types"
)

// template_render.go — shared cross-language helpers for the server-side
// view-layer topology (epic #3628). It mirrors the error-flow model in
// exception_flow.go.
//
// The view-layer capability answers two questions a rewrite needs to keep the
// presentation contract intact:
//
//   - "what template does this handler render?"   → a handler's outbound
//     RENDERS edge.
//   - "who renders users/list.html?"               → a template's inbound
//     RENDERS edges.
//
// To make a template rendered by two different handlers converge on ONE graph
// node, every language extractor emits:
//
//   - one SCOPE.Template / subtype="template" entity per distinct (normalized)
//     template name, with a SYNTHETIC constant SourceFile (TemplateSourceFile)
//     so EntityRecord.ComputeID(SourceFile+Kind+Name) collapses identical
//     template names — even across files, languages and frameworks — into a
//     single node. A 'users/list.html' rendered by a Flask view and an Express
//     route therefore share one node, and that node's inbound-RENDERS set is
//     the list of handlers that present it.
//
//   - one RENDERS edge (handler function/method → template node), carried as a
//     structural-ref ToID (TemplateTargetID) that the resolver binds via the
//     byQualifiedName exact-match tier (the entity's QualifiedName equals that
//     ToID).
//
// Precision-first / honest-partial: only STATIC template names are recorded.
// Dynamic-or-computed template names (a variable passed to render_template /
// res.render / view(), a Spring method returning a computed view name) emit NO
// edge — a single wrong RENDERS edge would mislead view-layer analysis. The
// detection of those shapes lives in each language extractor; this file owns
// only the node/edge construction so the convergence invariant is identical
// everywhere.

// TemplateSourceFile is the synthetic, constant SourceFile assigned to every
// template entity so identical template names converge to a single graph node
// under EntityRecord.ComputeID (which hashes SourceFile+Kind+Name).
const TemplateSourceFile = "<template>"

// TemplateName returns the canonical entity Name for a template. The
// "template:" prefix namespaces the node (so it never collides with a
// same-named code symbol) and keeps the human-readable logical name verbatim,
// e.g. "template:users/list.html", "template:dashboard", "template:welcome".
func TemplateName(name string) string {
	return "template:" + name
}

// TemplateTargetID returns the structural-ref ToID for a RENDERS edge pointing
// at a template entity. Shape:
//
//	scope:template:<name>
//
// This value is ALSO stored as the template entity's QualifiedName, so the
// resolver's byQualifiedName exact-match tier (internal/resolve/refs.go) binds
// the edge to that entity without any new linker code. Constant across
// languages so a Flask render_template('x.html') and an Express res.render('x')
// resolve to the same node when the normalized names match.
func TemplateTargetID(name string) string {
	return "scope:template:" + name
}

// NormalizeTemplateName canonicalizes a raw template-name token (already
// stripped of surrounding quotes by the caller) into the convergence key, or
// returns "" if the token is not a usable static name (empty, or containing
// characters that signal a dynamic / computed / interpolated name the caller
// must drop).
//
//	"users/list.html"  -> "users/list.html"   (Flask / Django / Express)
//	"users.list"       -> "users/list"        (Laravel dot-notation → slash)
//	"users/show"       -> "users/show"        (Rails / Express, no ext)
//	"./partials/nav"   -> "partials/nav"      (leading ./ stripped)
//
// Returns "" for tokens that contain interpolation / concatenation / operators
// (spaces, ${...}, "+", "#{...}", backticks, parentheses, etc.) so dynamic
// renders like render_template(name_var) or res.render(`p/${id}`) never
// fabricate a node. Laravel dot-notation is converted to slash form so
// view('users.list') and an explicit 'users/list' path converge.
func NormalizeTemplateName(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	// Reject anything that signals a dynamic / interpolated / concatenated name.
	// A static template literal is a plain path of identifier / path chars.
	for i := 0; i < len(t); i++ {
		c := t[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		switch {
		case isLetter || isDigit:
		case c == '/' || c == '.' || c == '_' || c == '-':
		default:
			return "" // space, +, $, {, #, `, (, etc. → dynamic, drop
		}
	}
	// Strip a leading "./" (relative-path noise) — but not ".." which would be
	// an unusual escape we leave to drop below.
	t = strings.TrimPrefix(t, "./")
	// Laravel / Blade dot-notation: convert dots to slashes UNLESS the token
	// looks like a path-with-extension (contains a slash already, or ends in a
	// known template extension). "users.list" -> "users/list"; "x.html" stays.
	if !strings.Contains(t, "/") && !hasTemplateExt(t) {
		t = strings.ReplaceAll(t, ".", "/")
	}
	t = strings.Trim(t, "/")
	if t == "" || strings.Contains(t, "..") {
		return ""
	}
	return t
}

// hasTemplateExt reports whether name ends in a recognized view-template file
// extension, in which case its dots are part of a path+extension and must not
// be rewritten to slashes.
func hasTemplateExt(name string) bool {
	for _, ext := range []string{
		".html", ".htm", ".jinja", ".jinja2", ".j2", ".twig", ".ejs",
		".pug", ".jade", ".hbs", ".handlebars", ".mustache", ".erb",
		".haml", ".slim", ".blade.php", ".php", ".jsp", ".thymeleaf",
		".vm", ".ftl", ".njk", ".liquid",
	} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// TemplateEntity builds the SCOPE.Template / template entity for a single
// normalized template name in the given language. The entity is deliberately
// file-agnostic (synthetic SourceFile) so it is the shared view-layer
// convergence node, and its QualifiedName equals the edge ToID so RENDERS edges
// resolve via byQualifiedName.
func TemplateEntity(name, lang string) types.EntityRecord {
	e := types.EntityRecord{
		Name:          TemplateName(name),
		QualifiedName: TemplateTargetID(name),
		Kind:          string(types.EntityKindTemplate),
		Subtype:       "template",
		Language:      lang,
		SourceFile:    TemplateSourceFile,
		StartLine:     1,
		EndLine:       1,
		Signature:     TemplateName(name),
		Properties: map[string]string{
			"template": name,
		},
	}
	e.ID = e.ComputeID()
	return e
}

// TemplateEdge is one resolved static render detected by a language extractor:
// the raw template-name token (already unquoted), the Name of the enclosing
// handler function / method, and the detector label.
type TemplateEdge struct {
	Name     string // raw template-name token (normalized internally)
	FromName string // enclosing handler/method Name; "" => file entity
	Pattern  string // detector label, e.g. "render_template", "res_render", "spring_view"
}

// EmitTemplateEdges appends, to *entities, the template entities and RENDERS
// edges for the given static render detections.
//
// entities[0] MUST be the file entity (every language extractor appends it
// first). Edges whose FromName is "" — or whose FromName has no matching host
// entity — attach to the file entity (index 0) as a conservative fallback so
// the edge is never silently dropped. Identical template names converge to one
// template entity (deduped by name) and one edge per (FromName, name) tuple.
//
// Returns the number of RENDERS edges emitted. Safe with nil/empty input.
// Tokens that NormalizeTemplateName rejects (dynamic / computed names) are
// skipped — precision over recall.
func EmitTemplateEdges(entities *[]types.EntityRecord, lang string, edges []TemplateEdge) int {
	if entities == nil || len(*entities) == 0 || len(edges) == 0 {
		return 0
	}

	hostByName := map[string]int{}
	for i := range *entities {
		hostByName[(*entities)[i].Name] = i
	}

	seenEdge := map[string]bool{}
	seenTpl := map[string]bool{}
	var newEntities []types.EntityRecord
	emitted := 0

	for _, ed := range edges {
		name := NormalizeTemplateName(ed.Name)
		if name == "" {
			continue // dynamic / computed / unusable name — drop
		}

		hostIdx := 0 // file entity by default
		if ed.FromName != "" {
			if idx, ok := hostByName[ed.FromName]; ok {
				hostIdx = idx
			}
		}

		edgeKey := ed.FromName + "\x00" + name
		if !seenEdge[edgeKey] {
			seenEdge[edgeKey] = true
			props := map[string]string{"template": name}
			if ed.Pattern != "" {
				props["pattern"] = ed.Pattern
			}
			(*entities)[hostIdx].Relationships = append((*entities)[hostIdx].Relationships,
				types.RelationshipRecord{
					ToID:       TemplateTargetID(name),
					Kind:       string(types.RelationshipKindRenders),
					Properties: props,
				})
			emitted++
		}

		if !seenTpl[name] {
			seenTpl[name] = true
			newEntities = append(newEntities, TemplateEntity(name, lang))
		}
	}

	*entities = append(*entities, newEntities...)
	return emitted
}
