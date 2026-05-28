'use server'
// Next.js server actions — 'use server' module of exported async mutations.
export async function addComment(formData: FormData) {
  const text = formData.get('text')
  await db.query('INSERT INTO comments (text) VALUES (?)', [text])
}

export async function deleteComment(id: string) {
  await db.query('DELETE FROM comments WHERE id = ?', [id])
}
