package mcp

import (
	"testing"

	"github.com/cajasmota/grafel/internal/graph"
)

func TestClassifyNoise(t *testing.T) {
	cases := []struct {
		name string
		e    graph.Entity
		want noiseKind
	}{
		{
			name: "file container component",
			e: graph.Entity{
				Kind: "SCOPE.Component", Name: "src/features/auth/login/Login.tablet.tsx",
				SourceFile: "src/features/auth/login/Login.tablet.tsx", StartLine: 0,
			}.WithProperties(map[string]string{"subtype": "file"}),
			want: noiseContainer,
		},
		{
			name: "container label==path no subtype",
			e: graph.Entity{
				Kind: "SCOPE.Component", Name: "app/(public)/login.tsx",
				SourceFile: "app/(public)/login.tsx", StartLine: 0,
			},
			want: noiseContainer,
		},
		{
			name: "drf implicit method shadow (empty qname, line 0)",
			e: graph.Entity{
				Kind: "SCOPE.Operation", Name: "LoginViewSet.retrieve",
				SourceFile: "core/views/auth_viewset.py", StartLine: 0, QualifiedName: "",
			}.WithProperties(map[string]string{"pattern_type": "drf_viewset_implicit_method"}),
			want: noiseShadow,
		},
		{
			name: "inferred-from-class-hierarchy provenance",
			e: graph.Entity{
				Kind: "SCOPE.Operation", Name: "Base.method", StartLine: 12,
				QualifiedName: "pkg.Base.method",
			}.WithProperties(map[string]string{"provenance": "INFERRED_FROM_CLASS_HIERARCHY"}),
			want: noiseShadow,
		},
		{
			name: "raw pattern node",
			e: graph.Entity{
				Kind: "SCOPE.Pattern", Name: "error_handling:try_catch:3",
				SourceFile: "x.py", StartLine: 10,
			},
			want: noisePattern,
		},
		{
			name: "process builtin map",
			e: graph.Entity{
				ID: "proc:11c264af58999ae9", Kind: "SCOPE.Process", Name: "Login → map",
				SourceFile: "src/features/auth/login/index.tsx", StartLine: 0,
			},
			want: noiseProcess,
		},
		{
			name: "real lined qualified operation",
			e: graph.Entity{
				Kind: "SCOPE.Operation", Name: "login", QualifiedName: "auth.login",
				SourceFile: "src/stores/authentication/authService.js", StartLine: 4,
			},
			want: noiseNone,
		},
		{
			name: "endpoint definition (lineless but legit)",
			e: graph.Entity{
				Kind: "http_endpoint_definition", Name: "http:POST:/api/v1/auth/login",
				SourceFile: "core/routers.py", StartLine: 0, QualifiedName: "",
			},
			want: noiseNone,
		},
		{
			name: "agent pattern is not raw pattern noise",
			e: graph.Entity{
				Kind: "AgentPattern", Name: "retry-policy", StartLine: 5,
			},
			want: noiseNone,
		},
		// #5782 (ADR-0025): a SCOPE.ChannelBinding is a real, config-linked
		// structural signal — explicitly NOT noise. It must not trip the
		// SCOPE.Pattern rule, the schema-field rule, or the lineless-shadow
		// rule (it carries StartLine=1 like every config entity).
		{
			name: "channel binding is a real structural signal (not noise)",
			e: graph.Entity{
				Kind: "SCOPE.ChannelBinding", Name: "orders-out", Subtype: "outgoing",
				SourceFile: "src/main/resources/application.properties", StartLine: 1, EndLine: 1,
			}.WithProperties(map[string]string{
				"channel": "orders-out", "direction": "outgoing",
				"connector": "smallrye-kafka", "topic": "orders.placed",
			},
			),
			want: noiseNone,
		},
		// #1712: Schema field members are noise.
		{
			name: "schema field member (SCOPE.Schema subtype=field)",
			e: graph.Entity{
				Kind: "SCOPE.Schema", Subtype: "field",
				Name:       "DeficiencyCreateSerializer.amount",
				SourceFile: "core/serializers/deficiency_serializer.py", StartLine: 12,
			},
			want: noiseSchemaField,
		},
		{
			name: "schema field member (no SCOPE. prefix)",
			e: graph.Entity{
				Kind: "Schema", Subtype: "field",
				Name:       "DeficiencyCreateSerializer.created_at",
				SourceFile: "core/serializers/deficiency_serializer.py", StartLine: 13,
			},
			want: noiseSchemaField,
		},
		{
			name: "schema class itself (no subtype) is NOT noise",
			e: graph.Entity{
				Kind: "SCOPE.Schema", Name: "DeficiencyCreateSerializer",
				SourceFile: "core/serializers/deficiency_serializer.py", StartLine: 8,
			},
			want: noiseNone,
		},
		// #1748: non-addressable function-body locals are noise.
		{
			name: "local_scope plain destructure binding",
			e: graph.Entity{
				Kind: "SCOPE.Component", Subtype: "const_destructure",
				Name:       "counts",
				SourceFile: "src/features/ContractProposals.jsx", StartLine: 48,
			}.WithProperties(map[string]string{
				"kind": "SCOPE.Component", "subtype": "const_destructure",
				"local_scope": "true",
			},
			),
			want: noiseLocalScope,
		},
		{
			name: "local_scope array destructure binding",
			e: graph.Entity{
				Kind: "SCOPE.Component", Subtype: "const_destructure",
				Name:       "a",
				SourceFile: "src/features/Cmp.jsx", StartLine: 10,
			}.WithProperties(map[string]string{
				"kind": "SCOPE.Component", "subtype": "const_destructure",
				"local_scope": "true",
			},
			),
			want: noiseLocalScope,
		},
		{
			name: "real top-level component (no local_scope) is NOT noise",
			e: graph.Entity{
				Kind: "SCOPE.Component", Subtype: "const_destructure",
				Name:       "foo",
				SourceFile: "src/features/Widget.jsx", StartLine: 3,
				// No local_scope property — module-scope binding.
			},
			want: noiseNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyNoise(&c.e); got != c.want {
				t.Fatalf("classifyNoise = %d, want %d", got, c.want)
			}
		})
	}
}

func TestRankTierOrdersRealAboveNoise(t *testing.T) {
	real := &graph.Entity{Kind: "SCOPE.Operation", Name: "login", QualifiedName: "a.login", StartLine: 4}
	shadow := &graph.Entity{Kind: "SCOPE.Operation", Name: "LoginViewSet.list", StartLine: 0}
	container := graph.EntityPtr(graph.Entity{Kind: "SCOPE.Component", Name: "x.tsx", SourceFile: "x.tsx", StartLine: 0}.WithProperties(map[string]string{"subtype": "file"}))

	if rankTier(real) >= rankTier(shadow) {
		t.Fatalf("real (%d) should rank above shadow (%d)", rankTier(real), rankTier(shadow))
	}
	if rankTier(shadow) >= rankTier(container) {
		t.Fatalf("shadow (%d) should rank above container (%d)", rankTier(shadow), rankTier(container))
	}
}

// TestRankTierSchemaFieldBelowParent verifies that a Schema field member ranks
// strictly below the real parent Schema class entity (#1712).
func TestRankTierSchemaFieldBelowParent(t *testing.T) {
	parent := &graph.Entity{
		Kind: "SCOPE.Schema", Name: "DeficiencyCreateSerializer",
		SourceFile: "core/serializers/deficiency_serializer.py", StartLine: 8,
	}
	field := &graph.Entity{
		Kind: "SCOPE.Schema", Subtype: "field",
		Name:       "DeficiencyCreateSerializer.amount",
		SourceFile: "core/serializers/deficiency_serializer.py", StartLine: 12,
	}
	if classifyNoise(parent) != noiseNone {
		t.Fatalf("parent serializer should be noiseNone, got %d", classifyNoise(parent))
	}
	if classifyNoise(field) != noiseSchemaField {
		t.Fatalf("schema field should be noiseSchemaField, got %d", classifyNoise(field))
	}
	if rankTier(parent) >= rankTier(field) {
		t.Fatalf("parent tier (%d) should be lower (better) than field tier (%d)", rankTier(parent), rankTier(field))
	}
}

// TestClassifyNoiseScopePattern verifies that SCOPE.Pattern entities (structural
// pattern nodes such as error_handling:try_catch:N) are classified as
// noisePattern — tier 8, below all other noise tiers (#1733).
func TestClassifyNoiseScopePattern(t *testing.T) {
	cases := []struct {
		name string
		e    graph.Entity
		want noiseKind
	}{
		{
			name: "canonical SCOPE.Pattern kind",
			e: graph.Entity{
				Kind: "SCOPE.Pattern", Name: "error_handling:try_catch:19",
				SourceFile: "src/api/handlers.go", StartLine: 42,
			},
			want: noisePattern,
		},
		{
			name: "bare Pattern kind (no SCOPE. prefix)",
			e: graph.Entity{
				Kind: "Pattern", Name: "auth:jwt_verify:3",
				SourceFile: "src/middleware/auth.go", StartLine: 7,
			},
			want: noisePattern,
		},
		{
			name: "AgentPattern is NOT structural noise (agent-learned, ADR-0018)",
			e: graph.Entity{
				// AgentPattern entities are agent-learned (ADR-0018); they have a
				// StartLine from their definition source and are real surfaceable
				// results — not structural noise like SCOPE.Pattern.
				Kind: "AgentPattern", Name: "use-retry-on-transient-errors",
				QualifiedName: "patterns.use-retry-on-transient-errors", StartLine: 3,
			},
			want: noiseNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyNoise(&c.e); got != c.want {
				t.Fatalf("classifyNoise = %d, want %d", got, c.want)
			}
		})
	}
}

// TestRankTierPatternBelowSchemaField verifies the tier-8 guarantee: SCOPE.Pattern
// nodes must rank strictly below Schema field members (tier 7) and below all
// other real entities (#1733).
func TestRankTierPatternBelowSchemaField(t *testing.T) {
	real := &graph.Entity{
		Kind: "SCOPE.Operation", Name: "authenticateJWT",
		QualifiedName: "middleware.authenticateJWT", StartLine: 12,
	}
	schemaField := &graph.Entity{
		Kind: "SCOPE.Schema", Subtype: "field",
		Name:       "TokenSerializer.expiry",
		SourceFile: "core/serializers/token_serializer.py", StartLine: 5,
	}
	pattern := &graph.Entity{
		Kind: "SCOPE.Pattern", Name: "error_handling:try_catch:19",
		SourceFile: "src/api/handlers.go", StartLine: 42,
	}

	// Real entity should rank better than both schema field and pattern.
	if rankTier(real) >= rankTier(schemaField) {
		t.Fatalf("real entity tier (%d) should be lower (better) than schema field tier (%d)",
			rankTier(real), rankTier(schemaField))
	}
	if rankTier(real) >= rankTier(pattern) {
		t.Fatalf("real entity tier (%d) should be lower (better) than pattern tier (%d)",
			rankTier(real), rankTier(pattern))
	}
	// Schema field (tier 7) should rank better than pattern (tier 8).
	if rankTier(schemaField) >= rankTier(pattern) {
		t.Fatalf("schema field tier (%d) should be lower (better) than pattern tier (%d)",
			rankTier(schemaField), rankTier(pattern))
	}
}

// TestRankTierPatternBelowProcess verifies that SCOPE.Pattern (tier 8) ranks
// strictly below noiseProcess (tier 6) — process nodes are structural but at
// least name a real call site, whereas pattern nodes are aggregated labels.
func TestRankTierPatternBelowProcess(t *testing.T) {
	process := &graph.Entity{
		ID: "proc:aabbccdd11223344", Kind: "SCOPE.Process", Name: "Login → map",
		SourceFile: "src/features/auth/login/index.tsx", StartLine: 0,
	}
	pattern := &graph.Entity{
		Kind: "SCOPE.Pattern", Name: "error_handling:try_catch:7",
		SourceFile: "src/api/users.ts", StartLine: 88,
	}
	if rankTier(process) >= rankTier(pattern) {
		t.Fatalf("process tier (%d) should be lower (better) than pattern tier (%d)",
			rankTier(process), rankTier(pattern))
	}
}

func TestPageSlice(t *testing.T) {
	s := []int{0, 1, 2, 3, 4}
	if got := pageSlice(s, 0, 2); len(got) != 2 || got[0] != 0 {
		t.Fatalf("pageSlice(0,2)=%v", got)
	}
	if got := pageSlice(s, 2, 2); len(got) != 2 || got[0] != 2 {
		t.Fatalf("pageSlice(2,2)=%v", got)
	}
	if got := pageSlice(s, 10, 2); len(got) != 0 {
		t.Fatalf("pageSlice(10,2) should be empty, got %v", got)
	}
	if got := pageSlice(s, 3, 0); len(got) != 2 {
		t.Fatalf("pageSlice(3,0) should be 2 items, got %v", got)
	}
}
