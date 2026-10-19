import createDOMPurify from "dompurify";

// The sandbox owns the origin boundary. CSP also stops resource-bearing CSS;
// the daemon has already filtered CSS before the bytes enter the envelope.
const policy = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src 'none'; font-src 'none'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";

/** Never insert the author's markup into the console's document. */
export function htmlPreviewSource(text, window) {
    const purifier = createDOMPurify(window);
    purifier.addHook("uponSanitizeAttribute", function (node, data) {
        if (data.attrName === "href" || data.attrName === "xlink:href") {
            data.keepAttr = node.localName === "use" && /^#[a-zA-Z0-9_-]+$/.test(data.attrValue);
        }
    });
    const clean = purifier.sanitize(text, {
        USE_PROFILES: { html: true, svg: true },
        WHOLE_DOCUMENT: true,
        FORBID_TAGS: ["script", "iframe", "object", "embed", "form", "input", "button", "select", "textarea", "base", "meta", "link", "img", "picture", "source", "video", "audio", "foreignObject", "animate", "animateTransform", "set"],
        FORBID_ATTR: ["src", "srcset", "action", "formaction", "target", "download", "ping", "poster", "background", "data", "srcdoc"],
        ALLOW_DATA_ATTR: false
    });
    // The trusted policy precedes all author content, including an author head.
    return '<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="' + policy + '">' +
        '<meta name="viewport" content="width=device-width, initial-scale=1">' +
        '<style>html{color-scheme:dark light}body{margin:16px;font:16px/1.5 system-ui;overflow-wrap:anywhere}svg{max-width:100%;height:auto}table{max-width:100%}</style>' +
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
