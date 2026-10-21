import createDOMPurify from "dompurify";
import { validInlineImage, inlineImagePixels, MAXIMUM_REPORT_IMAGE_PIXELS } from "./document-html-images.js";

// The sandbox owns the origin boundary. CSP also stops resource-bearing CSS;
// the daemon has already filtered CSS before the bytes enter the envelope.
const policy = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src 'none'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";

/** Never insert the author's markup into the console's document. */
export function htmlPreviewSource(text, window) {
    const purifier = createDOMPurify(window);
    let retainedImagePixels = 0;
    purifier.addHook("uponSanitizeAttribute", function (node, data) {
        if (data.attrName === "style" && /\\|url\s*\(|image-set\s*\(/i.test(data.attrValue)) data.keepAttr = false;
        if (data.attrName === "src") {
            data.keepAttr = node.localName === "img" && validInlineImage(data.attrValue);
        }
        if (data.attrName === "href" || data.attrName === "xlink:href") {
            data.keepAttr = node.localName === "use" && /^#[a-zA-Z0-9_-]+$/.test(data.attrValue);
        }
    });
    purifier.addHook("afterSanitizeAttributes", function (node) {
        if (node.localName !== "img") return;
        const pixels = inlineImagePixels(node.getAttribute("src"));
        if (!pixels || retainedImagePixels + pixels > MAXIMUM_REPORT_IMAGE_PIXELS) node.remove();
        else retainedImagePixels += pixels;
    });
    purifier.addHook("afterSanitizeElements", function (node) {
        if (node.localName === "style" && /\\|url\s*\(|image-set\s*\(|@import|@font-face/i.test(node.textContent)) node.textContent = "";
    });
    const clean = purifier.sanitize(text, {
        USE_PROFILES: { html: true, svg: true },
        ADD_TAGS: ["use"],
        WHOLE_DOCUMENT: true,
        FORBID_TAGS: ["script", "iframe", "object", "embed", "form", "input", "button", "select", "textarea", "base", "meta", "link", "picture", "source", "video", "audio", "foreignObject", "animate", "animateTransform", "set"],
        FORBID_ATTR: ["srcset", "action", "formaction", "target", "download", "ping", "poster", "background", "data", "srcdoc"],
        ALLOW_DATA_ATTR: false
    });
    // The trusted policy precedes all author content, including an author head.
    return '<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="' + policy + '">' +
        '<meta name="viewport" content="width=device-width, initial-scale=1">' +
        '<style>html{color-scheme:dark light}body{margin:16px;font:16px/1.5 system-ui;overflow-wrap:anywhere}svg,img{max-width:100%;height:auto}table{max-width:100%}</style>' +
        '</head><body>' + clean + '</body></html>';
}

export function mountHTMLPreview(element, answer) {
    const doc = element.ownerDocument;
    const frame = doc.createElement("iframe");
    frame.className = "document-html-preview";
    frame.title = answer.path;
    frame.setAttribute("sandbox", "");
    frame.setAttribute("referrerpolicy", "no-referrer");
    frame.srcdoc = htmlPreviewSource(answer.text, doc.defaultView);
    element.replaceChildren(frame);
}
