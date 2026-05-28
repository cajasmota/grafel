<script setup lang="ts">
// Nuxt page — useAsyncData/useFetch loaders + reactive state + ClientOnly.
const { data: users } = await useAsyncData('users', () => $fetch('/api/users'))
const { data: profile } = useFetch('/api/profile')
const count = ref(0)

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
