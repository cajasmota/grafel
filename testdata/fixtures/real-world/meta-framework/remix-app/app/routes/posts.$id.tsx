// Remix route module — loader (server) + action (server) + hydrated component.
import { json } from '@remix-run/node'
import { useLoaderData } from '@remix-run/react'
import { useState } from 'react'

export async function loader({ params }) {
  const post = await db.query('SELECT * FROM posts WHERE id = ' + params.id)
  return json({ post })
}

export async function action({ request }) {
  const form = await request.formData()
  return json({ ok: true })
}

export default function PostRoute() {
  const { post } = useLoaderData<typeof loader>()
  const [editing, setEditing] = useState(false)
  return <article onClick={() => setEditing(!editing)}>{post.title}</article>
}
