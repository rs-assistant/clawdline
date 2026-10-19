import { lazy, Suspense, useEffect, useState } from "react"
import type { PageModule } from "./types.js"
import { nextWord } from "../next-strings.js"

const UsageOverviewPage = lazy(() => import("./usage/UsageOverview.js"))
function UsagePage({ shown }: { shown: boolean }) {
  const [opened, setOpened] = useState(false)
  useEffect(() => { if (shown) setOpened(true) }, [shown])
  if (!shown && !opened) return null
  return <Suspense fallback={<section className="page board-page" hidden={!shown} role="status">{nextWord("usageLoading")}</section>}>
    <UsageOverviewPage shown={shown} />
  </Suspense>
}
export const page: PageModule = { id: "usage", requiresApiLevel: 22, Component: UsagePage }
