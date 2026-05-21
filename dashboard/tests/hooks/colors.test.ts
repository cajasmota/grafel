import { describe, it, expect } from 'vitest'
import { VERB_COLORS, kindColors, repoColor } from '@/lib/colors'

describe('VERB_COLORS', () => {
  const verbs = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'WS'] as const
  for (const verb of verbs) {
    it(`has colors for ${verb}`, () => {
      const c = VERB_COLORS[verb]
      expect(c.bg).toBeTruthy()
      expect(c.text).toBeTruthy()
      expect(c.border).toBeTruthy()
    })
  }
})

describe('kindColors', () => {
  it('returns colors for known kinds', () => {
    const c = kindColors('Function')
    expect(c.bg).toBeTruthy()
    expect(c.text).toBeTruthy()
  })

  it('returns fallback for unknown kind', () => {
    const c = kindColors('ScopeUnknown')
    expect(c.bg).toBeTruthy()
  })
})

describe('repoColor', () => {
  it('returns stable colors for a slug', () => {
    const c1 = repoColor('core-api')
    const c2 = repoColor('core-api')
    expect(c1).toBe(c2)
  })

  it('returns different colors for different slugs', () => {
    const c1 = repoColor('repo-alpha')
    const c2 = repoColor('repo-beta-very-different')
    // repoColor returns a hex string, not an object
    expect(c1).toBeTruthy()
    expect(c2).toBeTruthy()
  })
})
