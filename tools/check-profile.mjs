#!/usr/bin/env node
// Summarize go test -json without treating compilation, package concurrency,
// or cached package duration as the sum of individual test durations.
import { createReadStream, writeFileSync } from 'node:fs';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
export async function summarize(lines) {
  const packages = [], tests = [];
  let first, last;
  for await (const line of lines) {
    if (!line.trim()) continue;
    const event = JSON.parse(line);
    if (event.Time) {
      const time = Date.parse(event.Time);
      first = first === undefined ? time : Math.min(first, time);
      last = last === undefined ? time : Math.max(last, time);
    }
    if (!['pass', 'fail', 'skip'].includes(event.Action)) continue;
    const row = { package: event.Package, outcome: event.Action, seconds: event.Elapsed ?? 0 };
    if (event.Test) tests.push({ ...row, test: event.Test });
    else packages.push(row);
  }
  packages.sort((a, b) => b.seconds - a.seconds);
  tests.sort((a, b) => b.seconds - a.seconds);
  return { wallSeconds: first === undefined ? null : (last - first) / 1000, packages, tests };
}
if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [input, output] = process.argv.slice(2);
  if (!input || !output) throw new Error('usage: node tools/check-profile.mjs events.jsonl summary.json');
  const report = await summarize(createInterface({ input: createReadStream(input), crlfDelay: Infinity }));
  if (process.argv.includes('--require-tests') && report.tests.length === 0) throw new Error('Selected Go module ran no tests; use full package coverage');
  if (report.packages.length === 0) throw new Error('Go profile contains no completed packages');
  writeFileSync(output, JSON.stringify(report, null, 2) + '\n');
  console.log(`Go profile: ${report.packages.length} packages, ${report.tests.length} test results; details: ${output}`);
}
