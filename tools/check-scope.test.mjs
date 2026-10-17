import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { affectedTests, decodePackages, selectGo, selectWeb } from './check-scope.mjs';

test('Go changes run their own tests and compile transitive callers including test-only imports', () => {
  const packages = [
    { Dir: '/repo/internal/leaf', ImportPath: 'm/leaf' },
    { Dir: '/repo/internal/middle', ImportPath: 'm/middle', Imports: ['m/leaf'] },
    { Dir: '/repo/cmd/app', ImportPath: 'm/app', TestImports: ['m/middle'] },
    { Dir: '/repo/internal/unrelated', ImportPath: 'm/unrelated' },
  ];
  assert.deepEqual(selectGo(['internal/leaf/a.go'], packages, '/repo'), { tests: ['m/leaf'], compile: ['m/app', 'm/middle'] });
  assert.deepEqual(selectGo(['internal/leaf/a_test.go'], packages, '/repo'), { tests: ['m/leaf'], compile: [] });
  assert.deepEqual(selectGo(['internal/leaf/testdata/fixture.json'], packages, '/repo'), { tests: ['m/leaf'], compile: [] });
  assert.throws(() => selectGo(['internal/deleted/a.go'], packages, '/repo'), /no package owns/);
  assert.throws(() => selectGo([], [{ Error: {} }], '/repo'), /unreadable/);
});

test('Go JSON inventory decoding handles quotes and concatenated records, rejecting truncation', () => {
  const data = [{ Dir: '/repo/a{b}', ImportPath: 'm/a', Doc: 'quote " }' }, { Dir: '/repo/b', ImportPath: 'm/b' }];
  assert.deepEqual(decodePackages(data.map(v => JSON.stringify(v)).join('\n')), data);
  for (const broken of ['', '{"Dir":', '{"Dir":"unterminated']) assert.throws(() => decodePackages(broken));
});

test('web dependency closure includes callers and modified tests without selecting unrelated tests', () => {
  const imports = new Map([['leaf.ts', []], ['middle.ts', ['leaf.ts']], ['related.test.ts', ['middle.ts']], ['other.test.ts', []]]);
  assert.deepEqual(affectedTests(['leaf.ts'], imports, ['related.test.ts', 'other.test.ts']), ['related.test.ts']);
  assert.deepEqual(affectedTests(['other.test.ts'], imports, ['related.test.ts', 'other.test.ts']), ['other.test.ts']);
});

test('real TypeScript resolution follows .js imports, keeps filesystem readers and falls back for fixtures', async () => {
  const root = mkdtempSync(join(tmpdir(), 'check-web-scope-'));
  const write = (p, text) => { mkdirSync(join(root, p, '..'), { recursive: true }); writeFileSync(join(root, p), text); };
  try {
    for (const dir of ['web/console/src', 'web/console/tools', 'web/core/src', 'web/contract/src']) mkdirSync(join(root, dir), { recursive: true });
    write('web/console/tsconfig.json', JSON.stringify({ compilerOptions: { allowJs: true, module: 'esnext', moduleResolution: 'bundler' }, include: ['src'] }));
    write('web/console/src/leaf.ts', 'export const leaf=1;');
    write('web/console/src/middle.ts', 'export {leaf} from "./leaf.js";');
    write('web/console/src/related.test.ts', 'import {leaf} from "./middle.js";');
    write('web/console/src/other.test.ts', 'export {};');
    write('web/console/tools/read.test.mjs', 'import {readFileSync} from "node:fs";');
    let result = await selectWeb(['web/console/src/leaf.ts'], root);
    assert.equal(result.broad, false);
    assert.deepEqual(result.tests, ['web/console/src/related.test.ts', 'web/console/tools/read.test.mjs']);
    result = await selectWeb(['web/package-lock.json'], root);
    assert.equal(result.broad, true);
    assert.equal(result.tests.length, 3);
    write('web/console/src/dynamic.ts', 'export const load=(name:string)=>import(name);');
    result = await selectWeb(['web/console/src/leaf.ts'], root);
    assert.equal(result.broad, true);
    assert.equal(result.tests.length, 2);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
