import { useEffect, useRef, useState } from 'react'
import { useI18n } from '../i18n'
import { CalendarDate, daysInEthiopianMonth, ethiopianMonthNames, ethiopianToGregorian, formatEthiopian, formatGregorian, gregorianToEthiopian, parseISODate, toISODate } from './ethiopian'

type Parts = { year: string; month: string; day: string }

const emptyParts: Parts = { year: '', month: '', day: '' }

function partsFrom(iso: string): Parts {
  const parsed = parseISODate(iso)
  if (!parsed) return emptyParts
  const ethiopian = gregorianToEthiopian(parsed)
  return { year: String(ethiopian.year), month: String(ethiopian.month), day: String(ethiopian.day) }
}

function isoFrom(parts: Parts) {
  const date: CalendarDate = { year: Number(parts.year), month: Number(parts.month), day: Number(parts.day) }
  if (!parts.year || !parts.month || !parts.day || date.year < 1000 || date.year > 3000) return ''
  if (date.day > daysInEthiopianMonth(date.year, date.month)) return ''
  return toISODate(ethiopianToGregorian(date))
}

// DateField asks for a date in either calendar and always submits a Gregorian
// YYYY-MM-DD value under `name`, or YYYY-MM-DDTHH:mm with `withTime`. It follows
// the form's reset, so forms can keep using form.reset().
export function DateField({ name, label, defaultValue = '', required = false, withTime = false }: {
  name: string
  label: string
  defaultValue?: string
  required?: boolean
  withTime?: boolean
}) {
  const { t, language, calendar } = useI18n()
  const initialDate = defaultValue.slice(0, 10)
  const initialTime = defaultValue.slice(11, 16)
  const [useEthiopian, setUseEthiopian] = useState(calendar !== 'gregorian')
  const [parts, setParts] = useState<Parts>(() => partsFrom(initialDate))
  const [gregorian, setGregorian] = useState(initialDate)
  const [time, setTime] = useState(initialTime)
  const hidden = useRef<HTMLInputElement>(null)

  useEffect(() => {
    const form = hidden.current?.form
    if (!form) return
    const onReset = () => { setParts(partsFrom(initialDate)); setGregorian(initialDate); setTime(initialTime) }
    form.addEventListener('reset', onReset)
    return () => form.removeEventListener('reset', onReset)
  }, [initialDate, initialTime])

  const iso = useEthiopian ? isoFrom(parts) : gregorian
  const parsed = iso ? parseISODate(iso) : null
  const value = iso && withTime ? (time ? `${iso}T${time}` : '') : iso
  const days = parts.year && parts.month ? daysInEthiopianMonth(Number(parts.year), Number(parts.month)) : 30
  const change = (next: Partial<Parts>) => setParts((current) => {
    const merged = { ...current, ...next }
    const limit = merged.year && merged.month ? daysInEthiopianMonth(Number(merged.year), Number(merged.month)) : 30
    return Number(merged.day) > limit ? { ...merged, day: '' } : merged
  })
  const switchCalendar = () => {
    if (useEthiopian) setGregorian(iso)
    else setParts(partsFrom(gregorian))
    setUseEthiopian(!useEthiopian)
  }
  // Keep a date entered with time-less controls required only when asked for.
  const timeRequired = required || (withTime && Boolean(iso))

  return <fieldset className="date-field">
    <legend>{label}</legend>
    <input ref={hidden} type="hidden" name={name} value={value} />
    {useEthiopian
      ? <div className="date-field-parts">
          <select aria-label={t('date.day')} value={parts.day} required={required} onChange={(e) => change({ day: e.target.value })}>
            <option value="">{t('date.day')}</option>
            {Array.from({ length: days }, (_, index) => <option key={index} value={index + 1}>{index + 1}</option>)}
          </select>
          <select aria-label={t('date.month')} value={parts.month} required={required} onChange={(e) => change({ month: e.target.value })}>
            <option value="">{t('date.month')}</option>
            {ethiopianMonthNames(language).map((month, index) => <option key={month} value={index + 1}>{month}</option>)}
          </select>
          <input aria-label={t('date.year')} placeholder={t('date.year')} type="number" min="1900" max="2200" value={parts.year} required={required} onChange={(e) => change({ year: e.target.value })} />
        </div>
      : <input className="date-field-native" aria-label={label} type="date" value={gregorian} required={required} onChange={(e) => setGregorian(e.target.value)} />}
    {withTime && <input className="date-field-time" aria-label={t('date.time')} type="time" value={time} required={timeRequired} onChange={(e) => setTime(e.target.value)} />}
    <div className="date-field-foot">
      <small>{parsed ? (useEthiopian ? formatGregorian(parsed, language) : formatEthiopian(gregorianToEthiopian(parsed), language)) : ''}</small>
      <button type="button" onClick={switchCalendar}>{useEthiopian ? t('date.useGregorian') : t('date.useEthiopian')}</button>
    </div>
  </fieldset>
}
