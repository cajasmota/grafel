//go:build unix

package engine

// fifo_6478_test.go — deadline pin for the two engine reads
// docs/blocking-open-audit.md lists as name-chosen ("engine/coverage" family).
//
//   - LoadWrapperConfigs reads .grafel/wrappers.json INSIDE the indexed repo.
//   - parseQuarkusProperties opens src/main/resources/application.properties,
//     every segment of which is a literal, so no walker sits between the
//     attacker's `mkfifo` and the open.
//
// Both run during an index of an ordinary user repo, which is the whole point:
// the repo being indexed is not trusted input.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/grafel/internal/testsupport"
)

func TestLoadWrapperConfigsFIFODoesNotHang(t *testing.T) {
	repo := t.TempDir()
	testsupport.MkfifoInTemp(t, repo, ".grafel", "wrappers.json")

	var err error
	testsupport.MustReturn(t, "LoadWrapperConfigs with a FIFO at .grafel/wrappers.json", func() {
		_, err = LoadWrapperConfigs(repo)
	})
	if err == nil {
		t.Fatal("LoadWrapperConfigs returned a nil error for a FIFO; a refused wrapper config must be " +
			"reported, not silently treated as \"no wrappers configured\"")
	}
}

func TestParseQuarkusPropertiesFIFODoesNotHang(t *testing.T) {
	repo := t.TempDir()
	p := testsupport.MkfifoInTemp(t, repo, "src", "main", "resources", "application.properties")

	out := map[string]channelBinding{}
	testsupport.MustReturn(t, "parseQuarkusProperties with a FIFO application.properties", func() {
		parseQuarkusProperties(p, out)
	})
	if len(out) != 0 {
		t.Fatalf("parseQuarkusProperties produced %d bindings from a FIFO", len(out))
	}
}

// TestParseQuarkusPropertiesStillReadsARegularFile is the positive control: a
// parser that refused every file would pass the test above and silently delete
// every Kafka edge grafel finds in a Quarkus repo.
func TestParseQuarkusPropertiesStillReadsARegularFile(t *testing.T) {
	repo := t.TempDir()
	p := filepath.Join(repo, "application.properties")
	const body = "mp.messaging.incoming.orders.topic=orders\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := map[string]channelBinding{}
	parseQuarkusProperties(p, out)
	if len(out) == 0 {
		t.Fatal("parseQuarkusProperties parsed nothing from a valid properties file; the guard is " +
			"refusing regular files")
	}
}
