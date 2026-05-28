'use client'
// Next.js App-Router client component — hydration boundary.
import { useState } from 'react'

export function CommentBox({ postId }: { postId: string }) {
  const [text, setText] = useState('')
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button onClick={() => setOpen(!open)}>Comment</button>
      {open && <textarea value={text} onChange={(e) => setText(e.target.value)} />}
    </div>
  )
}
