import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { api, APIError } from '../lib/api'
import type { Identity } from '../lib/types'

export const SESSION_KEY = ['session']

async function loadSession() {
  try {
    return await api<Identity>('/me')
  } catch (e) {
    if (e instanceof APIError && e.status === 401) return null
    throw e
  }
}

// useSession is the signed-in user, or null. A connection failure also reads as
// signed out, because the sign-in page explains a service that is starting up.
export function useSession(enabled = true) {
  const query = useQuery({ queryKey: SESSION_KEY, queryFn: loadSession, enabled, staleTime: Infinity, retry: false, meta: { silent: true } })
  return { identity: query.data ?? null, loading: enabled && query.isPending }
}

export const homePath = (identity: Identity) => {
  if (identity.roles.includes('student')) return '/learn'
  if (identity.roles.includes('school_admin')) return '/admin/overview'
  return '/teach/class'
}

// useLogout ends the session and removes every cached record, so nothing from
// this account stays in memory on a shared computer.
export function useLogout() {
  const client = useQueryClient()
  const navigate = useNavigate()
  return async () => {
    await api('/auth/logout', { method: 'POST' }).catch(() => undefined)
    client.clear()
    client.setQueryData(SESSION_KEY, null)
    navigate('/login', { replace: true })
  }
}
