package dashboard

type repositoryChannel string

const (
	channelDubbo    repositoryChannel = "dubbo"
	channelHTTP     repositoryChannel = "http"
	channelKafka    repositoryChannel = "kafka"
	channelRabbitMQ repositoryChannel = "rabbitmq"
	channelOther    repositoryChannel = "other"
)

type repositoryEvidence string

const (
	evidenceConfirmed repositoryEvidence = "confirmed"
	evidenceInferred  repositoryEvidence = "inferred"
	evidenceDangling  repositoryEvidence = "dangling"
	evidenceAmbiguous repositoryEvidence = "ambiguous"
	evidenceExternal  repositoryEvidence = "external"
)

type repositoryDirection string

const (
	directionInbound  repositoryDirection = "inbound"
	directionOutbound repositoryDirection = "outbound"
	directionBoth     repositoryDirection = "both"
)

type repositoryTopologyQuery struct {
	Channels  []repositoryChannel
	Repos     []string
	Focus     string
	Direction repositoryDirection
	Depth     int
	Evidence  []repositoryEvidence
	MinCount  int
	Source    string
	Target    string
	Search    string
}

type repositoryEvidenceCounts struct {
	Confirmed int `json:"confirmed"`
	Inferred  int `json:"inferred"`
	Dangling  int `json:"dangling"`
	Ambiguous int `json:"ambiguous"`
	External  int `json:"external"`
}

type repositoryTopologyNode struct {
	ID                    string                   `json:"id"`
	Repository            string                   `json:"repository"`
	Label                 string                   `json:"label"`
	PrimaryLanguage       string                   `json:"primary_language,omitempty"`
	EntityCount           int                      `json:"entity_count"`
	ModuleCount           int                      `json:"module_count"`
	InboundRelationships  int                      `json:"inbound_relationships"`
	OutboundRelationships int                      `json:"outbound_relationships"`
	ConnectedRepositories int                      `json:"connected_repositories"`
	Evidence              repositoryEvidenceCounts `json:"evidence"`
	GraphState            string                   `json:"graph_state,omitempty"`
	Modules               []string                 `json:"modules"`
}

type repositoryTopologySample struct {
	ID         string             `json:"id"`
	Label      string             `json:"label"`
	Identifier string             `json:"identifier,omitempty"`
	Evidence   repositoryEvidence `json:"evidence"`
}

type repositoryTopologyEdge struct {
	ID                string                     `json:"id"`
	Source            string                     `json:"source"`
	Target            string                     `json:"target"`
	Channel           repositoryChannel          `json:"channel"`
	RelationshipCount int                        `json:"relationship_count"`
	ContractCount     int                        `json:"contract_count"`
	Evidence          repositoryEvidenceCounts   `json:"evidence"`
	Labels            []string                   `json:"labels"`
	Samples           []repositoryTopologySample `json:"samples"`
	HasMore           bool                       `json:"has_more"`
}

type repositoryTopologyFacet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

type repositoryTopologyFacets struct {
	Channels     []repositoryTopologyFacet `json:"channels"`
	Repositories []repositoryTopologyFacet `json:"repositories"`
	Evidence     []repositoryTopologyFacet `json:"evidence"`
}

type repositoryTopologySummary struct {
	RepositoryCount   int `json:"repository_count"`
	EdgeCount         int `json:"edge_count"`
	RelationshipCount int `json:"relationship_count"`
	PreLimitNodes     int `json:"pre_limit_nodes"`
	PreLimitEdges     int `json:"pre_limit_edges"`
}

type repositoryTopologyLimits struct {
	MaxNodes         int `json:"max_nodes"`
	MaxEdges         int `json:"max_edges"`
	InlineSamples    int `json:"inline_samples"`
	DetailPageSize   int `json:"detail_page_size"`
	MaxSearchRecords int `json:"max_search_records"`
}

type repositoryTopologyResponse struct {
	Nodes     []repositoryTopologyNode  `json:"nodes"`
	Edges     []repositoryTopologyEdge  `json:"edges"`
	Facets    repositoryTopologyFacets  `json:"facets"`
	Summary   repositoryTopologySummary `json:"summary"`
	Limits    repositoryTopologyLimits  `json:"limits"`
	Path      []string                  `json:"path"`
	PathFound bool                      `json:"path_found"`
	Truncated bool                      `json:"truncated"`
}

type repositoryTopologyDetail struct {
	ID           string             `json:"id"`
	Source       string             `json:"source"`
	Target       string             `json:"target"`
	Channel      repositoryChannel  `json:"channel"`
	Kind         string             `json:"kind,omitempty"`
	Method       string             `json:"method,omitempty"`
	Identifier   string             `json:"identifier,omitempty"`
	Label        string             `json:"label"`
	Evidence     repositoryEvidence `json:"evidence"`
	Confidence   float64            `json:"confidence,omitempty"`
	SourceEntity string             `json:"source_entity,omitempty"`
	TargetEntity string             `json:"target_entity,omitempty"`
	SourceFile   string             `json:"source_file,omitempty"`
	SourceLine   int                `json:"source_line,omitempty"`
	TargetFile   string             `json:"target_file,omitempty"`
	TargetLine   int                `json:"target_line,omitempty"`
	Properties   map[string]string  `json:"properties"`
}
