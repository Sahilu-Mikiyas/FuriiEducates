// Service worker template. The build fills in PRECACHE with the app's files
// (see vite.config.ts). It stores the app itself so it opens with no connection.
// It never touches /api: student records and answers are not kept here.
const PRECACHE = __PRECACHE__
const CACHE = 'furii-app-v1'
const SHELL = '/'
const NAVIGATION_TIMEOUT_MS = 4000

self.addEventListener('install', (event) => {
  event.waitUntil((async () => {
    const cache = await caches.open(CACHE)
    // File names change only when content does, so a new release downloads just
    // what changed. That matters on school connections.
    for (const url of PRECACHE) {
      if (!(await cache.match(url))) await cache.add(url)
    }
    await cache.put(SHELL, await fetch(SHELL, { cache: 'no-cache' }))
  })())
})

self.addEventListener('activate', (event) => {
  event.waitUntil((async () => {
    const keep = new Set([...PRECACHE, SHELL])
    const cache = await caches.open(CACHE)
    for (const request of await cache.keys()) {
      if (!keep.has(new URL(request.url).pathname)) await cache.delete(request)
    }
    for (const name of await caches.keys()) {
      if (name !== CACHE) await caches.delete(name)
    }
    await self.clients.claim()
  })())
})

// The page asks for this when no exam or practice is open, so an update never
// changes the app under a student who is working.
self.addEventListener('message', (event) => {
  if (event.data === 'SKIP_WAITING') self.skipWaiting()
})

async function navigate(request) {
  const cache = await caches.open(CACHE)
  const timeout = new Promise((resolve) => setTimeout(() => resolve(null), NAVIGATION_TIMEOUT_MS))
  try {
    const response = await Promise.race([fetch(request), timeout])
    if (response) return response
  } catch {
    // No connection: use the stored app below.
  }
  return (await cache.match(SHELL)) ?? fetch(request)
}

async function stored(request) {
  const cache = await caches.open(CACHE)
  const hit = await cache.match(request)
  if (hit) return hit
  const response = await fetch(request)
  if (response.ok) await cache.put(request, response.clone())
  return response
}

self.addEventListener('fetch', (event) => {
  const { request } = event
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin || url.pathname.startsWith('/api/')) return
  if (request.mode === 'navigate') event.respondWith(navigate(request))
  else if (url.pathname.startsWith('/assets/')) event.respondWith(stored(request))
})
