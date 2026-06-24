package javascript_test

import (
	"testing"

	"github.com/cajasmota/grafel/internal/types"
)

// issue5488_rsc_datafetch_test.go — React Server Component data-fetch edges
// (#5488, epic #5479).
//
// Proves an async App-Router Server Component (a `page.tsx`/`layout.tsx` with no
// `'use client'`) emits server-side data-fetch edges to the data sources it awaits:
//   - `await getUser(id)` (a `*.server.ts` model fn / imported data fn) → CALLS
//     edge component→getUser, tagged rsc_data_fetch=true.
//   - `await fetch(url)` → a data_fetch site + READS_FROM edge, tagged
//     rsc_data_fetch=true.
// Negative: a `'use client'` component making the same calls is NOT tagged as an
// RSC data-fetch (those are client-side event handlers / effects).

// serverComponent returns the implicit Server Component marker entity (or nil).
func serverComponent(ents []types.EntityRecord) *types.EntityRecord {
	for i := range ents {
		if ents[i].Kind == "SCOPE.Pattern" && ents[i].Subtype == "server_component" {
			return &ents[i]
		}
	}
	return nil
}

func rscEdge(e *types.EntityRecord, kind, toLeaf string) *types.RelationshipRecord {
	if e == nil {
		return nil
	}
	for i := range e.Relationships {
		r := &e.Relationships[i]
		if r.Kind == kind && r.Properties["rsc_data_fetch"] == "true" {
			if toLeaf == "" || r.ToID == toLeaf {
				return r
			}
		}
	}
	return nil
}

func TestNextjs5488RSCCallsModelFn(t *testing.T) {
	src := `import { getUser } from '@/lib/users.server'

export default async function Page({ params }: { params: { id: string } }) {
  const user = await getUser(params.id)
  return <div>{user.name}</div>
}
`
	ents := extractNext(t, "app/users/[id]/page.tsx", src)
	sc := serverComponent(ents)
	if sc == nil {
		t.Fatal("expected implicit Server Component marker")
	}
	if sc.Properties["rsc_data_fetch"] != "true" {
		t.Errorf("server component should be tagged rsc_data_fetch=true, props=%v", sc.Properties)
	}
	e := rscEdge(sc, "CALLS", "getUser")
	if e == nil {
		t.Fatalf("expected CALLS edge component→getUser tagged rsc_data_fetch, rels=%v", sc.Relationships)
	}
	if e.Properties["rendering"] != "server" {
		t.Errorf("CALLS edge rendering = %q, want server", e.Properties["rendering"])
	}
}

func TestNextjs5488RSCMemberChainCall(t *testing.T) {
	src := `export default async function Page() {
  const users = await db.user.findMany()
  return <ul>{users.map(u => <li key={u.id}>{u.name}</li>)}</ul>
}
`
	ents := extractNext(t, "app/page.tsx", src)
	sc := serverComponent(ents)
	// member chain `db.user.findMany` binds on its leaf name.
	if rscEdge(sc, "CALLS", "findMany") == nil {
		t.Fatalf("expected CALLS edge component→findMany (db.user.findMany), rels=%v", relsOf(sc))
	}
}

func TestNextjs5488RSCAwaitFetch(t *testing.T) {
	src := `export default async function Page() {
  const res = await fetch('https://api.example.com/users')
  const users = await res.json()
  return <div>{users.length}</div>
}
`
	ents := extractNext(t, "app/dashboard/page.tsx", src)
	sc := serverComponent(ents)
	if sc == nil {
		t.Fatal("expected implicit Server Component marker")
	}
	e := rscEdge(sc, "READS_FROM", "")
	if e == nil {
		t.Fatalf("expected READS_FROM edge for await fetch, rels=%v", relsOf(sc))
	}
	if got := e.Properties["url"]; got != "https://api.example.com/users" {
		t.Errorf("fetch url = %q, want the literal URL", got)
	}
	// The data_fetch site entity must exist and be tagged.
	var site *types.EntityRecord
	for i := range ents {
		if ents[i].Kind == "SCOPE.Operation" && ents[i].Subtype == "data_fetch" {
			site = &ents[i]
		}
	}
	if site == nil {
		t.Fatal("expected a data_fetch site entity for await fetch")
	}
	if site.Properties["rsc_data_fetch"] != "true" {
		t.Errorf("data_fetch site should be tagged rsc_data_fetch=true, props=%v", site.Properties)
	}
}

func TestNextjs5488ClientComponentNotTagged(t *testing.T) {
	// A 'use client' component making the same calls inside a handler must NOT be
	// tagged as an RSC server data-fetch.
	src := `'use client'
import { getUser } from '@/lib/users.server'

export default function Page() {
  const onClick = async () => {
    const user = await getUser('1')
    await fetch('/api/log')
  }
  return <button onClick={onClick}>load</button>
}
`
	ents := extractNext(t, "app/profile/page.tsx", src)
	// No server_component marker (it's a client component), so no rsc edge anywhere.
	if sc := serverComponent(ents); sc != nil {
		t.Errorf("'use client' file must not produce a server_component marker")
	}
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Properties["rsc_data_fetch"] == "true" {
				t.Errorf("client component must not emit rsc_data_fetch edges, got %+v on %s", r, ents[i].Name)
			}
		}
	}
}

func relsOf(e *types.EntityRecord) []types.RelationshipRecord {
	if e == nil {
		return nil
	}
	return e.Relationships
}
