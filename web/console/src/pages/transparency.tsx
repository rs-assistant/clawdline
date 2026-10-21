import { lazy, Suspense } from "react"
import { catalogWord } from "../catalog.js"
import type { PageModule } from "./types.js"
const Transparency = lazy(() => import("./transparency/TransparencyPage.js"))
function TransparencyPage({ shown }: { shown: boolean }) {
  if (!shown) return null
  return <Suspense fallback={<section className="page" aria-busy="true">{catalogWord("transparency", "title")}</section>}>
    <Transparency shown={shown} />
  </Suspense>
}
export const page: PageModule = { id: "transparency", Component: TransparencyPage }
