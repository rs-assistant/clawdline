import { T, fill } from "../core/i18n.js";

/* --------------------------------------------------------------------------
   The coordinator's row

   `session.coordinator` is the only boundary into this feature. The sessions API is the
   broker-authenticated source; task ancestry, labels and words such as "father" never infer
   coordinator identity. A missing field therefore remains exactly the old session row.

   Every visible word is a `T` name resolved at render time, never at module load: the
   translated strings arrive from `/v1/strings` after this module is evaluated, and a label
   frozen at import time would stay English forever.
   -------------------------------------------------------------------------- */

function nonempty(value) {
    return typeof value === "string" && value.trim() ? value.trim() : null;
}

/** Normalize the optional sessions-API boundary, or return null without touching the row. */
export function coordinatorForSession(session) {
    var source = session && session.coordinator;
    if (!source || typeof source !== "object" || Array.isArray(source)) return null;
    return {
        label: nonempty(source.label) || "Clawdfather",
        status: source.status === "online" ? "online"
            : (source.status === "offline" ? "offline" : "unknown"),
        commands: Array.isArray(source.commands) ? source.commands : []
    };
}

/** Small, testable facts from which the list builds the coordinator-only mark and badge. */
export function coordinatorRowModel(session) {
    var coordinator = coordinatorForSession(session);
    if (!coordinator) return null;
    return {
        badge: "Clawdfather",
        label: coordinator.label,
        mark: {
            role: "button",
            ariaHaspopup: "dialog",
            ariaLabel: fill(T.webCoordOpenControls, { name: coordinator.label })
        }
    };
}
