import type { ReportCardData } from '../../lib/types'

export type SubjectInsight = { name: string; total: number; change: number | null }

// subjectInsights picks a student's strongest and weakest subjects from a report
// card, with the change since the term before. It only compares the student with
// themself, and says nothing when every subject scored the same.
export function subjectInsights(card: ReportCardData) {
  const previous = card.earlier_terms?.at(-1)?.term_id
  const subjects: SubjectInsight[] = card.subjects.flatMap((subject) => {
    if (subject.total === null) return []
    const before = previous ? subject.earlier?.[previous] : null
    return [{ name: subject.name, total: subject.total, change: before === null || before === undefined ? null : Math.round((subject.total - before) * 100) / 100 }]
  }).sort((p, q) => q.total - p.total)
  if (subjects.length < 2 || subjects[0].total === subjects[subjects.length - 1].total) return { best: [], focus: [] }
  const count = subjects.length >= 4 ? 2 : 1
  return { best: subjects.slice(0, count), focus: subjects.slice(-count).reverse() }
}
