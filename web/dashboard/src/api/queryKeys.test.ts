/**
 * Unit tests for the TanStack Query key factory.
 *
 * Verifies that every key factory returns the correct stable array and that
 * hierarchical key relationships hold (child keys start with the parent prefix
 * so TanStack Query's `invalidateQueries` correctly scopes invalidations).
 */
import { describe, it, expect } from 'vitest'
import { queryKeys } from './queryKeys'

describe('queryKeys', () => {
  // ---- static keys --------------------------------------------------------

  it('queryKeys.runs is a stable array', () => {
    expect(queryKeys.runs).toEqual(['observability', 'runs'])
  })

  it('queryKeys.rubric is a stable array', () => {
    expect(queryKeys.rubric).toEqual(['observability', 'rubric'])
  })

  it('queryKeys.health is a stable array', () => {
    expect(queryKeys.health).toEqual(['observability', 'health'])
  })

  // ---- factory functions --------------------------------------------------

  describe('run(sessionId)', () => {
    it('returns a key scoped to the given session id', () => {
      expect(queryKeys.run('sess-abc')).toEqual(['observability', 'runs', 'sess-abc'])
    })

    it('starts with queryKeys.runs so invalidateQueries(runs) covers it', () => {
      const k = queryKeys.run('x')
      expect(k.slice(0, 2)).toEqual(queryKeys.runs)
    })
  })

  describe('runIterations(sessionId)', () => {
    it('returns the iterations key for a session', () => {
      expect(queryKeys.runIterations('sess-xyz')).toEqual([
        'observability',
        'runs',
        'sess-xyz',
        'iterations',
      ])
    })

    it('starts with run(sessionId) prefix so session invalidation covers it', () => {
      const sessionId = 'sess-xyz'
      const parent = queryKeys.run(sessionId)
      const child = queryKeys.runIterations(sessionId)
      expect(child.slice(0, parent.length)).toEqual([...parent])
    })
  })

  describe('iteration(n, iterLogDir?)', () => {
    it('uses an empty iterLogDir by default', () => {
      expect(queryKeys.iteration(5)).toEqual(['observability', 'iterations', 5, ''])
    })

    it('uses the provided iterLogDir when given', () => {
      expect(queryKeys.iteration(5, '/var/log/iters')).toEqual([
        'observability',
        'iterations',
        5,
        '/var/log/iters',
      ])
    })

    it('distinguishes keys by both n and iterLogDir', () => {
      const a = queryKeys.iteration(1, '')
      const b = queryKeys.iteration(1, '/alt')
      expect(a).not.toEqual(b)
    })
  })
})
