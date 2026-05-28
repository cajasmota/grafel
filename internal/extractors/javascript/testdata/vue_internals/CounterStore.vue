<script lang="ts">
// pinia_store proving fixture (issue #2890).
//
// Exercises the dedicated Pinia store entity model: a defineStore() call is
// entitized as a `pinia_store` operation whose `state`, `getters` and
// `actions` surface is broken out into `pinia_state` / `pinia_getter` /
// `pinia_action` member entities (CONTAINS from the store). Both the
// options-object syntax and the setup-function syntax are covered.
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

// Options syntax: state () => ({ … }), getters: { … }, actions: { … }.
export const useCounterStore = defineStore('counter', {
  state: () => ({
    count: 0,
    label: 'counter',
  }),
  getters: {
    doubled(state) {
      return state.count * 2
    },
    isPositive: (state) => state.count > 0,
  },
  actions: {
    increment() {
      this.count++
    },
    async load() {
      this.count = await Promise.resolve(0)
    },
  },
})

// Options syntax with a single-line state object (the common real-world form).
export const useCartStore = defineStore('cart', {
  state: () => ({ items: [], total: 0 }),
  actions: {
    add(item) { this.items.push(item) },
  },
})

// Setup syntax: ref/reactive → state, computed → getter, function → action.
export const useSessionStore = defineStore('session', () => {
  const token = ref('')
  const isAuthed = computed(() => token.value !== '')
  function setToken(t: string) {
    token.value = t
  }
  const clear = () => {
    token.value = ''
  }
  return { token, isAuthed, setToken, clear }
})
</script>
