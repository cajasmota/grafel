// counts_migrate_5870_pr7a_test.go — deretain-flip PR7a (#5870).
//
// The whoami/dashboard/stats/dead-code/license entity+relationship COUNTS now
// source from lr.entityCount()/lr.relCount() (Reader flag-ON, Doc len flag-OFF)
// instead of len(lr.Doc.Entities)/len(lr.Doc.Relationships), so they stay
// correct after a future slice empties lr.Doc. This exercises the real
// count-consumer seam groupIndexCounts across both flag paths.
package mcp

import "testing"

func TestGroupIndexCounts_ReaderParity_PR7a(t *testing.T) {
	doc, r := loadPR7aFixture(t)

	withServeFromMMap(t, false)
	lgOff := &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"corpus": docFullRepo(doc)}}
	wantE, wantR, _ := groupIndexCounts(lgOff)
	if wantE != len(doc.Entities) || wantR != len(doc.Relationships) {
		t.Fatalf("flag-OFF counts e=%d r=%d, want %d/%d", wantE, wantR, len(doc.Entities), len(doc.Relationships))
	}

	withServeFromMMap(t, true)
	lgOn := &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"corpus": readerEmptiedRepo(t, doc, r)}}
	gotE, gotR, _ := groupIndexCounts(lgOn)
	if gotE != wantE || gotR != wantR {
		t.Fatalf("flag-ON(emptied Doc) counts e=%d r=%d, want %d/%d (len(Doc.*) would be 0)", gotE, gotR, wantE, wantR)
	}

	// Retired-Reader fallback → Doc len.
	lgRet := &LoadedGroup{Name: "g", Repos: map[string]*LoadedRepo{"corpus": readerFullRepoRetired(t, doc, r)}}
	fbE, fbR, _ := groupIndexCounts(lgRet)
	if fbE != wantE || fbR != wantR {
		t.Fatalf("retired-Reader counts e=%d r=%d, want %d/%d", fbE, fbR, wantE, wantR)
	}
}
