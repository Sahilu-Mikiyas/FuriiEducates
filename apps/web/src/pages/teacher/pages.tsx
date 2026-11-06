import { ChangePasswordPanel, PreferencesPanel } from '../../features/account/account'
import { NotificationSettingsPanel } from '../../features/notifications/notifications'
import { ExamSessions } from '../../features/exams/ExamSessions'
import { Gradebook } from '../../features/exams/Gradebook'
import { GradingQueue } from '../../features/exams/GradingQueue'
import { TeacherPlanning } from '../../features/operations/LessonPlans'
import { ClassReadiness } from '../../features/prep/ClassReadiness'
import { useI18n } from '../../i18n'
import { useTeacherContext } from './TeacherLayout'

export function TeacherReadinessPage() {
  const { classID } = useTeacherContext()
  return classID ? <ClassReadiness key={classID} classID={classID} /> : null
}

export function TeacherSessionsPage() {
  return <ExamSessions />
}

export function TeacherGradebookPage() {
  const { classID } = useTeacherContext()
  return classID ? <Gradebook key={classID} classID={classID} /> : null
}

export function TeacherAnswersPage() {
  const { t } = useI18n()
  const { classID } = useTeacherContext()
  return classID
    ? <GradingQueue key={classID} classID={classID} listPath="/teacher/reviews" help={t('grading.teacherHelp')}
        gradePath={(review) => `/teacher/attempts/${review.attempt_id}/answers/${review.question_id}`} />
    : null
}

export function TeacherPlanningPage() {
  const { classID } = useTeacherContext()
  return classID ? <TeacherPlanning classID={classID} /> : null
}

export function TeacherAccountPage() {
  return <><NotificationSettingsPanel /><PreferencesPanel /><ChangePasswordPanel /></>
}
