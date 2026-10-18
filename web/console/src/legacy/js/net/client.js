/* --------------------------------------------------------------------------
   The transport seam

   JavaScript has no runtime interface declarations, so ClawdlineClient is a
   deliberately small structural contract.  Both transports may expose more
   compatibility methods, but the view's portable core depends only on these.
   -------------------------------------------------------------------------- */

export const LOCAL_MACHINE = "this-mac";

/** The Cloud inventory marker's session id, and how many ids it may name.
 *  Every console file reads these two here; the daemon's copy is
 *  internal/domain/cloud InventorySessionID and InventoryLimit. */
export const SESSION_INVENTORY_ID = "__clawdline_inventory_v1__";
export const SESSION_INVENTORY_LIMIT = 512;

/** Carry one lazy report selector through both the direct and current closed Cloud Board read. */
export function boardReportSelection(item, report) {
    if (typeof item !== "string" || !item || typeof report !== "string" || !report)
        throw new TypeError("a report selection needs non-empty item and report ids");
    const value = "report:" + item + ":" + report;
    if (new TextEncoder().encode(value).length > 200)
        throw new TypeError("the report selection exceeds its transport bound");
    return value;
}

export const ClawdlineClient = Object.freeze({
    methods: Object.freeze([
        "events", "sessions", "transcript", "send", "answer", "dispatch", "schedules"
    ])
});

/** Turn the old local id, or the cloud pair, into the one identity shape. */
export function sessionIdentity(value, fallbackMachine) {
    var machine = fallbackMachine || LOCAL_MACHINE;
    var session = value;
    if (value && typeof value === "object") {
        machine = value.machine || machine;
        session = value.session !== undefined ? value.session : value.id;
    }
    if (typeof machine !== "string" || !machine || typeof session !== "string" || !session) {
        throw new TypeError("a session identity needs non-empty machine and session strings");
    }
    return { machine: machine, session: session };
}

/** Stable only inside the client; unlike a slash join it cannot collide. */
export function sessionIdentityKey(value, fallbackMachine) {
    var identity = sessionIdentity(value, fallbackMachine);
    return identity.machine + "\u0000" + identity.session;
}

/** Protocol channel segments are printable ASCII; percent encoding preserves opaque ids. */
export function channelSegment(value) {
    if (typeof value !== "string" || !value) throw new TypeError("empty channel segment");
    return encodeURIComponent(value).replace(/\|/g, "%7C");
}

export function decodedChannelSegment(value) {
    try { return decodeURIComponent(value); } catch (e) { return value; }
}
