import assert from "node:assert/strict";
import test from "node:test";
import { bindWorktreeLifecycle } from "../../legacy/js/view/worktrees.js";
import { lifecyclePlace } from "../../legacy/lifecycle-place.ts";

function element() {
    return { textContent: "", dataset: {}, disabled: false, hidden: false,
        firstChild: null, addEventListener() {} };
}

function lifecycle(projectId) {
    const elements = Object.fromEntries([
        "project-worktree-lifecycle", "project-worktree-status", "project-worktree-summary",
        "project-worktree-rows", "project-worktree-refresh"
    ].map(id => [id, element()]));
    const view = bindWorktreeLifecycle(elements, {
        document: { documentElement: { lang: "en" } },
        read: async () => ({ machine: "this-mac", projectWorktreeLifecycle: {
            schemaVersion: 1, project: { id: projectId }, rows: [], complete: true,
            counts: { rows: 0 }
        } })
    });
    return { elements, view };
}

test("a Start Place opens the worktrees of its canonical Project", async () => {
    const { elements, view } = lifecycle("project-canonical");
    await view.enter(lifecyclePlace({ id: "place-directory", projectId: "project-canonical" }));
    assert.match(elements["project-worktree-status"].textContent, /Observation complete/);
    assert.equal(elements["project-worktree-lifecycle"].dataset.complete, "true");
});

test("a worktree answer for a different Project remains refused", async () => {
    const { elements, view } = lifecycle("project-other");
    await view.enter(lifecyclePlace({ id: "place-directory", projectId: "project-canonical" }));
    assert.match(elements["project-worktree-status"].textContent, /worktree_project_mismatch/);
});
