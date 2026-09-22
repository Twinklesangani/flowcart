import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

const source = readFileSync(new URL("../src/lib/request-generation.ts", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext } });
const { isCurrentGeneration } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

test("audit generation ignores an old response after a newer filter response", () => {
  assert.equal(isCurrentGeneration(1, 2), false);
  assert.equal(isCurrentGeneration(2, 2), true);
});