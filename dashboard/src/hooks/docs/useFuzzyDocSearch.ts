import { useState, useEffect, useMemo } from 'react'
import Fuse from 'fuse.js'
import { useDocTree } from './useDocTree'

export interface SearchResult {
  path: string
  title: string
  excerpt: string
}

/**
 * Client-side fuzzy search over docs tree using Fuse.js.
 * Indexes doc labels + path.
 */
export function useFuzzyDocSearch(group: string, query: string) {
  const { data: treeData } = useDocTree(group)
  const [results, setResults] = useState<SearchResult[]>([])

  // Flatten tree into searchable index
  const index = useMemo(() => {
    if (!treeData?.tree) return null

    const docs: Array<{ path: string; title: string; content: string }> = []

    const walk = (node: any, parentPath: string = '') => {
      const path = parentPath ? `${parentPath}/${node.path}` : node.path

      if (node.type === 'file' || node.label) {
        docs.push({
          path,
          title: node.label || node.title || '',
          content: node.label || '',
        })
      }

      if (node.children) {
        node.children.forEach((child: any) => walk(child, path))
      }
    }

    treeData.tree.forEach((root: any) => walk(root))

    if (docs.length === 0) return null

    return new Fuse(docs, {
      keys: ['title', 'content'],
      threshold: 0.4,
      minMatchCharLength: 1,
      includeScore: true,
      ignoreLocation: true,
    })
  }, [treeData])

  useEffect(() => {
    if (!index || !query.trim()) {
      setResults([])
      return
    }

    const hits = index.search(query).slice(0, 10)
    setResults(
      hits.map((hit) => ({
        path: hit.item.path,
        title: hit.item.title,
        excerpt: hit.item.content || hit.item.path,
      }))
    )
  }, [index, query])

  return { results, isLoading: !index }
}
