<script lang="ts">
  // SvelteKit page — hydrated on the client, prerendered at build.
  export const prerender = true
  export let data
  let likes = 0
</script>

<article>
  <h1>{data.post.title}</h1>
  <button on:click={() => (likes += 1)}>Like {likes}</button>
</article>
