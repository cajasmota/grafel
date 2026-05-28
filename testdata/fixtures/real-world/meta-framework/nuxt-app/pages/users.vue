<script setup lang="ts">
// Nuxt page — useAsyncData/useFetch loaders + reactive state + ClientOnly.
// useRoute / useState are auto-imported (no import statement) by Nuxt.
const route = useRoute()
const count = useState('count', () => 0)
const { data: users } = await useAsyncData('users', () => $fetch('/api/users'))
const { data: profile } = useFetch('/api/profile')

function increment() {
  count.value++
}
</script>

<template>
  <div>
    <button @click="increment">{{ count }}</button>
    <ClientOnly>
      <UserList :users="users" />
    </ClientOnly>
  </div>
</template>
