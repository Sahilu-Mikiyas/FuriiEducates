import { useEffect, useState } from 'react'

const sittingPaths = /^\/learn\/(assessments|prep\/session|prep\/offline)\//

// registerServiceWorker stores the app on the device in production. An update
// waits until no exam or practice is open. Visitors who only see the public
// landing page are not made to download the whole app.
export function registerServiceWorker() {
  if (!import.meta.env.PROD || !('serviceWorker' in navigator) || window.location.pathname === '/') return
  window.addEventListener('load', () => {
    void navigator.serviceWorker.register('/sw.js').then((registration) => {
      const activate = (worker: ServiceWorker | null) => {
        if (worker && !sittingPaths.test(window.location.pathname)) worker.postMessage('SKIP_WAITING')
      }
      activate(registration.waiting)
      registration.addEventListener('updatefound', () => {
        const worker = registration.installing
        worker?.addEventListener('statechange', () => {
          if (worker.state === 'installed' && navigator.serviceWorker.controller) activate(worker)
        })
      })
    }).catch(() => undefined)
  })
}

type InstallEvent = Event & { prompt: () => Promise<void>; userChoice: Promise<{ outcome: string }> }

let installEvent: InstallEvent | null = null
const listeners = new Set<() => void>()

if (typeof window !== 'undefined') {
  window.addEventListener('beforeinstallprompt', (event) => {
    event.preventDefault()
    installEvent = event as InstallEvent
    listeners.forEach((listener) => listener())
  })
  window.addEventListener('appinstalled', () => {
    installEvent = null
    listeners.forEach((listener) => listener())
  })
}

const isStandalone = () => window.matchMedia?.('(display-mode: standalone)').matches || (navigator as Navigator & { standalone?: boolean }).standalone === true
const isIOS = () => /iphone|ipad|ipod/i.test(navigator.userAgent)

// useInstall offers the browser's install prompt. iPhones have no prompt, so they
// get instructions instead.
export function useInstall() {
  const [, render] = useState(0)
  useEffect(() => {
    const listener = () => render((n) => n + 1)
    listeners.add(listener)
    return () => { listeners.delete(listener) }
  }, [])
  return {
    canInstall: Boolean(installEvent),
    showIOSHelp: isIOS() && !isStandalone(),
    install: async () => {
      if (!installEvent) return
      await installEvent.prompt()
      await installEvent.userChoice.catch(() => undefined)
      installEvent = null
      render((n) => n + 1)
    },
  }
}

// useOnline follows the browser's connection state. It can read true on a school
// network with no internet, so requests must still handle failure.
export function useOnline() {
  const [online, setOnline] = useState(navigator.onLine)
  useEffect(() => {
    const update = () => setOnline(navigator.onLine)
    window.addEventListener('online', update)
    window.addEventListener('offline', update)
    return () => { window.removeEventListener('online', update); window.removeEventListener('offline', update) }
  }, [])
  return online
}
