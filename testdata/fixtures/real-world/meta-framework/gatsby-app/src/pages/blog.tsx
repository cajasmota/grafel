// Gatsby page — file-system route, build-time GraphQL page query (SSG), hooks.
import { graphql, useStaticQuery } from 'gatsby'
import { useState } from 'react'

export const query = graphql`
  query BlogQuery {
    allPost { nodes { id title } }
  }
`

export default function BlogPage({ data }) {
  const site = useStaticQuery(graphql`query { site { siteMetadata { title } } }`)
  const [filter, setFilter] = useState('')
  return (
    <main>
      <h1>{site.site.siteMetadata.title}</h1>
      <input value={filter} onChange={(e) => setFilter(e.target.value)} />
      <ul>{data.allPost.nodes.map((p) => <li key={p.id}>{p.title}</li>)}</ul>
    </main>
  )
}
