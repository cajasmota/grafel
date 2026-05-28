// Nuxt config — prerender route rules drive static generation.
export default defineNuxtConfig({
  routeRules: {
    '/blog/**': { prerender: true },
  },
})
