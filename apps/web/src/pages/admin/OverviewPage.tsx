import { Link } from 'react-router'
import { useSession } from '../../app/session'
import { Metric } from '../../app/shared'
import { useI18n } from '../../i18n'
import { useClasses, useLearningSummary, useStudents, useYears } from '../../lib/queries'
import { adminPages } from './pages'

export default function OverviewPage() {
  const { t } = useI18n()
  const { identity } = useSession()
  const students = useStudents().data ?? []
  const classes = useClasses().data ?? []
  const years = useYears().data ?? []
  const summary = useLearningSummary().data
  const links = adminPages.filter((page) => page.id !== 'overview' && (page.id !== 'demo-requests' || identity?.platform_operator))
  return <>
    <section className="metrics">
      <Metric icon="♧" label={t('overview.students')} value={String(students.length)} detail={t('overview.studentsDetail')} />
      <Metric icon="▤" label={t('overview.classes')} value={String(classes.length)} detail={t('overview.classesDetail')} />
      <Metric icon="◷" label={t('overview.years')} value={String(years.length)} detail={t('overview.yearsDetail')} />
      <Metric icon="✳" label={t('overview.insights')} value={summary ? `${summary.average_score_percent}%` : '—'}
        detail={summary ? t('overview.insightsDetail', { submitted: summary.submitted_attempts, active: summary.active_recommendations }) : t('common.loading')} />
    </section>
    <nav className="overview-links" aria-label={t('overview.linksLabel')}>
      {links.map((page) => <Link key={page.id} to={`/admin/${page.id}`}>
        <span>{page.icon}</span><strong>{t(`admin.nav.${page.id}`)}</strong><small>{t(`admin.desc.${page.id}`)}</small><b aria-hidden="true">→</b>
      </Link>)}
    </nav>
  </>
}
