<!-- Source: https://github.com/sveltejs/svelte (synthetic based on real Svelte 4 patterns) | License: MIT -->

<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { writable, derived } from 'svelte/store'
  import { fade, slide } from 'svelte/transition'
  import { goto } from '$app/navigation'
  import { page } from '$app/stores'
  import type { Post } from '$lib/types'

  // Props
  export let initialPosts: Post[] = []
  export let totalCount: number = 0

  // Stores
  const posts = writable<Post[]>(initialPosts)
  const searchQuery = writable('')
  const currentPage = writable(1)
  const isLoading = writable(false)
  const error = writable<string | null>(null)

  const totalPages = derived(
    [currentPage],
    () => Math.ceil(totalCount / 10)
  )

  // Reactive state
  let confirmDelete: Post | null = null
  let searchTimeout: ReturnType<typeof setTimeout>

  // Fetch posts from API
  async function fetchPosts(query: string, page: number) {
    isLoading.set(true)
    error.set(null)
    try {
      const params = new URLSearchParams({
        ...(query && { search: query }),
        page: String(page),
        perPage: '10',
      })
      const response = await fetch(`/api/posts?${params}`)
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const data = await response.json()
      posts.set(data.items)
      totalCount = data.totalCount
    } catch (err) {
      error.set(err instanceof Error ? err.message : 'Failed to load posts')
    } finally {
      isLoading.set(false)
    }
  }

  function handleSearch(event: Event) {
    const value = (event.target as HTMLInputElement).value
    searchQuery.set(value)
    clearTimeout(searchTimeout)
    searchTimeout = setTimeout(() => {
      currentPage.set(1)
      fetchPosts(value, 1)
    }, 300)
  }

  async function handleDelete(post: Post) {
    confirmDelete = null
    try {
      const res = await fetch(`/api/posts/${post.id}`, { method: 'DELETE' })
      if (!res.ok) throw new Error('Delete failed')
      posts.update(p => p.filter(x => x.id !== post.id))
    } catch (err) {
      error.set('Failed to delete post')
    }
  }

  function formatDate(date: string) {
    return new Intl.RelativeTimeFormat('en').format(
      Math.round((new Date(date).getTime() - Date.now()) / (1000 * 60 * 60 * 24)),
      'day'
    )
  }

  onMount(() => fetchPosts($searchQuery, $currentPage))
  onDestroy(() => clearTimeout(searchTimeout))
</script>

<div class="post-list">
  <div class="filters">
    <input
      type="search"
      placeholder="Search posts..."
      on:input={handleSearch}
      bind:value={$searchQuery}
      class="search-input"
    />
  </div>

  {#if $isLoading}
    <div class="spinner" aria-live="polite" transition:fade>Loading...</div>
  {:else if $error}
    <div class="error" role="alert" transition:slide>
      {$error}
      <button on:click={() => fetchPosts($searchQuery, $currentPage)}>Retry</button>
    </div>
  {:else}
    <ul class="posts">
      {#each $posts as post (post.id)}
        <li class="post-card" transition:slide>
          {#if post.coverImage}
            <img src={post.coverImage} alt={post.title} loading="lazy" />
          {/if}
          <div class="content">
            <h2><a href="/posts/{post.slug}">{post.title}</a></h2>
            <p>{post.excerpt ?? ''}</p>
            <div class="meta">
              <span>{post.author.name}</span>
              <time datetime={post.publishedAt}>{formatDate(post.publishedAt)}</time>
              {#each post.tags as tag}
                <span class="tag">{tag.name}</span>
              {/each}
            </div>
          </div>
          <div class="actions">
            <a href="/posts/{post.id}/edit" class="btn">Edit</a>
            <button on:click={() => confirmDelete = post} class="btn-danger">Delete</button>
          </div>
        </li>
      {:else}
        <li class="empty">No posts found.</li>
      {/each}
    </ul>

    {#if $totalPages > 1}
      <div class="pagination">
        <button disabled={$currentPage === 1} on:click={() => { currentPage.update(p => p - 1); fetchPosts($searchQuery, $currentPage) }}>
          Previous
        </button>
        <span>Page {$currentPage} of {$totalPages}</span>
        <button disabled={$currentPage === $totalPages} on:click={() => { currentPage.update(p => p + 1); fetchPosts($searchQuery, $currentPage) }}>
          Next
        </button>
      </div>
    {/if}
  {/if}

  {#if confirmDelete}
    <div class="modal" transition:fade>
      <p>Delete "{confirmDelete.title}"?</p>
      <button on:click={() => handleDelete(confirmDelete!)}>Confirm</button>
      <button on:click={() => confirmDelete = null}>Cancel</button>
    </div>
  {/if}
</div>

<style>
  .post-list { max-width: 800px; margin: 0 auto; }
  .filters { margin-bottom: 1rem; }
  .search-input { width: 100%; padding: 0.5rem; }
  .posts { list-style: none; padding: 0; }
  .post-card { display: flex; gap: 1rem; padding: 1rem; border-bottom: 1px solid #eee; }
  .meta { font-size: 0.875rem; color: #666; }
  .tag { background: #f0f0f0; padding: 0.1rem 0.4rem; border-radius: 4px; margin-left: 0.25rem; }
  .modal { position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%);
           background: white; padding: 2rem; border-radius: 8px; box-shadow: 0 4px 16px rgba(0,0,0,.2); }
</style>
