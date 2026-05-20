package vue_test

import (
	"context"
	"testing"

	"github.com/cajasmota/archigraph/internal/extractor"
	_ "github.com/cajasmota/archigraph/internal/extractors/vue" // trigger init()
	"github.com/cajasmota/archigraph/internal/types"
)

// ---- helpers ----------------------------------------------------------------

func extract(t *testing.T, path string, src string) []types.EntityRecord {
	t.Helper()
	ext, ok := extractor.Get("vue")
	if !ok {
		t.Fatal("vue extractor not registered")
	}
	got, err := ext.Extract(context.Background(), extractor.FileInput{
		Path:     path,
		Content:  []byte(src),
		Language: "vue",
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	return got
}

func findByName(entities []types.EntityRecord, name string) *types.EntityRecord {
	for i := range entities {
		if entities[i].Name == name {
			return &entities[i]
		}
	}
	return nil
}

func findBySubtype(entities []types.EntityRecord, subtype string) []*types.EntityRecord {
	var out []*types.EntityRecord
	for i := range entities {
		if entities[i].Subtype == subtype {
			out = append(out, &entities[i])
		}
	}
	return out
}

func hasRelKind(entity *types.EntityRecord, kind string) bool {
	for _, r := range entity.Relationships {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

func relTarget(entity *types.EntityRecord, kind, toID string) bool {
	for _, r := range entity.Relationships {
		if r.Kind == kind && r.ToID == toID {
			return true
		}
	}
	return false
}

// ---- Registration -----------------------------------------------------------

func TestExtractor_Language(t *testing.T) {
	ext, ok := extractor.Get("vue")
	if !ok {
		t.Fatal("vue extractor not registered")
	}
	if ext.Language() != "vue" {
		t.Errorf("Language() = %q, want %q", ext.Language(), "vue")
	}
}

// ---- Empty / degenerate input -----------------------------------------------

func TestExtract_EmptyFile(t *testing.T) {
	entities := extract(t, "src/App.vue", "")
	if len(entities) == 0 {
		t.Fatal("expected at least 1 entity (degraded) for empty file")
	}
	comp := entities[0]
	if comp.EnrichmentStatus != types.StatusDegraded {
		t.Errorf("empty file: expected degraded, got %q", comp.EnrichmentStatus)
	}
}

func TestExtract_NoScriptNoTemplate(t *testing.T) {
	src := `<style scoped>
.foo { color: red; }
</style>`
	entities := extract(t, "src/Plain.vue", src)
	// Should at least return file entity + component entity
	if len(entities) < 1 {
		t.Fatal("expected at least 1 entity")
	}
}

// ---- Composition API (script setup) fixture ---------------------------------

// Vue 3 Composition API with <script setup> fixture
const compositionSFC = `<template>
  <div class="counter">
    <h1>{{ title }}</h1>
    <p>Count: {{ count }}</p>
    <ButtonBase @click="increment" label="+" />
    <ModalDialog v-if="showModal" @close="showModal = false" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useStore } from 'vuex'
import ButtonBase from './components/ButtonBase.vue'
import ModalDialog from './components/ModalDialog.vue'

const props = defineProps<{
  title: string
  initialCount?: number
}>()

const emit = defineEmits<{
  (e: 'update', value: number): void
}>()

defineExpose({ increment })

const count = ref(props.initialCount ?? 0)
const doubled = computed(() => count.value * 2)
const showModal = ref(false)

const router = useRouter()
const store = useStore()

watch(count, (newVal) => {
  emit('update', newVal)
})

onMounted(() => {
  console.log('mounted', count.value)
})

function increment() {
  count.value++
}
</script>

<style scoped>
.counter { padding: 1rem; }
</style>
`

func TestExtract_CompositionAPI(t *testing.T) {
	entities := extract(t, "src/Counter.vue", compositionSFC)

	// Must have file entity
	fileEnt := findByName(entities, "src/Counter.vue")
	if fileEnt == nil {
		t.Error("expected file entity with name 'src/Counter.vue'")
	}

	// Must have component entity
	comp := findByName(entities, "Counter")
	if comp == nil {
		t.Fatal("expected component entity named 'Counter'")
	}
	if comp.Kind != "SCOPE.Component" {
		t.Errorf("component Kind = %q, want SCOPE.Component", comp.Kind)
	}
	if comp.Subtype != "vue_component" {
		t.Errorf("component Subtype = %q, want vue_component", comp.Subtype)
	}

	// Must have defineProps entity
	dp := findBySubtype(entities, "define_props")
	if len(dp) == 0 {
		t.Error("expected at least one define_props entity")
	}

	// Must have defineEmits entity
	de := findBySubtype(entities, "define_emits")
	if len(de) == 0 {
		t.Error("expected at least one define_emits entity")
	}

	// Must have defineExpose entity
	dex := findBySubtype(entities, "define_expose")
	if len(dex) == 0 {
		t.Error("expected at least one define_expose entity")
	}

	// Component must have CALLS edges for Composition API calls
	if !hasRelKind(comp, "CALLS") {
		t.Error("expected CALLS edges on component entity")
	}
	for _, callee := range []string{"ref", "computed", "onMounted", "watch", "useRouter", "useStore"} {
		found := false
		for _, r := range comp.Relationships {
			if r.Kind == "CALLS" && r.ToID == callee {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CALLS edge to %q", callee)
		}
	}

	// Component must have RENDERS edges for <ButtonBase> and <ModalDialog>
	if !relTarget(comp, "RENDERS", "ButtonBase") {
		t.Error("expected RENDERS edge to ButtonBase")
	}
	if !relTarget(comp, "RENDERS", "ModalDialog") {
		t.Error("expected RENDERS edge to ModalDialog")
	}

	// Must have IMPORTS edges on file entity (from import statements)
	if fileEnt != nil {
		importsCount := 0
		for _, r := range fileEnt.Relationships {
			if r.Kind == "IMPORTS" {
				importsCount++
			}
		}
		if importsCount == 0 {
			// Check if import entities carry the IMPORTS edges
			importEnts := findBySubtype(entities, "import")
			importEdges := 0
			for _, ie := range importEnts {
				for _, r := range ie.Relationships {
					if r.Kind == "IMPORTS" {
						importEdges++
					}
				}
			}
			if importEdges == 0 {
				t.Error("expected IMPORTS edges from import statements")
			}
		}
	}
}

// ---- Options API fixture ---------------------------------------------------

const optionsSFC = `<template>
  <div>
    <UserCard :user="currentUser" />
    <ProductList :items="products" />
  </div>
</template>

<script>
import UserCard from './UserCard.vue'
import ProductList from './ProductList.vue'
import { mapState, mapActions } from 'vuex'

export default {
  name: 'Dashboard',
  components: {
    UserCard,
    ProductList,
  },
  props: {
    orgId: {
      type: String,
      required: true,
    },
  },
  data() {
    return {
      loading: false,
    }
  },
  computed: {
    ...mapState(['currentUser', 'products']),
  },
  methods: {
    loadData() {
      this.loading = true
      this.fetchProducts(this.orgId)
    },
    handleError(err) {
      console.error(err)
    },
    submitForm() {
      this.loadData()
    },
  },
  created() {
    this.loadData()
  },
}
</script>
`

func TestExtract_OptionsAPI(t *testing.T) {
	entities := extract(t, "src/Dashboard.vue", optionsSFC)

	// Component entity should use the `name:` field
	comp := findByName(entities, "Dashboard")
	if comp == nil {
		t.Fatal("expected component entity named 'Dashboard' (from name: field)")
	}
	if comp.Subtype != "vue_component" {
		t.Errorf("Subtype = %q, want vue_component", comp.Subtype)
	}

	// Options API methods
	methods := findBySubtype(entities, "method")
	methodNames := make(map[string]bool)
	for _, m := range methods {
		methodNames[m.Name] = true
	}
	for _, want := range []string{"loadData", "handleError", "submitForm"} {
		if !methodNames[want] {
			t.Errorf("expected method entity %q", want)
		}
	}

	// Component should RENDERS UserCard and ProductList
	if !relTarget(comp, "RENDERS", "UserCard") {
		t.Error("expected RENDERS edge to UserCard")
	}
	if !relTarget(comp, "RENDERS", "ProductList") {
		t.Error("expected RENDERS edge to ProductList")
	}

	// Vuex mapState / mapActions → CALLS edges
	foundMapState := false
	for _, r := range comp.Relationships {
		if r.Kind == "CALLS" && r.ToID == "mapState" {
			foundMapState = true
		}
	}
	if !foundMapState {
		t.Error("expected CALLS edge to mapState")
	}
}

// ---- Pinia store fixture ---------------------------------------------------

const piniaSFC = `<template>
  <div>
    <p>{{ counter.count }}</p>
    <CounterDisplay :value="counter.count" />
  </div>
</template>

<script setup>
import { useRouter, useRoute } from 'vue-router'
import { defineStore } from 'pinia'

const useCounterStore = defineStore('counter', {
  state: () => ({ count: 0 }),
  actions: {
    increment() { this.count++ },
  },
})

const counter = useCounterStore()
const router = useRouter()
const route = useRoute()
</script>
`

func TestExtract_Pinia(t *testing.T) {
	entities := extract(t, "src/PiniaDemo.vue", piniaSFC)

	comp := findByName(entities, "PiniaDemo")
	if comp == nil {
		t.Fatal("expected component entity PiniaDemo")
	}

	// Must have CALLS edges for defineStore, useRouter, useRoute
	for _, callee := range []string{"defineStore", "useRouter", "useRoute"} {
		found := false
		for _, r := range comp.Relationships {
			if r.Kind == "CALLS" && r.ToID == callee {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected CALLS edge to %q", callee)
		}
	}

	// CounterDisplay is a PascalCase child component
	if !relTarget(comp, "RENDERS", "CounterDisplay") {
		t.Error("expected RENDERS edge to CounterDisplay")
	}
}

// ---- Entity recall check --------------------------------------------------

// TestExtract_EntityRecall ensures >= 80% of expected entities are extracted
// from the comprehensive composition fixture.
func TestExtract_EntityRecall(t *testing.T) {
	entities := extract(t, "src/Counter.vue", compositionSFC)

	// Expected entities: file, component, defineProps, defineEmits, defineExpose
	// = 5 distinct named entities at minimum.
	type expectation struct {
		name    string
		kind    string
		subtype string
	}
	expected := []expectation{
		{"src/Counter.vue", "SCOPE.Component", "file"},
		{"Counter", "SCOPE.Component", "vue_component"},
		{"defineProps", "SCOPE.Operation", "define_props"},
		{"defineEmits", "SCOPE.Operation", "define_emits"},
		{"defineExpose", "SCOPE.Operation", "define_expose"},
	}

	found := 0
	for _, exp := range expected {
		for _, ent := range entities {
			if ent.Name == exp.name && ent.Kind == exp.kind && ent.Subtype == exp.subtype {
				found++
				break
			}
		}
	}

	recall := float64(found) / float64(len(expected))
	if recall < 0.80 {
		t.Errorf("entity recall = %.0f%% (%d/%d), want >= 80%%", recall*100, found, len(expected))
		for _, exp := range expected {
			matched := false
			for _, ent := range entities {
				if ent.Name == exp.name && ent.Kind == exp.kind && ent.Subtype == exp.subtype {
					matched = true
					break
				}
			}
			if !matched {
				t.Logf("  MISSING: name=%q kind=%q subtype=%q", exp.name, exp.kind, exp.subtype)
			}
		}
	}
}

// ---- No false positives ----------------------------------------------------

// TestExtract_NoFalsePositives verifies that pure <style>-only blocks and
// HTML-only templates don't produce spurious operation/call entities.
func TestExtract_NoFalsePositives(t *testing.T) {
	src := `<template>
  <div class="wrapper">
    <h1>Hello</h1>
    <p>World</p>
  </div>
</template>

<style scoped>
.wrapper { margin: 0 auto; }
</style>
`
	entities := extract(t, "src/HelloWorld.vue", src)

	// Only file entity and component entity expected — no operations, no calls
	for _, ent := range entities {
		if ent.Subtype == "define_props" || ent.Subtype == "define_emits" || ent.Subtype == "define_expose" {
			t.Errorf("unexpected entity with subtype %q in style-only SFC", ent.Subtype)
		}
		if ent.Kind == "SCOPE.Operation" {
			t.Errorf("unexpected SCOPE.Operation entity %q in style-only SFC", ent.Name)
		}
	}

	// Lowercase HTML tags should not produce RENDERS edges
	comp := findByName(entities, "HelloWorld")
	if comp != nil {
		for _, r := range comp.Relationships {
			if r.Kind == "RENDERS" {
				t.Errorf("false-positive RENDERS edge to %q (HTML tag, not Vue component)", r.ToID)
			}
		}
	}
}

// ---- Language tagging ------------------------------------------------------

func TestExtract_LanguageTaggedOnRelationships(t *testing.T) {
	entities := extract(t, "src/Counter.vue", compositionSFC)
	for _, ent := range entities {
		for _, r := range ent.Relationships {
			lang := r.Properties["language"]
			if lang != "vue" {
				t.Errorf("entity %q rel %q: language tag = %q, want %q", ent.Name, r.Kind, lang, "vue")
			}
		}
	}
}
