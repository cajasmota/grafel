package vue_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2856 — Vue Navigation (router_pattern) + Lifecycle
// (state_setter_emission).
//
//   - router_pattern        : vue-router createRouter({routes}), router.push/
//     replace, <router-link to> → NAVIGATES_TO edges.
//   - state_setter_emission : ref().value assignment + Pinia $patch →
//     state_setter operations + WRITES_TO edges.
func TestIssue2856_VueNavigation(t *testing.T) {
	src := `<script setup lang="ts">
import { useRouter, createRouter, createWebHistory } from 'vue-router'

const router = useRouter()

const r = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/home', component: Home },
    { path: '/users/:id', component: User },
  ],
})

function goSettings() {
  router.push('/settings')
  router.replace({ name: 'profile' })
}
</script>

<template>
  <router-link to="/about">About</router-link>
  <RouterLink :to="/contact">Contact</RouterLink>
</template>`

	ents := extract(t, "src/Nav.vue", src)

	routes := map[string]bool{}
	vias := map[string]bool{}
	for i := range ents {
		for _, r := range ents[i].Relationships {
			if r.Kind == "NAVIGATES_TO" {
				routes[r.Properties["route"]] = true
				vias[r.Properties["via"]] = true
			}
		}
	}

	for _, want := range []string{"/home", "/users/:id", "/settings", "profile", "/about", "/contact"} {
		if !routes[want] {
			t.Errorf("missing NAVIGATES_TO route %q; routes=%v vias=%v", want, routes, vias)
		}
	}
	for _, v := range []string{"route_table", "router_call", "router_link"} {
		if !vias[v] {
			t.Errorf("missing navigation via %q; vias=%v", v, vias)
		}
	}
}

func TestIssue2856_VueStateSetter(t *testing.T) {
	src := `<script setup lang="ts">
import { ref } from 'vue'
import { useUserStore } from '@/stores/user'

const count = ref(0)
const userStore = useUserStore()

function inc() {
  count.value = count.value + 1
  count.value += 1
  userStore.$patch({ name: 'x' })
}
</script>

<template><div>{{ count }}</div></template>`

	ents := extract(t, "src/Counter.vue", src)

	setters := map[string]string{} // name → state
	writes := map[string]bool{}
	for i := range ents {
		e := &ents[i]
		if e.Subtype != "state_setter" {
			continue
		}
		setters[e.Name] = e.Properties["state"]
		for _, r := range e.Relationships {
			if r.Kind == "WRITES_TO" {
				writes[r.ToID] = true
			}
		}
	}

	if setters["count.value="] != "count" {
		t.Errorf("missing state_setter count.value= → count; setters=%v; %s", setters, dump(ents))
	}
	if setters["userStore.$patch"] != "userStore" {
		t.Errorf("missing state_setter userStore.$patch → userStore; setters=%v", setters)
	}
	if !writes["state:count"] {
		t.Errorf("missing WRITES_TO state:count; writes=%v", writes)
	}
	if !writes["state:userStore"] {
		t.Errorf("missing WRITES_TO state:userStore; writes=%v", writes)
	}
}

// TestIssue2856_VueRealData exercises the registered Vue extractor over the
// in-repo real-world SFC fixture (extended for #2856) and asserts navigation +
// state-setter signals fire on real-shaped source.
func TestIssue2856_VueRealData(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world", "vue", "UserCard.vue")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real-world vue fixture not present: %v", err)
	}
	ents := extract(t, path, string(content))

	nav, setter := 0, 0
	for i := range ents {
		if ents[i].Subtype == "state_setter" {
			setter++
		}
		for _, r := range ents[i].Relationships {
			if r.Kind == "NAVIGATES_TO" {
				nav++
			}
		}
	}
	if nav < 1 {
		t.Errorf("vue real-data NAVIGATES_TO = %d, want >= 1; %s", nav, dump(ents))
	}
	if setter < 1 {
		t.Errorf("vue real-data state_setter = %d, want >= 1", setter)
	}
	t.Logf("real-data vue #2856: nav=%d setters=%d", nav, setter)
}

var _ = types.EntityRecord{}
