import { mountHTMLPreview } from "./document-html.js";
import { richText } from "./markdown.js";

/** Documents and transcript turns share one escaped, fail-visible Markdown implementation. */
export function documentBodyHTML(answer) {
    return richText(answer && answer.text);
}

/** HTML uses a separate, unprivileged browsing context; text keeps its existing renderer. */
export function renderDocumentBody(element, answer) {
    if (answer && answer.media_type === "text/html; charset=utf-8") {
        mountHTMLPreview(element, answer);
    } else {
        element.innerHTML = documentBodyHTML(answer);
    }
}
