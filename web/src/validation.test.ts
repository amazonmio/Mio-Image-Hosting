import { describe, expect, it } from 'vitest'
import { validPassword } from './validation'

describe('password validation', () => {
  it('rejects blank, short, oversized and malformed passwords', () => {
    for (const password of [' '.repeat(10), 'short', '界'.repeat(86), 'a'.repeat(10) + '\ud800']) {
      expect(validPassword(password)).toBe(false)
    }
  })
  it('counts Unicode characters while bounding UTF-8 bytes', () => {
    expect(validPassword('😀'.repeat(10))).toBe(true)
    expect(validPassword('a'.repeat(256))).toBe(true)
    expect(validPassword('a'.repeat(257))).toBe(false)
  })
})
