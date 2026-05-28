package vue_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2855 — Vue Data-Flow group: prop_extraction (defineProps),
// state_management (Pinia + ref/reactive), data_fetching (useFetch/axios),
// branch_conditions (v-if/v-show).
func TestIssue2855_VueDataFlow(t *testing.T) {
	src := `<script setup lang="ts">
import { ref, reactive } from 'vue'
import { useUserStore } from '@/stores/user'
import axios from 'axios'

const props = defineProps<{ title: string; count?: number }>()

const userStore = useUserStore()
const loading = ref(false)
const form = reactive({ name: '' })

async function load() {
  const res = await useFetch('/api/users')
  await axios.get('/api/profile')
}
</script>

<template>
  <div v-if="loading">Loading</div>
  <UserPanel v-else v-show="count > 0" />
</template>`

	ents := extract(t, "src/UserCard.vue", src)

	comp := findByName(ents, "UserCard")
	if comp == nil {
		t.Fatalf("UserCard not extracted; %s", dump(ents))
	}

	has := func(subtype, name string) bool {
		for i := range ents {
			e := &ents[i]
			if e.Subtype == subtype && e.Name == name {
				return true
			}
		}
		return false
	}
	hasSub := func(subtype string) bool {
		for i := range ents {
			if ents[i].Subtype == subtype {
				return true
			}
		}
		return false
	}

	// prop_extraction: defineProps generic fields.
	if !has("component_prop", "title") {
		t.Errorf("missing component_prop title; %s", dump(ents))
	}
	if !has("component_prop", "count") {
		t.Errorf("missing component_prop count")
	}

	// state_management: Pinia store + ref/reactive primitives.
	if !has("state_store", "useUserStore") {
		t.Errorf("missing state_store useUserStore; %s", dump(ents))
	}
	if !has("reactive_state", "loading") {
		t.Errorf("missing reactive_state loading")
	}
	if !has("reactive_state", "form") {
		t.Errorf("missing reactive_state form")
	}

	// data_fetching: useFetch + axios.
	if !hasSub("data_fetch") {
		t.Errorf("missing data_fetch entities; %s", dump(ents))
	}

	// branch_conditions: v-if + v-show.
	branchKinds := map[string]bool{}
	for i := range ents {
		if ents[i].Subtype == "branch_condition" {
			branchKinds[ents[i].Properties["branch_kind"]] = true
		}
	}
	if !branchKinds["v-if"] {
		t.Errorf("missing branch_condition v-if; %s", dump(ents))
	}
	if !branchKinds["v-show"] {
		t.Errorf("missing branch_condition v-show")
	}

	// CONTAINS wiring from component to a prop.
	var titleProp *types.EntityRecord
	for i := range ents {
		if ents[i].Subtype == "component_prop" && ents[i].Name == "title" {
			titleProp = &ents[i]
		}
	}
	if titleProp == nil || !relTarget(comp, "CONTAINS", "title") {
		t.Errorf("component missing CONTAINS → title prop; rels=%v", comp.Relationships)
	}
}

// TestIssue2855_VueRealData runs the registered Vue extractor over the in-repo
// real-world SFC fixture and asserts the Data-Flow signals fire on real-shaped
// source (not only the hand-written unit fixture above).
func TestIssue2855_VueRealData(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "real-world", "vue", "UserCard.vue")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("real-world vue fixture not present: %v", err)
	}
	ents := extract(t, path, string(content))

	count := func(subtype string) int {
		n := 0
		for i := range ents {
			if ents[i].Subtype == subtype {
				n++
			}
		}
		return n
	}
	if count("component_prop") < 3 {
		t.Errorf("vue component_prop = %d, want >= 3 (userId/title/expanded); %s", count("component_prop"), dump(ents))
	}
	if count("state_store") < 1 || count("reactive_state") < 2 {
		t.Errorf("vue state_management: store=%d reactive=%d, want store>=1 reactive>=2", count("state_store"), count("reactive_state"))
	}
	if count("data_fetch") < 1 {
		t.Errorf("vue data_fetch = %d, want >= 1 (useFetch/axios)", count("data_fetch"))
	}
	if count("branch_condition") < 2 {
		t.Errorf("vue branch_condition = %d, want >= 2 (v-if/v-show)", count("branch_condition"))
	}
	t.Logf("real-data vue: props=%d store=%d reactive=%d fetch=%d branch=%d",
		count("component_prop"), count("state_store"), count("reactive_state"),
		count("data_fetch"), count("branch_condition"))
}
