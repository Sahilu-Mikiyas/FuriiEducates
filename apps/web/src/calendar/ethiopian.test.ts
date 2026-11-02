import { describe, expect, it } from 'vitest'
import { daysInEthiopianMonth, ethiopianToGregorian, formatEthiopian, formatGregorian, gregorianToEthiopian, isEthiopianLeapYear, parseISODate, toISODate } from './ethiopian'

const known: [string, number, number, number][] = [
  ['2023-09-11', 2015, 13, 6], // Last day of a leap year.
  ['2023-09-12', 2016, 1, 1], // New Year after a leap year.
  ['2024-09-10', 2016, 13, 5],
  ['2024-09-11', 2017, 1, 1],
  ['2025-01-07', 2017, 4, 29], // Genna, Ethiopian Christmas.
  ['2025-01-19', 2017, 5, 11], // Timkat.
  ['2025-03-02', 2017, 6, 23], // Victory of Adwa.
  ['2026-09-11', 2019, 1, 1],
  ['2026-11-02', 2019, 2, 23],
  ['2027-09-11', 2019, 13, 6],
  ['2027-09-12', 2020, 1, 1],
  ['2000-01-01', 1992, 4, 22],
]

describe('Ethiopian calendar conversion', () => {
  it.each(known)('%s is %i/%i/%i', (iso, year, month, day) => {
    const gregorian = parseISODate(iso)!
    expect(gregorianToEthiopian(gregorian)).toEqual({ year, month, day })
    expect(toISODate(ethiopianToGregorian({ year, month, day }))).toBe(iso)
  })

  it('round trips every day for four centuries and always gives a valid date', () => {
    let previous = gregorianToEthiopian({ year: 1900, month: 1, day: 1 })
    const start = Date.UTC(1900, 0, 1)
    const days = 146097
    const wrong: string[] = []
    for (let offset = 1; offset <= days; offset++) {
      const moment = new Date(start + offset * 86400000)
      const gregorian = { year: moment.getUTCFullYear(), month: moment.getUTCMonth() + 1, day: moment.getUTCDate() }
      const ethiopian = gregorianToEthiopian(gregorian)
      const back = ethiopianToGregorian(ethiopian)
      // Consecutive Gregorian days must be consecutive Ethiopian days.
      const endOfMonth = previous.day === daysInEthiopianMonth(previous.year, previous.month)
      const endOfYear = endOfMonth && previous.month === 13
      const expected = {
        year: endOfYear ? previous.year + 1 : previous.year,
        month: endOfYear ? 1 : endOfMonth ? previous.month + 1 : previous.month,
        day: endOfMonth ? 1 : previous.day + 1,
      }
      const consecutive = ethiopian.year === expected.year && ethiopian.month === expected.month && ethiopian.day === expected.day
      const roundTrips = back.year === gregorian.year && back.month === gregorian.month && back.day === gregorian.day
      if (!consecutive || !roundTrips) wrong.push(toISODate(gregorian))
      previous = ethiopian
    }
    expect(wrong).toEqual([])
  })

  it('gives Pagume six days only in a leap year', () => {
    expect(isEthiopianLeapYear(2015)).toBe(true)
    expect(isEthiopianLeapYear(2019)).toBe(true)
    expect(isEthiopianLeapYear(2016)).toBe(false)
    expect(daysInEthiopianMonth(2019, 13)).toBe(6)
    expect(daysInEthiopianMonth(2018, 13)).toBe(5)
    expect(daysInEthiopianMonth(2018, 1)).toBe(30)
  })
})

describe('parseISODate', () => {
  it('reads dates and timestamps without shifting the day', () => {
    expect(parseISODate('2026-09-11')).toEqual({ year: 2026, month: 9, day: 11 })
    expect(parseISODate('2026-09-11T00:00:00Z')).toEqual({ year: 2026, month: 9, day: 11 })
  })

  it('rejects text and impossible dates', () => {
    expect(parseISODate('')).toBeNull()
    expect(parseISODate('soon')).toBeNull()
    expect(parseISODate('2026-02-30')).toBeNull()
    expect(parseISODate('2026-13-01')).toBeNull()
  })
})

describe('date formatting', () => {
  const date = { year: 2019, month: 2, day: 23 }

  it('writes Ethiopian dates in each language', () => {
    expect(formatEthiopian(date, 'en')).toBe('23 Tikimt 2019 E.C.')
    expect(formatEthiopian(date, 'am')).toBe('ጥቅምት 23፣ 2019 ዓ.ም.')
  })

  it('writes Gregorian dates without a time zone shift', () => {
    expect(formatGregorian({ year: 2026, month: 11, day: 2 }, 'en')).toBe('2 Nov 2026')
  })
})
