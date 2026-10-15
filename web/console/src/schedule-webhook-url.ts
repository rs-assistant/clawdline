/**
 * How the schedule history sheet draws a one-time webhook URL.
 *
 * The URL is a password: anyone holding it can start the schedule, so the page
 * keeps it in one variable and never in its markup. A person still needs to see
 * that there is a URL, where it points, and which one they copied, so the sheet
 * draws everything but the token, and of the token only its last four characters.
 */
export function maskedWebhookURL(url: string): string {
  let parsed: URL
  try {
    parsed = new URL(url)
  } catch {
    // refusal-ok: a value the URL parser refuses is not a machine refusal; it is hidden whole
    return "••••••••"
  }
  const path = parsed.pathname.replace(/\/+$/, "")
  const cut = path.lastIndexOf("/")
  const token = path.slice(cut + 1)
  const tail = token.length > 12 ? token.slice(-4) : ""
  return parsed.origin + path.slice(0, cut + 1) + "••••••••" + tail
}

/** The two faces of Copy URL, drawn with `currentColor`. */
export const COPY_ICON =
  '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.5" ' +
  'stroke-linecap="round" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/>' +
  '<path d="M10.5 5.5V3.5a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2"/></svg>'

export const CHECK_ICON =
  '<svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" ' +
  'stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 8.5l3 3 6-7"/></svg>'
