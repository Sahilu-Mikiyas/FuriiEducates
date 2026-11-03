import { AttendanceAlerts, AttendanceReport, Register } from '../../features/attendance/attendance'
import { useI18n } from '../../i18n'
import { useTeacherContext } from './TeacherLayout'

export default function TeacherAttendancePage() {
  const { t } = useI18n()
  const { classID } = useTeacherContext()
  if (!classID) return null
  return <div className="operations-page">
    <section className="panel">
      <div className="panel-head"><div><h2>{t('attendance.registerTitle')}</h2><p>{t('attendance.registerHelp')}</p></div></div>
      <Register classID={classID} base="/teach" />
    </section>
    <AttendanceAlerts classID={classID} base="/teach" />
    <AttendanceReport classID={classID} />
  </div>
}
