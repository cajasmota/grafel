package engine

import "testing"

// #6675 — a competing `use` declaration that is REJECTED for its own reasons
// (`self::`/`super::`-rooted, malformed module path) used to return before the
// binding map was touched, so it never poisoned the local name and the OTHER
// declaration was published as if unambiguous. Since #6669 resolves on
// (handler_module, handler_name), that MIS-JOINS rather than missing — the
// failure this arm's own rationale calls worse than a phantom.
//
// AXES. Varied here:
//
//   - the REJECTION SHAPE of the competitor: `super::`-rooted, `self::`-rooted,
//     `super::` inside a flat group, malformed module path — plus the two
//     shapes that must NOT poison, a glob and a nested group;
//   - the ORDER: the competitor PRECEDES the survivor in every shape and also
//     FOLLOWS it. Order is not incidental — poisoning has to both withdraw a
//     binding already published (competitor second) and block one not yet
//     published (competitor first), and those are two different lines;
//   - the BINDING AXIS: module (`crate::real` vs `super::stub`, same item) and
//     item (`create_item` vs `create_item_v2 as create_item`, same module).
//     #6670's round-3 review found the item axis entirely unpinned;
//   - the REASON two declarations coexist: a `cfg` pair and a `mod tests`
//     block. Neither is read by this pass, so this axis is cheap, but leaving
//     it constant would have left the claim "the pass ignores the attribute"
//     unobserved.
//
// Held CONSTANT, with the reason for each:
//
//   - SAME-FILE vs CROSS-FILE placement of the two competing declarations.
//     Both are always in src/router.rs, because rustUseBindings is handed one
//     file's `content` and there is no cross-file collision to construct: a
//     `use` in another file is not in this map and cannot contest anything in
//     it. The FIXTURE is nonetheless multi-file (the handler and its
//     `#[utoipa::path]` live in src/items.rs) — that is the only layout in
//     which a marker is emitted at all, and requireUtoipaDefIn is the premise
//     guard that the utoipa pass, not synthesizeAxumRoutes, is being observed.
//   - The local name (`create_item`) and the surviving module (`crate::real` /
//     `crate::handlers`). Nothing in rustAddUseLeaf branches on either string;
//     varying them would multiply the table without reaching a new line.
//   - The registration spelling, `routes!(create_item)` on a bare
//     `OpenApiRouter::new()`. The macro-recognition and mount-prefix paths are
//     pinned by the #6668 table and are upstream of everything under test here.
func TestUtoipaCrossFile_UpstreamRejectedCompetitorPoisons_6675(t *testing.T) {
	for _, tc := range []struct {
		label string
		uses  string
		want  int
	}{
		// ── WITNESS 1 of #6675: a `super::`-rooted competitor. ──────────────
		// Rejected by the relative-root refusal, which returned before the map
		// was touched; `crate::real` was then published as unambiguous even
		// though which cfg arm is live is unknowable here.
		{"witness1-super-competitor-follows", `#[cfg(feature = "x")]
use crate::real::create_item;
#[cfg(not(feature = "x"))]
use super::stub::create_item;`, 0},
		{"witness1-super-competitor-precedes", `#[cfg(not(feature = "x"))]
use super::stub::create_item;
#[cfg(feature = "x")]
use crate::real::create_item;`, 0},

		// The same rejection reached by the other relative root, and through a
		// flat group rather than a plain path — the group path recomputes
		// `module` from the base and could have lost the local name.
		{"self-competitor-follows", `use crate::real::create_item;
mod tests {
    use self::mocks::create_item;
}`, 0},
		{"super-in-group-competitor-precedes", `use super::{stub::create_item};
use crate::real::create_item;`, 0},

		// THE ITEM AXIS under a rejected competitor: one module, two items, the
		// alias making them one local name. The module axis alone would leave
		// the `name` half of the join key unpinned.
		{"item-axis-super-competitor-follows", `#[cfg(feature = "x")]
use crate::handlers::create_item;
#[cfg(not(feature = "x"))]
use super::handlers::create_item_v2 as create_item;`, 0},
		{"item-axis-super-competitor-precedes", `#[cfg(not(feature = "x"))]
use super::handlers::create_item_v2 as create_item;
#[cfg(feature = "x")]
use crate::handlers::create_item;`, 0},

		// A competitor rejected by the MODULE-VALIDITY guard rather than by the
		// relative-root one. `crate::{1bad::create_item}` yields the unusable
		// module `crate::1bad` — unpublishable, but it still names create_item.
		{"malformed-module-competitor-follows", `use crate::real::create_item;
mod tests {
    use crate::{1bad::create_item};
}`, 0},
		{"malformed-module-competitor-precedes", `use crate::{1bad::create_item};
use crate::real::create_item;`, 0},

		// OUTSIDE THE TWO WITNESSES, and reported as such: `{self as alias}`
		// binds the MODULE under a local name. It is unpublishable (a module is
		// not an item) but it DOES name a local, so under the contested-name
		// boundary it now poisons where before it was silent.
		{"self-as-alias-competitor-follows", `use crate::real::create_item;
use crate::items::{self as create_item};`, 0},

		// ── WITNESS 2 of #6675: a GLOB competitor. THE OVER-STRICT CONTROL. ──
		// `use crate::items::*;` names NO local. Nothing is contested, so the
		// surviving declaration must still publish. Poisoning here would drop a
		// legitimate marker silently — no error, just a missing enrichment —
		// which is why this direction is scored as well as the permissive one.
		{"witness2-glob-competitor-follows", `use crate::real::create_item;
use crate::items::*;`, 1},
		{"witness2-glob-competitor-precedes", `use crate::items::*;
use crate::real::create_item;`, 1},

		// The NESTED-GROUP carve-out, ruled in the code: its leaves are not
		// reliably parseable, so no local name is derived and none is poisoned.
		// Stated as a fixture so the carve-out is observed rather than asserted
		// in prose only.
		{"nested-group-competitor-follows", `use crate::real::create_item;
use crate::{items::{create_item, purge}, admin};`, 1},

		// A bare `self` leaf binds the MODULE's last segment, a name this leaf
		// does not carry — no local is derived, so nothing is contested.
		{"bare-self-leaf-competitor-follows", `use crate::real::create_item;
use crate::items::{self};`, 1},

		// CONTROLS. Without them the whole table passes under a mutant that
		// poisons unconditionally.
		{"single-binding-control", `use crate::real::create_item;`, 1},
		{"identical-duplicate-control", `use crate::real::create_item;
mod tests {
    use crate::real::create_item;
}`, 1},
	} {
		router := `
use utoipa_axum::router::OpenApiRouter;
use utoipa_axum::routes;
` + tc.uses + `

pub fn router() -> OpenApiRouter {
    OpenApiRouter::new().routes(routes!(create_item))
}
`
		ents := runUtoipaCrossFile(t, []utoipaCrossFileFile{
			{"src/items.rs", itemsModuleSrc},
			{"src/router.rs", router},
		})
		requireUtoipaDefIn(t, ents, "http:GET:/items", "list_items", "6675-rejected-competitor/"+tc.label)
		got := utoipaMarkers(ents)
		if len(got) != tc.want {
			t.Errorf("6675-rejected-competitor/%s: want %d marker(s), got %d (%v)",
				tc.label, tc.want, len(got), utoipaMarkerIDs(ents))
			continue
		}
		// THE INCIDENTAL BYTE. A count alone would pass on a marker that
		// survived with a DIFFERENT join key — e.g. a glob competitor that
		// poisoned `crate::real` and let some other declaration through. The
		// surviving marker must be the one the fixture names.
		for _, e := range got {
			if m := e.Properties["handler_module"]; m != "crate::real" {
				t.Errorf("6675-rejected-competitor/%s: handler_module = %q, want crate::real",
					tc.label, m)
			}
			if n := e.Properties["handler_name"]; n != "create_item" {
				t.Errorf("6675-rejected-competitor/%s: handler_name = %q, want create_item",
					tc.label, n)
			}
		}
	}
}
