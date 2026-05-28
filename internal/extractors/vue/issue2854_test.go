package vue_test

import (
	"testing"

	"github.com/cajasmota/archigraph/internal/types"
)

// Issue #2854 — Vue Structure-group: context_extraction (provide/inject) and
// hook_recognition (composables).
func TestIssue2854_VueContextAndComposables(t *testing.T) {
	src := `<script setup lang="ts">
import { provide, inject } from 'vue';
import { useTheme } from './composables/useTheme';

function useCounter() {
  const n = ref(0);
  return { n };
}

const theme = useTheme();
const counter = useCounter();
provide('user', { id: 1 });
const cfg = inject('appConfig');
</script>

<template>
  <ChildPanel />
</template>`

	ents := extract(t, "src/Dashboard.vue", src)

	comp := findByName(ents, "Dashboard")
	if comp == nil {
		t.Fatalf("Dashboard component not extracted")
	}

	// context_extraction: provide('user') provider + inject('appConfig') consumer.
	if !hasCtx(ents, "provide_context", "user") {
		t.Errorf("missing provide_context entity for key 'user'; ents=%s", dump(ents))
	}
	if !hasCtx(ents, "inject_context", "appConfig") {
		t.Errorf("missing inject_context entity for key 'appConfig'")
	}
	if !relTarget(comp, "USES", "provider:user") {
		t.Errorf("Dashboard missing USES → provider:user; rels=%v", comp.Relationships)
	}
	if !relTarget(comp, "USES", "consumer:appConfig") {
		t.Errorf("Dashboard missing USES → consumer:appConfig")
	}

	// hook_recognition: useTheme + useCounter USES_HOOK edges; useCounter def.
	if !relTarget(comp, "USES_HOOK", "useTheme") {
		t.Errorf("Dashboard missing USES_HOOK → useTheme; rels=%v", comp.Relationships)
	}
	if !relTarget(comp, "USES_HOOK", "useCounter") {
		t.Errorf("Dashboard missing USES_HOOK → useCounter")
	}
	if findBySub(ents, "vue_composable", "useCounter") == nil {
		t.Errorf("missing vue_composable entity for useCounter")
	}
}

func hasCtx(ents []types.EntityRecord, subtype, key string) bool {
	for _, e := range ents {
		if e.Subtype == subtype && e.Properties["context_key"] == key {
			return true
		}
	}
	return false
}

func findBySub(ents []types.EntityRecord, subtype, name string) *types.EntityRecord {
	for i := range ents {
		if ents[i].Subtype == subtype && ents[i].Name == name {
			return &ents[i]
		}
	}
	return nil
}

func dump(ents []types.EntityRecord) string {
	out := ""
	for _, e := range ents {
		out += e.Subtype + ":" + e.Name + " "
	}
	return out
}
