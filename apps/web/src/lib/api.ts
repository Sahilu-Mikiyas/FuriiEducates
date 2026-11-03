// Browsers reach the API through this site's own /api path: Vercel proxies it
// to the Go API (see vercel.json) and Vite proxies it in development. Same-origin
// requests keep the session cookie first-party, which Safari and iOS require.
// Set VITE_API_ORIGIN only for a deployment that has no /api proxy.
export const API_ORIGIN = (import.meta.env.VITE_API_ORIGIN ?? '').trim().replace(/\/+$/, '')

export const apiURL = (path: string) => `${API_ORIGIN}/api/v1${path}`

export class APIError extends Error {
  constructor(message: string, readonly status: number, readonly code: string) {
    super(message)
  }
}

export async function readAPIResponse(response: Response) {
  const contentType = response.headers.get('content-type')?.toLowerCase() ?? ''
  if (!contentType.includes('json')) {
    const responseURL = new URL(response.url)
    if (response.status === 502 || response.status === 503 || response.status === 504) {
      throw new APIError('The school service is starting up. Wait a minute and try again.', response.status, 'SERVICE_STARTING')
    }
    if (!API_ORIGIN && responseURL.origin === window.location.origin && response.ok) {
      throw new APIError('This site is not forwarding /api requests to the school service. Check the /api rewrite in vercel.json.', response.status, 'API_PROXY_MISSING')
    }
    if (response.status === 404) {
      throw new APIError(`The school service has no route for ${responseURL.pathname}. It may be running an older version.`, response.status, 'ROUTE_NOT_FOUND')
    }
    throw new APIError(`The school service returned an unexpected response (HTTP ${response.status}).`, response.status, 'UNEXPECTED_RESPONSE')
  }
  try {
    return await response.json()
  } catch {
    throw new APIError('The school service returned invalid JSON.', response.status, 'INVALID_JSON')
  }
}

async function request(path: string, init?: RequestInit) {
  const headers = new Headers(init?.headers)
  if (init?.body !== undefined && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(apiURL(path), { ...init, credentials: 'include', headers })
  const body = await readAPIResponse(response)
  if (!response.ok) {
    throw new APIError(body?.error?.message ?? 'The request could not be completed.', response.status, body?.error?.code ?? 'REQUEST_FAILED')
  }
  return body
}

// api sends a request with the session cookie and returns the response's data
// envelope, or throws an APIError carrying the server's message and code.
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  return (await request(path, init)).data as T
}

// apiPage is api for a paged list, which also returns the total across all pages.
export async function apiPage<T>(path: string): Promise<{ data: T; total: number }> {
  const body = await request(path)
  return { data: body.data as T, total: body.total as number }
}
