/* ==========================================================================
   What to call a project when it has no name of its own

   The mark a project has until somebody draws it one is the daemon's: it
   always sends a Project icon, including its deterministic fallback
   (`legacy/bridge.ts` `markForSession`). What is left here is the label.

   **Nothing here reads `document`, `window` or `localStorage`.**
   ========================================================================== */


/**
 * What to call the project on a button **that is not a control**.
 *
 * The tail of the path, which is the part that identifies a project — the same choice
 * `shortPath` makes about which end to keep. **This is a label and never a scope key**, and it
 * has exactly one caller left: the mark on a transport with no snippets route at all, where it
 * is a picture rather than a shortcut. Everywhere the header names the project a press would
 * open, it uses the `project.label` the Mac answers beside the list, because that one has been
 * through the registry match, the subdirectory prefix and the worktree fold — and a name derived
 * here disagreed with the sheet's heading exactly where that rule earns its keep. An inert mark
 * makes no claim about a snippet scope, and a button with no accessible name at all is worse
 * than one named after the folder it stands for.
 */
export function projectLabel(key) {
    var parts = String(key || "").replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || "";
}
