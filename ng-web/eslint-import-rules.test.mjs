// The Gatbase import rules of eslint.config.js must reject every import that couples a core-owned
// file to an optional module (or a module to the composition root): a core-only generated project
// does not ship the module folders and would stop compiling.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';

const cwd = path.dirname(fileURLToPath(import.meta.url));
const eslint = new ESLint({ cwd });

/** Returns the no-restricted-imports messages ESLint reports for `code` at `relativePath`. */
async function restrictedImports(relativePath, code) {
  const [result] = await eslint.lintText(code, { filePath: path.join(cwd, relativePath) });
  const messages = result.messages.filter((message) => message.ruleId === 'no-restricted-imports');

  return messages;
}

const moduleImport = (specifier, name) => `import { ${name} } from '${specifier}';\nexport const x = ${name};\n`;

const rejected = [
  ['the home page (core-owned) imports a module by alias', 'src/app/home/repro.ts', moduleImport('@/gatbase/parameters/module', 'parametersModule'), 'Core-owned code imports a module only'],
  ['generic core code imports a module by alias', 'src/app/core/repro.ts', moduleImport('@/gatbase/feature-flags/module', 'featureFlagsModule'), 'Core-owned code imports a module only'],
  ['the bootstrap file imports a module by alias', 'src/main.repro.ts', moduleImport('@/gatbase/parameters/module', 'parametersModule'), 'Core-owned code imports a module only'],
  ['the core module imports a module by relative path', 'src/app/gatbase/core/login/repro.ts', moduleImport('../../parameters/module', 'parametersModule'), 'The core module never imports another'],
  ['the core module imports a module by a deep relative path', 'src/app/gatbase/core/login/repro.ts', moduleImport('../../../gatbase/parameters/module', 'parametersModule'), 'The core module never imports another'],
  ['the shell imports a core file deeply by alias', 'src/app/layout/repro.ts', moduleImport('@/gatbase/core/login', 'login'), 'The skeleton imports Gatbase only'],
  ['the shell imports a module by relative path', 'src/app/layout/component/repro.ts', moduleImport('../../gatbase/parameters/module', 'parametersModule'), 'The skeleton imports Gatbase only'],
  ['a module imports the composition root by relative path', 'src/app/gatbase/parameters/repro.ts', moduleImport('../modules', 'gatbaseModules'), 'A module never imports a sibling module folder'],
  ['a module imports a sibling by a deep relative path', 'src/app/gatbase/parameters/repro.ts', moduleImport('../../gatbase/feature-flags/module', 'featureFlagsModule'), 'A module never imports a sibling module folder']
];

const allowed = [
  ['a module imports core through its barrel', 'src/app/gatbase/parameters/repro.ts', moduleImport('@/gatbase/core', 'coreModule')],
  ['the shell imports the composition root by alias', 'src/app/layout/repro.ts', moduleImport('@/gatbase/modules', 'gatbaseModules')],
  ['the shell imports the core barrel by alias', 'src/app/layout/repro.ts', moduleImport('@/gatbase/core', 'coreModule')],
  ['the home page imports the composition root by alias', 'src/app/home/repro.ts', moduleImport('@/gatbase/modules', 'gatbaseModules')],
  ['the bootstrap file imports the core barrel', 'src/main.repro.ts', moduleImport('@/gatbase/core', 'coreModule')]
];

for (const [name, file, code, expected] of rejected) {
  test(`rejects: ${name}`, async () => {
    const messages = await restrictedImports(file, code);

    assert.ok(
      messages.some((message) => message.message.includes(expected)),
      `${file} was not rejected by the expected rule (${expected}); got: ${JSON.stringify(messages.map((message) => message.message))}`
    );
  });
}

for (const [name, file, code] of allowed) {
  test(`allows: ${name}`, async () => {
    const messages = await restrictedImports(file, code);

    assert.deepEqual(
      messages.map((message) => message.message),
      []
    );
  });
}
