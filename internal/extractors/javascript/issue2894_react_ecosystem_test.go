// Package javascript_test — issue #2894 PR1 React Ecosystem proving tests.
//
// Proves the four PR1 framework_specific["React Ecosystem"] cells against the
// hand-written fixtures testdata/react_ecosystem/Store.tsx (Redux/RTK family +
// redux-saga) and testdata/react_ecosystem/Queries.tsx (TanStack/React Query +
// RTK Query). Each assertion is the proving artifact for a coverage cell:
//   - redux_store_extraction   → redux_slice / redux_store entities + reducers.
//   - redux_async_flow         → redux_async_thunk entity (createAsyncThunk).
//   - rtk_query_extraction     → rtk_query_api + rtk_query_endpoint entities.
//   - tanstack_query_extraction→ query_client entity + useQuery USES_HOOK edges.
package javascript_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

func ecoEntity(t *testing.T, ents []types.EntityRecord, name, wantSubtype string) *types.EntityRecord {
	t.Helper()
	e := findByName(ents, name)
	if e == nil {
		t.Fatalf("%s not extracted; names: %v", name, entityNames(ents))
	}
	if e.Properties["subtype"] != wantSubtype && e.Subtype != wantSubtype {
		t.Errorf("%s: subtype=%q (props=%q), want %q", name, e.Subtype, e.Properties["subtype"], wantSubtype)
	}
	return e
}

func TestIssue2894_ReduxStoreExtraction(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/Store.tsx")

	// redux_store_extraction — createSlice → redux_slice with reducers/actions.
	slice := ecoEntity(t, ents, "userSlice", "redux_slice")
	if slice.Properties["slice_name"] != "user" {
		t.Errorf("userSlice slice_name=%q, want \"user\"", slice.Properties["slice_name"])
	}
	if slice.Properties["reducers"] == "" {
		t.Errorf("userSlice reducers empty; props=%v", slice.Properties)
	}
	// reducers emitted as standalone operations + CONTAINS edge.
	if e := findByName(ents, "userSlice::setName"); e == nil {
		t.Errorf("userSlice::setName reducer entity not emitted; names: %v", entityNames(ents))
	}
	foundContains := false
	for _, r := range slice.Relationships {
		if r.Kind == "CONTAINS" && r.ToID == "userSlice::setName" {
			foundContains = true
		}
	}
	if !foundContains {
		t.Errorf("userSlice missing CONTAINS→userSlice::setName; rels=%v", slice.Relationships)
	}

	// configureStore + createStore → redux_store.
	store := ecoEntity(t, ents, "store", "redux_store")
	if store.Properties["factory"] != "configureStore" {
		t.Errorf("store factory=%q, want configureStore", store.Properties["factory"])
	}
	if store.Properties["reducer_slices"] == "" {
		t.Errorf("store reducer_slices empty; props=%v", store.Properties)
	}
	legacy := ecoEntity(t, ents, "legacyStore", "redux_store")
	if legacy.Properties["factory"] != "createStore" {
		t.Errorf("legacyStore factory=%q, want createStore", legacy.Properties["factory"])
	}

	// createEntityAdapter recognised.
	ecoEntity(t, ents, "usersAdapter", "entity_adapter")
}

func TestIssue2894_ReduxAsyncFlow(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/Store.tsx")
	thunk := ecoEntity(t, ents, "fetchUser", "redux_async_thunk")
	if thunk.Properties["action_type"] != "user/fetch" {
		t.Errorf("fetchUser action_type=%q, want \"user/fetch\"", thunk.Properties["action_type"])
	}
	if thunk.Properties["via"] != "redux_async" {
		t.Errorf("fetchUser via=%q, want redux_async", thunk.Properties["via"])
	}

	// redux-saga watcher + worker decoration.
	root := findByName(ents, "rootSaga")
	if root == nil {
		t.Fatalf("rootSaga generator not extracted")
	}
	if root.Properties["saga_role"] != "watcher" {
		t.Errorf("rootSaga saga_role=%q, want watcher; props=%v", root.Properties["saga_role"], root.Properties)
	}
	worker := findByName(ents, "loadUserSaga")
	if worker == nil {
		t.Fatalf("loadUserSaga generator not extracted")
	}
	if worker.Properties["saga_role"] != "worker" {
		t.Errorf("loadUserSaga saga_role=%q, want worker; props=%v", worker.Properties["saga_role"], worker.Properties)
	}
}

func TestIssue2894_RTKQueryExtraction(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/Queries.tsx")

	api := ecoEntity(t, ents, "usersApi", "rtk_query_api")
	if api.Properties["reducer_path"] != "usersApi" {
		t.Errorf("usersApi reducer_path=%q, want usersApi", api.Properties["reducer_path"])
	}
	if api.Properties["http_linkable"] != "true" {
		t.Errorf("usersApi http_linkable=%q, want true", api.Properties["http_linkable"])
	}
	// endpoints emitted with method + path; cross-repo linkable.
	getUsers := ecoEntity(t, ents, "usersApi::getUsers", "rtk_query_endpoint")
	if getUsers.Properties["endpoint_kind"] != "query" {
		t.Errorf("getUsers endpoint_kind=%q, want query", getUsers.Properties["endpoint_kind"])
	}
	if getUsers.Properties["http_path"] != "/users" {
		t.Errorf("getUsers http_path=%q, want /users", getUsers.Properties["http_path"])
	}
	addUser := ecoEntity(t, ents, "usersApi::addUser", "rtk_query_endpoint")
	if addUser.Properties["endpoint_kind"] != "mutation" {
		t.Errorf("addUser endpoint_kind=%q, want mutation", addUser.Properties["endpoint_kind"])
	}

	// injectEndpoints extends an existing api.
	ext := ecoEntity(t, ents, "extendedApi", "rtk_query_api")
	if ext.Properties["injected"] != "true" {
		t.Errorf("extendedApi injected=%q, want true", ext.Properties["injected"])
	}
	if findByName(ents, "extendedApi::deleteUser") == nil {
		t.Errorf("extendedApi::deleteUser endpoint not emitted")
	}
}

func TestIssue2894_TanstackQueryExtraction(t *testing.T) {
	ents := extractTSXFixture(t, "react_ecosystem/Queries.tsx")

	// useQuery/useMutation/useInfiniteQuery surface as USES_HOOK edges from the
	// custom hooks (generic hook_recognition pass) — the dominant data-layer
	// recall path. Confirm each TanStack hook fires.
	wantHook := func(consumer, hook string) {
		c := findByName(ents, consumer)
		if c == nil {
			t.Fatalf("%s custom hook not extracted", consumer)
		}
		for _, r := range c.Relationships {
			if r.Kind == "USES_HOOK" && r.ToID == hook {
				return
			}
		}
		t.Errorf("%s missing USES_HOOK→%s; rels=%v", consumer, hook, c.Relationships)
	}
	wantHook("useUsers", "useQuery")
	wantHook("useUserFeed", "useInfiniteQuery")
	wantHook("useCreateUser", "useMutation")
	wantHook("useCreateUser", "useQueryClient")
}
