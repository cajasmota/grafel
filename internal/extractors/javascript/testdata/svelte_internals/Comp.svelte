<script lang="ts">
	import { setContext, getContext } from 'svelte';
	import { writable } from 'svelte/store';

	// Svelte 4-style prop.
	export let title = 'Counter';

	// Svelte 5 runes: $state / $derived / $effect.
	let count = $state(0);
	let doubled = $derived(count * 2);

	// $props() rune (destructured).
	const { label, step = 1 } = $props();

	// Svelte 4 writable store.
	const total = writable(0);

	// Context API (provide + consume).
	setContext('theme', { dark: true });
	const auth = getContext('auth');

	// Reactive `$:` labelled statements (issue #2877 — reactive_statements).
	$: quadrupled = doubled * 2;
	$: {
		console.log('count changed', count);
	}

	$effect(() => {
		console.log('effect', count);
	});

	function bump() {
		count += step;
		total.update((t) => t + step);
	}
</script>

<div use:tooltip use:clickOutside={bump}>
	<h2>{title}</h2>
	<p>{label}: {count} (x2 {doubled}, x4 {quadrupled})</p>
	<button on:click={bump}>Add {step}</button>
</div>

<style>
	div {
		padding: 1rem;
	}
</style>
