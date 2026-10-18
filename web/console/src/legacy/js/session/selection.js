/* --------------------------------------------------------------------------
   Session selection

   A terminal id is display metadata inside a fleet.  The selectable thing is the route that
   owns it (machine + terminal/session id), plus the provider conversation id when the inventory
   has one.  This module owns that identity. It deliberately imports no transport, state or
   renderer: transports accept its frozen route.
   -------------------------------------------------------------------------- */

export const LOCAL_SESSION_MACHINE = "this-mac";

function nonempty(value) {
    return typeof value === "string" && value.length > 0 ? value : null;
}

function normalized(value) {
    return nonempty(value) ? value.trim().toLowerCase().slice(0, 32) : null;
}

function displayName(value) {
    if (!nonempty(value)) return null;
    var clean = value.replace(/[\u0000-\u001f\u007f\u061c\u200b-\u200f\u202a-\u202e\u2060-\u206f\ufeff]/g, " ")
        .replace(/\s+/g, " ").trim();
    return clean ? Array.from(clean).slice(0, 80).join("") : null;
}

/**
 * The human-facing form of an authenticated machine identity.
 *
 * Platform/provider words are used only when an inventory explicitly carries them. In
 * particular an opaque `mac_…` id is not proof that the host is macOS: Linux executors use the
 * same protocol namespace. Session labels and titles are deliberately ignored, because they name
 * the conversation rather than the computer that owns it.
 */
export function machinePresentation(value, copy) {
    value = typeof value === "string" ? { machine: value } : (value || {});
    var info = value.machineInfo || value.machine_info || value.machineDescriptor || {};
    var id = nonempty(value.machine) || nonempty(value.id) ||
        nonempty(value.identity && value.identity.machine) || "unknown-machine";
    var named = displayName(value.machineName) || displayName(value.machine_name) ||
        displayName(value.name) || displayName(info.name);
    var name = named || (id === LOCAL_SESSION_MACHINE
        ? displayName(copy && copy.webMachineThisMac)
        : (displayName(copy && copy.webStartMachine) || "Machine") + " · " + machineShortID(id)) ||
        "unknown-machine";
    var platform = normalized(value.machinePlatform) || normalized(value.machine_platform) ||
        normalized(value.platform) || normalized(info.platform) ||
        (id === LOCAL_SESSION_MACHINE ? "macos" : null);
    var provider = normalized(value.cloudProvider) || normalized(value.cloud_provider) ||
        normalized(value.provider) || normalized(info.provider);
    var kind = platform === "darwin" || platform === "macos" ? "mac"
        : platform === "linux" && provider === "aws" ? "linux-aws"
        : platform === "linux" ? "linux" : "unknown";
    var prefix = kind === "mac" ? displayName(copy && copy.webMachineMac)
        : kind === "linux-aws" ? displayName(copy && copy.webMachineLinuxAWS)
        : kind === "linux" ? displayName(copy && copy.webMachineLinux) : "";
    return Object.freeze({ id: id, name: name, platform: platform, provider: provider,
        kind: kind, label: prefix ? prefix + " · " + name : name });
}

export function machineShortID(id) {
    var shown = displayName(id) || "machine";
    return Array.from(shown).slice(-8).join("");
}

/** Duplicate display names need visible disambiguation: a tooltip does not exist on touch. */
export function machinePresentationForFleet(value, rows, copy) {
    var presented = machinePresentation(value, copy);
    var ids = Array.from(new Set((Array.isArray(rows) ? rows : []).map(function (row) {
        var other = machinePresentation(row, copy);
        return other.label === presented.label ? other.id : null;
    }).filter(Boolean))).sort();
    if (ids.length < 2) return presented;
    var short = machineShortID(presented.id);
    var collisions = ids.filter(function (id) { return machineShortID(id) === short; });
    if (collisions.length > 1) short += "-" + (collisions.indexOf(presented.id) + 1);
    return Object.freeze(Object.assign({}, presented,
        { label: presented.label + " · " + short }));
}

/**
 * An identity this module has already minted is closed. Re-deriving one from row fields would
 * read a `sessionId` that an identity does not carry — it spells that field `conversation` — and
 * silently return a key with `null` in the conversation position. Every caller that hands a
 * snapshot back (`sessionSelectionKey(open)`, and `byId(open)` through it) would then stop
 * matching the very row the selection came from.
 */
function mintedSelectionIdentity(value) {
    return value && typeof value === "object" && !Array.isArray(value) &&
        typeof value.key === "string" && value.key &&
        typeof value.rowId === "string" && value.rowId &&
        typeof value.machine === "string" && value.machine &&
        typeof value.session === "string" && value.session ? value : null;
}

/** The closed, immutable identity used for selection and stale-response checks. */
export function sessionSelectionIdentity(row, fallbackMachine) {
    var minted = mintedSelectionIdentity(row);
    if (minted) return minted;
    if (!row || typeof row !== "object" || Array.isArray(row)) return null;
    var carried = row.identity && typeof row.identity === "object" ? row.identity : null;
    var machine = nonempty(carried && carried.machine) || nonempty(row.machine) ||
        nonempty(fallbackMachine) || LOCAL_SESSION_MACHINE;
    var routeSession = nonempty(carried && carried.session) || nonempty(row.session) ||
        nonempty(row.id);
    var rowID = nonempty(row.id) || routeSession;
    if (!machine || !routeSession || !rowID) return null;
    var conversation = nonempty(row.sessionId);
    // Keep the established composer-pin spelling while making the owning route explicit.
    var key = JSON.stringify([rowID, machine, conversation || null]);
    return Object.freeze({
        key: key,
        rowId: rowID,
        machine: machine,
        session: routeSession,
        conversation: conversation,
        route: Object.freeze({ machine: machine, session: routeSession })
    });
}

export function sessionSelectionKey(row, fallbackMachine) {
    var identity = sessionSelectionIdentity(row, fallbackMachine);
    return identity ? identity.key : null;
}
