package resolve

import "regexp"

// vueDynamicPatterns are per-language patterns for Vue 3 Single File Components.
// Registered via init() into dynamicPatternsByLang.
//
// Vue SFCs share much of their dynamic pattern space with JavaScript / TypeScript
// (relative imports, process.env, React-style setter hooks in JS codebases that
// also use Vue). We inherit jsDynamicPatterns and extend with Vue-specific patterns.
//
// # Classification context
//
// The Vue extractor emits CALLS edges from Composition API calls and template
// render references. Three groups of callee shapes appear as unresolvable stubs:
//
//  1. Composition API built-ins (ref, reactive, computed, watch, watchEffect,
//     onMounted, onUnmounted, …) — these are Vue-provided symbols imported from
//     "vue". Without a full type-resolution pass the resolver cannot bind them
//     to the vue package entity. The `^(ref|reactive|computed|...)$` patterns
//     below classify them as Dynamic so they don't inflate bug-resolver counts.
//
//  2. Composable conventions — functions named `^use[A-Z]...` are the universal
//     Vue Composables convention (useRouter, useStore, useI18n, useFetch, …).
//     Installed libraries (vue-router, vuex, pinia, nuxt) all follow this pattern.
//     The resolver cannot distinguish a library composable from a user-defined one
//     without knowing which composables are bundled; the convention is safe to
//     gate because bare-word composable names rarely collide with real Go/Python/
//     Java symbols.
//
//  3. Vuex / Pinia helpers (mapState, mapGetters, mapActions, defineStore, …) —
//     these are Vuex/Pinia exports imported and called by name. The resolver
//     cannot bind bare `mapState` without knowing the Vuex version and how it is
//     configured. Conservative gate: exact-name matches only.
//
//  4. Vue Router (useRouter, useRoute, routerLink) — imported from "vue-router"
//     and called with the receiver stripped after template compilation. Exact-name
//     matches keep the gate conservative.
//
//  5. Vue template lifecycle events (v-model, v-if, @click handler names) —
//     the extractor does NOT emit these as CALLS edges; they are handled via
//     RENDERS edges instead. No dynamic patterns needed.
//
// # JS inheritance
//
// vueDynamicPatterns inherits jsDynamicPatterns so that relative-import paths
// (./foo, ../bar), process.env, eval, setState variants, and React-style
// handler conventions (^on[A-Z]…, ^handle[A-Z]…) are all classified correctly
// in Vue codebases that mix Vue + JS idioms. The per-language gate on
// jsDynamicPatterns ensures these patterns only fire for "javascript",
// "typescript", and now "vue" — not for Go / Python / Rust.
var vueSpecificPatterns = []*regexp.Regexp{
	// --- Composition API built-ins -------------------------------------------
	// Core Vue 3 reactivity and lifecycle primitives imported from "vue".
	// The extractor emits these as bare CALLS edge targets; without type
	// resolution the resolver cannot bind them to the "vue" package entity.
	regexp.MustCompile(`^ref$`),
	regexp.MustCompile(`^reactive$`),
	regexp.MustCompile(`^computed$`),
	regexp.MustCompile(`^watch$`),
	regexp.MustCompile(`^watchEffect$`),
	regexp.MustCompile(`^watchPostEffect$`),
	regexp.MustCompile(`^watchSyncEffect$`),
	regexp.MustCompile(`^onMounted$`),
	regexp.MustCompile(`^onUnmounted$`),
	regexp.MustCompile(`^onBeforeMount$`),
	regexp.MustCompile(`^onBeforeUnmount$`),
	regexp.MustCompile(`^onUpdated$`),
	regexp.MustCompile(`^onBeforeUpdate$`),
	regexp.MustCompile(`^onActivated$`),
	regexp.MustCompile(`^onDeactivated$`),
	regexp.MustCompile(`^onErrorCaptured$`),
	regexp.MustCompile(`^onRenderTracked$`),
	regexp.MustCompile(`^onRenderTriggered$`),
	regexp.MustCompile(`^onServerPrefetch$`),
	regexp.MustCompile(`^provide$`),
	regexp.MustCompile(`^inject$`),
	regexp.MustCompile(`^nextTick$`),
	regexp.MustCompile(`^createApp$`),
	regexp.MustCompile(`^defineComponent$`),
	regexp.MustCompile(`^defineAsyncComponent$`),
	regexp.MustCompile(`^resolveComponent$`),
	regexp.MustCompile(`^resolveDynamicComponent$`),
	regexp.MustCompile(`^toRef$`),
	regexp.MustCompile(`^toRefs$`),
	regexp.MustCompile(`^toRaw$`),
	regexp.MustCompile(`^markRaw$`),
	regexp.MustCompile(`^shallowRef$`),
	regexp.MustCompile(`^shallowReactive$`),
	regexp.MustCompile(`^shallowReadonly$`),
	regexp.MustCompile(`^readonly$`),
	regexp.MustCompile(`^isRef$`),
	regexp.MustCompile(`^isReactive$`),
	regexp.MustCompile(`^isReadonly$`),
	regexp.MustCompile(`^isProxy$`),
	regexp.MustCompile(`^unref$`),
	regexp.MustCompile(`^triggerRef$`),
	regexp.MustCompile(`^customRef$`),
	regexp.MustCompile(`^h$`),         // Vue render function
	regexp.MustCompile(`^mergeProps$`),
	regexp.MustCompile(`^withCtx$`),
	regexp.MustCompile(`^withDirectives$`),
	regexp.MustCompile(`^withModifiers$`),
	regexp.MustCompile(`^renderList$`),
	regexp.MustCompile(`^renderSlot$`),
	regexp.MustCompile(`^openBlock$`),
	regexp.MustCompile(`^createBlock$`),
	regexp.MustCompile(`^createVNode$`),
	regexp.MustCompile(`^createTextVNode$`),
	regexp.MustCompile(`^createCommentVNode$`),
	regexp.MustCompile(`^createStaticVNode$`),
	regexp.MustCompile(`^normalizeClass$`),
	regexp.MustCompile(`^normalizeStyle$`),

	// --- Composable convention -----------------------------------------------
	// Functions named ^use[A-Z]... are universally Vue Composables (vue-router,
	// vuex, pinia, vueuse, nuxt, user-defined). The `use` prefix + PascalCase
	// continuation is the RFC-mandated convention in Vue's Composition API docs.
	// Per-language gate (vue only) keeps this from biting Go's UseCase methods
	// or Ruby's use_ helpers or Python's use_ naming.
	regexp.MustCompile(`^use[A-Z][A-Za-z0-9]*$`),

	// --- Vuex helpers --------------------------------------------------------
	regexp.MustCompile(`^mapState$`),
	regexp.MustCompile(`^mapGetters$`),
	regexp.MustCompile(`^mapActions$`),
	regexp.MustCompile(`^mapMutations$`),
	regexp.MustCompile(`^createStore$`),

	// --- Pinia ---------------------------------------------------------------
	regexp.MustCompile(`^defineStore$`),
	regexp.MustCompile(`^storeToRefs$`),
	regexp.MustCompile(`^acceptHMRUpdate$`),
	regexp.MustCompile(`^createPinia$`),

	// --- Vue Router ----------------------------------------------------------
	regexp.MustCompile(`^createRouter$`),
	regexp.MustCompile(`^createWebHistory$`),
	regexp.MustCompile(`^createWebHashHistory$`),
	regexp.MustCompile(`^createMemoryHistory$`),
	regexp.MustCompile(`^routerLink$`),
	regexp.MustCompile(`^RouterLink$`),
	regexp.MustCompile(`^RouterView$`),
}

// vueDynamicPatterns is the full catalog for the "vue" language key:
// JS-shared patterns extended with Vue-specific Composition API, composable
// convention, Vuex/Pinia, and Vue Router additions.
var vueDynamicPatterns = append(
	append([]*regexp.Regexp{}, jsDynamicPatterns...),
	vueSpecificPatterns...,
)

func init() {
	dynamicPatternsByLang["vue"] = vueDynamicPatterns
}
