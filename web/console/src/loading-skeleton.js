/** Shared decorative geometry for React and the remaining DOM-owned panels. */
export const loadingLayouts = {
    rows: [68, 42, 76],
    cards: [72, 48, 64],
    text: [88, 62, 78, 48, 84, 58],
    detail: [64, 86, 52, 78, 60, 42],
};

function escape(value) {
    return String(value).replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

/** Labels and IDs are escaped; the shape is selected only from fixed geometry. */
export function loadingSkeletonHTML(label, id, kind = "rows") {
    const shape = Object.hasOwn(loadingLayouts, kind) ? kind : "rows";
    return '<div class="loading-skeleton loading-skeleton-' + shape + ' page-skeleton" role="status" data-loading-id="' + escape(id) + '">' +
        '<span class="loading-skeleton-label">' + escape(label) + '</span><div aria-hidden="true">' +
        loadingLayouts[shape].map(width => '<div class="loading-skeleton-row"><i class="loading-skeleton-mark page-skeleton-line"></i><div class="loading-skeleton-copy"><i class="page-skeleton-line" style="width:' + width + '%"></i><i class="page-skeleton-line"></i></div></div>').join("") +
        '</div></div>';
}
