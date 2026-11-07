import { isOffice, officeHome } from '../lib/roles'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { clearOfflinePacks } from '../features/prep/offlineStore'
import { api, APIError } from '../lib/api'
import type { Identity } from '../lib/types'

export const SESSION_KEY = ['session']

const IDENTITY_KEY = 'furii.offline-identity'
const LOGOUT_PENDING_KEY = 'furii.logout-pending'

function readStorage(key: string) {
  try { return window.localStorage.getItem(key) } catch { return null }
}
function writeStorage(key: string, value: string | null) {
  try {
    if (value === null) window.localStorage.removeItem(key)
    else window.localStorage.setItem(key, value)
  } catch { /* Blocked storage only loses offline sign-in. */ }
}

// rememberIdentity lets a student reopen the app with no connection. Only
// students, because offline practice is the only thing that works offline.
export function rememberIdentity(identity: Identity) {
  // A new sign-in replaces the session a pending sign-out was meant to end.
  writeStorage(LOGOUT_PENDING_KEY, null)
  writeStorage(IDENTITY_KEY, identity.roles.includes('student') ? JSON.stringify(identity) : null)
}

function rememberedIdentity(): Identity | null {
  try { return JSON.parse(readStorage(IDENTITY_KEY) ?? 'null') as Identity | null } catch { return null }
}

const unreachable = (error: unknown) => error instanceof TypeError || (error instanceof APIError && error.code === 'SERVICE_STARTING')

async function loadSession() {
  // Signing out with no connection cannot end the session on the server. Finish
  // that first, so the next person on a shared computer is not signed in as them.
  if (readStorage(LOGOUT_PENDING_KEY)) {
    await api('/auth/logout', { method: 'POST' }).then(() => writeStorage(LOGOUT_PENDING_KEY, null)).catch(() => undefined)
    return null
  }
  try {
    const identity = await api<Identity>('/me')
    rememberIdentity(identity)
    return identity
  } catch (e) {
    if (e instanceof APIError && e.status === 401) {
      writeStorage(IDENTITY_KEY, null)
      return null
    }
    if (unreachable(e)) {
      const remembered = rememberedIdentity()
      if (remembered) return remembered
    }
    throw e
  }
}

// useSession is the signed-in user, or null. With no connection a student stays
// signed in from their last visit; the server checks again on reconnecting. For
// anyone else a connection failure reads as signed out, because the sign-in page
// explains a service that is starting up.
export function useSession(enabled = true) {
  const query = useQuery({ queryKey: SESSION_KEY, queryFn: loadSession, enabled, staleTime: Infinity, retry: false, meta: { silent: true }, networkMode: 'always' })
  return { identity: query.data ?? null, loading: enabled && query.isPending }
}

export const homePath = (identity: Identity) => {
  if (identity.roles.includes('student')) return '/learn'
  if (isOffice(identity)) return officeHome(identity)
  if (identity.roles.includes('teacher')) return '/teach/class'
  return '/family'
}

// useLogout ends the session and removes every cached record and downloaded
// question, so nothing from this account stays on a shared computer. Practice
// waiting to upload stays until the same student signs in again.
export function useLogout() {
  const client = useQueryClient()
  const navigate = useNavigate()
  return async () => {
    const ended = await api('/auth/logout', { method: 'POST' }).then(() => true).catch(() => false)
    if (!ended) writeStorage(LOGOUT_PENDING_KEY, '1')
    writeStorage(IDENTITY_KEY, null)
    await clearOfflinePacks().catch(() => undefined)
    client.clear()
    client.setQueryData(SESSION_KEY, null)
    navigate('/login', { replace: true })
  }
}
