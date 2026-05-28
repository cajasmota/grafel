<!-- Source: synthetic, modelled on real Svelte 5 component data-flow patterns
     (export let / $props props, writable/derived stores, fetch in load,
     {#if}/{#each}/{:else if} branches, svelte-routing navigation) | License: MIT

     Used by issue #2855 (Data Flow group) + #2856 (Navigation + Lifecycle)
     real-data verification. -->
<script lang="ts">
  import { writable, derived } from 'svelte/store'
  import { Link, navigate } from 'svelte-routing'
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

  function openCreate() {
    navigate('/users/new')
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

  <Link to="/users/new">Add user</Link>
  <button on:click={load}>Reload {$count}</button>
</section>
