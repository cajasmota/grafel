// Tests for the Django models-import suffix rewrite pass.
//
// Refs PR #580 wave-10 Chain-fix A.
package engine

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func TestRewritePythonModelImports(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   types.RelationshipRecord
		want string
	}{
		{
			name: "Serializer suffix → Component",
			in:   types.RelationshipRecord{Kind: "IMPORTS", ToID: "Model:UserSerializer"},
			want: "Component:UserSerializer",
		},
		{
			name: "ViewSet suffix → View (IMPORTS)",
			in:   types.RelationshipRecord{Kind: "IMPORTS", ToID: "Model:UserViewSet"},
			want: "View:UserViewSet",
		},
		{
			name: "ViewSet suffix → View (DEPENDS_ON)",
			in:   types.RelationshipRecord{Kind: "DEPENDS_ON", ToID: "Model:OrderViewSet"},
			want: "View:OrderViewSet",
		},
		{
			name: "ListView suffix → View",
			in:   types.RelationshipRecord{Kind: "IMPORTS", ToID: "Model:UserListView"},
			want: "View:UserListView",
		},
		{
			name: "Plain View suffix → View",
			in:   types.RelationshipRecord{Kind: "IMPORTS", ToID: "Model:DashboardView"},
			want: "View:DashboardView",
		},
		{
			name: "lowercase Viewset typo → View",
			in:   types.RelationshipRecord{Kind: "DEPENDS_ON", ToID: "Model:ScheduleViewset"},
			want: "View:ScheduleViewset",
		},
		{
			name: "Genuine Model name preserved",
			in:   types.RelationshipRecord{Kind: "IMPORTS", ToID: "Model:User"},
			want: "Model:User",
		},
		{
			name: "Non-Model prefix untouched",
			in:   types.RelationshipRecord{Kind: "IMPORTS", ToID: "Component:UserSerializer"},
			want: "Component:UserSerializer",
		},
		{
			name: "Non-IMPORTS/DEPENDS_ON edge untouched",
			in:   types.RelationshipRecord{Kind: "CALLS", ToID: "Model:UserSerializer"},
			want: "Model:UserSerializer",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := rewritePythonModelImports([]types.RelationshipRecord{tc.in})
			if got := out[0].ToID; got != tc.want {
				t.Errorf("ToID: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyDjangoModelImportName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"UserSerializer":       "Component",
		"OrderViewSet":         "View",
		"OrderViewset":         "View",
		"DetailView":           "View",
		"BlogListView":         "View",
		"DashboardView":        "View",
		"APIView":              "View",
		"User":                 "",
		"Article":              "",
		"DocumentTemplate":     "",
		"ChecklistItem":        "",
		"PermissionPagesEnum":  "",
		"MA_JURISDICTION_NAME": "",
	}
	for name, want := range cases {
		name, want := name, want
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := classifyDjangoModelImportName(name); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
