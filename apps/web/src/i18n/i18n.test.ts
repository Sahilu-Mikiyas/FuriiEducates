import { describe, expect, it } from 'vitest'
import { am } from './am'
import { en } from './en'

const placeholders = (text: string) => (text.match(/\{\w+\}/g) ?? []).filter((name) => name !== '{"value"').sort()

describe('translations', () => {
  const keys = Object.keys(en) as (keyof typeof en)[]

  it('has every English key in Amharic and no extras', () => {
    expect(Object.keys(am).sort()).toEqual([...keys].sort())
  })

  it('never leaves an Amharic text empty or untranslated', () => {
    // These stay the same in every language.
    const same = new Set(['login.emailHint', 'staff.emailHint'])
    for (const key of keys) {
      expect(am[key].trim(), key).not.toBe('')
      if (!same.has(key)) expect(am[key], key).not.toBe(en[key])
    }
  })

  it('keeps the same placeholders in both languages', () => {
    for (const key of keys) expect(placeholders(am[key]), key).toEqual(placeholders(en[key]))
  })
})
