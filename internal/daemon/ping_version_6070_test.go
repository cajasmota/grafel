package daemon

import (
	"strings"
	"testing"

	"github.com/cajasmota/grafel/internal/daemon/proto"
	"github.com/cajasmota/grafel/internal/version"
)

// TestPing_ReportsBareReleaseAlongsideDisplayVersion is the daemon half of
// #6070.
//
// `grafel install` step 4 must decide whether the daemon answering the socket
// is the release it just installed. It used to derive that by string-matching
// the daemon's DECORATED version descriptor against a bare tag, which can never
// match — so the guard fired on the success path and aborted every install on
// every platform from v0.2.0 onward.
//
// The fix gives the installer a structured field to compare instead of a
// display string to parse. That only helps if the daemon actually POPULATES it,
// which is what this test pins: declaring the field in proto and forgetting to
// set it here would leave every daemon reporting an empty VersionBare, silently
// dropping the installer back onto the parse fallback forever.
func TestPing_ReportsBareReleaseAlongsideDisplayVersion(t *testing.T) {
	var reply proto.PingReply
	if err := (&Service{}).Ping(&proto.PingArgs{}, &reply); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if reply.VersionBare == "" {
		t.Fatal("PingReply.VersionBare is empty — the installer's release comparison " +
			"has nothing structured to compare and falls back to parsing the display string")
	}
	if reply.VersionBare != version.Version {
		t.Errorf("VersionBare = %q, want the bare release identifier version.Version = %q",
			reply.VersionBare, version.Version)
	}

	// The display field must keep its decoration — the two fields exist because
	// they differ. If they ever became identical, the structured field would be
	// pointless and callers comparing the display string would silently start
	// "working", re-hiding the defect.
	if reply.Version != version.String() {
		t.Errorf("Version = %q, want the decorated descriptor version.String() = %q",
			reply.Version, version.String())
	}
	if reply.Version == reply.VersionBare {
		t.Errorf("Version and VersionBare are both %q; the display field is supposed to "+
			"carry commit/build decoration that the bare field does not", reply.Version)
	}
	if !strings.HasPrefix(reply.Version, reply.VersionBare) {
		t.Errorf("Version %q does not start with VersionBare %q — install's parse "+
			"fallback for pre-v0.2.1 daemons extracts the leading token and would break",
			reply.Version, reply.VersionBare)
	}
}
