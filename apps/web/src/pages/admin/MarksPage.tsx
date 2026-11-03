import { useState } from 'react'
import { Marks } from '../../features/marks/Marks'
import { ClassReports } from '../../features/reports/ClassReports'
import { useI18n } from '../../i18n'
import { useClasses } from '../../lib/queries'
import { classLabel } from '../../lib/types'

function ClassPicker({ view }: { view: 'marks' | 'reports' }) {
  const { t } = useI18n()
  const classes = useClasses()
  const [picked, setPicked] = useState('')
  const classroom = classes.data?.find((c) => c.id === picked) ?? classes.data?.[0]
  if (classes.isSuccess && !classroom) return <section className="panel"><p className="empty">{t('attendance.noClasses')}</p></section>
  return <>
    <select className="register-class marks-class" aria-label={t('attendance.chooseClass')} value={classroom?.id ?? ''} onChange={(event) => setPicked(event.target.value)}>
      {classes.data?.map((c) => <option key={c.id} value={c.id}>{classLabel(c)}</option>)}
    </select>
    {classroom && (view === 'marks'
      ? <Marks key={classroom.id} classroom={classroom} />
      : <ClassReports key={classroom.id} classroom={classroom} canPublish />)}
  </>
}

export const AdminMarksPage = () => <ClassPicker view="marks" />
export const AdminReportCardsPage = () => <ClassPicker view="reports" />
