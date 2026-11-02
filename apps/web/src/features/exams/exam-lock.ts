import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../../lib/api'

type IntegritySignal = 'tab_hidden' | 'window_blur' | 'fullscreen_exit' | 'paste_attempt' | 'copy_attempt'

const SIGNAL_COOLDOWN_MS = 2000

export function fullscreenSupported() {
  return typeof document.documentElement.requestFullscreen === 'function'
}

// requestExamFullscreen must run inside a click handler. Browsers refuse it
// otherwise, and phones such as iPhones do not support it at all.
export function requestExamFullscreen() {
  if (!fullscreenSupported() || document.fullscreenElement) return
  void document.documentElement.requestFullscreen().catch(() => undefined)
}

// useExamLock watches a locked exam page and reports leaving the window,
// switching tabs, leaving full screen, and copy or paste attempts. A browser
// cannot truly lock a device, so these are signals for the invigilator to
// review, not proof of cheating. They never end the exam.
export function useExamLock(attemptID: string, active: boolean) {
  const [isFullscreen, setIsFullscreen] = useState(() => Boolean(document.fullscreenElement))
  const lastSent = useRef<Partial<Record<IntegritySignal, number>>>({})

  const report = useCallback((type: IntegritySignal) => {
    const now = Date.now()
    if (now - (lastSent.current[type] ?? 0) < SIGNAL_COOLDOWN_MS) return
    lastSent.current[type] = now
    void api(`/student/attempts/${attemptID}/integrity-events`, { method: 'POST', body: JSON.stringify({ type }) }).catch(() => undefined)
  }, [attemptID])

  useEffect(() => {
    if (!active || !attemptID) return
    const onVisibility = () => { if (document.visibilityState === 'hidden') report('tab_hidden') }
    const onBlur = () => { if (document.visibilityState === 'visible') report('window_blur') }
    const onFullscreen = () => {
      const entered = Boolean(document.fullscreenElement)
      setIsFullscreen(entered)
      if (!entered) report('fullscreen_exit')
    }
    const onPaste = (event: ClipboardEvent) => { event.preventDefault(); report('paste_attempt') }
    const onCopy = (event: ClipboardEvent) => { event.preventDefault(); report('copy_attempt') }
    const onContextMenu = (event: MouseEvent) => event.preventDefault()
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('blur', onBlur)
    document.addEventListener('fullscreenchange', onFullscreen)
    document.addEventListener('paste', onPaste)
    document.addEventListener('copy', onCopy)
    document.addEventListener('cut', onCopy)
    document.addEventListener('contextmenu', onContextMenu)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      window.removeEventListener('blur', onBlur)
      document.removeEventListener('fullscreenchange', onFullscreen)
      document.removeEventListener('paste', onPaste)
      document.removeEventListener('copy', onCopy)
      document.removeEventListener('cut', onCopy)
      document.removeEventListener('contextmenu', onContextMenu)
      if (document.fullscreenElement) void document.exitFullscreen().catch(() => undefined)
    }
  }, [active, attemptID, report])

  return { needsFullscreen: active && fullscreenSupported() && !isFullscreen }
}
