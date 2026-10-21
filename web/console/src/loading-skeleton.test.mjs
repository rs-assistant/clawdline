import test from "node:test";
import assert from "node:assert/strict";
import { loadingSkeletonHTML } from "./loading-skeleton.js";

test("a loading label cannot introduce active markup into a DOM-owned panel", () => {
    const html = loadingSkeletonHTML('<img src="https://example.test/" onerror="bad()">', 'SK-13" onclick="bad()');
    assert.ok(html.includes('&lt;img'));
    assert.ok(!/<img|<script|<button|<input|<a\s/.test(html));
    assert.ok(!/\sonclick="/.test(html));
    assert.match(html, /role="status"/);
    assert.match(html, /aria-hidden="true"/);
});

test("an unknown shape cannot add an attribute or executable content", () => {
    const html = loadingSkeletonHTML("Reading…", "SK-26", 'text" onload="bad()');
    assert.ok(!html.includes("onload"));
    assert.match(html, /loading-skeleton-rows/);
    assert.match(html, /Reading…/);
});
