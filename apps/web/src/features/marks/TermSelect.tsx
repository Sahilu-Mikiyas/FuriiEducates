import { useState } from 'react'
import { todayISO } from '../../calendar/ethiopian'
import { useI18n } from '../../i18n'
import { useTerms } from '../../lib/queries'
import type { Term } from '../../lib/types'

// useClassTerm offers the terms of a class's academic year. It opens on the term
// running today, or else the most recent one to have started.
export function useClassTerm(yearID: string) {
  const all = useTerms()
  const terms = (all.data ?? []).filter((term) => term.academic_year_id === yearID)
  const [picked, setPicked] = useState('')
  const today = todayISO()
  const fallback = terms.find((term) => term.starts_on <= today && term.ends_on >= today) ?? [...terms].reverse().find((term) => term.starts_on <= today) ?? terms[0]
  const term: Term | undefined = terms.find((item) => item.id === picked) ?? fallback
  return { terms, term, setPicked, loaded: all.isSuccess }
}

export function TermSelect({ terms, term, setPicked }: { terms: Term[]; term: Term | undefined; setPicked: (id: string) => void }) {
  const { t } = useI18n()
  return <select className="register-class" aria-label={t('marks.chooseTerm')} value={term?.id ?? ''} onChange={(event) => setPicked(event.target.value)}>
    {terms.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
  </select>
}
