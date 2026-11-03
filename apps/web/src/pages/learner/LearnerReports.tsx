import { useQuery } from '@tanstack/react-query'
import { ReportCardView, usePrint } from '../../features/reports/ReportCard'
import { useI18n } from '../../i18n'
import { api } from '../../lib/api'
import type { ReportCardData } from '../../lib/types'

export default function LearnerReports() {
  const { t } = useI18n()
  const { print, area } = usePrint<ReportCardData>()
  const query = useQuery({ queryKey: ['/student/report-cards'], queryFn: () => api<ReportCardData[]>('/student/report-cards') })
  const cards = query.data ?? []
  return <>
    <div className="learner-welcome"><h1>{t('reports.studentTitle')}</h1><p className="muted">{t('reports.studentHelp')}</p></div>
    {query.isSuccess && !cards.length && <section className="panel"><p className="empty">{t('reports.studentEmpty')}</p></section>}
    {cards.map((card) => <section className="panel" key={card.term_id}>
      <div className="panel-head"><div><h2>{card.term_name} · {card.year_name}</h2></div><button className="secondary compact" onClick={() => print(card)}>{t('reports.printCard')}</button></div>
      <ReportCardView card={card} />
    </section>)}
    {area((card) => <ReportCardView card={card} />)}
  </>
}
