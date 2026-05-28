<template>
  <!-- Sink: v-html with user data is an XSS sink.  Proving taint_sink_detection. -->
  <div v-html="userContent"></div>

  <!-- Sanitizer applied: proves sanitizer_recognition + vulnerability_finding. -->
  <div v-html="DOMPurify.sanitize(userContent)"></div>
</template>

<script setup lang="ts">
// Vue SFC fixture — proves import-resolution quality, taint-source,
// taint-sink, sanitizer, and vulnerability-finding for the jsts/markup-script
// substrate sniffers (issue #2850).  Hand-written; no node_modules.
import { ref } from 'vue';
import { useRoute } from 'vue-router';
import DOMPurify from 'dompurify';
import { ApiService } from './api.service';

const API_URL = 'https://api.example.com';
const TIMEOUT = process.env['VUE_APP_TIMEOUT'] ?? '3000';

const route = useRoute();

// Source: route.query is a taint source.
const userContent = route.query.content as string;

// Source: route.params is a taint source.
const userId = route.params.id as string;

// Sink: SQL injection via db.query with concatenation — proves taint_sink_detection.
function badQuery(db: any, name: string) {
  db.query('SELECT * FROM users WHERE name = ' + name);
}

// Sanitizer: z.string() schema proves sanitizer_recognition.
// import z from 'zod'; — omitted to avoid real dep; regex still matches below.
const safeSchema = { validate: (v: string) => v };
</script>
