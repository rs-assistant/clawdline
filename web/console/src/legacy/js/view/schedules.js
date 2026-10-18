import { esc } from "../core/esc.js";
import { T } from "../core/i18n.js";

var section = document.getElementById("schedules");
var count = document.getElementById("schedules-count");
var rows = document.getElementById("schedule-rows");

function relativeTime(unix, at) {
    if (!unix) return "";
    var seconds = unix - (at || Date.now() / 1000);
    var absolute = Math.abs(seconds);
    var unit = absolute < 90 * 60 ? "minute" : (absolute < 36 * 3600 ? "hour" : "day");
    var size = unit === "minute" ? 60 : (unit === "hour" ? 3600 : 86400);
    var value = Math.round(seconds / size);
    if (!value) value = seconds < 0 ? -1 : 1;
    try {
        return new Intl.RelativeTimeFormat(document.documentElement.lang || undefined,
            { numeric: "auto" }).format(value, unit);
    } catch (e) {
        var amount = Math.abs(value) + unit.charAt(0);
        return value < 0 ? amount + " ago" : "in " + amount;
    }
}

function result(value) {
    var state = String(value || "").toLowerCase();
    if (state === "success") return { state: state, label: T.webTaskDone };
    if (state === "failure" || state === "timeout" || state === "cancelled"
        || state === "spawn_failed") return { state: state, label: T.webTaskFailed };
    if (state === "queued" || state === "spawning" || state === "briefed") {
        return { state: state, label: T.webTaskRunning };
    }
    return { state: "none", label: "—" };
}

/** Resolve a retained occurrence against the bounded opaque-place listing.
 *
 * The run owns this path, not the schedule's current task template: editing a schedule must not
 * move an older conversation to a directory it never ran in. */
export function scheduleRunPlace(run, places) {
    var path = run && run.project_dir;
    return (places || []).filter(function (place) {
        return place && place.path === path;
    })[0] || null;
}

/** One schedule's retained task records, newest first as the server supplied them.
 *
 * A run can do one of three honest things: open the terminal that is still on the Session list,
 * resume a proven conversation after that tab has gone, or remain a readable result with no
 * action. `terminalIsOpen` is injected so the small renderer stays testable without importing the
 * live Session store. */
export function scheduleRunsHTML(runs, at, terminalIsOpen) {
    return (runs || []).map(function (run) {
        run = run || {};
        var outcome = result(run.state);
        var open = !!(run.terminal_id && typeof terminalIsOpen === "function"
            && terminalIsOpen(run.terminal_id));
        var action = open ? "open" : (run.session_id ? "resume" : "none");
        var actionLabel = open ? T.webResumeLive
            : (action === "resume" ? T.webResumeWith : outcome.label);
        var created = run.created ? new Date(run.created * 1000) : null;
        var when = created ? created.toLocaleString() : "";
        var relative = run.created ? relativeTime(run.created, at) : "";
        var assistant = String(run.assistant || "");
        var project = String(run.project_dir || "");
        var projectParts = project.split("/").filter(Boolean);
        var projectLabel = project === "/" ? "/" : (projectParts.pop() || "");
        // The timestamp already owns the first row. Repeating the same relative time under the
        // summary made the compact phone layout look like two different timestamps.
        var meta = [assistant, projectLabel].filter(Boolean).join(" · ");
        var summary = run.summary || "";
        return '<li class="schedule-run">' +
            '<button type="button" class="schedule-run-button" data-task-id="' +
                esc(run.task_id || "") + '" data-action="' + action + '"' +
                (action === "none" ? " disabled" : "") + '>' +
                '<span class="schedule-run-state" data-state="' + esc(outcome.state) + '">' +
                    esc(outcome.label) + '</span>' +
                '<time class="schedule-run-time"' + (when ? ' title="' + esc(when) + '"' : '') +
                    '>' + esc(relative || when) + '</time>' +
                // The summary is clamped to two lines in schedules.css. The title keeps the
                // whole sentence reachable where a pointer exists, without letting one verbose
                // run bury the ones under it.
                (summary ? '<span class="schedule-run-summary" title="' + esc(summary) + '">' +
                    esc(summary) + '</span>' : '') +
                '<span class="schedule-run-meta"' +
                    (project ? ' title="' + esc(project) + '"' : '') + '>' + esc(meta) + '</span>' +
                '<span class="schedule-run-action">' + esc(actionLabel) + '</span>' +
            '</button></li>';
    }).join("");
}
