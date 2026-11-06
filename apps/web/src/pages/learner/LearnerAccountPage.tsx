import { ChangePasswordPanel, PreferencesPanel } from '../../features/account/account'
import { NotificationSettingsPanel } from '../../features/notifications/notifications'

export default function LearnerAccountPage() {
  return <><NotificationSettingsPanel /><PreferencesPanel /><ChangePasswordPanel /></>
}
