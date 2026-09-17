package dashboard

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	repositoryTopologyDefaultDetailPageSize = 25
	repositoryTopologyMaxDetailPageSize     = 100
)

type repositoryTopologyDetailQuery struct {
	Source   string
	Target   string
	Channel  repositoryChannel
	Evidence []repositoryEvidence
	Search   string
	Page     int
	PageSize int
}

func (s *Server) handleV2RepositoryTopology(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	group := strings.TrimSpace(r.PathValue("group"))
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	query, err := parseRepositoryTopologyQuery(r)
	if err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	grp, err := s.graphs.GetGroupForRef(group, ref)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	index := repositoryTopologyIndexForGroup(s.graphs, group, ref, grp)
	if err := validateRepositoryTopologyQuery(index, query); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	response := filterRepositoryTopology(index, query)
	log.Printf("repository-topology group=%s nodes=%d edges=%d relationships=%d truncated=%v duration=%s", group, len(response.Nodes), len(response.Edges), response.Summary.RelationshipCount, response.Truncated, time.Since(started).Round(time.Millisecond))
	writeV2JSON(w, http.StatusOK, v2OK(response))
}

func (s *Server) handleV2RepositoryTopologyEdge(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	group := strings.TrimSpace(r.PathValue("group"))
	if group == "" {
		writeV2Err(w, http.StatusBadRequest, "bad_request", "group required")
		return
	}
	query, err := parseRepositoryTopologyDetailQuery(r)
	if err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	grp, err := s.graphs.GetGroupForRef(group, ref)
	if err != nil {
		writeV2Err(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	index := repositoryTopologyIndexForGroup(s.graphs, group, ref, grp)
	if err := validateRepositorySlug(index, query.Source); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := validateRepositorySlug(index, query.Target); err != nil {
		writeV2Err(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	details := repositoryTopologyDetails(index, query)
	offset := (query.Page - 1) * query.PageSize
	if offset > len(details) {
		offset = len(details)
	}
	end := offset + query.PageSize
	if end > len(details) {
		end = len(details)
	}
	page := append([]repositoryTopologyDetail(nil), details[offset:end]...)
	log.Printf("repository-topology-edge group=%s rows=%d total=%d duration=%s", group, len(page), len(details), time.Since(started).Round(time.Millisecond))
	writeV2JSON(w, http.StatusOK, v2Page(page, V2Pagination{Limit: query.PageSize, Offset: offset, Total: len(details)}))
}

func repositoryTopologyIndexForGroup(cache *GraphCache, group, ref string, grp *DashGroup) repositoryTopologyIndex {
	if cache == nil || cache.RepositoryTopologies == nil {
		return buildRepositoryTopologyIndex(grp)
	}
	return cache.RepositoryTopologies.GetOrBuild(group, ref, grp)
}

func validateRepositoryTopologyQuery(index repositoryTopologyIndex, query repositoryTopologyQuery) error {
	for _, repo := range query.Repos {
		if err := validateRepositorySlug(index, repo); err != nil {
			return err
		}
	}
	for _, repo := range []string{query.Focus, query.Source, query.Target} {
		if repo != "" {
			if err := validateRepositorySlug(index, repo); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRepositorySlug(index repositoryTopologyIndex, repo string) error {
	if _, ok := index.Repositories[repo]; !ok {
		return fmt.Errorf("unknown repository %q", repo)
	}
	return nil
}

func parseRepositoryTopologyDetailQuery(r *http.Request) (repositoryTopologyDetailQuery, error) {
	values := r.URL.Query()
	query := repositoryTopologyDetailQuery{
		Source: strings.TrimSpace(values.Get("source")), Target: strings.TrimSpace(values.Get("target")),
		Search: strings.TrimSpace(values.Get("q")), Page: 1, PageSize: repositoryTopologyDefaultDetailPageSize,
	}
	if query.Source == "" || query.Target == "" {
		return repositoryTopologyDetailQuery{}, fmt.Errorf("source and target are required")
	}
	query.Channel = repositoryChannel(strings.ToLower(strings.TrimSpace(values.Get("channel"))))
	if !isRepositoryChannel(query.Channel) {
		return repositoryTopologyDetailQuery{}, fmt.Errorf("invalid channel %q", values.Get("channel"))
	}
	var err error
	if raw := values.Get("evidence"); raw != "" {
		query.Evidence, err = parseRepositoryEvidence(raw)
		if err != nil {
			return repositoryTopologyDetailQuery{}, err
		}
	}
	if raw := strings.TrimSpace(values.Get("page")); raw != "" {
		query.Page, err = strconv.Atoi(raw)
		if err != nil || query.Page < 1 {
			return repositoryTopologyDetailQuery{}, fmt.Errorf("page must be a positive integer")
		}
	}
	if raw := strings.TrimSpace(values.Get("page_size")); raw != "" {
		query.PageSize, err = strconv.Atoi(raw)
		if err != nil || query.PageSize < 1 {
			return repositoryTopologyDetailQuery{}, fmt.Errorf("page_size must be a positive integer")
		}
	}
	if query.PageSize > repositoryTopologyMaxDetailPageSize {
		query.PageSize = repositoryTopologyMaxDetailPageSize
	}
	return query, nil
}

func repositoryTopologyDetails(index repositoryTopologyIndex, query repositoryTopologyDetailQuery) []repositoryTopologyDetail {
	details := make([]repositoryTopologyDetail, 0)
	for _, relationship := range index.Relationships {
		if relationship.SourceRepo != query.Source || relationship.TargetRepo != query.Target || relationship.Channel != query.Channel {
			continue
		}
		if len(query.Evidence) > 0 && !containsRepositoryEvidence(query.Evidence, relationship.Evidence) {
			continue
		}
		if query.Search != "" && !strings.Contains(relationship.SearchText, strings.ToLower(query.Search)) {
			continue
		}
		properties := make(map[string]string, len(relationship.Link.Properties))
		for key, value := range relationship.Link.Properties {
			properties[key] = value
		}
		details = append(details, repositoryTopologyDetail{
			ID: relationship.ID, Source: relationship.SourceRepo, Target: relationship.TargetRepo,
			Channel: relationship.Channel, Kind: relationship.Link.Kind, Method: relationship.Link.Method,
			Identifier: relationship.Identifier, Label: relationship.Label, Evidence: relationship.Evidence,
			Confidence: relationship.Link.Confidence, SourceEntity: relationship.Link.Source, TargetEntity: relationship.Link.Target,
			SourceFile: relationship.Link.SourceFile, SourceLine: relationship.Link.SourceLine,
			TargetFile: relationship.Link.TargetFile, TargetLine: relationship.Link.TargetLine, Properties: properties,
		})
	}
	sort.Slice(details, func(i, j int) bool {
		if details[i].Label != details[j].Label {
			return details[i].Label < details[j].Label
		}
		if details[i].Identifier != details[j].Identifier {
			return details[i].Identifier < details[j].Identifier
		}
		return details[i].ID < details[j].ID
	})
	return details
}
