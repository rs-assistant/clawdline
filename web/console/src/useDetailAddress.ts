import { useCallback, useEffect, useRef, useState } from "react"
import { arriveAtDetail, detailInHash, enterDetail, leaveDetail, restateDetail } from "./page-detail.js"

/**
 * The record a React page has open, held in the address (page-detail.ts).
 *
 * Answers the open id ("" for the list), `open(id)` and `close()`. The id
 * follows the address: the browser's Back, a typed address and the page's own
 * back control all arrive as the same change.
 */
export function useDetailAddress(page: string, shown: boolean): [string, (id: string) => void, () => void] {
  const [id, setId] = useState(() => detailInHash(page, location.hash))
  const idRef = useRef(id)
  idRef.current = id

  useEffect(() => {
    const follow = () => {
      // Another page's address says nothing about this page's record.
      if (!location.hash.includes(`page=${encodeURIComponent(page)}`)) return
      setId(detailInHash(page, location.hash))
    }
    window.addEventListener("popstate", follow)
    window.addEventListener("hashchange", follow)
    return () => {
      window.removeEventListener("popstate", follow)
      window.removeEventListener("hashchange", follow)
    }
  }, [page])

  // On arrival with a record named, and on coming back to a page still holding
  // one, the address is made to say so.
  const wasShown = useRef(false)
  useEffect(() => {
    if (shown && !wasShown.current && idRef.current) {
      if (detailInHash(page, location.hash) === idRef.current) arriveAtDetail(page, idRef.current)
      else restateDetail(page, idRef.current)
    }
    wasShown.current = shown
  }, [shown, page])

  const open = useCallback((next: string) => {
    enterDetail(page, next)
    setId(next)
  }, [page])
  const close = useCallback(() => {
    if (!leaveDetail(page)) setId("")
  }, [page])
  return [id, open, close]
}
