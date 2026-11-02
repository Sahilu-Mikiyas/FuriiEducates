import { createContext, ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { am, amServerErrors } from './am'
import { en } from './en'
import { APIError } from '../lib/api'

export type MessageKey = keyof typeof en
export type Language = 'en' | 'am'
export type CalendarMode = 'both' | 'ethiopian' | 'gregorian'
type Vars = Record<string, string | number>

export const languages: { code: Language; label: string }[] = [
  { code: 'en', label: 'English' },
  { code: 'am', label: 'አማርኛ' },
]

const dictionaries: Record<Language, Record<MessageKey, string>> = { en, am }
const LANGUAGE_KEY = 'furii.language'
const CALENDAR_KEY = 'furii.calendar'

const isLanguage = (value: string | null | undefined): value is Language => languages.some((language) => language.code === value)

function stored(key: string) {
  try { return window.localStorage.getItem(key) } catch { return null }
}
function store(key: string, value: string) {
  try { window.localStorage.setItem(key, value) } catch { /* A blocked store only loses the preference. */ }
}

function fill(text: string, vars?: Vars) {
  if (!vars) return text
  return text.replace(/\{(\w+)\}/g, (whole, name: string) => (name in vars ? String(vars[name]) : whole))
}

type Preferences = {
  language: Language
  setLanguage: (language: Language) => void
  setSchoolLanguage: (language: string) => void
  calendar: CalendarMode
  setCalendar: (calendar: CalendarMode) => void
  t: (key: MessageKey, vars?: Vars) => string
  // label translates a value that comes from the server, such as a status, and
  // shows the value itself when no translation exists.
  label: (prefix: string, value: string) => string
  errorText: (error: unknown, fallback: MessageKey) => string
}

const PreferencesContext = createContext<Preferences | null>(null)

// A choice made on this device wins, then the school's default language, then
// the browser's language.
export function PreferencesProvider({ children }: { children: ReactNode }) {
  const [chosen, setChosen] = useState<Language | null>(() => {
    const value = stored(LANGUAGE_KEY)
    return isLanguage(value) ? value : null
  })
  const [school, setSchool] = useState<Language | null>(null)
  const [calendar, setCalendarState] = useState<CalendarMode>(() => {
    const value = stored(CALENDAR_KEY)
    return value === 'ethiopian' || value === 'gregorian' ? value : 'both'
  })
  const browser: Language = navigator.language?.toLowerCase().startsWith('am') ? 'am' : 'en'
  const language = chosen ?? school ?? browser

  useEffect(() => { document.documentElement.lang = language }, [language])

  const setLanguage = useCallback((next: Language) => { store(LANGUAGE_KEY, next); setChosen(next) }, [])
  const setSchoolLanguage = useCallback((next: string) => { setSchool(isLanguage(next) ? next : null) }, [])
  const setCalendar = useCallback((next: CalendarMode) => { store(CALENDAR_KEY, next); setCalendarState(next) }, [])

  const value = useMemo<Preferences>(() => {
    const t = (key: MessageKey, vars?: Vars) => fill(dictionaries[language][key], vars)
    return {
      language, setLanguage, setSchoolLanguage, calendar, setCalendar, t,
      label: (prefix, raw) => {
        const key = `${prefix}.${raw}` as MessageKey
        return dictionaries[language][key] ?? raw.replace(/_/g, ' ')
      },
      errorText: (error, fallback) => {
        if (error instanceof APIError) return (language === 'am' && amServerErrors[error.code]) || error.message
        if (error instanceof TypeError) return t('error.network')
        return error instanceof Error ? error.message : t(fallback)
      },
    }
  }, [language, calendar, setLanguage, setSchoolLanguage, setCalendar])

  return <PreferencesContext.Provider value={value}>{children}</PreferencesContext.Provider>
}

export function useI18n() {
  const context = useContext(PreferencesContext)
  if (!context) throw new Error('useI18n needs PreferencesProvider')
  return context
}
