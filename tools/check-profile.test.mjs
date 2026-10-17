import { test } from 'node:test';
import assert from 'node:assert/strict';
import { summarize } from './check-profile.mjs';
test('profile separates concurrent package wall time, nested tests and failed results', async () => {
  const report = await summarize([
    { Time: '2026-10-17T00:00:00Z', Action: 'start', Package: 'a' },
    { Time: '2026-10-17T00:00:01Z', Action: 'pass', Package: 'a', Test: 'TestA/child', Elapsed: 1 },
    { Time: '2026-10-17T00:00:02Z', Action: 'fail', Package: 'b', Test: 'TestB', Elapsed: 2 },
    { Time: '2026-10-17T00:00:03Z', Action: 'pass', Package: 'a', Elapsed: 3 },
    { Time: '2026-10-17T00:00:03Z', Action: 'fail', Package: 'b', Elapsed: 3 },
  ].map(JSON.stringify));
  assert.equal(report.wallSeconds, 3);
  assert.equal(report.packages.length, 2);
  assert.equal(report.tests[0].test, 'TestB');
  assert.equal(report.tests[0].outcome, 'fail');
});
test('invalid or empty streams cannot claim a measured runtime', async () => {
  assert.equal((await summarize([])).wallSeconds, null);
  await assert.rejects(summarize(['invalid']), SyntaxError);
});
