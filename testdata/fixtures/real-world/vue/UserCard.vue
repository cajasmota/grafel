<!-- Source: synthetic, modelled on real Vue 3 SFC data-flow patterns
     (defineProps, Pinia store + ref/reactive state, useFetch/axios fetching,
     v-if/v-show branches, vue-router navigation) | License: MIT

     Used by issue #2855 (Data Flow group) + #2856 (Navigation + Lifecycle)
     real-data verification. -->
<script setup lang="ts">
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import { useUserStore } from '@/stores/user'

const router = useRouter()

const props = defineProps<{
  userId: string
  title?: string
  expanded?: boolean
}>()

const emit = defineEmits<{ (e: 'select', id: string): void }>()

const userStore = useUserStore()
const loading = ref(false)
const form = reactive({ name: '', email: '' })

const displayTitle = computed(() => props.title ?? 'User')

async function load() {
  loading.value = true
  const { data } = await useFetch(`/api/users/${props.userId}`)
  await axios.post('/api/audit', { userId: props.userId })
  loading.value = false
}

function openProfile() {
  router.push(`/users/${props.userId}`)
}
</script>

<template>
  <section class="user-card">
    <h2 v-if="title">{{ displayTitle }}</h2>
    <div v-show="expanded">
      <input v-model="form.name" />
      <input v-model="form.email" />
    </div>
    <ChildAvatar v-else :user-id="userId" />
    <router-link to="/users">All users</router-link>
    <button @click="emit('select', userId)">Select</button>
  </section>
</template>
