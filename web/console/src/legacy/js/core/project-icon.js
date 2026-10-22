/** Project marks fit the existing eight-cell slot, regardless of input size. */
function dpr() { return Math.max(1, Math.min(3, window.devicePixelRatio || 1)); }

export function drawIcon(canvas, icon, cellPx) {
    // Any replacement invalidates a pending image decode, including an empty mark.
    var generation = {};
    canvas._iconGeneration = generation;
    var ratio = dpr();
    var side = 8 * cellPx;
    if (icon && icon.svg) {
        var doc = new DOMParser().parseFromString(icon.svg, "image/svg+xml");
        var root = doc.documentElement;
        if (root.localName !== "svg" || doc.querySelector("parsererror")) return clear(canvas);
        root.setAttribute("xmlns", "http://www.w3.org/2000/svg");
        root.setAttribute("width", String(side));
        root.setAttribute("height", String(side));
        root.setAttribute("preserveAspectRatio", "xMidYMid meet");
        size(canvas, side, side, ratio);
        canvas.style.imageRendering = "auto";
        var image = new Image();
        image.onload = function () {
            if (canvas._iconGeneration !== generation) return;
            var g = canvas.getContext("2d");
            g.clearRect(0, 0, canvas.width, canvas.height);
            g.drawImage(image, 0, 0, canvas.width, canvas.height);
        };
        image.onerror = function () {
            if (canvas._iconGeneration === generation) {
                clear(canvas);
                canvas._iconKey = null;
            }
        };
        // Image context disables scripting and external subresources. Never insert
        // the supplied SVG into the document, even when it came from an older peer.
        image.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(new XMLSerializer().serializeToString(root));
        return true;
    }
    var cells = icon && icon.cells;
    if (!Array.isArray(cells) || !cells.length || cells.length > 64) return clear(canvas);
    var rows = cells.length, cols = 0;
    for (var r = 0; r < rows; r++) {
        if (!Array.isArray(cells[r])) return clear(canvas);
        cols = Math.max(cols, cells[r].length);
    }
    if (!cols || cols > 64) return clear(canvas);
    var unit = Math.min(cellPx, side / cols, side / rows);
    size(canvas, cols * unit, rows * unit, ratio);
    canvas.style.imageRendering = "pixelated";
    var g = canvas.getContext("2d");
    g.imageSmoothingEnabled = false;
    g.clearRect(0, 0, canvas.width, canvas.height);
    for (var y = 0; y < rows; y++) {
        for (var x = 0; x < cells[y].length; x++) {
            if (!/^#[0-9a-f]{6}$/i.test(cells[y][x] || "")) continue;
            g.fillStyle = cells[y][x];
            var left = Math.round(x * canvas.width / cols), top = Math.round(y * canvas.height / rows);
            g.fillRect(left, top, Math.round((x + 1) * canvas.width / cols) - left,
                Math.round((y + 1) * canvas.height / rows) - top);
        }
    }
    return true;
}

function size(canvas, width, height, ratio) {
    canvas.width = Math.max(1, Math.round(width * ratio));
    canvas.height = Math.max(1, Math.round(height * ratio));
    canvas.style.width = width + "px";
    canvas.style.height = height + "px";
}

function clear(canvas) {
    canvas.width = canvas.height = 0;
    canvas.style.width = canvas.style.height = "";
    return false;
}

export function drawIconOnce(canvas, icon, cellPx) {
    var key = JSON.stringify([icon || null, cellPx, dpr()]);
    if (canvas._iconKey === key) return canvas._iconDrawn;
    var drew = drawIcon(canvas, icon, cellPx);
    canvas._iconKey = key;
    canvas._iconDrawn = drew;
    return drew;
}
