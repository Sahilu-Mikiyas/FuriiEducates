import { useMemo } from 'react'
import { useI18n } from '../i18n'
import { CalendarDate, formatClock, formatEthiopian, formatGregorian, gregorianToEthiopian, parseISODate } from './ethiopian'

const localDate = (value: Date): CalendarDate => ({ year: value.getFullYear(), month: value.getMonth() + 1, day: value.getDate() })

// A plain date comes from the API as YYYY-MM-DD or as that date at midnight UTC.
// Reading it through the device time zone could show the wrong day.
const isPlainDate = (value: string) => /^\d{4}-\d{2}-\d{2}(T00:00:00(\.0+)?Z)?$/.test(value)

// useDates formats dates in the user's chosen calendar. By default the Ethiopian
// date comes first with the Gregorian date beside it.
export function useDates() {
  const { language, calendar } = useI18n()
  return useMemo(() => {
    const dayText = (day: CalendarDate) => {
      if (calendar === 'gregorian') return formatGregorian(day, language)
      const ethiopian = formatEthiopian(gregorianToEthiopian(day), language)
      return calendar === 'ethiopian' ? ethiopian : `${ethiopian} · ${formatGregorian(day, language)}`
    }
    const moment = (value: string) => {
      const parsed = new Date(value)
      return Number.isNaN(parsed.getTime()) ? null : parsed
    }
    return {
      dayText,
      // date formats a date or a timestamp as a day.
      date: (value: string | null | undefined) => {
        if (!value) return '—'
        if (isPlainDate(value)) {
          const day = parseISODate(value)
          return day ? dayText(day) : value
        }
        const parsed = moment(value)
        return parsed ? dayText(localDate(parsed)) : value
      },
      dateTime: (value: string | null | undefined) => {
        if (!value) return '—'
        const parsed = moment(value)
        return parsed ? `${dayText(localDate(parsed))}, ${formatClock(parsed, language)}` : value
      },
      time: (value: string | null | undefined) => {
        if (!value) return '—'
        const parsed = moment(value)
        return parsed ? formatClock(parsed, language) : value
      },
      today: () => dayText(localDate(new Date())),
    }
  }, [language, calendar])
}
