// handlers_v2_docs.go — WebUI v2 entity-centric docs endpoints.
//
// These endpoints serve the /g/:groupId/docs screen in webui-v2.
// They are SEPARATE from the v1 /api/docs/{group} markdown-portal endpoints
// in handlers_docs.go, which are left completely untouched.
//
// Routes (registered in server.go under the v2 section):
//
//	GET  /api/v2/groups/{group}/docs/tree                   → handleV2DocsTree
//	GET  /api/v2/groups/{group}/docs/entities/{entityId}    → handleV2DocsEntityDetail
//
// POST /api/v2/groups/{group}/docs/generate is intentionally NOT implemented.
// Doc generation is a long-running skill operation managed via the Pending
// screen (#1432). Adding a stub here would mislead callers; the UI shows a
// hint to run `archigraph generate-docs` instead (EntityStub behaviour).

package dashboard

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/cajasmota/archigraph/internal/graph"
)

// ── Wire shapes ────────────────────────────────────────────────────────────

// v2DocsTreeNode mirrors the DocsTreeNode TypeScript interface in data/types.ts.
type v2DocsTreeNode struct {
	Type     string           `json:"type"`
	Name     string           `json:"name"`
	ID       string           `json:"id,omitempty"`
	Children []v2DocsTreeNode `json:"children,omitempty"`
}

// v2DocsEntityReply is the data payload for the entity-detail endpoint.
type v2DocsEntityReply struct {
	Name           string            `json:"name"`
	Type           string            `json:"type"`
	Repo           string            `json:"repo"`
	File           string            `json:"file"`
	Line           int               `json:"line"`
	Signature      string            `json:"signature"`
	Description    string            `json:"description"`
	AIGenerated    bool              `json:"aiGenerated"`
	Params         []v2DocsParam     `json:"params"`
	Returns        *v2DocsReturn     `json:"returns"`
	Inbound        int               `json:"inbound"`
	Outbound       int               `json:"outbound"`
	Callers        []string          `json:"callers"`
	Callees        []string          `json:"callees"`
	ResponseShapes []v2DocsRespShape `json:"responseShapes,omitempty"`
	Stub           bool              `json:"stub,omitempty"`
}

type v2DocsParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Desc string `json:"desc"`
}

type v2DocsReturn struct {
	Type string `json:"type"`
	Desc string `json:"desc,omitempty"`
}

type v2DocsRespShape struct {
	Status int    `json:"status"`
	Shape  string `json:"shape"`
}

// ── Handlers ───────────────────────────────────────────────────────────────

// handleV2DocsTree — GET /api/v2/groups/{group}/docs/tree
//
// Returns the entity tree for the Docs screen left pane.
// The tree is built from the in-memory graph (not from on-disk docs files).
// Shape: repo → folder (by directory prefix) → entity leaf.
func (s *Server) handleV2DocsTree(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", "group not found: "+group)
		return
	}

	// Build per-repo trees.
	var roots []v2DocsTreeNode
	for _, repo := range sortedRepos(grp) {
		if repo.Doc == nil {
			continue
		}
		repoNode := buildV2EntityTree(repo.Slug, repo.Doc.Entities)
		if len(repoNode.Children) > 0 {
			roots = append(roots, repoNode)
		}
	}
	if roots == nil {
		roots = []v2DocsTreeNode{}
	}
	writeV2JSON(w, http.StatusOK, v2OK(roots))
}

// handleV2DocsEntityDetail — GET /api/v2/groups/{group}/docs/entities/{entityId}
//
// Returns the full documentation payload for a single entity.
// Description is sourced from enrichment frontmatter if available; otherwise
// stub=true is set and the UI renders EntityStub.
func (s *Server) handleV2DocsEntityDetail(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	entityID := r.PathValue("entityId")

	grp, err := s.graphs.GetGroup(group)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", "group not found: "+group)
		return
	}

	// Find entity across repos.
	var foundRepo *DashRepo
	var foundEntity *graph.Entity
	for _, repo := range sortedRepos(grp) {
		if repo.Doc == nil {
			continue
		}
		for i := range repo.Doc.Entities {
			e := &repo.Doc.Entities[i]
			if e.ID == entityID {
				foundRepo = repo
				foundEntity = e
				break
			}
		}
		if foundEntity != nil {
			break
		}
	}
	if foundEntity == nil {
		writeV2Err(w, http.StatusNotFound, "not_found", "entity not found: "+entityID)
		return
	}

	reply := buildV2EntityDetail(grp, foundRepo, foundEntity)
	writeV2JSON(w, http.StatusOK, v2OK(reply))
}

// ── Helpers ────────────────────────────────────────────────────────────────

// buildV2EntityTree groups entities from one repo into a folder tree.
func buildV2EntityTree(repoSlug string, entities []graph.Entity) v2DocsTreeNode {
	type folderNode struct {
		children map[string]*folderNode
		leaves   []v2DocsTreeNode
	}

	root := &folderNode{children: map[string]*folderNode{}}

	var insert func(f *folderNode, parts []string, leaf v2DocsTreeNode)
	insert = func(f *folderNode, parts []string, leaf v2DocsTreeNode) {
		if len(parts) == 0 {
			f.leaves = append(f.leaves, leaf)
			return
		}
		p := parts[0]
		if f.children[p] == nil {
			f.children[p] = &folderNode{children: map[string]*folderNode{}}
		}
		insert(f.children[p], parts[1:], leaf)
	}

	for i := range entities {
		e := &entities[i]
		kind := strings.ToLower(dashStripScopePrefix(e.Kind))
		leaf := v2DocsTreeNode{
			Type: kind,
			Name: e.Name,
			ID:   e.ID,
		}
		// Derive folder path from SourceFile directory.
		dir := filepath.Dir(e.SourceFile)
		if dir == "." || dir == "" {
			root.leaves = append(root.leaves, leaf)
			continue
		}
		parts := strings.Split(filepath.ToSlash(dir), "/")
		insert(root, parts, leaf)
	}

	var toNode func(name string, n *folderNode, isRepo bool) v2DocsTreeNode
	toNode = func(name string, n *folderNode, isRepo bool) v2DocsTreeNode {
		nodeType := "folder"
		if isRepo {
			nodeType = "repo"
		}
		result := v2DocsTreeNode{Type: nodeType, Name: name}
		// Add direct leaves first.
		result.Children = append(result.Children, n.leaves...)
		// Then recurse into sub-folders (sorted for determinism).
		subNames := make([]string, 0, len(n.children))
		for k := range n.children {
			subNames = append(subNames, k)
		}
		// Simple insertion sort — deterministic order.
		for i := 1; i < len(subNames); i++ {
			for j := i; j > 0 && subNames[j] < subNames[j-1]; j-- {
				subNames[j], subNames[j-1] = subNames[j-1], subNames[j]
			}
		}
		for _, sub := range subNames {
			child := toNode(sub, n.children[sub], false)
			result.Children = append(result.Children, child)
		}
		return result
	}

	return toNode(repoSlug, root, true)
}

// buildV2EntityDetail constructs the entity detail reply for a single entity.
func buildV2EntityDetail(grp *DashGroup, repo *DashRepo, e *graph.Entity) v2DocsEntityReply {
	reply := v2DocsEntityReply{
		Name:      e.Name,
		Type:      strings.ToLower(dashStripScopePrefix(e.Kind)),
		Repo:      repo.Slug,
		File:      e.SourceFile,
		Line:      e.StartLine,
		Signature: e.Signature,
		Params:    []v2DocsParam{},
		Callers:   []string{},
		Callees:   []string{},
	}

	// Build caller/callee name lists from the relationship graph.
	// Build an entity ID → name index across the whole group for cheap lookup.
	idToName := map[string]string{}
	for _, r := range sortedRepos(grp) {
		if r.Doc == nil {
			continue
		}
		for _, ent := range r.Doc.Entities {
			idToName[ent.ID] = ent.Name
		}
	}
	if repo.Doc != nil {
		inbound := 0
		outbound := 0
		for _, rel := range repo.Doc.Relationships {
			switch rel.Kind {
			case "CALLS", "RENDERS", "REFERENCES", "IMPORTS":
				if rel.ToID == e.ID {
					inbound++
					if name, ok := idToName[rel.FromID]; ok {
						reply.Callers = append(reply.Callers, name)
					}
				}
				if rel.FromID == e.ID {
					outbound++
					if name, ok := idToName[rel.ToID]; ok {
						reply.Callees = append(reply.Callees, name)
					}
				}
			}
		}
		reply.Inbound = inbound
		reply.Outbound = outbound
	}

	// Cap caller/callee lists at 50 per spec.
	if len(reply.Callers) > 50 {
		reply.Callers = reply.Callers[:50]
	}
	if len(reply.Callees) > 50 {
		reply.Callees = reply.Callees[:50]
	}

	// Attempt to load enrichment frontmatter for description.
	// Frontmatter files live at <repo-path>/.archigraph/entities/<entityId>.md
	// (same path pattern used by handleEnrichmentWriteback).
	description, aiGenerated := v2LoadEntityDescription(repo, e.ID)
	reply.Description = description
	reply.AIGenerated = aiGenerated
	reply.Stub = description == ""

	return reply
}

// v2LoadEntityDescription tries to read enrichment frontmatter for an entity.
// Returns ("", false) when no frontmatter file exists — triggering stub mode.
func v2LoadEntityDescription(repo *DashRepo, entityID string) (string, bool) {
	if repo.Path == "" {
		return "", false
	}
	fmPath := filepath.Join(repo.Path, ".archigraph", "entities", entityID+".md")
	fm, err := ParseEnrichmentFrontmatter(fmPath)
	if err != nil || fm == nil || !fm.HasData() {
		return "", false
	}
	return fm.Summary, true
}
