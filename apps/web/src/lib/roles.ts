import type { Identity } from './types'

// The office workspace (/admin) serves the administrator, the registrar and the
// finance officer. Each sees only the pages for their work; the server checks
// every request again.
export const officeRoles = ['school_admin', 'registrar', 'finance_officer']
export const staffRoleOptions = ['school_admin', 'teacher', 'registrar', 'finance_officer'] as const

const officePages: Record<string, string[]> = {
  registrar: ['students', 'attendance', 'academic-passport', 'notifications'],
  finance_officer: ['finance', 'notifications'],
}

export const isOffice = (identity: Identity) => identity.roles.some((role) => officeRoles.includes(role))

// canOpenOfficePage reports whether a person may open an office page.
export function canOpenOfficePage(identity: Identity, page: string) {
  if (identity.roles.includes('school_admin')) return true
  return identity.roles.some((role) => officePages[role]?.includes(page))
}

// officeHome is the first office page a person can open.
export function officeHome(identity: Identity) {
  if (identity.roles.includes('school_admin')) return '/admin/overview'
  if (identity.roles.includes('registrar')) return '/admin/students'
  return '/admin/finance'
}
