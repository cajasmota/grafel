<!-- Source: synthetic, modelled on real Svelte 5 component data-flow patterns
     (export let / $props props, writable/derived stores, fetch in load,
     {#if}/{#each}/{:else if} branches) | License: MIT

     Used by issue #2855 real-data verification (Data Flow group). -->
<script lang="ts">
  import { writable, derived } from 'svelte/store'
  import ChildRow from './ChildRow.svelte'

  export let title: string
  export let pageSize = 20

  let { selectedId = null } = $props()

  const users = writable<User[]>([])
  const count = derived(users, ($u) => $u.length)
  const loading = writable(false)

  async function load() {
    loading.set(true)
    const res = await fetch(`/api/users?size=${pageSize}`)
    users.set(await res.json())
    loading.set(false)
  }
</script>

<section>
  <h1>{title}</h1>

  {#if $loading}
    <p>Loading…</p>
  {:else if $count === 0}
    <p>No users</p>
  {:else}
    {#each $users as user (user.id)}
      <ChildRow {user} selected={user.id === selectedId} />
    {/each}
  {/if}

  <button on:click={load}>Reload {$count}</button>
</section>
