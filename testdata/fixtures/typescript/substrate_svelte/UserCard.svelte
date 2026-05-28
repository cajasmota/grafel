<script lang="ts">
// Svelte SFC fixture — proves import-resolution quality, taint-source,
// taint-sink, sanitizer, and vulnerability-finding for the jsts/markup-script
// substrate sniffers (issue #2850).  Hand-written; no node_modules.
import { page } from '$app/stores';
import DOMPurify from 'dompurify';
import { UserService } from './user.service';

const API_URL = 'https://api.example.com';
const SECRET = process.env.APP_SECRET ?? 'dev-only';

// Source: $page.params is a recognised taint source (route param).
$: userId = $page.params.id;

// Source: $page.url.searchParams is tainted (query string).
$: query = $page.url.searchParams.get('q');

// Sink: eval of a tainted value is a command-injection sink.
function runUnsafe(input: string) {
  eval(input);
}

// Sanitizer: DOMPurify.sanitize is the recognised HTML sanitizer.
$: safeHtml = DOMPurify.sanitize(userId ?? '');
</script>

<template>
  <!-- Sink: {@html expr} bypasses Svelte auto-escaping — XSS sink. -->
  {@html safeHtml}

  <!-- Source + sink chain that proves vulnerability_finding: route param
       passed directly to innerHTML equivalent. -->
  <p>{@html userId}</p>
</template>
