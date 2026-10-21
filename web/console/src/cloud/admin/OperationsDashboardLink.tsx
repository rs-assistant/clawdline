import { SidebarIcon } from "../../SidebarIcon.js"
import { nextWord } from "../../next-strings.js"

/** Discovery only: every operations read still rechecks superuser authority in Cloud. */
export function OperationsDashboardLink({ allowed }: { allowed: boolean }) {
  if (!allowed) return null
  return <a className="sidebar-item" id="nav-admin-dashboard" href="/admin/health">
    <SidebarIcon name="usage" />{nextWord("adminDashboardNav")}
  </a>
}
