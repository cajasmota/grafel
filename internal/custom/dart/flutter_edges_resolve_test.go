package dart_test

import (
	"context"
	"testing"

	extreg "github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/resolve"
	"github.com/cajasmota/archigraph/internal/types"

	_ "github.com/cajasmota/archigraph/internal/custom/dart"
)

func isHex16(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// TestFlutterEdgesResolveToHostAndTarget proves the screen graph materializes:
// the bare-name FromID (enclosing widget) and a widget-target ToID both
// rewrite to concrete hex entity IDs through the standard resolver path.
func TestFlutterEdgesResolveToHostAndTarget(t *testing.T) {
	src := `
class HomeScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () => Navigator.push(
        context,
        MaterialPageRoute(builder: (_) => const DetailScreen()),
      ),
    );
  }
}

class DetailScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) => Container();
}
`
	e, ok := extreg.Get("custom_dart_flutter")
	if !ok {
		t.Fatal("extractor not registered")
	}
	ents, err := e.Extract(context.Background(), extreg.FileInput{
		Path: "home.dart", Language: "dart", Content: []byte(src),
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	idx := resolve.BuildIndex(ents)
	resolve.ReferencesEmbedded(ents, idx)

	var homeID, detailID string
	for _, en := range ents {
		switch en.Name {
		case "HomeScreen":
			homeID = en.ID
		case "DetailScreen":
			detailID = en.ID
		}
	}
	if homeID == "" || detailID == "" {
		t.Fatalf("missing widget entities; got %d entities", len(ents))
	}

	var found *types.RelationshipRecord
	for ei := range ents {
		for ri := range ents[ei].Relationships {
			r := &ents[ei].Relationships[ri]
			if r.Kind == "NAVIGATES_TO" {
				found = r
			}
		}
	}
	if found == nil {
		t.Fatal("no NAVIGATES_TO edge emitted")
	}
	if found.FromID != homeID {
		t.Errorf("FromID = %q, want HomeScreen hex %q", found.FromID, homeID)
	}
	if found.ToID != detailID {
		t.Errorf("ToID = %q, want DetailScreen hex %q", found.ToID, detailID)
	}
	if !isHex16(found.FromID) || !isHex16(found.ToID) {
		t.Errorf("edge endpoints not both hex: from=%q to=%q", found.FromID, found.ToID)
	}
}
