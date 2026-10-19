# Static HTML reports

The document reader accepts self-contained `.html` reports alongside Markdown and text. The existing machine, Session, project/task scope and relative-path identity still govern every read; documents remain encrypted over Cloud. This format adds no operation, envelope field, protocol version, error code or capacity limit.

Reports can use embedded CSS, static inline SVG, tables, and native `details`/`summary`. Scripts, forms, navigation and external resources are removed or blocked. Multi-file sites and external images, fonts or scripts are outside this format. Use a responsive report and a horizontal scrolling wrapper for wide tables.

The daemon sanitizes HTML with pinned bluemonday and parses CSS with douceur. It keeps a static property/function allowlist, local SVG fragments and safe CSS variables. It rejects resource values and resource at-rules. The existing byte cap applies before and after cleanup. Markdown and text bytes keep their existing representation.

The console sanitizes received HTML again with pinned DOMPurify and renders it in an iframe with an empty sandbox, without script or same-origin privileges. A trusted CSP precedes report content and denies external resources and operations. The report never enters the console DOM. Its title and metadata use text nodes. The direct HTTP document route also applies CSP sandbox and retains its restrictive resource policy; use the console preview for report styling.

A new console with an older daemon cannot read HTML. An older console rejects the new MIME value conservatively; it does not gain HTML rendering. Deploy the console before enabling the daemon format. Existing Markdown and text continue to work. Rollback the console and daemon independently to compatible versions, retaining the report file for a later read.

Canonical HTML document locators are also accepted by Notes. Their machine, route Session, scope and relative-path checks remain the same as for Markdown and text; the Note links to the existing isolated document viewer. Other formats such as XHTML and JavaScript remain unsupported.

## Verification and measurements

The local browser fixture uses the real renderer and a real HTTP collector, in Chromium and WebKit at 390 and 1280 CSS pixels. It checks the report tables and native expansion, then attempts scripts, parent DOM access, navigation, popups and resource loads. The collector received zero requests. Chromium reports CSS attempts as failed with `csp`; a browser request event alone is not evidence of a request reaching a server.

The same sanitized report is 7,565 UTF-8 bytes and is fetched once in each sample. The original decoder rejects HTML, so its refusal cannot be compared with successful rendering as an efficiency improvement. Existing Markdown takes the same rendering path; short samples are frequently below the browser timer resolution and do not establish a speedup. Heap samples are coarse or unavailable and are not a memory-reduction claim.

Measured fetch, mount, ready times and raw Markdown samples are in [the browser record](performance/html-artifact-browser.json). `readyMs` includes browser automation waiting for the report table; it is not a production SLO. These samples are local component checks, not authenticated Cloud acceptance, an actual phone test or a capacity benchmark. Deployment, independent review and Cloud identity/revocation acceptance require separate evidence.
