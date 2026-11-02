// Conversion between the Gregorian and Ethiopian calendars through Julian day
// numbers. An Ethiopian year has 12 months of 30 days and a 13th month, Pagume,
// of 5 days, or 6 in a leap year.

export type CalendarDate = { year: number; month: number; day: number }

const ETHIOPIAN_EPOCH = 1724221 // Julian day number of 1 Meskerem, year 1.

function gregorianToJulianDay({ year, month, day }: CalendarDate) {
  const a = Math.floor((14 - month) / 12)
  const y = year + 4800 - a
  const m = month + 12 * a - 3
  return day + Math.floor((153 * m + 2) / 5) + 365 * y + Math.floor(y / 4) - Math.floor(y / 100) + Math.floor(y / 400) - 32045
}

function julianDayToGregorian(julianDay: number): CalendarDate {
  const a = julianDay + 32044
  const b = Math.floor((4 * a + 3) / 146097)
  const c = a - Math.floor((146097 * b) / 4)
  const d = Math.floor((4 * c + 3) / 1461)
  const e = c - Math.floor((1461 * d) / 4)
  const m = Math.floor((5 * e + 2) / 153)
  return {
    year: 100 * b + d - 4800 + Math.floor(m / 10),
    month: m + 3 - 12 * Math.floor(m / 10),
    day: e - Math.floor((153 * m + 2) / 5) + 1,
  }
}

function ethiopianToJulianDay({ year, month, day }: CalendarDate) {
  return ETHIOPIAN_EPOCH + 365 * (year - 1) + Math.floor(year / 4) + 30 * (month - 1) + day - 1
}

export const isEthiopianLeapYear = (year: number) => ((year % 4) + 4) % 4 === 3

export function daysInEthiopianMonth(year: number, month: number) {
  if (month < 13) return 30
  return isEthiopianLeapYear(year) ? 6 : 5
}

export function gregorianToEthiopian(date: CalendarDate): CalendarDate {
  const julianDay = gregorianToJulianDay(date)
  const year = Math.floor((4 * (julianDay - ETHIOPIAN_EPOCH) + 1463) / 1461)
  const dayOfYear = julianDay - ethiopianToJulianDay({ year, month: 1, day: 1 })
  return { year, month: Math.floor(dayOfYear / 30) + 1, day: (dayOfYear % 30) + 1 }
}

export function ethiopianToGregorian(date: CalendarDate): CalendarDate {
  return julianDayToGregorian(ethiopianToJulianDay(date))
}

// parseISODate reads the date part of an ISO value without any time zone shift.
export function parseISODate(value: string): CalendarDate | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(value)
  if (!match) return null
  const date = { year: Number(match[1]), month: Number(match[2]), day: Number(match[3]) }
  const roundTrip = julianDayToGregorian(gregorianToJulianDay(date))
  return roundTrip.month === date.month && roundTrip.day === date.day ? date : null
}

export function toISODate({ year, month, day }: CalendarDate) {
  return `${String(year).padStart(4, '0')}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
}

const monthNames = {
  en: ['Meskerem', 'Tikimt', 'Hidar', 'Tahsas', 'Tir', 'Yekatit', 'Megabit', 'Miyazya', 'Ginbot', 'Sene', 'Hamle', 'Nehase', 'Pagume'],
  am: ['መስከረም', 'ጥቅምት', 'ኅዳር', 'ታኅሣሥ', 'ጥር', 'የካቲት', 'መጋቢት', 'ሚያዝያ', 'ግንቦት', 'ሰኔ', 'ሐምሌ', 'ነሐሴ', 'ጳጉሜን'],
}

export const ethiopianMonthNames = (language: string) => (language === 'am' ? monthNames.am : monthNames.en)

export function formatEthiopian(date: CalendarDate, language: string) {
  const month = ethiopianMonthNames(language)[date.month - 1]
  return language === 'am' ? `${month} ${date.day}፣ ${date.year} ዓ.ም.` : `${date.day} ${month} ${date.year} E.C.`
}

export function formatGregorian(date: CalendarDate, language: string) {
  return new Intl.DateTimeFormat(language === 'am' ? 'am-ET' : 'en-GB', { day: 'numeric', month: 'short', year: 'numeric', calendar: 'gregory', timeZone: 'UTC' })
    .format(new Date(Date.UTC(date.year, date.month - 1, date.day)))
}

export function formatClock(value: Date, language: string) {
  return new Intl.DateTimeFormat(language === 'am' ? 'am-ET' : 'en-GB', { hour: '2-digit', minute: '2-digit', hour12: false }).format(value)
}
