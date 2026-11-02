import { createContext, FormEvent, ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { useI18n } from '../i18n'

type Feedback = { error: string; notice: string; setError: (text: string) => void; setNotice: (text: string) => void; clear: () => void }

const FeedbackContext = createContext<Feedback | null>(null)

let reportLoadError: (error: unknown) => void = () => undefined

// loadFailed receives errors from background data loading (see main.tsx).
export const loadFailed = (error: unknown) => reportLoadError(error)

export function FeedbackProvider({ children }: { children: ReactNode }) {
  const { errorText } = useI18n()
  const [error, setErrorState] = useState('')
  const [notice, setNoticeState] = useState('')
  const setError = useCallback((text: string) => { setNoticeState(''); setErrorState(text) }, [])
  const setNotice = useCallback((text: string) => { setErrorState(''); setNoticeState(text) }, [])
  const clear = useCallback(() => { setErrorState(''); setNoticeState('') }, [])
  useEffect(() => {
    reportLoadError = (e) => setError(errorText(e, 'error.load'))
    return () => { reportLoadError = () => undefined }
  }, [errorText, setError])
  const value = useMemo(() => ({ error, notice, setError, setNotice, clear }), [error, notice, setError, setNotice, clear])
  return <FeedbackContext.Provider value={value}>{children}</FeedbackContext.Provider>
}

export function useFeedback() {
  const context = useContext(FeedbackContext)
  if (!context) throw new Error('useFeedback needs FeedbackProvider')
  return context
}

// Toast shows the latest success or failure at the top of a page.
export function Toast() {
  const { t } = useI18n()
  const { error, notice, clear } = useFeedback()
  if (!error && !notice) return null
  return <div className={error ? 'toast error-toast' : 'toast'} role="status">
    {error || notice}
    <button onClick={clear} aria-label={t('common.dismiss')}>×</button>
  </div>
}

// useAction runs a change, tracks that it is busy, and reports the outcome.
export function useAction() {
  const { errorText } = useI18n()
  const { setError, setNotice, clear } = useFeedback()
  const [busy, setBusy] = useState(false)

  async function run(work: () => Promise<unknown>, message?: string) {
    clear(); setBusy(true)
    try {
      await work()
      if (message) setNotice(message)
      return true
    } catch (e) {
      setError(errorText(e, 'error.generic'))
      return false
    } finally { setBusy(false) }
  }

  // onSubmit makes a form handler. `work` receives the form's values and the
  // form resets when the work succeeds.
  const onSubmit = (work: (values: FormData, form: HTMLFormElement) => Promise<unknown>, message?: string, resetAfter = true) => (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = event.currentTarget
    void run(async () => {
      await work(new FormData(form), form)
      if (resetAfter) form.reset()
    }, message)
  }

  return { busy, run, onSubmit }
}
