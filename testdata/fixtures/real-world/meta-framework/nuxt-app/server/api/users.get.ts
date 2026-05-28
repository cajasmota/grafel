// Nuxt server route — server-only handler (server boundary).
export default defineEventHandler(async (event) => {
  const users = await db.query('SELECT * FROM users')
  return users
})
