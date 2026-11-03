import { describe, expect, it } from 'vitest'
import type { ReportCardData } from '../../lib/types'
import { subjectInsights } from './insights'

const card = (subjects: [string, number | null, number?][], earlier = false) => ({
  earlier_terms: earlier ? [{ term_id: 't1', name: 'Semester 1', average: 70, rank: 2 }] : [],
  subjects: subjects.map(([name, total, before]) => ({ subject_id: name, name, total, grade: '', counts_in_average: true, earlier: before === undefined ? undefined : { t1: before } })),
}) as unknown as ReportCardData

describe('subjectInsights', () => {
  it('picks the two strongest and two weakest of four or more subjects', () => {
    const { best, focus } = subjectInsights(card([['Maths', 90], ['English', 70], ['Physics', 50], ['Amharic', 80]]))
    expect(best.map((s) => s.name)).toEqual(['Maths', 'Amharic'])
    expect(focus.map((s) => s.name)).toEqual(['Physics', 'English'])
  })

  it('picks one each from two or three subjects', () => {
    const { best, focus } = subjectInsights(card([['Maths', 90], ['English', 70], ['Physics', 50]]))
    expect(best.map((s) => s.name)).toEqual(['Maths'])
    expect(focus.map((s) => s.name)).toEqual(['Physics'])
  })

  it('leaves out subjects with missing marks', () => {
    const { best, focus } = subjectInsights(card([['Maths', 90], ['English', null], ['Physics', 50]]))
    expect(best.map((s) => s.name)).toEqual(['Maths'])
    expect(focus.map((s) => s.name)).toEqual(['Physics'])
  })

  it('shows the change since the term before', () => {
    const { best, focus } = subjectInsights(card([['Maths', 90, 80], ['Physics', 50, 62.5]], true))
    expect(best[0].change).toBe(10)
    expect(focus[0].change).toBe(-12.5)
  })

  it('has no change without an earlier mark', () => {
    expect(subjectInsights(card([['Maths', 90], ['Physics', 50]])).best[0].change).toBeNull()
  })

  it('says nothing when it cannot tell subjects apart', () => {
    expect(subjectInsights(card([['Maths', 80]]))).toEqual({ best: [], focus: [] })
    expect(subjectInsights(card([['Maths', 80], ['Physics', 80]]))).toEqual({ best: [], focus: [] })
    expect(subjectInsights(card([]))).toEqual({ best: [], focus: [] })
  })
})
