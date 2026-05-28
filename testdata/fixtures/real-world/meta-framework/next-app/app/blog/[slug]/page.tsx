// Next.js App-Router server component page (Server Component by RSC default).
import { notFound } from 'next/navigation'
import { CommentBox } from './CommentBox'

export const revalidate = 3600

export async function generateStaticParams() {
  const posts = await fetch('https://cms.example.com/posts').then((r) => r.json())
  return posts.map((p: { slug: string }) => ({ slug: p.slug }))
}

export async function generateMetadata({ params }: { params: { slug: string } }) {
  return { title: params.slug }
}

export default async function BlogPostPage({ params }: { params: { slug: string } }) {
  const post = await fetch(`https://cms.example.com/posts/${params.slug}`).then((r) => r.json())
  if (!post) notFound()
  return (
    <article>
      <h1>{post.title}</h1>
      <CommentBox postId={post.id} />
    </article>
  )
}
