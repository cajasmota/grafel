package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// #6556: manifestFileNames carries one glob, "*.csproj", among thirteen
// literal filenames, and directoryHasManifest tests every entry with os.Stat,
// which does no glob expansion. So the C# entry could only ever match a file
// literally named "*.csproj", and a .NET project directory was never
// recognised as a backend boundary: the walk continued past it and
// owning_backend came out as an ancestor directory.
//
// The three rows below are the same layout in three ecosystems. Go and Node
// are the control: they find their manifest one directory above the handler,
// and must keep doing so.

// writeTree creates each named file (with its parent directories) under root.
func writeTree(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatalf("WriteFile(%q): %v", p, err)
		}
	}
}

func TestDeriveOwningBackend_ProjectFileManifest_6556(t *testing.T) {
	cases := []struct {
		name    string
		tree    []string
		handler string
		want    string
	}{
		{
			name:    "go.mod one level above the handler",
			tree:    []string{"services/orders/go.mod", "services/orders/handlers/order.go"},
			handler: "services/orders/handlers/order.go",
			want:    "orders",
		},
		{
			name:    "package.json one level above the handler",
			tree:    []string{"services/orders-api/package.json", "services/orders-api/src/routes/order.ts"},
			handler: "services/orders-api/src/routes/order.ts",
			want:    "orders-api",
		},
		{
			name:    "csproj one level above the handler",
			tree:    []string{"services/Orders.Api/Orders.Api.csproj", "services/Orders.Api/Controllers/OrderController.cs"},
			handler: "services/Orders.Api/Controllers/OrderController.cs",
			want:    "Orders.Api",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeTree(t, root, tc.tree...)
			t.Chdir(root)
			if got := deriveOwningBackend(tc.handler); got != tc.want {
				t.Errorf("deriveOwningBackend(%q) = %q, want %q", tc.handler, got, tc.want)
			}
		})
	}
}
