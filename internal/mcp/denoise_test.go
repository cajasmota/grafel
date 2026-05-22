package mcp

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/graph"
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
				Properties: map[string]string{"subtype": "file"},
			},
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
				Properties: map[string]string{"pattern_type": "drf_viewset_implicit_method"},
			},
			want: noiseShadow,
		},
		{
			name: "inferred-from-class-hierarchy provenance",
			e: graph.Entity{
				Kind: "SCOPE.Operation", Name: "Base.method", StartLine: 12,
				QualifiedName: "pkg.Base.method",
				Properties:    map[string]string{"provenance": "INFERRED_FROM_CLASS_HIERARCHY"},
			},
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
	container := &graph.Entity{Kind: "SCOPE.Component", Name: "x.tsx", SourceFile: "x.tsx", StartLine: 0, Properties: map[string]string{"subtype": "file"}}

	if rankTier(real) >= rankTier(shadow) {
		t.Fatalf("real (%d) should rank above shadow (%d)", rankTier(real), rankTier(shadow))
	}
	if rankTier(shadow) >= rankTier(container) {
		t.Fatalf("shadow (%d) should rank above container (%d)", rankTier(shadow), rankTier(container))
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
