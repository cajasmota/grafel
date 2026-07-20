package ingest

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cajasmota/grafel/internal/graph"
	"github.com/cajasmota/grafel/internal/types"
)

// maxDocBytes bounds how much of a single markdown file is read. Documentation
// files are small; this caps pathological inputs without a config knob.
const maxDocBytes = 1 << 20 // 1 MiB

// maxPDFBytes bounds how much of a single PDF is read. PDFs carry binary object
// streams and are larger than markdown, so they get a higher (but still
// bounded) cap. The PDF reader needs the whole file (it has a cross-reference
// trailer), so a too-small bound would fail to parse — this keeps real docs
// parseable while still capping pathological inputs.
const maxPDFBytes = 32 << 20 // 32 MiB

// docByteLimit returns the read cap for a doc path by type.
func docByteLimit(rel string) int {
	if isPDFPath(rel) {
		return maxPDFBytes
	}
	return maxDocBytes
}

// sectionProps builds the Section node's Properties. The "page" key is included
// only for PDF-derived sections (Page > 0); markdown sections (Page == 0) keep
// the original two-key shape.
func sectionProps(s *Section) map[string]string {
	p := map[string]string{
		"depth":   fmt.Sprintf("%d", s.Depth),
		"heading": s.HeadingText,
	}
	if s.Page > 0 {
		p["page"] = fmt.Sprintf("%d", s.Page)
	}
	return p
}

// Result is the output of an ingestion run: the doc/section nodes and the
// CONTAINS/MENTIONS edges to splice into the graph document.
type Result struct {
	Entities      []graph.Entity
	Relationships []graph.Relationship
	// Stats for the indexer's stderr summary.
	Documents int
	Sections  int
	Mentions  int
	FilesRead int
	// Skipped counts doc files that could not be parsed at all (e.g. encrypted
	// or corrupt PDFs); they contribute no nodes. A warning is logged per skip.
	Skipped int
}

// parseDoc dispatches one doc file to the right deterministic parser by
// extension: *.pdf → ParsePDF (pdf.go), everything else (*.md/*.markdown) →
// ParseDocument (markdown.go). It returns the parsed Document, its Sections,
// the language tag stamped on the emitted nodes, and an error ONLY for inputs
// that cannot be parsed at all (e.g. an encrypted or corrupt PDF) — callers
// skip + log those. A markdown parse never errors. A PDF with no text layer is
// NOT an error: it returns a Document with a Note and zero sections.
func parseDoc(rel string, content []byte) (doc Document, sections []Section, language string, err error) {
	if isPDFPath(rel) {
		doc, sections, err = ParsePDF(rel, content)
		return doc, sections, "pdf", err
	}
	doc, sections = ParseDocument(rel, content)
	return doc, sections, "markdown", nil
}

// Ingest runs the deterministic doc pipeline over docRelPaths (repo-relative
// slash paths — markdown AND PDF) and returns Document/Section nodes plus
// CONTAINS (Document→Section→subsection) and MENTIONS (Section→code entity)
// edges, ready to append to the graph document. Each path is dispatched to the
// markdown or PDF parser by extension (see parseDoc); both produce the same
// Document/Section shape so the MENTIONS linker is parser-agnostic.
//
// repoRoot is the absolute repo path used to read file bodies; repoTag is the
// per-repo slug stamped into every entity ID (matching graph.EntityID usage
// elsewhere in the indexer). codeEntities is the current graph entity set used
// to resolve exact name mentions.
//
// Fully deterministic: no LLM calls, no network. Output ordering is stable
// (files sorted, sections in source order, edges ID-tiebroken). A PDF that
// cannot be parsed (encrypted/corrupt) is skipped with a logged warning and a
// bumped Skipped counter; a PDF with no text layer yields a doc node + zero
// sections (honest empty, not an error).
func Ingest(repoRoot, repoTag string, docRelPaths []string, codeEntities []graph.Entity) Result {
	var res Result

	// Build the exact-name target index once from the code entities. Doc/section
	// nodes we are about to create are NOT in this set, so a section can never
	// MENTIONS another doc node.
	tuples := make([]NameTuple, 0, len(codeEntities))
	for i := range codeEntities {
		e := &codeEntities[i]
		tuples = append(tuples, NameTuple{
			Name:          e.Name,
			QualifiedName: e.QualifiedName,
			ID:            e.ID,
			Kind:          e.Kind,
		})
	}
	nameIdx := IndexNames(tuples)

	// Sort the file list for deterministic output.
	paths := append([]string(nil), docRelPaths...)
	sort.Strings(paths)

	for _, rel := range paths {
		rel = filepath.ToSlash(rel)
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		content, err := readBoundedFile(abs, docByteLimit(rel))
		if err != nil {
			// Best-effort: a doc we can't read contributes nothing.
			continue
		}

		doc, sections, language, perr := parseDoc(rel, content)
		if perr != nil {
			// Encrypted / corrupt PDF (or other unparsable input): skip with a
			// logged warning, contribute no nodes. Never fatal.
			log.Printf("ingest-docs: skipping %s: %v", rel, perr)
			res.Skipped++
			continue
		}
		res.FilesRead++

		// Document node. The doc kind is shared across markdown + PDF; the
		// Language property distinguishes them, plus an optional extraction note.
		docID := graph.EntityID(repoTag, string(types.EntityKindMarkdownDocument), rel, rel)
		docProps := map[string]string{"title": doc.Title}
		if doc.Note != "" {
			docProps["note"] = doc.Note
		}
		docEnt := graph.Entity{
			ID:            docID,
			Name:          path_base(rel),
			QualifiedName: repoTag + "::" + rel,
			Kind:          string(types.EntityKindMarkdownDocument),
			SourceFile:    rel,
			StartLine:     1,
			EndLine:       max1(doc.LineCount),
			Language:      language,
		}.WithProperties(docProps)
		res.Entities = append(res.Entities, docEnt)
		res.Documents++

		// Section nodes + CONTAINS hierarchy. Section identity is the file path
		// plus the heading line, so distinct sections (even same heading text)
		// get distinct IDs and the ID is stable across re-indexes.
		sectionIDs := make([]string, len(sections))
		for k := range sections {
			s := &sections[k]
			name := fmt.Sprintf("%s#L%d", rel, s.StartLine)
			secID := graph.EntityID(repoTag, string(types.EntityKindSection), name, rel)
			sectionIDs[k] = secID
			res.Entities = append(res.Entities, graph.Entity{
				ID:            secID,
				Name:          headingOrAnchor(s.HeadingText, s.StartLine),
				QualifiedName: repoTag + "::" + name,
				Kind:          string(types.EntityKindSection),
				SourceFile:    rel,
				StartLine:     s.StartLine,
				EndLine:       s.EndLine,
				Language:      language,
			}.WithProperties(sectionProps(s)))
			res.Sections++

			// CONTAINS: parent (another section, or the document for top-level).
			parentID := docID
			if s.ParentIndex >= 0 {
				parentID = sectionIDs[s.ParentIndex]
			}
			res.Relationships = append(res.Relationships, mkRel(parentID, secID, string(types.RelationshipKindContains), nil))
		}

		// MENTIONS: Section → code entity, exact-match only.
		mentions := LinkMentions(sections, nameIdx)
		for _, m := range mentions {
			res.Relationships = append(res.Relationships, mkRel(
				sectionIDs[m.SectionIndex],
				m.TargetID,
				string(types.RelationshipKindMentions),
				map[string]string{"token": m.Token, "target_kind": m.TargetKind},
			))
			res.Mentions++
		}
	}

	// Deterministic edge ordering (by from, to, kind).
	sort.SliceStable(res.Relationships, func(a, b int) bool {
		ra, rb := res.Relationships[a], res.Relationships[b]
		if ra.FromID != rb.FromID {
			return ra.FromID < rb.FromID
		}
		if ra.ToID != rb.ToID {
			return ra.ToID < rb.ToID
		}
		return ra.Kind < rb.Kind
	})
	sort.SliceStable(res.Entities, func(a, b int) bool {
		return res.Entities[a].ID < res.Entities[b].ID
	})
	return res
}

func mkRel(from, to, kind string, props map[string]string) graph.Relationship {
	return graph.Relationship{
		ID:     graph.RelationshipID(from, to, kind),
		FromID: from,
		ToID:   to,
		Kind:   kind,
	}.WithProperties(props)
}

// readBoundedFile reads at most limit bytes from path.
func readBoundedFile(path string, limit int) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // path derived from repo-relative walk result
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, err
	}
	return buf[:n], nil
}

func path_base(rel string) string {
	return filepath.Base(filepath.FromSlash(rel))
}

func headingOrAnchor(heading string, line int) string {
	if strings.TrimSpace(heading) != "" {
		return heading
	}
	return fmt.Sprintf("§L%d", line)
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
