import { useI18n } from '../../i18n'
import type { PaperEstimate, ReadinessTotal } from '../../lib/types'

export const allPapers = ['english', 'mathematics', 'aptitude', 'physics', 'chemistry', 'biology', 'history', 'geography', 'economics'] as const

export function usePaperName() {
  const { label } = useI18n()
  return (paper: string) => label('paper', paper)
}

export const heatClass = (percent: number) => (percent < 40 ? 'heat-low' : percent < 65 ? 'heat-mid' : 'heat-high')

// TotalBadge states an estimate as distance from the admission line.
export function TotalBadge({ total }: { total: ReadinessTotal }) {
  const { t, label } = useI18n()
  return <span className={`readiness-badge ${total.status}`} title={t('readiness.rangeLine', { low: Math.round(total.low), high: Math.round(total.high) })}>
    {Math.round(total.predicted)} · {label('readiness.status', total.status)}
  </span>
}

// PaperBars shows each paper's estimate with its uncertainty.
export function PaperBars({ papers, minResponses }: { papers: PaperEstimate[]; minResponses: number }) {
  const { t } = useI18n()
  const paperName = usePaperName()
  return <div className="paper-bars">
    {papers.map((paper) => <div className="paper-bar" key={paper.paper}>
      <span className="paper-bar-name">{paperName(paper.paper)}</span>
      {paper.score === null
        ? <small>{t('readiness.noWork')}</small>
        : <>
            <div className="paper-bar-track" role="img" aria-label={t('readiness.paperRange', { score: Math.round(paper.score), low: Math.round(paper.low ?? 0), high: Math.round(paper.high ?? 0) })}>
              <i className="paper-bar-range" style={{ left: `${paper.low ?? 0}%`, width: `${(paper.high ?? 0) - (paper.low ?? 0)}%` }} />
              <i className="paper-bar-point" style={{ left: `${paper.score}%` }} />
            </div>
            <b>{paper.enough_evidence ? Math.round(paper.score) : '—'}</b>
          </>}
      <small className="paper-bar-count">{paper.enough_evidence ? t('readiness.responses', { n: paper.responses }) : t('readiness.responsesOf', { n: paper.responses, total: minResponses })}</small>
    </div>)}
  </div>
}
