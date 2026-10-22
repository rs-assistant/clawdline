// Where the copied Cloud transport tells the browser's connection-health producer what happened
// (docs/admin-connection-health-contract.md §11). The transport imports only this file; the
// producer (`web/console/src/health-viewer/`) installs itself here when the page turned it on.
//
// Nothing installed is the default, and then every call below is a no-op. An installed observer
// that throws is caught here: observing a connection must never become a second failure of it.

var observer = null;

/** Install `next` (`{ record, correlation, monotonic }`); answers the function that removes it. */
export function installViewerHealthObserver(next) {
    observer = next || null;
    return function () { if (observer === next) observer = null; };
}

function record(observation) {
    var o = observer;
    if (!o) return;
    try { o.record(observation); } catch (e) { /* the observer's failure stays its own */ }
}

function correlation() {
    var o = observer;
    if (!o) return undefined;
    try { return o.correlation(); } catch (e) { return undefined; }
}

function monotonic() {
    var o = observer;
    try { return o ? o.monotonic() : null; } catch (e) { return null; }
}

var NOOP_SPAN = Object.freeze({
    complete: function () {},
    fail: function () {},
    mark: function () {},
    elapsed: function () { return undefined; }
});

/**
 * Start one stage attempt: records `viewer_stage_started` and answers its span. `complete(outcome)`
 * and `fail(error, outcome)` settle it once; later calls do nothing. `mark(stage, event, fields)`
 * records another stage under the same correlation (a read's response and its decrypt-apply).
 */
export function viewerHealthBegin(stage, cls) {
    if (!observer) return NOOP_SPAN;
    var trace = correlation();
    var started = monotonic();
    var settled = false;
    function elapsed() {
        var at = monotonic();
        return started === null || at === null ? undefined : Math.max(0, at - started);
    }
    function base(extra) {
        var o = { stage: stage, correlation: trace, elapsedMs: elapsed() };
        if (cls) o.class = cls;
        return Object.assign(o, extra);
    }
    record(Object.assign({ event: "viewer_stage_started", stage: stage, correlation: trace }, cls ? { class: cls } : {}));
    return {
        complete: function (outcome) {
            if (settled) return;
            settled = true;
            record(base({ event: "viewer_stage_completed", outcome: outcome || "ok" }));
        },
        fail: function (error, outcome) {
            if (settled) return;
            settled = true;
            record(base({ event: "viewer_stage_failed", error: error || "unclassified",
                outcome: outcome || (error === "timeout" ? "timeout" : "error") }));
        },
        mark: function (other, event, fields) {
            var o = Object.assign({ event: event, stage: other, correlation: trace, elapsedMs: elapsed() },
                cls ? { class: cls } : {}, fields || {});
            if (event === "viewer_stage_failed" && !o.error) o.error = "unclassified";
            record(o);
        },
        elapsed: elapsed
    };
}

/** One failed stage with no start of its own (a gap, an envelope that would not open). */
export function viewerHealthFailed(stage, error, fields) {
    if (!observer) return;
    record(Object.assign({ event: "viewer_stage_failed", stage: stage, correlation: correlation(),
        error: error || "unclassified", outcome: "error" }, fields || {}));
}

var AUTH_REFUSED = new Set(["sign_in", "unauthorized", "no_session", "forbidden", "token_expired",
    "token_superseded", "wrong_account", "wrong_device"]);
var NETWORK = new Set(["socket_error", "network", "network_error", "fetch_failed", "websocket_unavailable"]);
var CLOSED = new Set(["offline", "cloud_reconnecting", "carrier_closed", "relay_closed"]);
var RATE = new Set(["rate_limited", "over_capacity", "device_limit_reached"]);
var DECRYPT = new Set(["bad_payload", "unknown_key", "bad_signature", "decrypt_failed", "unknown_sender",
    "missing_device_key", "pairing_required"]);

/**
 * The fixed error word for a thrown transport failure. `where` is "connect" (opening the relay
 * socket: an `offline` there is a network that did not answer) or "read" (a read whose socket
 * went away: the carrier closed). Anything not named here is `unclassified`, never a guess.
 */
export function classifyViewerError(error, where) {
    var code = error && typeof error.code === "string" ? error.code : "";
    var status = error && Number.isInteger(error.status) ? error.status : null;
    if (code === "revoked") return "auth_revoked";
    if (AUTH_REFUSED.has(code)) return "auth_refused";
    if (code === "replay") return "replay_rejected";
    if (/timeout|deadline/.test(code)) return "timeout";
    if (NETWORK.has(code)) return "network_unreachable";
    if (CLOSED.has(code)) return where === "connect" && code === "offline" ? "network_unreachable" : "carrier_closed";
    if (DECRYPT.has(code)) return "decrypt_failed";
    if (RATE.has(code)) return "http_4xx";
    if (status !== null && status >= 500) return "http_5xx";
    if (status !== null && status >= 400) return "http_4xx";
    return "unclassified";
}
