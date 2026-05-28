// SvelteKit server load — server boundary + data loader, reads route params.
import type { PageServerLoad } from './$types'

export const load: PageServerLoad = async ({ params }) => {
  const post = await db.query('SELECT * FROM posts WHERE id = ' + params.id)
  return { post }
}
