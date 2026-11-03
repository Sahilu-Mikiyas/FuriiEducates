import { Marks } from '../../features/marks/Marks'
import { ClassReports } from '../../features/reports/ClassReports'
import { useTeacherContext } from './TeacherLayout'

export function TeacherMarksPage() {
  const { classroom } = useTeacherContext()
  return classroom ? <Marks key={classroom.id} classroom={classroom} /> : null
}

export function TeacherReportCardsPage() {
  const { classroom } = useTeacherContext()
  return classroom ? <ClassReports key={classroom.id} classroom={classroom} canPublish={false} /> : null
}
