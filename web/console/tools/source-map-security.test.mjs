import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { test } from "node:test";

// Exercise the parser PostCSS uses, rather than asserting a lockfile version.
const { SourceMapConsumer, SourceMapGenerator } = createRequire(import.meta.url)("source-map-js");
const basic = { version: 3, sources: ["fixture.css"], names: [], mappings: "AAAA" };
const indexed = (line, column = 0, map = basic) => ({
  version: 3, sections: [{ offset: { line, column }, map }],
});

test("CSS source maps reject section offsets that can stall map generation", () => {
  // Do not serialize the vulnerable map: the old constructor accepting it is
  // sufficient to fail this regression without hanging the test process.
  assert.throws(() => new SourceMapConsumer(indexed(2 ** 32)), /offset/i);
  assert.throws(() => new SourceMapConsumer(indexed(Infinity)), /offset/i);
  assert.throws(() => new SourceMapConsumer(indexed("100")), /offset/i);
  assert.throws(() => new SourceMapConsumer(indexed(0, -1)), /offset/i);
  assert.throws(() => new SourceMapConsumer(indexed(0.5)), /offset/i);
});

test("nested CSS source map offsets cannot bypass the generation bound", () => {
  assert.throws(() => new SourceMapConsumer(indexed(6_000_000, 0, indexed(6_000_000))), /offset/i);
});

test("ordinary indexed CSS maps still generate usable mappings", () => {
  const consumer = new SourceMapConsumer(indexed(2));
  const generator = new SourceMapGenerator();
  consumer.eachMapping((mapping) => generator.addMapping({
    generated: { line: mapping.generatedLine, column: mapping.generatedColumn },
    original: { line: mapping.originalLine, column: mapping.originalColumn },
    source: mapping.source,
  }));
  const generated = generator.toJSON();
  const roundTrip = new SourceMapConsumer(generated);
  assert.deepEqual(roundTrip.originalPositionFor({ line: 3, column: 0 }), {
    source: "fixture.css", line: 1, column: 0, name: null,
  });
});
