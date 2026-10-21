import { lazy, Suspense, useEffect, useState } from "react"
import type { PageModule } from "./types.js"
import { UsagePageSkeleton } from "./usage/UsageSkeleton.js"

const UsageOverviewPage = lazy(() => import("./usage/UsageOverview.js"))
function UsagePage({ shown }: { shown: boolean }) {
  const [opened, setOpened] = useState(false)
  useEffect(() => { if (shown) setOpened(true) }, [shown])
  if (!shown && !opened) return null
  return <Suspense fallback={<UsagePageSkeleton shown={shown} />}>
    <UsageOverviewPage shown={shown} />
  </Suspense>
}
export const page: PageModule = { id: "usage", requiresApiLevel: 22, Component: UsagePage }
